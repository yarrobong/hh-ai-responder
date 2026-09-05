package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testHardRequirement(requirement, category, status, vacancyEvidence, candidateEvidence string) HardRequirementEvaluation {
	return HardRequirementEvaluation{
		Requirement:       requirement,
		Category:          category,
		Status:            status,
		VacancyEvidence:   vacancyEvidence,
		CandidateEvidence: candidateEvidence,
	}
}

func testEvaluation(requirements ...HardRequirementEvaluation) VacancyEvaluation {
	return VacancyEvaluation{Score: 90, Apply: true, Reasons: []string{}, Missing: []string{}, HardRequirements: requirements}
}

func TestValidateHardRequirementsEducationIsAlwaysUnknown(t *testing.T) {
	candidate := CandidateContext{}
	vacancy := Vacancy{}

	missing := testEvaluation(testHardRequirement("высшее образование", hardRequirementCategoryEducation, hardRequirementStatusMissing, "Требуется высшее образование", "Образование отсутствует"))
	if err := validateHardRequirements(candidate, vacancy, missing); err == nil {
		t.Fatal("education missing was accepted without structured candidate education")
	}

	unknown := testEvaluation(testHardRequirement("высшее образование", hardRequirementCategoryEducation, hardRequirementStatusUnknown, "Требуется высшее образование", "not provided"))
	if err := validateHardRequirements(candidate, vacancy, unknown); err != nil {
		t.Fatalf("education unknown was rejected: %v", err)
	}
	if got := vacancyDecision(unknown, 65); got != VacancyReviewRequired {
		t.Fatalf("education unknown decision: got %s, want %s", got, VacancyReviewRequired)
	}
}

func TestEvaluateVacancyRetriesSemanticallyInvalidEducation(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() { aiRetryDelay = previousDelay })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[{"requirement":"высшее образование","category":"education","status":"missing","vacancy_evidence":"Требуется высшее образование","candidate_evidence":"Образование отсутствует"}]}`))
			return
		}
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[{"requirement":"высшее образование","category":"education","status":"unknown","vacancy_evidence":"Требуется высшее образование","candidate_evidence":"not provided"}]}`))
	}))
	defer server.Close()

	client := NewAIClient(context.Background(), server.URL, "test-model", "", time.Second, time.Second, 2)
	evaluation, err := client.EvaluateVacancy(vacancyEvaluationInput{Candidate: CandidateContext{}, Vacancy: Vacancy{Name: "Backend"}, Description: "Требуется высшее образование"})
	if err != nil {
		t.Fatalf("semantic retry failed: %v", err)
	}
	if calls.Load() != 2 || evaluation.HardRequirements[0].Status != hardRequirementStatusUnknown {
		t.Fatalf("unexpected semantic retry result: calls=%d evaluation=%+v", calls.Load(), evaluation)
	}
}

func TestValidateHardRequirementsExperienceRequiresUnknownDuration(t *testing.T) {
	candidate := CandidateContext{Experience: "5 лет в backend-разработке"}
	vacancy := Vacancy{WorkExperience: "Опыт 3-6 лет"}
	for _, status := range []string{hardRequirementStatusMissing, hardRequirementStatusMet} {
		evaluation := testEvaluation(testHardRequirement("3 года опыта", hardRequirementCategoryExperienceYears, status, "Опыт от 3 лет", "not provided"))
		if err := validateHardRequirements(candidate, vacancy, evaluation); err == nil {
			t.Fatalf("experience status %s was accepted without structured duration", status)
		}
	}
	unknown := testEvaluation(testHardRequirement("3 года опыта", hardRequirementCategoryExperienceYears, hardRequirementStatusUnknown, "Опыт от 3 лет", "not provided"))
	if err := validateHardRequirements(candidate, vacancy, unknown); err != nil {
		t.Fatalf("experience unknown was rejected: %v", err)
	}
	if got := vacancyDecision(unknown, 65); got != VacancyReviewRequired {
		t.Fatalf("experience unknown decision: got %s, want %s", got, VacancyReviewRequired)
	}
}

func TestValidateHardRequirementsDoesNotInventExperienceRequirement(t *testing.T) {
	evaluation := testEvaluation(testHardRequirement("минимальный опыт", hardRequirementCategoryExperienceYears, hardRequirementStatusUnknown, "Опыт не требуется", "not provided"))
	if err := validateHardRequirements(CandidateContext{}, Vacancy{WorkExperience: "Без опыта"}, evaluation); err == nil {
		t.Fatal("experience requirement was accepted for a no-experience vacancy")
	}
}

func TestValidateHardRequirementsLocationIsConservative(t *testing.T) {
	candidate := CandidateContext{Location: "Екатеринбург"}
	vacancy := Vacancy{Area: NamedObject{Name: "Ташкент"}}
	missing := testEvaluation(testHardRequirement("офис в Ташкенте", hardRequirementCategoryLocation, hardRequirementStatusMissing, "Работа в офисе Ташкента", "Кандидат находится в Екатеринбурге"))
	if err := validateHardRequirements(candidate, vacancy, missing); err == nil {
		t.Fatal("different-city location missing was accepted without relocation fact")
	}
	unknown := testEvaluation(testHardRequirement("офис в Ташкенте", hardRequirementCategoryLocation, hardRequirementStatusUnknown, "Работа в офисе Ташкента", "not provided"))
	if err := validateHardRequirements(candidate, vacancy, unknown); err != nil {
		t.Fatalf("different-city location unknown was rejected: %v", err)
	}

	sameCity := testEvaluation(testHardRequirement("офис в Екатеринбурге", hardRequirementCategoryLocation, hardRequirementStatusMet, "Работа в офисе Екатеринбурга", "Локация кандидата: Екатеринбург"))
	if err := validateHardRequirements(candidate, Vacancy{Area: NamedObject{Name: "Екатеринбург"}}, sameCity); err != nil {
		t.Fatalf("same-city location met was rejected: %v", err)
	}
}

func TestValidateHardRequirementsMissingSkillRequiresExplicitNegativeFact(t *testing.T) {
	candidate := CandidateContext{Skills: "Python, Django"}
	missing := testEvaluation(testHardRequirement("Kafka", hardRequirementCategorySkill, hardRequirementStatusMissing, "Kafka обязателен", "Kafka отсутствует в резюме"))
	if err := validateHardRequirements(candidate, Vacancy{}, missing); err == nil {
		t.Fatal("missing skill was accepted from absence in candidate context")
	}
	unknown := testEvaluation(testHardRequirement("Kafka", hardRequirementCategorySkill, hardRequirementStatusUnknown, "Kafka обязателен", "not provided"))
	if err := validateHardRequirements(candidate, Vacancy{}, unknown); err != nil {
		t.Fatalf("unknown skill was rejected: %v", err)
	}
}

func TestValidateHardRequirementsEvidenceAndOptionalRequirements(t *testing.T) {
	for _, requirement := range []HardRequirementEvaluation{
		testHardRequirement("Python", hardRequirementCategorySkill, hardRequirementStatusUnknown, "", "not provided"),
		testHardRequirement("Python", hardRequirementCategorySkill, hardRequirementStatusMet, "Python обязателен", ""),
		testHardRequirement("Python", hardRequirementCategorySkill, hardRequirementStatusMissing, "Python обязателен", ""),
	} {
		if err := validateHardRequirements(CandidateContext{Skills: "Python"}, Vacancy{}, testEvaluation(requirement)); err == nil {
			t.Fatalf("invalid evidence was accepted: %+v", requirement)
		}
	}

	optional := testEvaluation(testHardRequirement("знание Kubernetes будет плюсом", hardRequirementCategorySkill, hardRequirementStatusMissing, "Kubernetes будет плюсом", "not provided"))
	if got := vacancyDecision(optional, 65); got == VacancyReject {
		t.Fatal("optional requirement caused REJECT")
	}
	if err := validateHardRequirements(CandidateContext{}, Vacancy{}, optional); err == nil {
		t.Fatal("optional requirement was accepted as hard requirement")
	}

	grounded := testEvaluation(testHardRequirement("Python", hardRequirementCategorySkill, hardRequirementStatusMet, "Python обязателен", "Python указан в навыках"))
	if err := validateHardRequirements(CandidateContext{Skills: "Python"}, Vacancy{}, grounded); err != nil {
		t.Fatalf("grounded skill evidence was rejected: %v", err)
	}
}
