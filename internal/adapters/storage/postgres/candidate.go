package postgresstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/candidateparity"
	"hh-ai-responder/internal/ports"
)

type Candidate = candidate.Candidate
type CandidateAchievement = candidate.CandidateAchievement
type CandidateKnowledgeEvent = candidate.CandidateKnowledgeEvent
type CandidateProject = candidate.CandidateProject
type CandidateUnknown = candidate.CandidateUnknown
type CanonicalCandidateClaim = candidate.CanonicalCandidateClaim
type CanonicalCandidateConstraint = candidate.CanonicalCandidateConstraint
type CanonicalCandidateContact = candidate.CanonicalCandidateContact
type CanonicalCandidateEducation = candidate.CanonicalCandidateEducation
type CanonicalCandidateExperience = candidate.CanonicalCandidateExperience
type CanonicalCandidateLanguage = candidate.CanonicalCandidateLanguage
type CanonicalCandidatePreference = candidate.CanonicalCandidatePreference
type CanonicalCandidateProject = candidate.CanonicalCandidateProject
type CanonicalCandidateSkill = candidate.CanonicalCandidateSkill
type CanonicalCandidateStory = candidate.CanonicalCandidateStory
type CanonicalExternalReference = candidate.CanonicalExternalReference
type CanonicalClaimPolarity = candidate.CanonicalClaimPolarity
type CanonicalSkillCapability = candidate.CanonicalSkillCapability
type CanonicalSkillUsageContext = candidate.CanonicalSkillUsageContext
type CanonicalSkillUse = candidate.CanonicalSkillUse
type KnowledgeMetadata = candidate.KnowledgeMetadata
type KnowledgeProposal = candidate.KnowledgeProposal
type ProjectFact = candidate.ProjectFact
type ResumeFacts = candidate.ResumeFacts

const (
	CanonicalSkillUsageCommercial        = candidate.CanonicalSkillUsageCommercial
	CanonicalSkillUsagePetProject        = candidate.CanonicalSkillUsagePetProject
	CanonicalSkillUsageEducational       = candidate.CanonicalSkillUsageEducational
	CanonicalSkillUsagePersonal          = candidate.CanonicalSkillUsagePersonal
	CanonicalSkillUsageStudiedOnly       = candidate.CanonicalSkillUsageStudiedOnly
	CanonicalSkillUsageUnknown           = candidate.CanonicalSkillUsageUnknown
	CanonicalSkillUsageExplicitlyNotUsed = candidate.CanonicalSkillUsageExplicitlyNotUsed
	CanonicalClaimPositive               = candidate.CanonicalClaimPositive
	CanonicalClaimNegative               = candidate.CanonicalClaimNegative
	CanonicalClaimActive                 = candidate.CanonicalClaimActive
	CanonicalClaimDisputed               = candidate.CanonicalClaimDisputed
	CanonicalClaimSuperseded             = candidate.CanonicalClaimSuperseded
	CandidateUnknownNeedsConfirmation    = candidate.CandidateUnknownNeedsConfirmation
	CandidateUnknownConfirmed            = candidate.CandidateUnknownConfirmed
	CandidateUnknownRejected             = candidate.CandidateUnknownRejected
	CandidateUnknownDismissed            = candidate.CandidateUnknownDismissed
	CandidateUnknownSuperseded           = candidate.CandidateUnknownSuperseded
	KnowledgeProposalPending             = candidate.KnowledgeProposalPending
	KnowledgeProposalConfirmed           = candidate.KnowledgeProposalConfirmed
	KnowledgeProposalRejected            = candidate.KnowledgeProposalRejected
	TruthStatusConfirmed                 = candidate.TruthStatusConfirmed
	TruthStatusVerified                  = candidate.TruthStatusVerified
	TruthStatusHypothesis                = candidate.TruthStatusHypothesis
	TruthStatusUnknown                   = candidate.TruthStatusUnknown
)

var ErrCandidateNotFound = errors.New("candidate not found")

// ErrConflict means that a mutation was based on a stale candidate version.
// Callers must reload and explicitly retry the command; the repository never
// silently overwrites a concurrent candidate mutation.
var ErrConflict = errors.New("candidate version conflict")

// CandidateRepository owns canonical Candidate persistence only. Mutation
// legality and semantic indexing remain outside this adapter.
type CandidateRepository struct {
	pool        *pgxpool.Pool
	db          postgresDBTX
	candidateID string
}

func NewCandidateRepository(pool *pgxpool.Pool) *CandidateRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &CandidateRepository{pool: pool, db: db}
}

func NewCandidateRepositoryForID(pool *pgxpool.Pool, candidateID string) *CandidateRepository {
	repository := NewCandidateRepository(pool)
	repository.candidateID = strings.TrimSpace(candidateID)
	return repository
}

func NewCandidateRepositoryForTx(tx pgx.Tx) *CandidateRepository {
	return &CandidateRepository{db: tx}
}

func NewCandidateRepositoryForTxID(tx pgx.Tx, candidateID string) *CandidateRepository {
	return &CandidateRepository{db: tx, candidateID: strings.TrimSpace(candidateID)}
}

func (r *CandidateRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres candidate repository is not configured")
	}
	return nil
}

func (r *CandidateRepository) CurrentCandidate(ctx context.Context) (Candidate, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return Candidate{}, err
	}
	if err := r.requireDB(); err != nil {
		return Candidate{}, err
	}
	var candidate Candidate
	var identity, profile, resumeFacts []byte
	var createdAt, updatedAt pgtype.Timestamptz
	var createdAtNS, updatedAtNS pgtype.Int8
	query := `SELECT id, version, created_at, updated_at, created_at_ns, updated_at_ns, identity, profile, resume_facts FROM candidates ORDER BY id LIMIT 1`
	args := []any{}
	if r.candidateID != "" {
		query = `SELECT id, version, created_at, updated_at, created_at_ns, updated_at_ns, identity, profile, resume_facts FROM candidates WHERE id=$1`
		args = append(args, r.candidateID)
	}
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&candidate.ID, &candidate.Version, &createdAt, &updatedAt, &createdAtNS, &updatedAtNS, &identity, &profile, &resumeFacts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrCandidateNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("load candidate root: %w", err)
	}
	candidate.CreatedAt, candidate.UpdatedAt = postgresTimeExact(createdAt, createdAtNS), postgresTimeExact(updatedAt, updatedAtNS)
	if err := decodeNullableJSON(identity, &candidate.Identity); err != nil {
		return Candidate{}, fmt.Errorf("decode candidate identity: %w", err)
	}
	if err := decodeNullableJSON(profile, &candidate.Profile); err != nil {
		return Candidate{}, fmt.Errorf("decode candidate profile: %w", err)
	}
	if len(resumeFacts) > 0 && string(resumeFacts) != "null" {
		candidate.ResumeFacts = &ResumeFacts{}
		if err := decodeNullableJSON(resumeFacts, candidate.ResumeFacts); err != nil {
			return Candidate{}, fmt.Errorf("decode candidate resume facts: %w", err)
		}
	}
	if err := r.loadCandidateChildren(ctx, &candidate); err != nil {
		return Candidate{}, err
	}
	sortCandidate(&candidate)
	if err := validateCanonicalCandidate(candidate); err != nil {
		return Candidate{}, fmt.Errorf("invalid canonical candidate state: %w", err)
	}
	return candidate, nil
}

func (r *CandidateRepository) PersistCandidate(ctx context.Context, candidate Candidate) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := validateCanonicalCandidate(candidate); err != nil {
		return fmt.Errorf("persist canonical candidate: %w", err)
	}
	if r.pool != nil {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin candidate transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := NewCandidateRepositoryForTxID(tx, r.candidateID).replaceCandidate(ctx, candidate); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit candidate transaction: %w", err)
		}
		return nil
	}
	return r.replaceCandidate(ctx, candidate)
}

// PersistCandidateIfVersion is the production mutation primitive. It is only
// intended to be called with a repository bound to CandidateTx. The row lock
// is acquired before replacing children, so concurrent writers either commit
// one complete aggregate or receive ErrConflict and roll back completely.
func (r *CandidateRepository) PersistCandidateIfVersion(ctx context.Context, candidate Candidate, expectedVersion int) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if expectedVersion <= 0 || candidate.Version != expectedVersion+1 {
		return fmt.Errorf("candidate version must advance from %d to %d: %w", expectedVersion, expectedVersion+1, ErrConflict)
	}
	if err := validateCanonicalCandidate(candidate); err != nil {
		return fmt.Errorf("persist canonical candidate mutation: %w", err)
	}
	var current int
	err := r.db.QueryRow(ctx, `SELECT version FROM candidates WHERE id=$1 FOR UPDATE`, candidate.ID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCandidateNotFound
	}
	if err != nil {
		return fmt.Errorf("lock candidate for mutation: %w", err)
	}
	if current != expectedVersion {
		return fmt.Errorf("candidate version is %d, expected %d: %w", current, expectedVersion, ErrConflict)
	}
	if err := r.replaceCandidate(ctx, candidate); err != nil {
		return err
	}
	return nil
}

// ImportCandidate is additive and is used by the explicit migration. An
// existing identical aggregate is a no-op; a different aggregate is a hard
// conflict and is never overwritten.
func (r *CandidateRepository) ImportCandidate(ctx context.Context, candidate Candidate) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := validateCanonicalCandidate(candidate); err != nil {
		return fmt.Errorf("import canonical candidate: %w", err)
	}
	if existing, err := r.CurrentCandidate(ctx); err == nil {
		if candidateEqual(existing, candidate) {
			return nil
		}
		return fmt.Errorf("candidate %q already exists with different canonical state: %w", candidate.ID, ErrRepositoryConflict)
	} else if !errors.Is(err, ErrCandidateNotFound) {
		return err
	}
	if r.pool != nil {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin candidate import: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := NewCandidateRepositoryForTxID(tx, r.candidateID).insertCandidate(ctx, candidate); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit candidate import: %w", err)
		}
		return nil
	}
	return r.insertCandidate(ctx, candidate)
}

func (r *CandidateRepository) replaceCandidate(ctx context.Context, candidate Candidate) error {
	// Event rows are never deleted. All other current-state rows are replaced
	// inside the same transaction, so a future writer can update a whole
	// knowledge snapshot without a JSON/SQL split-brain.
	for _, query := range []string{
		`DELETE FROM candidate_knowledge_sources WHERE candidate_id=$1`,
		`DELETE FROM candidate_evidence WHERE candidate_id=$1`,
		`DELETE FROM candidate_skill_uses WHERE candidate_id=$1`,
		`DELETE FROM candidate_skill_capabilities WHERE skill_id IN (SELECT id FROM candidate_skills WHERE candidate_id=$1)`,
		`DELETE FROM candidate_skills WHERE candidate_id=$1`,
		`DELETE FROM candidate_story_refs WHERE story_id IN (SELECT id FROM candidate_stories WHERE candidate_id=$1)`,
		`DELETE FROM candidate_contacts WHERE candidate_id=$1`,
		`DELETE FROM candidate_external_references WHERE candidate_id=$1`,
		`DELETE FROM candidate_education WHERE candidate_id=$1`,
		`DELETE FROM candidate_languages WHERE candidate_id=$1`,
		`DELETE FROM candidate_experiences WHERE candidate_id=$1`,
		`DELETE FROM candidate_achievements WHERE candidate_id=$1`,
		`DELETE FROM candidate_projects WHERE candidate_id=$1`,
		`DELETE FROM candidate_preferences WHERE candidate_id=$1`,
		`DELETE FROM candidate_constraints WHERE candidate_id=$1`,
		`DELETE FROM candidate_claims WHERE candidate_id=$1`,
		`DELETE FROM candidate_stories WHERE candidate_id=$1`,
		`DELETE FROM candidate_unknowns WHERE candidate_id=$1`,
		`DELETE FROM candidate_knowledge_proposals WHERE candidate_id=$1`,
	} {
		if _, err := r.db.Exec(ctx, query, candidate.ID); err != nil {
			return fmt.Errorf("replace candidate children: %w", err)
		}
	}
	identity, err := candidateJSONBytes(candidate.Identity)
	if err != nil {
		return err
	}
	profile, err := candidateJSONBytes(candidate.Profile)
	if err != nil {
		return err
	}
	var resumeFacts any
	if candidate.ResumeFacts != nil {
		resumeFacts, err = candidateJSONBytes(candidate.ResumeFacts)
		if err != nil {
			return err
		}
	}
	fingerprint, err := candidateFingerprint(candidate)
	if err != nil {
		return err
	}
	result, err := r.db.Exec(ctx, `UPDATE candidates SET version=$2,created_at=$3,updated_at=$4,created_at_ns=$5,updated_at_ns=$6,identity=$7,profile=$8,resume_facts=$9,source_fingerprint=$10 WHERE id=$1`, candidate.ID, candidate.Version, candidateNullableTime(candidate.CreatedAt), candidateNullableTime(candidate.UpdatedAt), nullableNanos(candidate.CreatedAt), nullableNanos(candidate.UpdatedAt), identity, profile, resumeFacts, fingerprint)
	if err != nil {
		return fmt.Errorf("replace candidate root: %w", err)
	}
	if result.RowsAffected() == 0 {
		if _, err := r.db.Exec(ctx, `INSERT INTO candidates(id,version,created_at,updated_at,created_at_ns,updated_at_ns,identity,profile,resume_facts,source_fingerprint) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, candidate.ID, candidate.Version, candidateNullableTime(candidate.CreatedAt), candidateNullableTime(candidate.UpdatedAt), nullableNanos(candidate.CreatedAt), nullableNanos(candidate.UpdatedAt), identity, profile, resumeFacts, fingerprint); err != nil {
			return mapCandidatePostgresError("insert candidate root", err)
		}
	}
	return r.insertCandidateChildrenWithoutRoot(ctx, candidate)
}

func (r *CandidateRepository) insertCandidate(ctx context.Context, candidate Candidate) error {
	identity, err := candidateJSONBytes(candidate.Identity)
	if err != nil {
		return err
	}
	profile, err := candidateJSONBytes(candidate.Profile)
	if err != nil {
		return err
	}
	var resumeFacts any
	if candidate.ResumeFacts != nil {
		resumeFacts, err = candidateJSONBytes(candidate.ResumeFacts)
		if err != nil {
			return err
		}
	}
	fingerprint, err := candidateFingerprint(candidate)
	if err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `INSERT INTO candidates(id, version, created_at, updated_at, created_at_ns, updated_at_ns, identity, profile, resume_facts, source_fingerprint) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, candidate.ID, candidate.Version, candidateNullableTime(candidate.CreatedAt), candidateNullableTime(candidate.UpdatedAt), nullableNanos(candidate.CreatedAt), nullableNanos(candidate.UpdatedAt), identity, profile, resumeFacts, fingerprint); err != nil {
		return mapCandidatePostgresError("insert candidate", err)
	}
	if err := r.insertCandidateChildrenRows(ctx, candidate); err != nil {
		return err
	}
	return nil
}

func (r *CandidateRepository) insertCandidateChildrenWithoutRoot(ctx context.Context, candidate Candidate) error {
	// The child writer is shared by initial import and transactional updates;
	// the root is handled separately so append-only events remain attached.
	return r.insertCandidateChildrenRows(ctx, candidate)
}

func (r *CandidateRepository) insertCandidateChildrenRows(ctx context.Context, c Candidate) error {
	insertedSkillUses := map[string]bool{}
	for _, value := range c.Contacts {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_contacts(id,candidate_id,kind,value,metadata) VALUES($1,$2,$3,$4,$5)`, value.ID, c.ID, value.Kind, value.Value, meta); err != nil {
			return fmt.Errorf("insert candidate contact %s: %w", value.ID, err)
		}
	}
	for _, value := range c.ExternalReferences {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_external_references(id,candidate_id,kind,url,metadata) VALUES($1,$2,$3,$4,$5)`, value.ID, c.ID, value.Kind, value.URL, meta); err != nil {
			return fmt.Errorf("insert candidate external reference %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Education {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_education(id,candidate_id,level,institution,specialty,details,claim_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, c.ID, value.Level, value.Institution, value.Specialty, value.Details, value.ClaimID, meta); err != nil {
			return fmt.Errorf("insert candidate education %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Languages {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_languages(id,candidate_id,name,level,claim_id,metadata) VALUES($1,$2,$3,$4,$5,$6)`, value.ID, c.ID, value.Name, value.Level, value.ClaimID, meta); err != nil {
			return fmt.Errorf("insert candidate language %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Experience {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		responsibilities, _ := candidateJSONBytes(value.Responsibilities)
		skillsUsed, _ := candidateJSONBytes(value.SkillsUsed)
		achievements, _ := candidateJSONBytes(value.Achievements)
		stories, _ := candidateJSONBytes(value.StoryIDs)
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_experiences(id,candidate_id,company,position,start_date,end_date,employment_type,description,responsibilities,skills_used,achievements,story_ids,claim_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, value.ID, c.ID, value.Company, value.Position, value.StartDate, value.EndDate, value.EmploymentType, value.Description, responsibilities, skillsUsed, achievements, stories, value.ClaimID, meta); err != nil {
			return fmt.Errorf("insert candidate experience %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Skills {
		cannotClaim, _ := candidateJSONBytes(value.CannotClaim)
		sourceIDs, _ := candidateJSONBytes(value.SourceIDs)
		claimIDs, _ := candidateJSONBytes(value.ClaimIDs)
		assertions, _ := candidateJSONBytes(value.SourceAssertions)
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_skills(id,candidate_id,canonical_name,display_name,level,category,last_used,cannot_claim,source_ids,claim_ids,negative,state,metadata,source_assertions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, value.ID, c.ID, value.Name, value.DisplayName, value.Level, value.Category, value.LastUsed, cannotClaim, sourceIDs, claimIDs, value.Negative, value.State, meta, assertions); err != nil {
			return mapCandidatePostgresError("insert candidate skill", err)
		}
		for _, capability := range value.Capabilities {
			if _, err = r.db.Exec(ctx, `INSERT INTO candidate_skill_capabilities(skill_id,capability_id,capability,claim_id) VALUES($1,$2,$3,$4)`, value.ID, capability.ID, capability.Text, capability.ClaimID); err != nil {
				return fmt.Errorf("insert skill capability %s: %w", capability.ID, err)
			}
		}
	}
	for _, value := range c.Projects {
		period, _ := candidateJSONBytes(value.Period)
		technologies, _ := candidateJSONBytes(value.Technologies)
		tasks, _ := candidateJSONBytes(value.Tasks)
		results, _ := candidateJSONBytes(value.Results)
		related, _ := candidateJSONBytes(value.RelatedSkills)
		sourceIDs, _ := candidateJSONBytes(value.SourceIDs)
		claimIDs, _ := candidateJSONBytes(value.ClaimIDs)
		uses, _ := candidateJSONBytes(value.SkillUses)
		achievementIDs, _ := candidateJSONBytes(value.AchievementIDs)
		storyIDs, _ := candidateJSONBytes(value.StoryIDs)
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		profileSource, _ := candidateJSONBytes(value.ProfileSource)
		detailedSource, _ := candidateJSONBytes(value.DetailedSource)
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_projects(id,candidate_id,name,type,role,period,description,technologies,tasks,results,related_skills,source_ids,claim_ids,skill_uses,achievement_ids,story_ids,metadata,profile_source,detailed_source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, value.ID, c.ID, value.Name, value.Type, value.Role, period, value.Description, technologies, tasks, results, related, sourceIDs, claimIDs, uses, achievementIDs, storyIDs, meta, profileSource, detailedSource); err != nil {
			return fmt.Errorf("insert candidate project %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Achievements {
		solution, _ := candidateJSONBytes(value.Solution)
		actions, _ := candidateJSONBytes(value.Actions)
		result, _ := candidateJSONBytes(value.Result)
		technologies, _ := candidateJSONBytes(value.Technologies)
		meta, err := candidateJSONBytes(value.KnowledgeMetadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_achievements(id,candidate_id,title,problem,solution,actions,result,technologies,project_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, value.ID, c.ID, value.Title, value.Problem, solution, actions, result, technologies, candidateNullableString(value.ProjectID), meta); err != nil {
			return fmt.Errorf("insert candidate achievement %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Preferences {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_preferences(id,candidate_id,kind,value,claim_id,metadata) VALUES($1,$2,$3,$4,$5,$6)`, value.ID, c.ID, value.Kind, value.Value, value.ClaimID, meta); err != nil {
			return fmt.Errorf("insert candidate preference %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Constraints {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_constraints(id,candidate_id,kind,value,claim_id,metadata) VALUES($1,$2,$3,$4,$5,$6)`, value.ID, c.ID, value.Kind, value.Value, value.ClaimID, meta); err != nil {
			return fmt.Errorf("insert candidate constraint %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Claims {
		meta, err := candidateJSONBytes(value.Metadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_claims(id,candidate_id,subject_type,subject_id,field,value,polarity,state,conflict_set_id,supersedes_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, c.ID, value.SubjectType, value.SubjectID, value.Field, value.Value, value.Polarity, value.State, value.ConflictSetID, value.SupersedesID, meta); err != nil {
			return mapCandidatePostgresError("insert candidate claim", err)
		}
	}
	for _, value := range c.Stories {
		payload, err := candidateJSONBytes(value)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_stories(id,candidate_id,title,payload) VALUES($1,$2,$3,$4)`, value.ID, c.ID, value.Title, payload); err != nil {
			return fmt.Errorf("insert candidate story %s: %w", value.ID, err)
		}
		for _, ref := range value.ProfileRefs {
			kind := "unknown"
			for _, exp := range c.Experience {
				if exp.ID == ref {
					kind = "experience"
				}
			}
			for _, project := range c.Projects {
				if project.ID == ref {
					kind = "project"
				}
			}
			if _, err = r.db.Exec(ctx, `INSERT INTO candidate_story_refs(story_id,entity_type,entity_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, value.ID, kind, ref); err != nil {
				return fmt.Errorf("insert story reference %s: %w", value.ID, err)
			}
		}
	}
	for _, value := range c.Experience {
		for _, storyID := range value.StoryIDs {
			if _, err := r.db.Exec(ctx, `INSERT INTO candidate_story_refs(story_id,entity_type,entity_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, storyID, "experience", value.ID); err != nil {
				return fmt.Errorf("insert experience story reference %s: %w", value.ID, err)
			}
		}
	}
	for _, value := range c.Projects {
		for _, storyID := range value.StoryIDs {
			if _, err := r.db.Exec(ctx, `INSERT INTO candidate_story_refs(story_id,entity_type,entity_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, storyID, "project", value.ID); err != nil {
				return fmt.Errorf("insert project story reference %s: %w", value.ID, err)
			}
		}
	}
	for _, value := range c.Unknowns {
		meta, err := candidateJSONBytes(value.KnowledgeMetadata)
		if err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `INSERT INTO candidate_unknowns(id,candidate_id,question,related_entity,hypothesis,status,gap_key,source,conversation_id,application_id,vacancy_id,employer_message_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, value.ID, c.ID, value.Question, value.RelatedEntity, value.Hypothesis, value.Status, value.GapKey, value.Source, value.ConversationID, value.ApplicationID, value.VacancyID, value.EmployerMessageID, meta); err != nil {
			return fmt.Errorf("insert candidate unknown %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Proposals {
		if _, err := r.db.Exec(ctx, `INSERT INTO candidate_knowledge_proposals(id,candidate_id,entity_type,entity_id,proposed_value,reason,source,confidence,status,created_at,created_at_ns,base_value,unknown_id,clarification_id,conversation_id,employer_message_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, value.ID, c.ID, value.EntityType, value.EntityID, candidateNullableJSON(value.ProposedValue), value.Reason, value.Source, value.Confidence, value.Status, value.CreatedAt, value.CreatedAt.UnixNano(), candidateNullableJSON(value.BaseValue), value.UnknownID, value.ClarificationID, value.ConversationID, value.EmployerMessageID); err != nil {
			return fmt.Errorf("insert candidate proposal %s: %w", value.ID, err)
		}
	}
	for _, value := range c.Events {
		if _, err := r.db.Exec(ctx, `INSERT INTO candidate_knowledge_events(id,candidate_id,timestamp,timestamp_ns,action,entity_type,entity_id,old_value,new_value,source,actor,unknown_id,clarification_id,proposal_id,conversation_id,employer_message_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT (id) DO NOTHING`, value.ID, c.ID, value.Timestamp, value.Timestamp.UnixNano(), value.Action, value.EntityType, value.EntityID, candidateNullableJSON(value.OldValue), candidateNullableJSON(value.NewValue), value.Source, value.Actor, value.UnknownID, value.ClarificationID, value.ProposalID, value.ConversationID, value.EmployerMessageID); err != nil {
			return mapCandidatePostgresError("insert candidate knowledge event", err)
		}
	}
	for _, value := range c.Skills {
		for _, use := range value.Uses {
			if insertedSkillUses[use.ID] {
				continue
			}
			if err := r.insertSkillUse(ctx, c.ID, use, value.ID); err != nil {
				return err
			}
			insertedSkillUses[use.ID] = true
		}
	}
	for _, value := range c.Projects {
		for _, use := range value.SkillUses {
			if insertedSkillUses[use.ID] {
				continue
			}
			if err := r.insertSkillUse(ctx, c.ID, use, use.SkillID); err != nil {
				return err
			}
			insertedSkillUses[use.ID] = true
		}
	}
	for _, value := range c.Experience {
		for _, use := range value.SkillsUsed {
			if insertedSkillUses[use.ID] {
				continue
			}
			if err := r.insertSkillUse(ctx, c.ID, use, use.SkillID); err != nil {
				return err
			}
			insertedSkillUses[use.ID] = true
		}
	}
	if err := r.insertMetadataRelations(ctx, c); err != nil {
		return err
	}
	return nil
}

func (r *CandidateRepository) insertSkillUse(ctx context.Context, candidateID string, value CanonicalSkillUse, fallbackSkillID string) error {
	skillID := value.SkillID
	if skillID == "" {
		skillID = fallbackSkillID
	}
	evidence, _ := candidateJSONBytes(value.Evidence)
	if _, err := r.db.Exec(ctx, `INSERT INTO candidate_skill_uses(id,candidate_id,skill_id,usage_context,experience_id,project_id,evidence,claim_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, candidateID, skillID, value.Context, candidateNullableString(value.ExperienceID), candidateNullableString(value.ProjectID), evidence, value.ClaimID); err != nil {
		return fmt.Errorf("insert skill use %s: %w", value.ID, err)
	}
	return nil
}

func (r *CandidateRepository) insertMetadataRelations(ctx context.Context, c Candidate) error {
	type owner struct {
		kind, id string
		meta     KnowledgeMetadata
	}
	owners := []owner{{"identity_full_name", c.ID + ":full_name", c.Identity.FullNameMetadata}, {"identity_location", c.ID + ":location", c.Identity.LocationMetadata}}
	for _, value := range c.Contacts {
		owners = append(owners, owner{"contact", value.ID, value.Metadata})
	}
	for _, value := range c.ExternalReferences {
		owners = append(owners, owner{"external_reference", value.ID, value.Metadata})
	}
	for _, value := range c.Education {
		owners = append(owners, owner{"education", value.ID, value.Metadata})
	}
	for _, value := range c.Languages {
		owners = append(owners, owner{"language", value.ID, value.Metadata})
	}
	for _, value := range c.Experience {
		owners = append(owners, owner{"experience", value.ID, value.Metadata})
	}
	for _, value := range c.Skills {
		owners = append(owners, owner{"skill", value.ID, value.Metadata})
		for _, assertion := range value.SourceAssertions {
			owners = append(owners, owner{"skill_assertion", assertion.ID, assertion.Metadata})
		}
	}
	for _, value := range c.Projects {
		owners = append(owners, owner{"project", value.ID, value.Metadata})
	}
	for _, value := range c.Achievements {
		owners = append(owners, owner{"achievement", value.ID, value.KnowledgeMetadata})
	}
	for _, value := range c.Preferences {
		owners = append(owners, owner{"preference", value.ID, value.Metadata})
	}
	for _, value := range c.Constraints {
		owners = append(owners, owner{"constraint", value.ID, value.Metadata})
	}
	for _, value := range c.Claims {
		owners = append(owners, owner{"claim", value.ID, value.Metadata})
	}
	for _, value := range c.Unknowns {
		owners = append(owners, owner{"unknown", value.ID, value.KnowledgeMetadata})
	}
	for _, value := range owners {
		for index, source := range value.meta.Sources {
			evidence, _ := candidateJSONBytes(source.Evidence)
			if _, err := r.db.Exec(ctx, `INSERT INTO candidate_knowledge_sources(candidate_id,owner_type,owner_id,source_index,source_type,reference,evidence,observed_at,observed_at_ns) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.ID, value.kind, value.id, index, source.Type, source.Reference, evidence, nullableTimePtr(source.ObservedAt), nullablePtrNanos(source.ObservedAt)); err != nil {
				return fmt.Errorf("insert candidate source %s/%s: %w", value.kind, value.id, err)
			}
		}
		for index, evidence := range value.meta.Evidence {
			if _, err := r.db.Exec(ctx, `INSERT INTO candidate_evidence(candidate_id,owner_type,owner_id,evidence_index,value) VALUES($1,$2,$3,$4,$5)`, c.ID, value.kind, value.id, index, evidence); err != nil {
				return fmt.Errorf("insert candidate evidence %s/%s: %w", value.kind, value.id, err)
			}
		}
	}
	return nil
}

func (r *CandidateRepository) loadCandidateChildren(ctx context.Context, c *Candidate) error {
	if err := loadRows(ctx, r.db, `SELECT id,kind,value,metadata FROM candidate_contacts WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateContact
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Kind, &v.Value, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Contacts = append(c.Contacts, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate contacts: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,kind,url,metadata FROM candidate_external_references WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalExternalReference
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Kind, &v.URL, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.ExternalReferences = append(c.ExternalReferences, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate external references: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,level,institution,specialty,details,claim_id,metadata FROM candidate_education WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateEducation
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Level, &v.Institution, &v.Specialty, &v.Details, &v.ClaimID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Education = append(c.Education, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate education: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,name,level,claim_id,metadata FROM candidate_languages WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateLanguage
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Level, &v.ClaimID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Languages = append(c.Languages, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate languages: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,company,position,start_date,end_date,employment_type,description,responsibilities,skills_used,achievements,story_ids,claim_id,metadata FROM candidate_experiences WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateExperience
		var responsibilities, skills, achievements, stories, meta []byte
		if err := rows.Scan(&v.ID, &v.Company, &v.Position, &v.StartDate, &v.EndDate, &v.EmploymentType, &v.Description, &responsibilities, &skills, &achievements, &stories, &v.ClaimID, &meta); err != nil {
			return err
		}
		for _, x := range []struct {
			raw    []byte
			target any
		}{{responsibilities, &v.Responsibilities}, {skills, &v.SkillsUsed}, {achievements, &v.Achievements}, {stories, &v.StoryIDs}, {meta, &v.Metadata}} {
			if err := decodeNullableJSON(x.raw, x.target); err != nil {
				return err
			}
		}
		c.Experience = append(c.Experience, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate experiences: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,canonical_name,display_name,level,category,last_used,cannot_claim,source_ids,claim_ids,negative,state,metadata,source_assertions FROM candidate_skills WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateSkill
		var cannot, sourceIDs, claimIDs, meta, assertions []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.DisplayName, &v.Level, &v.Category, &v.LastUsed, &cannot, &sourceIDs, &claimIDs, &v.Negative, &v.State, &meta, &assertions); err != nil {
			return err
		}
		for _, x := range []struct {
			raw    []byte
			target any
		}{{cannot, &v.CannotClaim}, {sourceIDs, &v.SourceIDs}, {claimIDs, &v.ClaimIDs}, {meta, &v.Metadata}, {assertions, &v.SourceAssertions}} {
			if err := decodeNullableJSON(x.raw, x.target); err != nil {
				return err
			}
		}
		c.Skills = append(c.Skills, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate skills: %w", err)
	}
	if err := loadSkillsNested(ctx, r.db, c.ID, c.Skills); err != nil {
		return fmt.Errorf("load candidate skill relations: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,name,type,role,period,description,technologies,tasks,results,related_skills,source_ids,claim_ids,skill_uses,achievement_ids,story_ids,metadata,profile_source,detailed_source FROM candidate_projects WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateProject
		var period, technologies, tasks, results, related, sourceIDs, claimIDs, uses, achievementIDs, storyIDs, meta, profileSource, detailedSource []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Type, &v.Role, &period, &v.Description, &technologies, &tasks, &results, &related, &sourceIDs, &claimIDs, &uses, &achievementIDs, &storyIDs, &meta, &profileSource, &detailedSource); err != nil {
			return err
		}
		for _, x := range []struct {
			raw    []byte
			target any
		}{{period, &v.Period}, {technologies, &v.Technologies}, {tasks, &v.Tasks}, {results, &v.Results}, {related, &v.RelatedSkills}, {sourceIDs, &v.SourceIDs}, {claimIDs, &v.ClaimIDs}, {uses, &v.SkillUses}, {achievementIDs, &v.AchievementIDs}, {storyIDs, &v.StoryIDs}, {meta, &v.Metadata}} {
			if err := decodeNullableJSON(x.raw, x.target); err != nil {
				return err
			}
		}
		if len(profileSource) > 0 && string(profileSource) != "null" {
			v.ProfileSource = &ProjectFact{}
			if err := decodeNullableJSON(profileSource, v.ProfileSource); err != nil {
				return err
			}
		}
		if len(detailedSource) > 0 && string(detailedSource) != "null" {
			v.DetailedSource = &CandidateProject{}
			if err := decodeNullableJSON(detailedSource, v.DetailedSource); err != nil {
				return err
			}
		}
		c.Projects = append(c.Projects, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate projects: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,title,problem,solution,actions,result,technologies,project_id,metadata FROM candidate_achievements WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CandidateAchievement
		var solution, actions, result, technologies, meta []byte
		var projectID *string
		if err := rows.Scan(&v.ID, &v.Title, &v.Problem, &solution, &actions, &result, &technologies, &projectID, &meta); err != nil {
			return err
		}
		if projectID != nil {
			v.ProjectID = *projectID
		}
		for _, x := range []struct {
			raw    []byte
			target any
		}{{solution, &v.Solution}, {actions, &v.Actions}, {result, &v.Result}, {technologies, &v.Technologies}, {meta, &v.KnowledgeMetadata}} {
			if err := decodeNullableJSON(x.raw, x.target); err != nil {
				return err
			}
		}
		c.Achievements = append(c.Achievements, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate achievements: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,kind,value,claim_id,metadata FROM candidate_preferences WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidatePreference
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Kind, &v.Value, &v.ClaimID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Preferences = append(c.Preferences, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate preferences: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,kind,value,claim_id,metadata FROM candidate_constraints WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateConstraint
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Kind, &v.Value, &v.ClaimID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Constraints = append(c.Constraints, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate constraints: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,subject_type,subject_id,field,value,polarity,state,conflict_set_id,supersedes_id,metadata FROM candidate_claims WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CanonicalCandidateClaim
		var raw []byte
		if err := rows.Scan(&v.ID, &v.SubjectType, &v.SubjectID, &v.Field, &v.Value, &v.Polarity, &v.State, &v.ConflictSetID, &v.SupersedesID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.Metadata); err != nil {
			return err
		}
		c.Claims = append(c.Claims, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate claims: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,payload FROM candidate_stories WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var v CanonicalCandidateStory
		if err := decodeNullableJSON(raw, &v); err != nil {
			return err
		}
		v.ID = id
		c.Stories = append(c.Stories, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate stories: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,question,related_entity,hypothesis,status,gap_key,source,conversation_id,application_id,vacancy_id,employer_message_id,metadata FROM candidate_unknowns WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CandidateUnknown
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Question, &v.RelatedEntity, &v.Hypothesis, &v.Status, &v.GapKey, &v.Source, &v.ConversationID, &v.ApplicationID, &v.VacancyID, &v.EmployerMessageID, &raw); err != nil {
			return err
		}
		if err := decodeNullableJSON(raw, &v.KnowledgeMetadata); err != nil {
			return err
		}
		c.Unknowns = append(c.Unknowns, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate unknowns: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,entity_type,entity_id,proposed_value,reason,source,confidence,status,created_at,created_at_ns,base_value,unknown_id,clarification_id,conversation_id,employer_message_id FROM candidate_knowledge_proposals WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v KnowledgeProposal
		var created pgtype.Timestamptz
		var ns pgtype.Int8
		var proposed, base []byte
		if err := rows.Scan(&v.ID, &v.EntityType, &v.EntityID, &proposed, &v.Reason, &v.Source, &v.Confidence, &v.Status, &created, &ns, &base, &v.UnknownID, &v.ClarificationID, &v.ConversationID, &v.EmployerMessageID); err != nil {
			return err
		}
		v.ProposedValue = append(json.RawMessage(nil), proposed...)
		v.BaseValue = append(json.RawMessage(nil), base...)
		v.CreatedAt = postgresTimeExact(created, ns)
		c.Proposals = append(c.Proposals, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate proposals: %w", err)
	}
	if err := loadRows(ctx, r.db, `SELECT id,timestamp,timestamp_ns,action,entity_type,entity_id,old_value,new_value,source,actor,unknown_id,clarification_id,proposal_id,conversation_id,employer_message_id FROM candidate_knowledge_events WHERE candidate_id=$1 ORDER BY id`, []any{c.ID}, func(rows pgx.Rows) error {
		var v CandidateKnowledgeEvent
		var ts pgtype.Timestamptz
		var ns pgtype.Int8
		var oldValue, newValue []byte
		if err := rows.Scan(&v.ID, &ts, &ns, &v.Action, &v.EntityType, &v.EntityID, &oldValue, &newValue, &v.Source, &v.Actor, &v.UnknownID, &v.ClarificationID, &v.ProposalID, &v.ConversationID, &v.EmployerMessageID); err != nil {
			return err
		}
		v.Timestamp = postgresTimeExact(ts, ns)
		v.OldValue = append(json.RawMessage(nil), oldValue...)
		v.NewValue = append(json.RawMessage(nil), newValue...)
		c.Events = append(c.Events, v)
		return nil
	}); err != nil {
		return fmt.Errorf("load candidate events: %w", err)
	}
	return nil
}

func loadSkillsNested(ctx context.Context, db postgresDBTX, candidateID string, skills []CanonicalCandidateSkill) error {
	byID := make(map[string]*CanonicalCandidateSkill, len(skills))
	for index := range skills {
		byID[skills[index].ID] = &skills[index]
	}
	rows, err := db.Query(ctx, `SELECT c.skill_id,c.capability_id,c.capability,c.claim_id FROM candidate_skill_capabilities c JOIN candidate_skills s ON s.id=c.skill_id WHERE s.candidate_id=$1 ORDER BY c.skill_id,c.capability_id`, candidateID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var skillID string
		var value CanonicalSkillCapability
		if err := rows.Scan(&skillID, &value.ID, &value.Text, &value.ClaimID); err != nil {
			rows.Close()
			return err
		}
		if skill := byID[skillID]; skill != nil {
			skill.Capabilities = append(skill.Capabilities, value)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = db.Query(ctx, `SELECT id,skill_id,usage_context,experience_id,project_id,evidence,claim_id FROM candidate_skill_uses WHERE candidate_id=$1 ORDER BY skill_id,id`, candidateID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var skillID string
		var value CanonicalSkillUse
		var experienceID, projectID *string
		var evidence []byte
		if err := rows.Scan(&value.ID, &skillID, &value.Context, &experienceID, &projectID, &evidence, &value.ClaimID); err != nil {
			rows.Close()
			return err
		}
		value.SkillID = skillID
		if experienceID != nil {
			value.ExperienceID = *experienceID
		}
		if projectID != nil {
			value.ProjectID = *projectID
		}
		if err := decodeNullableJSON(evidence, &value.Evidence); err != nil {
			rows.Close()
			return err
		}
		if skill := byID[skillID]; skill != nil {
			skill.Uses = append(skill.Uses, value)
		}
	}
	return rows.Err()
}

func loadRows(ctx context.Context, db postgresDBTX, query string, args []any, scan func(pgx.Rows) error) error {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func candidateJSONBytes(value any) ([]byte, error) { return json.Marshal(value) }
func candidateNullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return []byte(value)
}
func candidateNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func candidateNullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
func nullableNanos(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UnixNano()
}
func nullableTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}
func nullablePtrNanos(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UnixNano()
}

func validateCanonicalCandidate(c Candidate) error {
	if strings.TrimSpace(c.ID) == "" || c.Version <= 0 {
		return errors.New("candidate requires a stable id and positive version")
	}
	validateMeta := func(m KnowledgeMetadata) error {
		if m.TruthStatus != "" && !candidate.ValidTruthStatus(m.TruthStatus) {
			return fmt.Errorf("invalid truth_status %q", m.TruthStatus)
		}
		for _, s := range m.Sources {
			if !candidate.ValidKnowledgeSource(s.Type) {
				return fmt.Errorf("invalid knowledge source %q", s.Type)
			}
		}
		return nil
	}
	for _, v := range c.Contacts {
		if v.ID == "" || v.Kind == "" {
			return errors.New("contact requires id")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.ExternalReferences {
		if v.ID == "" || v.Kind == "" {
			return errors.New("external reference requires id and kind")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Education {
		if v.ID == "" {
			return errors.New("education requires id")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Languages {
		if v.ID == "" {
			return errors.New("language requires id")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Experience {
		if v.ID == "" {
			return errors.New("experience requires id")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	projects := map[string]bool{}
	for _, v := range c.Projects {
		if v.ID == "" {
			return errors.New("project requires id")
		}
		projects[v.ID] = true
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
		for _, u := range v.SkillUses {
			if u.ProjectID != "" && u.ProjectID != v.ID {
				return fmt.Errorf("project skill use %s has inconsistent project reference", u.ID)
			}
		}
	}
	experiences := map[string]bool{}
	for _, v := range c.Experience {
		experiences[v.ID] = true
	}
	skills := map[string]bool{}
	for _, v := range c.Skills {
		if v.ID == "" || v.Name == "" {
			return errors.New("skill requires id and name")
		}
		if !candidate.ValidSkillLevel(v.Level) {
			return fmt.Errorf("invalid skill level %q", v.Level)
		}
		if v.State != CanonicalClaimActive && v.State != CanonicalClaimDisputed && v.State != CanonicalClaimSuperseded {
			return fmt.Errorf("invalid skill state %q", v.State)
		}
		skills[v.ID] = true
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
		for _, u := range append([]CanonicalSkillUse{}, v.Uses...) {
			if u.SkillID != "" && u.SkillID != v.ID {
				return fmt.Errorf("skill use %s has inconsistent skill reference", u.ID)
			}
		}
	}
	for _, v := range c.Experience {
		for _, use := range v.SkillsUsed {
			if use.SkillID != "" && !skills[use.SkillID] {
				return fmt.Errorf("experience skill use %s references missing skill %s", use.ID, use.SkillID)
			}
			if use.ProjectID != "" && !projects[use.ProjectID] {
				return fmt.Errorf("experience skill use %s references missing project %s", use.ID, use.ProjectID)
			}
			if use.ExperienceID != "" && use.ExperienceID != v.ID {
				return fmt.Errorf("experience skill use %s has inconsistent experience reference", use.ID)
			}
			if !validSkillUsageContext(use.Context) {
				return fmt.Errorf("invalid skill usage context %q", use.Context)
			}
		}
	}
	for _, v := range c.Projects {
		for _, use := range v.SkillUses {
			if use.SkillID != "" && !skills[use.SkillID] {
				return fmt.Errorf("project skill use %s references missing skill %s", use.ID, use.SkillID)
			}
			if use.ExperienceID != "" && !experiences[use.ExperienceID] {
				return fmt.Errorf("project skill use %s references missing experience %s", use.ID, use.ExperienceID)
			}
			if use.ProjectID != "" && use.ProjectID != v.ID {
				return fmt.Errorf("project skill use %s has inconsistent project reference", use.ID)
			}
			if !validSkillUsageContext(use.Context) {
				return fmt.Errorf("invalid skill usage context %q", use.Context)
			}
		}
	}
	for _, v := range c.Skills {
		for _, u := range v.Uses {
			if u.ProjectID != "" && !projects[u.ProjectID] {
				return fmt.Errorf("skill use %s references missing project %s", u.ID, u.ProjectID)
			}
			if u.ExperienceID != "" && !experiences[u.ExperienceID] {
				return fmt.Errorf("skill use %s references missing experience %s", u.ID, u.ExperienceID)
			}
			if !validSkillUsageContext(u.Context) {
				return fmt.Errorf("invalid skill usage context %q", u.Context)
			}
		}
	}
	for _, v := range c.Claims {
		if v.ID == "" || v.SubjectType == "" || v.SubjectID == "" {
			return errors.New("claim requires identity")
		}
		if v.Polarity != CanonicalClaimPositive && v.Polarity != CanonicalClaimNegative {
			return fmt.Errorf("invalid claim polarity %q", v.Polarity)
		}
		if v.State != CanonicalClaimActive && v.State != CanonicalClaimDisputed && v.State != CanonicalClaimSuperseded {
			return fmt.Errorf("invalid claim state %q", v.State)
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Achievements {
		if v.ID == "" || v.Title == "" {
			return errors.New("achievement requires id and title")
		}
		if v.ProjectID != "" && !projects[v.ProjectID] {
			return fmt.Errorf("achievement %s references missing project %s", v.ID, v.ProjectID)
		}
		if err := validateMeta(v.KnowledgeMetadata); err != nil {
			return err
		}
	}
	for _, v := range c.Preferences {
		if v.ID == "" || v.Kind == "" {
			return errors.New("preference requires id and kind")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Constraints {
		if v.ID == "" || v.Kind == "" {
			return errors.New("constraint requires id and kind")
		}
		if err := validateMeta(v.Metadata); err != nil {
			return err
		}
	}
	for _, v := range c.Unknowns {
		if v.ID == "" || v.Question == "" {
			return errors.New("unknown requires id and question")
		}
		switch v.Status {
		case CandidateUnknownNeedsConfirmation, CandidateUnknownConfirmed, CandidateUnknownRejected, CandidateUnknownDismissed, CandidateUnknownSuperseded:
		default:
			return fmt.Errorf("invalid unknown status %q", v.Status)
		}
		if err := validateMeta(v.KnowledgeMetadata); err != nil {
			return err
		}
	}
	for _, v := range c.Events {
		if v.ID == "" || v.Timestamp.IsZero() {
			return errors.New("event requires stable id and timestamp")
		}
		if !candidate.ValidKnowledgeSource(v.Source) {
			return fmt.Errorf("invalid event source %q", v.Source)
		}
	}
	for _, v := range c.Proposals {
		if v.ID == "" || v.EntityID == "" {
			return errors.New("proposal requires stable identity")
		}
		switch v.Status {
		case KnowledgeProposalPending, KnowledgeProposalConfirmed, KnowledgeProposalRejected:
		default:
			return fmt.Errorf("invalid proposal status %q", v.Status)
		}
	}
	for _, v := range c.Stories {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validSkillUsageContext(value CanonicalSkillUsageContext) bool {
	switch value {
	case CanonicalSkillUsageCommercial, CanonicalSkillUsagePetProject, CanonicalSkillUsageEducational, CanonicalSkillUsagePersonal, CanonicalSkillUsageStudiedOnly, CanonicalSkillUsageUnknown, CanonicalSkillUsageExplicitlyNotUsed:
		return true
	default:
		return false
	}
}

func mapCandidatePostgresError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" || pgErr.Code == "23503" || pgErr.Code == "23514" {
			return fmt.Errorf("%s: %w (%s)", action, ErrRepositoryConflict, pgErr.ConstraintName)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func candidateFingerprint(candidate Candidate) (string, error) {
	raw, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateCandidate preserves the canonical persistence precondition for
// migration callers without exporting the adapter's internal validation steps.
func ValidateCandidate(value Candidate) error { return validateCanonicalCandidate(value) }

// CandidateFingerprint is the stable fingerprint used by import/migration
// idempotency checks.
func CandidateFingerprint(value Candidate) (string, error) { return candidateFingerprint(value) }

func candidateEqual(left, right Candidate) bool {
	return candidateparity.Equivalent(left, right)
}

func sortCandidate(value *Candidate) { candidate.SortCanonicalCandidate(value) }

var _ ports.CandidateReader = (*CandidateRepository)(nil)
var _ ports.CandidateWriter = (*CandidateRepository)(nil)
var _ ports.CandidateMutationWriter = (*CandidateRepository)(nil)
