package ports

import (
	"context"

	"hh-ai-responder/internal/candidate"
)

// CandidateReader exposes the canonical candidate snapshot used by pure
// candidate use cases and orchestration. Legacy profile/knowledge storage is
// deliberately not part of this contract.
type CandidateReader interface {
	CurrentCandidate(context.Context) (candidate.Candidate, error)
}

// CandidateWriter is the non-versioned persistence capability used by
// migration/import flows. It does not describe candidate mutation policy.
type CandidateWriter interface {
	PersistCandidate(context.Context, candidate.Candidate) error
}

// CandidateMutationWriter is the version-checked persistence primitive for a
// canonical candidate mutation. The transaction that provides the row lock is
// infrastructure; callers only depend on the atomic business operation.
type CandidateMutationWriter interface {
	PersistCandidateIfVersion(context.Context, candidate.Candidate, int) error
}
