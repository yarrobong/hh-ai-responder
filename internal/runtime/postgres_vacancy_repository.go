package runtime

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
)

// PostgresVacancyRepository is a root compatibility alias. The authoritative
// implementation, SQL, scanners and error translation live in postgresstorage.
type PostgresVacancyRepository = postgresstorage.VacancyRepository

func NewPostgresVacancyRepository(pool *pgxpool.Pool) *PostgresVacancyRepository {
	return postgresstorage.NewVacancyRepository(pool)
}

// newPostgresVacancyRepositoryTx preserves the root transaction composition
// API while constructing the adapter-owned repository over the current tx.
func newPostgresVacancyRepositoryTx(tx pgx.Tx) *PostgresVacancyRepository {
	return postgresstorage.NewVacancyRepositoryForTx(tx)
}
