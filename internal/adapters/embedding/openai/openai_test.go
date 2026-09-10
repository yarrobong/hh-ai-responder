package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/semantic"
)

func TestProviderPreservesRequestAndResponseIndexOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var request embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "model" || strings.Join(request.Input, ",") != "first,second" {
			t.Fatalf("request=%+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"index": 1, "embedding": []float32{0, 1}}, {"index": 0, "embedding": []float32{1, 0}}}})
	}))
	defer server.Close()

	provider := New(Options{BaseURL: server.URL, APIKey: "secret", Model: "model", Dimensions: 2, Timeout: time.Second})
	vectors, err := provider.Embed(context.Background(), []string{"first", "second"})
	if err != nil || len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("vectors=%v err=%v", vectors, err)
	}
}

func TestProviderFailsSafelyForMalformedAndInvalidResponses(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{name: "non-2xx", body: `{"error":{"message":"private detail"}}`, code: http.StatusBadGateway},
		{name: "malformed", body: `{`, code: http.StatusOK},
		{name: "wrong-count", body: `{"data":[]}`, code: http.StatusOK},
		{name: "wrong-dimension", body: `{"data":[{"index":0,"embedding":[1]}]}`, code: http.StatusOK},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.code)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()
			provider := New(Options{BaseURL: server.URL, Model: "model", Dimensions: 2, Timeout: time.Second})
			if _, err := provider.Embed(context.Background(), []string{"text"}); err == nil {
				t.Fatal("invalid provider response must fail")
			}
		})
	}
}

func TestProviderHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := New(Options{BaseURL: server.URL, Model: "model", Dimensions: 2, Timeout: time.Second})
	if _, err := provider.Embed(ctx, []string{"text"}); err == nil {
		t.Fatal("cancelled context must fail")
	}
}

func TestProviderSupportsConfigured1024DimensionsWithoutCoercion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"index": 0, "embedding": make([]float32, 1024)}}})
	}))
	defer server.Close()
	provider := New(Options{BaseURL: server.URL, Model: "embedding-1024", Dimensions: 1024, Timeout: time.Second})
	vectors, err := provider.Embed(context.Background(), []string{"text"})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != 1024 {
		t.Fatalf("vectors=%d/%d err=%v", len(vectors), len(vectors[0]), err)
	}
}

func TestProviderDimensionMismatchIsTyped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"index": 0, "embedding": []float32{1}}}})
	}))
	defer server.Close()
	_, err := New(Options{BaseURL: server.URL, Model: "embedding-1024", Dimensions: 1024, Timeout: time.Second}).Embed(context.Background(), []string{"text"})
	if !errors.Is(err, semantic.ErrEmbeddingDimensionMismatch) {
		t.Fatalf("typed mismatch error=%v", err)
	}
}
