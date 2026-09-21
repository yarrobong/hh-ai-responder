package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
)

func TestProviderCompletePreservesOpenAIRequestAndNormalizesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer super-secret-test-key" {
			t.Fatalf("unexpected authorization: %q", got)
		}
		var request providerRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "model" || !strings.EqualFold(request.Messages[0].Role, "system") || request.Messages[0].Content != "  system\nline  " || request.Messages[1].Content != "\tuser\nexact  " {
			t.Fatalf("message payload was changed: %+v", request.Messages)
		}
		if request.Stream || request.MaxTokens != 321 || request.Temperature != 0.1 {
			t.Fatalf("request options were changed: %+v", request)
		}
		_, _ = io.WriteString(w, `{"model":"actual-model","choices":[{"message":{"role":"assistant","content":"  result \n"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`)
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: "super-secret-test-key", Model: "model", Attempts: 1, HTTPClient: server.Client()})
	response, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{
		Model: "model",
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: "  system\nline  "},
			{Role: llmvalue.RoleUser, Content: "\tuser\nexact  "},
		},
		MaxTokens:   321,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "result" || response.FinishReason != "stop" || response.Model != "actual-model" || response.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected normalized response: %+v", response)
	}
}

func TestProviderOmitsZeroOptionsLikeTheLegacyTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["temperature"]; ok {
			t.Fatal("zero temperature was not omitted")
		}
		if _, ok := fields["max_tokens"]; ok {
			t.Fatal("zero max_tokens was not omitted")
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider := New(Options{BaseURL: server.URL, Attempts: 1, HTTPClient: server.Client()})
	if _, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderSendsExplicitZeroTemperature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal(err)
		}
		raw, ok := fields["temperature"]
		if !ok || string(raw) != "0" {
			t.Fatalf("explicit zero temperature=%s, want JSON number 0", raw)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	provider := New(Options{BaseURL: server.URL, Attempts: 1, HTTPClient: server.Client()})
	if _, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{
		Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}, TemperatureSet: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRejectsMalformedEmptyAndZeroChoiceResponses(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: "{"},
		{name: "zero choices", body: `{"choices":[]}`},
		{name: "empty content", body: `{"choices":[{"message":{"content":""}}]}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()
			provider := New(Options{BaseURL: server.URL, Attempts: 1, HTTPClient: server.Client()})
			if _, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}}); err == nil {
				t.Fatal("invalid provider response passed")
			}
		})
	}
}

func TestProviderReturnsNetworkErrorWithoutLeakingRequestData(t *testing.T) {
	networkErr := errors.New("synthetic network failure")
	provider := New(Options{
		BaseURL:  "http://provider.invalid",
		Attempts: 1,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, networkErr
		})},
	})
	_, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "private prompt"}}})
	if !errors.Is(err, networkErr) || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("network error was not preserved safely: %v", err)
	}
}

func TestProviderStructuredCompatibilityVariants(t *testing.T) {
	schema := &llmvalue.JSONSchema{Name: "fixture", Schema: json.RawMessage(`{"type":"object"}`), Strict: true}
	tests := []struct {
		name             string
		baseURL          string
		model            string
		wantFormat       string
		wantSchema       bool
		wantReasoning    bool
		wantIncludeFalse bool
	}{
		{name: "mistral", baseURL: "https://api.mistral.ai", model: "ministral", wantFormat: "json_schema", wantSchema: true},
		{name: "groq", baseURL: "https://api.groq.com/openai", model: "openai/gpt-oss-120b", wantFormat: "json_object", wantReasoning: true, wantIncludeFalse: true},
		{name: "generic", baseURL: "https://example.test", model: "model", wantFormat: "json_object"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var request providerRequest
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Request: r, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)), Header: make(http.Header)}, nil
			})}
			provider := New(Options{BaseURL: testCase.baseURL, Model: testCase.model, Attempts: 1, HTTPClient: client})
			_, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Model: testCase.model, Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}, ResponseFormat: &llmvalue.ResponseFormat{Type: "json_schema", JSONSchema: schema}})
			if err != nil {
				t.Fatal(err)
			}
			if request.ResponseFormat == nil || request.ResponseFormat.Type != testCase.wantFormat || (request.ResponseFormat.JSONSchema != nil) != testCase.wantSchema {
				t.Fatalf("unexpected response format: %+v", request.ResponseFormat)
			}
			if testCase.wantSchema && request.ResponseFormat.JSONSchema.Name != "fixture" {
				t.Fatalf("schema was not preserved: %+v", request.ResponseFormat.JSONSchema)
			}
			if (request.ReasoningEffort != "") != testCase.wantReasoning || (request.IncludeReasoning != nil) != testCase.wantReasoning || (request.IncludeReasoning != nil && *request.IncludeReasoning != !testCase.wantIncludeFalse) {
				t.Fatalf("unexpected reasoning options: effort=%q include=%v", request.ReasoningEffort, request.IncludeReasoning)
			}
		})
	}
}

func TestProviderErrorsAreBoundedAndAuthenticationIsNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"super-secret-test-key and private prompt"}`)
			}))
			defer server.Close()
			provider := New(Options{BaseURL: server.URL, APIKey: "super-secret-test-key", Attempts: 2, RetryDelay: 0, HTTPClient: server.Client()})
			_, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Model: "model", Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "private prompt"}}})
			if err == nil || strings.Contains(err.Error(), "super-secret-test-key") || strings.Contains(err.Error(), "private prompt") {
				t.Fatalf("unsafe provider error: %v", err)
			}
			wantCalls := int32(2)
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("status %d used %d calls, want %d", status, calls.Load(), wantCalls)
			}
		})
	}
}

func TestProviderRetriesTransportErrorsAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"recovered"}}]}`)
	}))
	defer server.Close()
	provider := New(Options{BaseURL: server.URL, Attempts: 2, RetryDelay: 0, HTTPClient: server.Client()})
	response, err := provider.Complete(context.Background(), llmvalue.CompletionRequest{Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}})
	if err != nil || response.Content != "recovered" || calls.Load() != 2 {
		t.Fatalf("transport retry failed: response=%+v err=%v calls=%d", response, err, calls.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider = New(Options{BaseURL: server.URL, Attempts: 2, RetryDelay: time.Hour, HTTPClient: server.Client()})
	if _, err := provider.Complete(ctx, llmvalue.CompletionRequest{Messages: []llmvalue.Message{{Role: llmvalue.RoleUser, Content: "x"}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled completion error=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
