package hhwritepreflight

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/writeapproval"
)

type Dependencies struct {
	Chats     ChatStateReader
	Vacancies VacancyResponseStateReader
	Now       func() time.Time
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}
}

// PreflightChatMessage obtains exactly one fresh targeted chat state and
// compares it with the already-validated local authorization evidence.
func (s *Service) PreflightChatMessage(ctx context.Context, input ChatInput) Result {
	result := Result{Operation: OperationChatMessage, TargetID: input.ExpectedExternalID}
	if err := contextError(ctx); err != nil {
		return unavailableResult(result, err)
	}
	if s == nil || s.deps.Chats == nil {
		return unavailableResult(result, errors.New("fresh HH chat read is unavailable"))
	}
	externalID := strings.TrimSpace(input.ExpectedExternalID)
	if externalID == "" {
		return blockedResult(result, "HH conversation destination is ambiguous")
	}
	state, err := s.deps.Chats.ReadChatState(ctx, externalID)
	if err != nil {
		return unavailableResult(result, err)
	}
	result.Evidence = Evidence{
		Operation: OperationChatMessage, TargetID: externalID,
		ObservedExternalID: state.ExternalID, ObservedState: state.State,
		ObservedMessageID: state.LastMessageID, MessageCount: state.MessageCount,
		ObservedAt: s.now(),
	}

	if strings.TrimSpace(state.ExternalID) == "" || state.ExternalID != externalID {
		result.Reasons = append(result.Reasons, "HH conversation destination is ambiguous")
	}
	if input.Authorization.LastMessageID != state.LastMessageID {
		result.Reasons = append(result.Reasons, "new HH message arrived after approval")
	}
	if state.State == "rejected" || state.State == "closed" {
		result.Reasons = append(result.Reasons, "HH conversation is closed or rejected")
	}
	if input.ActionType == writeapproval.ActionConversationReply {
		if state.State != "" && state.State != "candidate_action_required" {
			result.Reasons = append(result.Reasons, "HH conversation no longer requires a candidate reply")
		}
		if state.ReplyRequirement != "" && state.ReplyRequirement != ReplyRequirementRequired {
			result.Reasons = append(result.Reasons, "fresh HH conversation reply policy is not REPLY_REQUIRED")
		}
	}
	if len(state.Warnings) > 0 {
		result.Reasons = append(result.Reasons, "fresh HH conversation has consistency warnings")
	}
	if input.LocalMessageCount >= 0 && state.MessageCount < input.LocalMessageCount {
		result.Reasons = append(result.Reasons, "HH conversation history is incomplete")
	}
	if len(result.Reasons) > 0 {
		result.Status = StatusStale
		if state.State == "rejected" || state.State == "closed" {
			result.Status = StatusBlocked
		}
		return result
	}
	result.Status = StatusPassed
	return result
}

// PreflightVacancyResponse retains the existing narrow availability checks
// for callers that expose a typed vacancy-response reader. The legacy root
// application workflow currently owns the provider HTML parser and is not
// routed through this service; this method therefore does not invent test or
// resume identity checks that the reader did not provide.
func (s *Service) PreflightVacancyResponse(ctx context.Context, input VacancyInput) VacancyResult {
	result := VacancyResult{Operation: OperationVacancyResponse, VacancyID: input.VacancyID}
	if err := contextError(ctx); err != nil {
		return vacancyUnavailable(result, err)
	}
	if input.VacancyID <= 0 {
		return vacancyBlocked(result, "vacancy identity is invalid")
	}
	if s == nil || s.deps.Vacancies == nil {
		return vacancyUnavailable(result, errors.New("fresh HH vacancy response read is unavailable"))
	}
	state, err := s.deps.Vacancies.ReadVacancyResponseState(ctx, input.VacancyID)
	if err != nil {
		return vacancyUnavailable(result, err)
	}
	result.Evidence = VacancyEvidence{
		Operation: OperationVacancyResponse, VacancyID: input.VacancyID,
		ResponseURL: state.ResponseURL, Archived: state.Archived,
		ArchivedKnown: state.ArchivedKnown, AlreadyResponded: state.AlreadyResponded,
		AlreadyRespondedKnown: state.AlreadyRespondedKnown, CanApply: state.CanApply,
		CanApplyKnown: state.CanApplyKnown, TestPresent: state.TestPresent,
		TestPresentKnown: state.TestPresentKnown, LetterRequired: state.LetterRequired,
		LetterRequiredKnown: state.LetterRequiredKnown, ObservedAt: s.now(),
	}
	if state.ArchivedKnown && state.Archived {
		return vacancyBlocked(result, "vacancy is archived")
	}
	if state.AlreadyRespondedKnown && state.AlreadyResponded {
		return vacancyBlocked(result, "already responded according to vacancy detail")
	}
	if state.CanApplyKnown && !state.CanApply {
		return vacancyBlocked(result, "vacancy does not allow an application")
	}
	if !state.ArchivedKnown {
		result.Reasons = append(result.Reasons, "archived state is unknown")
	}
	if !state.AlreadyRespondedKnown {
		result.Reasons = append(result.Reasons, "already-responded state is unknown")
	}
	if state.TestPresentKnown && state.TestPresent {
		// A caller that has prepared an atomic test response supplies the
		// expected applicability. In that flow the test is safe to continue
		// only after the separate exact metadata comparison passes. Callers
		// without prepared test data retain the legacy fail-closed behavior.
		if input.ExpectedTestPresent == nil {
			result.Reasons = append(result.Reasons, "vacancy has a test; safe live test flow is not enabled")
		}
	}
	if !state.TestPresentKnown {
		result.Reasons = append(result.Reasons, "test state is unknown")
	}
	if !state.LetterRequiredKnown {
		result.Reasons = append(result.Reasons, "cover-letter requirement is unknown")
	}
	if !state.CanApplyKnown {
		result.Reasons = append(result.Reasons, "application availability is unknown")
	}
	if input.ExpectedTestPresent != nil && *input.ExpectedTestPresent != state.TestPresent {
		result.Reasons = append(result.Reasons, "vacancy test applicability changed")
	}
	if strings.TrimSpace(input.ExpectedResponseURL) != "" && input.ExpectedResponseURL != state.ResponseURL {
		result.Reasons = append(result.Reasons, "vacancy response destination changed")
	}
	if len(result.Reasons) > 0 {
		result.Status = StatusManualReview
		return result
	}
	result.Status = StatusPassed
	return result
}

func (s *Service) now() time.Time {
	if s == nil || s.deps.Now == nil {
		return time.Now().UTC()
	}
	return s.deps.Now().UTC()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("HH preflight context is nil")
	}
	return ctx.Err()
}

func unavailableResult(result Result, err error) Result {
	result.Status, result.Err = StatusUnavailable, err
	result.Reasons = []string{"fresh HH conversation preflight failed"}
	return result
}

func blockedResult(result Result, reason string) Result {
	result.Status, result.Reasons = StatusBlocked, []string{reason}
	return result
}

func vacancyUnavailable(result VacancyResult, err error) VacancyResult {
	result.Status, result.Err = StatusUnavailable, err
	result.Reasons = []string{"fresh HH vacancy response preflight failed"}
	return result
}

func vacancyBlocked(result VacancyResult, reason string) VacancyResult {
	result.Status, result.Reasons = StatusBlocked, []string{reason}
	return result
}
