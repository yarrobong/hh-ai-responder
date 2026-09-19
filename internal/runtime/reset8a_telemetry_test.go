package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	vacancyanalysis "hh-ai-responder/internal/usecase/vacancyanalysis"
)

func TestReset8ATelemetryIsOptionalAndBounded(t *testing.T) {
	value := HardRequirementEvaluation{
		Requirement:       "2 года AI/ML/NLP",
		Category:          hardRequirementCategoryExperienceYears,
		Status:            hardRequirementStatusUnknown,
		VacancyEvidence:   "Опыт в AI/ML/NLP не менее 2 лет.",
		CandidateEvidence: "not provided",
		Telemetry: &vacancyanalysis.RequirementTelemetry{
			SourceContext:               "Опыт в AI/ML/NLP не менее 2 лет.",
			SourceField:                 "description",
			ExtractionClassification:    "EXPLICIT_HARD",
			MandatoryCue:                "minimum_duration",
			CandidateEvidenceProvenance: "CANDIDATE_PROFILE",
			ExperienceClassification:    "TECHNOLOGY_SPECIFIC_DURATION",
			ClassificationDiagnostics:   []string{"role_marker:ai_ml_nlp"},
		},
	}
	trace := CareerAgentVacancyResult{LocalPolicyGate: ReasonHardUnknown, FinalReasonCode: ReasonHardUnknown}
	encoded, err := json.Marshal(struct {
		Requirement HardRequirementEvaluation `json:"requirement"`
		Trace       CareerAgentVacancyResult  `json:"trace"`
	}{Requirement: value, Trace: trace})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "cookie") || strings.Contains(strings.ToLower(string(encoded)), "authorization") {
		t.Fatalf("telemetry contains a secret-like field: %s", encoded)
	}
	if len([]rune(value.Telemetry.SourceContext)) > 240 {
		t.Fatalf("source context is unbounded: %d runes", len([]rune(value.Telemetry.SourceContext)))
	}

	var decoded struct {
		Requirement HardRequirementEvaluation `json:"requirement"`
		Trace       CareerAgentVacancyResult  `json:"trace"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Requirement.Telemetry == nil || decoded.Requirement.Telemetry.ExperienceClassification != "TECHNOLOGY_SPECIFIC_DURATION" {
		t.Fatalf("telemetry was not preserved: %+v", decoded.Requirement.Telemetry)
	}
	if decoded.Trace.LocalPolicyGate != ReasonHardUnknown {
		t.Fatalf("local policy gate=%q, want %q", decoded.Trace.LocalPolicyGate, ReasonHardUnknown)
	}
}

func TestReset8AOldRequirementJSONRemainsReadable(t *testing.T) {
	oldJSON := []byte(`{"requirement":"FastAPI","category":"skill","status":"unknown","vacancy_evidence":"FastAPI","candidate_evidence":"not provided"}`)
	var value HardRequirementEvaluation
	if err := json.Unmarshal(oldJSON, &value); err != nil {
		t.Fatal(err)
	}
	if value.Telemetry != nil {
		t.Fatalf("legacy JSON unexpectedly created telemetry: %+v", value.Telemetry)
	}
}

func TestReset8ARequirementTelemetryUsesActualCandidateSource(t *testing.T) {
	trusted := LegacyCandidateContext{
		Profile: CandidateProfile{Skills: []CandidateSkill{{
			Name:        "Redis",
			Level:       SkillLevelWorking,
			ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true},
		}}},
	}
	value := Vacancy{Description: "Redis / Redis Streams"}
	requirement := HardRequirementCandidate{Requirement: "Redis", Category: hardRequirementCategorySkill, VacancyEvidence: "Redis / Redis Streams"}
	derived := deriveHardRequirements(trusted, value, value.Description, []HardRequirementCandidate{requirement})
	if len(derived) != 1 || derived[0].Telemetry == nil {
		t.Fatalf("missing requirement telemetry: %+v", derived)
	}
	if derived[0].Telemetry.CandidateEvidenceProvenance != vacancyanalysis.CandidateEvidenceHHResume {
		t.Fatalf("provenance=%q, want %q", derived[0].Telemetry.CandidateEvidenceProvenance, vacancyanalysis.CandidateEvidenceHHResume)
	}

	legacy := LegacyCandidateContext{Skills: "Redis"}
	legacyDerived := deriveHardRequirements(legacy, value, value.Description, []HardRequirementCandidate{requirement})
	if len(legacyDerived) != 1 || legacyDerived[0].Telemetry == nil {
		t.Fatalf("missing legacy requirement telemetry: %+v", legacyDerived)
	}
	if legacyDerived[0].Telemetry.CandidateEvidenceProvenance != vacancyanalysis.CandidateEvidenceLegacyAggregate {
		t.Fatalf("legacy provenance=%q, want %q", legacyDerived[0].Telemetry.CandidateEvidenceProvenance, vacancyanalysis.CandidateEvidenceLegacyAggregate)
	}
}

func TestReset8ARequirementTelemetryClassifiesSpecificDuration(t *testing.T) {
	candidate := LegacyCandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 11}
	value := Vacancy{Description: "Опыт в AI/ML/NLP не менее 2 лет."}
	derived := deriveHardRequirements(candidate, value, value.Description, []HardRequirementCandidate{{
		Requirement: "2 года AI/ML/NLP", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: value.Description,
	}})
	if len(derived) != 1 || derived[0].Telemetry == nil {
		t.Fatalf("missing duration telemetry: %+v", derived)
	}
	if got := derived[0].Telemetry.ExperienceClassification; got != vacancyanalysis.ExperienceClassificationTechnology {
		t.Fatalf("experience classification=%q, want %q", got, vacancyanalysis.ExperienceClassificationTechnology)
	}
	if len([]rune(derived[0].Telemetry.SourceContext)) > 240 {
		t.Fatalf("source context is unbounded: %d runes", len([]rune(derived[0].Telemetry.SourceContext)))
	}
}

func TestReset8ALocalPolicyGateIsRecordedWithFinalReason(t *testing.T) {
	summary := RunSummaryResult{}
	trace := CareerAgentVacancyResult{}
	recordAIDecisionBreakdown(&summary, &trace, VacancyEvaluation{Score: 60, Apply: true, Recommendation: "APPLY"}, 65)
	if trace.LocalPolicyGate != ReasonScoreBelowThreshold {
		t.Fatalf("local policy gate=%q, want %q", trace.LocalPolicyGate, ReasonScoreBelowThreshold)
	}
	if trace.FinalReasonCode != "" {
		t.Fatalf("recordAIDecisionBreakdown unexpectedly changed final reason field: %q", trace.FinalReasonCode)
	}
}
