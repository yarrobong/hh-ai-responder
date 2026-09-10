package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Keep the transaction API intentionally small and concrete. JSON stores do
// not implement it because staged multi-file writes are not atomic.
type CareerTx interface {
	Vacancies() VacancyRepository
	Applications() ApplicationRepository
	Conversations() ConversationRepository
}

// CareerRepositoriesTx is the descriptive compatibility name for callers
// that prefer the transaction boundary to be explicit in the type name.
type CareerRepositoriesTx = CareerTx

type PostgresCareerStore struct{ pool *pgxpool.Pool }

func NewPostgresCareerStore(pool *pgxpool.Pool) *PostgresCareerStore {
	return &PostgresCareerStore{pool: pool}
}

func (s *PostgresCareerStore) WithTx(ctx context.Context, fn func(CareerTx) error) error {
	ctx = postgresContext(ctx)
	if s == nil || s.pool == nil {
		return errors.New("postgres career store is not configured")
	}
	if fn == nil {
		return errors.New("postgres transaction callback is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres career transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	repos := postgresCareerTx{
		vacancies:     newPostgresVacancyRepositoryTx(tx),
		applications:  newPostgresApplicationRepositoryTx(tx),
		conversations: newPostgresConversationRepositoryTx(tx),
	}
	if err := fn(repos); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres career transaction: %w", err)
	}
	return nil
}

type postgresCareerTx struct {
	vacancies     *PostgresVacancyRepository
	applications  *PostgresApplicationRepository
	conversations *PostgresConversationRepository
}

func (t postgresCareerTx) Vacancies() VacancyRepository          { return t.vacancies }
func (t postgresCareerTx) Applications() ApplicationRepository   { return t.applications }
func (t postgresCareerTx) Conversations() ConversationRepository { return t.conversations }
