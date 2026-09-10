package runtime

import (
	"testing"
	"time"
)

func TestR51RootCandidateBoundaryKeepsSafeProjectionSemantics(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.TotalExperienceMonths = ProfileIntFact{Value: 11, ProfileFact: confirmedProfileFact(CandidateSourceHHResume, at)}
	profile.Skills = []CandidateSkill{
		{Name: "Python", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)},
		{Name: "Kubernetes", Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUnknown, Evidence: []string{"not established"}}},
	}
	profile.EmployerCommunicationPreferences.Salary = ProfileStringFact{Value: "от 120000", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}
	profile.WorkPreferences.Relocation = ProfileStringFact{Value: "не готов", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}

	canonical, _, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, CandidateID: "r51-synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := CanonicalEmployerSafeProjection(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Profile.TotalExperienceMonths.Value != 11 {
		t.Fatalf("experience months changed: %d", canonical.Profile.TotalExperienceMonths.Value)
	}
	if len(view.Profile.Skills) != 1 || view.Profile.Skills[0].Name != "Python" {
		t.Fatalf("unsafe or unknown skill entered employer projection: %+v", view.Profile.Skills)
	}
	if view.Profile.SalaryPreference != "от 120000" || view.Profile.Relocation != "не готов" {
		t.Fatalf("confirmed safe preferences were not projected: %+v", view.Profile)
	}
}
