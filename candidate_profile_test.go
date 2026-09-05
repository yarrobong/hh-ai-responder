package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileBootstrapCreatesValidProfileWithUnknownEmptyAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_profile.json")
	profile := NewCandidateProfile(time.Now())
	input := strings.NewReader(strings.Repeat("\n", 100))
	if err := runProfileBootstrap(&profile, path, input, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCandidateProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Skills) != len(bootstrapSkillNames) {
		t.Fatalf("empty bootstrap answers did not leave all listed skills unknown: got %d", len(loaded.Skills))
	}
	for _, skill := range loaded.Skills {
		if skill.Source != CandidateSourceUnknown || skill.Confirmed || skill.Level != SkillLevelUnknown {
			t.Fatalf("empty answer became a confirmed fact: %+v", skill)
		}
	}
	for _, skill := range loaded.Skills {
		if skill.Source == CandidateSourceDerived {
			t.Fatalf("bootstrap created derived fact: %+v", skill)
		}
	}
}

func TestProfileBootstrapPersistsExplicitlyConfirmedFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_profile.json")
	profile := NewCandidateProfile(time.Now())
	var input strings.Builder
	writeConfirmed := func(value string) {
		input.WriteString(value + "\nда\n")
	}
	writeConfirmed("Екатеринбург")
	writeConfirmed("higher")
	writeConfirmed("University")
	writeConfirmed("Computer Science")
	writeConfirmed("Backend Developer")
	input.WriteString("нет\n")
	for range bootstrapSkillNames {
		input.WriteString("3\nпроектная задача\nда\n")
	}
	for range bootstrapProjectNames {
		input.WriteString("да\n")
		writeConfirmed("Developer")
		writeConfirmed("Python")
		writeConfirmed("реализовал функциональность")
		writeConfirmed("сократил ручной труд")
	}
	input.WriteString("да\nда\nда\nда\n")
	input.WriteString("да\nда\nда\nда\n")
	if err := runProfileBootstrap(&profile, path, strings.NewReader(input.String()), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCandidateProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity.Location.Value != "Екатеринбург" || loaded.WorkPreferences.SalaryMinimum.Value != 50000 {
		t.Fatalf("confirmed personal/preferences facts missing: %+v", loaded)
	}
	if len(loaded.Projects) != len(bootstrapProjectNames) || len(loaded.Skills) != len(bootstrapSkillNames) {
		t.Fatalf("bootstrap facts missing: skills=%d projects=%d", len(loaded.Skills), len(loaded.Projects))
	}
	for _, skill := range loaded.Skills {
		if skill.Source != CandidateSourceUserConfirmed || !skill.Confirmed || len(skill.Evidence) == 0 {
			t.Fatalf("skill was not explicitly confirmed with evidence: %+v", skill)
		}
	}
	for _, project := range loaded.Projects {
		if project.Source != CandidateSourceUserConfirmed || !project.Confirmed || len(project.Evidence) == 0 {
			t.Fatalf("project was not explicitly confirmed with evidence: %+v", project)
		}
	}
	for _, fact := range []ProfileFact{loaded.WorkPreferences.WorkMode.ProfileFact, loaded.WorkPreferences.Relocation.ProfileFact, loaded.WorkPreferences.BusinessTrips.ProfileFact, loaded.WorkPreferences.SalaryMinimum.ProfileFact, loaded.WorkPreferences.PrimaryRoles.ProfileFact, loaded.WorkPreferences.SecondaryRoles.ProfileFact, loaded.EmployerCommunicationPreferences.AlwaysEmphasize.ProfileFact, loaded.EmployerCommunicationPreferences.AvoidClaiming.ProfileFact} {
		if fact.Source != CandidateSourceUserConfirmed || !fact.Confirmed || len(fact.Evidence) == 0 {
			t.Fatalf("confirmed bootstrap preference lacks trust metadata: %+v", fact)
		}
	}
}

func confirmedProfileFact(source CandidateSource, now time.Time) ProfileFact {
	return ProfileFact{Source: source, Confirmed: true, ConfirmedAt: now, Evidence: []string{"test evidence"}}
}

func TestCandidateProfileSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_profile.json")
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(now)
	profile.Skills = []CandidateSkill{{Name: "Git", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)}}
	if err := SaveCandidateProfile(path, profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCandidateProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "Git" || loaded.Skills[0].Source != CandidateSourceUserConfirmed {
		t.Fatalf("profile did not round-trip: %+v", loaded)
	}
	if mode := fileMode(t, path).Perm(); mode != 0o600 {
		t.Fatalf("profile permissions = %o, want 600", mode)
	}
}

func TestCorruptCandidateProfileFailsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_profile.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCandidateProfile(path); err == nil {
		t.Fatal("corrupt profile was accepted")
	}
}

func TestCandidateSourcePriority(t *testing.T) {
	if sourcePriority(CandidateSourceUserConfirmed) <= sourcePriority(CandidateSourceHHResume) ||
		sourcePriority(CandidateSourceHHResume) <= sourcePriority(CandidateSourceGithubVerified) ||
		sourcePriority(CandidateSourceGithubVerified) <= sourcePriority(CandidateSourceDerived) {
		t.Fatal("candidate source priorities are not ordered safely")
	}
}

func TestUserConfirmedSkillSurvivesHHMerge(t *testing.T) {
	now := time.Now()
	profile := NewCandidateProfile(now)
	profile.Skills = []CandidateSkill{{Name: "Git", Level: SkillLevelConfident, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)}}
	profile.MergeHHResumeFacts(ResumeItem{Area: "Екатеринбург", Skills: "Git, Docker"}, ResumeFacts{}, now.Add(time.Hour))
	git, ok := profile.ResolveSkill("Git")
	if !ok || git.Source != CandidateSourceUserConfirmed || git.Level != SkillLevelConfident {
		t.Fatalf("user fact was overwritten by HH: %+v, found=%t", git, ok)
	}
	docker, ok := profile.ResolveSkill("Docker")
	if !ok || docker.Source != CandidateSourceHHResume {
		t.Fatalf("HH skill was not imported: %+v, found=%t", docker, ok)
	}
}

func TestConfirmedGitResolvesOnlyGit(t *testing.T) {
	now := time.Now()
	candidate := CandidateContext{Profile: CandidateProfile{Skills: []CandidateSkill{{Name: "Git", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, now)}}}}
	met := deriveHardRequirements(candidate, Vacancy{}, "Требуется Git", []HardRequirementCandidate{{Requirement: "Git", Category: hardRequirementCategorySkill, VacancyEvidence: "Требуется Git"}})
	if len(met) != 1 || met[0].Status != hardRequirementStatusMet {
		t.Fatalf("confirmed Git did not resolve: %+v", met)
	}
	notGit := deriveHardRequirements(candidate, Vacancy{}, "Требуется GitHub", []HardRequirementCandidate{{Requirement: "GitHub", Category: hardRequirementCategorySkill, VacancyEvidence: "Требуется GitHub"}})
	if len(notGit) != 1 || notGit[0].Status != hardRequirementStatusUnknown {
		t.Fatalf("Git was fuzzy-matched to GitHub: %+v", notGit)
	}
}

func TestUnknownKubernetesCreatesDeduplicatedPendingQuestion(t *testing.T) {
	now := time.Now()
	r := &HHAIResponder{candidateProfile: NewCandidateProfile(now)}
	requirement := HardRequirementEvaluation{Requirement: "Kubernetes", Category: hardRequirementCategorySkill, Status: hardRequirementStatusUnknown}
	r.addPendingQuestionForRequirement(Vacancy{ID: 101}, requirement)
	r.addPendingQuestionForRequirement(Vacancy{ID: 202}, requirement)
	if len(r.candidateProfile.UnknownPendingFacts) != 1 {
		t.Fatalf("duplicate pending question was created: %+v", r.candidateProfile.UnknownPendingFacts)
	}
	if r.candidateProfile.UnknownPendingFacts[0].VacancyID != 101 {
		t.Fatalf("first vacancy context was not preserved: %+v", r.candidateProfile.UnknownPendingFacts[0])
	}
}

func TestAnsweringPendingQuestionChangesFutureEvaluation(t *testing.T) {
	now := time.Now()
	profile := NewCandidateProfile(now)
	profile.AddPendingQuestion(PendingProfileQuestion{Topic: "Kubernetes", Question: "Работал ли ты с Kubernetes?", Reason: "required skill"})
	if err := profile.AnswerPendingQuestion(0, SkillLevelWorking, now); err != nil {
		t.Fatal(err)
	}
	candidate := CandidateContext{Profile: profile}
	got := deriveHardRequirements(candidate, Vacancy{}, "Требуется Kubernetes", []HardRequirementCandidate{{Requirement: "Kubernetes", Category: hardRequirementCategorySkill, VacancyEvidence: "Требуется Kubernetes"}})
	if len(got) != 1 || got[0].Status != hardRequirementStatusMet {
		t.Fatalf("answered profile fact did not affect evaluation: %+v", got)
	}
}

func TestDerivedFactIsExcludedFromCoverLetterContext(t *testing.T) {
	now := time.Now()
	candidate := CandidateContext{Profile: CandidateProfile{Skills: []CandidateSkill{{Name: "Kubernetes", Level: SkillLevelWorking, ProfileFact: ProfileFact{Source: CandidateSourceDerived, Confirmed: false, ConfirmedAt: now}}}}}
	prompt := buildLetterSystemPrompt(candidate, "")
	if strings.Contains(prompt, "Kubernetes") {
		t.Fatalf("derived skill appeared in cover-letter context: %s", prompt)
	}
}

func TestCandidateProfileNeverPersistsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate_profile.json")
	profile := NewCandidateProfile(time.Now())
	profile.Skills = []CandidateSkill{{Name: "Git", Level: SkillLevelWorking, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: time.Now(), Evidence: []string{"api_key=do-not-save"}}}}
	if err := SaveCandidateProfile(path, profile); err == nil {
		t.Fatal("secret-bearing profile was persisted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("secret profile file exists: %v", err)
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
