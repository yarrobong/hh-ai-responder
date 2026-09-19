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
