package runtime

// These OpenAI-shaped values are test fixtures only. Runtime protocol DTOs
// belong to internal/adapters/llm/openai.
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model            string              `json:"model"`
	Messages         []AIMessage         `json:"messages"`
	Stream           bool                `json:"stream"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Temperature      float64             `json:"temperature,omitempty"`
	ResponseFormat   *ChatResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort  string              `json:"reasoning_effort,omitempty"`
	IncludeReasoning *bool               `json:"include_reasoning,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
}

type ChatCompletionChoice struct {
	Message      AIMessage `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
