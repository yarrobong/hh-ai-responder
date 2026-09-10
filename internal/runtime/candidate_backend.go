package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CandidatePersistence is selected once at the composition root. A caller
// receives a reader and a mutation service from the same backend, making
// Postgres-read/JSON-write and JSON-read/Postgres-write combinations impossible
// in production construction.
type CandidatePersistence struct {
	Backend           string
	Repository        CandidateRepository
	Mutations         *CandidateMutationService
	SemanticRetriever CandidateSemanticRetriever
	Pool              *pgxpool.Pool
	CandidateID       string
}

func BuildCandidatePersistence(ctx context.Context, config Config) (CandidatePersistence, func(), error) {
	backend, err := normalizeStorageBackend(config.StorageBackend)
	if err != nil {
		return CandidatePersistence{}, func() {}, err
	}
	candidateID := strings.TrimSpace(config.CandidateID)
	if candidateID == "" {
		candidateID = "candidate-local"
	}
	profilePath := config.CandidateProfilePath
	if strings.TrimSpace(profilePath) == "" {
		profilePath = "candidate_profile.json"
	}
	if backend == storageBackendJSON {
		repo := NewJSONCandidateRepository(CandidateRepositoryConfig{ProfilePath: profilePath, StoriesPath: config.CandidateStoriesPath, CandidateID: candidateID, Contacts: config.Contacts, GitHubURL: config.GithubURL})
		return CandidatePersistence{Backend: backend, Repository: repo, Mutations: NewCandidateMutationService(backend, repo, nil, profilePath), CandidateID: candidateID}, func() {}, nil
	}
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: config.DatabaseURL})
	if err != nil {
		return CandidatePersistence{}, func() {}, err
	}
	// Activation is deliberately a preflight, not an implicit migration. A
	// missing schema or candidate row is actionable and never falls back to JSON.
	repo := NewPostgresCandidateRepositoryForID(pool, candidateID)
	if err := preflightPostgresCandidate(ctx, repo, candidateID); err != nil {
		pool.Close()
		return CandidatePersistence{}, func() {}, err
	}
	store := NewPostgresCandidateStoreForID(pool, candidateID)
	mutations := NewCandidateMutationService(backend, repo, store, profilePath)
	var semanticRetriever CandidateSemanticRetriever
	if strings.TrimSpace(config.EmbeddingProvider) != "" {
		provider, providerErr := configuredEmbeddingProvider(config)
		if providerErr != nil {
			pool.Close()
			return CandidatePersistence{}, func() {}, fmt.Errorf("configure semantic embeddings: %w", providerErr)
		}
		semanticService := NewCandidateSemanticSearchService(NewPostgresCandidateSemanticRepository(pool), provider, repo)
		mutations.SetSemanticIndexer(NewCandidateSemanticIndexService(NewPostgresCandidateSemanticRepository(pool), provider))
		semanticRetriever = semanticService
	}
	return CandidatePersistence{Backend: backend, Repository: repo, Mutations: mutations, SemanticRetriever: semanticRetriever, Pool: pool, CandidateID: candidateID}, pool.Close, nil
}

func preflightPostgresCandidate(ctx context.Context, repo *PostgresCandidateRepository, candidateID string) error {
	if repo == nil {
		return errors.New("postgres candidate repository is not configured")
	}
	candidate, err := repo.CurrentCandidate(ctx)
	if err != nil {
		if isMissingCandidateSchema(err) {
			return errors.New("PostgreSQL candidate storage is not initialized; run: hh-ai-responder candidate migrate-postgres --dry-run, then candidate migrate-postgres --apply")
		}
		if errors.Is(err, ErrCandidateNotFound) {
			return fmt.Errorf("PostgreSQL candidate storage has no candidate %q; run: hh-ai-responder candidate migrate-postgres --dry-run, then candidate migrate-postgres --apply", candidateID)
		}
		return fmt.Errorf("PostgreSQL candidate storage preflight failed: %w", err)
	}
	if candidate.ID != candidateID || candidate.Version <= 0 {
		return errors.New("PostgreSQL candidate storage preflight found an incompatible candidate")
	}
	return nil
}

// CandidateKnowledgeSnapshot is used by compatibility consumers such as the
// dashboard. It is a detached read view and has no Save authority in postgres
// mode; all writes still go through CandidateMutationService.
func CandidateKnowledgeSnapshot(candidate Candidate, profilePath string) *CandidateKnowledgeBase {
	kb := NewCandidateKnowledgeBase(profilePath)
	for _, skill := range candidate.Skills {
		if len(skill.SourceAssertions) > 0 {
			for _, assertion := range skill.SourceAssertions {
				kb.Skills = append(kb.Skills, CandidateSkillDetailed{ID: assertion.ID, Name: assertion.Name, Category: assertion.Category, Level: assertion.Level, Projects: assertion.Projects, CanDo: assertion.CanDo, CannotClaim: assertion.CannotClaim, LastUsed: assertion.LastUsed, Negative: assertion.Negative, KnowledgeMetadata: assertion.Metadata})
			}
		} else {
			kb.Skills = append(kb.Skills, CandidateSkillDetailed{ID: skill.ID, Name: skill.DisplayName, Level: skill.Level, Category: skill.Category, CannotClaim: skill.CannotClaim, LastUsed: skill.LastUsed, Negative: skill.Negative, KnowledgeMetadata: skill.Metadata})
		}
	}
	for _, project := range candidate.Projects {
		if project.DetailedSource != nil {
			kb.Projects = append(kb.Projects, *project.DetailedSource)
		}
	}
	kb.Achievements = append(kb.Achievements, candidate.Achievements...)
	kb.Unknowns = append(kb.Unknowns, candidate.Unknowns...)
	kb.Proposals = append(kb.Proposals, candidate.Proposals...)
	kb.Events = append(kb.Events, candidate.Events...)
	return kb
}

func joinCanonicalSkills(view EmployerSafeCandidateKnowledge) string {
	values := make([]string, 0, len(view.Skills))
	for _, skill := range view.Skills {
		if skill.Negative {
			continue
		}
		if name := strings.TrimSpace(skill.Name); name != "" {
			values = append(values, name)
		}
	}
	return strings.Join(values, ", ")
}

func joinCanonicalExperience(view EmployerSafeCandidateKnowledge) string {
	parts := make([]string, 0, len(view.Profile.WorkExperience)+len(view.Profile.Projects))
	for _, item := range view.Profile.WorkExperience {
		if value := strings.TrimSpace(item.Description); value != "" {
			parts = append(parts, value)
		}
	}
	for _, item := range view.Profile.Projects {
		if value := strings.TrimSpace(item.Description); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func canonicalStoriesForLegacy(candidate Candidate) []CandidateStory {
	// Stories are intentionally not used as evidence. The legacy prompt path
	// expects CandidateStory, so this read-only presentation conversion keeps
	// narrative data available without changing truth semantics.
	result := make([]CandidateStory, 0, len(candidate.Stories))
	for _, story := range candidate.Stories {
		result = append(result, CandidateStory{ID: story.ID, Title: story.Title, Situation: story.Situation, Task: story.Task, Action: firstNonEmpty(story.Action, story.Actions), Result: firstNonEmpty(story.Result, story.Outcome), Technologies: append([]string{}, story.Technologies...), Skills: append([]string{}, story.Skills...), Keywords: append([]string{}, story.Keywords...), Tags: append([]string{}, story.Tags...), Roles: append([]string{}, story.Roles...)})
	}
	return result
}

func canonicalProfileForLegacy(candidate Candidate) CandidateProfile {
	profile := CandidateProfile{}
	profile.Identity.FullName = ProfileStringFact{Value: candidate.Identity.FullName, ProfileFact: canonicalProfileFact(candidate.Identity.FullNameMetadata)}
	profile.Identity.Location = ProfileStringFact{Value: candidate.Identity.Location, ProfileFact: canonicalProfileFact(candidate.Identity.LocationMetadata)}
	view := canonicalSafeProfile(candidate)
	profile.Education = append(profile.Education, view.Education...)
	profile.WorkExperience = append(profile.WorkExperience, view.WorkExperience...)
	profile.Projects = append(profile.Projects, view.Projects...)
	profile.Skills = append(profile.Skills, view.Skills...)
	profile.Languages = append(profile.Languages, view.Languages...)
	if view.TotalExperienceMonths != nil {
		profile.TotalExperienceMonths = ProfileIntFact{Value: *view.TotalExperienceMonths, ProfileFact: candidate.Profile.TotalExperienceMonths.ProfileFact}
	}
	profile.WorkPreferences.Relocation = ProfileStringFact{Value: view.Relocation, ProfileFact: candidate.Profile.WorkPreferences.Relocation.ProfileFact}
	profile.WorkPreferences.WorkMode = ProfileStringFact{Value: view.WorkMode, ProfileFact: candidate.Profile.WorkPreferences.WorkMode.ProfileFact}
	profile.WorkPreferences.BusinessTrips = ProfileStringFact{Value: view.BusinessTrips, ProfileFact: candidate.Profile.WorkPreferences.BusinessTrips.ProfileFact}
	profile.WorkPreferences.PrimaryRoles = ProfileStringFact{Value: view.PrimaryRoles, ProfileFact: candidate.Profile.WorkPreferences.PrimaryRoles.ProfileFact}
	profile.WorkPreferences.SecondaryRoles = ProfileStringFact{Value: view.SecondaryRoles, ProfileFact: candidate.Profile.WorkPreferences.SecondaryRoles.ProfileFact}
	profile.WorkPreferences.PreferredRoles = ProfileStringFact{Value: view.PreferredRoles, ProfileFact: candidate.Profile.WorkPreferences.PreferredRoles.ProfileFact}
	profile.EmployerCommunicationPreferences.Salary = ProfileStringFact{Value: view.SalaryPreference, ProfileFact: candidate.Profile.Communication.Salary.ProfileFact}
	profile.EmployerCommunicationPreferences.AlwaysEmphasize = ProfileListFact{Values: append([]string{}, view.AlwaysEmphasize...), ProfileFact: candidate.Profile.Communication.AlwaysEmphasize.ProfileFact}
	profile.EmployerCommunicationPreferences.AvoidClaiming = ProfileListFact{Values: append([]string{}, view.AvoidClaiming...), ProfileFact: candidate.Profile.Communication.AvoidClaiming.ProfileFact}
	return profile
}
