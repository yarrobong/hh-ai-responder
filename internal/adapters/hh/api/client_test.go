package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryTokenStore struct {
	mu     sync.Mutex
	loaded OAuthTokens
	saved  []OAuthTokens
	err    error
}

func (s *memoryTokenStore) Load(context.Context) (OAuthTokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return OAuthTokens{}, s.err
	}
	return s.loaded, nil
}

func (s *memoryTokenStore) Save(_ context.Context, tokens OAuthTokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = tokens
	s.saved = append(s.saved, tokens)
	return nil
}

func (s *memoryTokenStore) Delete(context.Context) error { return nil }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func newAPIClient(t *testing.T, baseURL string, store TokenStore) *APIHHClient {
	t.Helper()
	client, err := NewAPIHHClient(APIClientOptions{
		BaseURL:       mustURL(t, baseURL),
		HTTPClient:    &http.Client{},
		TokenStore:    store,
		UserAgent:     "hh-ai-responder/test",
		Now:           func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
		Timeout:       2 * time.Second,
		MaxRetryAfter: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func validTokens() OAuthTokens {
	return OAuthTokens{
		AccessToken:  "access-token-fixture",
		RefreshToken: "refresh-token-fixture",
		TokenType:    "Bearer",
		ExpiresAt:    time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC),
	}
}

func TestAPIClientSendsBearerAndHHUserAgent(t *testing.T) {
	var gotMethod string
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	_, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).Get(context.Background(), "/me")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method=%q, want GET", gotMethod)
	}
	if got := gotHeader.Get("Authorization"); got != "Bearer access-token-fixture" {
		t.Fatalf("Authorization=%q", got)
	}
	if got := gotHeader.Get("User-Agent"); got != "hh-ai-responder/test" {
		t.Fatalf("User-Agent=%q", got)
	}
	if got := gotHeader.Get("Accept"); got == "" {
		t.Fatal("Accept header is missing")
	}
}

func TestAPIClientConstructsOnlyGETRequests(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	if _, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).Get(context.Background(), "/read-only"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0] != http.MethodGet {
		t.Fatalf("methods=%v, want one GET", methods)
	}
}

func TestAPIClientMapsHTTPStatusesToSafeTypedErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   APIErrorCode
		wantStatus int
	}{
		{name: "auth required", status: http.StatusUnauthorized, body: `{"message":"login required"}`, wantCode: APIErrorAuthRequired, wantStatus: http.StatusUnauthorized},
		{name: "token expired", status: http.StatusUnauthorized, body: `{"oauth_error":"token_expired"}`, wantCode: APIErrorTokenExpired, wantStatus: http.StatusUnauthorized},
		{name: "token revoked", status: http.StatusUnauthorized, body: `{"oauth_error":"token_revoked"}`, wantCode: APIErrorTokenRevoked, wantStatus: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, body: `{"type":"access_denied"}`, wantCode: APIErrorForbidden, wantStatus: http.StatusForbidden},
		{name: "application missing", status: http.StatusNotFound, body: `{"description":"not found"}`, wantCode: APIErrorApplicationNotFound, wantStatus: http.StatusNotFound},
		{name: "remote error", status: http.StatusInternalServerError, body: `{"description":"upstream failed"}`, wantCode: APIErrorRemote, wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-ID", "request-fixture")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			tokens := validTokens()
			tokens.RefreshToken = ""
			_, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: tokens}).Get(context.Background(), "/resource")
			if err == nil {
				t.Fatal("expected typed API error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error=%T %v, want *APIError", err, err)
			}
			if apiErr.Code != tt.wantCode || apiErr.Status != tt.wantStatus || apiErr.Path != "/resource" || apiErr.RequestID != "request-fixture" {
				t.Fatalf("api error=%+v, want code=%s status=%d path=/resource request id", apiErr, tt.wantCode, tt.wantStatus)
			}
		})
	}
}

func TestAPIClientMapsRateLimitWithBoundedRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "999999999999999999999999")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"description":"private response body"}`)
	}))
	defer server.Close()

	_, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).Get(context.Background(), "/limited")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != APIErrorRateLimited {
		t.Fatalf("error=%T %v, want RATE_LIMITED", err, err)
	}
	if apiErr.RetryAfter != 3*time.Second {
		t.Fatalf("RetryAfter=%s, want 3s", apiErr.RetryAfter)
	}
	if strings.Contains(err.Error(), "private response body") {
		t.Fatalf("error exposed response body: %v", err)
	}
}

func TestAPIClientRespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).Get(ctx, "/slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%T %v, want context deadline exceeded", err, err)
	}
}

func TestAPIClientRefreshesOnceOnExpiredResponseAndRetriesGET(t *testing.T) {
	var apiCalls, refreshCalls int
	var methods []string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		methods = append(methods, r.Method)
		if apiCalls == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer old-access" {
				t.Errorf("first Authorization=%q", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"oauth_error":"token_expired"}`)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
			t.Errorf("retry Authorization=%q", got)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer apiServer.Close()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		if r.Method != http.MethodPost {
			t.Errorf("refresh method=%s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	store := &memoryTokenStore{loaded: OAuthTokens{
		AccessToken: "old-access", RefreshToken: "old-refresh", TokenType: "Bearer",
		ExpiresAt: time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC),
	}}
	client, err := NewAPIHHClient(APIClientOptions{
		BaseURL: mustURL(t, apiServer.URL), HTTPClient: &http.Client{}, TokenStore: store,
		UserAgent: "hh-ai-responder/test", Now: func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
		OAuthConfig: OAuthConfig{TokenURL: tokenServer.URL, ClientID: "client-fixture", ClientSecret: "secret-fixture", RedirectURI: "http://127.0.0.1/callback", HTTPClient: tokenServer.Client(), Now: func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/me"); err != nil {
		t.Fatal(err)
	}
	if apiCalls != 2 || refreshCalls != 1 || len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodGet {
		t.Fatalf("apiCalls=%d refreshCalls=%d methods=%v", apiCalls, refreshCalls, methods)
	}
	if len(store.saved) != 1 || store.saved[0].AccessToken != "new-access" || store.saved[0].RefreshToken != "new-refresh" {
		t.Fatalf("saved tokens=%+v", store.saved)
	}
}

func TestAPIClientErrorsNeverExposeSecretsOrResponseBodies(t *testing.T) {
	const accessToken = "synthetic-access-token"
	const clientSecret = "synthetic-client-secret"
	const responseSecret = "synthetic-response-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", responseSecret)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","client_secret":"`+clientSecret+`","description":"`+responseSecret+`"}`)
	}))
	defer server.Close()

	_, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: OAuthTokens{
		AccessToken: accessToken, RefreshToken: "synthetic-refresh-token", TokenType: "Bearer",
		ExpiresAt: time.Now().Add(time.Hour),
	}}).Get(context.Background(), "/secret-free")
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	for _, secret := range []string{accessToken, clientSecret, responseSecret, "synthetic-refresh-token", "Set-Cookie", "Authorization"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error exposed %q: %s", secret, message)
		}
	}
}
