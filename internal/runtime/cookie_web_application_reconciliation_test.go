package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"hh-ai-responder/internal/hhwebsession"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

func TestCookieWebApplicationEvidenceReaderScopesProviderEvidenceToVacancy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"vacancyId":42,"alreadyResponded":false,"canApply":true}}`)
		case "/applicant/negotiations":
			_, _ = io.WriteString(w, `<script>{"redirectConfig":{},"applicantNegotiations":{"topicList":[{"id":"target-topic","vacancyId":42,"initialTopicType":"RESPONSE_BY_APPLICANT"},{"id":"other-topic","vacancyId":99,"initialTopicType":"RESPONSE_BY_APPLICANT"}],"paging":{"next":{"disabled":true}}},"vacanciesShort":{"vacanciesList":[]}}</script>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	reader := newCookieWebApplicationEvidenceReader(session.ReadClient(), base)
	snapshot, err := reader.ReadVacancyResponseEvidence(context.Background(), applicationreconciliation.Target{VacancyID: 42, ResumeID: "approved-hash"})
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if !snapshot.PreflightAvailable || !snapshot.ApplicationsAvailable || len(snapshot.Applications) != 1 || snapshot.Applications[0].VacancyID != 42 || snapshot.Applications[0].NegotiationID != "target-topic" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestCookieWebApplicationEvidenceReaderDoesNotConfirmLocalAttemptAlone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"redirectConfig":{"vacancyId":42,"alreadyResponded":false,"canApply":true}}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, _ := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	reader := newCookieWebApplicationEvidenceReader(session.ReadClient(), base)
	snapshot, err := reader.ReadVacancyResponseEvidence(context.Background(), applicationreconciliation.Target{VacancyID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Applications) != 0 || snapshot.Preflight.AlreadyResponded {
		t.Fatalf("local/no provider state confirmed delivery: %+v", snapshot)
	}
}
