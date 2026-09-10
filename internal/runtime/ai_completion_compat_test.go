package runtime

import (
	"context"
	"errors"
	"testing"

	llmvalue "hh-ai-responder/internal/llm"
)

type compatibilityCompletionProvider struct {
	calls int
}

func (p *compatibilityCompletionProvider) Complete(context.Context, llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	p.calls++
	return llmvalue.CompletionResponse{Content: "not valid"}, nil
}

func TestLegacyStructuredCompatibilityDoesNotRetry(t *testing.T) {
	provider := &compatibilityCompletionProvider{}
	client := &AIClient{ctx: context.Background(), model: "model", provider: provider, attempts: 3}

	_, err := client.ChatStructured("system", "user", 100, 0.1, func(string) error {
		return errors.New("invalid legacy response")
	})
	if err == nil || err.Error() != "invalid legacy response" {
		t.Fatalf("unexpected compatibility validation error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("legacy compatibility path retried semantic validation: calls=%d", provider.calls)
	}
}
