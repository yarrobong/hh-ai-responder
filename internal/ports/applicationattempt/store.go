// Package applicationattempt defines the narrow persistence capability for
// durable automatic application attempts.
package applicationattempt

import (
	"context"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
)

type ReserveResult struct {
	Reserved bool
	Attempt  domain.Attempt
	Existing *domain.Attempt
}

type Store interface {
	Reserve(context.Context, domain.Attempt) (ReserveResult, error)
	RecordOutcome(context.Context, string, domain.State, time.Time, int, string) error
	Get(context.Context, string) (domain.Attempt, error)
}

// BlockingReader is the read-only capability used by automatic application
// gating. It is deliberately separate from Store so policy code cannot gain a
// write capability merely by checking whether a target is blocked.
type BlockingReader interface {
	FindBlocking(context.Context, int) (domain.Attempt, error)
}

// ReadQuery is the bounded, workflow-specific query used by operator
// inspection. A zero Limit is invalid; callers must choose an explicit cap.
type ReadQuery struct {
	Limit     int
	States    []domain.State
	VacancyID *int
}

// Reader is deliberately separate from Store. Inspection cannot acquire
// reservation or outcome-recording capability through this interface.
type Reader interface {
	List(context.Context, ReadQuery) ([]domain.Attempt, error)
	GetByID(context.Context, string) (domain.Attempt, error)
}
