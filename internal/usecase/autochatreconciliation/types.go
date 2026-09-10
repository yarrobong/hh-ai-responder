package autochatreconciliation

import (
	"context"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
)

type HistoryMessage struct {
	ProviderMessageID string
	Sender            string
	Direction         string
	Timestamp         time.Time
}

type HistorySnapshot struct {
	ConversationID string
	Messages       []HistoryMessage
	ObservedAt     time.Time
}

// HistoryReader is a targeted provider-read capability. It contains no
// arbitrary requester and no HH writer.
type HistoryReader interface {
	ReadConversationHistory(context.Context, string) (HistorySnapshot, error)
}

type AttemptReader interface {
	GetByID(context.Context, string) (domain.Attempt, error)
}

type ReconciliationWriter interface {
	RecordReconciliation(context.Context, string, domain.ReconciliationEvidence, time.Time) error
}

type Status string

const (
	StatusConfirmed     Status = "CONFIRMED"
	StatusInsufficient  Status = "INSUFFICIENT"
	StatusUnavailable   Status = "UNAVAILABLE"
	StatusConflicting   Status = "CONFLICTING"
	StatusUnsupported   Status = "UNSUPPORTED"
	StatusNotApplicable Status = "NOT_APPLICABLE"
	StatusPersistence   Status = "PERSISTENCE_ERROR"
)

type Result struct {
	Attempt       domain.Attempt                `json:"attempt"`
	PreviousState domain.State                  `json:"previous_state"`
	NewState      domain.State                  `json:"new_state"`
	Status        Status                        `json:"status"`
	Evidence      domain.ReconciliationEvidence `json:"evidence"`
	ReadAttempted bool                          `json:"read_attempted"`
	Reason        string                        `json:"reason,omitempty"`
}
