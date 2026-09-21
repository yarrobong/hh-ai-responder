// Package llm contains infrastructure-neutral values for text completion.
//
// It deliberately does not know about HTTP or any particular provider
// protocol. Domain callers may use these values to request text or structured
// text, while business validation remains with those callers.
package llm

import (
	"encoding/json"
)

// Role is the role vocabulary used by the current chat-completion callers.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one ordered text message supplied to a completion provider.
type Message struct {
	Role    Role
	Content string
}

// JSONSchema describes a provider-independent structured-output request.
// Schema contains the encoded JSON object so this package need not model a
// provider's schema DTO or accept an untyped value in its public API.
type JSONSchema struct {
	Name   string
	Schema json.RawMessage
	Strict bool
}

// ResponseFormat is an optional transport-level response-format request.
// The meaning of the returned text remains the caller's responsibility.
type ResponseFormat struct {
	Type       string
	JSONSchema *JSONSchema
}

// CompletionRequest is the narrow request understood by the completion port.
// TemperatureSet distinguishes an explicit zero from an omitted value.
type CompletionRequest struct {
	Model            string
	Messages         []Message
	MaxTokens        int
	Temperature      float64
	TemperatureSet   bool
	ResponseFormat   *ResponseFormat
	ReasoningEffort  string
	IncludeReasoning *bool
}

// Usage contains token metadata returned by a provider when available.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// CompletionResponse is the normalized result of one non-streaming
// completion request.
type CompletionResponse struct {
	Content      string
	FinishReason string
	Usage        Usage
	Model        string
}
