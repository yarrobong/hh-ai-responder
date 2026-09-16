package vacancyreview

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/ports"
)

// Store is the narrow persistence boundary for vacancy freshness and review
// state. Implementations must keep the event and current-state mutation in
// one transaction.
type Store interface {
	ports.VacancyFreshnessReader
	GetReviewState(context.Context, int) (ReviewState, error)
	ListReviewStates(context.Context, []int) (map[int]ReviewState, error)
	RecordReviewAction(context.Context, Action) error
	ListReviewEvents(context.Context, int) ([]Event, error)
}

type Action struct {
	VacancyID  int
	State      State
	OccurredAt time.Time
	Reason     string
	Source     string
}

// Service owns review semantics and provides a reusable read projection for
// later queue stages. It has no HH transport or AI behavior.
type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) Record(ctx context.Context, action Action) error {
	if s == nil || s.store == nil {
		return errors.New("vacancy review store is not configured")
	}
	if action.OccurredAt.IsZero() {
		action.OccurredAt = time.Now().UTC()
	}
	if action.Source == "" {
		action.Source = SourceUser
	}
	action.Source = strings.TrimSpace(action.Source)
	if action.VacancyID <= 0 {
		return errors.New("vacancy review vacancy id must be positive")
	}
	if err := ValidateAction(action.State, action.Source, action.OccurredAt); err != nil {
		return err
	}
	return s.store.RecordReviewAction(ctx, action)
}

func (s *Service) MarkSeen(ctx context.Context, vacancyID int, at time.Time) error {
	return s.Record(ctx, Action{VacancyID: vacancyID, State: StateSeen, OccurredAt: at})
}

func (s *Service) MarkInteresting(ctx context.Context, vacancyID int, at time.Time) error {
	return s.Record(ctx, Action{VacancyID: vacancyID, State: StateInteresting, OccurredAt: at})
}

func (s *Service) Dismiss(ctx context.Context, vacancyID int, reason string, at time.Time) error {
	return s.Record(ctx, Action{VacancyID: vacancyID, State: StateDismissed, Reason: reason, OccurredAt: at})
}

func (s *Service) MarkPrepared(ctx context.Context, vacancyID int, at time.Time) error {
	return s.Record(ctx, Action{VacancyID: vacancyID, State: StatePrepared, OccurredAt: at})
}

func (s *Service) MarkApplied(ctx context.Context, vacancyID int, at time.Time) error {
	return s.Record(ctx, Action{VacancyID: vacancyID, State: StateApplied, Source: SourceSystemApplication, OccurredAt: at})
}

// ListEffective performs bounded bulk reads: one freshness read, one review
// state read, and one application read. It never marks a vacancy seen.
func (s *Service) ListEffective(ctx context.Context, vacancyIDs []int, applications ports.ApplicationReader) ([]EffectiveState, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("vacancy review store is not configured")
	}
	freshness, err := s.store.ListFreshness(ctx, vacancyIDs)
	if err != nil {
		return nil, err
	}
	states, err := s.store.ListReviewStates(ctx, vacancyIDs)
	if err != nil {
		return nil, err
	}
	applicationLinked := make(map[int]bool)
	if applications != nil {
		values, err := applications.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			if value.VacancyID > 0 {
				applicationLinked[value.VacancyID] = true
			}
		}
	}
	result := make([]EffectiveState, 0, len(vacancyIDs))
	for _, id := range vacancyIDs {
		value := EffectiveState{VacancyID: id, State: StateUnseen, Freshness: freshness[id], ApplicationLinked: applicationLinked[id]}
		if state, ok := states[id]; ok {
			copy := state
			value.Stored = &copy
			value.State = state.State
			if value.Freshness.MaterialFingerprint == "" || state.DecisionMaterialFingerprint == "" ||
				value.Freshness.FingerprintVersion <= 0 || state.FingerprintVersion <= 0 ||
				value.Freshness.FingerprintVersion != state.FingerprintVersion {
				// Hashes from different algorithm contracts are not comparable.
				// The next explicit review decision records the current version.
				value.ChangedSinceReview = nil
			} else {
				changed := value.Freshness.MaterialFingerprint != state.DecisionMaterialFingerprint
				value.ChangedSinceReview = &changed
			}
		}
		if value.ApplicationLinked {
			value.State = StateApplied
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) Effective(ctx context.Context, vacancyID int, applications ports.ApplicationReader) (EffectiveState, error) {
	values, err := s.ListEffective(ctx, []int{vacancyID}, applications)
	if err != nil {
		return EffectiveState{}, err
	}
	if len(values) != 1 {
		return EffectiveState{}, errors.New("vacancy review projection returned no state")
	}
	return values[0], nil
}

const StateUnseen State = "unseen"
