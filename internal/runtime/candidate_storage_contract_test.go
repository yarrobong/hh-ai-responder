package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// These are characterization tests for the current file-backed contract.
// In particular, they intentionally do not claim that CandidateKnowledgeBase
// saves all files in one transaction, that CandidateProfile uses the core
// store lock, or that raw HH responses / exact LLM prompts are persisted.

func TestCandidateProfileStorageContract_RestartPreservesFactsAndUnknowns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate_profile.json")
	confirmedAt := time.Date(2026, 9, 2, 8, 30, 0, 0, time.UTC)
	questionAt := time.Date(2026, 9, 3, 9, 45, 0, 0, time.UTC)
	beforeSave := time.Now().UTC()
	profile := NewCandidateProfile(confirmedAt)
	profile.Identity = CandidateIdentity{
		FullName: ProfileStringFact{Value: "Fixture Candidate", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, confirmedAt)},
		Location: ProfileStringFact{Value: "Екатеринбург", ProfileFact: ProfileFact{Source: CandidateSourceUnknown}},
	}
	profile.Skills = []CandidateSkill{
		{Name: "Python", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, confirmedAt)},
		{Name: "Kubernetes", Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceDerived, Evidence: []string{"unverified fixture hypothesis"}}},
	}
	profile.WorkPreferences = WorkPreferences{
		PrimaryRoles:  ProfileStringFact{Value: "Backend developer", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, confirmedAt)},
		SalaryMinimum: ProfileIntFact{Value: 150000, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, confirmedAt)},
	}
	profile.UnknownPendingFacts = []PendingProfileQuestion{{
		Topic: "Kubernetes", Question: "Есть ли практический опыт Kubernetes?", Category: "skill",
		Reason: "vacancy requirement is not confirmed", CreatedAt: questionAt,
	}}

	if err := SaveCandidateProfile(path, profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCandidateProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || loaded.UpdatedAt.Before(beforeSave) || loaded.UpdatedAt.IsZero() {
		t.Fatalf("profile storage timestamp/version contract changed: %+v", loaded)
	}
	if loaded.Identity.FullName.Source != CandidateSourceUserConfirmed || !loaded.Identity.FullName.Confirmed ||
		!loaded.Identity.FullName.ConfirmedAt.Equal(confirmedAt) ||
		!reflect.DeepEqual(loaded.Identity.FullName.Evidence, []string{"test evidence"}) {
		t.Fatalf("profile provenance was not persisted: %+v", loaded.Identity.FullName)
	}
	if loaded.Identity.Location.Source != CandidateSourceUnknown || loaded.Identity.Location.Confirmed {
		t.Fatalf("unknown profile fact changed meaning: %+v", loaded.Identity.Location)
	}
	if len(loaded.UnknownPendingFacts) != 1 || !loaded.UnknownPendingFacts[0].CreatedAt.Equal(questionAt) ||
		loaded.UnknownPendingFacts[0].Reason == "" {
		t.Fatalf("pending unknown was not persisted: %+v", loaded.UnknownPendingFacts)
	}

	// Restart must produce equivalent domain state, apart from the documented
	// Save-time UpdatedAt refresh of the legacy profile envelope.
	restarted, err := LoadCandidateProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.UpdatedAt.Equal(loaded.UpdatedAt) || !reflect.DeepEqual(restarted.Skills, loaded.Skills) ||
		!reflect.DeepEqual(restarted.UnknownPendingFacts, loaded.UnknownPendingFacts) ||
		!reflect.DeepEqual(restarted.WorkPreferences, loaded.WorkPreferences) {
		t.Fatal("profile restart changed domain state")
	}
}

func TestCandidateProfileStorageContract_MissingAndUnknownSchemaAreFailClosed(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-profile.json")
	profile, err := LoadCandidateProfile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Version != 1 || profile.UpdatedAt.IsZero() {
		t.Fatalf("missing profile did not return the current empty default: %+v", profile)
	}

	unknownVersion := filepath.Join(dir, "unknown-version.json")
	raw := `{"version":2,"updated_at":"2026-09-01T00:00:00Z","identity":{},"work_preferences":{},"employer_communication_preferences":{}}`
	if err := os.WriteFile(unknownVersion, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCandidateProfile(unknownVersion); err == nil {
		t.Fatal("unknown profile schema version was accepted")
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCandidateProfile(corrupt); err == nil {
		t.Fatal("corrupt profile was accepted")
	}
}

func TestCandidateKnowledgeStorageContract_RestartPreservesMetadataProposalsUnknownsAndEvents(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "candidate_profile.json")
	profile := NewCandidateProfile(time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC))
	profile.Skills = []CandidateSkill{{Name: "Python", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC))}}
	if err := SaveCandidateProfile(profilePath, profile); err != nil {
		t.Fatal(err)
	}

	kb := NewCandidateKnowledgeBase(profilePath)
	if err := kb.Load(); err != nil {
		t.Fatal(err)
	}
	confirmed := knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)
	hypothesis := knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)
	if err := kb.AddSkill(CandidateSkillDetailed{
		ID: "skill-python", Name: "Python", Level: SkillLevelWorking,
		Projects: []string{"project-api"}, CanDo: []string{"integrate APIs"},
		CannotClaim: []string{"senior scale"}, KnowledgeMetadata: confirmed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.AddProject(CandidateProject{
		ID: "project-api", Name: "API tool", Type: CandidateProjectPersonal,
		Period:      CandidateProjectPeriod{Start: "2026-01", End: "2026-02"},
		Description: "Fixture integration", Technologies: []string{"Python"},
		KnowledgeMetadata: hypothesis,
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.AddAchievement(CandidateAchievement{
		ID: "achievement-api", Title: "API integration", ProjectID: "project-api",
		Result: []string{"less manual work"}, KnowledgeMetadata: hypothesis,
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.AddUnknown(CandidateUnknown{
		ID: "unknown-kubernetes", Question: "Used Kubernetes?", Hypothesis: "May have used it",
		Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: hypothesis,
	}); err != nil {
		t.Fatal(err)
	}
	proposalResult, err := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorAI}).UpdateSkill(
		CandidateSkillDetailed{Name: "Django", Level: SkillLevelWorking},
		KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceDerived, Evidence: []string{"analysis fixture"}}, Reason: "stage for user review"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if proposalResult.ProposalID == "" || len(kb.Proposals) == 0 {
		t.Fatalf("proposal was not staged: %+v", proposalResult)
	}
	knownEventAt := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	if err := kb.AddEvent(CandidateKnowledgeEvent{
		ID: "event-fixed", Timestamp: knownEventAt, Action: "note", EntityType: "skill", EntityID: "skill-python",
		OldValue: json.RawMessage(`{"level":"unknown"}`), NewValue: json.RawMessage(`{"level":"working"}`),
		Source: KnowledgeSourceUserConfirmed, Actor: "fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if err := kb.Save(); err != nil {
		t.Fatal(err)
	}

	restarted := NewCandidateKnowledgeBase(profilePath)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kb.Skills, restarted.Skills) || !reflect.DeepEqual(kb.Projects, restarted.Projects) ||
		!reflect.DeepEqual(kb.Achievements, restarted.Achievements) || !reflect.DeepEqual(kb.Unknowns, restarted.Unknowns) ||
		!knowledgeProposalsEquivalent(kb.Proposals, restarted.Proposals) || !knowledgeEventsEquivalent(kb.Events, restarted.Events) {
		t.Fatal("knowledge restart changed persisted domain state")
	}
	if restarted.Skills[0].TruthStatus != TruthStatusConfirmed || restarted.Skills[0].Sources[0].Type != KnowledgeSourceUserConfirmed ||
		!restarted.Skills[0].Sources[0].ObservedAt.Equal(*restarted.Skills[0].ConfirmedAt) ||
		len(restarted.Skills[0].Evidence) == 0 {
		t.Fatalf("confirmed provenance/evidence changed: %+v", restarted.Skills[0])
	}
	if restarted.Projects[0].TruthStatus != TruthStatusHypothesis || restarted.Unknowns[0].Status != CandidateUnknownNeedsConfirmation ||
		restarted.Proposals[0].Status != KnowledgeProposalPending || restarted.Events[len(restarted.Events)-1].Timestamp != knownEventAt {
		t.Fatalf("truth/unknown/proposal/event contract changed: %+v %+v %+v", restarted.Projects[0], restarted.Unknowns[0], restarted.Proposals[0])
	}
}

func TestCandidateKnowledgeStorageContract_SeparateFilesAndProfileBoundary(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "candidate_profile.json")
	profile := NewCandidateProfile(time.Now().UTC())
	if err := SaveCandidateProfile(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	profileBefore, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	kb := NewCandidateKnowledgeBase(profilePath)
	if err := kb.Load(); err != nil {
		t.Fatal(err)
	}
	if err := kb.AddSkill(CandidateSkillDetailed{Name: "Git", KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)}); err != nil {
		t.Fatal(err)
	}
	if err := kb.Save(); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(profilePath); err != nil || !reflect.DeepEqual(after, profileBefore) {
		t.Fatalf("knowledge Save rewrote the separate legacy profile: err=%v", err)
	}
	for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "proposals", "events"} {
		if _, err := os.Stat(filepath.Join(dir, "candidate_"+kind+".json")); err != nil {
			t.Fatalf("knowledge collection %s was not persisted separately: %v", kind, err)
		}
	}
	// There is deliberately no test claiming multi-file atomicity: the current
	// implementation can fail between collection renames. The same applies to
	// profile locking and absent raw-HH/exact-prompt artifacts.
}

func knowledgeProposalsEquivalent(left, right []KnowledgeProposal) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].ID != right[i].ID || left[i].EntityType != right[i].EntityType || left[i].EntityID != right[i].EntityID ||
			left[i].Reason != right[i].Reason || left[i].Source != right[i].Source || !reflect.DeepEqual(left[i].Confidence, right[i].Confidence) ||
			left[i].Status != right[i].Status || !left[i].CreatedAt.Equal(right[i].CreatedAt) ||
			canonicalKnowledgeJSON(left[i].ProposedValue) != canonicalKnowledgeJSON(right[i].ProposedValue) ||
			canonicalKnowledgeJSON(left[i].BaseValue) != canonicalKnowledgeJSON(right[i].BaseValue) {
			return false
		}
	}
	return true
}

func knowledgeEventsEquivalent(left, right []CandidateKnowledgeEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].ID != right[i].ID || !left[i].Timestamp.Equal(right[i].Timestamp) || left[i].Action != right[i].Action ||
			left[i].EntityType != right[i].EntityType || left[i].EntityID != right[i].EntityID || left[i].Source != right[i].Source || left[i].Actor != right[i].Actor ||
			canonicalKnowledgeJSON(left[i].OldValue) != canonicalKnowledgeJSON(right[i].OldValue) ||
			canonicalKnowledgeJSON(left[i].NewValue) != canonicalKnowledgeJSON(right[i].NewValue) {
			return false
		}
	}
	return true
}

func canonicalKnowledgeJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(canonical)
}
