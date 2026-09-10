package candidateacquisition

import (
	"strings"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

type ReconciliationInput struct {
	Candidate candidate.Candidate
	Request   CandidateClarificationRequest
	// Context may be supplied by a conversation-aware root adapter. When nil,
	// reconciliation resolves the stored original employer question directly.
	Context *candidatecontext.CandidateContext
}

type ReconciliationResult struct {
	Changed          bool
	Status           CandidateClarificationStatus
	ResolutionReason string
}

// Reconcile checks only acquisition state against a current Candidate
// snapshot. It never mutates Candidate data or performs persistence.
func Reconcile(input ReconciliationInput) (ReconciliationResult, error) {
	if input.Request.Status != ClarificationPending {
		return ReconciliationResult{Status: input.Request.Status}, nil
	}
	query := strings.TrimSpace(input.Request.OriginalEmployerQuestion)
	if query == "" {
		query = input.Request.Question
	}
	var resolved candidatecontext.CandidateContext
	if input.Context != nil {
		resolved = *input.Context
	} else {
		var err error
		resolved, err = candidatecontext.NewResolver(input.Candidate).Resolve(candidatecontext.ResolveInput{Query: query, EmployerMessage: true})
		if err != nil {
			return ReconciliationResult{}, err
		}
	}
	if resolved.MessageIntent != candidatecontext.EmployerMessageIntentFactualQuestion && resolved.MessageIntent != candidatecontext.EmployerMessageIntentCompound {
		return ReconciliationResult{Changed: true, Status: ClarificationResolvedExistingKnowledge, ResolutionReason: "clarification no longer represents a candidate factual question"}, nil
	}
	if len(resolved.UnknownAtomicFacts) == 0 && len(resolved.PartiallyResolvedFacts) == 0 && resolved.RequiresCandidateInput() == false {
		return ReconciliationResult{Changed: true, Status: ClarificationResolvedExistingKnowledge, ResolutionReason: "resolved from current Candidate knowledge"}, nil
	}
	return ReconciliationResult{Status: ClarificationPending}, nil
}
