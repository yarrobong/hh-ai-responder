package vacancyanalysis

import (
	"encoding/json"

	llmvalue "hh-ai-responder/internal/llm"
)

func JSONResponseFormat() *llmvalue.ResponseFormat {
	return &llmvalue.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &llmvalue.JSONSchema{
			Name:   "vacancy_evaluation",
			Strict: true,
			Schema: mustSchemaJSON(),
		},
	}
}

func mustSchemaJSON() json.RawMessage {
	value := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"score", "apply", "reasons", "missing", "hard_requirements"},
		"properties": map[string]any{
			"score":   map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"apply":   map[string]any{"type": "boolean"},
			"reasons": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"missing": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"hard_requirements": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"requirement", "category", "vacancy_evidence"},
					"properties": map[string]any{
						"requirement": map[string]any{"type": "string"},
						"category": map[string]any{"type": "string", "enum": []string{
							HardRequirementCategoryEducation, HardRequirementCategoryLocation, HardRequirementCategoryExperienceYears,
							HardRequirementCategorySkill, HardRequirementCategoryLanguage, HardRequirementCategoryLicense,
							HardRequirementCategoryCitizenship, HardRequirementCategoryOther,
						}},
						"vacancy_evidence": map[string]any{"type": "string"},
					},
				},
			},
			"strong_match": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
