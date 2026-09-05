package main

import (
	"strings"
	"testing"
	"time"
)

func TestGenericHHExperienceRequirementsUseMinimumOnly(t *testing.T) {
	tests := []struct {
		name         string
		months       int
		known        bool
		work         string
		wantStatus   string
		wantEvidence string
	}{
		{name: "18 months meets 1-3", months: 18, known: true, work: "Опыт 1-3 года", wantStatus: hardRequirementStatusMet},
		{name: "8 months misses 1-3", months: 8, known: true, work: "Опыт 1-3 года", wantStatus: hardRequirementStatusMissing},
		{name: "50 months is not above range", months: 50, known: true, work: "Опыт 1-3 года", wantStatus: hardRequirementStatusMet},
		{name: "unknown duration is unknown", work: "Опыт 1-3 года", wantStatus: hardRequirementStatusUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := CandidateContext{TotalExperienceMonthsKnown: test.known, TotalExperienceMonths: test.months}
			got := localStructuredHardRequirements(VacancyPreflight{WorkExperienceKnown: true, WorkExperience: test.work}, candidate)
			if len(got) != 1 || got[0].Status != test.wantStatus {
				t.Fatalf("got requirements=%+v, want status=%s", got, test.wantStatus)
			}
			if test.wantStatus != hardRequirementStatusUnknown && !strings.Contains(got[0].CandidateEvidence, "Candidate total experience:") {
				t.Fatalf("trusted candidate evidence missing: %+v", got[0])
			}
		})
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

func TestGenericExperienceDerivationRequiresHHWorkExperienceEvidence(t *testing.T) {
	candidate := CandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 18}
	met, _ := deriveHardRequirementStatus(candidate, Vacancy{WorkExperience: "Опыт 1-3 года"}, HardRequirementCandidate{
		Requirement: "1-3 года опыта", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Опыт 1-3 года",
	})
	if met != hardRequirementStatusMet {
		t.Fatalf("generic HH requirement was not met: %s", met)
	}
	unknown, _ := deriveHardRequirementStatus(candidate, Vacancy{WorkExperience: "Опыт 1-3 года"}, HardRequirementCandidate{
		Requirement: "3 года SRE", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Нужно 3 года SRE",
	})
	if unknown != hardRequirementStatusUnknown {
		t.Fatalf("role-specific requirement used generic HH duration: %s", unknown)
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
