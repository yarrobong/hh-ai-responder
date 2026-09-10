package applicationattemptpolicy

import (
	"context"
	"errors"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

var ErrAttemptStoreUnavailable = errors.New("automatic application attempt store is unavailable")

type Classification string

const (
	NoAttempt          Classification = "NO_ATTEMPT"
	BlockingConfirmed  Classification = "BLOCKING_CONFIRMED"
	BlockingUnresolved Classification = "BLOCKING_UNRESOLVED"
	ReplayableHistory  Classification = "REPLAYABLE_HISTORY"
	StoreUnavailable   Classification = "ATTEMPT_STORE_UNAVAILABLE"
)

type Decision struct {
	Classification Classification
	Attempt        *domain.Attempt
	Reason         string
}

type Gate struct {
	reader attemptport.BlockingReader
}

func NewGate(reader attemptport.BlockingReader) *Gate {
	return &Gate{reader: reader}
}

// Evaluate returns the authoritative automatic gate decision for one
// vacancy. A missing blocking record means normal selection may continue;
// REJECTED and NOT_SENT history is therefore replayable only as a fresh future
// attempt, never as reuse of the old AttemptID.
func (g *Gate) Evaluate(ctx context.Context, vacancyID int) (Decision, error) {
	if ctx == nil {
		return Decision{Classification: StoreUnavailable, Reason: ErrAttemptStoreUnavailable.Error()}, ErrAttemptStoreUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Decision{Classification: StoreUnavailable, Reason: err.Error()}, err
	}
	if g == nil || g.reader == nil {
		return Decision{Classification: StoreUnavailable, Reason: ErrAttemptStoreUnavailable.Error()}, ErrAttemptStoreUnavailable
	}
	attempt, err := g.reader.FindBlocking(ctx, vacancyID)
	if errors.Is(err, domain.ErrAttemptNotFound) {
		return Decision{Classification: NoAttempt, Reason: "no blocking automatic application attempt exists"}, nil
	}
	if err != nil {
		return Decision{Classification: StoreUnavailable, Reason: err.Error()}, errors.Join(ErrAttemptStoreUnavailable, err)
	}
	if attempt.VacancyID != vacancyID || !domain.IsBlocking(attempt.State) {
		return Decision{Classification: StoreUnavailable, Reason: "attempt store returned an invalid blocking record"}, ErrAttemptStoreUnavailable
	}
	value := attempt
	decision := Decision{Attempt: &value, Classification: ClassifyState(attempt.State)}
	switch decision.Classification {
	case BlockingConfirmed:
		decision.Reason = "durable automatic application attempt blocks replay"
	case BlockingUnresolved:
		decision.Reason = "automatic application delivery remains unresolved; replay is forbidden"
	default:
		return Decision{Classification: StoreUnavailable, Reason: "unknown blocking attempt state"}, ErrAttemptStoreUnavailable
	}
	return decision, nil
}

// ClassifyState is the single state-to-gate mapping used by policy tests and
// documentation. Storage adapters persist/query states; they do not interpret
// replay safety.
func ClassifyState(state domain.State) Classification {
	switch state {
	case domain.StateTargetResponseConfirmed, domain.StateAccepted:
		return BlockingConfirmed
	case domain.StateSending, domain.StateDeliveryUncertain:
		return BlockingUnresolved
	case domain.StateRejected, domain.StateNotSent:
		return ReplayableHistory
	default:
		return StoreUnavailable
	}
}
