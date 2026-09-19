package api

import (
	"testing"
	"time"
)

func TestMapVacancyWireNormalizesStructuredFields(t *testing.T) {
	value, err := mapVacancyWire(wireVacancy{
		ID: "7", Name: "Python backend", Description: "Integrations", Employer: wireNamed{Name: "Fixture employer"},
		Area: wireNamed{Name: "Yekaterinburg"}, Address: wireAddress{Raw: "Lenina street"},
		Salary:    wireSalary{From: "70000", To: "100000", Currency: "RUR"},
		KeySkills: wireNames{"Python", "Django"}, ProfessionalRoles: wireNames{"Developer"},
		Experience: wireNamed{ID: "between1And3", Name: "1-3 years"}, Employment: wireNamed{Name: "Full time"},
		Schedule: wireNamed{Name: "Flexible"}, WorkFormat: wireNames{"Remote"},
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
	if got := canonicalWorkFormat(wireNames{"REMOTE", "ON_SITE"}); got != "hybrid" {
		t.Fatalf("work format=%q, want hybrid", got)
	}
}

func boolPtr(value bool) *bool { return &value }
