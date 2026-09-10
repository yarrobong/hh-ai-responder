// Package openai contains the narrow OpenAI-compatible embedding HTTP
// adapter. Provider request/response DTOs are private to this package.
package openai

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

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
)

const DefaultModel = "text-embedding-3-small"

type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Provider   string
	Dimensions int
	Timeout    time.Duration
}

type Provider struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	contract   semantic.EmbeddingContract
	client     *http.Client
}

var _ ports.EmbeddingProvider = (*Provider)(nil)

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

func New(options Options) *Provider {
	baseURL := options.BaseURL
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = DefaultModel
	}
	dimensions := options.Dimensions
	if dimensions <= 0 {
		dimensions = semantic.CandidateSemanticEmbeddingDimensions
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	baseURL = strings.TrimRight(baseURL, "/")
	providerName := strings.ToLower(strings.TrimSpace(options.Provider))
	if providerName == "" {
		providerName = "openai-compatible"
	}
	contract, _ := semantic.NewEmbeddingContract(providerName, model, dimensions, baseURL)
	return &Provider{baseURL: baseURL, apiKey: options.APIKey, model: model, dimensions: dimensions, contract: contract, client: &http.Client{Timeout: timeout}}
}

func (p *Provider) Model() string {
	if p == nil {
		return ""
	}
	return p.model
}

func (p *Provider) Dimensions() int {
	if p == nil {
		return 0
	}
	return p.dimensions
}

func (p *Provider) EmbeddingContract() semantic.EmbeddingContract {
	if p == nil {
		return semantic.EmbeddingContract{}
	}
	return p.contract
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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
		if item.Index < 0 || item.Index >= len(result) {
			return nil, fmt.Errorf("embedding provider returned invalid result index %d", item.Index)
		}
		if err := semantic.ValidateVector(item.Embedding, p.dimensions, "embedding provider"); err != nil {
			return nil, err
		}
		if result[item.Index] != nil {
			return nil, errors.New("embedding provider returned duplicate or missing indexes")
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
