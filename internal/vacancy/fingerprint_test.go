package vacancy

import (
	"testing"
	"time"
)

func TestFingerprintsNormalizeEquivalentSourceValues(t *testing.T) {
	first := Vacancy{ExternalID: "42", Name: " Backend ", Description: "line\r\ntext", Requirements: []string{"Django", "Python"}, Skills: []string{"Go", "Python"}, Links: map[string]string{"desktop": "HTTPS://HH.RU/vacancy/42"}, PublishedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("plus5", 5*60*60))}
	second := first
	second.Name = "Backend"
	second.Description = "line\ntext"
	second.Requirements = []string{"Python", "Django"}
	second.Skills = []string{"Python", "Go"}
	second.Links = map[string]string{"desktop": "https://hh.ru/vacancy/42"}
	second.PublishedAt = first.PublishedAt.UTC()
	if got := Fingerprints(first); got != Fingerprints(second) {
		t.Fatalf("equivalent source states have different fingerprints: %#v %#v", got, Fingerprints(second))
	}
}

func TestFingerprintsSeparateMaterialAndNonMaterialChanges(t *testing.T) {
	base := Vacancy{ExternalID: "42", Name: "Backend", Description: "API integrations", TotalResponsesCountKnown: true, TotalResponsesCount: 1, ResponseURL: "https://hh.ru/apply/1"}
	responseChange := base
	responseChange.TotalResponsesCount = 2
	responseChange.ResponseURL = "https://hh.ru/apply/2"
	if Fingerprints(base).SourceFingerprint == Fingerprints(responseChange).SourceFingerprint {
		t.Fatal("provider source fingerprint ignored a provider response change")
	}
	if Fingerprints(base).MaterialFingerprint != Fingerprints(responseChange).MaterialFingerprint {
		t.Fatal("non-material response change changed material fingerprint")
	}

	materialChange := base
	materialChange.Description = "API integrations and support"
	if Fingerprints(base).MaterialFingerprint == Fingerprints(materialChange).MaterialFingerprint {
		t.Fatal("material description change did not change material fingerprint")
	}
	localChange := base
	localChange.MatchResult = &MatchResult{Score: 99}
	localChange.ApplicationRecommendation = &ApplicationRecommendation{Decision: RecommendationApply}
	if Fingerprints(base) != Fingerprints(localChange) {
		t.Fatal("local match/recommendation changed provider fingerprints")
	}
}

func TestFingerprintVersionBumpIsRequiredForProfessionalRoles(t *testing.T) {
	withoutRoles := Vacancy{ExternalID: "42", Name: "Backend", Description: "API"}
	withRoles := withoutRoles
	withRoles.ProfessionalRoles = []string{"96"}

	v1WithoutRoles, ok := FingerprintsForVersion(withoutRoles, LegacyFingerprintVersion)
	if !ok {
		t.Fatal("legacy fingerprint version is not supported")
	}
	v1WithRoles, ok := FingerprintsForVersion(withRoles, LegacyFingerprintVersion)
	if !ok {
		t.Fatal("legacy fingerprint version is not supported")
	}
	if v1WithoutRoles != v1WithRoles {
		t.Fatal("v1 compatibility projection unexpectedly included professional_roles")
	}

	v2WithoutRoles := Fingerprints(withoutRoles)
	v2WithRoles := Fingerprints(withRoles)
	if v2WithoutRoles == v2WithRoles {
		t.Fatal("current fingerprint ignored professional_roles")
	}
	if FingerprintVersion != 2 {
		t.Fatalf("FingerprintVersion=%d, want 2 after changing hash input", FingerprintVersion)
	}
}

func TestFingerprintVersionRejectsUnknownContract(t *testing.T) {
	if pair, ok := FingerprintsForVersion(Vacancy{ExternalID: "42"}, 999); ok || pair != (FingerprintPair{}) {
		t.Fatalf("unknown fingerprint version returned %#v, supported=%t", pair, ok)
	}
}

func TestObservationFingerprintChangesBaselinesAlgorithmUpgrade(t *testing.T) {
	withoutRoles := Vacancy{ExternalID: "42", Name: "Backend", Description: "API"}
	withRoles := withoutRoles
	withRoles.ProfessionalRoles = []string{"96"}
	legacy, ok := FingerprintsForVersion(withoutRoles, LegacyFingerprintVersion)
	if !ok {
		t.Fatal("legacy fingerprint version is not supported")
	}
	current := Fingerprints(withRoles)
	legacyCurrent, ok := FingerprintsForVersion(withRoles, LegacyFingerprintVersion)
	if !ok {
		t.Fatal("legacy fingerprint version is not supported")
	}
	previous := Freshness{FingerprintVersion: LegacyFingerprintVersion, SourceFingerprint: legacy.SourceFingerprint, MaterialFingerprint: legacy.MaterialFingerprint}
	sourceChanged, materialChanged := ObservationFingerprintChanges(previous, current, legacyCurrent, true)
	if sourceChanged || materialChanged {
		t.Fatalf("role-only enrichment created a fake change: source=%t material=%t", sourceChanged, materialChanged)
	}

	changed := withRoles
	changed.Description = "API and support"
	legacyChanged, ok := FingerprintsForVersion(changed, LegacyFingerprintVersion)
	if !ok {
		t.Fatal("legacy fingerprint version is not supported")
	}
	sourceChanged, materialChanged = ObservationFingerprintChanges(previous, Fingerprints(changed), legacyChanged, true)
	if !sourceChanged || !materialChanged {
		t.Fatalf("real v1-known provider change was hidden: source=%t material=%t", sourceChanged, materialChanged)
	}
}
