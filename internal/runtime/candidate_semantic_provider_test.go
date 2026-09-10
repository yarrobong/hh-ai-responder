package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatAndEmbeddingUseIndependentEndpointsAndKeys(t *testing.T) {
	var chatCalls, embeddingCalls atomic.Int32
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer chat-key" {
			t.Fatalf("chat authorization=%q", got)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("chat path=%q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "ok"}}}})
	}))
	defer chat.Close()
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embeddingCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer embedding-key" {
			t.Fatalf("embedding authorization=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{1, 0}}}})
	}))
	defer embedding.Close()

	cfg := Config{AIBaseURL: chat.URL, AIAPIKey: "chat-key", AIModel: "chat-model", EmbeddingProvider: "openai-compatible", EmbeddingBaseURL: embedding.URL, EmbeddingAPIKey: "embedding-key", EmbeddingModel: "embedding-model", EmbeddingDimensions: 2, AITimeout: time.Second}
	provider, err := configuredEmbeddingProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAIClient(context.Background(), cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, time.Second, time.Second, 1).Chat("system", "user", 10, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Embed(context.Background(), []string{"text"}); err != nil {
		t.Fatal(err)
	}
	if chatCalls.Load() != 1 || embeddingCalls.Load() != 1 {
		t.Fatalf("calls chat=%d embedding=%d", chatCalls.Load(), embeddingCalls.Load())
	}
}

func TestConfiguredEmbeddingProviderUsesLegacyEndpointAndKeyFallback(t *testing.T) {
	provider, err := configuredEmbeddingProvider(Config{AIBaseURL: "https://chat.example/v1", AIAPIKey: "chat-key", EmbeddingProvider: "openai-compatible", EmbeddingModel: "embedding-model", EmbeddingDimensions: 1536})
	if err != nil {
		t.Fatal(err)
	}
	concrete, ok := provider.(*OpenAICompatibleEmbeddingProvider)
	if !ok || concrete.baseURL != "https://chat.example/v1" || concrete.apiKey != "chat-key" {
		t.Fatalf("fallback provider=%+v", concrete)
	}
}

func TestOpenAICompatibleEmbeddingProviderBatchesRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request path/auth: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "fake" || len(request.Input) != 2 {
			t.Fatalf("request=%+v", request)
		}
		response := map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{1, 0, 0}}, {"index": 1, "embedding": []float32{0, 1, 0}}}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	provider := NewOpenAICompatibleEmbeddingProvider(server.URL, "test-key", "fake", time.Second)
	provider.dimensions = 3
	vectors, err := provider.Embed(context.Background(), []string{"first", "second"})
	if err != nil || len(vectors) != 2 || len(vectors[0]) != 3 || vectors[1][1] != 1 {
		t.Fatalf("vectors=%v err=%v", vectors, err)
	}
}
