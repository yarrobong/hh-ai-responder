package hhwritereconcile

import (
	"context"
	"time"
)

type Operation string

const (
	OperationChatMessage Operation = "chat_message"
)

type TransportOutcome string

const (
	TransportAccepted             TransportOutcome = "accepted"
	TransportRejected             TransportOutcome = "rejected"
	TransportNotSent              TransportOutcome = "not_sent"
	TransportAmbiguous            TransportOutcome = "ambiguous"
	TransportPersistenceUncertain TransportOutcome = "persistence_uncertain"
)

type ActionState string

const (
	ActionPending           ActionState = "pending"
	ActionApproved          ActionState = "approved"
	ActionSending           ActionState = "sending"
	ActionSent              ActionState = "sent"
	ActionFailed            ActionState = "failed"
	ActionCancelled         ActionState = "cancelled"
	ActionStale             ActionState = "stale"
	ActionSentUnconfirmed   ActionState = "sent_unconfirmed"
	ActionDeliveryUncertain ActionState = "delivery_uncertain"
	ActionManualReview      ActionState = "manual_review"
	ActionDeliveryConfirmed ActionState = "delivery_confirmed"
)

type DecisionStatus string

const (
	StatusConfirmed         DecisionStatus = "confirmed"
	StatusNotConfirmed      DecisionStatus = "not_confirmed"
	StatusUncertain         DecisionStatus = "uncertain"
	StatusRemoteUnavailable DecisionStatus = "remote_unavailable"
	StatusManualReview      DecisionStatus = "manual_review"
	StatusNotApplicable     DecisionStatus = "not_applicable"
)

type MatchStrength string

const (
	MatchExactProviderID   MatchStrength = "exact_provider_id"
	MatchStrongExisting    MatchStrength = "strong_existing_match"
	MatchNoMatch           MatchStrength = "no_match"
	MatchConflict          MatchStrength = "conflict"
	MatchRemoteUnavailable MatchStrength = "remote_unavailable"
)

// AttemptEvidence is detached from both the HH writer and local persistence.
// It is the durable/observed context needed to evaluate one already-attempted
// operation; it is not an instruction to send anything.
type AttemptEvidence struct {
	ActionID             string
	Operation            Operation
	TargetConversationID string
	ProviderMessageID    string
	IdempotencyKey       string
	ExactText            string
	AttemptedAt          time.Time
	TransportOutcome     TransportOutcome
	ExistingActionState  ActionState
}

// ChatDeliveryMessage is the small semantic message projection required by
// the deterministic matcher. Empty sender/direction means that the reader
// did not expose that attribute; known contradictory values never match.
type ChatDeliveryMessage struct {
	ProviderID string
	Sender     string
	Direction  string
	Text       string
	Timestamp  time.Time
}

type ChatDeliverySnapshot struct {
	ConversationID string
	Messages       []ChatDeliveryMessage
	ObservedAt     time.Time
}

// ChatDeliveryReader is a targeted, read-only capability. It is intentionally
// not a generic HTTP client and has no mutation methods.
type ChatDeliveryReader interface {
	ReadChatDelivery(context.Context, string) (ChatDeliverySnapshot, error)
}

type Evidence struct {
	ConversationID string
	ProviderID     string
	MatchStrength  MatchStrength
	Sender         string
	Direction      string
	ObservedAt     time.Time
}

type TransitionIntent struct {
	From     ActionState
	To       ActionState
	Reason   string
	Evidence Evidence
}

type Result struct {
	ActionID      string
	Operation     Operation
	Status        DecisionStatus
	MatchStrength MatchStrength
	ReadAttempted bool
	Evidence      Evidence
	Transition    TransitionIntent
}
