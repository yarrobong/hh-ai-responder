package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	domain "hh-ai-responder/internal/applicationattempt"
	hhwrite "hh-ai-responder/internal/ports/hhwrite"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
)

func validAPIApplicationApproval(now time.Time) APIApplicationApproval {
	value := APIApplicationApproval{
		Version:          1,
		VacancyID:        42,
		ProviderResumeID: "resume-provider-7",
		CoverLetter:      "Здравствуйте! Готов обсудить интеграции и поддержку API.",
		Nonce:            "nonce-7",
		Status:           "READY_FOR_EXPLICIT_SEND",
		FinalDecision:    "MATCH",
		PreviewFreshAt:   now,
	}
	value.ContentHash = contentHash(value.CoverLetter)
	return value
}

type controlledApplicationWriter struct {
	calls  int
	result hhwrite.WriteResult
}

func (w *controlledApplicationWriter) SubmitVacancyResponse(context.Context, hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	w.calls++
	if w.result.Class != "" {
		return w.result, errors.New("provider result fixture")
	}
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, Class: hhwrite.ApplicationResultSuccess}, nil
}

func TestControlledApplicationGatewayCapsMutationsAtOnePerInvocation(t *testing.T) {
	writer := &controlledApplicationWriter{}
	service := hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: writer}, hhwritegateway.Options{WriteEnabled: true, MaxWritesPerRun: 1})
	if _, err := service.SubmitVacancyResponse(context.Background(), hhwritegateway.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitVacancyResponse(context.Background(), hhwritegateway.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-2"}); err == nil {
		t.Fatal("second mutation was not blocked by invocation cap")
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls=%d, want 1", writer.calls)
	}
}

func TestAPIApplicationExecutorKeepsAlreadyAppliedVacancyBlocking(t *testing.T) {
	writer := &controlledApplicationWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultAlreadyApplied}}
	gateway := hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: writer}, hhwritegateway.Options{WriteEnabled: true, MaxWritesPerRun: 1})
	execution, err := (apiApplicationExecutor{gateway: gateway}).SubmitApplication(context.Background(), applicationsubmission.ApplicationRequest{VacancyID: 42, ResumeID: "resume-1"})
	if execution.Outcome != applicationsubmission.ExecutionDeliveryUncertain || execution.ApplicationClass != hhwrite.ApplicationResultAlreadyApplied || err == nil {
		t.Fatalf("execution=%+v err=%v, want uncertain already-applied result", execution, err)
	}
}

func TestAPIApplicationExecutorSurfacesManualChallengeClass(t *testing.T) {
	writer := &controlledApplicationWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultManualChallenge}}
	gateway := hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: writer}, hhwritegateway.Options{WriteEnabled: true, MaxWritesPerRun: 1})
	execution, err := (apiApplicationExecutor{gateway: gateway}).SubmitApplication(context.Background(), applicationsubmission.ApplicationRequest{VacancyID: 42, ResumeID: "resume-1"})
	if execution.Outcome != applicationsubmission.ExecutionRejected || execution.ApplicationClass != hhwrite.ApplicationResultManualChallenge || err == nil {
		t.Fatalf("execution=%+v err=%v, want rejected manual-challenge result", execution, err)
	}
}

type apiReconciliationStore struct {
	attempt domain.Attempt
}

func (s *apiReconciliationStore) Get(context.Context, string) (domain.Attempt, error) {
	return s.attempt, nil
}
func (s *apiReconciliationStore) FindBlocking(context.Context, int) (domain.Attempt, error) {
	return s.attempt, nil
}
func (s *apiReconciliationStore) RecordReconciliation(_ context.Context, _ string, evidence domain.ReconciliationEvidence, now time.Time) error {
	updated, err := s.attempt.WithReconciliation(evidence, now)
	if err == nil {
		s.attempt = updated
	}
	return err
}

type apiReconciliationReader struct {
	snapshot applicationreconciliation.EvidenceSnapshot
	reads    int
}

func (r *apiReconciliationReader) ReadVacancyResponseEvidence(context.Context, applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error) {
	r.reads++
	return r.snapshot, nil
}

func TestReconcileControlledAPIApplicationMapsAllRequiredFinalOutcomes(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	baseAttempt, err := domain.New(42, "resume-provider-7", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		class     hhwrite.ApplicationResultClass
		confirmed bool
		want      APIApplicationFinalOutcome
	}{
		{name: "success reconciled", class: hhwrite.ApplicationResultSuccess, confirmed: true, want: APIApplicationFinalPostSuccessReconciled},
		{name: "success unconfirmed", class: hhwrite.ApplicationResultSuccess, confirmed: false, want: APIApplicationFinalPostSuccessUnconfirmed},
		{name: "already applied", class: hhwrite.ApplicationResultAlreadyApplied, confirmed: true, want: APIApplicationFinalAlreadyAppliedReconciled},
		{name: "unknown reconciled", class: hhwrite.ApplicationResultUnknownSendResult, confirmed: true, want: APIApplicationFinalUnknownSendReconciledSuccess},
		{name: "unknown unresolved", class: hhwrite.ApplicationResultUnknownSendResult, confirmed: false, want: APIApplicationFinalUnknownSendUnresolved},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempt := baseAttempt
			store := &apiReconciliationStore{attempt: attempt}
			reader := &apiReconciliationReader{snapshot: applicationreconciliation.EvidenceSnapshot{
				VacancyID: 42, PreflightAvailable: true, ApplicationsAvailable: true,
				Preflight: applicationreconciliation.PreflightEvidence{AlreadyRespondedKnown: test.confirmed, AlreadyResponded: test.confirmed},
			}}
			got, _, err := reconcileControlledAPIApplication(context.Background(), store, reader, attempt.AttemptID, test.class, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || reader.reads != 1 {
				t.Fatalf("outcome=%s reads=%d, want %s and one targeted read", got, reader.reads, test.want)
			}
		})
	}
}

func TestAPIApplicationEvidenceReaderUsesTargetedGETsOnly(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/vacancies/42":
			_, _ = io.WriteString(w, `{"id":"42","type":{"id":"open"},"archived":false,"has_test":false,"response_letter_required":false,"negotiations_url":"/negotiations?vacancy_id=42","suitable_resumes_url":"/resumes/suitable?vacancy_id=42"}`)
		case "/resumes/suitable":
			_, _ = io.WriteString(w, `{"items":[{"id":"resume-provider-7"}],"page":0,"pages":1,"found":1}`)
		case "/negotiations":
			_, _ = io.WriteString(w, `{"items":[{"id":"negotiation-42","vacancy_id":"42","resume_id":"resume-provider-7"}],"page":0,"pages":1,"found":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hhapi.NewAPIHHClient(hhapi.APIClientOptions{BaseURL: base, HTTPClient: server.Client(), TokenStore: &shadowTokenStore{tokens: hhapi.OAuthTokens{AccessToken: "token", TokenType: "bearer", ExpiresAt: time.Now().Add(time.Hour)}}, UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (&apiApplicationEvidenceReader{client: client}).ReadVacancyResponseEvidence(context.Background(), applicationreconciliation.Target{VacancyID: 42, ResumeID: "resume-provider-7"})
	if err != nil || !snapshot.PreflightAvailable || !snapshot.ApplicationsAvailable || len(snapshot.Applications) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	for _, method := range methods {
		if strings.Contains(method, " /applications") {
			t.Fatalf("unsupported generic applications read was used: %v", methods)
		}
	}
}

func TestValidateAPIApplicationApprovalRequiresExactFreshIdentity(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	base := validAPIApplicationApproval(now)
	tests := []struct {
		name  string
		value APIApplicationApproval
		err   error
	}{
		{name: "valid at freshness boundary", value: func() APIApplicationApproval {
			value := base
			value.PreviewFreshAt = now.Add(-apiApplicationApprovalMaxAge)
			return value
		}()},
		{name: "stale", value: func() APIApplicationApproval {
			value := base
			value.PreviewFreshAt = now.Add(-apiApplicationApprovalMaxAge - time.Nanosecond)
			return value
		}(), err: errAPIApplicationApprovalStale},
		{name: "wrong vacancy", value: func() APIApplicationApproval { value := base; value.VacancyID = 43; return value }(), err: errAPIApplicationApprovalIdentity},
		{name: "wrong resume", value: func() APIApplicationApproval { value := base; value.ProviderResumeID = "other"; return value }(), err: errAPIApplicationApprovalIdentity},
		{name: "wrong status", value: func() APIApplicationApproval { value := base; value.Status = "BLOCKED"; return value }(), err: errAPIApplicationApprovalState},
		{name: "wrong decision", value: func() APIApplicationApproval { value := base; value.FinalDecision = "REVIEW_REQUIRED"; return value }(), err: errAPIApplicationApprovalState},
		{name: "content mismatch", value: func() APIApplicationApproval { value := base; value.CoverLetter += " changed"; return value }(), err: errAPIApplicationApprovalContent},
		{name: "nonce missing", value: func() APIApplicationApproval { value := base; value.Nonce = ""; return value }(), err: errAPIApplicationApprovalNonce},
		{name: "nonce used", value: func() APIApplicationApproval { value := base; used := now; value.NonceUsedAt = &used; return value }(), err: errAPIApplicationApprovalNonce},
		{name: "invalid letter", value: func() APIApplicationApproval {
			value := base
			value.CoverLetter = "```json\n{}\n```"
			value.ContentHash = contentHash(value.CoverLetter)
			return value
		}(), err: errAPIApplicationApprovalContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAPIApplicationApproval(test.value, 42, "resume-provider-7", now)
			if test.err == nil {
				if err != nil {
					t.Fatalf("validation error=%v", err)
				}
				return
			}
			if !errors.Is(err, test.err) {
				t.Fatalf("error=%v, want %v", err, test.err)
			}
		})
	}
}
