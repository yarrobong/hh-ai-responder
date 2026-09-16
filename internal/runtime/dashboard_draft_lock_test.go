package runtime

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingDraftAI struct {
	response string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	calls    atomic.Int32
}

func (f *blockingDraftAI) ChatStructuredWithSchema(_ string, _ string, _ int, _ float64, _ *ChatJSONSchema, validator func(string) error) (string, error) {
	f.calls.Add(1)
	f.once.Do(func() { close(f.started) })
	<-f.release
	if validator != nil {
		if err := validator(f.response); err != nil {
			return "", err
		}
	}
	return f.response, nil
}

func asyncDraftTestServer(t *testing.T, ai StructuredAIClient) (*DashboardServer, EmployerConversation) {
	t.Helper()
	s := dashboardTestServer(t)
	_, conversation := dashboardFixture(t, s)
	kb := contextTestKnowledge()
	kb.ProfilePath = s.Knowledge.ProfilePath
	*s.Knowledge = *kb
	s.Resolver = NewCandidateContextResolver(s.Knowledge)
	s.Orchestrator = NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{
		ConversationStore: s.Conversations, Resolver: s.Resolver, Drafts: s.Drafts,
		Clarifications: s.Clarifications, Updater: s.ProposalUpdater,
	})
	s.backgroundContext = context.Background()
	return s, conversation
}

func waitDraftWorkersDone(t *testing.T, s *DashboardServer) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if s.draftWorkersActive.Load() == 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("Inbox draft worker did not finish")
		case <-ticker.C:
		}
	}
}

func draftTestItem(t *testing.T, s *DashboardServer, conversationID string) CandidateInboxItem {
	t.Helper()
	c, err := s.Conversations.GetConversation(conversationID)
	if err != nil {
		t.Fatal(err)
	}
	latest := deliveredMessages(c.Messages)[len(deliveredMessages(c.Messages))-1]
	return CandidateInboxItem{Conversation: c, LatestMessage: &latest, Workflow: CareerWorkflowProjection{State: WorkflowNeedsReply}}
}

func TestInboxDraftWorkerDoesNotHoldDashboardLockDuringPreparation(t *testing.T) {
	ai := &blockingDraftAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil), started: make(chan struct{}), release: make(chan struct{})}
	s, conversation := asyncDraftTestServer(t, ai)

	item := draftTestItem(t, s, conversation.ID)
	s.scheduleInboxDrafts([]CandidateInboxItem{item})
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("draft worker did not reach the blocking preparation seam")
	}
	if code := dashboardRequest(s, http.MethodGet, "/api/inbox", "").Code; code != http.StatusOK {
		t.Fatalf("inbox status=%d", code)
	}
	if !s.mu.TryLock() {
		t.Fatal("draft preparation still owns DashboardServer.mu")
	}
	s.mu.Unlock()
	if got := dashboardRequest(s, http.MethodGet, "/api/notifications", "").Code; got != http.StatusOK {
		t.Fatalf("notifications status=%d", got)
	}
	close(ai.release)
	waitDraftWorkersDone(t, s)
	values, err := s.Drafts.List()
	if err != nil || len(values) != 1 || values[0].ConversationID != conversation.ID {
		t.Fatalf("successful detached draft was not committed: %+v err=%v", values, err)
	}
}

func TestInboxDraftWorkerDiscardsStaleResult(t *testing.T) {
	ai := &blockingDraftAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil), started: make(chan struct{}), release: make(chan struct{})}
	s, conversation := asyncDraftTestServer(t, ai)

	s.scheduleInboxDrafts([]CandidateInboxItem{draftTestItem(t, s, conversation.ID)})
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("draft worker did not reach the blocking preparation seam")
	}
	if _, err := s.Conversations.AppendMessage(conversation.ID, conversationTestMessage("new", ConversationSenderEmployer, "А ещё вопрос про Docker?", 3)); err != nil {
		t.Fatal(err)
	}
	close(ai.release)
	waitDraftWorkersDone(t, s)
	values, err := s.Drafts.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("stale draft was committed: %+v", values)
	}
	if !s.mu.TryLock() {
		t.Fatal("stale-result path leaked DashboardServer.mu")
	}
	s.mu.Unlock()
}

func TestInboxDraftSchedulingDeduplicatesConcurrentTarget(t *testing.T) {
	ai := &blockingDraftAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil), started: make(chan struct{}), release: make(chan struct{})}
	s, conversation := asyncDraftTestServer(t, ai)
	item := draftTestItem(t, s, conversation.ID)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.scheduleInboxDrafts([]CandidateInboxItem{item})
		}()
	}
	wg.Wait()
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("deduplicated worker did not start")
	}
	if got := ai.calls.Load(); got != 1 {
		t.Fatalf("duplicate AI workers started: %d", got)
	}
	close(ai.release)
	waitDraftWorkersDone(t, s)
	values, err := s.Drafts.List()
	if err != nil || len(values) != 1 {
		t.Fatalf("duplicate scheduling committed %d drafts: %+v err=%v", len(values), values, err)
	}
}

func TestInboxDraftWorkerFailureReleasesBookkeeping(t *testing.T) {
	ai := &blockingDraftAI{response: "not-json", started: make(chan struct{}), release: make(chan struct{})}
	s, conversation := asyncDraftTestServer(t, ai)
	item := draftTestItem(t, s, conversation.ID)
	s.scheduleInboxDrafts([]CandidateInboxItem{item})
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("draft worker did not start")
	}
	close(ai.release)
	waitDraftWorkersDone(t, s)
	s.statusMu.Lock()
	remaining := len(s.draftTargets)
	s.statusMu.Unlock()
	if remaining != 0 {
		t.Fatalf("failed worker leaked in-flight targets: %d", remaining)
	}
	if !s.mu.TryLock() {
		t.Fatal("failed worker leaked DashboardServer.mu")
	}
	s.mu.Unlock()
}
