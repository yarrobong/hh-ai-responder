package main

import (
	"bytes"
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

func TestEvaluateVacancyDerivesEducationLocallyWithoutSemanticRetry(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() { aiRetryDelay = previousDelay })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[{"requirement":"высшее образование","category":"education","vacancy_evidence":"Требуется высшее образование"}]}`))
	}))
	defer server.Close()

	client := NewAIClient(context.Background(), server.URL, "test-model", "", time.Second, time.Second, 2)
	evaluation, err := client.EvaluateVacancy(vacancyEvaluationInput{Candidate: CandidateContext{}, Vacancy: Vacancy{Name: "Backend"}, Description: "Требуется высшее образование"})
	if err != nil {
		t.Fatalf("semantic retry failed: %v", err)
	}
	if calls.Load() != 1 || evaluation.HardRequirements[0].Status != hardRequirementStatusUnknown {
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

func TestLocationRequirementMatchesCandidateUsesExplicitEvidence(t *testing.T) {
	candidateLocation := "Екатеринбург"
	tests := []struct {
		name        string
		requirement string
		evidence    string
		want        bool
	}{
		{name: "same city", requirement: "Екатеринбург", evidence: "место работы: Екатеринбург", want: true},
		{name: "same city with district", requirement: "Екатеринбург, р-н Октябрьский", evidence: "офис в Екатеринбурге, р-н Октябрьский", want: true},
		{name: "specific village is not candidate city", requirement: "с. Кадниково", evidence: "место работы: Свердловская область, Сысертский р-он, с. Кадниково", want: false},
		{name: "different city", requirement: "Берёзовский", evidence: "работа в Берёзовском", want: false},
		{name: "different region city", requirement: "Тула", evidence: "офис в Туле", want: false},
		{name: "business trips", requirement: "готовность к выездам", evidence: "готовность к выездам на объекты заказчика", want: false},
		{name: "relocation", requirement: "релокация в Елабугу", evidence: "готовность к релокации в Елабугу", want: false},
		{name: "onsite without city", requirement: "очный формат", evidence: "работа в очном формате", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := locationRequirementMatchesCandidate(candidateLocation, test.requirement, test.evidence); got != test.want {
				t.Fatalf("location match: got %t, want %t", got, test.want)
			}
		})
	}
}

func TestDeriveHardRequirementLocationDoesNotUseVacancyArea(t *testing.T) {
	status, evidence := deriveHardRequirementStatus(
		CandidateContext{Location: "Екатеринбург"},
		Vacancy{Area: NamedObject{Name: "Екатеринбург"}},
		HardRequirementCandidate{
			Requirement:     "с. Кадниково",
			Category:        hardRequirementCategoryLocation,
			VacancyEvidence: "место работы: Свердловская область, Сысертский р-он, с. Кадниково",
		},
	)
	if status != hardRequirementStatusUnknown || evidence != "not provided" {
		t.Fatalf("AI location requirement was grounded by generic vacancy area: status=%q evidence=%q", status, evidence)
	}
}

func TestAIHardRequirementLocationCannotUseVacancyAreaAsEvidence(t *testing.T) {
	derived := deriveHardRequirements(
		CandidateContext{Location: "Екатеринбург"},
		Vacancy{Area: NamedObject{Name: "Екатеринбург"}},
		"",
		[]HardRequirementCandidate{{
			Requirement:     "Екатеринбург",
			Category:        hardRequirementCategoryLocation,
			VacancyEvidence: "Екатеринбург",
		}},
	)
	if len(derived) != 0 {
		t.Fatalf("AI location requirement was accepted from generic vacancy area: %+v", derived)
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

func TestDeriveHardRequirementsIsDeterministicAndConservative(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	candidate := CandidateContext{
		Skills:   "Python, Django",
		Location: "Екатеринбург",
	}
	vacancy := Vacancy{
		Area:           NamedObject{Name: "Ташкент"},
		WorkExperience: "Опыт 3-6 лет",
	}
	description := "Требуется FastAPI. Kubernetes будет плюсом. Обязательное образование (не указано в вакансии). Офис в Ташкенте."

	requirements := []HardRequirementCandidate{
		{Requirement: "FastAPI", Category: hardRequirementCategorySkill, VacancyEvidence: "Требуется FastAPI"},
		{Requirement: "3+ года", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Опыт 3-6 лет"},
		{Requirement: "офис в Ташкенте", Category: hardRequirementCategoryLocation, VacancyEvidence: "Ташкент"},
		{Requirement: "высшее образование", Category: hardRequirementCategoryEducation, VacancyEvidence: "Требуется высшее образование"},
		{Requirement: "лицензия", Category: hardRequirementCategoryLicense, VacancyEvidence: "обязательная лицензия"},
		{Requirement: "гражданство РФ", Category: hardRequirementCategoryCitizenship, VacancyEvidence: "гражданство РФ"},
		{Requirement: "Kubernetes", Category: hardRequirementCategorySkill, VacancyEvidence: "Kubernetes"},
	}
	derived := deriveHardRequirements(candidate, vacancy, description, requirements)
	if len(derived) != 2 {
		t.Fatalf("unexpected derived requirements, optional/unsupported items were not discarded: %+v", derived)
	}

	byRequirement := make(map[string]HardRequirementEvaluation, len(derived))
	for _, requirement := range derived {
		byRequirement[requirement.Requirement] = requirement
	}
	for _, requirement := range []string{"FastAPI", "офис в Ташкенте"} {
		if byRequirement[requirement].Status != hardRequirementStatusUnknown {
			t.Fatalf("%q should be unknown: %+v", requirement, byRequirement[requirement])
		}
	}
	if _, ok := byRequirement["3+ года"]; ok {
		t.Fatal("generic HH WorkExperience was incorrectly turned into a hard requirement")
	}
	if _, ok := byRequirement["высшее образование"]; ok {
		t.Fatal("education without supported vacancy evidence should have been discarded")
	}
}

func TestDeriveHardRequirementStatusesForKnownFacts(t *testing.T) {
	candidate := CandidateContext{Skills: "Python, Django", Location: "Екатеринбург"}
	vacancy := Vacancy{Area: NamedObject{Name: "Екатеринбург"}}
	description := "Python обязателен. Офис в Екатеринбурге. Kafka обязателен."

	derived := deriveHardRequirements(candidate, vacancy, description, []HardRequirementCandidate{
		{Requirement: "Python", Category: hardRequirementCategorySkill, VacancyEvidence: "Python обязателен"},
		{Requirement: "офис в Екатеринбурге", Category: hardRequirementCategoryLocation, VacancyEvidence: "Офис в Екатеринбурге"},
		{Requirement: "Kafka", Category: hardRequirementCategorySkill, VacancyEvidence: "Kafka обязателен"},
	})
	if len(derived) != 3 {
		t.Fatalf("unexpected derived requirement count: %+v", derived)
	}
	if derived[0].Status != hardRequirementStatusMet || derived[1].Status != hardRequirementStatusMet {
		t.Fatalf("known skill/location facts were not met: %+v", derived)
	}
	if derived[2].Status != hardRequirementStatusUnknown {
		t.Fatalf("missing skill must remain unknown: %+v", derived[2])
	}
}

func TestUnsupportedRequirementDoesNotFailWholeEvaluation(t *testing.T) {
	derived := deriveHardRequirements(
		CandidateContext{Skills: "Python"},
		Vacancy{},
		"Python обязателен.",
		[]HardRequirementCandidate{
			{Requirement: "образование", Category: hardRequirementCategoryEducation, VacancyEvidence: "образование обязательно"},
			{Requirement: "Python", Category: hardRequirementCategorySkill, VacancyEvidence: "Python обязателен"},
		},
	)
	if len(derived) != 1 || derived[0].Requirement != "Python" {
		t.Fatalf("unsupported requirement poisoned valid extraction: %+v", derived)
	}
}

func TestEvaluateVacancyInvalidJSONStillRetries(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(&bytes.Buffer{}, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() { aiRetryDelay = previousDelay })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, aiCompletionResponse("not json"))
			return
		}
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[{"requirement":"Python","category":"skill","vacancy_evidence":"Python обязателен"}]}`))
	}))
	defer server.Close()

	client := NewAIClient(context.Background(), server.URL, "test-model", "", time.Second, time.Second, 2)
	evaluation, err := client.EvaluateVacancy(vacancyEvaluationInput{
		Candidate:   CandidateContext{Skills: "Python"},
		Description: "Python обязателен",
	})
	if err != nil {
		t.Fatalf("invalid JSON was not retried: %v", err)
	}
	if calls.Load() != 2 || len(evaluation.HardRequirements) != 1 {
		t.Fatalf("unexpected retry result: calls=%d evaluation=%+v", calls.Load(), evaluation)
	}
}
