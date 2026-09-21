package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func validManualAPIApplicationApproval(now time.Time) APIApplicationApproval {
	value := validAPIApplicationApproval(now)
	score := 82
	value.Status = pilotManualReviewStatus
	value.FinalDecision = "REVIEW_REQUIRED"
	value.ApprovalBasis = manualApprovalBasis
	value.OperatorApproved = true
	value.OperatorApprovalTimestamp = now
	value.OriginalAIScore = &score
	value.OriginalAIRecommendation = "UNCERTAIN"
	value.OriginalAIRecommendationReasons = []string{"operator review required"}
	value.OriginalFinalDecision = "REVIEW_REQUIRED"
	value.PilotArtifactHash = strings.Repeat("a", 64)
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

func TestValidateAPIApplicationApprovalAcceptsManualReviewBasisAndRejectsMalformedProvenance(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	manual := validManualAPIApplicationApproval(now)
	if err := validateAPIApplicationApproval(manual, 42, "resume-provider-7", now); err != nil {
		t.Fatalf("valid manual approval error=%v", err)
	}
	for _, recommendation := range []string{"APPLY", "UNCERTAIN", "DO_NOT_APPLY"} {
		t.Run("manual approval preserves AI recommendation "+recommendation, func(t *testing.T) {
			value := manual
			value.OriginalAIRecommendation = recommendation
			if err := validateAPIApplicationApproval(value, 42, "resume-provider-7", now); err != nil {
				t.Fatalf("manual approval recommendation %q rejected: %v", recommendation, err)
			}
		})
	}
	if err := validateAPIApplicationApproval(validAPIApplicationApproval(now), 42, "resume-provider-7", now); err != nil {
		t.Fatalf("automatic approval compatibility error=%v", err)
	}
	tests := []struct {
		name   string
		mutate func(*APIApplicationApproval)
	}{
		{name: "wrong status", mutate: func(value *APIApplicationApproval) { value.Status = "READY_FOR_EXPLICIT_SEND" }},
		{name: "match final decision", mutate: func(value *APIApplicationApproval) { value.FinalDecision = "MATCH" }},
		{name: "wrong basis", mutate: func(value *APIApplicationApproval) { value.ApprovalBasis = "OTHER" }},
		{name: "operator not approved", mutate: func(value *APIApplicationApproval) { value.OperatorApproved = false }},
		{name: "missing approval timestamp", mutate: func(value *APIApplicationApproval) { value.OperatorApprovalTimestamp = time.Time{} }},
		{name: "missing original score", mutate: func(value *APIApplicationApproval) { value.OriginalAIScore = nil }},
		{name: "invalid original recommendation", mutate: func(value *APIApplicationApproval) { value.OriginalAIRecommendation = "INVALID" }},
		{name: "missing pilot hash", mutate: func(value *APIApplicationApproval) { value.PilotArtifactHash = "" }},
		{name: "missing nonce", mutate: func(value *APIApplicationApproval) { value.Nonce = "" }},
		{name: "used nonce", mutate: func(value *APIApplicationApproval) { used := now; value.NonceUsedAt = &used }},
		{name: "changed letter", mutate: func(value *APIApplicationApproval) { value.CoverLetter += " changed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := manual
			test.mutate(&value)
			if err := validateAPIApplicationApproval(value, 42, "resume-provider-7", now); err == nil {
				t.Fatal("malformed manual approval unexpectedly passed")
			}
		})
	}
}

func TestValidateControlledAPIApplicationPreflightRequiresAllFreshProviderSafetyFacts(t *testing.T) {
	base := VacancyPreflight{
		Available: true, ArchivedKnown: true, Archived: false,
		CanApplyKnown: true, CanApply: true,
		TestPresentKnown: true, TestPresent: false,
		SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true,
		SuitableResumesScanComplete: true, NegotiationScanComplete: true,
		VacancyTypeKnown: true, VacancyTypeID: "open",
		ActiveState:              VacancyActiveStateActive,
		AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded},
	}
	tests := []struct {
		name   string
		mutate func(*VacancyPreflight)
	}{
		{name: "duplicate", mutate: func(value *VacancyPreflight) { value.AlreadyRespondedEvidence.Value = AlreadyRespondedYes }},
		{name: "duplicate unknown", mutate: func(value *VacancyPreflight) { value.AlreadyRespondedEvidence.Value = AlreadyRespondedUnknown }},
		{name: "unsuitable", mutate: func(value *VacancyPreflight) { value.SelectedResumeSuitable = false }},
		{name: "suitability unknown", mutate: func(value *VacancyPreflight) { value.SelectedResumeSuitableKnown = false }},
		{name: "availability unavailable", mutate: func(value *VacancyPreflight) { value.Available = false }},
		{name: "inactive", mutate: func(value *VacancyPreflight) { value.ActiveState = VacancyActiveStateInactive }},
		{name: "active unknown", mutate: func(value *VacancyPreflight) { value.ActiveState = VacancyActiveStateUnknown }},
		{name: "can apply false", mutate: func(value *VacancyPreflight) { value.CanApply = false }},
		{name: "can apply unknown", mutate: func(value *VacancyPreflight) { value.CanApplyKnown = false }},
		{name: "test required", mutate: func(value *VacancyPreflight) { value.TestPresent = true }},
		{name: "test unknown", mutate: func(value *VacancyPreflight) { value.TestPresentKnown = false }},
		{name: "suitable scan incomplete", mutate: func(value *VacancyPreflight) { value.SuitableResumesScanComplete = false }},
		{name: "negotiation scan incomplete", mutate: func(value *VacancyPreflight) { value.NegotiationScanComplete = false }},
		{name: "unknown vacancy type", mutate: func(value *VacancyPreflight) { value.VacancyTypeKnown = false }},
		{name: "direct response path", mutate: func(value *VacancyPreflight) { value.ResponseIdentifierPresent = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if err := validateControlledAPIApplicationPreflight(value); err == nil {
				t.Fatal("unsafe fresh preflight unexpectedly passed")
			}
		})
	}
	if err := validateControlledAPIApplicationPreflight(base); err != nil {
		t.Fatalf("safe fresh preflight was blocked: %v", err)
	}
}

func TestConsumeAPIApplicationApprovalNonceIsDurableAndOneTime(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "approval.json")
	approval := validManualAPIApplicationApproval(now)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := consumeAPIApplicationApprovalNonce(path, approval, 42, "resume-provider-7", now); err != nil {
		t.Fatal(err)
	}
	consumed, err := loadAPIApplicationApproval(path)
	if err != nil || consumed.NonceUsedAt == nil {
		t.Fatalf("consumed approval=%+v err=%v", consumed, err)
	}
	if err := consumeAPIApplicationApprovalNonce(path, approval, 42, "resume-provider-7", now); !errors.Is(err, errAPIApplicationApprovalNonce) {
		t.Fatalf("second consumption error=%v, want nonce error", err)
	}
}

func TestConsumeAPIApplicationApprovalNonceAllowsExactlyOneCompetingConsumer(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "approval.json")
	approval := validManualAPIApplicationApproval(now)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			errs <- consumeAPIApplicationApprovalNonce(path, approval, 42, "resume-provider-7", now)
		}()
	}
	wait.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, errAPIApplicationApprovalNonce) {
			t.Fatalf("competing consumer error=%v, want nonce conflict", err)
		}
	}
	if successes != 1 {
		t.Fatalf("competing consumer successes=%d, want exactly one", successes)
	}
}

func TestConsumeAPIApplicationApprovalNonceAcrossProcesses(t *testing.T) {
	if os.Getenv("HH_API_NONCE_CHILD") == "1" {
		path := os.Getenv("HH_API_NONCE_APPROVAL")
		resultPath := os.Getenv("HH_API_NONCE_RESULT")
		barrier := os.Getenv("HH_API_NONCE_BARRIER")
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(barrier); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = os.WriteFile(resultPath, []byte("other"), 0o600)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		approval, err := loadAPIApplicationApproval(path)
		if err == nil {
			err = consumeAPIApplicationApprovalNonce(path, approval, 42, "resume-provider-7", now)
		}
		result := "other"
		if err == nil {
			result = "success"
		} else if errors.Is(err, errAPIApplicationApprovalNonce) {
			result = "nonce"
		}
		_ = os.WriteFile(resultPath, []byte(result), 0o600)
		return
	}

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "approval.json")
	approval := validManualAPIApplicationApproval(now)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	barrier := filepath.Join(dir, "start")
	commands := make([]*exec.Cmd, 0, 2)
	results := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		resultPath := filepath.Join(dir, fmt.Sprintf("result-%d", index))
		cmd := exec.Command(os.Args[0], "-test.run", "^TestConsumeAPIApplicationApprovalNonceAcrossProcesses$", "-test.v")
		cmd.Env = append(os.Environ(), "HH_API_NONCE_CHILD=1", "HH_API_NONCE_APPROVAL="+path, "HH_API_NONCE_RESULT="+resultPath, "HH_API_NONCE_BARRIER="+barrier)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		results = append(results, resultPath)
	}
	if err := os.WriteFile(barrier, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("nonce child failed: %v", err)
		}
	}
	successes, nonceFailures := 0, 0
	for _, resultPath := range results {
		result, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal(err)
		}
		switch string(result) {
		case "success":
			successes++
		case "nonce":
			nonceFailures++
		default:
			t.Fatalf("unexpected child result %q", result)
		}
	}
	if successes != 1 || nonceFailures != 1 {
		t.Fatalf("cross-process nonce results success=%d nonce=%d, want 1/1", successes, nonceFailures)
	}
}
