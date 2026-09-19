package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthorizationURLEscapesStateAndRedirect(t *testing.T) {
	got, err := BuildAuthorizationURL(OAuthConfig{
		AuthorizeURL: "https://hh.test/oauth/authorize",
		ClientID:     "operator-client", RedirectURI: "http://127.0.0.1:9876/callback?next=one&two=2",
	}, "state value")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("state") != "state value" {
		t.Fatalf("query=%v", parsed.Query())
	}
	if parsed.Query().Get("redirect_uri") != "http://127.0.0.1:9876/callback?next=one&two=2" {
		t.Fatalf("redirect_uri=%q", parsed.Query().Get("redirect_uri"))
	}
	if parsed.Query().Get("client_id") != "operator-client" || parsed.Query().Get("response_type") != "code" {
		t.Fatalf("query=%v", parsed.Query())
	}
	if parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("code_challenge") == "" {
		t.Fatalf("missing PKCE parameters: %v", parsed.Query())
	}
	if strings.Contains(got, "operator-secret") {
		t.Fatal("authorization URL contains a client secret")
	}
}

func TestNewAuthorizationStateIsNonEmptyAndRandom(t *testing.T) {
	one, err := NewAuthorizationState()
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewAuthorizationState()
	if err != nil {
		t.Fatal(err)
	}
	if one == "" || two == "" || one == two {
		t.Fatalf("states=%q,%q", one, two)
	}
}

func TestAuthorizationSessionUsesPKCES256(t *testing.T) {
	session, err := NewAuthorizationSession(OAuthConfig{
		AuthorizeURL: "https://hh.test/oauth/authorize",
		TokenURL:     "https://hh.test/oauth/token",
		ClientID:     "operator-client", RedirectURI: "http://127.0.0.1/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(session.AuthorizationURL())
	if err != nil {
		t.Fatal(err)
	}
	if session.State() == "" || parsed.Query().Get("state") != session.State() {
		t.Fatalf("state mismatch: session=%q query=%q", session.State(), parsed.Query().Get("state"))
	}
	digest := sha256.Sum256([]byte(session.codeVerifier))
	want := base64.RawURLEncoding.EncodeToString(digest[:])
	if parsed.Query().Get("code_challenge") != want {
		t.Fatalf("code_challenge=%q, want %q", parsed.Query().Get("code_challenge"), want)
	}
	if session.codeVerifier == "" || strings.Contains(session.AuthorizationURL(), session.codeVerifier) {
		t.Fatal("verifier was not kept private from the authorization URL")
	}
}

func TestExchangeAuthorizationCodeUsesFormAndParsesExpiry(t *testing.T) {
	const authCode = "authorization-code-fixture"
	const verifier = "verifier-fixture"
	var gotForm url.Values
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-fixture","refresh_token":"refresh-fixture","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	tokens, err := ExchangeAuthorizationCode(context.Background(), OAuthConfig{
		TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret",
		RedirectURI: "http://127.0.0.1/callback", HTTPClient: server.Client(), Now: func() time.Time { return now },
	}, authCode, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type=%q", gotContentType)
	}
	for key, want := range map[string]string{
		"grant_type": "authorization_code", "code": authCode, "code_verifier": verifier,
		"client_id": "operator-client", "client_secret": "operator-secret",
		"redirect_uri": "http://127.0.0.1/callback",
	} {
		if gotForm.Get(key) != want {
			t.Fatalf("form[%q]=%q, want %q", key, gotForm.Get(key), want)
		}
	}
	if tokens.AccessToken != "access-fixture" || tokens.RefreshToken != "refresh-fixture" || tokens.TokenType != "Bearer" || !tokens.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected token metadata: type=%q expiry=%s", tokens.TokenType, tokens.ExpiresAt)
	}
}

func TestTokenExchangeRejectsExpiresInBeforeDurationOverflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-fixture","token_type":"Bearer","expires_in":`+strconv.FormatInt(math.MaxInt64/int64(time.Second)+1, 10)+`}`)
	}))
	defer server.Close()

	_, err := ExchangeAuthorizationCode(context.Background(), OAuthConfig{
		TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret",
		RedirectURI: "http://127.0.0.1/callback", HTTPClient: server.Client(),
	}, "code-fixture", "verifier-fixture")
	if err == nil || !strings.Contains(err.Error(), "response") {
		t.Fatalf("overflow expires_in error=%v", err)
	}
}

func TestTokenExchangeRequiresSecretAndRedirectBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()

	tests := []struct {
		name   string
		config OAuthConfig
	}{
		{
			name:   "missing client secret",
			config: OAuthConfig{TokenURL: server.URL, ClientID: "operator-client", RedirectURI: "http://127.0.0.1/callback", HTTPClient: server.Client()},
		},
		{
			name:   "missing redirect URI",
			config: OAuthConfig{TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret", HTTPClient: server.Client()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExchangeAuthorizationCode(context.Background(), tt.config, "code-fixture", "verifier-fixture")
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("token endpoint received %d requests", requests)
	}
}

func TestAuthorizationSessionRejectsStateMismatchAndProviderErrorBeforeExchange(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	session, err := NewAuthorizationSession(OAuthConfig{
		AuthorizeURL: "https://hh.test/oauth/authorize", TokenURL: server.URL,
		ClientID: "operator-client", ClientSecret: "operator-secret", RedirectURI: "http://127.0.0.1/callback",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ExchangeCallback(context.Background(), AuthorizationCallback{State: "wrong-state", Code: "code-fixture"}); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("state mismatch error=%v", err)
	}
	if _, err := session.ExchangeCallback(context.Background(), AuthorizationCallback{State: session.State(), Error: "access_denied", ErrorDescription: "private provider detail"}); err == nil || !strings.Contains(err.Error(), "provider") || strings.Contains(err.Error(), "access_denied") || strings.Contains(err.Error(), "private") {
		t.Fatalf("provider error=%v", err)
	}
	if requests != 0 {
		t.Fatalf("token endpoint received %d requests", requests)
	}
}

func TestRefreshTokensUsesFormAndRotatesRefreshToken(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":120}`)
	}))
	defer server.Close()
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	got, err := RefreshTokens(context.Background(), OAuthConfig{
		TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret", HTTPClient: server.Client(),
		RedirectURI: "http://127.0.0.1/callback",
		Now:         func() time.Time { return now },
	}, OAuthTokens{RefreshToken: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "rotated-refresh" || !got.ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("unexpected refresh result: type=%q expiry=%s", got.TokenType, got.ExpiresAt)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "old-refresh" {
		t.Fatalf("refresh form=%v", gotForm)
	}
}

func TestRefreshTokensPreservesRefreshTokenWhenProviderOmitsReplacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","token_type":"Bearer","expires_in":120}`)
	}))
	defer server.Close()
	got, err := RefreshTokens(context.Background(), OAuthConfig{TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret", RedirectURI: "http://127.0.0.1/callback", HTTPClient: server.Client()}, OAuthTokens{RefreshToken: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "old-refresh" {
		t.Fatalf("RefreshToken=%q, want preserved prior value", got.RefreshToken)
	}
}

func TestRefreshTokensRejectsExplicitEmptyRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"","token_type":"Bearer","expires_in":120}`)
	}))
	defer server.Close()

	_, err := RefreshTokens(context.Background(), OAuthConfig{
		TokenURL: server.URL, ClientID: "operator-client", ClientSecret: "operator-secret",
		RedirectURI: "http://127.0.0.1/callback", HTTPClient: server.Client(),
	}, OAuthTokens{RefreshToken: "old-refresh"})
	if err == nil || !strings.Contains(err.Error(), "response") {
		t.Fatalf("explicit empty refresh token error=%v", err)
	}
}

func TestOAuthErrorsDoNotContainSubmittedOrReturnedSecrets(t *testing.T) {
	const submitted = "authorization-secret-fixture"
	const returned = "returned-secret-fixture"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid","access_token":"returned-secret-fixture","refresh_token":"returned-secret-fixture"}`)
	}))
	defer server.Close()
	_, err := ExchangeAuthorizationCode(context.Background(), OAuthConfig{TokenURL: server.URL, ClientID: "client", ClientSecret: submitted, HTTPClient: server.Client()}, "code", "verifier")
	if err == nil || strings.Contains(err.Error(), submitted) || strings.Contains(err.Error(), returned) {
		t.Fatalf("unsafe OAuth error=%v", err)
	}
}
