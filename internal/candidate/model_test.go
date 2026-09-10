package candidate

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func testMetadata(source KnowledgeSource, status TruthStatus, at time.Time, confidence *float64) KnowledgeMetadata {
	meta := KnowledgeMetadata{
		Confidence: confidence, TruthStatus: status,
		Sources:  []KnowledgeSourceRecord{{Type: source, Evidence: []string{"synthetic evidence"}, ObservedAt: &at}},
		Evidence: []string{"synthetic evidence"}, CreatedAt: at, UpdatedAt: at,
	}
	if status == TruthStatusConfirmed {
		meta.ConfirmedAt = &at
	}
	return meta
}

func TestSourcePriorityAndTruthAreIndependent(t *testing.T) {
	if CompareSourcePriority(CandidateSourceUserConfirmed, CandidateSourceHHResume) <= 0 ||
		CompareSourcePriority(CandidateSourceHHResume, CandidateSourceGithubVerified) <= 0 ||
		CompareSourcePriority(CandidateSourceGithubVerified, CandidateSourceDerived) <= 0 {
		t.Fatal("source priority changed")
	}
	high := 0.99
	meta := testMetadata(KnowledgeSourceDerived, TruthStatusHypothesis, time.Now().UTC(), &high)
	if CanExposeToEmployer(meta) {
		t.Fatal("high-confidence derived knowledge must remain unexposable")
	}
	confirmed := testMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, time.Now().UTC(), nil)
	if !CanExposeToEmployer(confirmed) {
		t.Fatal("confirmed knowledge was not exposable")
	}
}

func TestUnknownIsDistinctFromZeroAndRestrictedByProjection(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	unknown := CandidateSkillDetailed{ID: "unknown-k8s", Name: "Kubernetes", Level: SkillLevelUnknown, KnowledgeMetadata: testMetadata(KnowledgeSourceUnknown, TruthStatusUnknown, at, nil)}
	confirmed := CandidateSkillDetailed{ID: "confirmed-python", Name: "Python", Level: SkillLevelWorking, KnowledgeMetadata: testMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, nil)}
	value := Candidate{Version: 1, ID: "synthetic", Skills: []CanonicalCandidateSkill{
		{ID: unknown.ID, Name: "kubernetes", DisplayName: unknown.Name, Level: unknown.Level, Metadata: unknown.KnowledgeMetadata, State: CanonicalClaimActive, SourceAssertions: []CanonicalCandidateSkillAssertion{{ID: unknown.ID, Name: unknown.Name, Level: unknown.Level, Metadata: unknown.KnowledgeMetadata}}},
		{ID: confirmed.ID, Name: "python", DisplayName: confirmed.Name, Level: confirmed.Level, Metadata: confirmed.KnowledgeMetadata, State: CanonicalClaimActive, SourceAssertions: []CanonicalCandidateSkillAssertion{{ID: confirmed.ID, Name: confirmed.Name, Detailed: true, Level: confirmed.Level, Metadata: confirmed.KnowledgeMetadata}}},
	}}
	view, err := CanonicalEmployerSafeProjection(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Skills) != 1 || view.Skills[0].Name != "Python" {
		t.Fatalf("unsafe skill projected: %+v", view.Skills)
	}
	if unknown.Level == SkillLevelBasic || unknown.Level == "" {
		t.Fatal("unknown level collapsed into a known value")
	}
}

func TestCanonicalJSONPreservesPresenceAndExperienceMonths(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := Candidate{Version: 1, ID: "candidate-json", Profile: CanonicalProfileSnapshot{TotalExperienceMonths: ProfileIntFact{Value: 11, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: at, Evidence: []string{"structured HH value"}}}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Candidate
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, roundTrip) {
		t.Fatalf("candidate JSON round trip changed value: %s", raw)
	}
	if roundTrip.Profile.TotalExperienceMonths.Value != 11 {
		t.Fatal("11 months was rounded or lost")
	}
}

func TestCanonicalValidationRejectsDuplicateIDs(t *testing.T) {
	value := Candidate{Version: 1, ID: "candidate-validation", Skills: []CanonicalCandidateSkill{{ID: "same", Name: "Python"}, {ID: "same", Name: "Go"}}}
	if err := value.Validate(); err == nil {
		t.Fatal("duplicate canonical IDs must fail intrinsic validation")
	}
}
