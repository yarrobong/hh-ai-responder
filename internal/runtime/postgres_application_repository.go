package runtime

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
)

// PostgresApplicationRepository is a root compatibility alias. The
// authoritative Application SQL, scanner, event ledger and error translation
// live in the PostgreSQL storage adapter.
type PostgresApplicationRepository = postgresstorage.ApplicationRepository

var ErrRepositoryConflict = postgresstorage.ErrRepositoryConflict

func NewPostgresApplicationRepository(pool *pgxpool.Pool) *PostgresApplicationRepository {
	return postgresstorage.NewApplicationRepository(pool)
}

func newPostgresApplicationRepositoryTx(tx pgx.Tx) *PostgresApplicationRepository {
	return postgresstorage.NewApplicationRepositoryForTx(tx)
}
