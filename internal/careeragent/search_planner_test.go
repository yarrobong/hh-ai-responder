package careeragent

import (
	"encoding/json"
	"testing"
)

func eligibleFamilyPresent(families []EligibleSearchFamily, want RoleFamily) bool {
	return eligibleFamilyByName(families, want) != nil
}

func eligibleFamilyByName(families []EligibleSearchFamily, want RoleFamily) *EligibleSearchFamily {
	for index := range families {
		if families[index].Family == want {
			return &families[index]
		}
	}
	return nil
}

func TestDeriveEligibleSearchFamiliesPromotesStrongSecondaryWebBackend(t *testing.T) {
	resume := ResumeProfile{
		ID:      "python",
		Title:   "Backend-разработчик (Python/Django) / автоматизация и интеграции",
		Skills:  []string{"Python", "Django Framework", "PHP", "Laravel", "REST API"},
		Enabled: true,
	}
	families := DeriveEligibleSearchFamilies(resume)
	if !eligibleFamilyPresent(families, RoleFamilyPythonBackend) {
		t.Fatalf("Python backend was not eligible: %+v", families)
	}
	web := eligibleFamilyByName(families, RoleFamilyWebBackend)
	if web == nil || len(web.Evidence) == 0 {
		t.Fatalf("strong secondary WEB_BACKEND evidence was not promoted: %+v", families)
	}
}

func TestDeriveEligibleSearchFamiliesDoesNotPromoteGenericSecondary(t *testing.T) {
	resume := ResumeProfile{
		ID: "support", Title: "Технический специалист",
		Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true,
	}
	for _, family := range DeriveEligibleSearchFamilies(resume) {
		if family.Family == RoleFamilyWebBackend || family.Family == RoleFamilySystemAdmin {
			t.Fatalf("generic evidence created an unsupported search family: %+v", family)
		}
	}
}

func TestSearchProfileMetadataIsOptionalForLegacyJSON(t *testing.T) {
	var profile SearchProfile
	if err := json.Unmarshal([]byte(`{"id":"search-1","resume_id":"r","query":"Python backend","reason":"resume role/title","params":{}}`), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID != "search-1" || profile.Query != "Python backend" || profile.ProfileType != "" {
		t.Fatalf("legacy profile was not accepted: %+v", profile)
	}
}
