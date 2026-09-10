package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	openaiembedding "hh-ai-responder/internal/adapters/embedding/openai"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
)

const defaultEmbeddingModel = openaiembedding.DefaultModel

// OpenAICompatibleEmbeddingProvider preserves the historical root constructor
// while delegating HTTP behavior to the narrow adapter.
type OpenAICompatibleEmbeddingProvider struct {
	baseURL      string
	apiKey       string
	providerName string
	model        string
	dimensions   int
	timeout      time.Duration
	provider     *openaiembedding.Provider
}

func NewOpenAICompatibleEmbeddingProvider(baseURL, apiKey, model string, timeout time.Duration, dimensions ...int) *OpenAICompatibleEmbeddingProvider {
	return newOpenAICompatibleEmbeddingProvider("openai-compatible", baseURL, apiKey, model, timeout, dimensions...)
}

func newOpenAICompatibleEmbeddingProvider(providerName, baseURL, apiKey, model string, timeout time.Duration, dimensions ...int) *OpenAICompatibleEmbeddingProvider {
	if strings.TrimSpace(model) == "" {
		model = defaultEmbeddingModel
	}
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	dimension := CandidateSemanticEmbeddingDimensions
	if len(dimensions) > 0 && dimensions[0] > 0 {
		dimension = dimensions[0]
	}
	provider := &OpenAICompatibleEmbeddingProvider{baseURL: baseURL, apiKey: apiKey, providerName: providerName, model: model, dimensions: dimension, timeout: timeout}
	provider.provider = openaiembedding.New(openaiembedding.Options{BaseURL: baseURL, APIKey: apiKey, Model: model, Provider: providerName, Dimensions: dimension, Timeout: timeout})
	return provider
}

func (p *OpenAICompatibleEmbeddingProvider) Model() string {
	if p == nil {
		return ""
	}
	return p.model
}

func (p *OpenAICompatibleEmbeddingProvider) Dimensions() int {
	if p == nil {
		return 0
	}
	return p.dimensions
}

func (p *OpenAICompatibleEmbeddingProvider) EmbeddingContract() semantic.EmbeddingContract {
	if p == nil {
		return semantic.EmbeddingContract{}
	}
	contract, _ := semantic.NewEmbeddingContract(p.providerName, p.model, p.dimensions, p.baseURL)
	return contract
}

func (p *OpenAICompatibleEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p == nil || p.provider == nil {
		return nil, errors.New("embedding provider is not configured")
	}
	provider := p.provider
	// Rebuild when compatibility callers adjust the legacy fields directly.
	if provider.Dimensions() != p.dimensions {
		provider = openaiembedding.New(openaiembedding.Options{BaseURL: p.baseURL, APIKey: p.apiKey, Model: p.model, Provider: p.providerName, Dimensions: p.dimensions, Timeout: p.timeout})
	}
	return provider.Embed(ctx, texts)
}

func configuredEmbeddingProvider(cfg Config) (EmbeddingProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider))
	if provider == "" {
		return nil, errors.New("semantic embedding provider is not configured; set EMBEDDING_PROVIDER")
	}
	switch provider {
	case "openai", "openai-compatible":
		baseURL := firstNonEmpty(cfg.EmbeddingBaseURL, cfg.AIBaseURL)
		apiKey := firstNonEmpty(cfg.EmbeddingAPIKey, cfg.AIAPIKey)
		model := firstNonEmpty(cfg.EmbeddingModel, defaultEmbeddingModel)
		dimensions := cfg.EmbeddingDimensions
		if dimensions <= 0 {
			dimensions = CandidateSemanticEmbeddingDimensions
		}
		contract, err := semantic.NewEmbeddingContract(provider, model, dimensions, baseURL)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(contract.Endpoint) == "" {
			return nil, errors.New("effective embedding base URL is required when semantic embeddings are enabled")
		}
		return newOpenAICompatibleEmbeddingProvider(provider, baseURL, apiKey, model, cfg.AITimeout, dimensions), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", provider)
	}
}

var _ ports.EmbeddingContractProvider = (*OpenAICompatibleEmbeddingProvider)(nil)
