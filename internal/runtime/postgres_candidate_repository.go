package runtime

import (
	"hh-ai-responder/internal/candidate"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	"hh-ai-responder/internal/ports"
)

// Root names remain compatibility shims while canonical Candidate SQL is
// owned by the PostgreSQL storage adapter.
type CandidateWriter = ports.CandidateWriter
type PostgresCandidateRepository = postgresstorage.CandidateRepository

var (
	ErrCandidateNotFound = postgresstorage.ErrCandidateNotFound
	ErrConflict          = postgresstorage.ErrConflict
)

func NewPostgresCandidateRepository(pool *pgxpool.Pool) *PostgresCandidateRepository {
	return postgresstorage.NewCandidateRepository(pool)
}

func NewPostgresCandidateRepositoryForID(pool *pgxpool.Pool, candidateID string) *PostgresCandidateRepository {
	return postgresstorage.NewCandidateRepositoryForID(pool, candidateID)
}

func newPostgresCandidateRepositoryTx(tx pgx.Tx) *PostgresCandidateRepository {
	return postgresstorage.NewCandidateRepositoryForTx(tx)
}

func newPostgresCandidateRepositoryTxForID(tx pgx.Tx, candidateID string) *PostgresCandidateRepository {
	return postgresstorage.NewCandidateRepositoryForTxID(tx, candidateID)
}

func candidateFingerprint(value Candidate) (string, error) {
	return postgresstorage.CandidateFingerprint(value)
}

func validateCanonicalCandidate(value Candidate) error {
	return postgresstorage.ValidateCandidate(value)
}

func validSkillUsageContext(value CanonicalSkillUsageContext) bool {
	switch value {
	case candidate.CanonicalSkillUsageCommercial, candidate.CanonicalSkillUsagePetProject, candidate.CanonicalSkillUsageEducational, candidate.CanonicalSkillUsagePersonal, candidate.CanonicalSkillUsageStudiedOnly, candidate.CanonicalSkillUsageUnknown, candidate.CanonicalSkillUsageExplicitlyNotUsed:
		return true
	default:
		return false
	}
}
