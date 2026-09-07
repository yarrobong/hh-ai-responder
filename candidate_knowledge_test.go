package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func knowledgeTestMetadata(source KnowledgeSource, status TruthStatus) KnowledgeMetadata {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	meta := KnowledgeMetadata{
		TruthStatus: status, CreatedAt: now, UpdatedAt: now,
		Sources:  []KnowledgeSourceRecord{{Type: source, Evidence: []string{"explicit test evidence"}, ObservedAt: &now}},
		Evidence: []string{"test assertion"},
	}
	if status == TruthStatusConfirmed {
		meta.ConfirmedAt = &now
	}
	return meta
}

func knowledgeTestBase(t *testing.T) *CandidateKnowledgeBase {
	t.Helper()
	kb := NewCandidateKnowledgeBase(filepath.Join(t.TempDir(), "candidate_profile.json"))
	if err := kb.Load(); err != nil {
		t.Fatal(err)
	}
	return kb
}

func requireKnowledgeOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readKnowledgeTestFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	requireKnowledgeOK(t, err)
	return raw
}

func TestKnowledgeLoadMissingCollectionsIsReadOnly(t *testing.T) {
	kb := knowledgeTestBase(t)
	if kb.Profile.Version != 1 || len(kb.Skills)+len(kb.Projects)+len(kb.Achievements)+len(kb.Unknowns)+len(kb.Events) != 0 {
		t.Fatal("empty database did not load")
	}
	entries, err := os.ReadDir(filepath.Dir(kb.ProfilePath))
	requireKnowledgeOK(t, err)
	if len(entries) != 0 {
		t.Fatal("Load wrote files")
	}
	if err := NewCandidateKnowledgeBase("").Load(); err == nil {
		t.Fatal("empty profile path was accepted")
	}
}

func TestKnowledgeSaveLoadAllEntitiesAndEvents(t *testing.T) {
	kb := knowledgeTestBase(t)
	confidence := 0.85
	meta := knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)
	meta.Confidence = &confidence
	skill := CandidateSkillDetailed{
		ID: "skill-python", Name: "Python", Category: "backend", Level: SkillLevelWorking,
		Projects: []string{"project-api"}, CanDo: []string{"integrate APIs"}, CannotClaim: []string{"highload expert"},
		LastUsed: "2026", KnowledgeMetadata: meta,
	}
	requireKnowledgeOK(t, kb.AddSkill(skill))
	requireKnowledgeOK(t, kb.AddProject(CandidateProject{
		ID: "project-api", Name: "API tool", Type: CandidateProjectPersonal, Role: "developer",
		Period: CandidateProjectPeriod{Start: "2026-01", End: "2026-02"}, Technologies: []string{"Python"},
		Tasks: []string{"connect services"}, Results: []string{"working integration"}, RelatedSkills: []string{skill.ID},
		KnowledgeMetadata: meta,
	}))
	requireKnowledgeOK(t, kb.AddAchievement(CandidateAchievement{
		ID: "achievement-api", Title: "API integration", Problem: "manual work", Solution: []string{"integration"},
		Actions: []string{"implemented client"}, Result: []string{"automated task"}, Technologies: []string{"Python"},
		ProjectID: "project-api", KnowledgeMetadata: meta,
	}))
	requireKnowledgeOK(t, kb.AddUnknown(CandidateUnknown{
		ID: "unknown-node", Question: "Used Node.js backend?", RelatedEntity: "project-api", Hypothesis: "May have used Node.js",
		Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis),
	}))
	if len(kb.Events) != 4 {
		t.Fatal("Add methods did not record events")
	}
	for _, event := range kb.Events {
		if event.Actor != "local_storage" || event.Action != "add" || len(event.NewValue) == 0 || string(event.OldValue) != "null" {
			t.Fatalf("invalid add event: %+v", event)
		}
	}
	requireKnowledgeOK(t, kb.AddEvent(CandidateKnowledgeEvent{
		ID: "event-note", Action: "note", EntityType: "skill", EntityID: skill.ID,
		OldValue: json.RawMessage(`{"level":"unknown"}`), NewValue: json.RawMessage(`{"level":"working"}`),
		Source: KnowledgeSourceUserConfirmed, Actor: "test_importer",
	}))
	requireKnowledgeOK(t, kb.Save())
	loaded := NewCandidateKnowledgeBase(kb.ProfilePath)
	requireKnowledgeOK(t, loaded.Load())
	if !reflect.DeepEqual(kb.Skills, loaded.Skills) || !reflect.DeepEqual(kb.Projects, loaded.Projects) ||
		!reflect.DeepEqual(kb.Achievements, loaded.Achievements) || !reflect.DeepEqual(kb.Unknowns, loaded.Unknowns) {
		t.Fatal("knowledge did not round-trip")
	}
	// RawMessage preserves whitespace from the indented file; compare JSON values.
	eventsBefore, err := json.Marshal(kb.Events)
	requireKnowledgeOK(t, err)
	eventsAfter, err := json.Marshal(loaded.Events)
	requireKnowledgeOK(t, err)
	if !bytes.Equal(eventsBefore, eventsAfter) {
		t.Fatal("event snapshots did not round-trip")
	}
	if *loaded.Skills[0].Confidence != confidence || loaded.Skills[0].Sources[0].Type != KnowledgeSourceUserConfirmed {
		t.Fatal("source/confidence changed")
	}
	for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "events"} {
		path := filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_"+kind+".json")
		if fileMode(t, path).Perm() != 0o600 {
			t.Fatal("knowledge file is not private")
		}
	}
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(kb.ProfilePath), ".candidate_*.tmp"))
	requireKnowledgeOK(t, err)
	if len(entries) != 0 {
		t.Fatal("temporary files remain after Save")
	}
}

func TestKnowledgeConfidenceNeverGrantsTruth(t *testing.T) {
	sources := []KnowledgeSource{KnowledgeSourceUserConfirmed, KnowledgeSourceHHResume, KnowledgeSourceGithubVerified,
		KnowledgeSourceCandidateInterview, KnowledgeSourceProjectAnalysis, KnowledgeSourceDerived, KnowledgeSourceUnknown}
	statuses := []TruthStatus{TruthStatusConfirmed, TruthStatusVerified, TruthStatusHypothesis, TruthStatusUnknown}
	for _, source := range sources {
		for _, status := range statuses {
			t.Run(string(source)+"/"+string(status), func(t *testing.T) {
				kb := knowledgeTestBase(t)
				meta := knowledgeTestMetadata(source, status)
				confidence := 1.0
				meta.Confidence = &confidence
				err := kb.AddSkill(CandidateSkillDetailed{Name: "Node.js", Level: SkillLevelAdvanced, KnowledgeMetadata: meta})
				allowed := status == TruthStatusHypothesis || status == TruthStatusUnknown ||
					(status == TruthStatusConfirmed && source == KnowledgeSourceUserConfirmed) ||
					(status == TruthStatusVerified && (source == KnowledgeSourceHHResume || source == KnowledgeSourceGithubVerified))
				if (err == nil) != allowed {
					t.Fatalf("allowed=%t, error=%v", allowed, err)
				}
				if !allowed {
					if len(kb.Skills)+len(kb.Events) != 0 {
						t.Fatal("failed Add changed facts/events")
					}
					return
				}
				requireKnowledgeOK(t, kb.Save())
				requireKnowledgeOK(t, kb.Load())
				if kb.Skills[0].TruthStatus != status || *kb.Skills[0].Confidence != confidence {
					t.Fatal("storage promoted knowledge or altered confidence")
				}
			})
		}
	}
}

func TestKnowledgeMetadataRejectsInvalidConfidenceAndUnprovenSources(t *testing.T) {
	for _, confidence := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		meta := knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)
		meta.Confidence = &confidence
		if err := knowledgeTestBase(t).AddSkill(CandidateSkillDetailed{Name: "Python", KnowledgeMetadata: meta}); err == nil {
			t.Fatal("invalid confidence accepted")
		}
	}
	for _, change := range []func(*KnowledgeMetadata){
		func(m *KnowledgeMetadata) { m.Sources = nil },
		func(m *KnowledgeMetadata) { m.Sources[0].Type = "invalid" },
		func(m *KnowledgeMetadata) { m.TruthStatus = "invalid" },
		func(m *KnowledgeMetadata) { m.Sources[0].Evidence = []string{" "} },
		func(m *KnowledgeMetadata) { m.ConfirmedAt = nil },
		func(m *KnowledgeMetadata) { m.UpdatedAt = m.CreatedAt.Add(-time.Hour) },
		func(m *KnowledgeMetadata) {
			m.Sources = []KnowledgeSourceRecord{{Type: KnowledgeSourceDerived, Evidence: []string{"guess"}}, {Type: KnowledgeSourceUserConfirmed}}
		},
	} {
		meta := knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)
		change(&meta)
		if err := knowledgeTestBase(t).AddSkill(CandidateSkillDetailed{Name: "Python", KnowledgeMetadata: meta}); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestKnowledgeMigrationPreservesLegacyAndIsRepeatable(t *testing.T) {
	kb := knowledgeTestBase(t)
	now := time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(now)
	profile.Skills = []CandidateSkill{
		{Name: "Python", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)},
		{Name: "Git", Level: SkillLevelUnknown, ProfileFact: confirmedProfileFact(CandidateSourceHHResume, now)},
		{Name: "Go", Level: SkillLevelBasic, ProfileFact: confirmedProfileFact(CandidateSourceGithubVerified, now)},
		{Name: "Node.js", Level: SkillLevelAdvanced, ProfileFact: ProfileFact{Source: CandidateSourceDerived, Evidence: []string{"hypothesis"}}},
		{Name: "XML", Level: SkillLevelUnknown, Negative: true, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)},
		{Name: "Kubernetes", Level: SkillLevelUnknown},
		{Name: "Java", Level: SkillLevelBasic, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed}},
	}
	profile.Projects = []ProjectFact{{Name: "Test project", Role: "developer", Description: "Legacy description", Technologies: []string{"Python"},
		BusinessImpact: "Less manual work", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)}}
	requireKnowledgeOK(t, SaveCandidateProfile(kb.ProfilePath, profile))
	before := readKnowledgeTestFile(t, kb.ProfilePath)
	storiesPath := filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_stories.json")
	stories := []byte(`{"version":1,"stories":[{"title":"Story","action":"Built something"}]}`)
	requireKnowledgeOK(t, os.WriteFile(storiesPath, stories, 0o600))
	requireKnowledgeOK(t, kb.Load())
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	if len(kb.Skills) != 7 || len(kb.Projects) != 1 || len(kb.Events) != 8 {
		t.Fatal("migration did not copy all assertions")
	}
	wantStatuses := []TruthStatus{TruthStatusConfirmed, TruthStatusVerified, TruthStatusVerified, TruthStatusHypothesis, TruthStatusConfirmed, TruthStatusUnknown, TruthStatusUnknown}
	for i, skill := range kb.Skills {
		if skill.TruthStatus != wantStatuses[i] || skill.Confidence != nil || skill.Level != profile.Skills[i].Level || skill.Negative != profile.Skills[i].Negative {
			t.Fatalf("migration changed the meaning of skill %d", i)
		}
		wantSource := KnowledgeSource(profile.Skills[i].Source)
		if wantSource == "" {
			wantSource = KnowledgeSourceUnknown
		}
		if skill.Sources[0].Type != wantSource || !reflect.DeepEqual(skill.Evidence, profile.Skills[i].Evidence) {
			t.Fatal("migration lost provenance")
		}
	}
	if !kb.Skills[0].ConfirmedAt.Equal(now) || !kb.Skills[0].Sources[0].ObservedAt.Equal(now) {
		t.Fatal("migration lost original confirmation time")
	}
	project := kb.Projects[0]
	if project.Type != "" || project.Period != (CandidateProjectPeriod{}) || project.Description != profile.Projects[0].Description ||
		len(project.Tasks) != 0 || len(project.RelatedSkills) != 0 || project.Results[0] != profile.Projects[0].BusinessImpact {
		t.Fatal("migration inferred or lost project facts")
	}
	if len(kb.Achievements)+len(kb.Unknowns) != 0 {
		t.Fatal("migration invented knowledge from stories")
	}
	// Enrichment must survive migration even when the old profile is unchanged.
	kb.Skills[0].CanDo = []string{"explicit later enrichment"}
	requireKnowledgeOK(t, kb.Save())
	first := make(map[string][]byte)
	for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "events"} {
		path := filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_"+kind+".json")
		first[path] = readKnowledgeTestFile(t, path)
	}
	requireKnowledgeOK(t, kb.Load())
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	requireKnowledgeOK(t, kb.Save())
	for path, raw := range first {
		if !bytes.Equal(raw, readKnowledgeTestFile(t, path)) {
			t.Fatal("repeated migration changed knowledge or duplicated events")
		}
	}
	if !bytes.Equal(before, readKnowledgeTestFile(t, kb.ProfilePath)) || !bytes.Equal(stories, readKnowledgeTestFile(t, storiesPath)) {
		t.Fatal("migration rewrote legacy files")
	}
	for _, event := range kb.Events {
		if event.Actor != "legacy_migration" || event.Action != "migrate" {
			t.Fatal("migration was not audited")
		}
	}
}

func TestKnowledgeMigrationDeduplicatesExactAssertionsAndKeepsConflicts(t *testing.T) {
	kb := knowledgeTestBase(t)
	fact := CandidateSkill{Name: "Go", Level: SkillLevelBasic, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, time.Now())}
	alias := fact
	alias.Name = " golang "
	conflict := fact
	conflict.Level = SkillLevelAdvanced
	kb.Profile.Skills = []CandidateSkill{fact, alias, conflict}
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	if len(kb.Skills) != 2 || len(kb.Events) != 2 {
		t.Fatal("migration duplicated equivalent assertions or discarded conflicting levels")
	}
	// Fail the entire in-memory migration rather than retaining earlier copies.
	bad := knowledgeTestBase(t)
	bad.Profile.Skills = []CandidateSkill{fact, {Name: "", Level: SkillLevelUnknown}}
	if err := bad.MigrateLegacyProfile(); err == nil || len(bad.Skills)+len(bad.Events) != 0 {
		t.Fatal("failed migration partially changed state")
	}
}

func TestKnowledgeSaveRejectsManualPromotionBeforeAnyWrite(t *testing.T) {
	kb := knowledgeTestBase(t)
	requireKnowledgeOK(t, kb.AddSkill(CandidateSkillDetailed{Name: "Node.js", KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)}))
	requireKnowledgeOK(t, kb.Save())
	path := filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_skills.json")
	before := readKnowledgeTestFile(t, path)
	kb.Skills[0].TruthStatus = TruthStatusConfirmed
	now := time.Now()
	kb.Skills[0].ConfirmedAt = &now
	if err := kb.Save(); err == nil {
		t.Fatal("direct mutation bypassed validation")
	}
	if !bytes.Equal(before, readKnowledgeTestFile(t, path)) {
		t.Fatal("failed validation changed file")
	}
}

func TestKnowledgeLoadRejectsMalformedFilesWithoutReplacingState(t *testing.T) {
	for name, raw := range map[string]string{
		"broken": "{", "null": "null", "empty": "", "array": "[]",
		"version": `{"version":2,"skills":[]}`, "missing": `{"version":1}`,
		"null_collection": `{"version":1,"skills":null}`, "extra_root": `{"version":1,"skills":[],"extra":true}`,
		"extra_field": `{"version":1,"skills":[{"id":"test","surprise":true}]}`,
		"trailing":    `{"version":1,"skills":[]} {}`, "secret": `{"version":1,"skills":[],"api_key":"secret"}`,
	} {
		t.Run(name, func(t *testing.T) {
			kb := knowledgeTestBase(t)
			requireKnowledgeOK(t, kb.AddSkill(CandidateSkillDetailed{ID: "keep", Name: "Git", KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceHHResume, TruthStatusVerified)}))
			requireKnowledgeOK(t, os.WriteFile(filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_skills.json"), []byte(raw), 0o600))
			if err := kb.Load(); err == nil {
				t.Fatal("invalid file loaded")
			}
			if len(kb.Skills) != 1 || kb.Skills[0].ID != "keep" || len(kb.Events) != 1 {
				t.Fatal("failed Load replaced in-memory state")
			}
		})
	}
	for _, status := range []TruthStatus{TruthStatusConfirmed, TruthStatusVerified} {
		kb := knowledgeTestBase(t)
		skill := CandidateSkillDetailed{ID: "bad", Name: "Node.js", Level: SkillLevelAdvanced,
			KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceDerived, status)}
		raw, err := json.Marshal(map[string]any{"version": 1, "skills": []CandidateSkillDetailed{skill}})
		requireKnowledgeOK(t, err)
		requireKnowledgeOK(t, os.WriteFile(filepath.Join(filepath.Dir(kb.ProfilePath), "candidate_skills.json"), raw, 0o600))
		if err := kb.Load(); err == nil {
			t.Fatal("on-disk derived fact was promoted")
		}
	}
}

func TestKnowledgeUnknownResolutionRequiresUserEvidence(t *testing.T) {
	kb := knowledgeTestBase(t)
	unknown := CandidateUnknown{Question: "Used Node.js?", Hypothesis: "Used Node.js", Status: CandidateUnknownConfirmed,
		KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)}
	if err := kb.AddUnknown(unknown); err == nil {
		t.Fatal("derived question was confirmed")
	}
	unknown.Status = CandidateUnknownRejected
	if err := kb.AddUnknown(unknown); err == nil {
		t.Fatal("question rejected without user evidence")
	}
	unknown.Sources = []KnowledgeSourceRecord{{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"user denied the hypothesis"}}}
	requireKnowledgeOK(t, kb.AddUnknown(unknown))
	unknown.Status = CandidateUnknownConfirmed
	unknown.KnowledgeMetadata = knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)
	requireKnowledgeOK(t, kb.AddUnknown(unknown))
	if len(kb.Skills) != 0 {
		t.Fatal("storing a resolved question implicitly added a skill")
	}
}

func TestKnowledgeAddOwnsValuesAndProtectsIDs(t *testing.T) {
	kb := knowledgeTestBase(t)
	confidence := 0.0
	meta := knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)
	meta.Confidence = &confidence
	skill := CandidateSkillDetailed{ID: "skill-test", Name: "Git", CanDo: []string{"original"}, KnowledgeMetadata: meta}
	requireKnowledgeOK(t, kb.AddSkill(skill))
	skill.CanDo[0] = "changed"
	skill.Sources[0].Type = KnowledgeSourceUserConfirmed
	confidence = 1.0
	if kb.Skills[0].CanDo[0] != "original" || kb.Skills[0].Sources[0].Type != KnowledgeSourceDerived || *kb.Skills[0].Confidence != 0 {
		t.Fatal("caller mutated stored knowledge by alias")
	}
	if err := kb.AddSkill(skill); err == nil || len(kb.Events) != 1 || len(kb.Skills) != 1 {
		t.Fatal("duplicate id replaced an assertion")
	}
	if err := kb.AddEvent(kb.Events[0]); err == nil || len(kb.Events) != 1 {
		t.Fatal("duplicate event accepted")
	}
	kb.Skills = append(kb.Skills, kb.Skills[0])
	if err := kb.Save(); err == nil {
		t.Fatal("duplicate ids persisted")
	}
}

func TestKnowledgeRejectsSecretsInFactsAndEvents(t *testing.T) {
	kb := knowledgeTestBase(t)
	meta := knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis)
	meta.Evidence = []string{"api_key=must-not-persist"}
	if err := kb.AddSkill(CandidateSkillDetailed{Name: "Git", KnowledgeMetadata: meta}); err == nil {
		t.Fatal("secret in fact accepted")
	}
	if err := kb.AddEvent(CandidateKnowledgeEvent{Action: "add", EntityType: "skill", EntityID: "test", Source: KnowledgeSourceDerived,
		Actor: "test", NewValue: json.RawMessage(`{"authorization":"must-not-persist"}`)}); err == nil {
		t.Fatal("secret in event accepted")
	}
	if len(kb.Skills)+len(kb.Events) != 0 {
		t.Fatal("secret-bearing Add changed state")
	}
}

func TestKnowledgeFoundationDoesNotChangeProfileCommands(t *testing.T) {
	kb := knowledgeTestBase(t)
	profile := NewCandidateProfile(time.Now())
	profile.Skills = []CandidateSkill{{Name: "Git", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, time.Now())}}
	sourcePath := filepath.Join(filepath.Dir(kb.ProfilePath), "import.json")
	requireKnowledgeOK(t, SaveCandidateProfile(sourcePath, profile))
	var out bytes.Buffer
	requireKnowledgeOK(t, runProfileCommand([]string{"import", sourcePath, "-candidate-profile", kb.ProfilePath}, strings.NewReader(""), &out))
	requireKnowledgeOK(t, kb.Load())
	requireKnowledgeOK(t, kb.MigrateLegacyProfile())
	requireKnowledgeOK(t, kb.Save())
	requireKnowledgeOK(t, runProfileCommand([]string{"show", "-candidate-profile", kb.ProfilePath}, strings.NewReader(""), &out))
	var shown CandidateProfile
	requireKnowledgeOK(t, json.Unmarshal(out.Bytes(), &shown))
	if len(shown.Skills) != 1 || shown.Skills[0].Level != SkillLevelWorking {
		t.Fatal("legacy show format changed")
	}
	storiesPath := filepath.Join(filepath.Dir(kb.ProfilePath), "stories.json")
	requireKnowledgeOK(t, os.WriteFile(storiesPath, []byte(`[{"title":"Original case","task":"Original task"}]`), 0o600))
	out.Reset()
	requireKnowledgeOK(t, runProfileCommand([]string{"stories", "-candidate-stories", storiesPath}, strings.NewReader(""), &out))
	if !strings.Contains(out.String(), "Original case") {
		t.Fatal("legacy stories command changed")
	}
}
