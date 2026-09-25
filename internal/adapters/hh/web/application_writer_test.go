package hhweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hh-ai-responder/internal/hhwebsession"
	hhwrite "hh-ai-responder/internal/ports/hhwrite"
)

func TestCookieWebVacancyResponseWriterBuildsExactApprovedRequest(t *testing.T) {
	var received http.Request
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			w.Header().Add("Set-Cookie", "auth=ok; Path=/")
			w.Header().Add("Set-Cookie", "_xsrf=xsrf-secret; Path=/")
			w.WriteHeader(http.StatusOK)
			return
		}
		received = *r
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"responseId":"response-42"}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/seed"))
	if err != nil {
		t.Fatal(err)
	}
	_ = seed.Body.Close()
	writer, err := newCookieWebVacancyResponseWriter(base, session, "fixture-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := writer.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 42, ResumeHash: "approved-browser-hash", Letter: "approved letter", RefererURL: server.URL + "/vacancy/42", IgnorePostponed: "true"})
	if err != nil || result.Class != hhwrite.ApplicationResultSuccess || result.ProviderID != "response-42" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if received.Method != http.MethodPost || received.URL.Path != "/applicant/vacancy_response/popup" || received.Header.Get("Cookie") == "" {
		t.Fatalf("request=%+v cookie=%q", received, received.Header.Get("Cookie"))
	}
	if received.Header.Get("User-Agent") != "fixture-agent" || received.Header.Get("Accept") != "application/json" || received.Header.Get("X-Requested-With") != "XMLHttpRequest" || received.Header.Get("X-Xsrftoken") != "xsrf-secret" || received.Header.Get("Referer") != server.URL+"/vacancy/42" {
		t.Fatalf("headers=%v", received.Header)
	}
	if got := form.Get("_xsrf"); got != "xsrf-secret" || form.Get("vacancy_id") != "42" || form.Get("resume_hash") != "approved-browser-hash" || form.Get("letter") != "approved letter" || form.Get("ignore_postponed") != "true" {
		t.Fatalf("form=%v", form)
	}
	if _, exists := form["uidPk"]; exists {
		t.Fatal("test payload was emitted")
	}
	if strings.Contains(jsonString(result), "xsrf-secret") {
		t.Fatal("XSRF leaked into result")
	}
}

func TestCookieWebVacancyResponseWriterRejectsNonHHProductionBase(t *testing.T) {
	base, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCookieWebVacancyResponseWriter(base, session, "agent"); err == nil {
		t.Fatal("production writer accepted a non-HH base URL")
	}
}

func TestCookieWebVacancyResponseWriterRejectsTestAndUnexpectedRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			w.Header().Set("Set-Cookie", "_xsrf=token; Path=/")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/seed"))
	if seed != nil {
		_ = seed.Body.Close()
	}
	writer, _ := newCookieWebVacancyResponseWriter(base, session, "agent", true)
	request := hhwrite.VacancyResponseRequest{VacancyID: 1, ResumeHash: "hash", RefererURL: server.URL + "/vacancy/1", IgnorePostponed: "true", Test: &hhwrite.VacancyTestSubmission{GUID: "not-supported"}}
	result, err := writer.SubmitVacancyResponse(context.Background(), request)
	if err == nil || result.Outcome != hhwrite.OutcomeNotSent || result.Metadata["blocked_phase"] != "pre_send" {
		t.Fatalf("test rejection result=%+v err=%v", result, err)
	}
	request.Test = nil
	result, err = writer.SubmitVacancyResponse(context.Background(), request)
	if err == nil || result.Outcome != hhwrite.OutcomeAmbiguous || !hhwrite.IsAmbiguous(err) {
		t.Fatalf("redirect result=%+v err=%v", result, err)
	}
}

func TestCookieWebVacancyResponseWriterClassifiesDeterministicFailures(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		class   hhwrite.ApplicationResultClass
		outcome hhwrite.Outcome
	}{
		{http.StatusUnauthorized, `{}`, hhwrite.ApplicationResultAuthRequired, hhwrite.OutcomeRejected},
		{http.StatusUnauthorized, `{"error":"already applied"}`, hhwrite.ApplicationResultAuthRequired, hhwrite.OutcomeRejected},
		{http.StatusForbidden, `{"error":"already applied"}`, hhwrite.ApplicationResultManualChallenge, hhwrite.OutcomeRejected},
		{http.StatusTooManyRequests, `{}`, hhwrite.ApplicationResultRateLimited, hhwrite.OutcomeRejected},
		{http.StatusTooManyRequests, `{"error":"already applied"}`, hhwrite.ApplicationResultRateLimited, hhwrite.OutcomeRejected},
		{http.StatusInternalServerError, `{"error":"already applied"}`, hhwrite.ApplicationResultUnknownSendResult, hhwrite.OutcomeAmbiguous},
		{http.StatusFound, `{"error":"already applied"}`, hhwrite.ApplicationResultUnknownSendResult, hhwrite.OutcomeAmbiguous},
		{http.StatusOK, `not-json: already applied`, hhwrite.ApplicationResultUnknownSendResult, hhwrite.OutcomeAmbiguous},
		{http.StatusOK, `{"error":"already applied"}`, hhwrite.ApplicationResultAlreadyApplied, hhwrite.OutcomeAccepted},
		{http.StatusUnprocessableEntity, `{"error":"already applied"}`, hhwrite.ApplicationResultAlreadyApplied, hhwrite.OutcomeAccepted},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				w.Header().Set("Set-Cookie", "_xsrf=token; Path=/")
				return
			}
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		base, _ := url.Parse(server.URL)
		session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
		if err != nil {
			t.Fatal(err)
		}
		seed, _ := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/seed"))
		if seed != nil {
			_ = seed.Body.Close()
		}
		writer, _ := newCookieWebVacancyResponseWriter(base, session, "agent", true)
		result, err := writer.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 1, ResumeHash: "hash", RefererURL: server.URL + "/vacancy/1", IgnorePostponed: "true"})
		server.Close()
		if err == nil || result.Class != tc.class || result.Outcome != tc.outcome {
			t.Errorf("status=%d result=%+v err=%v", tc.status, result, err)
		}
	}
}

func TestCookieWebVacancyResponseWriterStopsOnPersistenceFailureBeforeAndAfterPost(t *testing.T) {
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			w.Header().Set("Set-Cookie", "_xsrf=token; Path=/")
			return
		}
		postCount++
		w.Header().Set("Set-Cookie", "server-state=changed; Path=/")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	missingPath := filepath.Join(t.TempDir(), "missing", "cookies.txt")
	session, _ := hhwebsession.New(missingPath, hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	writer, _ := newCookieWebVacancyResponseWriter(base, session, "agent", true)
	result, err := writer.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 1, ResumeHash: "hash", RefererURL: server.URL + "/vacancy/1", IgnorePostponed: "true"})
	if err == nil || result.Metadata["transport_attempted"] != "false" || postCount != 0 {
		t.Fatalf("pre-send persistence result=%+v err=%v posts=%d", result, err, postCount)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.txt")
	session, _ = hhwebsession.New(path, hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	seed, _ := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/seed"))
	if seed != nil {
		_ = seed.Body.Close()
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	writer, _ = newCookieWebVacancyResponseWriter(base, session, "agent", true)
	result, err = writer.SubmitVacancyResponse(context.Background(), hhwrite.VacancyResponseRequest{VacancyID: 1, ResumeHash: "hash", RefererURL: server.URL + "/vacancy/1", IgnorePostponed: "true"})
	if err == nil || result.Metadata["transport_attempted"] != "true" || result.Metadata["reconciliation_required"] != "true" || postCount != 1 {
		t.Fatalf("post persistence result=%+v err=%v posts=%d", result, err, postCount)
	}
}

func jsonString(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

var _ = errors.Is
