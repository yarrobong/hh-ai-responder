package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in: private data is copied to a temporary directory, never printed or mutated.
func stage22LocalDashboard(tb testing.TB) *DashboardServer {
	tb.Helper()
	source := os.Getenv("HH_PERF_DATASET")
	if source == "" {
		tb.Skip("set HH_PERF_DATASET to measure a private local dataset")
	}
	dir := tb.TempDir()
	files, err := filepath.Glob(filepath.Join(source, "*.json"))
	if err != nil {
		tb.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(file)), raw, 0600); err != nil {
			tb.Fatal(err)
		}
	}
	start := time.Now()
	s, err := loadDashboard(context.Background(), dir, Config{DryRun: true, AIBaseURL: "http://127.0.0.1:1", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	if err != nil {
		tb.Fatal(err)
	}
	vacancies, err := s.Vacancies.List()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Logf("startup=%s conversations=%d messages=%d applications=%d vacancies=%d actions=%d audit=%d", time.Since(start), len(s.Conversations.conversations), stage22MessageCount(s.Conversations), len(s.Applications.applications), len(vacancies), len(s.WriteGateway.Actions.List()), len(s.WriteGateway.Audit.List()))
	s.Sync.client = &syncFakeReadClient{}
	return s
}
func stage22MessageCount(s *ConversationStore) int {
	n := 0
	for _, c := range s.conversations {
		n += len(c.Messages)
	}
	return n
}
func TestStage22LocalBaseline(t *testing.T) {
	s := stage22LocalDashboard(t)
	for _, path := range []string{"/", "/inbox", "/api/dashboard", "/api/inbox", "/api/applications", "/api/vacancies", "/api/knowledge", "/api/health", "/api/conversations/" + s.Conversations.conversations[0].ID} {
		before := perfSnapshot()
		start := time.Now()
		w := dashboardRequest(s, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		duration := time.Since(start)
		loads := int64(0)
		after := perfSnapshot()
		for k, v := range after {
			if strings.HasSuffix(k, ".Load") {
				loads += v.Calls - before[k].Calls
			}
		}
		name := path
		if strings.HasPrefix(path, "/api/conversations/") {
			name = "/api/conversations/{id}"
		}
		t.Logf("GET %s duration=%s bytes=%d store_loads=%d", name, duration, w.Body.Len(), loads)
		start = time.Now()
		w = dashboardRequest(s, "GET", path, "")
		if w.Code != 200 {
			t.Fatal("cached read failed")
		}
		t.Logf("GET %s repeated=%s", name, time.Since(start))
	}
}
func BenchmarkStage22Local(b *testing.B) {
	s := stage22LocalDashboard(b)
	for _, entry := range []struct {
		name string
		fn   func()
	}{
		{"LoadConversations", func() {
			if err := s.Conversations.Load(); err != nil {
				b.Fatal(err)
			}
		}},
		{"Inbox", func() {
			if _, err := s.inbox(); err != nil {
				b.Fatal(err)
			}
		}},
		{"Conversation", func() {
			if _, err := s.conversationDetail(s.Conversations.conversations[0].ID); err != nil {
				b.Fatal(err)
			}
		}},
		{"Eligibility261", func() {
			if _, err := BuildConversationEligibilityReports(s.Conversations, s.Applications, s.Clarifications, s.Resolver); err != nil {
				b.Fatal(err)
			}
		}},
		{"WriteStatus", func() {
			BuildHHWriteStatusReportWithConversations(Config{DryRun: true}, s.WriteGateway.Actions, s.WriteGateway.Audit, s.Conversations)
		}},
	} {
		b.Run(entry.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				entry.fn()
			}
		})
	}
}

func stage22HTTPClient(tb testing.TB, count int, latency time.Duration) *HHAIResponderReadClient {
	tb.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			tb.Error("unexpected HH write")
			w.WriteHeader(405)
			return
		}
		time.Sleep(latency)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/chatik/api/chats" {
			fmt.Fprint(w, `{"chats":{"items":[`)
			for i := 1; i <= count; i++ {
				if i > 1 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"id":%d}`, i)
			}
			fmt.Fprint(w, `]},"resources":{}}`)
			return
		}
		id := r.URL.Query().Get("chatId")
		fmt.Fprintf(w, `{"chat":{"id":%s,"currentParticipantId":"me","resources":{"NEGOTIATION_TOPIC":["topic"]},"lastActivityTime":"2026-09-01T12:00:00Z","messages":{"items":[{"id":1,"type":"SIMPLE","participantId":"me","text":"fixture","creationTime":"2026-09-01T12:00:00Z"}]}},"resources":{"negotiation_topics":{"topic":{"currentApplicantState":"RESPONSE"}}}}`, id)
	}))
	tb.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	jar := &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	jar.SetCookies(base, []*http.Cookie{{Name: "_xsrf", Value: "fixture"}})
	r := &HHAIResponder{ctx: context.Background(), baseURL: base, chatURL: server.URL, client: server.Client(), jar: jar}
	r.requester = NewHHRequester(context.Background(), r.client, 0)
	r.requester.readOnly = true
	return NewHHAIResponderReadClient(r)
}
func TestStage22NetworkBaseline(t *testing.T) {
	logger = NewLogger(io.Discard, LevelError)
	c := stage22HTTPClient(t, 261, time.Millisecond)
	start := time.Now()
	page, err := c.ReadConversations(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("full count=%d duration=%s", len(page.Items), time.Since(start))
	start = time.Now()
	_, err = c.ReadConversation(context.Background(), "261")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("targeted duration=%s", time.Since(start))
	start = time.Now()
	_, err = c.ReadConversationState(context.Background(), "261")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("preflight read duration=%s", time.Since(start))
}

// Explicit opt-in to GET-only contract verification. Cookies are copied to a
// private temp file; no candidate store, prompt, cookie or body is printed.
func TestStage22LiveReadContract(t *testing.T) {
	if os.Getenv("HH_PERF_LIVE_READ") != "true" {
		t.Skip("explicit live read opt-in required")
	}
	if os.Getenv("HH_WRITE_ENABLED") != "false" || os.Getenv("HH_DRY_RUN") != "true" {
		t.Fatal("unsafe performance environment")
	}
	dir := t.TempDir()
	raw, err := os.ReadFile("cookies.txt")
	if err != nil {
		t.Fatal("local cookies unavailable")
	}
	cookiePath := filepath.Join(dir, "cookies.txt")
	if os.WriteFile(cookiePath, raw, 0600) != nil {
		t.Fatal("cannot stage private cookies")
	}
	previous := logger
	logger = NewLogger(io.Discard, LevelError)
	defer func() { logger = previous }()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	start := time.Now()
	r, err := NewHHAIResponder(ctx, dashboardReadConfig(Config{CookiesPath: cookiePath, RequestInterval: defaultRequestInterval, HHReadConcurrency: 4}))
	if err != nil {
		t.Fatal("GET-only HH initialization failed")
	}
	t.Logf("live reader cold initialization=%s", time.Since(start))
	client := NewHHAIResponderReadClient(r)
	start = time.Now()
	page, err := client.readChatsContext(ctx, "")
	if err != nil {
		t.Fatal("GET-only chat list unavailable")
	}
	t.Logf("live list items=%d duration=%s next_cursor_present=%t", len(page.Chats.Items), time.Since(start), page.Chats.NextFrom != "")
	if len(page.Chats.Items) == 0 {
		return
	}
	chat := page.Chats.Items[0]
	start = time.Now()
	data, err := client.readChatData(ctx, chat.Id)
	if err != nil {
		t.Fatal("GET-only chat detail unavailable")
	}
	t.Logf("live detail duration=%s identity_match=%t last_activity_present=%t last_message_present=%t topic_refs=%d vacancy_refs=%d messages=%d has_more=%t", time.Since(start), data.Chat.ID == chat.Id, !chat.LastActivityTime.IsZero(), chat.LastMessage != nil, len(data.Chat.Resources.NegotiationTopic), len(data.Chat.Resources.Vacancy), len(data.Chat.Messages.Items), data.Chat.Messages.HasMore)
	start = time.Now()
	_, err = client.ReadConversationState(ctx, fmt.Sprint(chat.Id))
	if err != nil {
		t.Fatal("GET-only preflight read failed")
	}
	t.Logf("live targeted preflight read=%s", time.Since(start))
	if os.Getenv("HH_PERF_LIVE_FULL") == "true" {
		local := stage22LocalDashboard(t)
		local.Sync.client = client
		start = time.Now()
		result, err := local.Sync.SyncConversations(ctx)
		if err != nil || len(result.Errors) > 0 {
			t.Fatal("GET-only full sync failed")
		}
		t.Logf("live full fetched=%d duration=%s requests=%d detail_misses=%d", result.Fetched, time.Since(start), result.Performance.Requests, result.Performance.CacheMisses)
		start = time.Now()
		result, err = local.Sync.syncRead(ctx, "inbox")
		if err != nil || len(result.Errors) > 0 {
			t.Fatal("GET-only metadata sync failed")
		}
		t.Logf("live metadata fetched=%d duration=%s requests=%d detail_hits=%d detail_misses=%d", result.Fetched, time.Since(start), result.Performance.Requests, result.Performance.CacheHits, result.Performance.CacheMisses)
	}

}

func BenchmarkStage22CurrentAction(b *testing.B) {
	actions := []DashboardAction{}
	for i := 0; i < 261; i++ {
		actions = append(actions, DashboardAction{ApprovedHHAction: ApprovedHHAction{ID: fmt.Sprint(i), Status: HHWriteDeliveryConfirmed, CreatedAt: time.Now()}})
	}
	actions = append(actions, DashboardAction{ApprovedHHAction: ApprovedHHAction{ID: "approved", Status: HHWriteApproved}})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selectDashboardActions(actions)
	}
}
func BenchmarkStage22BatchMerge(b *testing.B) {
	dir := b.TempDir()
	c := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	svc := NewHHReadSyncServiceWithOptions(&syncFakeReadClient{}, HHReadSyncServiceOptions{Conversations: c, StatePath: filepath.Join(dir, HHSyncStateFilename)})
	records := []HHConversationRecord{}
	now := time.Now().UTC()
	for i := 0; i < 261; i++ {
		records = append(records, HHConversationRecord{ExternalID: fmt.Sprint(i + 1), Status: "RESPONSE", CreatedAt: now, UpdatedAt: now, Messages: []HHMessageRecord{{ExternalID: fmt.Sprint(i + 1), Sender: "candidate", Text: "fixture", Timestamp: now}}})
	}
	result := SyncResult{}
	if err := svc.commitReadBatch(nil, nil, records, &result); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result = SyncResult{}
		if err := svc.commitReadBatch(nil, nil, records, &result); err != nil {
			b.Fatal(err)
		}
		if result.Unchanged != 261 {
			b.Fatal("non-idempotent batch")
		}
	}
}
func BenchmarkStage22Reconciliation(b *testing.B) {
	s := stage22LocalDashboard(b)
	r := NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(s.Vacancies), NewJSONApplicationRepository(s.Applications), NewJSONConversationRepository(s.Conversations), NewVacancyAnalyzer(), s.Knowledge)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Reconcile(true); err != nil {
			b.Fatal(err)
		}
	}
}
