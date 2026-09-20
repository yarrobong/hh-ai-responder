package applicationattempt

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
)

type fakeStore struct {
	attempts                map[string]domain.Attempt
	reserveErr              error
	outcomeErr              error
	reservedBeforeExecution bool
}

func (s *fakeStore) Reserve(_ context.Context, value domain.Attempt) (attemptport.ReserveResult, error) {
	if s.reserveErr != nil {
		return attemptport.ReserveResult{}, s.reserveErr
	}
	if s.attempts == nil {
		s.attempts = map[string]domain.Attempt{}
	}
	for _, existing := range s.attempts {
		if existing.VacancyID == value.VacancyID && domain.IsBlocking(existing.State) {
			return attemptport.ReserveResult{Existing: &existing}, domain.ErrTargetBlocked
		}
	}
	s.attempts[value.AttemptID] = value
	return attemptport.ReserveResult{Reserved: true, Attempt: value}, nil
}

func (s *fakeStore) RecordOutcome(_ context.Context, id string, state domain.State, updatedAt time.Time, providerStatus int, errorClass string) error {
	if s.outcomeErr != nil {
		return s.outcomeErr
	}
	value, ok := s.attempts[id]
	if !ok {
		return domain.ErrAttemptNotFound
	}
	updated, err := value.WithOutcome(state, updatedAt, providerStatus, errorClass)
	if err != nil {
		return err
	}
	s.attempts[id] = updated
	return nil
}

func (s *fakeStore) Get(_ context.Context, id string) (domain.Attempt, error) {
	value, ok := s.attempts[id]
	if !ok {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	return value, nil
}

type fakeExecutor struct {
	result  applicationsubmission.ExecutionResult
	err     error
	calls   int
	observe func()
}

func (e *fakeExecutor) SubmitApplication(context.Context, applicationsubmission.ApplicationRequest) (applicationsubmission.ExecutionResult, error) {
	e.calls++
	if e.observe != nil {
		e.observe()
	}
	return e.result, e.err
}

func requestFixture() applicationsubmission.ApplicationRequest {
	return applicationsubmission.ApplicationRequest{VacancyID: 42, ResumeID: "resume-1"}
}

func TestExecutorReservesBeforeDispatchAndPersistsAccepted(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted, ProviderStatus: 200}}
	fake.observe = func() {
		if len(store.attempts) != 1 {
			t.Fatal("executor observed no durable SENDING reservation")
		}
		for _, value := range store.attempts {
			if value.State != domain.StateSending {
				t.Fatalf("executor observed state %s", value.State)
			}
		}
	}
	executor := NewExecutor(store, fake, func() time.Time { return time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC) })
	result, err := executor.SubmitApplication(context.Background(), requestFixture())
	if err != nil || result.Outcome != applicationsubmission.ExecutionAccepted || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	for _, value := range store.attempts {
		if value.State != domain.StateAccepted {
			t.Fatalf("final state=%s", value.State)
		}
	}
}

func TestExecutorAmbiguousBlocksNextDispatch(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionDeliveryUncertain}}
	executor := NewExecutor(store, fake, nil)
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); !errors.Is(err, ErrAttemptBlocked) || fake.calls != 1 {
		t.Fatalf("ambiguous attempt was replayed: err=%v calls=%d", err, fake.calls)
	}
}

func TestExecutorReservationAndOutcomeFailuresDoNotRetry(t *testing.T) {
	store := &fakeStore{reserveErr: errors.New("disk unavailable")}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}}
	executor := NewExecutor(store, fake, nil)
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); !errors.Is(err, ErrReservationPersistence) || fake.calls != 0 {
		t.Fatalf("reservation failure dispatched: err=%v calls=%d", err, fake.calls)
	}

	store = &fakeStore{outcomeErr: errors.New("disk full")}
	fake = &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}}
	executor = NewExecutor(store, fake, nil)
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); !errors.Is(err, ErrOutcomePersistenceUncertain) || fake.calls != 1 || len(store.attempts) != 1 {
		t.Fatalf("outcome persistence failure did not retain reservation: err=%v calls=%d attempts=%d", err, fake.calls, len(store.attempts))
	}
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); !errors.Is(err, ErrAttemptBlocked) || fake.calls != 1 {
		t.Fatalf("persistence-uncertain reservation was replayed: err=%v calls=%d", err, fake.calls)
	}
}

func TestExecutorPreloadedSendingAndAcceptedResiduesBlock(t *testing.T) {
	for _, state := range []domain.State{domain.StateSending, domain.StateAccepted} {
		t.Run(string(state), func(t *testing.T) {
			preloaded := func() domain.Attempt {
				value, err := domain.New(42, "resume-1", time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				if state == domain.StateAccepted {
					value, err = value.WithOutcome(state, value.UpdatedAt.Add(time.Minute), 200, "")
					if err != nil {
						t.Fatal(err)
					}
				}
				return value
			}()
			store := &fakeStore{attempts: map[string]domain.Attempt{preloaded.AttemptID: preloaded}}
			fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}}
			_, err := NewExecutor(store, fake, nil).SubmitApplication(context.Background(), requestFixture())
			if !errors.Is(err, ErrAttemptBlocked) || fake.calls != 0 {
				t.Fatalf("preloaded %s was not blocking: err=%v calls=%d", state, err, fake.calls)
			}
		})
	}
}

func TestExecutorBlockedHandlerCannotAuthorizeDispatch(t *testing.T) {
	preloaded := func() domain.Attempt {
		value, err := domain.New(42, "resume-1", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}()
	store := &fakeStore{attempts: map[string]domain.Attempt{preloaded.AttemptID: preloaded}}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}}
	called := 0
	_, err := NewExecutorWithBlockedHandler(store, fake, nil, func(context.Context, int) error {
		called++
		return nil
	}).SubmitApplication(context.Background(), requestFixture())
	if !errors.Is(err, ErrAttemptBlocked) || called != 1 || fake.calls != 0 {
		t.Fatalf("blocked reconciliation handler changed dispatch safety: err=%v handler=%d writer=%d", err, called, fake.calls)
	}
}

func TestExecutorRejectedAttemptCanBeReplayedAsNewAttempt(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionRejected, ProviderStatus: 429}}
	executor := NewExecutor(store, fake, nil)
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); err != nil || fake.calls != 2 {
		t.Fatalf("REJECTED was not replayable on a later independent attempt: err=%v calls=%d", err, fake.calls)
	}
}

func TestExecutorProvenPreDispatchFailureReleasesReservation(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}}
	executor := NewExecutor(store, fake, nil)
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); err != nil {
		t.Fatal(err)
	}
	fake.result = applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}
	if _, err := executor.SubmitApplication(context.Background(), requestFixture()); err != nil || fake.calls != 2 {
		t.Fatalf("proven pre-dispatch failure did not release reservation: err=%v calls=%d", err, fake.calls)
	}
}

func TestExecutorPossibleDispatchLocksVacancyAcrossResumeAndRestart(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionDeliveryUncertain}}
	if _, err := NewExecutor(store, fake, nil).SubmitApplication(context.Background(), requestFixture()); err != nil {
		t.Fatal(err)
	}
	// A new executor instance models a process restart. The persisted store,
	// not the executor instance, must keep the vacancy blocked.
	restarted := NewExecutor(store, &fakeExecutor{result: applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted}}, nil)
	_, err := restarted.SubmitApplication(context.Background(), applicationsubmission.ApplicationRequest{VacancyID: 42, ResumeID: "resume-2"})
	if !errors.Is(err, ErrAttemptBlocked) {
		t.Fatalf("different resume bypassed possible-dispatch lock: %v", err)
	}
}
