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

	hhreadport "hh-ai-responder/internal/ports/hhread"
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
		t.Fatal("Authorization header mismatch")
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
				t.Error("first Authorization header mismatch")
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"oauth_error":"token_expired"}`)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
			t.Error("retry Authorization header mismatch")
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
		t.Fatalf("refresh token persistence mismatch: saved_count=%d", len(store.saved))
	}
}

func TestAPIClientRefreshFailuresRemainRemoteErrors(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		status        int
		invalidConfig bool
		closeServer   bool
	}{
		{name: "malformed response", response: "{not-json", status: http.StatusOK},
		{name: "transient provider failure", response: `{"error":"temporary"}`, status: http.StatusServiceUnavailable},
		{name: "invalid configuration", invalidConfig: true},
		{name: "network failure", closeServer: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"oauth_error":"token_expired"}`)
			}))
			defer apiServer.Close()

			refreshURL := ""
			var refreshClient *http.Client
			var refreshServer *httptest.Server
			if !tt.invalidConfig {
				refreshServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.response)
				}))
				refreshURL = refreshServer.URL
				refreshClient = refreshServer.Client()
				if tt.closeServer {
					refreshServer.Close()
				}
				if !tt.closeServer {
					defer refreshServer.Close()
				}
			}

			config := OAuthConfig{
				TokenURL: refreshURL, ClientID: "client-fixture", ClientSecret: "secret-fixture",
				RedirectURI: "http://127.0.0.1/callback", HTTPClient: refreshClient,
			}
			if tt.invalidConfig {
				config.ClientID = ""
			}
			client, err := NewAPIHHClient(APIClientOptions{
				BaseURL: mustURL(t, apiServer.URL), TokenStore: &memoryTokenStore{loaded: OAuthTokens{
					AccessToken: "old-access", RefreshToken: "old-refresh", TokenType: "Bearer",
					ExpiresAt: time.Now().Add(time.Hour),
				}},
				UserAgent: "hh-ai-responder/test", OAuthConfig: config,
			})
			if err != nil {
				t.Fatal("client construction failed")
			}
			_, err = client.Get(context.Background(), "/me")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != APIErrorRemote {
				t.Fatalf("case classified as %v, want REMOTE_ERROR", apiErrCode(err))
			}
		})
	}
}

func TestAPIClientRefreshCancellationPreservesContextError(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"oauth_error":"token_expired"}`)
	}))
	defer apiServer.Close()
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer refreshServer.Close()

	client, err := NewAPIHHClient(APIClientOptions{
		BaseURL: mustURL(t, apiServer.URL), TokenStore: &memoryTokenStore{loaded: OAuthTokens{
			AccessToken: "old-access", RefreshToken: "old-refresh", TokenType: "Bearer",
			ExpiresAt: time.Now().Add(time.Hour),
		}}, UserAgent: "hh-ai-responder/test",
		OAuthConfig: OAuthConfig{TokenURL: refreshServer.URL, ClientID: "client-fixture", ClientSecret: "secret-fixture", RedirectURI: "http://127.0.0.1/callback", HTTPClient: refreshServer.Client()},
	})
	if err != nil {
		t.Fatal("client construction failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.Get(ctx, "/me")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != APIErrorRemote || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh cancellation classification=%v context_preserved=%v", apiErrCode(err), errors.Is(err, context.DeadlineExceeded))
	}
}

func apiErrCode(err error) APIErrorCode {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.Code
	}
	return "<non-api-error>"
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
			t.Fatal("error redaction check failed")
		}
	}
}

func TestAPIHHClientCurrentUserReadsMe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/me" {
			t.Fatalf("request=%s %s, want GET /me", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"user-17","auth_type":"applicant"}`)
	}))
	defer server.Close()

	got, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "user-17" || got.AuthType != "applicant" {
		t.Fatalf("user=%+v", got)
	}
}

func TestAPIHHClientReadsResumeListAndDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/resumes/mine":
			_, _ = io.WriteString(w, `{"items":[{"id":"resume-1","title":"Python backend","area":{"name":"Yekaterinburg"},"skills":"Python, Django","skill_set":["Python","Django"],"salary":{"amount":90000,"currency":"RUR"},"total_experience":36}]}`)
		case "/resumes/resume-1":
			_, _ = io.WriteString(w, `{"id":"resume-1","title":"Python backend","description":"API integrations","experience":{"name":"1-3 years"},"updated_at":"2026-09-18T10:00:00Z"}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()})
	resumes, err := client.ReadResumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(resumes) != 1 || resumes[0].ID != "resume-1" || resumes[0].Title != "Python backend" || len(resumes[0].Skills) != 2 || resumes[0].Skills[0] != "Python" || resumes[0].Area != "Yekaterinburg" || resumes[0].Salary != "90000" || resumes[0].Currency != "RUR" || resumes[0].TotalExperienceMonths != 36 || !resumes[0].TotalExperienceMonthsKnown {
		t.Fatalf("resumes=%+v", resumes)
	}
	detail, err := client.ReadResume(context.Background(), "resume-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "resume-1" || detail.Description != "API integrations" || detail.Experience != "1-3 years" || detail.UpdatedAt.IsZero() {
		t.Fatalf("detail=%+v", detail)
	}
}

func TestAPIHHClientReadsVacanciesWithAPIQueryAndPagination(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vacancies" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		_, _ = io.WriteString(w, `{"items":[{"id":"42","name":"Integration specialist","employer":{"name":"Fixture employer"},"area":{"name":"Yekaterinburg"},"alternate_url":"https://hh.example/vacancy/42","published_at":"2026-09-17T08:00:00Z"}],"page":1,"pages":3}`)
	}))
	defer server.Close()

	client, err := NewAPIHHClient(APIClientOptions{
		BaseURL: mustURL(t, server.URL), HTTPClient: server.Client(), TokenStore: &memoryTokenStore{loaded: validTokens()},
		UserAgent: "hh-ai-responder/test", SearchParams: url.Values{
			"text": {"Python backend"}, "area": {"3"}, "search_period": {"7"}, "items_on_page": {"50"},
			"career_agent_include": {"Django"}, "resume": {"browser-only"},
		},
		Now: func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ReadVacancies(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("page") != "1" || gotQuery.Get("text") != "Python backend" || gotQuery.Get("area") != "3" || gotQuery.Get("period") != "7" || gotQuery.Get("per_page") != "50" || gotQuery.Get("career_agent_include") != "" || gotQuery.Get("resume") != "" {
		t.Fatalf("query=%v", gotQuery)
	}
	if page.NextCursor != "2" || len(page.Items) != 1 || page.Items[0].ID != 42 || page.Items[0].Company != "Fixture employer" || page.Items[0].URL != "https://hh.example/vacancy/42" {
		t.Fatalf("page=%+v", page)
	}
}

func TestAPIHHClientReadsVacancySearchRelationsArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vacancies" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"items":[{"id":"42","relations":["got_response"]}]}`)
	}))
	defer server.Close()

	page, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).ReadVacancies(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 42 || page.Items[0].AlreadyResponded == nil || !*page.Items[0].AlreadyResponded {
		t.Fatalf("page=%+v", page)
	}
}

func TestAPIHHClientReadsVacancyDetailWithExplicitRelation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vacancies/42" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":42,"name":"Integration specialist","description":"REST API automation","employer":{"name":"Fixture employer"},"area":{"name":"Yekaterinburg"},"address":{"raw":"Lenina street"},"salary_range":{"from":90000,"to":120000,"currency":"RUR"},"key_skills":[{"name":"Python"},{"name":"REST API"}],"professional_roles":[{"name":"Developer"}],"experience":{"id":"between1And3","name":"1-3 years"},"employment":{"name":"Full time"},"schedule":{"name":"Flexible"},"work_format":[{"id":"REMOTE","name":"Из дома"}],"workplace":{"code":"ON_SITE","name":"На месте работодателя"},"published_at":"2026-09-17T08:00:00Z","archived":null,"response_letter_required":true,"has_test":false,"relation":{"already_responded":false}}`)
	}))
	defer server.Close()

	got, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).ReadVacancyDetail(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Title != "Integration specialist" || got.Description != "REST API automation" || got.Company != "Fixture employer" || got.Address != "Lenina street" || got.Salary != "90000–120000" || got.Currency != "RUR" || len(got.KeySkills) != 2 || got.Experience != "between1And3" || got.EmploymentType != "Full time" || got.Schedule != "Flexible" || got.WorkFormat != "hybrid" || !got.ResponseLetterRequired || !got.ResponseLetterRequiredKnown || got.ArchivedKnown || got.UserTestPresentKnown != true || got.UserTestPresent != false {
		t.Fatalf("vacancy=%+v", got)
	}
	if _, ok := got.Metadata["already_responded"]; ok {
		t.Fatalf("unknown relation was normalized as already_responded: %+v", got.Metadata)
	}
	if got.AlreadyResponded == nil || *got.AlreadyResponded || got.AlreadyRespondedEvidence == "" {
		t.Fatalf("explicit relation was not preserved: value=%v evidence=%q", got.AlreadyResponded, got.AlreadyRespondedEvidence)
	}
}

func TestAPIHHClientVacancyDetailMissingRelationPreservesUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vacancies/42" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":42,"name":"Integration specialist","salary_range":{"from":90000,"currency":"RUR"}}`)
	}))
	defer server.Close()

	got, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).ReadVacancyDetail(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Title != "Integration specialist" || got.AlreadyResponded != nil || got.AlreadyRespondedEvidence != "" {
		t.Fatalf("missing relation was not preserved as unknown: %+v", got)
	}
}

func TestAPIHHClientVacancyDetailMissingRelationRequiredCapabilityReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vacancies/42" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":42,"name":"Integration specialist"}`)
	}))
	defer server.Close()

	got, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).ReadVacancyDetailRequiringRelation(context.Background(), 42)
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != "duplicate-state" {
		t.Fatalf("error=%T %v, want duplicate-state capability error", err, err)
	}
	if got.ID != 0 || got.Title != "" {
		t.Fatalf("required relation returned successful detail: %+v", got)
	}
}

func TestAPIHHClientVacancyDetailConflictingRelationRequiredCapabilityReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":42,"name":"Integration specialist","relation":{"already_responded":false,"responded":true}}`)
	}))
	defer server.Close()

	got, err := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()}).ReadVacancyDetailRequiringRelation(context.Background(), 42)
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != "duplicate-state" || got.ID != 0 {
		t.Fatalf("detail=%+v error=%T %v, want empty detail and duplicate-state capability error", got, err, err)
	}
}

func TestAPIHHClientUnsupportedApplicationAndConversationReadsReturnCapabilityErrors(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		t.Fatalf("unsupported capability reached endpoint %q", r.URL.Path)
	}))
	defer server.Close()
	client := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()})

	for name, read := range map[string]func() error{
		"applications": func() error {
			page, err := client.ReadApplications(context.Background(), "")
			if len(page.Items) != 0 || page.NextCursor != "" {
				t.Fatalf("applications page=%+v", page)
			}
			return err
		},
		"conversations": func() error {
			page, err := client.ReadConversations(context.Background(), "")
			if len(page.Items) != 0 || page.NextCursor != "" {
				t.Fatalf("conversations page=%+v", page)
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := read()
			var capabilityErr *CapabilityError
			if !errors.As(err, &capabilityErr) || capabilityErr.Capability != name {
				t.Fatalf("error=%T %v, want typed capability error for %s", err, err, name)
			}
		})
	}
	if serverCalls != 0 {
		t.Fatalf("unsupported reads made %d HTTP calls", serverCalls)
	}
}

var _ hhreadport.ResumeReadSource = (*APIHHClient)(nil)
