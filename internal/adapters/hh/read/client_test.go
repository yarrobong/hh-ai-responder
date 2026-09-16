package hhread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/usecase/hhreadsync"
)

func testClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: server.Client(), SearchParams: url.Values{"text": {"Python/Django"}, "area": {"3"}}, XSRFToken: "fixture-xsrf", UserID: 17, RequestInterval: 0, ReadConcurrency: 1})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server
}

func TestReadVacanciesPreservesResponseCountKnowledgeAndHeaders(t *testing.T) {
	var requests atomic.Int32
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("method=%s", r.Method)
		}
		if got := r.URL.Query().Get("text"); got != "Python/Django" {
			t.Errorf("text query=%q", got)
		}
		if got := r.URL.Query().Get("area"); got != "3" {
			t.Errorf("area query=%q", got)
		}
		if got := r.URL.Query().Get("page"); got != "0" {
			t.Errorf("page query=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("user agent not preserved: %q", got)
		}
		if got := r.Header.Get("Accept-Language"); got != acceptLanguageHeader {
			t.Errorf("language not preserved: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected authorization header")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`prefix,"vacancies":[{"vacancyId":1,"name":"Unknown count","description":"fixture","links":{"desktop":"/vacancy/1"}},{"vacancyId":2,"name":"Zero","description":"fixture","totalResponsesCount":0,"links":{"desktop":"/vacancy/2"}},{"vacancyId":3,"name":"Positive","description":"fixture","totalResponsesCount":9,"links":{"desktop":"/vacancy/3"}}]}`))
	}))
	defer server.Close()
	page, err := client.ReadVacancies(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || len(page.Items) != 3 {
		t.Fatalf("requests=%d items=%d", requests.Load(), len(page.Items))
	}
	for i, want := range []bool{false, true, true} {
		if page.Items[i].TotalResponsesCountKnown != want {
			t.Fatalf("item %d count known=%v", i, page.Items[i].TotalResponsesCountKnown)
		}
	}
	if page.Items[1].TotalResponsesCount != 0 || page.Items[2].TotalResponsesCount != 9 {
		t.Fatalf("counts were not retained: %+v", page.Items)
	}
}

func TestReadVacancyDetailMapsRichProviderFields(t *testing.T) {
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/vacancy":
			_, _ = w.Write([]byte(`prefix,"vacancies":[{"vacancyId":7,"name":"Partial","area":{"name":"Екатеринбург"},"links":{"desktop":"https://hh.example/vacancy/7"}}]}`))
		case "/vacancy/7":
			_, _ = w.Write([]byte(`{"redirectConfig":{"area":{"name":"Екатеринбург"},"workExperience":"between1And3","workFormats":[{"workFormatsElement":["REMOTE"]}],"publicationTime":"2026-09-09T10:00:00+03:00","lastChangeTime":{"$":"2026-09-10T11:00:00+03:00"},"company":{"id":12,"name":"Fixture"}},"vacancyView":{"description":"<p>Python integration</p>","requirements":[{"name":"Python"}],"keySkills":[{"name":"Python"},{"name":"REST API"}],"professional_roles":[{"id":"96","name":"Developer"}],"salary_range":{"from":60000,"currency":"RUR"},"links":{"desktop":"https://hh.example/vacancy/7"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	search, err := client.ReadVacancies(context.Background(), "")
	if err != nil || len(search.Items) != 1 {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	detail, err := client.ReadVacancyDetail(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Description != "<p>Python integration</p>" || len(detail.Requirements) != 1 || len(detail.KeySkills) != 2 || detail.WorkFormat != "remote" || detail.AreaName != "Екатеринбург" || detail.Experience != "between1And3" || detail.Salary != "60000" || detail.Currency != "RUR" || len(detail.ProfessionalRoles) != 1 || detail.PublishedAt.IsZero() || detail.UpdatedAt.IsZero() {
		t.Fatalf("detail mapping lost provider fields: %+v", detail)
	}
}

func TestReadVacancyDetailRejectsUnavailableAndMalformedResponses(t *testing.T) {
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vacancy/404":
			w.WriteHeader(http.StatusNotFound)
		case "/vacancy/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/vacancy/7":
			_, _ = w.Write([]byte(`{"redirectConfig":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, id := range []int{404, 500, 7} {
		if _, err := client.ReadVacancyDetail(context.Background(), id); err == nil {
			t.Fatalf("vacancy %d unexpectedly parsed", id)
		}
	}
}

func TestReadApplicationsMapsObservedNegotiations(t *testing.T) {
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/applicant/negotiations" || r.URL.Query().Get("page") != "0" {
			t.Errorf("request=%s", r.URL.String())
		}
		_, _ = w.Write([]byte(`<script>{"redirectConfig":{},"applicantNegotiations":{"topicList":[{"id":71,"vacancyId":42,"lastState":"RESPONSE","chatId":"51","creationTime":"2026-09-01T12:00:00+03:00","lastModified":"2026-09-02T12:00:00+03:00"}],"paging":{"next":{"page":1,"disabled":false}}},"vacanciesShort":{"vacanciesList":[{"vacancyId":42,"name":"Fixture role","company":{"name":"Fixture company"},"links":{"desktop":"https://hh.example/vacancy/42"}}]}}</script>`))
	}))
	defer server.Close()
	page, err := client.ReadApplications(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor != "1" {
		t.Fatalf("page=%+v", page)
	}
	value := page.Items[0]
	if value.ExternalID != "71" || value.VacancyID != 42 || value.Company != "Fixture company" || value.ConversationExternal != "51" || value.Metadata["hh_id"] != "71" {
		t.Fatalf("value=%+v", value)
	}
}

func TestReadConversationsBoundedExpandsOnlyRequestedPrefix(t *testing.T) {
	var listCalls, detailCalls atomic.Int32
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chatik/api/chats":
			listCalls.Add(1)
			var body strings.Builder
			body.WriteString(`{"chats":{"nextFrom":"opaque-next","items":[`)
			for i := 1; i <= 20; i++ {
				if i > 1 {
					body.WriteByte(',')
				}
				fmt.Fprintf(&body, `{"id":%d}`, i)
			}
			body.WriteString(`]},"resources":{}}`)
			_, _ = w.Write([]byte(body.String()))
		case "/chatik/api/chat_data":
			detailCalls.Add(1)
			id := r.URL.Query().Get("chatId")
			_, _ = fmt.Fprintf(w, `{"chat":{"id":%s,"resources":{},"messages":{"items":[]}},"resources":{}}`, id)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	page, err := client.ReadConversationsBounded(context.Background(), "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.NextCursor != "opaque-next" || listCalls.Load() != 1 || detailCalls.Load() != 3 {
		t.Fatalf("bounded page=%+v listCalls=%d detailCalls=%d", page, listCalls.Load(), detailCalls.Load())
	}
}

func TestReadConversationRetainsServiceUnavailableAndButtons(t *testing.T) {
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chatik/api/chat_data" || r.URL.Query().Get("chatId") != "7" || r.URL.Query().Get("applicantId") != "17" {
			t.Errorf("request=%s", r.URL.String())
		}
		if r.Header.Get("X-Xsrftoken") != "fixture-xsrf" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Errorf("chat headers missing")
		}
		_, _ = w.Write([]byte(`{"chat":{"id":7,"currentParticipantId":"me","resources":{"NEGOTIATION_TOPIC":["topic"]},"lastActivityTime":"2026-09-01T12:00:00Z","messages":{"items":[{"id":1,"type":"SIMPLE","participantId":"me","text":"candidate","creationTime":"2026-09-01T11:00:00Z","actions":{"text_buttons":[{"size":"small","text":"Confirm"}]}},{"id":2,"type":"PARTICIPANT_LEFT","creationTime":"2026-09-01T12:00:00Z","hasContent":false},{"id":3,"type":"SIMPLE","participantId":"hr","creationTime":"2026-09-01T13:00:00Z"}]}},"resources":{"participants":{"hr":{"type":"EMPLOYER_USER"}},"negotiation_topics":{"topic":{"currentApplicantState":"RESPONSE"}}}}`))
	}))
	defer server.Close()
	record, err := client.ReadConversation(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if record.ExternalID != "7" || len(record.Messages) != 3 || record.Messages[0].Actions[0].Label != "Confirm" {
		t.Fatalf("record=%+v", record)
	}
	if record.Messages[1].Sender != "system" || !record.Messages[2].ContentUnavailable || record.Messages[2].Sender != "employer" {
		t.Fatalf("messages=%+v", record.Messages)
	}
}

func TestParseChatDataArchivedCompatibility(t *testing.T) {
	variants := []struct {
		name  string
		json  string
		kind  rawChatArchivedKind
		value bool
	}{
		{name: "false", json: "false", kind: rawChatArchivedBool, value: false},
		{name: "true", json: "true", kind: rawChatArchivedBool, value: true},
		{name: "missing", json: "__missing__", kind: rawChatArchivedMissing},
		{name: "null", json: "null", kind: rawChatArchivedNull},
		{name: "current object", json: `{"@hidden":false}`, kind: rawChatArchivedObject},
	}
	for _, test := range variants {
		t.Run(test.name, func(t *testing.T) {
			payload := archivedFixtureWithValue(test.json)
			parsed, err := parseChatData([]byte(payload), 7)
			if err != nil {
				t.Fatal(err)
			}
			archived := parsed.Resources.Vacancies["42"].Archived
			gotValue, gotValueKnown := archived.boolValue, archived.boolValue != nil
			if archived.kind != test.kind || gotValueKnown != (test.kind == rawChatArchivedBool) || (gotValueKnown && *gotValue != test.value) {
				t.Fatalf("archived=%+v want kind=%d value=%v", archived, test.kind, test.value)
			}
		})
	}
}

func TestParseChatDataArchivedRejectsUnsupportedShapes(t *testing.T) {
	for _, archived := range []string{`"false"`, `1`, `[]`, `{"unexpected":true}`, `{"@hidden":"false"}`} {
		t.Run(archived, func(t *testing.T) {
			_, err := parseChatData([]byte(archivedFixtureWithValue(archived)), 7)
			if err == nil {
				t.Fatal("unsupported archived shape decoded")
			}
			var fieldErr *providerFieldDecodeError
			if !errors.As(err, &fieldErr) || fieldErr.field != "resources.vacancies.archived" {
				t.Fatalf("error=%v, want provider field decode error", err)
			}
		})
	}
}

func TestReadConversationCurrentArchivedObjectPreservesMapping(t *testing.T) {
	fixture, err := os.ReadFile("testdata/chat_detail_archived_object.json")
	if err != nil {
		t.Fatal(err)
	}
	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s, want GET", r.Method)
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	record, err := client.ReadConversation(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if record.ExternalID != "7" || record.VacancyExternalID != "42" || record.VacancyID != 42 {
		t.Fatalf("identity relation changed: %+v", record)
	}
	if len(record.Messages) != 2 {
		t.Fatalf("messages=%d, want 2", len(record.Messages))
	}
	if got := record.Messages; got[0].ExternalID != "101" || got[0].Sender != "employer" || got[0].Direction != "incoming" || got[1].ExternalID != "102" || got[1].Sender != "candidate" || got[1].Direction != "outgoing" {
		t.Fatalf("message identity/sender/direction changed: %+v", got)
	}
	if !record.Messages[0].Timestamp.Before(record.Messages[1].Timestamp) {
		t.Fatalf("message ordering changed: %+v", record.Messages)
	}
	mapped, warnings := hhreadsync.MapMessages(record.Messages)
	if len(warnings) != 0 || len(mapped) != 2 || mapped[0].ID != "hh-message-101" || mapped[1].ID != "hh-message-102" {
		t.Fatalf("sync mapping changed: messages=%+v warnings=%v", mapped, warnings)
	}
}

func archivedFixtureWithValue(archived string) string {
	field := `"archived":` + archived + ","
	if archived == "__missing__" {
		field = ""
	}
	return `{"chat":{"id":7,"currentParticipantId":"me","resources":{"VACANCY":["42"]},"messages":{"items":[]}},"resources":{"vacancies":{"42":{"vacancyId":42,` + field + `"name":"Fixture role"}}}}`
}

func TestRawChatArchivedJSONContract(t *testing.T) {
	value := rawChatArchived{}
	if err := json.Unmarshal([]byte(`{"@hidden":false}`), &value); err != nil {
		t.Fatal(err)
	}
	if value.kind != rawChatArchivedObject || value.boolValue != nil || value.hidden {
		t.Fatalf("value=%+v", value)
	}
}

func TestReadCancellationBeforeThrottleDoesNotCallHTTP(t *testing.T) {
	var calls atomic.Int32
	client, server := testClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ReadVacancies(ctx, ""); err == nil {
		t.Fatal("canceled read succeeded")
	}
	if calls.Load() != 0 {
		t.Fatalf("canceled request reached HTTP: %d", calls.Load())
	}
}

func TestReadTransportPreservesIntervalAnd429Cancellation(t *testing.T) {
	var calls atomic.Int32
	var starts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		starts.Add(1)
		if starts.Load() == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`prefix,"vacancies":[]}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client, err := NewClient(Options{BaseURL: base, ChatURL: base, HTTPClient: server.Client(), RequestInterval: 20 * time.Millisecond, ReadConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.ReadVacancies(ctx, ""); err == nil {
		t.Fatal("429 retry ignored context cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("429 retry started too early: %d calls", calls.Load())
	}
}

func TestReadTransportMapsHTTPStatusesWithoutBodyLeak(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`authorization=fixture-secret`))
			}))
			defer server.Close()
			_, err := client.ReadVacancies(context.Background(), "")
			if err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "authorization") {
				t.Fatalf("unsafe status error: %v", err)
			}
		})
	}
}

func TestReadClientHasOnlyTypedReadCapabilities(t *testing.T) {
	typeName := reflect.TypeOf((*Client)(nil))
	for i := 0; i < typeName.NumMethod(); i++ {
		method := typeName.Method(i)
		if strings.Contains(strings.ToLower(method.Name), "send") || strings.Contains(strings.ToLower(method.Name), "apply") || strings.Contains(strings.ToLower(method.Name), "write") || strings.Contains(strings.ToLower(method.Name), "answer") || strings.Contains(strings.ToLower(method.Name), "raw") || method.Name == "Do" || method.Name == "Request" {
			t.Fatalf("write/raw capability exported: %s", method.Name)
		}
	}
	var _ hhread.VacancyPage
}
