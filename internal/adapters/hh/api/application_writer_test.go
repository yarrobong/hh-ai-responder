package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	hhwrite "hh-ai-responder/internal/ports/hhwrite"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAPIApplicationWriterPostsExactNegotiationForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/negotiations" {
			t.Fatalf("request=%s %s, want POST /negotiations", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token-fixture" {
			t.Fatalf("authorization=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "hh-ai-responder/test" {
			t.Fatalf("user-agent=%q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type=%q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if values.Get("vacancy_id") != "42" || values.Get("resume_id") != "resume-provider-7" || values.Get("message") != "truthful letter" {
			t.Fatalf("form=%v", values)
		}
		if len(values) != 3 {
			t.Fatalf("form has unexpected fields: %v", values)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"negotiation-42"}`)
	}))
	defer server.Close()

	client := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()})
	writer := NewAPIApplicationWriter(client)
	result, err := writer.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{
		VacancyID:        42,
		ProviderResumeID: "resume-provider-7",
		Letter:           "truthful letter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Class != hhwrite.ApplicationResultSuccess || result.Outcome != hhwrite.OutcomeAccepted || result.ProviderID != "negotiation-42" {
		t.Fatalf("result=%+v", result)
	}
}

func TestAPIApplicationWriterClassifiesDeterministicResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		class   hhwrite.ApplicationResultClass
		outcome hhwrite.Outcome
	}{
		{name: "already applied", status: http.StatusConflict, body: `{"error":"already_applied"}`, class: hhwrite.ApplicationResultAlreadyApplied, outcome: hhwrite.OutcomeRejected},
		{name: "auth required", status: http.StatusUnauthorized, body: `{"error":"login required"}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"slow down"}`, class: hhwrite.ApplicationResultRateLimited, outcome: hhwrite.OutcomeRejected},
		{name: "business rejected", status: http.StatusUnprocessableEntity, body: `{"error":"resume is unsuitable"}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			client := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()})
			result, err := NewAPIApplicationWriter(client).SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-7"})
			if err == nil {
				t.Fatal("expected provider error")
			}
			if result.Class != tt.class || result.Outcome != tt.outcome {
				t.Fatalf("result=%+v, want class=%s outcome=%s", result, tt.class, tt.outcome)
			}
			if strings.Contains(err.Error(), "login required") || strings.Contains(err.Error(), "resume is unsuitable") {
				t.Fatalf("error exposed provider body: %v", err)
			}
		})
	}
}

func TestAPIApplicationWriterMapsAmbiguousTransportWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	connectionErr := errors.New("connection reset after dispatch")
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/negotiations" {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		return nil, connectionErr
	})
	client := newAPIClient(t, "https://api.hh.test", &memoryTokenStore{loaded: validTokens()})
	client.httpClient = &http.Client{Transport: transport}

	result, err := NewAPIApplicationWriter(client).SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-7"})
	if err == nil || !hhwrite.IsAmbiguous(err) {
		t.Fatalf("err=%v, want ambiguous error", err)
	}
	if result.Class != hhwrite.ApplicationResultUnknownSendResult || result.Outcome != hhwrite.OutcomeAmbiguous {
		t.Fatalf("result=%+v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want exactly one POST", calls.Load())
	}
}
