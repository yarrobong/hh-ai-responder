package runtime

import (
	"context"
	"errors"
	"fmt"

	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	attemptpolicy "hh-ai-responder/internal/usecase/applicationattemptpolicy"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

// automaticApplicationGate is the runtime composition seam for the narrow
// cross-run policy. It deliberately does not reserve or submit anything.
func (r *HHAIResponder) automaticApplicationGate(ctx context.Context, vacancyID int) (attemptpolicy.Decision, error) {
	if r == nil {
		return attemptpolicy.Decision{Classification: attemptpolicy.StoreUnavailable, Reason: attemptpolicy.ErrAttemptStoreUnavailable.Error()}, attemptpolicy.ErrAttemptStoreUnavailable
	}
	if r.attemptStoreInitErr != nil {
		return attemptpolicy.Decision{Classification: attemptpolicy.StoreUnavailable, Reason: r.attemptStoreInitErr.Error()}, fmt.Errorf("%w: %v", attemptpolicy.ErrAttemptStoreUnavailable, r.attemptStoreInitErr)
	}
	// Directly constructed dry-run compatibility responders may not install an
	// attempt store because they cannot perform an HH write. A live automatic
	// path without authoritative attempt storage must fail closed.
	if r.applicationAttempts == nil {
		if r.dryRun {
			return attemptpolicy.Decision{Classification: attemptpolicy.NoAttempt, Reason: "automatic attempt store is not configured for this dry-run compatibility responder"}, nil
		}
		return attemptpolicy.Decision{Classification: attemptpolicy.StoreUnavailable, Reason: attemptpolicy.ErrAttemptStoreUnavailable.Error()}, attemptpolicy.ErrAttemptStoreUnavailable
	}
	reader, ok := r.applicationAttempts.(attemptport.BlockingReader)
	if !ok {
		return attemptpolicy.Decision{Classification: attemptpolicy.StoreUnavailable, Reason: "automatic attempt store does not expose blocking reads"}, attemptpolicy.ErrAttemptStoreUnavailable
	}
	return attemptpolicy.NewGate(reader).Evaluate(ctx, vacancyID)
}

func (r *HHAIResponder) reconcileAutomaticAttempt(ctx context.Context, decision attemptpolicy.Decision) (applicationreconciliation.Result, error) {
	if decision.Attempt == nil {
		return applicationreconciliation.Result{}, errors.New("automatic attempt is missing from gate decision")
	}
	service := r.applicationReconciliationService()
	if service == nil {
		return applicationreconciliation.Result{}, applicationreconciliation.ErrNotConfigured
	}
	return service.Reconcile(ctx, decision.Attempt.AttemptID)
}
