package vacancy_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/vacancy"
)

func TestVacancyJSONCompatibility(t *testing.T) {
	amount := 120000
	value := vacancy.Vacancy{
		ID: 42, Name: "Python developer", ExternalID: "hh-42",
		Requirements: []string{"Python"}, Skills: []string{"Django"},
		Area:                vacancy.NamedObject{Name: "Екатеринбург"},
		Company:             vacancy.Company{ID: 7, Name: "Example", CompanySiteURL: "https://example.test"},
		Compensation:        vacancy.Compensation{To: &amount, Currency: "RUR"},
		TotalResponsesCount: 0, TotalResponsesCountKnown: true,
		DataCompleteness:       vacancy.DataCompletenessPartial,
		MatchResult:            &vacancy.MatchResult{Score: 82, Recommendation: &vacancy.ApplicationRecommendation{Decision: vacancy.RecommendationMaybe, Reason: "review"}},
		ReconciliationEvidence: []vacancy.ReconciliationEvidence{{Method: "fixture", Source: "hh", Confidence: 0.8, ReconciledAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)}},
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"id":42`, `"vacancyId":42`, `"totalResponsesCount":0`, `"companySiteUrl":"https://example.test"`, `"match_result"`, `"data_completeness":"partial"`, `"reconciliation_evidence"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("marshal missing %s: %s", field, text)
		}
	}

	var decoded vacancy.Vacancy
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != value.ID || !decoded.TotalResponsesCountKnown || decoded.TotalResponsesCount != 0 {
		t.Fatalf("round trip changed id/count semantics: %+v", decoded)
	}
	if decoded.MatchResult == nil || decoded.MatchResult.Recommendation == nil || decoded.DataCompleteness != vacancy.DataCompletenessPartial || len(decoded.ReconciliationEvidence) != 1 {
		t.Fatalf("round trip dropped embedded vacancy values: %+v", decoded)
	}
}

func TestVacancyLegacyIDsAndResponseCount(t *testing.T) {
	tests := []struct {
		name, raw string
		id, count int
		known     bool
	}{
		{name: "legacy id", raw: `{"vacancyId":7,"totalResponsesCount":0}`, id: 7, known: true},
		{name: "domain id wins", raw: `{"id":8,"vacancyId":7}`, id: 8},
		{name: "absent count", raw: `{"vacancyId":9}`, id: 9},
		{name: "null count", raw: `{"vacancyId":10,"totalResponsesCount":null}`, id: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value vacancy.Vacancy
			if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
				t.Fatal(err)
			}
			if value.ID != test.id || value.TotalResponsesCount != test.count || value.TotalResponsesCountKnown != test.known {
				t.Fatalf("got id=%d count=%d known=%t", value.ID, value.TotalResponsesCount, value.TotalResponsesCountKnown)
			}
		})
	}
}

func TestFormatCompensationCompatibility(t *testing.T) {
	from, to := 80000, 120000
	tests := []struct {
		name, want string
		value      *vacancy.Compensation
	}{
		{name: "range", want: "80000-120000 RUR", value: &vacancy.Compensation{From: &from, To: &to, Currency: "RUR"}},
		{name: "from", want: "80000+ RUR", value: &vacancy.Compensation{From: &from, Currency: "RUR"}},
		{name: "to", want: "0-120000 RUR", value: &vacancy.Compensation{To: &to, Currency: "RUR"}},
		{name: "nil", want: "", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vacancy.FormatCompensation(test.value); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
