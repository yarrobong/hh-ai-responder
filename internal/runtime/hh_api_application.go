package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	hhwrite "hh-ai-responder/internal/ports/hhwrite"
	attemptusecase "hh-ai-responder/internal/usecase/applicationattempt"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
	"hh-ai-responder/internal/vacancy"
)

const (
	apiApplicationApprovalVersion = 1
	apiApplicationApprovalMaxAge  = 30 * time.Minute
	manualApprovalBasis           = "OPERATOR_MANUAL_REVIEW"
)

var (
	errAPIApplicationApprovalStale    = errors.New("API application approval is stale")
	errAPIApplicationApprovalIdentity = errors.New("API application approval identity does not match the command")
	errAPIApplicationApprovalState    = errors.New("API application approval is not ready for explicit send")
	errAPIApplicationApprovalContent  = errors.New("API application approval content is invalid")
	errAPIApplicationApprovalNonce    = errors.New("API application approval nonce is invalid or already used")
)

// APIApplicationApproval is the minimal artifact accepted by the controlled
// API application command. It deliberately contains no cookies, tokens, or
// candidate-private context.
type APIApplicationApproval struct {
	Version                         int        `json:"version"`
	VacancyID                       int        `json:"vacancy_id"`
	ProviderResumeID                string     `json:"provider_resume_id,omitempty"`
	SelectedResumeID                string     `json:"selected_resume_id,omitempty"`
	BrowserResumeHash               string     `json:"browser_resume_hash,omitempty"`
	CoverLetter                     string     `json:"cover_letter"`
	ContentHash                     string     `json:"content_hash"`
	Nonce                           string     `json:"nonce"`
	NonceUsedAt                     *time.Time `json:"nonce_used_at,omitempty"`
	Status                          string     `json:"status"`
	FinalDecision                   string     `json:"final_decision"`
	PreviewFreshAt                  time.Time  `json:"preview_fresh_at"`
	ApprovalBasis                   string     `json:"approval_basis,omitempty"`
	OperatorApproved                bool       `json:"operator_approved,omitempty"`
	OperatorApprovalTimestamp       time.Time  `json:"operator_approval_timestamp,omitempty"`
	OriginalAIScore                 *int       `json:"original_ai_score,omitempty"`
	OriginalAIRecommendation        string     `json:"original_ai_recommendation,omitempty"`
	OriginalAIRecommendationReasons []string   `json:"original_ai_recommendation_reasons,omitempty"`
	OriginalFinalDecision           string     `json:"original_final_decision,omitempty"`
	PilotArtifactHash               string     `json:"pilot_artifact_hash,omitempty"`
	PreparationID                   string     `json:"preparation_id,omitempty"`
	PreparationHash                 string     `json:"preparation_hash,omitempty"`
}

func loadAPIApplicationApproval(path string) (APIApplicationApproval, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return APIApplicationApproval{}, errors.New("explicit --approval-file is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return APIApplicationApproval{}, errors.New("API application approval file could not be opened")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var approval APIApplicationApproval
	if err := decoder.Decode(&approval); err != nil {
		return APIApplicationApproval{}, fmt.Errorf("invalid API application approval: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return APIApplicationApproval{}, errors.New("invalid API application approval: multiple JSON values")
	}
	return approval, nil
}

func validateAPIApplicationApproval(approval APIApplicationApproval, vacancyID int, providerResumeID string, now time.Time) error {
	if approval.Version != apiApplicationApprovalVersion || approval.VacancyID <= 0 || approval.VacancyID != vacancyID {
		return errAPIApplicationApprovalIdentity
	}
	if err := validatePreparationReferenceShape(approval.PreparationID, approval.PreparationHash); err != nil {
		return errAPIApplicationApprovalIdentity
	}
	approvedResumeID := strings.TrimSpace(approval.ProviderResumeID)
	if approvedResumeID == "" {
		approvedResumeID = strings.TrimSpace(approval.SelectedResumeID)
	}
	if approvedResumeID == "" || strings.TrimSpace(providerResumeID) == "" || approvedResumeID != strings.TrimSpace(providerResumeID) {
		return errAPIApplicationApprovalIdentity
	}
	automatic := approval.Status == "READY_FOR_EXPLICIT_SEND" && approval.FinalDecision == "MATCH" && strings.TrimSpace(approval.ApprovalBasis) == "" && !approval.OperatorApproved
	manual := approval.Status == pilotManualReviewStatus && approval.FinalDecision == "REVIEW_REQUIRED" && approval.ApprovalBasis == manualApprovalBasis && approval.OperatorApproved
	if !automatic && !manual {
		return errAPIApplicationApprovalState
	}
	if manual {
		if approval.OperatorApprovalTimestamp.IsZero() || approval.OperatorApprovalTimestamp.After(now) || approval.OriginalAIScore == nil || *approval.OriginalAIScore < 0 || *approval.OriginalAIScore > 100 || !isSupportedAIRecommendation(approval.OriginalAIRecommendation) || approval.OriginalFinalDecision != "REVIEW_REQUIRED" || strings.TrimSpace(approval.PilotArtifactHash) == "" {
			return errAPIApplicationApprovalState
		}
	}
	if approval.NonceUsedAt != nil || strings.TrimSpace(approval.Nonce) == "" {
		return errAPIApplicationApprovalNonce
	}
	if approval.CoverLetter != "" {
		if err := validatePilotCoverLetter(approval.CoverLetter); err != nil {
			return fmt.Errorf("%w: %v", errAPIApplicationApprovalContent, err)
		}
	}
	if strings.TrimSpace(approval.ContentHash) == "" || contentHash(approval.CoverLetter) != strings.TrimSpace(approval.ContentHash) {
		return errAPIApplicationApprovalContent
	}
	if now.IsZero() || approval.PreviewFreshAt.IsZero() || approval.PreviewFreshAt.After(now) || now.Sub(approval.PreviewFreshAt) > apiApplicationApprovalMaxAge {
		return errAPIApplicationApprovalStale
	}
	return nil
}

func validateControlledAPIApplicationPreflight(preflight VacancyPreflight) error {
	if preflight.alreadyRespondedEvidence().Value != AlreadyRespondedNo {
		return errors.New("fresh API preflight did not prove duplicate state NO")
	}
	if !preflight.SelectedResumeSuitableKnown || !preflight.SelectedResumeSuitable || !preflight.SuitableResumesScanComplete {
		return errors.New("fresh API preflight did not prove selected resume suitability")
	}
	if !preflight.Available {
		return errors.New("fresh API preflight did not prove application availability AVAILABLE")
	}
	if !preflight.ArchivedKnown || preflight.Archived || preflight.activeState() != VacancyActiveStateActive {
		return errors.New("fresh API preflight did not prove an active vacancy")
	}
	if !preflight.CanApplyKnown || !preflight.CanApply {
		return errors.New("fresh API preflight did not prove can_apply")
	}
	if !preflight.TestPresentKnown || preflight.TestPresent {
		return errors.New("fresh API preflight did not prove a no-test vacancy")
	}
	if !preflight.NegotiationScanComplete {
		return errors.New("fresh API preflight did not complete the duplicate scan")
	}
	if !preflight.VacancyTypeKnown || strings.EqualFold(strings.TrimSpace(preflight.VacancyTypeID), "direct") || strings.EqualFold(strings.TrimSpace(preflight.VacancyTypeID), "closed") || preflight.ResponseIdentifierPresent {
		return errors.New("fresh API preflight did not prove the standard applicant path")
	}
	return nil
}

func approvalProviderResumeID(approval APIApplicationApproval) string {
	if strings.TrimSpace(approval.ProviderResumeID) != "" {
		return strings.TrimSpace(approval.ProviderResumeID)
	}
	return strings.TrimSpace(approval.SelectedResumeID)
}

func consumeAPIApplicationApprovalNonce(path string, expected APIApplicationApproval, vacancyID int, providerResumeID string, now time.Time) error {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(expected.Nonce) == "" || now.IsZero() {
		return errAPIApplicationApprovalNonce
	}
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errAPIApplicationApprovalNonce
		}
		return fmt.Errorf("API application approval nonce lock could not be acquired: %w", err)
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	}()
	current, err := loadAPIApplicationApproval(path)
	if err != nil {
		return err
	}
	if current.NonceUsedAt != nil || current.Nonce != expected.Nonce {
		return errAPIApplicationApprovalNonce
	}
	if current.VacancyID != expected.VacancyID || current.VacancyID != vacancyID || approvalProviderResumeID(current) != approvalProviderResumeID(expected) || approvalProviderResumeID(current) != strings.TrimSpace(providerResumeID) {
		return errAPIApplicationApprovalIdentity
	}
	if current.ContentHash != expected.ContentHash {
		return errAPIApplicationApprovalContent
	}
	if err := validateAPIApplicationApproval(current, vacancyID, providerResumeID, now); err != nil {
		return err
	}
	usedAt := now.UTC()
	current.NonceUsedAt = &usedAt
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return errors.New("API application approval could not be encoded")
	}
	if err := platform.WritePrivateFileAtomic(path, append(raw, '\n'), ".hh-api-approval-*.tmp"); err != nil {
		return fmt.Errorf("API application approval could not be persisted: %w", err)
	}
	return nil
}

func runHHAPIApply(ctx context.Context, args []string, cfg Config, stdout, stderr io.Writer, deps HHAPICommandDeps) error {
	_ = stderr
	vacancyID, providerResumeID, approvalPath, err := parseHHAPIApplyArgs(args)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.HHTransport), "api") {
		return errors.New("hh-api apply requires HH_TRANSPORT=api")
	}
	if !cfg.DryRun && !cfg.HHWriteEnabled {
		return errors.New("hh-api apply requires HH_WRITE_ENABLED=true when HH_DRY_RUN=false")
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		return err
	}
	if err := validateAPIApplicationApproval(approval, vacancyID, providerResumeID, now); err != nil {
		return err
	}
	service, err := newControlledApplicationService(ctx, cfg, deps, 1)
	if err != nil {
		return err
	}
	result, execErr := service.Execute(ctx, approvalPath, now)
	if cfg.DryRun {
		_, _ = fmt.Fprintf(stdout, "WOULD_APPLY vacancy_id=%d resume_id=%s approval_file=explicit preflight=AVAILABLE\n", vacancyID, safeHHAPIResumeID(providerResumeID))
		return execErr
	}
	_, _ = fmt.Fprintf(stdout, "APPLICATION_RESULT vacancy_id=%d resume_id=%s status=%s class=%s\n", vacancyID, safeHHAPIResumeID(providerResumeID), result.Submission.Status, result.Submission.Execution.ApplicationClass)
	if result.FinalOutcome != "" {
		_, _ = fmt.Fprintf(stdout, "FINAL_OUTCOME=%s\n", result.FinalOutcome)
	}
	return execErr
}

type controlledApplicationService struct {
	client    *hhapi.APIHHClient
	transport controlledApplicationTransport
	workflow  ports.CareerWorkflowReader
	store     attemptport.Store
	gateway   *hhwritegateway.Service
	maxRun    int
	write     bool
	dryRun    bool
	now       func() time.Time
}

type controlledAPIApplicationService = controlledApplicationService

type controlledAPIApplicationResult struct {
	Approval           APIApplicationApproval
	Preflight          VacancyPreflight
	Submission         applicationsubmission.Result
	AttemptID          string
	TransportAttempted bool
	FinalOutcome       APIApplicationFinalOutcome
	Reconciliation     applicationreconciliation.Result
}

func newControlledAPIApplicationService(ctx context.Context, cfg Config, deps HHAPICommandDeps, maxWritesPerRun int) (*controlledAPIApplicationService, error) {
	oauthConfig := hhAPIReadOAuthConfig(cfg)
	_, client, err := newHHAPIClient(cfg, oauthConfig, deps)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	service := &controlledApplicationService{client: client, workflow: deps.CareerWorkflow, transport: &apiControlledApplicationTransport{client: client}, maxRun: maxWritesPerRun, write: cfg.HHWriteEnabled, dryRun: cfg.DryRun, now: func() time.Time { return now }}
	if cfg.DryRun {
		return service, nil
	}
	if !cfg.HHWriteEnabled {
		return nil, errors.New("controlled API application requires HH_WRITE_ENABLED=true when HH_DRY_RUN=false")
	}
	store := deps.ApplicationAttempts
	if store == nil {
		backend, backendErr := normalizeStorageBackend(cfg.StorageBackend)
		if backendErr != nil {
			return nil, backendErr
		}
		store, err = buildAutomaticApplicationAttemptStore(ctx, cfg, backend, nil)
		if err != nil {
			return nil, fmt.Errorf("HH API application attempt store is unavailable: %w", err)
		}
	}
	audit := deps.ApplicationAudit
	if cfg.HHMaxWritesPerDay > 0 && audit == nil {
		audit, err = loadHHAPIApplicationAudit()
		if err != nil {
			return nil, err
		}
	}
	service.store = store
	service.gateway = hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: hhapi.NewAPIApplicationWriter(client), Audit: audit}, hhwritegateway.Options{
		WriteEnabled: true, DryRun: false, MaxWritesPerRun: maxWritesPerRun, MaxWritesPerDay: cfg.HHMaxWritesPerDay, Now: service.now,
	})
	return service, nil
}

func newControlledApplicationService(ctx context.Context, cfg Config, deps HHAPICommandDeps, maxWritesPerRun int) (*controlledApplicationService, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.HHTransport), "browser") {
		transport, err := newCookieControlledApplicationTransport(cfg, userAgent)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		if deps.Now != nil {
			now = deps.Now().UTC()
		}
		service := &controlledApplicationService{transport: transport, workflow: deps.CareerWorkflow, maxRun: maxWritesPerRun, write: cfg.HHWriteEnabled, dryRun: cfg.DryRun, now: func() time.Time { return now }}
		if cfg.DryRun {
			return service, nil
		}
		if !cfg.HHWriteEnabled {
			return nil, errors.New("controlled browser application requires HH_WRITE_ENABLED=true when HH_DRY_RUN=false")
		}
		store := deps.ApplicationAttempts
		if store == nil {
			backend, backendErr := normalizeStorageBackend(cfg.StorageBackend)
			if backendErr != nil {
				return nil, backendErr
			}
			store, err = buildAutomaticApplicationAttemptStore(ctx, cfg, backend, nil)
			if err != nil {
				return nil, fmt.Errorf("cookie web application attempt store is unavailable: %w", err)
			}
		}
		audit := deps.ApplicationAudit
		if cfg.HHMaxWritesPerDay > 0 && audit == nil {
			audit, err = loadHHAPIApplicationAudit()
			if err != nil {
				return nil, err
			}
		}
		service.store = store
		service.gateway = hhwritegateway.NewService(hhwritegateway.Dependencies{VacancyResponseWriter: transport.Writer(), Audit: audit}, hhwritegateway.Options{WriteEnabled: true, DryRun: false, MaxWritesPerRun: maxWritesPerRun, MaxWritesPerDay: cfg.HHMaxWritesPerDay, Now: service.now})
		return service, nil
	}
	return newControlledAPIApplicationService(ctx, cfg, deps, maxWritesPerRun)
}

func (s *controlledApplicationService) Execute(ctx context.Context, approvalPath string, now time.Time) (controlledAPIApplicationResult, error) {
	result := controlledAPIApplicationResult{}
	if s == nil || s.transport == nil {
		return result, errors.New("controlled API application service is unavailable")
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		return result, err
	}
	result.Approval = approval
	providerResumeID := approvalProviderResumeID(approval)
	if err := validateAPIApplicationApproval(approval, approval.VacancyID, providerResumeID, now); err != nil {
		return result, err
	}
	if err := validatePreparationApprovalBinding(ctx, s.workflow, approval, approval.VacancyID, providerResumeID); err != nil {
		return result, err
	}
	// This is intentionally the last HH read before nonce reservation and the
	// possible gateway transport. Batch callers reuse the service but never the
	// preflight result between items.
	preparedContext, err := s.transport.Prepare(ctx, approval)
	if err != nil {
		return result, err
	}
	preflight := preparedContext.Preflight
	result.Preflight = preparedContext.Preflight
	if err := validateControlledAPIApplicationPreflight(preflight); err != nil {
		return result, fmt.Errorf("HH API apply blocked by preflight: %w", err)
	}
	if approval.CoverLetter == "" && (!preflight.LetterRequiredKnown || preflight.LetterRequired) {
		return result, errors.New("HH API apply requires a validated cover letter when HH requires one or its state is unknown")
	}
	if preflight.TestPresentKnown && preflight.TestPresent {
		return result, errors.New("HH API apply does not support a vacancy with a required test")
	}
	if preflight.ResponseIdentifierPresent {
		return result, errors.New("HH API apply does not support a direct or external response path")
	}
	prepared := applicationsubmission.PreparedApplication{VacancyID: approval.VacancyID, Vacancy: vacancy.Vacancy{ID: approval.VacancyID}, ResumeID: preparedContext.ResumeID, CoverLetter: approval.CoverLetter}
	prepared.Vacancy.Links = map[string]string{"desktop": preparedContext.RefererURL}
	input := applicationsubmission.Input{Prepared: prepared, CurrentResumeID: preparedContext.ResumeID, RequireCurrentResumeID: true, ResponseURL: preparedContext.ResponseURL}
	if s.dryRun {
		result.Submission = applicationsubmission.Result{VacancyID: approval.VacancyID, Status: applicationsubmission.StatusPreview}
		return result, nil
	}
	if s.store == nil || s.gateway == nil {
		return result, errors.New("controlled API application write dependencies are unavailable")
	}
	if err := consumeAPIApplicationApprovalNonce(approvalPath, approval, approval.VacancyID, providerResumeID, now); err != nil {
		return result, err
	}
	executor := attemptusecase.NewExecutor(s.store, apiApplicationExecutor{gateway: s.gateway}, s.now)
	submissionService := applicationsubmission.NewService(applicationsubmission.Dependencies{Vacancies: apiApplicationApplicabilityReader{preflight: preflight}, Executor: executor}, applicationsubmission.Options{WriteEnabled: s.write, DryRun: false, RequireAvailabilityEvidence: true})
	submission, submitErr := submissionService.Submit(ctx, input)
	result.Submission = submission
	result.AttemptID = submission.Execution.AttemptID
	result.TransportAttempted = submission.Execution.TransportTried
	if result.AttemptID != "" && (submission.Execution.ApplicationClass == hhwrite.ApplicationResultSuccess || submission.Execution.ApplicationClass == hhwrite.ApplicationResultAlreadyApplied || submission.Execution.ApplicationClass == hhwrite.ApplicationResultUnknownSendResult) {
		reconciliationStore, ok := s.store.(applicationreconciliation.AttemptStore)
		if !ok {
			return result, fmt.Errorf("HH API application reconciliation store capability is unavailable")
		}
		finalOutcome, reconciliation, reconcileErr := reconcileControlledAPIApplication(ctx, reconciliationStore, preparedContext.EvidenceReader, result.AttemptID, submission.Execution.ApplicationClass, now)
		result.FinalOutcome, result.Reconciliation = finalOutcome, reconciliation
		if reconcileErr != nil {
			return result, reconcileErr
		}
	}
	return result, submitErr
}

func loadHHAPIApplicationAudit() (hhwritegateway.AuditSink, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, errors.New("HH API application daily audit location is unavailable")
	}
	audit := NewHHWriteAuditStore(filepath.Join(workingDirectory, HHWriteEventsFilename))
	if err := audit.Load(); err != nil {
		return nil, fmt.Errorf("HH API application daily audit is unavailable: %w", err)
	}
	return hhGatewayAuditSink{audit: audit}, nil
}

func parseHHAPIApplyArgs(args []string) (int, string, string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, "", "", errors.New("usage: hh-api apply <vacancy-id> --resume-id <provider-id> --approval-file <path>")
	}
	vacancyID, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || vacancyID <= 0 {
		return 0, "", "", errors.New("HH API apply vacancy ID is invalid")
	}
	resumeID, approvalPath := "", ""
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--resume-id":
			if resumeID != "" || index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return 0, "", "", errors.New("hh-api apply requires exactly one --resume-id value")
			}
			resumeID = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--resume-id="):
			if resumeID != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--resume-id=")) == "" {
				return 0, "", "", errors.New("hh-api apply requires exactly one --resume-id value")
			}
			resumeID = strings.TrimSpace(strings.TrimPrefix(arg, "--resume-id="))
		case arg == "--approval-file":
			if approvalPath != "" || index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return 0, "", "", errors.New("hh-api apply requires exactly one explicit --approval-file value")
			}
			approvalPath = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--approval-file="):
			if approvalPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--approval-file=")) == "" {
				return 0, "", "", errors.New("hh-api apply requires exactly one explicit --approval-file value")
			}
			approvalPath = strings.TrimSpace(strings.TrimPrefix(arg, "--approval-file="))
		default:
			return 0, "", "", errors.New("hh-api apply accepts only one vacancy ID, --resume-id, and --approval-file")
		}
	}
	if resumeID == "" || approvalPath == "" {
		return 0, "", "", errors.New("hh-api apply requires --resume-id and explicit --approval-file")
	}
	return vacancyID, resumeID, approvalPath, nil
}

type apiApplicationApplicabilityReader struct{ preflight VacancyPreflight }

func (r apiApplicationApplicabilityReader) ReadApplicability(context.Context, vacancy.Vacancy) (applicationsubmission.Applicability, error) {
	availableKnown := r.preflight.Available || (r.preflight.CanApplyKnown && !r.preflight.CanApply) || (r.preflight.SelectedResumeSuitableKnown && !r.preflight.SelectedResumeSuitable)
	return applicationsubmission.Applicability{
		Available: r.preflight.Available, AvailableKnown: availableKnown,
		Archived: r.preflight.Archived, ArchivedKnown: r.preflight.ArchivedKnown,
		AlreadyResponded: r.preflight.AlreadyResponded, AlreadyRespondedKnown: r.preflight.AlreadyRespondedKnown,
		TestPresent: r.preflight.TestPresent, TestPresentKnown: r.preflight.TestPresentKnown,
		LetterRequired: r.preflight.LetterRequired, LetterRequiredKnown: r.preflight.LetterRequiredKnown,
		CanApply: r.preflight.CanApply, CanApplyKnown: r.preflight.CanApplyKnown,
		ResponseURL: r.preflight.ResponseURL,
	}, nil
}

type apiApplicationExecutor struct{ gateway *hhwritegateway.Service }

func (e apiApplicationExecutor) SubmitApplication(ctx context.Context, request applicationsubmission.ApplicationRequest) (applicationsubmission.ExecutionResult, error) {
	if e.gateway == nil {
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, errors.New("HH API application write gateway is unavailable")
	}
	result, err := e.gateway.SubmitVacancyResponse(ctx, hhwritegateway.VacancyResponseRequest{VacancyID: request.VacancyID, ProviderResumeID: request.ResumeID, Letter: request.Letter})
	execution := applicationsubmission.ExecutionResult{ApplicationClass: result.ApplicationClass, ProviderID: result.ProviderID, ProviderStatus: result.ProviderStatus, Metadata: result.Metadata, TransportTried: result.TransportAttempted}
	switch result.Outcome {
	case hhwritegateway.OutcomeAccepted:
		execution.Outcome = applicationsubmission.ExecutionAccepted
	case hhwritegateway.OutcomeDeliveryUncertain, hhwritegateway.OutcomePersistenceUncertain:
		execution.Outcome = applicationsubmission.ExecutionDeliveryUncertain
	case hhwritegateway.OutcomeRejected:
		if result.ApplicationClass == hhwrite.ApplicationResultAlreadyApplied {
			execution.Outcome = applicationsubmission.ExecutionDeliveryUncertain
		} else {
			execution.Outcome = applicationsubmission.ExecutionRejected
		}
	default:
		execution.Outcome = applicationsubmission.ExecutionNotSent
	}
	return execution, err
}

type APIApplicationFinalOutcome string

const (
	APIApplicationFinalPostSuccessReconciled        APIApplicationFinalOutcome = "POST_SUCCESS_RECONCILED"
	APIApplicationFinalPostSuccessUnconfirmed       APIApplicationFinalOutcome = "POST_SUCCESS_UNCONFIRMED"
	APIApplicationFinalAlreadyAppliedReconciled     APIApplicationFinalOutcome = "ALREADY_APPLIED_RECONCILED"
	APIApplicationFinalUnknownSendReconciledSuccess APIApplicationFinalOutcome = "UNKNOWN_SEND_RECONCILED_SUCCESS"
	APIApplicationFinalUnknownSendUnresolved        APIApplicationFinalOutcome = "UNKNOWN_SEND_UNRESOLVED"
)

func reconcileControlledAPIApplication(ctx context.Context, store applicationreconciliation.AttemptStore, reader applicationreconciliation.EvidenceReader, attemptID string, class hhwrite.ApplicationResultClass, now time.Time) (APIApplicationFinalOutcome, applicationreconciliation.Result, error) {
	if class != hhwrite.ApplicationResultSuccess && class != hhwrite.ApplicationResultAlreadyApplied && class != hhwrite.ApplicationResultUnknownSendResult {
		return "", applicationreconciliation.Result{}, nil
	}
	service := applicationreconciliation.NewService(applicationreconciliation.Dependencies{Attempts: store, Reader: reader, Now: func() time.Time { return now }})
	reconciled, err := service.Reconcile(ctx, attemptID)
	if class == hhwrite.ApplicationResultSuccess {
		if reconciled.Status == applicationreconciliation.StatusConfirmed {
			return APIApplicationFinalPostSuccessReconciled, reconciled, err
		}
		return APIApplicationFinalPostSuccessUnconfirmed, reconciled, err
	}
	if class == hhwrite.ApplicationResultAlreadyApplied {
		if err != nil {
			return APIApplicationFinalUnknownSendUnresolved, reconciled, err
		}
		return APIApplicationFinalAlreadyAppliedReconciled, reconciled, nil
	}
	if reconciled.Status == applicationreconciliation.StatusConfirmed {
		return APIApplicationFinalUnknownSendReconciledSuccess, reconciled, err
	}
	return APIApplicationFinalUnknownSendUnresolved, reconciled, err
}

type apiApplicationEvidenceReader struct{ client *hhapi.APIHHClient }

var _ applicationreconciliation.EvidenceReader = (*apiApplicationEvidenceReader)(nil)

func (r *apiApplicationEvidenceReader) ReadVacancyResponseEvidence(ctx context.Context, target applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error) {
	if r == nil || r.client == nil || target.VacancyID <= 0 || strings.TrimSpace(target.ResumeID) == "" {
		return applicationreconciliation.EvidenceSnapshot{}, errors.New("API application reconciliation target is invalid")
	}
	preflight, err := apiVacancyPreflightWithSource(ctx, r.client, target.VacancyID, target.ResumeID)
	snapshot := applicationreconciliation.EvidenceSnapshot{VacancyID: target.VacancyID, ObservedAt: time.Now().UTC()}
	if err != nil {
		snapshot.PreflightError = "targeted API vacancy preflight failed"
		snapshot.ApplicationsError = "targeted API negotiation read failed"
		return snapshot, err
	}
	evidence := preflight.alreadyRespondedEvidence()
	snapshot.PreflightAvailable = true
	snapshot.Preflight = applicationreconciliation.PreflightEvidence{
		Available: preflight.Available, AlreadyResponded: evidence.Value == AlreadyRespondedYes,
		AlreadyRespondedKnown: evidence.Value != AlreadyRespondedUnknown, CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown,
	}
	snapshot.ApplicationsAvailable = true
	if preflight.ExistingNegotiation && strings.TrimSpace(preflight.NegotiationID) != "" {
		snapshot.Applications = []applicationreconciliation.ProviderResponse{{
			VacancyID: target.VacancyID, NegotiationID: preflight.NegotiationID, ResponseByApplicant: true,
			Source: "api_targeted_negotiation_scan",
		}}
	}
	return snapshot, nil
}
