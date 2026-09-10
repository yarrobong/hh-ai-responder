package candidateinterpretation

import (
	"encoding/json"

	llmvalue "hh-ai-responder/internal/llm"
)

func JSONResponseFormat() *llmvalue.ResponseFormat {
	return &llmvalue.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &llmvalue.JSONSchema{
			Name:   "candidate_knowledge_interpretation",
			Strict: true,
			Schema: candidateKnowledgeExtractionSchema(),
		},
	}
}

func candidateKnowledgeExtractionSchema() json.RawMessage {
	value := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"proposals"}, "properties": map[string]any{
			"proposals": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false,
				"required": []string{"type", "skill", "usage_context", "level", "truth_status", "story"}, "properties": map[string]any{
					"type":  map[string]any{"type": "string", "enum": []string{"skill_usage", "story"}},
					"skill": map[string]any{"type": "string"}, "usage_context": map[string]any{"type": "string", "enum": []string{"commercial", "pet_project", "educational", "personal", "studied_only", "unknown", "explicitly_not_used"}},
					"level":        map[string]any{"type": "string", "enum": []string{"unknown", "heard_of", "basic", "working", "confident", "advanced"}},
					"truth_status": map[string]any{"type": "string", "enum": []string{"", "hypothesis"}},
					"story":        map[string]any{"type": "object", "additionalProperties": false, "required": []string{"title", "situation", "task", "action", "result", "referenced_experience", "referenced_project"}, "properties": map[string]any{"title": map[string]any{"type": "string"}, "situation": map[string]any{"type": "string"}, "task": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"}, "result": map[string]any{"type": "string"}, "referenced_experience": map[string]any{"type": "string"}, "referenced_project": map[string]any{"type": "string"}}},
				},
			}},
		},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
