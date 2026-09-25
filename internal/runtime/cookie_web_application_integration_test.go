package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

type cookieWebIntegrationAttemptStore struct {
	attempts map[string]domain.Attempt
}

var _ attemptport.Store = (*cookieWebIntegrationAttemptStore)(nil)

func (s *cookieWebIntegrationAttemptStore) Reserve(_ context.Context, attempt domain.Attempt) (attemptport.ReserveResult, error) {
	if s.attempts == nil {
		s.attempts = map[string]domain.Attempt{}
	}
	for _, existing := range s.attempts {
		if existing.VacancyID == attempt.VacancyID && domain.IsBlocking(existing.State) {
			return attemptport.ReserveResult{Existing: &existing}, domain.ErrTargetBlocked
		}
	}
	s.attempts[attempt.AttemptID] = attempt
	return attemptport.ReserveResult{Reserved: true, Attempt: attempt}, nil
}

func (s *cookieWebIntegrationAttemptStore) RecordOutcome(_ context.Context, id string, state domain.State, updatedAt time.Time, providerStatus int, errorClass string) error {
	attempt, ok := s.attempts[id]
	if !ok {
		return domain.ErrAttemptNotFound
	}
	updated, err := attempt.WithOutcome(state, updatedAt, providerStatus, errorClass)
	if err != nil {
		return err
	}
	s.attempts[id] = updated
	return nil
}

func (s *cookieWebIntegrationAttemptStore) Get(_ context.Context, id string) (domain.Attempt, error) {
	attempt, ok := s.attempts[id]
	if !ok {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	return attempt, nil
}

func (s *cookieWebIntegrationAttemptStore) FindBlocking(_ context.Context, vacancyID int) (domain.Attempt, error) {
	for _, attempt := range s.attempts {
		if attempt.VacancyID == vacancyID && domain.IsBlocking(attempt.State) {
			return attempt, nil
		}
	}
	return domain.Attempt{}, domain.ErrAttemptNotFound
}

func (s *cookieWebIntegrationAttemptStore) RecordReconciliation(_ context.Context, id string, evidence domain.ReconciliationEvidence, now time.Time) error {
	attempt, ok := s.attempts[id]
	if !ok {
		return domain.ErrAttemptNotFound
	}
	updated, err := attempt.WithReconciliation(evidence, now)
	if err != nil {
		return err
	}
	s.attempts[id] = updated
	return nil
}

func TestCookieOnlyControlledApplyLiveCompositionUsesWebSessionAndReconciles(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	var responded atomic.Bool
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount.Add(1)
		}
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /vacancy/42":
			w.Header().Add("Set-Cookie", "_xsrf=xsrf-42; Path=/")
			w.Header().Add("Set-Cookie", "hhtoken=authenticated; Path=/")
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false}}`)
		case http.MethodGet + " /applicant/my_resumes":
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "hhtoken=authenticated") {
				t.Fatalf("authenticated cookie was not persisted: %q", cookie)
			}
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"272272326","hash":"browser-hash-42"},"title":[{"string":"Backend developer"}]}]}</script>`)
		case http.MethodGet + " /applicant/vacancy_response":
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "hhtoken=authenticated") {
				t.Fatalf("authenticated cookie was not persisted: %q", cookie)
			}
			alreadyResponded := responded.Load()
			_, _ = fmt.Fprintf(w, `{"redirectConfig":{"archived":false,"alreadyResponded":%t,"testPresent":false,"responseLetterRequired":false,"canApply":%t,"vacancyType":"open"}}`, alreadyResponded, !alreadyResponded)
		case http.MethodGet + " /applicant/negotiations":
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "hhtoken=authenticated") {
				t.Fatalf("authenticated cookie was not persisted: %q", cookie)
			}
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantNegotiations":{"topicList":[],"paging":{"next":{"disabled":true}}},"vacanciesShort":{"vacanciesList":[]}}</script>`)
		case http.MethodPost + " /applicant/vacancy_response/popup":
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "hhtoken=authenticated") {
				t.Fatalf("authenticated cookie was not sent to writer: %q", cookie)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse application form: %v", err)
			}
			if got, want := r.Form.Get("_xsrf"), "xsrf-42"; got != want {
				t.Fatalf("xsrf=%q, want %q", got, want)
			}
			if got, want := r.Form.Get("vacancy_id"), "42"; got != want {
				t.Fatalf("vacancy_id=%q, want %q", got, want)
			}
			if got, want := r.Form.Get("resume_hash"), "browser-hash-42"; got != want {
				t.Fatalf("resume_hash=%q, want %q", got, want)
			}
			if got, want := r.Form.Get("letter"), "Approved browser letter"; got != want {
				t.Fatalf("letter=%q, want %q", got, want)
			}
			if got, want := r.Form.Get("ignore_postponed"), "true"; got != want {
				t.Fatalf("ignore_postponed=%q, want %q", got, want)
			}
			responded.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"success":true,"id":"provider-response-42"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	cookieFile := "# Netscape HTTP Cookie File\n"
	if err := os.WriteFile(cookiePath, []byte(cookieFile), 0o600); err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(dir, "approval.json")
	approval := APIApplicationApproval{
		Version: 1, VacancyID: 42, ProviderResumeID: "272272326", BrowserResumeHash: "browser-hash-42",
		CoverLetter: "Approved browser letter", ContentHash: contentHash("Approved browser letter"), Nonce: "browser-nonce-42",
		Status: "READY_FOR_EXPLICIT_SEND", FinalDecision: "MATCH", PreviewFreshAt: now,
	}
	if err := os.WriteFile(approvalPath, mustIntegrationJSON(t, approval), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &cookieWebIntegrationAttemptStore{}
	service, err := newControlledApplicationService(context.Background(), Config{
		HHTransport: "browser", SearchURL: server.URL, CookiesPath: cookiePath, DryRun: false, HHWriteEnabled: true,
	}, HHAPICommandDeps{ApplicationAttempts: store, CookieWebTestBaseURL: mustIntegrationURL(t, server.URL), Now: func() time.Time { return now }}, 1)
	if err != nil {
		t.Fatalf("cookies-only service construction failed: %v", err)
	}
	if service.client != nil {
		t.Fatal("browser composition unexpectedly constructed an OAuth/API client")
	}

	result, err := service.Execute(context.Background(), approvalPath, now)
	if err != nil {
		t.Fatalf("cookies-only live composition failed: %v", err)
	}
	if postCount.Load() != 1 || !responded.Load() {
		t.Fatalf("post_count=%d responded=%t", postCount.Load(), responded.Load())
	}
	if result.FinalOutcome != APIApplicationFinalPostSuccessReconciled {
		t.Fatalf("final outcome=%s, want %s; reconciliation=%+v", result.FinalOutcome, APIApplicationFinalPostSuccessReconciled, result.Reconciliation)
	}
	updated, err := loadAPIApplicationApproval(approvalPath)
	if err != nil || updated.NonceUsedAt == nil {
		t.Fatalf("nonce was not durably consumed: approval=%+v err=%v", updated, err)
	}
	if len(store.attempts) != 1 {
		t.Fatalf("attempt reservations=%d, want 1", len(store.attempts))
	}
}

func TestCookieOnlyControlledApplyDryRunDoesNotConsumeNonceReserveAttemptOrPost(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount.Add(1)
			t.Fatalf("cookie-only dry-run issued POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/vacancy/42":
			w.Header().Add("Set-Cookie", "_xsrf=xsrf-dry-run; Path=/")
			w.Header().Add("Set-Cookie", "hhtoken=authenticated; Path=/")
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false}}`)
		case "/applicant/my_resumes":
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"272272326","hash":"browser-hash-42"}}]}</script>`)
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookiePath, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(dir, "approval.json")
	approval := APIApplicationApproval{
		Version: 1, VacancyID: 42, ProviderResumeID: "272272326", BrowserResumeHash: "browser-hash-42",
		CoverLetter: "Approved browser letter", ContentHash: contentHash("Approved browser letter"), Nonce: "browser-dry-run-nonce",
		Status: "READY_FOR_EXPLICIT_SEND", FinalDecision: "MATCH", PreviewFreshAt: now,
	}
	if err := os.WriteFile(approvalPath, mustIntegrationJSON(t, approval), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &cookieWebIntegrationAttemptStore{}
	service, err := newControlledApplicationService(context.Background(), Config{
		HHTransport: "browser", SearchURL: server.URL, CookiesPath: cookiePath, DryRun: true, HHWriteEnabled: false,
		HHOAuthTokenURL: "::invalid", HHAPITokenFile: filepath.Join(dir, "absent-oauth-token.json"),
	}, HHAPICommandDeps{ApplicationAttempts: store, CookieWebTestBaseURL: mustIntegrationURL(t, server.URL), Now: func() time.Time { return now }}, 1)
	if err != nil {
		t.Fatalf("cookies-only dry-run construction failed without OAuth: %v", err)
	}
	if _, err := service.Execute(context.Background(), approvalPath, now); err != nil {
		t.Fatalf("cookies-only dry-run failed: %v", err)
	}
	unchanged, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.NonceUsedAt != nil || len(store.attempts) != 0 || postCount.Load() != 0 {
		t.Fatalf("dry-run mutated state: nonce=%v attempts=%d posts=%d", unchanged.NonceUsedAt, len(store.attempts), postCount.Load())
	}
}

func TestCookieOnlyControlledApplyBlocksAfterUnsafeSetCookieBeforeNonceAttemptOrPost(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 30, 0, 0, time.UTC)
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount.Add(1)
			t.Fatalf("unsafe cookie preflight issued POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/vacancy/42":
			w.Header().Add("Set-Cookie", "evil=secret-parent; Domain=hh.ru; Path=/")
			w.Header().Add("Set-Cookie", "_xsrf=xsrf-unsafe; Path=/")
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false}}`)
		case "/applicant/my_resumes":
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"272272326","hash":"browser-hash-42"}}]}</script>`)
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookiePath, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(dir, "approval.json")
	approval := APIApplicationApproval{
		Version: 1, VacancyID: 42, ProviderResumeID: "272272326", BrowserResumeHash: "browser-hash-42",
		CoverLetter: "Approved browser letter", ContentHash: contentHash("Approved browser letter"), Nonce: "unsafe-cookie-nonce",
		Status: "READY_FOR_EXPLICIT_SEND", FinalDecision: "MATCH", PreviewFreshAt: now,
	}
	if err := os.WriteFile(approvalPath, mustIntegrationJSON(t, approval), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &cookieWebIntegrationAttemptStore{}
	service, err := newControlledApplicationService(context.Background(), Config{
		HHTransport: "browser", SearchURL: server.URL, CookiesPath: cookiePath, DryRun: false, HHWriteEnabled: true,
	}, HHAPICommandDeps{ApplicationAttempts: store, CookieWebTestBaseURL: mustIntegrationURL(t, server.URL), Now: func() time.Time { return now }}, 1)
	if err != nil {
		t.Fatalf("cookies-only service construction failed: %v", err)
	}
	if _, err := service.Execute(context.Background(), approvalPath, now); err == nil {
		t.Fatal("unsafe Set-Cookie unexpectedly allowed application send")
	}
	updated, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NonceUsedAt != nil || len(store.attempts) != 0 || postCount.Load() != 0 {
		t.Fatalf("unsafe cookie mutated application state: nonce=%v attempts=%d posts=%d", updated.NonceUsedAt, len(store.attempts), postCount.Load())
	}
}

func TestCookieOnlyControlledApplyBlocksMismatchedProviderBeforeNonceAttemptOrPost(t *testing.T) {
	now := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount.Add(1)
		}
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /vacancy/42":
			w.Header().Set("Set-Cookie", "_xsrf=xsrf-mismatch; Path=/")
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false}}`)
		case http.MethodGet + " /applicant/my_resumes":
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantResumes":[{"_attributes":{"id":"272272326","hash":"browser-hash-42"}}]}</script>`)
		case http.MethodGet + " /applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookiePath, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	approvalPath := filepath.Join(dir, "approval.json")
	approval := APIApplicationApproval{
		Version: 1, VacancyID: 42, ProviderResumeID: "different-provider", BrowserResumeHash: "browser-hash-42",
		CoverLetter: "Approved browser letter", ContentHash: contentHash("Approved browser letter"), Nonce: "mismatch-nonce",
		Status: "READY_FOR_EXPLICIT_SEND", FinalDecision: "MATCH", PreviewFreshAt: now,
	}
	if err := os.WriteFile(approvalPath, mustIntegrationJSON(t, approval), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &cookieWebIntegrationAttemptStore{}
	service, err := newControlledApplicationService(context.Background(), Config{
		HHTransport: "browser", SearchURL: server.URL, CookiesPath: cookiePath, DryRun: false, HHWriteEnabled: true,
	}, HHAPICommandDeps{ApplicationAttempts: store, CookieWebTestBaseURL: mustIntegrationURL(t, server.URL), Now: func() time.Time { return now }}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(context.Background(), approvalPath, now); err == nil {
		t.Fatal("mismatched provider identity unexpectedly passed preflight")
	}
	updated, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NonceUsedAt != nil || len(store.attempts) != 0 || postCount.Load() != 0 {
		t.Fatalf("provider mismatch mutated state: nonce=%v attempts=%d posts=%d", updated.NonceUsedAt, len(store.attempts), postCount.Load())
	}
}

func mustIntegrationJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustIntegrationURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
