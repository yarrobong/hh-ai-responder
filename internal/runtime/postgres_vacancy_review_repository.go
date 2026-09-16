package runtime

import (
	"github.com/jackc/pgx/v5/pgxpool"

	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
)

// PostgresVacancyReviewRepository keeps the root composition API aligned with
// the canonical PostgreSQL adapter without introducing a JSON review source.
type PostgresVacancyReviewRepository = postgresstorage.VacancyReviewRepository

func NewPostgresVacancyReviewRepository(pool *pgxpool.Pool) *PostgresVacancyReviewRepository {
	return postgresstorage.NewVacancyReviewRepository(pool)
}
