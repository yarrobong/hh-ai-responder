package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CandidateTx is the candidate-specific transaction boundary. It is not a
// generic unit-of-work abstraction: one callback owns one canonical aggregate
// mutation and its event/provenance writes.
type CandidateTx interface {
	Candidate() CandidateWriter
	Repository() *PostgresCandidateRepository
	CurrentCandidate(context.Context) (Candidate, error)
	PersistCandidateIfVersion(context.Context, Candidate, int) error
}

type PostgresCandidateStore struct {
	pool        *pgxpool.Pool
	candidateID string
}

func NewPostgresCandidateStore(pool *pgxpool.Pool) *PostgresCandidateStore {
	return &PostgresCandidateStore{pool: pool}
}

func NewPostgresCandidateStoreForID(pool *pgxpool.Pool, candidateID string) *PostgresCandidateStore {
	return &PostgresCandidateStore{pool: pool, candidateID: candidateID}
}

func (s *PostgresCandidateStore) Repository() *PostgresCandidateRepository {
	if s == nil {
		return nil
	}
	return NewPostgresCandidateRepositoryForID(s.pool, s.candidateID)
}

func (s *PostgresCandidateStore) WithTx(ctx context.Context, fn func(CandidateTx) error) error {
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
	repo := newPostgresCandidateRepositoryTxForID(tx, s.candidateID)
	if err := fn(postgresCandidateTx{repo: repo}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres candidate transaction: %w", err)
	}
	return nil
}

type postgresCandidateTx struct{ repo *PostgresCandidateRepository }

func (t postgresCandidateTx) Candidate() CandidateWriter               { return t.repo }
func (t postgresCandidateTx) Repository() *PostgresCandidateRepository { return t.repo }
func (t postgresCandidateTx) CurrentCandidate(ctx context.Context) (Candidate, error) {
	return t.repo.CurrentCandidate(ctx)
}
func (t postgresCandidateTx) PersistCandidateIfVersion(ctx context.Context, candidate Candidate, expectedVersion int) error {
	return t.repo.PersistCandidateIfVersion(ctx, candidate, expectedVersion)
}
