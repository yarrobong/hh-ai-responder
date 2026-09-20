package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
)

const (
	hhAPIAccessTokenSentinel  = "access-token-task6-sentinel"
	hhAPIRefreshTokenSentinel = "refresh-token-task6-sentinel"
	hhAPIAuthCodeSentinel     = "authorization-code-task6-sentinel"
	hhAPIClientSecretSentinel = "client-secret-task6-sentinel"
	hhAPICookieSentinel       = "cookie-task6-sentinel"
)

func TestHHAPIAuthLocalhostUsesPKCECallbackAndRedactsOutput(t *testing.T) {
	tokenRequests := 0
	var exchanged url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests++
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			exchanged = r.Form
			writeHHAPIJSON(t, w, map[string]any{
				"access_token":  hhAPIAccessTokenSentinel,
				"refresh_token": hhAPIRefreshTokenSentinel,
				"token_type":    "bearer",
				"expires_in":    3600,
			})
		case "/me":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+hhAPIAccessTokenSentinel {
				t.Fatalf("unexpected /me request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
			}
			writeHHAPIJSON(t, w, map[string]any{"id": "private-user-id", "auth_type": "applicant"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "http://127.0.0.1:9876/callback")
	var openedURL string
	var out, errOut bytes.Buffer
	deps := HHAPICommandDeps{
		OpenBrowser: func(_ context.Context, value string) error {
			openedURL = value
			return nil
		},
		ReceiveLocalhostCallback: func(_ context.Context, redirectURI string) (string, error) {
			parsed, err := url.Parse(openedURL)
			if err != nil {
				return "", err
			}
			return redirectURI + "?code=" + url.QueryEscape(hhAPIAuthCodeSentinel) + "&state=" + url.QueryEscape(parsed.Query().Get("state")), nil
		},
	}

	if err := runHHAPICommandWithDeps(context.Background(), []string{"auth"}, cfg, strings.NewReader(""), &out, &errOut, deps); err != nil {
		t.Fatal(err)
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
	if exchanged.Get("code") != hhAPIAuthCodeSentinel || exchanged.Get("code_verifier") == "" {
		t.Fatalf("exchange omitted code or PKCE verifier")
	}
	authorizeURL, err := url.Parse(openedURL)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := hhapi.CodeChallengeS256(exchanged.Get("code_verifier"))
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("code_challenge") != challenge || authorizeURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL did not bind PKCE challenge")
	}
	if _, err := os.Stat(tokenFile); err != nil {
		t.Fatalf("token file was not saved: %v", err)
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIAuthManualReadsOnlyFullRedirectURLFromStdin(t *testing.T) {
	var exchanged url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			exchanged = r.Form
			writeHHAPIJSON(t, w, map[string]any{"access_token": hhAPIAccessTokenSentinel, "token_type": "bearer", "expires_in": 3600})
			return
		}
		if r.URL.Path == "/me" {
			writeHHAPIJSON(t, w, map[string]any{"id": "private-user-id", "auth_type": "applicant"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	stdin := &redirectFromOutputReader{output: &out, code: hhAPIAuthCodeSentinel}
	if err := runHHAPICommandWithDeps(context.Background(), []string{"auth"}, cfg, stdin, &out, &errOut, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	if exchanged.Get("code") != hhAPIAuthCodeSentinel {
		t.Fatalf("manual callback code was not used")
	}
	if exchanged.Get("code") == "" || strings.Contains(errOut.String(), hhAPIClientSecretSentinel) {
		t.Fatalf("manual auth leaked sensitive material")
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIAuthRejectsStateMismatchBeforeTokenExchange(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenRequests++
		}
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", filepath.Join(t.TempDir(), "api-token.json"), "http://127.0.0.1:9876/callback")
	var openedURL string
	var out, errOut bytes.Buffer
	err := runHHAPICommandWithDeps(context.Background(), []string{"auth"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{
		OpenBrowser: func(_ context.Context, value string) error {
			openedURL = value
			return nil
		},
		ReceiveLocalhostCallback: func(_ context.Context, redirectURI string) (string, error) {
			return redirectURI + "?code=" + url.QueryEscape(hhAPIAuthCodeSentinel) + "&state=mismatched-state-sentinel", nil
		},
	})
	if err == nil || tokenRequests != 0 {
		t.Fatalf("state mismatch result = %v, token requests = %d", err, tokenRequests)
	}
	if openedURL == "" {
		t.Fatal("authorization URL was not offered to the browser seam")
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIAuthRejectsAuthorizationCodeArgAndDoesNotReadEnvironment(t *testing.T) {
	t.Setenv("HH_AUTH_CODE", hhAPIAuthCodeSentinel)
	cfg := testHHAPIConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1/token", filepath.Join(t.TempDir(), "api-token.json"), "https://operator.example/callback")
	var out, errOut bytes.Buffer
	err := runHHAPICommandWithDeps(context.Background(), []string{"auth", "--code=" + hhAPIAuthCodeSentinel}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{})
	if err == nil {
		t.Fatal("authorization code argument was accepted")
	}
	if strings.Contains(err.Error(), hhAPIAuthCodeSentinel) || strings.Contains(out.String()+errOut.String(), hhAPIAuthCodeSentinel) {
		t.Fatal("authorization code sentinel was exposed")
	}
}

func TestHHAPIDoctorReadsOnlySafeAPIEndpoints(t *testing.T) {
	var mu sync.Mutex
	var methods, paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/me":
			writeHHAPIJSON(t, w, map[string]any{"id": "private-user-id", "auth_type": "applicant"})
		case "/resumes/mine":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-1", "title": "Backend"}}})
		case "/vacancies":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	if err := hhapi.NewFileTokenStore(tokenFile).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, RefreshToken: hhAPIRefreshTokenSentinel, TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	cfg.HHOAuthClientSecret = ""
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"doctor"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotMethods, gotPaths := append([]string(nil), methods...), append([]string(nil), paths...)
	mu.Unlock()
	if len(gotMethods) != 3 || len(gotPaths) != 3 {
		t.Fatalf("doctor requests = methods %v paths %v, want /me, /resumes/mine, /vacancies", gotMethods, gotPaths)
	}
	for _, method := range gotMethods {
		if method != http.MethodGet {
			t.Fatalf("doctor issued non-GET request: %v", gotMethods)
		}
	}
	if !strings.Contains(out.String(), "Overall: AUTH_OK") || !strings.Contains(out.String(), "Token expired: no") {
		t.Fatalf("unexpected doctor output: %q", out.String())
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIDoctorClassifiesVacancyDecodeErrorAsReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			writeHHAPIJSON(t, w, map[string]any{"id": "private-user-id", "auth_type": "applicant"})
		case "/resumes/mine":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-1", "title": "Backend"}}})
		case "/vacancies":
			_, _ = io.WriteString(w, `{"items":[{"id":"42","relations":[42]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	if err := hhapi.NewFileTokenStore(tokenFile).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	err := runHHAPICommandWithDeps(context.Background(), []string{"doctor"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{})
	if err == nil {
		t.Fatal("doctor succeeded after vacancy decode error")
	}
	output := out.String()
	for _, want := range []string{"/me: AUTH_OK", "Applicant resumes: OK", "Vacancy read: ERROR", "Overall: ERROR"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output=%q, missing %q", output, want)
		}
	}
	if strings.Contains(output, "Overall: AUTH_REQUIRED") {
		t.Fatalf("vacancy decode error was misclassified as auth failure: %q", output)
	}
	assertHHAPISafeOutput(t, output+errOut.String())
}

func TestHHAPIPreflightIsGETOnlyAndSanitized(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/resumes/mine":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-1", "title": "Backend"}}})
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{
				"id": "42", "relations": []string{"favorited"},
				"negotiations_url":     "/negotiations?vacancy_id=42",
				"suitable_resumes_url": "/vacancies/42/suitable_resumes",
				"archived":             false, "has_test": false, "response_letter_required": false,
				"type": map[string]any{"id": "open"}, "apply_alternate_url": "https://hh.example/applicant/vacancy_response?vacancyId=42",
			})
		case "/vacancies/42/suitable_resumes":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-1"}}, "page": 0, "pages": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	if err := hhapi.NewFileTokenStore(tokenFile).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"preflight", "42", "--resume-id", "resume-1"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, want := range []string{"Vacancy ID: 42", "requested resume ID: present", "selected provider resume title: Backend", "duplicate: NO", "suitable: YES", "archived: NO", "vacancy type: open", "response_url present: absent", "apply_alternate_url present: present", "has_test: NO", "response_letter_required: NO", "got_response relation: NO", "negotiations URL present: YES", "suitable resumes URL present: YES", "suitable endpoint scan complete: YES", "suitable resume IDs discovered: 1", "requested resume present: YES", "selected resume suitable: YES", "existing negotiation: NO", "negotiation ID: absent", "negotiation collections discovered: 0", "negotiation collections checked: 0", "negotiation pages checked: 1", "matching negotiation: NO", "negotiation scan complete: YES", "application availability: AVAILABLE", "final duplicate state: NO"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output=%q, missing %q", output, want)
		}
	}
	if strings.Contains(output, server.URL) || strings.Contains(output, "resume-1") {
		t.Fatalf("preflight output exposed provider details: %q", output)
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("methods=%v, want GET only", methods)
		}
	}
	assertHHAPISafeOutput(t, output+errOut.String())
}

func TestHHAPIAllResumePreflightPrintsReadOnlyMatrix(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/resumes/mine":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{
				map[string]any{"id": "resume-a", "title": "Support"},
				map[string]any{"id": "resume-b", "title": "Backend"},
			}})
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{"id": "42", "suitable_resumes_url": "/vacancies/42/suitable_resumes", "archived": true})
		case "/vacancies/42/suitable_resumes":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-a"}}, "page": 0, "pages": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	if err := hhapi.NewFileTokenStore(tokenFile).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"preflight", "42", "--all-resumes"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, want := range []string{"resume matrix: GET-only; no routing or resume switching", "resume title: Support", "resume title: Backend", "present in suitable_resumes: YES", "present in suitable_resumes: NO", "scan complete: YES", "application availability: UNAVAILABLE"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output=%q, missing %q", output, want)
		}
	}
	if strings.Contains(output, "resume-a") || strings.Contains(output, "resume-b") {
		t.Fatalf("all-resume output exposed raw provider IDs: %q", output)
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("methods=%v, want GET only", methods)
		}
	}
	assertHHAPISafeOutput(t, output+errOut.String())
}

func TestHHAPIDoctorReportsMissingTokenWithoutNetworkAccess(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "missing-token.json")
	cfg := testHHAPIConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	err := runHHAPICommandWithDeps(context.Background(), []string{"doctor"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{})
	if err == nil || !strings.Contains(out.String(), "Token file: absent") {
		t.Fatalf("missing-token doctor result = %v, output=%q", err, out.String())
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIDoctorDoesNotRefreshOrSaveAfterAuthFailure(t *testing.T) {
	var methods []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		switch r.URL.Path {
		case "/me":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"token_expired"}`)
		case "/token":
			writeHHAPIJSON(t, w, map[string]any{
				"access_token":  "rotated-access-token-task6-sentinel",
				"refresh_token": "rotated-refresh-token-task6-sentinel",
				"token_type":    "bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "api-token.json")
	if err := hhapi.NewFileTokenStore(tokenFile).Save(context.Background(), hhapi.OAuthTokens{
		AccessToken: hhAPIAccessTokenSentinel, RefreshToken: hhAPIRefreshTokenSentinel, TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"doctor"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{}); err == nil {
		t.Fatal("doctor succeeded after API auth failure")
	}
	after, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("doctor changed the token file after an auth failure")
	}
	mu.Lock()
	gotMethods := append([]string(nil), methods...)
	mu.Unlock()
	for _, method := range gotMethods {
		if method != http.MethodGet {
			t.Fatalf("doctor issued non-GET request after auth failure: %v", gotMethods)
		}
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPILogoutDeletesOnlyConfiguredTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "configured-token.json")
	otherFile := filepath.Join(dir, "other-token.json")
	if err := os.WriteFile(tokenFile, []byte(`{"access_token":"`+hhAPIAccessTokenSentinel+`","token_type":"bearer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherFile, []byte(hhAPIRefreshTokenSentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1/token", tokenFile, "https://operator.example/callback")
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"logout"}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("configured token file still exists: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Fatalf("unconfigured token file was changed: %v", err)
	}
	if !strings.Contains(out.String(), "Deleted: YES") {
		t.Fatalf("unexpected logout output: %q", out.String())
	}
	assertHHAPISafeOutput(t, out.String()+errOut.String())
}

func TestHHAPIApplyDryRunValidatesReadsAndNeverPosts(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{
				"id": "42", "type": map[string]any{"id": "open"}, "archived": false,
				"has_test": false, "response_letter_required": false,
				"apply_alternate_url": "https://hh.example/applicant/vacancy_response?vacancyId=42",
				"negotiations_url":    "/negotiations?vacancy_id=42", "suitable_resumes_url": "/resumes/suitable?vacancy_id=42",
			})
		case "/resumes/suitable":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1, "found": 0})
		default:
			if r.Method == http.MethodPost {
				t.Fatalf("dry-run issued POST %s", r.URL.Path)
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	approvalPath := filepath.Join(t.TempDir(), "approval.json")
	approval := validAPIApplicationApproval(now)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	if err := hhapi.NewFileTokenStore(tokenPath).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenPath, "https://operator.example/callback")
	cfg.HHTransport = "api"
	var out, errOut bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"apply", "42", "--resume-id", "resume-provider-7", "--approval-file", approvalPath}, cfg, strings.NewReader(""), &out, &errOut, HHAPICommandDeps{HTTPClient: server.Client(), Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "WOULD_APPLY") {
		t.Fatalf("output=%q, want WOULD_APPLY", out.String())
	}
	for _, method := range methods {
		if strings.HasPrefix(method, http.MethodPost+" ") {
			t.Fatalf("dry-run methods=%v", methods)
		}
	}
}

func TestHHAPIApplyManualReviewDryRunDoesNotConsumeNonceOrPost(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{"id": "42", "type": map[string]any{"id": "open"}, "archived": false, "has_test": false, "response_letter_required": false, "apply_alternate_url": "https://hh.example/applicant/vacancy_response?vacancyId=42", "negotiations_url": "/negotiations?vacancy_id=42", "suitable_resumes_url": "/resumes/suitable?vacancy_id=42"})
		case "/resumes/suitable":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1, "found": 0})
		default:
			if r.Method == http.MethodPost {
				t.Fatalf("manual dry-run issued POST %s", r.URL.Path)
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	approvalPath := filepath.Join(dir, "approval.json")
	approval := validManualAPIApplicationApproval(now)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dir, "token.json")
	if err := hhapi.NewFileTokenStore(tokenPath).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenPath, "https://operator.example/callback")
	cfg.HHTransport = "api"
	var out bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"apply", "42", "--resume-id", "resume-provider-7", "--approval-file", approvalPath}, cfg, strings.NewReader(""), &out, io.Discard, HHAPICommandDeps{HTTPClient: server.Client(), Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "WOULD_APPLY") {
		t.Fatalf("output=%q, want WOULD_APPLY", out.String())
	}
	unchanged, err := loadAPIApplicationApproval(approvalPath)
	if err != nil || unchanged.NonceUsedAt != nil {
		t.Fatalf("dry-run consumed nonce: approval=%+v err=%v", unchanged, err)
	}
	for _, method := range methods {
		if strings.HasPrefix(method, http.MethodPost+" ") {
			t.Fatalf("manual dry-run methods=%v", methods)
		}
	}
}

func TestHHAPIApplyManualReviewFreshDuplicateStillBlocks(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
			t.Fatalf("fresh duplicate preflight issued POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{"id": "42", "type": map[string]any{"id": "open"}, "archived": false, "has_test": false, "response_letter_required": false, "negotiations_url": "/negotiations?vacancy_id=42", "suitable_resumes_url": "/resumes/suitable?vacancy_id=42"})
		case "/resumes/suitable":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "negotiation-42", "vacancy_id": "42", "resume_id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	approvalPath := filepath.Join(dir, "approval.json")
	raw, err := json.Marshal(validManualAPIApplicationApproval(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dir, "token.json")
	if err := hhapi.NewFileTokenStore(tokenPath).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenPath, "https://operator.example/callback")
	cfg.HHTransport = "api"
	err = runHHAPICommandWithDeps(context.Background(), []string{"apply", "42", "--resume-id", "resume-provider-7", "--approval-file", approvalPath}, cfg, strings.NewReader(""), io.Discard, io.Discard, HHAPICommandDeps{HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err == nil || postCount != 0 {
		t.Fatalf("duplicate apply error=%v postCount=%d, want blocked and zero POST", err, postCount)
	}
}

func TestHHAPIApplyRejectsLiveConfigurationWithoutWriteEnabled(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	approvalPath := filepath.Join(t.TempDir(), "approval.json")
	raw, err := json.Marshal(validAPIApplicationApproval(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1/token", filepath.Join(t.TempDir(), "token.json"), "https://operator.example/callback")
	cfg.HHTransport, cfg.DryRun, cfg.HHWriteEnabled = "api", false, false
	err = runHHAPICommandWithDeps(context.Background(), []string{"apply", "42", "--resume-id", "resume-provider-7", "--approval-file", approvalPath}, cfg, strings.NewReader(""), io.Discard, io.Discard, HHAPICommandDeps{Now: func() time.Time { return now }})
	if err == nil || !strings.Contains(err.Error(), "HH_WRITE_ENABLED") {
		t.Fatalf("error=%v, want HH_WRITE_ENABLED gate", err)
	}
}

func TestNewHandlersWiresHHAPI(t *testing.T) {
	if NewHandlers().HHAPI == nil {
		t.Fatal("HHAPI handler is not wired")
	}
}

func testHHAPIConfig(t *testing.T, baseURL, tokenURL, tokenFile, redirectURI string) Config {
	t.Helper()
	return Config{
		HHAPIBaseURL:        baseURL,
		HHOAuthAuthorizeURL: baseURL + "/authorize",
		HHOAuthTokenURL:     tokenURL,
		HHOAuthClientID:     "operator-client",
		HHOAuthClientSecret: hhAPIClientSecretSentinel,
		HHOAuthRedirectURI:  redirectURI,
		HHOAuthUserAgent:    "hh-api-task6-test",
		HHAPITokenFile:      tokenFile,
		DryRun:              true,
		HHWriteEnabled:      false,
	}
}

func writeHHAPIJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func assertHHAPISafeOutput(t *testing.T, output string) {
	t.Helper()
	for _, sentinel := range []string{hhAPIAccessTokenSentinel, hhAPIRefreshTokenSentinel, hhAPIAuthCodeSentinel, hhAPIClientSecretSentinel, hhAPICookieSentinel} {
		if strings.Contains(output, sentinel) {
			t.Fatalf("sensitive sentinel %q appeared in output %q", sentinel, output)
		}
	}
}

type redirectFromOutputReader struct {
	output *bytes.Buffer
	code   string
	done   bool
}

func (r *redirectFromOutputReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	lines := strings.Split(r.output.String(), "\n")
	var authURL string
	for _, line := range lines {
		if strings.HasPrefix(line, "Authorization URL: ") {
			authURL = strings.TrimPrefix(line, "Authorization URL: ")
			break
		}
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		return 0, err
	}
	redirect := "https://operator.example/callback?code=" + url.QueryEscape(r.code) + "&state=" + url.QueryEscape(parsed.Query().Get("state")) + "\n"
	return copy(p, redirect), nil
}
