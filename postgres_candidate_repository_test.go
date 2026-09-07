package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresCandidateRepositoryContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 9, 1, 10, 11, 12, 123456789, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Identity.FullName = ProfileStringFact{Value: "Canonical Candidate", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}
	profile.Identity.Location = ProfileStringFact{Value: "Екатеринбург", ProfileFact: ProfileFact{Source: CandidateSourceUnknown}}
	profile.Skills = []CandidateSkill{{Name: "Python", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}}
	profile.Projects = []ProjectFact{{Name: "API project", Description: "integration", Technologies: []string{"Python"}, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}}
	profile.UnknownPendingFacts = []PendingProfileQuestion{{Topic: "Redis", Question: "Used Redis?", Reason: "not established", CreatedAt: at}}
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	profile.WorkPreferences.WorkMode = ProfileStringFact{Value: "remote-" + suffix, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}
	profile.EmployerCommunicationPreferences.AvoidClaiming = ProfileListFact{Values: []string{"Kubernetes-" + suffix}, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}
	profile.Skills[0].Name = "UniqueSkill-" + fmt.Sprintf("%x", time.Now().UnixNano())
	profile.Projects[0].Name += "-" + suffix
	profile.UnknownPendingFacts[0].Question += " " + suffix
	input := CanonicalCandidateInput{Profile: profile, CandidateID: "candidate-contract-" + suffix, Contacts: "candidate-" + suffix + "@example.test", GitHubURL: "https://github.com/example/" + suffix}
	want, diagnostics, err := BuildCanonicalCandidate(input)
	if err != nil || len(diagnostics.Conflicts) != 0 {
		t.Fatalf("build canonical candidate: candidate=%+v diagnostics=%+v err=%v", want, diagnostics, err)
	}
	t.Cleanup(func() {
		cleanupStatements := []string{
			`DELETE FROM candidate_knowledge_sources WHERE candidate_id=$1`,
			`DELETE FROM candidate_evidence WHERE candidate_id=$1`,
			`DELETE FROM candidate_skill_uses WHERE candidate_id=$1`,
			`DELETE FROM candidate_skill_capabilities WHERE skill_id IN (SELECT id FROM candidate_skills WHERE candidate_id=$1)`,
			`DELETE FROM candidate_skills WHERE candidate_id=$1`,
			`DELETE FROM candidate_story_refs WHERE story_id IN (SELECT id FROM candidate_stories WHERE candidate_id=$1)`,
			`DELETE FROM candidate_contacts WHERE candidate_id=$1`, `DELETE FROM candidate_external_references WHERE candidate_id=$1`,
			`DELETE FROM candidate_education WHERE candidate_id=$1`, `DELETE FROM candidate_languages WHERE candidate_id=$1`,
			`DELETE FROM candidate_experiences WHERE candidate_id=$1`, `DELETE FROM candidate_achievements WHERE candidate_id=$1`,
			`DELETE FROM candidate_projects WHERE candidate_id=$1`, `DELETE FROM candidate_preferences WHERE candidate_id=$1`,
			`DELETE FROM candidate_constraints WHERE candidate_id=$1`, `DELETE FROM candidate_claims WHERE candidate_id=$1`,
			`DELETE FROM candidate_stories WHERE candidate_id=$1`, `DELETE FROM candidate_unknowns WHERE candidate_id=$1`,
			`DELETE FROM candidate_knowledge_proposals WHERE candidate_id=$1`, `DELETE FROM candidate_knowledge_events WHERE candidate_id=$1`,
			`DELETE FROM candidates WHERE id=$1`,
		}
		for _, statement := range cleanupStatements {
			_, _ = pool.Exec(ctx, statement, want.ID)
		}
	})
	projectID := want.Projects[0].ID
	skillID := want.Skills[0].ID
	experienceID := "experience-" + suffix
	use := CanonicalSkillUse{ID: "skill-use-" + suffix, SkillID: skillID, ProjectID: projectID, ExperienceID: experienceID, Context: CanonicalSkillUsageCommercial, Evidence: []string{"fixture evidence"}, ClaimID: "claim-use-" + suffix}
	experienceMeta := knowledgeTestMetadata(KnowledgeSourceHHResume, TruthStatusVerified)
	want.Experience = append(want.Experience, CanonicalCandidateExperience{ID: experienceID, Company: "Example", Position: "Integrator", StartDate: "2024-01", SkillsUsed: []CanonicalSkillUse{use}, Metadata: experienceMeta})
	want.Skills[0].Uses = append(want.Skills[0].Uses, use)
	want.Projects[0].SkillUses = append(want.Projects[0].SkillUses, use)
	want.Projects[0].StoryIDs = []string{"story-" + suffix}
	want.Achievements = append(want.Achievements, CandidateAchievement{ID: "achievement-" + suffix, Title: "Reduced manual work", ProjectID: projectID, Result: []string{"less manual work"}, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)})
	want.Stories = append(want.Stories, CanonicalCandidateStory{ID: "story-" + suffix, Title: "API integration", Summary: "A real fixture story", ProfileRefs: []string{projectID}})
	want.Claims = append(want.Claims, CanonicalCandidateClaim{ID: "claim-negative-" + suffix, SubjectType: "skill", SubjectID: skillID, Field: "cannot_claim", Value: `{"value":"senior scale"}`, Polarity: CanonicalClaimNegative, State: CanonicalClaimDisputed, ConflictSetID: "conflict-" + suffix, SupersedesID: "claim-old-" + suffix, Metadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)})
	want.Proposals = append(want.Proposals, KnowledgeProposal{ID: "proposal-" + suffix, EntityType: "skill", EntityID: skillID, ProposedValue: []byte(`{"level":"advanced"}`), Reason: "fixture proposal", Source: KnowledgeSourceDerived, Status: KnowledgeProposalPending, CreatedAt: at})
	want.Events = append(want.Events, CandidateKnowledgeEvent{ID: "event-" + suffix, Timestamp: at.Add(time.Minute), Action: "fixture", EntityType: "skill", EntityID: skillID, OldValue: []byte(`{"level":"working"}`), NewValue: []byte(`{"level":"advanced"}`), Source: KnowledgeSourceUserConfirmed, Actor: "fixture"})
	sortCanonicalCandidate(&want)
	t.Logf("candidate id=%s skill ids=%v", want.ID, func() []string {
		values := []string{}
		for _, skill := range want.Skills {
			values = append(values, skill.ID)
		}
		return values
	}())
	// The opt-in contract uses a unique ID so it does not delete another
	// developer's candidate from a shared test database.
	repo := NewPostgresCandidateRepositoryForID(pool, want.ID)
	if err := repo.ImportCandidate(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.CurrentCandidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectCandidateEqual(got, want) {
		t.Fatalf("canonical candidate changed on PostgreSQL round-trip: got=%+v want=%+v", got, want)
	}
	if err := repo.ImportCandidate(ctx, want); err != nil {
		t.Fatalf("identical import was not idempotent: %v", err)
	}
	if err := NewPostgresCandidateStoreForID(pool, want.ID).WithTx(ctx, func(tx CandidateTx) error {
		changed := want
		changed.Identity.Location = "transactional update"
		return tx.Candidate().PersistCandidate(ctx, changed)
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.CurrentCandidate(ctx)
	if err != nil || changed.Identity.Location != "transactional update" {
		t.Fatalf("transactional candidate update was not committed: candidate=%+v err=%v", changed, err)
	}
	beforeRollback := changed
	rollbackErr := NewPostgresCandidateStoreForID(pool, want.ID).WithTx(ctx, func(tx CandidateTx) error {
		rolledBack := beforeRollback
		rolledBack.Identity.Location = "must rollback"
		if err := tx.Candidate().PersistCandidate(ctx, rolledBack); err != nil {
			return err
		}
		return errors.New("intentional candidate rollback")
	})
	if rollbackErr == nil {
		t.Fatal("candidate transaction rollback error was ignored")
	}
	afterRollback, err := repo.CurrentCandidate(ctx)
	if err != nil || !reflectCandidateEqual(afterRollback, beforeRollback) {
		t.Fatalf("candidate state changed after rollback: candidate=%+v err=%v", afterRollback, err)
	}
}
