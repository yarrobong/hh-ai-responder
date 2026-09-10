// Package applicationattempt contains the automatic-only executor wrapper
// that places a durable local reservation immediately before HH dispatch.
package applicationattempt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

var (
	ErrReservationPersistence      = errors.New("automatic application attempt reservation failed")
	ErrAttemptBlocked              = errors.New("automatic application is blocked by an existing attempt")
	ErrOutcomePersistenceUncertain = errors.New("automatic application outcome persistence is uncertain")
)

type Executor struct {
	store          attemptport.Store
	next           applicationsubmission.ApplicationExecutor
	now            func() time.Time
	blockedHandler func(context.Context, int) error
	notifications  reliabilitynotifications.Sink
}

func NewExecutor(store attemptport.Store, next applicationsubmission.ApplicationExecutor, now func() time.Time) *Executor {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Executor{store: store, next: next, now: now}
}

// NewExecutorWithBlockedHandler preserves R14.1 dispatch semantics while
// allowing the caller to perform a read-only reconciliation when a durable
// blocking attempt is encountered. The handler cannot authorize a dispatch.
func NewExecutorWithBlockedHandler(store attemptport.Store, next applicationsubmission.ApplicationExecutor, now func() time.Time, handler func(context.Context, int) error) *Executor {
	return NewExecutorWithBlockedHandlerAndNotifications(store, next, now, handler, nil)
}

// NewExecutorWithBlockedHandlerAndNotifications preserves dispatch semantics
// while adding best-effort advisory projection. Notification projection has
// no authority over reservation, dispatch, or replay state.
func NewExecutorWithBlockedHandlerAndNotifications(store attemptport.Store, next applicationsubmission.ApplicationExecutor, now func() time.Time, handler func(context.Context, int) error, notifications reliabilitynotifications.Sink) *Executor {
	e := NewExecutor(store, next, now)
	if e != nil {
		e.blockedHandler = handler
		e.notifications = notifications
	}
	return e
}

func (e *Executor) SubmitApplication(ctx context.Context, request applicationsubmission.ApplicationRequest) (applicationsubmission.ExecutionResult, error) {
	if e == nil || e.store == nil || e.next == nil {
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, ErrReservationPersistence
	}
	attempt, err := domain.New(request.VacancyID, request.ResumeID, e.now())
	if err != nil {
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, fmt.Errorf("%w: %v", ErrReservationPersistence, err)
	}
	reserved, err := e.store.Reserve(ctx, attempt)
	if err != nil {
		if errors.Is(err, domain.ErrTargetBlocked) {
			if reserved.Existing != nil {
				e.projectOutcome(ctx, reliabilitynotifications.ApplicationOutcome{AttemptID: reserved.Existing.AttemptID, VacancyID: reserved.Existing.VacancyID, State: string(reserved.Existing.State)})
			}
			if e.blockedHandler != nil {
				// Reconciliation is best-effort observability/recovery only. Any
				// handler result still leaves the existing attempt blocking.
				_ = e.blockedHandler(ctx, request.VacancyID)
			}
			return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, fmt.Errorf("%w: %v", ErrAttemptBlocked, err)
		}
		e.projectStoreUnavailable(ctx, request.VacancyID, err)
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, fmt.Errorf("%w: %v", ErrReservationPersistence, err)
	}
	if !reserved.Reserved {
		e.projectStoreUnavailable(ctx, request.VacancyID, ErrReservationPersistence)
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, ErrReservationPersistence
	}

	execution, executionErr := e.next.SubmitApplication(ctx, request)
	execution.AttemptID = attempt.AttemptID
	state := stateForExecution(execution.Outcome)
	errorClass := strings.TrimSpace(executionErrString(executionErr))
	if err := e.store.RecordOutcome(ctx, attempt.AttemptID, state, e.now(), execution.ProviderStatus, errorClass); err != nil {
		// The durable SENDING record is intentionally retained by the store. The
		// caller must never turn this into a retryable NOT_SENT result.
		execution.Outcome = applicationsubmission.ExecutionDeliveryUncertain
		e.projectOutcome(ctx, reliabilitynotifications.ApplicationOutcome{AttemptID: attempt.AttemptID, VacancyID: attempt.VacancyID, State: string(domain.StateSending), PersistenceUncertain: true})
		return execution, fmt.Errorf("%w: %v", ErrOutcomePersistenceUncertain, err)
	}
	e.projectOutcome(ctx, reliabilitynotifications.ApplicationOutcome{AttemptID: attempt.AttemptID, VacancyID: attempt.VacancyID, State: string(state)})
	return execution, executionErr
}

func (e *Executor) projectOutcome(ctx context.Context, event reliabilitynotifications.ApplicationOutcome) {
	if e != nil && e.notifications != nil {
		_ = e.notifications.ProjectApplicationOutcome(ctx, event)
	}
}

func (e *Executor) projectStoreUnavailable(ctx context.Context, vacancyID int, _ error) {
	if e != nil && e.notifications != nil {
		_ = e.notifications.ProjectApplicationStoreUnavailable(ctx, reliabilitynotifications.ApplicationStoreHealth{VacancyID: vacancyID})
	}
}

func stateForExecution(outcome applicationsubmission.ExecutionOutcome) domain.State {
	switch outcome {
	case applicationsubmission.ExecutionAccepted:
		return domain.StateAccepted
	case applicationsubmission.ExecutionRejected:
		return domain.StateRejected
	case applicationsubmission.ExecutionDeliveryUncertain:
		return domain.StateDeliveryUncertain
	default:
		return domain.StateNotSent
	}
}

func executionErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
