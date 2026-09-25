package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/coverletter"
)

func buildCareerWorkflowStore(config Config, backend string, career CareerRepositories) (ports.CareerWorkflowStore, func(), error) {
	switch backend {
	case storageBackendJSON:
		return jsonstorage.NewCareerWorkflowRepository(config.CareerAgentWorkflowPath), func() {}, nil
	case storageBackendPostgres:
		if career.Postgres == nil || career.Postgres.Pool() == nil {
			return nil, func() {}, errors.New("postgres career workflow store requires the selected career pool")
		}
		return postgresstorage.NewCareerWorkflowRepository(career.Postgres.Pool()), func() {}, nil
	default:
		return nil, func() {}, fmt.Errorf("unsupported career workflow backend %q", backend)
	}
}

// executeCareerAgentWorkflow owns only the local durable lifecycle. It has no
// HH capability and treats workflow persistence as telemetry/artifact state.
func executeCareerAgentWorkflow(ctx context.Context, store ports.CareerWorkflowStore, report *CareerAgentRunReport, process func() error) error {
	if process == nil {
		return errors.New("career agent process callback is required")
	}
	if store == nil {
		return process()
	}
	if report == nil {
		return errors.New("career agent report is required")
	}
	if err := store.RecoverInterruptedRuns(ctx, time.Now().UTC()); err != nil {
		return fmt.Errorf("recover interrupted Career Agent runs: %w", err)
	}
	if err := store.StartRun(ctx, report.Run); err != nil {
		return fmt.Errorf("start Career Agent workflow run: %w", err)
	}

	processErr := process()
	if itemErr := persistCareerAgentRunItems(ctx, store, report); itemErr != nil && processErr == nil {
		processErr = fmt.Errorf("persist Career Agent run items: %w", itemErr)
	}
	status := careeragent.AgentRunStatusCompleted
	result := "completed"
	if processErr != nil {
		status, result = careeragent.AgentRunStatusFailed, "failed"
	} else if report.Summary.Errors > 0 {
		status, result = careeragent.AgentRunStatusPartial, "completed_with_errors"
	}
	if err := report.Run.Finish(status, result, time.Now().UTC(), processErr); err != nil {
		return fmt.Errorf("finish Career Agent workflow run: %w", err)
	}
	if err := store.FinishRun(ctx, report.Run); err != nil {
		return fmt.Errorf("persist Career Agent workflow run: %w", err)
	}
	return processErr
}

func persistCareerAgentRunItems(ctx context.Context, store ports.CareerWorkflowStore, report *CareerAgentRunReport) error {
	for _, result := range report.Vacancies {
		item, err := careerAgentRunItem(result, report.Run.ID)
		if err != nil {
			return err
		}
		if err := store.UpsertRunItem(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func careerAgentRunItem(result CareerAgentVacancyResult, runID string) (careeragent.AgentRunItem, error) {
	if result.VacancyID <= 0 {
		return careeragent.AgentRunItem{}, errors.New("Career Agent vacancy result requires a vacancy id")
	}
	status := careeragent.AgentRunItemStatusCompleted
	stage := careeragent.AgentRunStageAnalysis
	switch result.TerminalOutcome {
	case TerminalAIMatch:
		status = careeragent.AgentRunItemStatusMatched
	case TerminalAIReject, TerminalDeterministicReject, TerminalAlreadyResponded, TerminalAttemptBlocked:
		status = careeragent.AgentRunItemStatusRejected
	case TerminalReviewRequired:
		status, stage = careeragent.AgentRunItemStatusReviewRequired, careeragent.AgentRunStageReview
	case TerminalError, TerminalDetailFetchFailed:
		status = careeragent.AgentRunItemStatusFailed
		stage = careeragent.AgentRunStageCareerAgent
	}
	evidence, err := json.Marshal(struct {
		TerminalOutcome string `json:"terminal_outcome"`
		FinalDecision   string `json:"final_decision,omitempty"`
		FinalReasonCode string `json:"final_reason_code,omitempty"`
		RouteReason     string `json:"route_reason_code,omitempty"`
		AIEvaluated     bool   `json:"ai_evaluated"`
		AIScore         *int   `json:"ai_score,omitempty"`
		WouldApply      bool   `json:"would_apply"`
	}{
		TerminalOutcome: boundedWorkflowText(result.TerminalOutcome),
		FinalDecision:   boundedWorkflowText(result.FinalDecision),
		FinalReasonCode: boundedWorkflowText(result.FinalReasonCode),
		RouteReason:     boundedWorkflowText(result.FinalRouteReasonCode),
		AIEvaluated:     result.AIEvaluated,
		AIScore:         boundedWorkflowScore(result.AIScore),
		WouldApply:      result.WouldApply,
	})
	if err != nil {
		return careeragent.AgentRunItem{}, fmt.Errorf("encode Career Agent run item evidence: %w", err)
	}
	createdAt := result.ProcessedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	item := careeragent.AgentRunItem{
		ID:           fmt.Sprintf("%s-vacancy-%d", strings.TrimSpace(runID), result.VacancyID),
		RunID:        runID,
		VacancyID:    result.VacancyID,
		Stage:        stage,
		Status:       status,
		DecisionCode: boundedWorkflowText(result.FinalDecision),
		Evidence:     evidence,
		CreatedAt:    createdAt.UTC(),
	}
	if status == careeragent.AgentRunItemStatusFailed {
		item.ErrorCode = "PROCESSING_ERROR"
	}
	if score := boundedWorkflowScore(result.AIScore); score != nil {
		confidence := float64(*score) / 100
		item.Confidence = &confidence
	}
	return item, item.Validate()
}

func boundedWorkflowScore(value *int) *int {
	if value == nil || *value < 0 || *value > 100 {
		return nil
	}
	copy := *value
	return &copy
}

func boundedWorkflowText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 200 {
		return value[:200] + "…"
	}
	return value
}

func (r *HHAIResponder) persistCareerAgentPreparation(value Vacancy, selectedResume ResumeItem, trace CareerAgentVacancyResult, result applicationprocessing.Result) error {
	if r == nil || r.careerWorkflowStore == nil || result.Prepared == nil || strings.TrimSpace(r.careerAgentMode) == "" {
		return nil
	}
	preparation, err := r.buildCareerAgentPreparation(value, selectedResume, trace, result)
	if err != nil {
		return err
	}
	return r.careerWorkflowStore.UpsertPreparation(ctxOrBackground(r.ctx), preparation)
}

func (r *HHAIResponder) buildCareerAgentPreparation(value Vacancy, selectedResume ResumeItem, trace CareerAgentVacancyResult, result applicationprocessing.Result) (careeragent.ApplicationPreparation, error) {
	if r == nil || result.Prepared == nil || strings.TrimSpace(r.careerAgentMode) == "" {
		return careeragent.ApplicationPreparation{}, nil
	}
	if r.careerAgentCandidateVersion <= 0 || strings.TrimSpace(r.careerAgentCandidateID) == "" || strings.TrimSpace(r.careerAgentCandidateHash) == "" {
		return careeragent.ApplicationPreparation{}, errors.New("candidate snapshot identity is unavailable")
	}
	internalResumeID, providerResumeID, err := r.canonicalResumeBinding(selectedResume, trace.SelectedResume)
	if err != nil {
		return careeragent.ApplicationPreparation{}, err
	}
	preparedResumeID := strings.TrimSpace(result.Prepared.ResumeID)
	if preparedResumeID == "" || (preparedResumeID != providerResumeID && preparedResumeID != strings.TrimSpace(selectedResume.Hash) && preparedResumeID != internalResumeID) {
		return careeragent.ApplicationPreparation{}, errors.New("prepared application resume identity does not match selected resume")
	}
	testDrafts := json.RawMessage(nil)
	if result.Prepared.Test != nil && len(result.Prepared.Test.Answers) > 0 {
		raw, err := json.Marshal(result.Prepared.Test.Answers)
		if err != nil {
			return careeragent.ApplicationPreparation{}, fmt.Errorf("encode preparation test drafts: %w", err)
		}
		testDrafts = raw
	}
	requests := make([]careeragent.KnowledgeRequest, 0, len(result.Prepared.CandidateContext.MissingInformation))
	for _, missing := range result.Prepared.CandidateContext.MissingInformation {
		question := boundedWorkflowText(missing.Question)
		if question == "" {
			continue
		}
		requests = append(requests, careeragent.KnowledgeRequest{Topic: "candidate_context", Question: question, Status: "pending"})
	}
	now := time.Now().UTC()
	preparation := careeragent.ApplicationPreparation{
		VacancyID: value.ID, ResumeID: internalResumeID, ResumeProviderID: providerResumeID, ResumeFingerprint: strings.TrimSpace(selectedResume.Hash), BrowserResumeHash: strings.TrimSpace(selectedResume.Hash),
		CandidateID: r.careerAgentCandidateID, CandidateVersion: r.careerAgentCandidateVersion,
		CandidateSnapshotHash: r.careerAgentCandidateHash, RouteStatus: careeragent.ResumeRouteMatch,
		RouteConfidence: boundedWorkflowText(trace.ResumeConfidence), Evidence: workflowPreparationEvidence(trace, result.Prepared.CoverLetterStatus, result.Prepared.CoverLetterFallbackReason, result.Prepared.CoverLetterEvidence),
		CoverLetter: result.Prepared.CoverLetter, TestAnswerDrafts: testDrafts, KnowledgeRequests: requests,
		Status: careeragent.PreparationStatusReady, CreatedAt: now, UpdatedAt: now,
	}
	if result.Prepared.CoverLetterStatus == coverletter.DraftStatusReviewRequired {
		preparation.Status = careeragent.PreparationStatusReviewRequired
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)
	preparation.ID = fmt.Sprintf("preparation-vacancy-%d-%s", value.ID, preparation.InputFingerprint[:12])
	if err := preparation.Validate(); err != nil {
		return careeragent.ApplicationPreparation{}, err
	}
	return preparation, nil
}

func (r *HHAIResponder) canonicalResumeBinding(selected ResumeItem, routeID string) (string, string, error) {
	providerID := strings.TrimSpace(r.resumeIdentifierForValue(selected))
	if providerID == "" {
		return "", "", errors.New("selected resume provider identity is unavailable")
	}

	for _, profile := range r.careerAgentResumes {
		profileProviderID := strings.TrimSpace(profile.ProviderID)
		matches := false
		if strings.TrimSpace(selected.ProviderID) != "" && profileProviderID != "" {
			matches = profileProviderID == providerID
		} else {
			matches = (strings.TrimSpace(profile.Hash) != "" && strings.TrimSpace(profile.Hash) == strings.TrimSpace(selected.Hash)) ||
				(profile.HHID > 0 && profile.HHID == selected.Id)
		}
		if !matches {
			continue
		}
		internalID := strings.TrimSpace(profile.ID)
		if internalID == "" {
			return "", "", errors.New("selected resume internal identity is unavailable")
		}
		if route := strings.TrimSpace(routeID); route != "" && route != internalID && route != strings.TrimSpace(profile.ProviderID) && route != strings.TrimSpace(profile.Hash) {
			return "", "", errors.New("selected resume route identity does not match current resume")
		}
		return internalID, providerID, nil
	}

	if route := strings.TrimSpace(routeID); route != "" {
		return route, providerID, nil
	}
	return careeragent.StableResumeID(selected.Hash, selected.Id, selected.Title), providerID, nil
}

func (r *HHAIResponder) rememberCareerAgentCandidate(value Candidate) error {
	if r == nil {
		return errors.New("HH responder is not configured")
	}
	fingerprint, err := candidateFingerprint(value)
	if err != nil {
		return fmt.Errorf("canonical candidate fingerprint failed: %w", err)
	}
	r.careerAgentCandidateID = value.ID
	r.careerAgentCandidateVersion = value.Version
	r.careerAgentCandidateHash = fingerprint
	return nil
}

func workflowPreparationEvidence(trace CareerAgentVacancyResult, status coverletter.DraftStatus, fallbackReason string, evidence []coverletter.DraftEvidence) json.RawMessage {
	raw, _ := json.Marshal(struct {
		SelectedResume      string                      `json:"selected_resume,omitempty"`
		RouteReason         string                      `json:"route_reason_code,omitempty"`
		Decision            string                      `json:"decision,omitempty"`
		AIScore             *int                        `json:"ai_score,omitempty"`
		CoverLetterStatus   coverletter.DraftStatus     `json:"cover_letter_status,omitempty"`
		CoverLetterFallback string                      `json:"cover_letter_fallback_reason,omitempty"`
		CoverLetterEvidence []coverletter.DraftEvidence `json:"cover_letter_evidence,omitempty"`
	}{
		SelectedResume: boundedWorkflowText(trace.SelectedResume), RouteReason: boundedWorkflowText(trace.FinalRouteReasonCode),
		Decision: boundedWorkflowText(trace.FinalDecision), AIScore: boundedWorkflowScore(trace.AIScore), CoverLetterStatus: status,
		CoverLetterFallback: boundedWorkflowText(fallbackReason), CoverLetterEvidence: append([]coverletter.DraftEvidence(nil), evidence...),
	})
	return raw
}
