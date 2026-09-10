// Package openai contains the OpenAI-compatible text completion adapter.
// Provider protocol DTOs are private to this package.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
)

const chatCompletionsPath = "/v1/chat/completions"

// Options contains only provider construction values. Configuration loading
// and environment access belong to the composition root.
type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Attempts   int
	HTTPClient *http.Client
	RetryDelay time.Duration

	// Debug and Warn are optional, caller-controlled observability hooks. The
	// adapter never logs prompts, completions, credentials, or response bodies.
	Debug func(string)
	Warn  func(string)
}

// Provider implements the infrastructure-neutral completion port.
type Provider struct {
	baseURL    string
	apiKey     string
	model      string
	attempts   int
	client     *http.Client
	retryDelay time.Duration
	debug      func(string)
	warn       func(string)
}

var _ llmport.CompletionProvider = (*Provider)(nil)

type providerMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type providerRequest struct {
	Model            string                  `json:"model"`
	Messages         []providerMessage       `json:"messages"`
	Stream           bool                    `json:"stream"`
	MaxTokens        int                     `json:"max_tokens,omitempty"`
	Temperature      float64                 `json:"temperature,omitempty"`
	ResponseFormat   *providerResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort  string                  `json:"reasoning_effort,omitempty"`
	IncludeReasoning *bool                   `json:"include_reasoning,omitempty"`
}

type providerResponseFormat struct {
	Type       string              `json:"type"`
	JSONSchema *providerJSONSchema `json:"json_schema,omitempty"`
}

type providerJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type providerResponse struct {
	Model   string           `json:"model"`
	Choices []providerChoice `json:"choices"`
	Usage   providerUsage    `json:"usage"`
}

type providerChoice struct {
	Message      providerMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type providerUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type httpStatusError struct {
	status int
	model  string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("ai request failed (model=%s status=%d)", e.model, e.status)
}

// New constructs a provider around the supplied reusable HTTP client.
func New(options Options) *Provider {
	baseURL := options.BaseURL
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	attempts := options.Attempts
	if attempts < 1 {
		attempts = 1
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     options.APIKey,
		model:      options.Model,
		attempts:   attempts,
		client:     client,
		retryDelay: options.RetryDelay,
		debug:      options.Debug,
		warn:       options.Warn,
	}
}

// Model returns the configured model name for composition and diagnostics.
func (p *Provider) Model() string {
	if p == nil {
		return ""
	}
	return p.model
}

// Complete performs the current non-streaming chat-completion operation.
// Transport failures and invalid provider responses use the configured
// transport retry policy. Semantic retries (for example, invalid business
// JSON) are intentionally outside this adapter.
func (p *Provider) Complete(ctx context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	if p == nil || p.client == nil {
		return llmvalue.CompletionResponse{}, errors.New("completion provider is not configured")
	}
	if ctx == nil {
		return llmvalue.CompletionResponse{}, errors.New("completion context is nil")
	}

	payload, err := p.buildRequest(request)
	if err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llmvalue.CompletionResponse{}, fmt.Errorf("encode completion request: %w", err)
	}
	endpoint := p.baseURL + chatCompletionsPath
	if p.debug != nil {
		p.debug(fmt.Sprintf("AI request endpoint=%s model=%s messages=%d max_tokens=%d temperature=%.2f payload_bytes=%d", endpoint, payload.Model, len(payload.Messages), payload.MaxTokens, payload.Temperature, len(body)))
	}

	var lastErr error
	for attempt := 1; attempt <= p.attempts; attempt++ {
		result, requestErr := p.completeOnce(ctx, endpoint, body, payload.Model)
		if requestErr == nil {
			return result, nil
		}
		lastErr = requestErr
		if attempt == p.attempts || ctx.Err() != nil || !retryable(requestErr) {
			break
		}
		if p.warn != nil {
			p.warn(fmt.Sprintf("AI request failed, retrying (%d/%d): %v", attempt, p.attempts, requestErr))
		}
		if err := wait(ctx, p.retryDelay); err != nil {
			return llmvalue.CompletionResponse{}, err
		}
	}
	return llmvalue.CompletionResponse{}, lastErr
}

func (p *Provider) buildRequest(request llmvalue.CompletionRequest) (providerRequest, error) {
	model := request.Model
	if model == "" {
		model = p.model
	}
	messages := make([]providerMessage, len(request.Messages))
	for i, message := range request.Messages {
		messages[i] = providerMessage{Role: string(message.Role), Content: message.Content}
	}
	payload := providerRequest{
		Model:            model,
		Messages:         messages,
		Stream:           false,
		MaxTokens:        request.MaxTokens,
		Temperature:      request.Temperature,
		ReasoningEffort:  request.ReasoningEffort,
		IncludeReasoning: request.IncludeReasoning,
	}
	if request.ResponseFormat != nil {
		payload.ResponseFormat = &providerResponseFormat{Type: request.ResponseFormat.Type}
		if request.ResponseFormat.JSONSchema != nil {
			payload.ResponseFormat.JSONSchema = &providerJSONSchema{
				Name:   request.ResponseFormat.JSONSchema.Name,
				Schema: request.ResponseFormat.JSONSchema.Schema,
				Strict: request.ResponseFormat.JSONSchema.Strict,
			}
		}
	}

	// Preserve current provider compatibility: Mistral accepts the supplied
	// strict schema, while other OpenAI-compatible endpoints receive JSON
	// object mode. Groq GPT-OSS also receives the existing safe reasoning
	// options.
	if payload.ResponseFormat != nil && payload.ResponseFormat.Type == "json_schema" && !p.supportsMistralJSONSchema() {
		payload.ResponseFormat.Type = "json_object"
		payload.ResponseFormat.JSONSchema = nil
	}
	if p.supportsGroqGPTOSSReasoning() {
		includeReasoning := false
		payload.ReasoningEffort = "low"
		payload.IncludeReasoning = &includeReasoning
	}
	return payload, nil
}

func (p *Provider) completeOnce(ctx context.Context, endpoint string, body []byte, model string) (llmvalue.CompletionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	defer resp.Body.Close()
	if p.debug != nil {
		method, responseURL := http.MethodPost, endpoint
		if resp.Request != nil {
			method = resp.Request.Method
			if resp.Request.URL != nil {
				responseURL = resp.Request.URL.String()
			}
		}
		p.debug(fmt.Sprintf("%d %s %s", resp.StatusCode, method, responseURL))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return llmvalue.CompletionResponse{}, &httpStatusError{status: resp.StatusCode, model: model}
	}

	var decoded providerResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return llmvalue.CompletionResponse{}, fmt.Errorf("ai response decode failed (model=%s status=%d): %w", model, resp.StatusCode, err)
	}
	if len(decoded.Choices) == 0 {
		return llmvalue.CompletionResponse{}, fmt.Errorf("ai response has no choices (model=%s status=%d completion_tokens=%d total_tokens=%d)", model, resp.StatusCode, decoded.Usage.CompletionTokens, decoded.Usage.TotalTokens)
	}
	choice := decoded.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" {
		return llmvalue.CompletionResponse{}, fmt.Errorf("ai response has empty content (model=%s status=%d finish_reason=%s completion_tokens=%d total_tokens=%d)", model, resp.StatusCode, choice.FinishReason, decoded.Usage.CompletionTokens, decoded.Usage.TotalTokens)
	}
	return llmvalue.CompletionResponse{
		Content:      content,
		FinishReason: choice.FinishReason,
		Usage: llmvalue.Usage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
		Model: decoded.Model,
	}, nil
}

func retryable(err error) bool {
	statusErr, ok := err.(*httpStatusError)
	if !ok {
		return true
	}
	return statusErr.status != http.StatusUnauthorized && statusErr.status != http.StatusForbidden
}

func (p *Provider) supportsGroqGPTOSSReasoning() bool {
	parsed, err := url.Parse(p.baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	model := strings.ToLower(strings.TrimSpace(p.model))
	return (host == "api.groq.com" || strings.HasSuffix(host, ".api.groq.com")) && strings.HasPrefix(model, "openai/gpt-oss-")
}

func (p *Provider) supportsMistralJSONSchema() bool {
	parsed, err := url.Parse(p.baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.mistral.ai")
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
