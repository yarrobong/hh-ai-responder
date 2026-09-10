package followuporchestration

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/employerreply"
	"hh-ai-responder/internal/usecase/followupdraft"
)

type Service struct {
	snapshots      SnapshotLoader
	eligibility    EligibilityEvaluator
	followUp       DraftPreparer
	drafts         DraftStore
	clarifications ClarificationWriter
}

func NewService(dependencies Dependencies) *Service {
	return &Service{snapshots: dependencies.Snapshots, eligibility: dependencies.Eligibility, followUp: dependencies.FollowUp, drafts: dependencies.Drafts, clarifications: dependencies.Clarifications}
}

// Prepare evaluates and prepares one local follow-up proposal. The leaf
// generation is proposal-only; a fresh reload/evaluation always happens
// before a generated draft is persisted.
func (s *Service) Prepare(ctx context.Context, applicationID string, now time.Time) (Result, error) {
	if s == nil || s.snapshots == nil || s.eligibility == nil || s.followUp == nil {
		return Result{}, errors.New("follow-up orchestration is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("follow-up orchestration context is nil")
	}
	if strings.TrimSpace(applicationID) == "" {
		return Result{}, errors.New("application id is required")
	}
	loaded, err := s.snapshots.Load(ctx, applicationID, now)
	if err != nil {
		return Result{}, err
	}
	eligibility := s.eligibility.Evaluate(loaded, now)
	if !isEligible(eligibility) {
		return Result{Decision: manualReview(eligibility.Reason, eligibility.Warnings), Outcome: OutcomeManualReview}, nil
	}

	if s.drafts != nil {
		if saved, ok, findErr := s.drafts.FindReusable(ctx, applicationID, loaded.Fingerprint, loaded.Generation.Context); findErr != nil {
			return Result{}, findErr
		} else if ok {
			if saved.Invalid {
				return Result{Decision: manualReview("Saved draft no longer matches candidate knowledge", []string{"stale_ai_draft"}), Outcome: OutcomeManualReview}, nil
			}
			return Result{Decision: reusedDecision(saved), Outcome: OutcomeReused, Reused: true}, nil
		}
	}
	loaded.Generation.Eligibility = eligibility
	result, err := s.followUp.Prepare(ctx, loaded.Generation)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	decision := result.Decision
	if decision.Action == employerreply.ActionNeedCandidate && s.clarifications != nil {
		if err := s.clarifications.Persist(ctx, ClarificationInput{
			ConversationID: loaded.ConversationID, ApplicationID: loaded.ApplicationID,
			VacancyID: loaded.VacancyID, EmployerMessage: loaded.EmployerMessage,
			EmployerMessageID: loaded.EmployerMessageID, Reason: decision.Reason,
			Missing: append([]employerreply.MissingInformation{}, decision.MissingInformation...),
		}); err != nil {
			return Result{}, err
		}
		return Result{Decision: decision, Outcome: OutcomeNeedsCandidate}, nil
	}
	if decision.Action != employerreply.ActionDraftReply || s.drafts == nil {
		return Result{Decision: decision, Outcome: outcomeFor(decision.Action)}, nil
	}

	// Generation is intentionally proposal-only. Reload all relevant local
	// state and evaluate again immediately before committing the draft.
	fresh, err := s.snapshots.Reload(ctx, applicationID, now)
	if err != nil {
		return Result{Decision: manualReview("Follow-up state unavailable", []string{"fresh_state_unavailable"}), Outcome: OutcomeManualReview}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	freshEligibility := s.eligibility.Evaluate(fresh, now)
	if !isEligible(freshEligibility) {
		return Result{Decision: manualReview("Follow-up state changed", freshEligibility.Warnings), Outcome: OutcomeManualReview}, nil
	}
	if err := s.drafts.Save(ctx, Draft{ApplicationID: fresh.ApplicationID, ConversationID: fresh.ConversationID, InputFingerprint: fresh.Fingerprint, Text: decision.Draft, DecisionReason: decision.Reason, UsedFacts: append([]string{}, decision.UsedFacts...)}); err != nil {
		return Result{}, err
	}
	return Result{Decision: decision, Outcome: OutcomeDraftReady}, nil
}

func isEligible(value followupdraft.Eligibility) bool {
	return value.Status == followupdraft.EligibilityStatusEligible
}

func manualReview(reason string, warnings []string) employerreply.Decision {
	return employerreply.Decision{Action: employerreply.ActionManualReview, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: []employerreply.MissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: append([]string{}, warnings...)}
}

func reusedDecision(draft Draft) employerreply.Decision {
	return employerreply.Decision{Action: employerreply.ActionDraftReply, Draft: draft.Text, Reason: draft.DecisionReason, Confidence: 1, UsedFacts: append([]string{}, draft.UsedFacts...), MissingInformation: []employerreply.MissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: []string{}}
}

func outcomeFor(action employerreply.Action) string {
	switch action {
	case employerreply.ActionDraftReply:
		return OutcomeDraftReady
	case employerreply.ActionNeedCandidate:
		return OutcomeNeedsCandidate
	default:
		return OutcomeManualReview
	}
}
