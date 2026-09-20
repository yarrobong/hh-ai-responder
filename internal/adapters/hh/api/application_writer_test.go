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
		{name: "already applied documented forbidden", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"already_applied"}]}`, class: hhwrite.ApplicationResultAlreadyApplied, outcome: hhwrite.OutcomeRejected},
		{name: "already applied does not require conflict", status: http.StatusConflict, body: `{"errors":[{"type":"negotiations","value":"already_applied"}]}`, class: hhwrite.ApplicationResultAlreadyApplied, outcome: hhwrite.OutcomeRejected},
		{name: "auth required", status: http.StatusUnauthorized, body: `{"error":"login required"}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "oauth bad authorization", status: http.StatusForbidden, body: `{"errors":[{"type":"oauth","value":"bad_authorization"}]}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "oauth token expired", status: http.StatusForbidden, body: `{"errors":[{"type":"oauth","value":"token_expired"}]}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "oauth token revoked", status: http.StatusForbidden, body: `{"errors":[{"type":"oauth","value":"token_revoked"}]}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "oauth application not found", status: http.StatusForbidden, body: `{"errors":[{"type":"oauth","value":"application_not_found"}]}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "oauth user auth expected", status: http.StatusForbidden, body: `{"errors":[{"type":"oauth","value":"user_auth_expected"}]}`, class: hhwrite.ApplicationResultAuthRequired, outcome: hhwrite.OutcomeNotSent},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"slow down"}`, class: hhwrite.ApplicationResultRateLimited, outcome: hhwrite.OutcomeRejected},
		{name: "invalid vacancy", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"invalid_vacancy"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "resume not found", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"resume_not_found"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "test required", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"test_required"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "resume visibility conflict", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"resume_visibility_conflict"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "application denied", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"application_denied"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "wrong state", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"wrong_state"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "empty message", status: http.StatusBadRequest, body: `{"errors":[{"type":"negotiations","value":"empty_message"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "too long message", status: http.StatusBadRequest, body: `{"errors":[{"type":"negotiations","value":"too_long_message"}]}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "captcha required", status: http.StatusForbidden, body: `{"errors":[{"type":"negotiations","value":"captcha_required","captcha_url":"https://evil.example/challenge","fallback_url":"https://evil.example/fallback"}]}`, class: hhwrite.ApplicationResultManualChallenge, outcome: hhwrite.OutcomeRejected},
		{name: "arbitrary duplicate text is not duplicate classification", status: http.StatusForbidden, body: `{"message":"duplicate record in an unrelated diagnostic"}`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
		{name: "malformed structured body is terminal business rejection", status: http.StatusForbidden, body: `not-json duplicate`, class: hhwrite.ApplicationResultBusinessRejected, outcome: hhwrite.OutcomeRejected},
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
			if strings.Contains(err.Error(), "login required") || strings.Contains(err.Error(), "resume is unsuitable") || strings.Contains(err.Error(), "evil.example") || strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("error exposed provider body: %v", err)
			}
		})
	}
}

func TestAPIApplicationWriterBoundsStructuredErrorEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"errors":[{"type":"negotiations","value":"`+strings.Repeat("x", maxProviderFieldLength*4)+`"}]}`)
	}))
	defer server.Close()
	client := newAPIClient(t, server.URL, &memoryTokenStore{loaded: validTokens()})
	result, err := NewAPIApplicationWriter(client).SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 42, ProviderResumeID: "resume-7"})
	if err == nil || result.Class != hhwrite.ApplicationResultBusinessRejected {
		t.Fatalf("result=%+v err=%v, want terminal business rejection", result, err)
	}
	var transportErr *hhwrite.TransportError
	if !errors.As(err, &transportErr) || transportErr == nil {
		t.Fatalf("err=%T %v, want transport error", err, err)
	}
	if len(transportErr.ErrorFields["value"]) > maxProviderFieldLength {
		t.Fatalf("structured value length=%d, want <=%d", len(transportErr.ErrorFields["value"]), maxProviderFieldLength)
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
