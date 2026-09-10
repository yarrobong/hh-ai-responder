package runtime

import (
	"encoding/json"
	"errors"
	"fmt"

	llmvalue "hh-ai-responder/internal/llm"
)

type ChatResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *ChatJSONSchema `json:"json_schema,omitempty"`
}

type ChatJSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

func llmResponseFormat(format *ChatResponseFormat) (*llmvalue.ResponseFormat, error) {
	if format == nil {
		return nil, nil
	}
	result := &llmvalue.ResponseFormat{Type: format.Type}
	if format.JSONSchema == nil {
		return result, nil
	}
	rawSchema, err := json.Marshal(format.JSONSchema.Schema)
	if err != nil {
		return nil, fmt.Errorf("encode structured AI response schema: %w", err)
	}
	result.JSONSchema = &llmvalue.JSONSchema{
		Name:   format.JSONSchema.Name,
		Schema: rawSchema,
		Strict: format.JSONSchema.Strict,
	}
	return result, nil
}

// legacyCompletion is the only implementation behind the deprecated root
// Chat* methods. It translates the old call shape into the sole completion
// port and performs exactly one provider call. Structured business parsing and
// semantic retry belong to the importable usecase that initiated the request.
func (c *AIClient) legacyCompletion(systemPrompt, userPrompt string, maxTokens int, temperature float64, format *ChatResponseFormat, validator func(string) error) (string, error) {
	if c == nil || c.provider == nil {
		return "", errors.New("AI completion provider is not configured")
	}
	if format != nil && validator == nil {
		return "", errors.New("structured AI response validator is required")
	}
	request := llmvalue.CompletionRequest{
		Model:       c.model,
		Messages:    []llmvalue.Message{{Role: llmvalue.RoleSystem, Content: systemPrompt}, {Role: llmvalue.RoleUser, Content: userPrompt}},
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
	var err error
	request.ResponseFormat, err = llmResponseFormat(format)
	if err != nil {
		return "", err
	}
	response, err := c.Complete(c.ctx, request)
	if err != nil {
		return "", err
	}
	if validator != nil {
		if err := validator(response.Content); err != nil {
			return "", err
		}
	}
	return response.Content, nil
}
