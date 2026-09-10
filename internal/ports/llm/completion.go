// Package llm defines the application boundary for text completion.
package llm

import (
	"context"

	llmvalue "hh-ai-responder/internal/llm"
)

// CompletionProvider is the only capability required by current text-AI
// callers. It performs one non-streaming completion operation; prompts,
// domain parsing, and business policy stay outside this boundary.
type CompletionProvider interface {
	Complete(context.Context, llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error)
}
