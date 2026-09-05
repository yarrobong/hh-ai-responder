package main

import (
	"strings"
	"testing"
	"time"
)

func TestGenericHHExperienceIsSoftContext(t *testing.T) {
	for _, work := range []string{"between1And3", "between3And6", "moreThan6"} {
		for _, months := range []int{0, 11} {
			got := localStructuredHardRequirements(VacancyPreflight{WorkExperienceKnown: true, WorkExperience: work}, CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: months})
			if len(got) != 0 {
				t.Fatalf("HH WorkExperience %q created a hard requirement for %d months: %+v", work, months, got)
			}
		}
	}
}

func TestCandidateContextCarriesResumeFacts(t *testing.T) {
	r := &HHAIResponder{
		resumeFacts: ResumeFacts{
			EducationKnown:             true,
			EducationLevel:             educationLevelHigher,
			EducationDetails:           "structured level",
			TotalExperienceMonthsKnown: true,
			TotalExperienceMonths:      18,
		},
	}
	candidate := r.candidateContext(ResumeItem{})
	if !candidate.EducationKnown || candidate.EducationLevel != educationLevelHigher || candidate.EducationDetails != "structured level" {
		t.Fatalf("education facts were not carried to CandidateContext: %+v", candidate)
	}
	if !candidate.TotalExperienceMonthsKnown || candidate.TotalExperienceMonths != 18 {
		t.Fatalf("experience facts were not carried to CandidateContext: %+v", candidate)
	}
}

func TestNoExperienceDoesNotCreateGenericRequirement(t *testing.T) {
	for _, value := range []string{"noExperience", "Без опыта", "Опыт не требуется"} {
		if got := localStructuredHardRequirements(VacancyPreflight{WorkExperienceKnown: true, WorkExperience: value}, CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 100}); len(got) != 0 {
			t.Fatalf("%q created a requirement: %+v", value, got)
		}
	}
}

func TestDescriptionSpecificExperienceDoesNotUseTotalExperience(t *testing.T) {
	got := deriveHardRequirements(
		CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 50},
		Vacancy{WorkExperience: "Опыт 1-3 года"},
		"Нужно 3 года SRE.",
		[]HardRequirementCandidate{{Requirement: "3 года SRE", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Нужно 3 года SRE"}},
	)
	if len(got) != 1 || got[0].Status != hardRequirementStatusUnknown {
		t.Fatalf("role-specific experience was inferred from total: %+v", got)
	}
}

func TestDescriptionExperienceUsesExplicitTotalDurationPolicy(t *testing.T) {
	candidate := CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 11}
	softGap := deriveHardRequirements(candidate, Vacancy{WorkExperience: "between1And3"}, "Опыт от 1 года", []HardRequirementCandidate{{
		Requirement: "опыт от 1 года", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Опыт от 1 года",
	}})
	if len(softGap) != 1 || softGap[0].Status != hardRequirementStatusUnknown || !softGap[0].Soft {
		t.Fatalf("one-year description requirement was not a soft gap: %+v", softGap)
	}
	largeGap := deriveHardRequirements(candidate, Vacancy{WorkExperience: "between1And3"}, "Опыт от 3 лет", []HardRequirementCandidate{{
		Requirement: "опыт от 3 лет", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Опыт от 3 лет",
	}})
	if len(largeGap) != 1 || largeGap[0].Status != hardRequirementStatusMissing || largeGap[0].Soft {
		t.Fatalf("three-year generic requirement was not a hard mismatch: %+v", largeGap)
	}
	roleSpecific := deriveHardRequirements(CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 50}, Vacancy{}, "Нужно 3 года SRE", []HardRequirementCandidate{{
		Requirement: "3 года SRE", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Нужно 3 года SRE",
	}})
	if len(roleSpecific) != 1 || roleSpecific[0].Status != hardRequirementStatusUnknown {
		t.Fatalf("role-specific requirement used generic total experience: %+v", roleSpecific)
	}
}

func TestEducationRequirementsUseTrustedLevel(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		requirement string
		wantStatus  string
	}{
		{name: "higher meets higher", level: educationLevelHigher, requirement: "высшее образование", wantStatus: hardRequirementStatusMet},
		{name: "secondary professional misses higher", level: educationLevelSecondaryProfessional, requirement: "высшее образование", wantStatus: hardRequirementStatusMissing},
		{name: "secondary professional meets alternative", level: educationLevelSecondaryProfessional, requirement: "высшее или среднее профессиональное", wantStatus: hardRequirementStatusMet},
		{name: "incomplete higher meets explicit alternative", level: educationLevelIncompleteHigher, requirement: "высшее или незаконченное высшее", wantStatus: hardRequirementStatusMet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := deriveHardRequirements(
				CandidateContext{EducationKnown: true, EducationLevel: test.level, EducationDetails: "structured education"},
				Vacancy{},
				test.requirement,
				[]HardRequirementCandidate{{Requirement: test.requirement, Category: hardRequirementCategoryEducation, VacancyEvidence: test.requirement}},
			)
			if len(got) != 1 || got[0].Status != test.wantStatus || !strings.HasPrefix(got[0].CandidateEvidence, "Candidate education:") {
				t.Fatalf("got requirements=%+v, want status=%s", got, test.wantStatus)
			}
		})
	}
}

func TestEducationSpecializationWithoutStructuredProfileIsUnknown(t *testing.T) {
	got := deriveHardRequirements(
		CandidateContext{EducationKnown: true, EducationLevel: educationLevelHigher},
		Vacancy{},
		"Высшее образование по ИБ.",
		[]HardRequirementCandidate{{Requirement: "высшее образование по ИБ", Category: hardRequirementCategoryEducation, VacancyEvidence: "Высшее образование по ИБ"}},
	)
	if len(got) != 1 || got[0].Status != hardRequirementStatusUnknown {
		t.Fatalf("specialization was inferred from level: %+v", got)
	}
}

func TestParseResumeFactsCalculatesAndMergesExperiencePeriods(t *testing.T) {
	body := []byte(`{"redirectConfig":{},"applicantResume":{"experience":[
{"startDate":"2020-01-01","endDate":"2021-01-01","position":"One","description":"text from resume"},
{"startDate":"2020-06-01","endDate":"2022-01-01","position":"Two","description":"another text"}
]}}<html>page continues</html>`)
	facts, err := parseResumeFacts(body, time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !facts.TotalExperienceMonthsKnown || facts.TotalExperienceMonths != 24 {
		t.Fatalf("overlapping periods were double-counted: %+v", facts)
	}
	if strings.Contains(facts.EducationDetails, "text from resume") {
		t.Fatalf("experience free text became education evidence: %+v", facts)
	}
}

func TestParseResumeFactsCalculatesCurrentEmployment(t *testing.T) {
	body := []byte(`{"redirectConfig":{},"applicantResume":{"experience":[{"startDate":"2024-01-01","endDate":null}]}}`)
	facts, err := parseResumeFacts(body, time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !facts.TotalExperienceMonthsKnown || facts.TotalExperienceMonths != 18 {
		t.Fatalf("current employment duration: %+v", facts)
	}
}

func TestParseResumeFactsFailsClosedForMalformedOrPartialDates(t *testing.T) {
	for _, startDate := range []string{"2020-01", "not-a-date"} {
		body := []byte(`{"redirectConfig":{},"applicantResume":{"experience":[{"startDate":"` + startDate + `","endDate":"2021-01-01"}]}}`)
		facts, err := parseResumeFacts(body, time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if facts.TotalExperienceMonthsKnown {
			t.Fatalf("invalid date %q produced known duration: %+v", startDate, facts)
		}
	}
}

func TestParseResumeFactsPrefersStructuredTotalExperience(t *testing.T) {
	body := []byte(`{"redirectConfig":{},"applicantResume":{"totalExperienceMonths":18,"experience":[{"startDate":"not-a-date"}]}}`)
	facts, err := parseResumeFacts(body, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !facts.TotalExperienceMonthsKnown || facts.TotalExperienceMonths != 18 {
		t.Fatalf("structured total experience was not preferred: %+v", facts)
	}
}

func TestParseResumeFactsExtractsUnambiguousEducationLevel(t *testing.T) {
	body := []byte(`{"redirectConfig":{},"applicantResume":{"educationLevel":"secondaryProfessional","education":[{"specialty":"Техник","organization":"Колледж"}]}}`)
	facts, err := parseResumeFacts(body, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !facts.EducationKnown || facts.EducationLevel != educationLevelSecondaryProfessional {
		t.Fatalf("education level was not extracted: %+v", facts)
	}
	if !strings.Contains(facts.EducationDetails, "Колледж") {
		t.Fatalf("structured education evidence missing: %+v", facts)
	}
}
