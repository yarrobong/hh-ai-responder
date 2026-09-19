package careeragent

import (
	"encoding/json"
	"strings"
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

func TestPlanSearchesCoversEligibleFamiliesBeforeVariants(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "automation", Title: "Инженер внедрения и интеграций", Skills: []string{"API-интеграции", "Webhooks"}, Enabled: true},
		{ID: "python", Title: "Backend-разработчик (Python/Django)", Skills: []string{"Python", "Django Framework", "PHP", "Laravel"}, Enabled: true},
		{ID: "support", Title: "Технический специалист", Skills: []string{"Техническая поддержка", "Диагностика неисправностей"}, Enabled: true},
	}
	profiles := PlanSearches(resumes, CandidateSignals{}, SearchConstraints{MaxProfiles: 4, SearchPeriodDays: 7})
	seen := map[RoleFamily]bool{}
	for _, profile := range profiles {
		seen[profile.RoleFamily] = true
	}
	for _, family := range []RoleFamily{RoleFamilyAutomationIntegrations, RoleFamilyPythonBackend, RoleFamilyWebBackend, RoleFamilyTechSupport} {
		if !seen[family] {
			t.Fatalf("eligible family did not receive a profile: %s; profiles=%+v", family, profiles)
		}
	}
}

func TestPlanSearchesDoesNotCreateGenericOnlyProfiles(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "r", Title: "Разработчик", Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	if len(profiles) != 0 {
		t.Fatalf("generic skills created broad search profiles: %+v", profiles)
	}
}

func TestPlanSearchesCollapsesEquivalentQueriesAndBoundsLanguageVariants(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "python", Title: "Python backend", SearchHints: []string{"Python backend", "python   backend"}, Skills: []string{"Python", "Django"}, Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	if len(profiles) > 6 {
		t.Fatalf("language/technology variants exceeded bound: %d profiles=%+v", len(profiles), profiles)
	}
	keys := map[string]bool{}
	for _, profile := range profiles {
		key := canonicalSearchProfileKey(profile)
		if keys[key] {
			t.Fatalf("equivalent profile was not collapsed: %+v", profile)
		}
		keys[key] = true
	}
}

func TestCanonicalSearchProfileKeyIgnoresSourceResumeIDs(t *testing.T) {
	base := SearchProfile{RoleFamily: RoleFamilyTechSupport, Query: "technical support", SourceResumeIDs: []string{"a"}}
	other := base
	other.SourceResumeIDs = []string{"b"}
	if canonicalSearchProfileKey(base) != canonicalSearchProfileKey(other) {
		t.Fatalf("source provenance changed canonical identity: %q != %q", canonicalSearchProfileKey(base), canonicalSearchProfileKey(other))
	}
}

func TestCanonicalSearchProfileKeyNormalizesQueryCase(t *testing.T) {
	base := SearchProfile{RoleFamily: RoleFamilyPythonBackend, Query: "Python backend"}
	other := SearchProfile{RoleFamily: RoleFamilyPythonBackend, Query: "python   backend"}
	if canonicalSearchProfileKey(base) != canonicalSearchProfileKey(other) {
		t.Fatalf("query case or spacing changed canonical identity: %q != %q", canonicalSearchProfileKey(base), canonicalSearchProfileKey(other))
	}
}

func TestPlanSearchesUsesTrustedRoleVariantOnlyInsideEligibleFamily(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "support", Title: "Техническая поддержка", Enabled: true}}, CandidateSignals{Roles: []string{"специалист технической поддержки"}}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	foundVariant := false
	for _, profile := range profiles {
		if profile.RoleFamily == RoleFamilyTechSupport && strings.Contains(profile.Query, "специалист технической поддержки") {
			foundVariant = true
		}
	}
	if !foundVariant {
		t.Fatalf("trusted support role variant was not emitted: %+v", profiles)
	}

	unsupported := PlanSearches([]ResumeProfile{{ID: "support", Title: "Техническая поддержка", Enabled: true}}, CandidateSignals{Roles: []string{"системный аналитик"}}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range unsupported {
		if profile.RoleFamily == RoleFamilySystemAnalyst {
			t.Fatalf("unsupported signal activated a new family: %+v", unsupported)
		}
	}
}

func TestPlanSearchesKeepsRussianRoleAliases(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{
		{ID: "support", Title: "Специалист технической поддержки", Enabled: true},
		{ID: "implementation", Title: "Инженер внедрения", Enabled: true},
		{ID: "integration", Title: "Специалист по интеграциям", Enabled: true},
		{ID: "python", Title: "Python-разработчик", Skills: []string{"Python"}, Enabled: true},
		{ID: "backend", Title: "Backend-разработчик", Skills: []string{"PHP"}, Enabled: true},
	}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	queries := map[string]bool{}
	for _, profile := range profiles {
		queries[strings.ToLower(profile.Query)] = true
	}
	for _, query := range []string{"специалист технической поддержки", "инженер внедрения", "специалист по интеграциям", "python-разработчик", "backend-разработчик"} {
		if !queries[strings.ToLower(query)] {
			t.Fatalf("Russian role alias was not preserved: %q; profiles=%+v", query, profiles)
		}
	}
}

func TestPlanSearchesDoesNotExpandUnrelatedResumeTitleAcrossFamilies(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{
		ID: "automation", Title: "Специалист по автоматизации и интеграциям / инженер внедрения",
		Skills: []string{"Python", "Django", "React", "TypeScript", "Webhooks"}, Enabled: true,
	}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range profiles {
		if profile.RoleFamily != RoleFamilyAutomationIntegrations && strings.Contains(strings.ToLower(profile.Query), "автоматизац") {
			t.Fatalf("unrelated title leaked into another family: %+v", profile)
		}
	}
}

func TestBroadFallbackUsesOneTrustedPhraseWithoutUnvalidatedOR(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "r", Title: "Backend-разработчик", Skills: []string{"Python"}, Enabled: true}}, CandidateSignals{Roles: []string{"Backend-разработчик"}}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range profiles {
		if profile.ProfileType == SearchProfileBroadFallback && strings.Contains(profile.Query, " OR ") {
			t.Fatalf("unvalidated OR fallback was emitted: %+v", profile)
		}
	}
}

func TestBroadFallbackUsesOneProvenPhraseUntilORValidation(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "support", Title: "Техническая поддержка", Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range profiles {
		if profile.ProfileType == SearchProfileBroadFallback && strings.Contains(profile.Query, " OR ") {
			t.Fatalf("unvalidated provider OR semantics leaked into planner: %+v", profile)
		}
	}
}
