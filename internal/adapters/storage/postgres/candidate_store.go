package postgresstorage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
)

// CandidateTx is the Candidate-specific persistence facade for one already
// open PostgreSQL transaction. It exposes persistence capabilities, not
// Candidate mutation policy.
type CandidateTx interface {
	Candidate() ports.CandidateWriter
	Repository() *CandidateRepository
	CurrentCandidate(context.Context) (candidate.Candidate, error)
	PersistCandidateIfVersion(context.Context, candidate.Candidate, int) error
}

// CandidateStore owns the PostgreSQL transaction lifecycle for Candidate
// persistence. The application owns the pool and its connection lifecycle.
type CandidateStore struct {
	pool        *pgxpool.Pool
	candidateID string
}

func NewCandidateStore(pool *pgxpool.Pool) *CandidateStore {
	return &CandidateStore{pool: pool}
}

func NewCandidateStoreForID(pool *pgxpool.Pool, candidateID string) *CandidateStore {
	return &CandidateStore{pool: pool, candidateID: candidateID}
}

func (s *CandidateStore) Repository() *CandidateRepository {
	if s == nil {
		return nil
	}
	return NewCandidateRepositoryForID(s.pool, s.candidateID)
}

// WithTx begins one Candidate transaction, binds the canonical repository to
// it, and commits only after the typed callback succeeds. The deferred
// rollback preserves the previous behavior for callback errors, commit
// errors, context cancellation, and panics; rollback errors are ignored.
func (s *CandidateStore) WithTx(ctx context.Context, fn func(CandidateTx) error) error {
	ctx = postgresContext(ctx)
	if s == nil || s.pool == nil {
		return errors.New("postgres candidate store is not configured")
	}
	if fn == nil {
		return errors.New("candidate transaction callback is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres candidate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	repo := NewCandidateRepositoryForTxID(tx, s.candidateID)
	if err := fn(candidateTx{repo: repo}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres candidate transaction: %w", err)
	}
	return nil
}

type candidateTx struct{ repo *CandidateRepository }

func (t candidateTx) Candidate() ports.CandidateWriter { return t.repo }

func (t candidateTx) Repository() *CandidateRepository { return t.repo }

func (t candidateTx) CurrentCandidate(ctx context.Context) (candidate.Candidate, error) {
	return t.repo.CurrentCandidate(ctx)
}

func (t candidateTx) PersistCandidateIfVersion(ctx context.Context, value candidate.Candidate, expectedVersion int) error {
	return t.repo.PersistCandidateIfVersion(ctx, value, expectedVersion)
}

var _ CandidateTx = candidateTx{}
