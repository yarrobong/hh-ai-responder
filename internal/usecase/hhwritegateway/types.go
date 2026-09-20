package hhwritegateway

import (
	"context"
	"errors"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
)

type ActionStatus string

const (
	ActionPending           ActionStatus = "pending"
	ActionApproved          ActionStatus = "approved"
	ActionSending           ActionStatus = "sending"
	ActionSent              ActionStatus = "sent"
	ActionFailed            ActionStatus = "failed"
	ActionCancelled         ActionStatus = "cancelled"
	ActionStale             ActionStatus = "stale"
	ActionSentUnconfirmed   ActionStatus = "sent_unconfirmed"
	ActionDeliveryUncertain ActionStatus = "delivery_uncertain"
	ActionManualReview      ActionStatus = "manual_review"
	ActionDeliveryConfirmed ActionStatus = "delivery_confirmed"
)

// Action is the small durable projection needed by the execution core. The
// storage adapter is responsible for translating its native action model.
type Action struct {
	ID             string
	Operation      string
	ConversationID string
	Text           string
	Nonce          string
	Status         ActionStatus
	NonceUsedAt    *time.Time
	ProviderID     string
	AttemptedAt    *time.Time
	UpdatedAt      time.Time
}

type ActionRecord struct {
	Outcome    hhwrite.Outcome
	Status     ActionStatus
	ProviderID string
	Timestamp  time.Time
	Error      string
}

// ActionStore exposes semantic transitions rather than a generic repository.
// Reserve must durably persist the sending state and consumed nonce before it
// returns. This is the replay-protection boundary.
type ActionStore interface {
	Load(context.Context, string) (Action, error)
	Reserve(context.Context, string, time.Time) (Action, error)
	Record(context.Context, string, ActionRecord) error
}

type AuditEvent struct {
	ActionID            string
	Operation           string
	ConversationID      string
	Type                string
	Result              string
	ProviderID          string
	ProviderStatus      int
	ResponseContentType string
	ResponseBody        string
	ErrorFields         map[string]string
	CorrelationIDs      map[string]string
	Error               string
	Metadata            map[string]string
	CreatedAt           time.Time
}

type AuditSink interface {
	Append(context.Context, AuditEvent) error
}

type AttemptCounter interface {
	AttemptsSince(context.Context, time.Time) (int, error)
}

type Dependencies struct {
	ChatMessageWriter     hhwrite.ChatMessageWriter
	ChatLeaveWriter       hhwrite.ChatLeaveWriter
	VacancyResponseWriter hhwrite.VacancyResponseWriter
	ResumeWriter          hhwrite.ResumeWriter
	JobSearchStatusWriter hhwrite.JobSearchStatusWriter
	Actions               ActionStore
	Audit                 AuditSink
}

type Options struct {
	WriteEnabled    bool
	DryRun          bool
	MaxWritesPerRun int
	MaxWritesPerDay int
	Now             func() time.Time
}

type GatewayOutcome string

const (
	OutcomeBlocked              GatewayOutcome = "blocked"
	OutcomeDryRun               GatewayOutcome = "dry_run"
	OutcomeAccepted             GatewayOutcome = "accepted"
	OutcomeRejected             GatewayOutcome = "rejected"
	OutcomeNotSent              GatewayOutcome = "not_sent"
	OutcomeDeliveryUncertain    GatewayOutcome = "delivery_uncertain"
	OutcomePersistenceUncertain GatewayOutcome = "persistence_uncertain"
)

type Result struct {
	ActionID            string
	Operation           string
	Outcome             GatewayOutcome
	ApplicationClass    hhwrite.ApplicationResultClass
	Status              ActionStatus
	TransportAttempted  bool
	ProviderID          string
	ProviderStatus      int
	Timestamp           time.Time
	Metadata            map[string]string
	NeedsReconciliation bool
	Error               string
}

type ChatMessageRequest struct {
	ActionID       string
	ConversationID string
	Text           string
	IdempotencyKey string
}

type LeaveChatRequest = hhwrite.ChatLeaveRequest
type VacancyResponseRequest = hhwrite.VacancyResponseRequest
type ResumeTouchRequest = hhwrite.ResumeTouchRequest
type JobSearchStatusRequest = hhwrite.JobSearchStatusRequest

var (
	ErrWriteDisabled            = errors.New("HH writes are disabled")
	ErrDryRun                   = errors.New("HH_DRY_RUN blocks HH writes")
	ErrActionNotFound           = errors.New("HH write action not found")
	ErrActionNotSendable        = errors.New("HH write action is not sendable")
	ErrNonceConsumed            = errors.New("HH write action nonce was already consumed")
	ErrMissingNonce             = errors.New("HH write action nonce is missing")
	ErrCapabilityUnavailable    = errors.New("HH write capability is unavailable")
	ErrPostTransportPersistence = errors.New("HH write outcome persistence failed after transport")
	ErrPreTransportPersistence  = errors.New("HH write state persistence failed before transport")
)
