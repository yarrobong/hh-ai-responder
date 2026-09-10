package jsonstorage

import (
	"context"
	"errors"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
)

type CandidateRepositoryConfig struct {
	ProfilePath string
	StoriesPath string
	CandidateID string
	Contacts    string
	GitHubURL   string
}

// CandidateRepository is the JSON implementation of the canonical read
// boundary. It loads each legacy source through its storage owner and delegates
// normalization to the domain assembly implementation.
type CandidateRepository struct {
	config CandidateRepositoryConfig
	input  *candidate.CanonicalCandidateInput
}

func NewCandidateRepository(config CandidateRepositoryConfig) *CandidateRepository {
	return &CandidateRepository{config: config}
}

func NewCandidateRepositoryFromInput(input candidate.CanonicalCandidateInput) *CandidateRepository {
	return &CandidateRepository{input: &input}
}

func (r *CandidateRepository) CurrentCandidate(ctx context.Context) (candidate.Candidate, error) {
	value, _, err := r.CurrentCandidateWithDiagnostics(ctx)
	return value, err
}

func (r *CandidateRepository) CurrentCandidateWithDiagnostics(ctx context.Context) (candidate.Candidate, candidate.CanonicalCandidateDiagnostics, error) {
	if err := contextError(ctx); err != nil {
		return candidate.Candidate{}, candidate.CanonicalCandidateDiagnostics{}, err
	}
	if r == nil {
		return candidate.Candidate{}, candidate.CanonicalCandidateDiagnostics{}, errors.New("candidate repository is not configured")
	}
	input := candidate.CanonicalCandidateInput{}
	if r.input != nil {
		input = *r.input
	} else {
		profile, err := NewCandidateProfileStore(r.config.ProfilePath).Load()
		if err != nil {
			return candidate.Candidate{}, candidate.CanonicalCandidateDiagnostics{}, err
		}
		knowledge, err := NewCandidateKnowledgeStore(r.config.ProfilePath).Load()
		if err != nil {
			return candidate.Candidate{}, candidate.CanonicalCandidateDiagnostics{}, err
		}
		stories, err := NewCandidateStoryStore(r.config.StoriesPath).Load()
		if err != nil {
			return candidate.Candidate{}, candidate.CanonicalCandidateDiagnostics{}, err
		}
		input.Profile = profile
		input.Knowledge = knowledge
		input.Stories = append([]candidate.CandidateStory{}, stories.Stories...)
	}
	if r.input == nil || r.config.CandidateID != "" {
		input.CandidateID = r.config.CandidateID
	}
	if r.input == nil || r.config.Contacts != "" {
		input.Contacts = r.config.Contacts
	}
	if r.input == nil || r.config.GitHubURL != "" {
		input.GitHubURL = r.config.GitHubURL
	}
	return candidate.BuildCanonicalCandidate(input)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

var _ ports.CandidateReader = (*CandidateRepository)(nil)
