package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInspectHHWebPageFixtures(t *testing.T) {
	tests := []struct {
		name           string
		kind           string
		body           string
		wantClass      WebPageClass
		wantResponded  bool
		wantCanApply   bool
		wantForm       bool
		wantTest       bool
		wantTestScoped bool
	}{
		{
			name:          "known responded HTML",
			kind:          webTraceResponse,
			body:          `<main data-qa="vacancy-response-status">Вы уже откликались на эту вакансию</main>`,
			wantClass:     WebPageVacancyResponded,
			wantResponded: true,
		},
		{
			name:         "active vacancy apply button",
			kind:         webTraceVacancy,
			body:         `<a data-qa="vacancy-response-link" href="/applicant/vacancy_response?vacancyId=42">Откликнуться</a>`,
			wantClass:    WebPageVacancyActive,
			wantCanApply: true,
		},
		{
			name:         "valid application form GET",
			kind:         webTraceResponse,
			body:         `<form method="post" action="/applicant/vacancy_response?vacancyId=42"><input type="hidden" name="vacancyId" value="42"><textarea name="letter"></textarea><select name="resume"></select></form>`,
			wantClass:    WebPageApplicationForm,
			wantCanApply: true,
			wantForm:     true,
		},
		{
			name:      "missing button is unknown",
			kind:      webTraceResponse,
			body:      `<main data-qa="vacancy-response-page">Нет доступного действия</main>`,
			wantClass: WebPageUnknown,
		},
		{
			name:      "generic test UI is unknown",
			kind:      webTraceResponse,
			body:      `<div data-qa="generic-test-component"><span>Тест</span></div>`,
			wantClass: WebPageUnknown,
		},
		{
			name:      "generic embedded component state",
			kind:      webTraceResponse,
			body:      `{"componentState":{"testRequired":true,"canApply":false}}`,
			wantClass: WebPageUnknown,
		},
		{
			name:           "explicit vacancy test",
			kind:           webTraceResponse,
			body:           `{"redirectConfig":{"vacancyId":42,"testPresent":true}}`,
			wantClass:      WebPageApplicationTest,
			wantTest:       true,
			wantTestScoped: true,
		},
		{
			name:      "other vacancy marker ignored",
			kind:      webTraceResponse,
			body:      `{"redirectConfig":{"vacancyId":99,"alreadyResponded":true,"testPresent":true,"canApply":true}}`,
			wantClass: WebPageUnknown,
		},
		{
			name:      "login page",
			kind:      webTraceResponse,
			body:      `<a href="/account/login">Войти</a>`,
			wantClass: WebPageLoginRequired,
		},
		{
			name:      "challenge page",
			kind:      webTraceResponse,
			body:      `<div class="captcha-container">challenge</div>`,
			wantClass: WebPageChallenge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := inspectHHWebPage([]byte(test.body), 42, test.kind)
			if got.Class != test.wantClass || got.ExplicitRespondedMarker != test.wantResponded || got.ApplicationFormPresent != test.wantForm || got.TestMarkerPresent != test.wantTest || got.TestMarkerVacancyScoped != test.wantTestScoped {
				t.Fatalf("inspection=%+v", got)
			}
			if test.wantCanApply && !got.ExplicitApplyAction && !got.StateCanApply && !(got.ApplicationFormPresent && got.FormVacancyIDMatches) {
				t.Fatalf("expected apply evidence: %+v", got)
			}
		})
	}
}

func TestParseVacancyPreflightTriStateFixtures(t *testing.T) {
	tests := []struct {
		name              string
		body              string
		wantResponded     AlreadyRespondedValue
		wantCanApplyKnown bool
		wantCanApply      bool
		wantTestKnown     bool
		wantTest          bool
	}{
		{name: "active button", body: `<a data-qa="vacancy-response-link" href="/applicant/vacancy_response?vacancyId=42">Откликнуться</a>`, wantResponded: AlreadyRespondedNo, wantCanApplyKnown: true, wantCanApply: true},
		{name: "missing button", body: `<main>vacancy response</main>`, wantResponded: AlreadyRespondedUnknown},
		{name: "generic test UI", body: `<div data-qa="generic-test-component">test</div>`, wantResponded: AlreadyRespondedUnknown},
		{name: "explicit scoped test", body: `{"redirectConfig":{"vacancyId":42,"testPresent":true}}`, wantResponded: AlreadyRespondedUnknown, wantTestKnown: true, wantTest: true},
		{name: "other vacancy ignored", body: `{"redirectConfig":{"vacancyId":99,"alreadyResponded":true,"canApply":true,"testPresent":true}}`, wantResponded: AlreadyRespondedUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseVacancyPreflight([]byte(test.body), Vacancy{ID: 42}, "https://example.test/applicant/vacancy_response?vacancyId=42")
			if err != nil {
				t.Fatal(err)
			}
			if evidence := got.alreadyRespondedEvidence(); evidence.Value != test.wantResponded {
				t.Fatalf("responded evidence=%+v", evidence)
			}
			if got.CanApplyKnown != test.wantCanApplyKnown || got.CanApply != test.wantCanApply || got.TestPresentKnown != test.wantTestKnown || got.TestPresent != test.wantTest {
				t.Fatalf("preflight=%+v", got)
			}
		})
	}
}

func TestWebTraceNeverPOSTs(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<a data-qa="vacancy-response-link" href="/applicant/vacancy_response?vacancyId=42">Откликнуться</a>`)
	}))
	defer server.Close()
	baseURL := mustURL(t, server.URL)
	ctx := context.Background()
	r := &HHAIResponder{ctx: ctx, baseURL: baseURL, requester: NewHHRequester(ctx, server.Client(), 0)}
	if _, err := r.traceVacancyWeb(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 0 {
		t.Fatalf("diagnostic issued %d non-GET requests", posts.Load())
	}
}

func TestRenderWebTraceContainsOnlySafeMetadata(t *testing.T) {
	line := renderWebTraceRecord(WebTraceRecord{VacancyID: 42, RequestKind: webTraceResponse, InitialURLPath: "/applicant/vacancy_response", FinalURLPath: "/applicant/vacancy_response", Status: 200, ContentType: "text/html", PageClassification: WebPageUnknown})
	if strings.Contains(strings.ToLower(line), "cookie") || strings.Contains(strings.ToLower(line), "authorization") || strings.Contains(line, "<") {
		t.Fatalf("unsafe trace output: %s", line)
	}
}
