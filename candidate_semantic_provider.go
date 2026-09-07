package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultEmbeddingModel = "text-embedding-3-small"

// OpenAICompatibleEmbeddingProvider is deliberately smaller than AIClient:
// embeddings need only one batch endpoint and do not share chat semantics.
type OpenAICompatibleEmbeddingProvider struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewOpenAICompatibleEmbeddingProvider(baseURL, apiKey, model string, timeout time.Duration) *OpenAICompatibleEmbeddingProvider {
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultEmbeddingModel
	}
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	return &OpenAICompatibleEmbeddingProvider{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, dimensions: CandidateSemanticEmbeddingDimensions, client: &http.Client{Timeout: timeout}}
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

func (p *OpenAICompatibleEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("embedding provider is not configured")
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("embedding text %d is empty", i)
		}
	}
	body, err := json.Marshal(embeddingRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding provider returned HTTP %d", resp.StatusCode)
	}
	var decoded embeddingResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if decoded.Error != nil {
		return nil, errors.New("embedding provider returned an error")
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d texts", len(decoded.Data), len(texts))
	}
	result := make([][]float32, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(result) || len(item.Embedding) != p.dimensions {
			return nil, fmt.Errorf("embedding provider returned invalid vector dimensions")
		}
		result[item.Index] = append([]float32{}, item.Embedding...)
	}
	for _, vector := range result {
		if len(vector) != p.dimensions {
			return nil, errors.New("embedding provider returned duplicate or missing indexes")
		}
	}
	return result, nil
}

func configuredEmbeddingProvider(cfg Config) (EmbeddingProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider))
	if provider == "" {
		return nil, errors.New("semantic embedding provider is not configured; set EMBEDDING_PROVIDER")
	}
	switch provider {
	case "openai", "openai-compatible":
		return NewOpenAICompatibleEmbeddingProvider(cfg.AIBaseURL, cfg.AIAPIKey, cfg.EmbeddingModel, cfg.AITimeout), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", provider)
	}
}
