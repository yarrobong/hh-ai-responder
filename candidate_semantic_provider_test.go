package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAICompatibleEmbeddingProviderBatchesRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request path/auth: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request embeddingRequest
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
