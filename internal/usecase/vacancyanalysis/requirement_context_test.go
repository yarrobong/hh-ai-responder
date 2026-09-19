package vacancyanalysis

import "testing"

func TestRequirementContextClassificationUsesBoundedCues(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		evidence  string
		wantClass string
		wantCue   string
	}{
		{name: "Russian desirable", source: "Знание Kafka желательно", evidence: "Знание Kafka желательно", wantClass: RequirementExtractionPreference, wantCue: "желательно"},
		{name: "Russian plus", source: "Опыт Kubernetes будет плюсом", evidence: "Опыт Kubernetes будет плюсом", wantClass: RequirementExtractionPreference, wantCue: "будет плюсом"},
		{name: "Russian welcomed", source: "Опыт с RabbitMQ приветствуется", evidence: "Опыт с RabbitMQ приветствуется", wantClass: RequirementExtractionPreference, wantCue: "приветствуется"},
		{name: "Russian preferred", source: "Высшее образование предпочтительно", evidence: "Высшее образование предпочтительно", wantClass: RequirementExtractionPreference, wantCue: "предпочтительно"},
		{name: "Russian not required", source: "Опыт FastAPI не обязательно", evidence: "Опыт FastAPI не обязательно", wantClass: RequirementExtractionPreference, wantCue: "не обязательно"},
		{name: "English must", source: "Must have PostgreSQL", evidence: "Must have PostgreSQL", wantClass: RequirementExtractionExplicitHard, wantCue: "must"},
		{name: "Russian required", source: "Требуется опыт от 3 лет", evidence: "Требуется опыт от 3 лет", wantClass: RequirementExtractionExplicitHard, wantCue: "требуется"},
		{name: "Russian mandatory", source: "Обязателен опыт работы с ETL", evidence: "Обязателен опыт работы с ETL", wantClass: RequirementExtractionExplicitHard, wantCue: "обязателен"},
		{name: "optional wins over substring", source: "Опыт будет преимуществом, но не обязателен", evidence: "Опыт будет преимуществом, но не обязателен", wantClass: RequirementExtractionPreference, wantCue: "не обязателен"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyRequirementContext(test.source, test.evidence)
			if got.Classification != test.wantClass || got.MandatoryCue != test.wantCue {
				t.Fatalf("classification=%q cue=%q, want %q/%q", got.Classification, got.MandatoryCue, test.wantClass, test.wantCue)
			}
			if len([]rune(got.SourceContext)) > 240 {
				t.Fatalf("source context is unbounded: %d runes", len([]rune(got.SourceContext)))
			}
		})
	}
}

func TestRequirementContextDoesNotUseNeighboringSentence(t *testing.T) {
	got := classifyRequirementContext("Знание Kafka желательно. Опыт Python обязателен.", "Знание Kafka")
	if got.Classification != RequirementExtractionPreference || got.MandatoryCue != "желательно" {
		t.Fatalf("neighboring mandatory cue changed classification to %q", got.Classification)
	}
}
