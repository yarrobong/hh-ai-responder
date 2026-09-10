package runtime

import (
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Root names remain compatibility aliases. Candidate transaction lifecycle
// and the transaction-scoped persistence facade are owned by postgresstorage.
type CandidateTx = postgresstorage.CandidateTx
type PostgresCandidateStore = postgresstorage.CandidateStore

func NewPostgresCandidateStore(pool *pgxpool.Pool) *PostgresCandidateStore {
	return postgresstorage.NewCandidateStore(pool)
}

func NewPostgresCandidateStoreForID(pool *pgxpool.Pool, candidateID string) *PostgresCandidateStore {
	return postgresstorage.NewCandidateStoreForID(pool, candidateID)
}
