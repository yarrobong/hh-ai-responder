package testanswer

import (
	"encoding/json"

	llmvalue "hh-ai-responder/internal/llm"
)

// ResponseFormat returns the unchanged structured-output contract used by
// the previous root SolveTests implementation.
func ResponseFormat() *llmvalue.ResponseFormat {
	const schema = `{"type":"object","additionalProperties":false,"required":["solutions"],"properties":{"solutions":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["task_id"],"properties":{"task_id":{"type":"integer"},"solution_id":{"type":"integer"},"text_solution":{"type":"string"}}}}}}`
	return &llmvalue.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &llmvalue.JSONSchema{
			Name:   "test_solutions",
			Schema: json.RawMessage(schema),
			Strict: true,
		},
	}
}
