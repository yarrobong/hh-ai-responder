package applicationattempt

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
)

type State string

const (
	StateSending                 State = "SENDING"
	StateAccepted                State = "ACCEPTED"
	StateRejected                State = "REJECTED"
	StateNotSent                 State = "NOT_SENT"
	StateDeliveryUncertain       State = "DELIVERY_UNCERTAIN"
	StateTargetResponseConfirmed State = "TARGET_RESPONSE_CONFIRMED"
)

// EvidenceKind is the typed result of a read-only provider reconciliation.
// Absence is deliberately represented as insufficient evidence, never as a
// non-delivery assertion.
type EvidenceKind string

const (
	EvidenceConfirmedResponseExists EvidenceKind = "CONFIRMED_RESPONSE_EXISTS"
	EvidenceInsufficient            EvidenceKind = "INSUFFICIENT_EVIDENCE"
	EvidenceUnavailable             EvidenceKind = "EVIDENCE_UNAVAILABLE"
	EvidenceConflicting             EvidenceKind = "CONFLICTING_EVIDENCE"
)

type EvidenceStrength string

const (
	EvidenceStrong EvidenceStrength = "STRONG"
	EvidenceAbsent EvidenceStrength = "ABSENT"
)

const EvidenceSourceManualProviderVerification = "MANUAL_PROVIDER_VERIFICATION"

// ReconciliationEvidence records the latest bounded read observation. It is
// additive so R14.1 JSON records remain loadable.
type ReconciliationEvidence struct {
	Kind                   EvidenceKind             `json:"kind,omitempty"`
	Strength               EvidenceStrength         `json:"strength,omitempty"`
	Source                 string                   `json:"source,omitempty"`
	ConfirmationSource     string                   `json:"confirmation_source,omitempty"`
	ProviderApplicationID  string                   `json:"provider_application_id,omitempty"`
	ProviderNegotiationID  string                   `json:"provider_negotiation_id,omitempty"`
	ProviderConversationID string                   `json:"provider_conversation_id,omitempty"`
	ProviderIdentities     []ProviderIdentity       `json:"provider_identities,omitempty"`
	History                []ReconciliationEvidence `json:"history,omitempty"`
	ProviderResponseAt     *time.Time               `json:"provider_response_at,omitempty"`
	ObservedAt             time.Time                `json:"observed_at,omitempty"`
}

// ProviderIdentity keeps provider identifiers typed and source-bound. A
// negotiation/topic id and a conversation/chat id are different identities;
// they must not be compared as if they were one generic provider id.
type ProviderIdentity struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	VacancyID int    `json:"vacancy_id"`
}

var (
	ErrInvalidAttempt    = errors.New("invalid application attempt")
	ErrInvalidTransition = errors.New("invalid application attempt transition")
	ErrAttemptNotFound   = errors.New("application attempt not found")
	ErrTargetBlocked     = errors.New("automatic application target is blocked by an existing attempt")
	ErrImmutableIdentity = errors.New("application attempt identity is immutable")
)

// Attempt is the durable local write-safety record. AttemptID is only a local
// correlation identifier; it is not sent as a provider idempotency key.
type Attempt struct {
	AttemptID      string                  `json:"attempt_id"`
	VacancyID      int                     `json:"vacancy_id"`
	ResumeID       string                  `json:"resume_id"`
	State          State                   `json:"state"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	ProviderStatus int                     `json:"provider_status,omitempty"`
	ErrorClass     string                  `json:"error_class,omitempty"`
	Reconciliation *ReconciliationEvidence `json:"reconciliation,omitempty"`
}

func New(vacancyID int, resumeID string, now time.Time) (Attempt, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id, err := NewID()
	if err != nil {
		return Attempt{}, err
	}
	value := Attempt{AttemptID: id, VacancyID: vacancyID, ResumeID: strings.TrimSpace(resumeID), State: StateSending, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if err := value.Validate(); err != nil {
		return Attempt{}, err
	}
	return value, nil
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate application attempt id: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func (a Attempt) Validate() error {
	if strings.TrimSpace(a.AttemptID) == "" || a.VacancyID <= 0 || strings.TrimSpace(a.ResumeID) == "" || !validState(a.State) || a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return ErrInvalidAttempt
	}
	return nil
}

func ValidTransition(from, to State) bool {
	if to == StateTargetResponseConfirmed {
		return from == StateSending || from == StateAccepted || from == StateDeliveryUncertain
	}
	if from != StateSending {
		return false
	}
	switch to {
	case StateAccepted, StateRejected, StateNotSent, StateDeliveryUncertain:
		return true
	default:
		return false
	}
}

func IsBlocking(state State) bool {
	switch state {
	case StateSending, StateAccepted, StateDeliveryUncertain, StateTargetResponseConfirmed:
		return true
	default:
		return false
	}
}

func IsReplayable(state State) bool {
	return state == StateRejected || state == StateNotSent
}

func (a Attempt) WithOutcome(state State, now time.Time, providerStatus int, errorClass string) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if !ValidTransition(a.State, state) {
		return Attempt{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.State, state)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(a.CreatedAt) {
		now = a.CreatedAt
	}
	a.State = state
	a.UpdatedAt = now.UTC()
	a.ProviderStatus = providerStatus
	a.ErrorClass = strings.TrimSpace(errorClass)
	return a, nil
}

// WithReconciliation applies only the safe reconciliation transition. A
// confirmed attempt stays confirmed even if a concurrent, weaker read arrives
// later.
func (a Attempt) WithReconciliation(evidence ReconciliationEvidence, now time.Time) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(a.CreatedAt) {
		now = a.CreatedAt
	}
	if evidence.ObservedAt.IsZero() {
		evidence.ObservedAt = now.UTC()
	}
	if evidence.Kind == EvidenceConfirmedResponseExists {
		if a.State != StateTargetResponseConfirmed {
			if !ValidTransition(a.State, StateTargetResponseConfirmed) {
				return Attempt{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.State, StateTargetResponseConfirmed)
			}
			a.State = StateTargetResponseConfirmed
		}
		if a.Reconciliation != nil && strings.TrimSpace(a.Reconciliation.ProviderApplicationID) != "" &&
			strings.TrimSpace(evidence.ProviderApplicationID) != "" && a.Reconciliation.ProviderApplicationID != evidence.ProviderApplicationID {
			return Attempt{}, fmt.Errorf("%w: provider application identity changed", ErrInvalidTransition)
		}
		if a.Reconciliation != nil && strings.TrimSpace(a.Reconciliation.ProviderNegotiationID) != "" &&
			strings.TrimSpace(evidence.ProviderNegotiationID) != "" && a.Reconciliation.ProviderNegotiationID != evidence.ProviderNegotiationID {
			return Attempt{}, fmt.Errorf("%w: provider negotiation identity changed", ErrInvalidTransition)
		}
		if a.Reconciliation != nil && strings.TrimSpace(a.Reconciliation.ProviderConversationID) != "" &&
			strings.TrimSpace(evidence.ProviderConversationID) != "" && a.Reconciliation.ProviderConversationID != evidence.ProviderConversationID {
			return Attempt{}, fmt.Errorf("%w: provider conversation identity changed", ErrInvalidTransition)
		}
		if a.Reconciliation != nil {
			if evidence.ProviderApplicationID == "" {
				evidence.ProviderApplicationID = a.Reconciliation.ProviderApplicationID
			}
			if evidence.ProviderNegotiationID == "" {
				evidence.ProviderNegotiationID = a.Reconciliation.ProviderNegotiationID
			}
			if evidence.ProviderResponseAt == nil {
				evidence.ProviderResponseAt = a.Reconciliation.ProviderResponseAt
			}
			if evidence.ConfirmationSource == "" {
				evidence.ConfirmationSource = a.Reconciliation.ConfirmationSource
			}
			if len(evidence.ProviderIdentities) == 0 {
				evidence.ProviderIdentities = append([]ProviderIdentity(nil), a.Reconciliation.ProviderIdentities...)
			}
		}
		if a.Reconciliation != nil {
			previous := *a.Reconciliation
			previous.History = nil
			evidence.History = append(append([]ReconciliationEvidence(nil), a.Reconciliation.History...), previous)
		}
		a.Reconciliation = &evidence
	} else if a.State != StateTargetResponseConfirmed {
		// Keep unresolved attempts blocking. This records the latest observation
		// without turning absence or uncertainty into NOT_SENT.
		if a.Reconciliation != nil {
			previous := *a.Reconciliation
			previous.History = nil
			evidence.History = append(append([]ReconciliationEvidence(nil), a.Reconciliation.History...), previous)
		}
		a.Reconciliation = &evidence
	}
	a.UpdatedAt = now.UTC()
	return a, nil
}

func SameIdentity(a, b Attempt) bool {
	return a.AttemptID == b.AttemptID && a.VacancyID == b.VacancyID && a.ResumeID == b.ResumeID && a.CreatedAt.Equal(b.CreatedAt)
}

func validState(value State) bool {
	switch value {
	case StateSending, StateAccepted, StateRejected, StateNotSent, StateDeliveryUncertain, StateTargetResponseConfirmed:
		return true
	default:
		return false
	}
}
