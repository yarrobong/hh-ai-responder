package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func intelligenceMetadata(source KnowledgeSource, status TruthStatus) KnowledgeMetadata {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	metadata := KnowledgeMetadata{
		TruthStatus: status,
		Sources:     []KnowledgeSourceRecord{{Type: source, Evidence: []string{"synthetic test evidence"}}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if status == TruthStatusConfirmed {
		metadata.ConfirmedAt = &now
	}
	return metadata
}

func intelligenceKB(skills ...CandidateSkillDetailed) *CandidateKnowledgeBase {
	kb := NewCandidateKnowledgeBase("")
	kb.Skills = skills
	return kb
}

func confirmedIntelligenceSkill(name string) CandidateSkillDetailed {
	return CandidateSkillDetailed{
		ID: "skill-" + strings.ToLower(name), Name: name, Level: SkillLevelWorking,
		KnowledgeMetadata: intelligenceMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed),
	}
}

func TestVacancyAnalyzerHighScoreForPythonDjango(t *testing.T) {
	kb := intelligenceKB(confirmedIntelligenceSkill("Python"), confirmedIntelligenceSkill("Django"))
	kb.Profile.WorkPreferences.PrimaryRoles = ProfileStringFact{
		Value: "Backend Developer", ProfileFact: ProfileFact{Confirmed: true},
	}

	result := NewVacancyAnalyzer().Analyze(Vacancy{
		Title: "Python Developer", Skills: []string{"Python", "Django"},
	}, kb)

	if result.Score < 75 {
		t.Fatalf("score=%d, want high score", result.Score)
	}
	if len(result.MatchedSkills) != 2 || len(result.UnknownSkills) != 0 {
		t.Fatalf("skill match=%+v unknown=%+v", result.MatchedSkills, result.UnknownSkills)
	}
	if result.Recommendation == nil || result.Recommendation.Decision != RecommendationApply {
		t.Fatalf("recommendation=%+v, want apply", result.Recommendation)
	}
}

func TestVacancyAnalyzerSeparatesUnknownFromMissing(t *testing.T) {
	kb := intelligenceKB(confirmedIntelligenceSkill("Python"))
	result := NewVacancyAnalyzer().Analyze(Vacancy{Skills: []string{"Python", "Kubernetes"}}, kb)

	if len(result.MissingSkills) != 0 || len(result.UnknownSkills) != 1 || result.UnknownSkills[0] != "Kubernetes" {
		t.Fatalf("missing=%+v unknown=%+v", result.MissingSkills, result.UnknownSkills)
	}
	if len(result.RiskDetails) == 0 || result.RiskDetails[0].Type != "unknown_skill" {
		t.Fatalf("risk details=%+v, want unknown skill risk", result.RiskDetails)
	}
}

func TestVacancyAnalyzerExplicitNegativeIsMissing(t *testing.T) {
	skill := confirmedIntelligenceSkill("Docker")
	skill.Negative = true
	result := NewVacancyAnalyzer().Analyze(Vacancy{Skills: []string{"Docker"}}, intelligenceKB(skill))

	if len(result.MissingSkills) != 1 || len(result.UnknownSkills) != 0 {
		t.Fatalf("missing=%+v unknown=%+v", result.MissingSkills, result.UnknownSkills)
	}
}

func TestVacancyAnalyzerExperienceGapDoesNotBlock(t *testing.T) {
	kb := intelligenceKB(confirmedIntelligenceSkill("Python"))
	kb.Profile.TotalExperienceMonths = ProfileIntFact{Value: 11, ProfileFact: ProfileFact{Confirmed: true}}
	result := NewVacancyAnalyzer().Analyze(Vacancy{
		Skills: []string{"Python"}, WorkExperience: "Опыт 1-3 года",
	}, kb)

	if !strings.Contains(result.ExperienceNote, "Близкий уровень опыта") {
		t.Fatalf("experience note=%q", result.ExperienceNote)
	}
	if result.Recommendation == nil || result.Recommendation.Decision == RecommendationSkip {
		t.Fatalf("recommendation=%+v, experience gap must not block", result.Recommendation)
	}
}

func TestVacancyAnalyzerIgnoresHypothesisSkill(t *testing.T) {
	hypothesis := CandidateSkillDetailed{
		ID: "docker", Name: "Docker", Level: SkillLevelWorking,
		KnowledgeMetadata: intelligenceMetadata(KnowledgeSourceDerived, TruthStatusHypothesis),
	}
	result := NewVacancyAnalyzer().Analyze(Vacancy{Skills: []string{"Docker"}}, intelligenceKB(hypothesis))

	if len(result.MatchedSkills) != 0 || len(result.MissingSkills) != 0 || len(result.UnknownSkills) != 1 {
		t.Fatalf("matched=%+v missing=%+v unknown=%+v", result.MatchedSkills, result.MissingSkills, result.UnknownSkills)
	}
}

func TestVacancyAnalyzerDifferentOfficeCityCreatesRisk(t *testing.T) {
	kb := intelligenceKB(confirmedIntelligenceSkill("Python"))
	kb.Profile.Identity.Location = ProfileStringFact{
		Value: "Екатеринбург", ProfileFact: ProfileFact{Confirmed: true},
	}
	result := NewVacancyAnalyzer().Analyze(Vacancy{
		Skills: []string{"Python"}, Location: "Москва",
	}, kb)

	found := false
	for _, risk := range result.RiskDetails {
		found = found || risk.Type == "location"
	}
	if !found {
		t.Fatalf("risk details=%+v, want location risk", result.RiskDetails)
	}
}

func TestVacancyAnalyzerEmptyKnowledgeDoesNotPanic(t *testing.T) {
	result := NewVacancyAnalyzer().Analyze(Vacancy{Skills: []string{"Python", "Kubernetes"}}, NewCandidateKnowledgeBase(""))
	if result.Recommendation == nil || len(result.UnknownSkills) != 2 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVacancyStoreCRUDAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), VacanciesFilename)
	store := NewVacancyStore(path)
	created, err := store.Create(Vacancy{ExternalID: "hh-1", Title: "Python Developer"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created=%+v, want generated identity and timestamps", created)
	}
	created.Company.Name = "Example"
	if err := store.Update(created); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := NewVacancyStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := loaded.Get("hh-1")
	if err != nil || got.Company.Name != "Example" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := loaded.Delete(got.ID); err != nil {
		t.Fatal(err)
	}
	values, err := loaded.List()
	if err != nil || len(values) != 0 {
		t.Fatalf("values=%+v err=%v", values, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file disappeared unexpectedly: %v", err)
	}
}
