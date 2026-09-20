package api

import (
	"testing"
	"time"
)

func TestDecodeVacancyRelationsPreservesProviderRelationIDs(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    []string
		wantNil bool
	}{
		{name: "provider relation IDs", json: `{"id":"42","relations":["favorited","got_response"]}`, want: []string{"favorited", "got_response"}},
		{name: "empty array", json: `{"id":"42","relations":[]}`, want: []string{}},
		{name: "null", json: `{"id":"42","relations":null}`, wantNil: true},
		{name: "missing", json: `{"id":"42"}`, wantNil: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value wireVacancy
			if err := decodeWire([]byte(test.json), &value); err != nil {
				t.Fatal(err)
			}
			if test.wantNil {
				if value.Relations != nil {
					t.Fatalf("relations=%#v, want nil", value.Relations)
				}
				return
			}
			if len(value.Relations) != len(test.want) {
				t.Fatalf("relations=%#v, want %#v", value.Relations, test.want)
			}
			for index, relationID := range test.want {
				if value.Relations[index] != relationID {
					t.Fatalf("relations=%#v, want %#v", value.Relations, test.want)
				}
			}
		})
	}
}

func TestMapVacancyWireUsesGotResponseAsPositiveRelationEvidence(t *testing.T) {
	var wireValue wireVacancy
	if err := decodeWire([]byte(`{"id":"42","relations":["favorited","got_response"]}`), &wireValue); err != nil {
		t.Fatal(err)
	}
	value, err := mapVacancyWire(wireValue)
	if err != nil {
		t.Fatal(err)
	}
	if value.AlreadyResponded == nil || !*value.AlreadyResponded || value.AlreadyRespondedEvidence != "vacancy.relations.got_response" {
		t.Fatalf("response relation was not mapped authoritatively: %+v", value)
	}
}

func TestMapVacancyWirePreservesApplicantPreflightResources(t *testing.T) {
	value, err := mapVacancyWire(wireVacancy{
		ID: "42", Relations: []string{"favorited", "got_response"},
		NegotiationsURL: "/negotiations?vacancy_id=42", SuitableResumesURL: "/vacancies/42/suitable_resumes",
		Type: wireNamed{ID: "open"}, ApplyAlternateURL: "https://hh.example/applicant/vacancy_response?vacancyId=42",
		ClosedForApplicants: boolPtr(false), QuickResponsesAllowed: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Relations) != 2 || value.Relations[1] != "got_response" || value.NegotiationsURL == "" || value.SuitableResumesURL == "" || value.TypeID != "open" || !value.TypeIDKnown || value.ApplyAlternateURL == "" || !value.ClosedForApplicantsKnown || value.ClosedForApplicants || !value.QuickResponsesAllowedKnown || !value.QuickResponsesAllowed {
		t.Fatalf("applicant preflight fields were not preserved: %+v", value)
	}
}

func TestMapVacancyWireDoesNotFabricateNegativeRelationFromNonResponseIDs(t *testing.T) {
	var wireValue wireVacancy
	if err := decodeWire([]byte(`{"id":"42","relations":["favorited"]}`), &wireValue); err != nil {
		t.Fatal(err)
	}
	value, err := mapVacancyWire(wireValue)
	if err != nil {
		t.Fatal(err)
	}
	if value.AlreadyResponded != nil || value.AlreadyRespondedEvidence != "" {
		t.Fatalf("non-response relation became a negative response state: %+v", value)
	}
}

func TestMapVacancyWireNormalizesStructuredFields(t *testing.T) {
	value, err := mapVacancyWire(wireVacancy{
		ID: "7", Name: "Python backend", Description: "Integrations", Employer: wireNamed{Name: "Fixture employer"},
		Area: wireNamed{Name: "Yekaterinburg"}, Address: wireAddress{Raw: "Lenina street"},
		Salary:    wireSalary{From: "70000", To: "100000", Currency: "RUR"},
		KeySkills: wireNames{"Python", "Django"}, ProfessionalRoles: wireNames{"Developer"},
		Experience: wireNamed{ID: "between1And3", Name: "1-3 years"}, Employment: wireNamed{Name: "Full time"},
		Schedule: wireNamed{Name: "Flexible"}, WorkFormat: wireWorkFormats{{Name: "Remote"}},
		PublishedAt: "2026-09-17T08:00:00Z", Archived: boolPtr(true), ResponseLetterRequired: boolPtr(false), UserTestPresent: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != 7 || value.Title != "Python backend" || value.Company != "Fixture employer" || value.Location != "Yekaterinburg" || value.AreaName != "Yekaterinburg" || value.Address != "Lenina street" || value.Salary != "70000–100000" || value.Currency != "RUR" || value.WorkFormat != "remote" || value.Experience != "between1And3" || value.PublishedAt.IsZero() || !value.Archived || !value.ArchivedKnown || value.ResponseLetterRequiredKnown != true || value.UserTestPresentKnown != true {
		t.Fatalf("value=%+v", value)
	}
}

func TestMapResumeWireKeepsAbsentFieldsUnknown(t *testing.T) {
	value, err := mapResumeWire(wireResume{ID: "resume-unknown", Title: "Support specialist", UpdatedAt: ""})
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "resume-unknown" || value.Title != "Support specialist" || value.UpdatedAt != (time.Time{}) || value.Skills != nil || value.Area != "" || value.Salary != "" {
		t.Fatalf("value=%+v", value)
	}
}

func TestMapResumeWireUsesStructuredSkillSetSalaryAmountAndTotalExperience(t *testing.T) {
	value, err := mapResumeWire(wireResume{
		ID: "resume-structured", SkillsText: "free-form Python, Django", SkillSet: wireNames{"Python", "Django"},
		Salary: wireSalary{Amount: 110000, Currency: "RUR"}, TotalExperience: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Skills) != 2 || value.Skills[0] != "Python" || value.Salary != "110000" || value.Currency != "RUR" || value.TotalExperienceMonths != 42 || !value.TotalExperienceMonthsKnown || value.Experience != "42" {
		t.Fatalf("value=%+v", value)
	}
}

func TestMapResumeWireDoesNotPromoteFreeTextSkills(t *testing.T) {
	value, err := mapResumeWire(wireResume{ID: "resume-text-only", SkillsText: "Python, Django"})
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Skills) != 0 {
		t.Fatalf("free-text skills became structured skills: %+v", value.Skills)
	}
}

func TestCanonicalWorkFormatReturnsHybridWhenRemoteAndOfficeArePresent(t *testing.T) {
	if got := canonicalWorkFormat(wireWorkFormats{
		{ID: "REMOTE", Name: "Из дома"},
		{Code: "ON_SITE", Name: "На месте работодателя"},
	}); got != "hybrid" {
		t.Fatalf("work format=%q, want hybrid", got)
	}
}

func boolPtr(value bool) *bool { return &value }
