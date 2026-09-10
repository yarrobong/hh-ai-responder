// Package autochatattemptport defines the narrow durable authority used by
// legacy automatic-chat orchestration.
package autochatattemptport

import (
	"context"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
)

type ReserveResult struct {
	Reserved bool
	Attempt  domain.Attempt
	Existing *domain.Attempt
}

type Store interface {
	FindBlockingForTrigger(context.Context, string, string) (domain.Attempt, error)
	Reserve(context.Context, domain.Attempt) (ReserveResult, error)
	RecordOutcome(context.Context, string, domain.State, time.Time, string, int, string) error
}

// ReadQuery is the bounded, workflow-specific query used by operator
// inspection. A zero Limit is invalid; callers must choose an explicit cap.
type ReadQuery struct {
	Limit          int
	States         []domain.State
	ConversationID *string
	ActionType     *domain.ActionType
}

// Reader is deliberately separate from Store. Inspection cannot acquire
// reservation or outcome-recording capability through this interface.
type Reader interface {
	List(context.Context, ReadQuery) ([]domain.Attempt, error)
	GetByID(context.Context, string) (domain.Attempt, error)
}

// ReconciliationWriter is the only local mutation capability exposed to the
// auto-chat reconciliation use case. It cannot reserve an attempt or execute
// an HH action.
type ReconciliationWriter interface {
	RecordReconciliation(context.Context, string, domain.ReconciliationEvidence, time.Time) error
}
