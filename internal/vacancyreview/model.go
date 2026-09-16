package vacancyreview

import (
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/vacancy"
)

// State is explicit user review state. An absent current-state row is the
// canonical representation of unseen/never explicitly reviewed.
type State string

const (
	StateSeen        State = "seen"
	StateInteresting State = "interesting"
	StateDismissed   State = "dismissed"
	StatePrepared    State = "prepared"
	StateApplied     State = "applied"
)

func (s State) Valid() bool {
	switch s {
	case StateSeen, StateInteresting, StateDismissed, StatePrepared, StateApplied:
		return true
	default:
		return false
	}
}

func (s State) String() string { return string(s) }

// EffectiveState is the queue-facing projection. ApplicationLinked is local
// evidence that this system handled the vacancy; it is not fresh HH
// already-responded evidence.
type EffectiveState struct {
	VacancyID          int
	State              State
	ApplicationLinked  bool
	Stored             *ReviewState
	Freshness          vacancy.Freshness
	ChangedSinceReview *bool
}

type ReviewState struct {
	VacancyID                   int
	State                       State
	StateChangedAt              time.Time
	DecisionSourceFingerprint   string
	DecisionMaterialFingerprint string
	FingerprintVersion          int
	Reason                      string
	UpdatedAt                   time.Time
}

type Event struct {
	ID                  int64
	VacancyID           int
	Type                State
	OccurredAt          time.Time
	SourceFingerprint   string
	MaterialFingerprint string
	FingerprintVersion  int
	Reason              string
	Source              string
}

const (
	SourceUser              = "user"
	SourceSystemApplication = "system_application"
	SourceMigration         = "migration"
)

func ValidateSource(source string) error {
	switch strings.TrimSpace(source) {
	case SourceUser, SourceSystemApplication, SourceMigration:
		return nil
	default:
		return errors.New("invalid vacancy review event source")
	}
}

func ValidateAction(state State, source string, at time.Time) error {
	if !state.Valid() {
		return errors.New("invalid vacancy review state")
	}
	if at.IsZero() {
		return errors.New("vacancy review event timestamp is required")
	}
	return ValidateSource(source)
}

// CanTransition keeps the lifecycle useful for explicit reconsideration while
// making applied terminal. Repeating an action is handled separately by the
// repository's idempotency rule.
func CanTransition(from *State, to State) bool {
	if !to.Valid() {
		return false
	}
	if from == nil {
		return true
	}
	if *from == StateApplied {
		return to == StateApplied
	}
	switch to {
	case StateSeen:
		return *from == StateSeen
	case StateInteresting:
		return *from == StateSeen || *from == StateInteresting || *from == StateDismissed
	case StateDismissed:
		return *from == StateSeen || *from == StateInteresting || *from == StateDismissed
	case StatePrepared:
		return *from != StateApplied
	case StateApplied:
		return true
	default:
		return false
	}
}
