package runtime

import applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"

// automaticApplicationBudget counts possible provider dispatches, not only
// accepted responses. It is intentionally per-run and contains no persistent
// daily policy.
type automaticApplicationBudget struct {
	limit            int
	dispatchAttempts int
	plannedPreviews  int
}

func newAutomaticApplicationBudget(limit int) *automaticApplicationBudget {
	return &automaticApplicationBudget{limit: limit}
}

func (b *automaticApplicationBudget) reached(dryRun bool) bool {
	if b == nil || b.limit <= 0 {
		return false
	}
	if dryRun {
		return b.plannedPreviews >= b.limit
	}
	return b.dispatchAttempts >= b.limit
}

func (b *automaticApplicationBudget) recordPreview() {
	if b != nil {
		b.plannedPreviews++
	}
}

func (b *automaticApplicationBudget) recordExecution(result applicationsubmission.ExecutionResult) bool {
	if b == nil || !result.TransportTried {
		return false
	}
	b.dispatchAttempts++
	return true
}
