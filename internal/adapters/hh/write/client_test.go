package hhwrite

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	hhwrite "hh-ai-responder/internal/ports/hhwrite"
)

func TestClientPreservesMutationContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body []byte
		var err error
		if r.URL.Path != "/applicant/resumes/touch" {
			body, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
		}
		switch r.URL.Path {
		case "/chatik/api/send":
			var value map[string]any
			if err := json.Unmarshal(body, &value); err != nil {
				t.Fatal(err)
			}
			if value["chatId"] != float64(7) || value["text"] != "hello" || value["idempotencyKey"] != strings.Repeat("a", 32) {
				t.Fatalf("chat body = %#v", value)
			}
			if r.Header.Get("X-Xsrftoken") != "token" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Fatalf("chat auth headers missing: %v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"messageId":"message-7"}`)
		case "/chatik/api/leave":
			var value map[string]any
			if err := json.Unmarshal(body, &value); err != nil || value["chatId"] != float64(7) {
				t.Fatalf("leave body = %s, err=%v", body, err)
			}
			if r.Header.Get("X-hhtmSource") != "app" || r.Header.Get("X-hhtmSourceLabel") != "resume" {
				t.Fatalf("leave headers missing: %v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		case "/applicant/vacancy_response/popup":
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("X-Hhtmsource") != "vacancy_response" {
				t.Fatalf("application headers = %v", r.Header)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil || values.Get("vacancy_id") != "42" || values.Get("resume_hash") != "resume" || values.Get("letter") != "letter" || values.Get("task_9") != "101" {
				t.Fatalf("application form = %s, err=%v", body, err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		case "/applicant/resumes/touch":
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
				t.Fatalf("touch content type = %q", r.Header.Get("Content-Type"))
			}
			if err := r.ParseMultipartForm(1024); err != nil || r.FormValue("resume") != "resume" || r.FormValue("undirectable") != "true" {
				t.Fatalf("touch form = %#v, err=%v", r.MultipartForm, err)
			}
			w.WriteHeader(http.StatusOK)
		case "/profile/shards/user_statuses/job_search_status":
			if r.URL.Query().Get("status") != "looking_for_offers" || r.Header.Get("X-hhtmSource") != "resume_list" {
				t.Fatalf("job status request = %s headers=%v", r.URL.String(), r.Header)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, ResumeProfileURL: base, HTTPClient: server.Client(), XSRFToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if result, err := client.SendChatMessage(ctx, hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)}); err != nil || result.ProviderID != "message-7" || result.Outcome != hhwrite.OutcomeAccepted {
		t.Fatalf("chat result=%+v err=%v", result, err)
	}
	if _, err := client.LeaveChat(ctx, hhwrite.ChatLeaveRequest{ConversationID: "7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SubmitVacancyResponse(ctx, hhwrite.VacancyResponseRequest{
		VacancyID: 42, ResumeHash: "resume", Letter: "letter", RefererURL: server.URL, IgnorePostponed: "true",
		Test: &hhwrite.VacancyTestSubmission{Answers: []hhwrite.VacancyTestAnswer{{TaskID: 9, ChoiceID: "101"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TouchResume(ctx, hhwrite.ResumeTouchRequest{ResumeHash: "resume"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetJobSearchStatus(ctx, hhwrite.JobSearchStatusRequest{Status: "looking_for_offers"}); err != nil {
		t.Fatal(err)
	}
}

func TestClientAmbiguousWriteIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	connectionErr := errors.New("connection reset after request body")
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if _, err := io.ReadAll(request.Body); err != nil {
			t.Fatal(err)
		}
		return nil, connectionErr
	})
	base, _ := url.Parse("https://hh.example")
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: &http.Client{Transport: transport}, XSRFToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SendChatMessage(context.Background(), hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)})
	if !hhwrite.IsAmbiguous(err) || result.Outcome != hhwrite.OutcomeAmbiguous || calls.Load() != 1 || !errors.Is(err, connectionErr) {
		t.Fatalf("ambiguous result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	applicationResult, applicationErr := client.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{
		VacancyID: 42, ResumeHash: "resume", Letter: "letter", RefererURL: "https://hh.example/vacancy/42", IgnorePostponed: "true",
	})
	if !hhwrite.IsAmbiguous(applicationErr) || applicationResult.Outcome != hhwrite.OutcomeAmbiguous || calls.Load() != 2 {
		t.Fatalf("ambiguous application result=%+v err=%v calls=%d", applicationResult, applicationErr, calls.Load())
	}
}

func TestClientPreDispatchCancellationDoesNotCallHTTP(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})
	base, _ := url.Parse("https://hh.example")
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: &http.Client{Transport: transport}, XSRFToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := client.SendChatMessage(ctx, hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)})
	if result.Outcome != hhwrite.OutcomeNotSent || calls.Load() != 0 {
		t.Fatalf("pre-dispatch result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	var transportErr *hhwrite.TransportError
	if !errors.As(err, &transportErr) || transportErr.Outcome != hhwrite.OutcomeNotSent {
		t.Fatalf("missing not-sent classification: %v", err)
	}
}

func TestClientDefiniteProviderRejectionIsConcreteAndNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"rejected"}`)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: server.Client(), XSRFToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.SendChatMessage(context.Background(), hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)})
			var transportErr *hhwrite.TransportError
			if result.Outcome != hhwrite.OutcomeRejected || !errors.As(err, &transportErr) || transportErr.Outcome != result.Outcome || hhwrite.IsAmbiguous(err) || calls.Load() != 1 {
				t.Fatalf("status=%d result=%+v err=%v calls=%d", status, result, err, calls.Load())
			}
		})
	}
}

func TestClientAmbiguousHTTPStatusesAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"provider state is unknown"}`)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: server.Client(), XSRFToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.SendChatMessage(context.Background(), hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)})
			var transportErr *hhwrite.TransportError
			if result.Outcome != hhwrite.OutcomeAmbiguous || !hhwrite.IsAmbiguous(err) || !errors.As(err, &transportErr) || transportErr.Outcome != result.Outcome || calls.Load() != 1 {
				t.Fatalf("status=%d result=%+v err=%v calls=%d", status, result, err, calls.Load())
			}
		})
	}
}

func TestClientFiveHundredIsAmbiguousForEveryMutation(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) (hhwrite.WriteResult, error)
	}{
		{name: "chat message", call: func(client *Client) (hhwrite.WriteResult, error) {
			return client.SendChatMessage(context.Background(), hhwrite.ChatMessageRequest{ConversationID: "7", Text: "hello", IdempotencyKey: strings.Repeat("a", 32)})
		}},
		{name: "chat leave", call: func(client *Client) (hhwrite.WriteResult, error) {
			return client.LeaveChat(context.Background(), hhwrite.ChatLeaveRequest{ConversationID: "7"})
		}},
		{name: "vacancy response", call: func(client *Client) (hhwrite.WriteResult, error) {
			return client.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 42, ResumeHash: "resume", Letter: "letter", RefererURL: "https://hh.example/vacancy/42"})
		}},
		{name: "resume touch", call: func(client *Client) (hhwrite.WriteResult, error) {
			return client.TouchResume(context.Background(), hhwrite.ResumeTouchRequest{ResumeHash: "resume"})
		}},
		{name: "job-search status", call: func(client *Client) (hhwrite.WriteResult, error) {
			return client.SetJobSearchStatus(context.Background(), hhwrite.JobSearchStatusRequest{Status: "looking_for_offers"})
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client, err := NewClient(Options{BaseURL: base, ChatURL: base, ResumeProfileURL: base, HTTPClient: server.Client(), XSRFToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := testCase.call(client)
			var transportErr *hhwrite.TransportError
			if result.Outcome != hhwrite.OutcomeAmbiguous || !hhwrite.IsAmbiguous(err) || !errors.As(err, &transportErr) || transportErr.Outcome != result.Outcome || calls.Load() != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
			}
		})
	}
}

func TestClientSuccessDecodeFailureIsAmbiguous(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{malformed`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: server.Client(), XSRFToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.LeaveChat(context.Background(), hhwrite.ChatLeaveRequest{ConversationID: "7"})
	if result.Outcome != hhwrite.OutcomeAmbiguous || !hhwrite.IsAmbiguous(err) || calls.Load() != 1 {
		t.Fatalf("decode result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
