package vacancyanalysis

import "strings"

type RequirementContextClassification struct {
	SourceContext             string
	Classification            string
	MandatoryCue              string
	ClassificationDiagnostics []string
}

var optionalRequirementCues = []string{
	"не обязательно",
	"не обязателен",
	"будет преимуществом",
	"будет плюсом",
	"nice to have",
	"would be a plus",
	"желательно",
	"приветствуется",
	"предпочтительно",
	"preferred",
	"optional",
}

var mandatoryRequirementCues = []string{
	"minimum",
	"must",
	"required",
	"не менее",
	"обязателен",
	"обязательно",
	"требуется",
}

func classifyRequirementContext(source, evidence string) RequirementContextClassification {
	result := RequirementContextClassification{Classification: RequirementExtractionAmbiguous}
	sentence, exact := requirementSourceSentence(source, evidence)
	result.SourceContext = boundedRequirementContext(sentence, 240)
	if !exact {
		result.ClassificationDiagnostics = append(result.ClassificationDiagnostics, "source_context_not_exact")
	}
	text := normalizeEvidenceText(sentence)
	if text == "" {
		result.ClassificationDiagnostics = append(result.ClassificationDiagnostics, "source_context_unavailable")
		return result
	}
	if cue := firstCue(text, optionalRequirementCues); cue != "" {
		result.Classification = RequirementExtractionPreference
		result.MandatoryCue = cue
		return result
	}
	if cue := firstCue(text, mandatoryRequirementCues); cue != "" {
		result.Classification = RequirementExtractionExplicitHard
		result.MandatoryCue = cue
		return result
	}
	return result
}

func requirementSourceSentence(source, evidence string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false
	}
	for _, sentence := range strings.FieldsFunc(source, func(r rune) bool {
		return strings.ContainsRune(".!?;|", r)
	}) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if strings.TrimSpace(evidence) == "" || containsNormalizedText(sentence, evidence) {
			return sentence, true
		}
	}
	return source, false
}

func boundedRequirementContext(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func firstCue(text string, cues []string) string {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return cue
		}
	}
	return ""
}
