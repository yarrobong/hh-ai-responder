package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stage22Transport struct {
	base    http.RoundTripper
	calls   atomic.Int64
	active  atomic.Int64
	maximum atomic.Int64
	lists   atomic.Int64
}

func (t *stage22Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	if r.URL.Path == "/chatik/api/chats" {
		t.lists.Add(1)
	}
	n := t.active.Add(1)
	defer t.active.Add(-1)
	for old := t.maximum.Load(); n > old; old = t.maximum.Load() {
		if t.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	return t.base.RoundTrip(r)
}
func stage22Trace(c *HHAIResponderReadClient) *stage22Transport {
	base := c.responder.requester.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	tr := &stage22Transport{base: base}
	c.responder.requester.client.Transport = tr
	return tr
}
func TestStage22TargetedReadAndPreflightNeverList(t *testing.T) {
	c := stage22HTTPClient(t, 261, 0)
	tr := stage22Trace(c)
	for i := 0; i < 2; i++ {
		record, err := c.ReadConversation(context.Background(), "261")
		if err != nil || record.ExternalID != "261" {
			t.Fatal("targeted read failed")
		}
	}
	if _, err := c.ReadConversationState(context.Background(), "261"); err != nil {
		t.Fatal(err)
	}
	if tr.calls.Load() != 3 || tr.lists.Load() != 0 {
		t.Fatalf("targeted requests=%d lists=%d", tr.calls.Load(), tr.lists.Load())
	}
}
func TestStage22BoundedReadsAndCancellation(t *testing.T) {
	c := stage22HTTPClient(t, 40, 5*time.Millisecond)
	c.responder.requester.readConcurrency = 4
	tr := stage22Trace(c)
	if _, err := c.ReadConversations(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if tr.maximum.Load() > 4 || tr.maximum.Load() < 2 {
		t.Fatalf("workers maximum=%d", tr.maximum.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := tr.calls.Load()
	if _, err := c.ReadConversations(ctx, ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	if tr.calls.Load() != before {
		t.Fatal("canceled read hit network")
	}
}
func TestStage22Read429BackoffAndNoWriteRetry(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	}))
	defer server.Close()
	requester := NewHHRequester(context.Background(), server.Client(), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if _, err := requester.Do(req); err == nil {
		t.Fatal("backoff did not honor cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("429 retried before Retry-After")
	}
	req, _ = http.NewRequest("POST", server.URL, nil)
	if _, err := requester.Do(req); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("write retried")
	}
}
func TestStage22ReadInterval(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	requester := NewHHRequester(context.Background(), server.Client(), 20*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", server.URL, nil)
			if _, err := requester.Do(req); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 17*time.Millisecond {
			t.Fatal("HH interval bypassed")
		}
	}
}

type stage22BlockingClient struct {
	syncFakeReadClient
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (c *stage22BlockingClient) ReadConversations(ctx context.Context, cursor string) (HHConversationPage, error) {
	c.calls.Add(1)
	select {
	case c.entered <- struct{}{}:
	default:
	}
	select {
	case <-c.release:
		return HHConversationPage{}, nil
	case <-ctx.Done():
		return HHConversationPage{}, ctx.Err()
	}
}
func (c *stage22BlockingClient) ReadConversation(ctx context.Context, id string) (HHConversationRecord, error) {
	_, err := c.ReadConversations(ctx, "")
	return HHConversationRecord{ExternalID: id, Status: "RESPONSE", UpdatedAt: time.Now()}, err
}
func TestStage22CoalescesFullAndTargetedSync(t *testing.T) {
	for _, target := range []string{"conversations", "conversation:12"} {
		t.Run(target, func(t *testing.T) {
			v, a, c, path := newSyncStores(t)
			client := &stage22BlockingClient{entered: make(chan struct{}, 1), release: make(chan struct{})}
			svc := NewHHReadSyncService(client, v, a, c, path)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := svc.syncRead(context.Background(), target); err != nil {
					t.Error(err)
				}
			}()
			<-client.entered
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
					defer cancel()
					svc.syncRead(ctx, target)
				}()
			}
			time.Sleep(80 * time.Millisecond)
			close(client.release)
			wg.Wait()
			if client.calls.Load() != 1 {
				t.Fatalf("duplicate requests=%d", client.calls.Load())
			}
		})
	}
}
func TestStage22DashboardReadsDuringAsyncSync(t *testing.T) {
	s := dashboardTestServer(t)
	client := &stage22BlockingClient{entered: make(chan struct{}, 1), release: make(chan struct{})}
	s.Sync.client = client
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/sync/conversations", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Career-Agent", "local")
	r.Header.Set("Prefer", "respond-async")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	<-client.entered
	if w := dashboardRequest(s, "GET", "/api/inbox", ""); w.Code != 200 {
		t.Fatal("HH network blocked local Inbox")
	}
	r.Body = io.NopCloser(strings.NewReader("{}"))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal("duplicate sync not coalesced")
	}
	close(client.release)
	deadline := time.Now().Add(time.Second)
	for s.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.running.Load() {
		t.Fatal("sync did not finish")
	}
	if client.calls.Load() != 1 {
		t.Fatal("duplicate async sync")
	}
}
func TestStage22ExternalReplacementInvalidatesDisplayCache(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	if err := s.Conversations.Save(); err != nil {
		t.Fatal(err)
	}
	if w := dashboardRequest(s, "GET", "/api/inbox", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	info, _ := os.Stat(s.Conversations.path)
	external := NewConversationStore(s.Conversations.path)
	if err := external.Load(); err != nil {
		t.Fatal(err)
	}
	changed, _ := external.GetConversation(c.ID)
	changed.VacancyTitle = "Externally updated title"
	if _, err := external.UpsertConversation(changed); err != nil {
		t.Fatal(err)
	}
	if err := external.Save(); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(s.Conversations.path, info.ModTime(), info.ModTime())
	w := dashboardRequest(s, "GET", "/api/inbox", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Externally updated title") {
		t.Fatal("external replacement hidden by cache")
	}
	os.WriteFile(s.Conversations.path, []byte("broken"), 0600)
	if w := dashboardRequest(s, "GET", "/api/inbox", ""); w.Code != 500 {
		t.Fatal("corrupt changed file served cached view")
	}
}
func TestStage22BatchMergePreservesExternalEvidence(t *testing.T) {
	v, a, c, path := newSyncStores(t)
	svc := NewHHReadSyncService(&syncFakeReadClient{}, v, a, c, path)
	initial := HHConversationRecord{ExternalID: "1", Status: "RESPONSE", CreatedAt: time.Now().Add(-time.Hour), Messages: []HHMessageRecord{{ExternalID: "m1", Sender: "employer", Text: "fixture", Timestamp: time.Now().Add(-time.Hour)}}}
	if _, err := svc.importConversation(initial); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	external := NewConversationStore(c.path)
	if err := external.Load(); err != nil {
		t.Fatal(err)
	}
	old, _ := external.GetByHHConversationID("1")
	if err := external.AppendMessageTestOnly(old.ID); err != nil {
		t.Fatal(err)
	}
	incoming := initial
	incoming.Messages = append(incoming.Messages, HHMessageRecord{ExternalID: "m3", Sender: "employer", Text: "new", Timestamp: time.Now()})
	result := SyncResult{}
	before := perfSnapshot()["store.ConversationStore.saveUnlocked"].Calls
	if err := svc.commitReadBatch(nil, nil, []HHConversationRecord{incoming}, &result); err != nil {
		t.Fatal(err)
	}
	got, _ := c.GetByHHConversationID("1")
	if len(got.Messages) != 3 {
		t.Fatal("external message lost")
	}
	if perfSnapshot()["store.ConversationStore.saveUnlocked"].Calls-before != 1 {
		t.Fatal("batch did not use one conversation commit")
	}
	result = SyncResult{}
	before = perfSnapshot()["store.ConversationStore.saveUnlocked"].Calls
	if err := svc.commitReadBatch(nil, nil, []HHConversationRecord{incoming}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Unchanged != 1 || perfSnapshot()["store.ConversationStore.saveUnlocked"].Calls != before {
		t.Fatalf("idempotent sync rewrote store: result=%+v before=%d after=%d", result, before, perfSnapshot()["store.ConversationStore.saveUnlocked"].Calls)
	}
}
func (s *ConversationStore) AppendMessageTestOnly(id string) error {
	_, err := s.AppendMessage(id, ConversationMessage{ExternalID: "m2", Sender: ConversationSenderCandidate, Direction: ConversationOutgoing, Source: ConversationSourceHH, Text: "external", Timestamp: time.Now().Add(-time.Minute)})
	if err != nil {
		return err
	}
	return s.Save()
}
func BenchmarkStage22ReadWorkers(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			c := stage22HTTPClient(b, 261, 8*time.Millisecond)
			c.responder.requester.readConcurrency = workers
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.ReadConversations(context.Background(), ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func TestStage22MetadataRefreshOnlySkipsProvenDisplaySnapshot(t *testing.T) {
	now := time.Now().UTC()
	chat := ChatListItem{Id: 1, LastActivityTime: now, LastMessage: &ChatMessage{ID: 2, Text: "latest"}}
	list := &ChatsResponse{}
	c := EmployerConversation{HHConversationID: "1", Messages: []ConversationMessage{{ExternalID: "2", ID: "hh-message-2", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Source: ConversationSourceHH, Text: "latest", Timestamp: now}}, HHMetadata: map[string]string{"sync_list_fingerprint": hhListFingerprint(chat, list), "sync_detail_at": now.Format(time.RFC3339Nano)}}
	ctx := context.WithValue(context.Background(), hhMetadataContextKey{}, map[string]EmployerConversation{"1": c})
	if !hhMetadataUnchanged(ctx, chat, list) {
		t.Fatal("unchanged metadata not recognized")
	}
	if hhMetadataUnchanged(context.Background(), chat, list) {
		t.Fatal("full sync used metadata cache")
	}
	chat.LastMessage.Text = "changed"
	if hhMetadataUnchanged(ctx, chat, list) {
		t.Fatal("changed message skipped")
	}
	chat.LastMessage.Text = "latest"
	chat.LastActivityTime = now.Add(time.Second)
	if hhMetadataUnchanged(ctx, chat, list) {
		t.Fatal("changed metadata skipped")
	}
}

func TestStage22CriticalPreflightReloadsDurableTerminalAction(t *testing.T) {
	g, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, g, draft)
	external := NewApprovedHHActionStore(g.Actions.path)
	if err := external.Load(); err != nil {
		t.Fatal(err)
	}
	changed, _ := external.Get(action.ID)
	changed.Status = HHWriteDeliveryConfirmed
	changed.ExternalMessageID = "delivered"
	now := time.Now()
	changed.NonceUsedAt = &now
	if err := external.put(changed); err != nil {
		t.Fatal(err)
	}
	if err := external.Save(); err != nil {
		t.Fatal(err)
	}
	result := g.PreflightAction(context.Background(), action.ID)
	if result.Allowed || reader.reads != 0 || writer.calls != 0 {
		t.Fatal("preflight ignored durable terminal state")
	}
	current, _ := g.Actions.Get(action.ID)
	if current.Status != HHWriteDeliveryConfirmed {
		t.Fatal("terminal state regressed")
	}
}

func TestStage22ReadConcurrencyConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		env   string
		args  []string
		want  int
		valid bool
	}{{"", nil, 4, true}, {"2", nil, 2, true}, {"invalid", []string{"--hh-read-concurrency=1"}, 1, true}, {"0", nil, 0, false}, {"9", nil, 0, false}, {"invalid", nil, 0, false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			t.Setenv("HH_READ_CONCURRENCY", tc.env)
			previousFlags, previousArgs := flag.CommandLine, os.Args
			defer func() { flag.CommandLine, os.Args = previousFlags, previousArgs }()
			flag.CommandLine = flag.NewFlagSet("stage22", flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)
			os.Args = append([]string{"stage22"}, tc.args...)
			cfg, err := parseConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("configuration valid=%t want=%t", err == nil, tc.valid)
			}
			if err == nil && cfg.HHReadConcurrency != tc.want {
				t.Fatal("CLI/env precedence failed")
			}
		})
	}
}
func TestStage22ContextMatchingParity(t *testing.T) {
	// Equivalent to the pre-optimization implementation, including aliases and
	// punctuation. Facts and relevance must not change with memoization.
	for _, query := range []string{"Опыт Golang, postgres и k8s.", "Есть Django REST API?", "C++, C# и .NET", "а сколько лет?", "", "Python/Django; опыт XML"} {
		for _, names := range [][]string{{"go", "golang", "Kubernetes", "postgresql"}, {"Django", "REST API", "FastAPI", "XML"}, {"C++", "C#", ".NET", "Python"}} {
			expected := []string{}
			for _, name := range names {
				want := uncachedContextCanonical(name)
				tokens := contextTokens(query)
				for i, token := range tokens {
					tokens[i] = uncachedContextCanonical(strings.TrimRight(token, "."))
				}
				if want != "" && strings.Contains(" "+strings.Join(tokens, " ")+" ", " "+want+" ") {
					expected = contextAppendUnique(expected, name)
				}
			}
			if !reflect.DeepEqual(expected, contextMatchingNames(query, names)) {
				t.Fatal("relevance normalization changed")
			}
		}
	}
}
func TestStage22BatchDoesNotAnalyzeUnderFileLock(t *testing.T) {
	v, a, c, path := newSyncStores(t)
	analyzer := stage22LockCheckingAnalyzer{t: t, path: v.path}
	svc := NewHHReadSyncService(&syncFakeReadClient{}, v, a, c, analyzer, path)
	result := SyncResult{}
	if err := svc.commitReadBatch([]HHVacancyRecord{syncFixtureVacancy("Python")}, nil, nil, &result); err != nil {
		t.Fatal(err)
	}
}

type stage22LockCheckingAnalyzer struct {
	t    *testing.T
	path string
}

func (a stage22LockCheckingAnalyzer) Analyze(v Vacancy, k any) MatchResult {
	if _, err := os.Stat(a.path + ".lock"); err == nil {
		a.t.Error("analysis ran under data file lock")
	}
	return NewVacancyAnalyzer().Analyze(v, k)
}

func TestStage22SyntheticActionTimings(t *testing.T) {
	g, reader, writer, _, draft := newGatewayFixture(t, false, nil)
	g.DryRun = true
	reader.state.State = string(ConversationCandidateActionRequired)
	reader.state.ReplyRequirement = ReplyRequired
	start := time.Now()
	edited, err := g.EditDraft(draft.ID, "Есть опыт работы с Python.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture draft edit=%s", time.Since(start))
	start = time.Now()
	action, err := g.ApproveDraft(edited.ID, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture approval=%s", time.Since(start))
	start = time.Now()
	p := g.PreflightAction(context.Background(), action.ID)
	if !p.Allowed {
		t.Fatal("fixture preflight failed")
	}
	t.Logf("fixture local preflight including durable commit=%s", time.Since(start))
	if writer.calls != 0 {
		t.Fatal("unexpected writer call")
	}
}

func TestStage22DeliveryReconciliationReadsOnlyDestination(t *testing.T) {
	g, _, writer, _, draft := newGatewayFixture(t, false, nil)
	g.DryRun = true
	action := approveGatewayDraft(t, g, draft)
	action.Status = HHWriteSentUnconfirmed
	action.ExternalMessageID = "1"
	now := time.Now()
	action.NonceUsedAt = &now
	if err := g.Actions.put(action); err != nil {
		t.Fatal(err)
	}
	if err := g.Actions.Save(); err != nil {
		t.Fatal(err)
	}
	client := stage22HTTPClient(t, 261, 0)
	trace := stage22Trace(client)
	g.ReadClient = client
	result, err := g.ReconcileDelivery(context.Background(), action.ID)
	if err != nil || result.Status != HHWriteDeliveryConfirmed {
		t.Fatal("destination reconciliation failed")
	}
	if trace.calls.Load() != 1 || trace.lists.Load() != 0 || writer.calls != 0 {
		t.Fatal("reconciliation performed unrelated reads or a write")
	}
}
