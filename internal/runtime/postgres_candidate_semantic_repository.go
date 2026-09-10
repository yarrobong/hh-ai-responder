package runtime

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
)

// PostgresCandidateSemanticRepository is retained as a root compatibility
// alias. The authoritative implementation and all semantic PostgreSQL SQL
// live in the storage adapter.
type PostgresCandidateSemanticRepository = postgresstorage.SemanticRepository

func NewPostgresCandidateSemanticRepository(pool *pgxpool.Pool) *PostgresCandidateSemanticRepository {
	return postgresstorage.NewSemanticRepository(pool)
}

func newPostgresCandidateSemanticRepositoryTx(tx pgx.Tx) *PostgresCandidateSemanticRepository {
	return postgresstorage.NewSemanticRepositoryForTx(tx)
}
