package jsonstorage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

func storageMetadata(at time.Time) candidate.KnowledgeMetadata {
	return candidate.KnowledgeMetadata{
		TruthStatus: candidate.TruthStatusUnknown,
		Sources:     []candidate.KnowledgeSourceRecord{{Type: candidate.KnowledgeSourceUnknown}},
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}

func storageCollections(at time.Time) KnowledgeCollections {
	return KnowledgeCollections{
		Skills:       []candidate.CandidateSkillDetailed{{ID: "skill-1", Name: "Go", Level: candidate.SkillLevelUnknown, KnowledgeMetadata: storageMetadata(at)}},
		Projects:     []candidate.CandidateProject{{ID: "project-1", Name: "Synthetic project", KnowledgeMetadata: storageMetadata(at)}},
		Achievements: []candidate.CandidateAchievement{{ID: "achievement-1", Title: "Synthetic achievement", KnowledgeMetadata: storageMetadata(at)}},
		Unknowns:     []candidate.CandidateUnknown{{ID: "unknown-1", Question: "Unknown question", Status: candidate.CandidateUnknownNeedsConfirmation, KnowledgeMetadata: storageMetadata(at)}},
		Proposals:    []candidate.KnowledgeProposal{{ID: "proposal-1", EntityType: "skill", EntityID: "skill-1", ProposedValue: json.RawMessage(`{"id":"skill-1"}`), Reason: "synthetic", Source: candidate.KnowledgeSourceDerived, Status: candidate.KnowledgeProposalPending, CreatedAt: at, BaseValue: json.RawMessage("null")}},
		Events:       []candidate.CandidateKnowledgeEvent{{ID: "event-1", Timestamp: at, Action: "add", EntityType: "skill", EntityID: "skill-1", OldValue: json.RawMessage("null"), NewValue: json.RawMessage("null"), Source: candidate.KnowledgeSourceUnknown, Actor: "test"}},
	}
}

func TestCandidateKnowledgeStoreRoundTripAndPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	store := NewCandidateKnowledgeStore(filepath.Join(dir, "candidate_profile.json"))
	want := storageCollections(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	// json.Indent normalizes embedded RawMessage whitespace while preserving
	// its JSON value; compare that value after the normalization.
	want.Proposals[0].ProposedValue = got.Proposals[0].ProposedValue
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("knowledge round trip changed values: %#v != %#v", got, want)
	}
	if mode := fileMode(t, dir); mode.Perm() != 0o700 {
		t.Fatalf("private directory mode = %o, want 700", mode.Perm())
	}
	for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "proposals", "events"} {
		if mode := fileMode(t, filepath.Join(dir, "candidate_"+kind+".json")); mode.Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", kind, mode.Perm())
		}
	}
}

func TestCandidateKnowledgeStoreCorruptJSONFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "candidate_skills.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCandidateKnowledgeStore(filepath.Join(dir, "candidate_profile.json")).Load(); err == nil || !strings.Contains(err.Error(), "decode knowledge skills") {
		t.Fatalf("corrupt JSON was accepted: %v", err)
	}
}

func TestCandidateProfileStoreRoundTripStrictAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "candidate_profile.json")
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	profile := candidate.NewProfile(at)
	profile.Identity.FullName.Value = "Synthetic Candidate"
	if err := NewCandidateProfileStore(path).Save(profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewCandidateProfileStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity.FullName.Value != profile.Identity.FullName.Value || loaded.Version != 1 {
		t.Fatalf("profile round trip changed values: got=%+v want=%+v", loaded, profile)
	}
	if mode := fileMode(t, dir); mode.Perm() != 0o700 {
		t.Fatalf("profile directory mode = %o, want 700", mode.Perm())
	}
	if mode := fileMode(t, path); mode.Perm() != 0o600 {
		t.Fatalf("profile mode = %o, want 600", mode.Perm())
	}

	if err := os.WriteFile(path, []byte(`{"version":1,"updated_at":"2026-09-08T12:00:00Z","identity":{},"work_preferences":{},"employer_communication_preferences":{},"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCandidateProfileStore(path).Load(); err == nil {
		t.Fatal("unknown profile field was accepted")
	}
}

func TestCandidateStoryStoreRoundTripMalformedAndMissing(t *testing.T) {
	dir := t.TempDir()
	missing, err := NewCandidateStoryStore(filepath.Join(dir, "missing.json")).Load()
	if err != nil || missing.Version != 1 || len(missing.Stories) != 0 {
		t.Fatalf("missing stories semantics changed: %+v %v", missing, err)
	}
	path := filepath.Join(dir, "candidate_stories.json")
	if err := os.WriteFile(path, []byte(`[{"id":"story-1","title":"Synthetic","action":"Did work"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewCandidateStoryStore(path).Load()
	if err != nil || len(loaded.Stories) != 1 || loaded.Stories[0].ID != "story-1" {
		t.Fatalf("story round trip failed: %+v %v", loaded, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCandidateStoryStore(path).Load(); err == nil {
		t.Fatal("malformed stories were accepted")
	}
}

func TestCandidateRepositoryAssemblesCanonicalSnapshotFromJSON(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "candidate_profile.json")
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	profile := candidate.NewProfile(at)
	profile.TotalExperienceMonths = candidate.ProfileIntFact{Value: 11, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceHHResume, Confirmed: true, ConfirmedAt: at, Evidence: []string{"structured HH value"}}}
	profile.Skills = []candidate.CandidateSkill{{Name: "Kubernetes", Level: candidate.SkillLevelUnknown, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUnknown}}}
	profile.WorkPreferences.Relocation = candidate.ProfileStringFact{Value: "не готов", ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: at, Evidence: []string{"explicit preference"}}}
	profile.EmployerCommunicationPreferences.Salary = candidate.ProfileStringFact{Value: "от 120000", ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: at, Evidence: []string{"explicit preference"}}}
	if err := NewCandidateProfileStore(profilePath).Save(profile); err != nil {
		t.Fatal(err)
	}
	storiesPath := filepath.Join(dir, "candidate_stories.json")
	if err := os.WriteFile(storiesPath, []byte(`[{"id":"story-1","title":"Synthetic story","summary":"Narrative"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := NewCandidateRepository(CandidateRepositoryConfig{ProfilePath: profilePath, StoriesPath: storiesPath, CandidateID: "candidate-json"})
	value, err := repo.CurrentCandidate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "candidate-json" || value.Profile.TotalExperienceMonths.Value != 11 || value.Profile.WorkPreferences.Relocation.Value != "не готов" || value.Profile.Communication.Salary.Value != "от 120000" || len(value.Stories) != 1 {
		t.Fatalf("canonical JSON snapshot lost source data: %+v", value)
	}
	if len(value.Skills) != 1 || value.Skills[0].Name != "kubernetes" || value.Skills[0].Metadata.TruthStatus != candidate.TruthStatusUnknown || value.Skills[0].Negative {
		t.Fatalf("unknown fact was promoted: %+v", value.Skills)
	}
}

func TestCandidateRepositoryReturnsDetachedSnapshots(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "candidate_profile.json")
	if err := NewCandidateProfileStore(profilePath).Save(candidate.NewProfile(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	store := NewCandidateKnowledgeStore(profilePath)
	collections := storageCollections(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	collections.Proposals = nil
	if err := store.Save(collections); err != nil {
		t.Fatal(err)
	}
	repo := NewCandidateRepository(CandidateRepositoryConfig{ProfilePath: profilePath, CandidateID: "detached"})
	first, err := repo.CurrentCandidate(nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Skills[0].SourceAssertions[0].Name = "mutated"
	second, err := repo.CurrentCandidate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Skills[0].SourceAssertions[0].Name == "mutated" {
		t.Fatal("candidate repository leaked mutable storage state")
	}
}

func TestCandidateRepositoryParallelReads(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "candidate_profile.json")
	if err := NewCandidateProfileStore(profilePath).Save(candidate.NewProfile(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	repo := NewCandidateRepository(CandidateRepositoryConfig{ProfilePath: profilePath, CandidateID: "parallel"})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := repo.CurrentCandidate(nil)
			if err != nil {
				errs <- err
				return
			}
			if value.ID != "parallel" {
				errs <- fmt.Errorf("candidate id = %q", value.ID)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestCandidateKnowledgeStoreValidationFailureLeavesExistingFilesIntact(t *testing.T) {
	dir := t.TempDir()
	store := NewCandidateKnowledgeStore(filepath.Join(dir, "candidate_profile.json"))
	want := storageCollections(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "candidate_skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := want
	invalid.Skills = []candidate.CandidateSkillDetailed{{ID: "skill-2", Name: "Python", Level: candidate.SkillLevelUnknown}}
	if err := store.Save(invalid); err == nil {
		t.Fatal("invalid collection was accepted")
	}
	after, err := os.ReadFile(filepath.Join(dir, "candidate_skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("validation failure changed an existing collection")
	}
	tmp, err := filepath.Glob(filepath.Join(dir, ".candidate_*.tmp"))
	if err != nil || len(tmp) != 0 {
		t.Fatalf("staged temporary files leaked: %v %v", tmp, err)
	}
}

func TestCandidateKnowledgeStoreRenameFailureKeepsHistoricalPartialSaveSemantics(t *testing.T) {
	dir := t.TempDir()
	store := NewCandidateKnowledgeStore(filepath.Join(dir, "candidate_profile.json"))
	want := storageCollections(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "candidate_projects.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "candidate_projects.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	updated := storageCollections(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	updated.Skills[0].Name = "Changed"
	if err := store.Save(updated); err == nil {
		t.Fatal("rename failure was not reported")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "candidate_skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Changed") {
		t.Fatal("expected the historical partial rename behavior to remain observable")
	}
	tmp, err := filepath.Glob(filepath.Join(dir, ".candidate_*.tmp"))
	if err != nil || len(tmp) != 0 {
		t.Fatalf("staged temporary files leaked after rename failure: %v %v", tmp, err)
	}
}

func TestCandidateClarificationStoreRoundTripDuplicateAndEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_clarifications.json")
	store := NewCandidateClarificationStore(path)
	request, err := store.Create(candidateacquisition.CandidateClarificationRequest{ID: "clarification-1", Topic: "Redis", Question: "Used Redis?", Reason: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(request); err == nil || err.Error() != "clarification id already exists" {
		t.Fatalf("duplicate clarification was accepted: %v", err)
	}
	answer := candidateacquisition.CandidateAnswer{Kind: "free_text", Raw: "Used in a pet project", ReceivedAt: time.Now().UTC()}
	if err := store.RecordAnswerEvidence(request.ID, answer); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := NewCandidateClarificationStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	values, err := loaded.List()
	if err != nil || len(values) != 1 || values[0].Answer == nil || values[0].Answer.Raw != answer.Raw || values[0].Status != candidateacquisition.ClarificationPending {
		t.Fatalf("clarification evidence round trip failed: %#v %v", values, err)
	}
	if mode := fileMode(t, path); mode.Perm() != 0o600 {
		t.Fatalf("clarification mode = %o, want 600", mode.Perm())
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
