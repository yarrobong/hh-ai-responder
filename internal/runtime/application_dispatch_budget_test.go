package runtime

import (
	"testing"

	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
)

func TestAutomaticApplicationBudgetCountsAmbiguousDispatches(t *testing.T) {
	budget := newAutomaticApplicationBudget(2)
	ambiguous := applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionDeliveryUncertain, TransportTried: true}
	if budget.reached(false) {
		t.Fatal("empty dispatch budget was already exhausted")
	}
	if !budget.recordExecution(ambiguous) || !budget.recordExecution(ambiguous) {
		t.Fatal("ambiguous executions were not counted as dispatches")
	}
	if budget.dispatchAttempts != 2 || !budget.reached(false) {
		t.Fatalf("budget=%+v", budget)
	}
	if !budget.reached(false) {
		t.Fatal("third provider dispatch was not gated")
	}
}

func TestAutomaticApplicationBudgetDoesNotCountPreDispatchFailure(t *testing.T) {
	budget := newAutomaticApplicationBudget(1)
	if budget.recordExecution(applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}) {
		t.Fatal("pre-dispatch failure consumed provider dispatch budget")
	}
	if budget.reached(false) {
		t.Fatal("pre-dispatch failure exhausted provider dispatch budget")
	}
}
