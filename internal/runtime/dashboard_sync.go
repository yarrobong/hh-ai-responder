package runtime

import (
	"context"
	"net/http"
	"strings"
	"time"

	"hh-ai-responder/internal/platform/scheduler"
	"hh-ai-responder/internal/usecase/inboxrefresh"
)

func conversationHHCheckedAt(c EmployerConversation) time.Time {
	if c.HHMetadata == nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339Nano, c.HHMetadata["sync_detail_at"])
	if err != nil {
		return time.Time{}
	}
	return at
}

func (s *DashboardServer) targetedRefreshRunning(hhID string) bool {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.asyncTargets["conversation:"+hhID]
}

// scheduleInboxDrafts starts only local/AI work after the Inbox projection has
// been returned. It never calls HH and the orchestrator's cache key prevents
// regeneration when employer message, relevant knowledge and prompt version
// are unchanged.
func (s *DashboardServer) scheduleInboxDrafts(items []CandidateInboxItem) {
	if s == nil || s.backgroundContext == nil || s.Orchestrator == nil {
		return
	}
	for _, item := range items {
		if item.Workflow.State != WorkflowNeedsReply || item.LatestMessage == nil || item.LatestMessage.Sender != ConversationSenderEmployer {
			continue
		}
		fresh := false
		currentCacheKey := ""
		if context, err := NewConversationContextBuilder(s.Conversations, s.Resolver).BuildForReply(item.Conversation.ID); err == nil {
			_, _, currentCacheKey = employerDraftMetadata(s.Resolver, context)
		}
		for _, draft := range item.AIDrafts {
			if draft.Type == AIDraftEmployerReply && draft.Status == AIDraftGenerated && draft.InputMessageID == item.LatestMessage.ID && currentCacheKey != "" && draft.InputFingerprint == currentCacheKey {
				fresh = true
				break
			}
		}
		if fresh {
			continue
		}
		target := "draft:" + item.Conversation.ID
		s.statusMu.Lock()
		if s.draftTargets == nil {
			s.draftTargets = map[string]bool{}
		}
		if s.draftTargets[target] {
			s.statusMu.Unlock()
			continue
		}
		s.draftTargets[target] = true
		s.statusMu.Unlock()
		go s.generateInboxDraft(target, item.Conversation.ID)
	}
}

func (s *DashboardServer) generateInboxDraft(target, conversationID string) {
	defer func() {
		s.statusMu.Lock()
		delete(s.draftTargets, target)
		s.statusMu.Unlock()
	}()
	select {
	case <-s.backgroundContext.Done():
		return
	default:
	}
	// Store mutation is serialized with ordinary dashboard reads/writes. The
	// AI request is deliberately outside HH transport and remains background
	// work, so the initial Inbox render is not delayed by it.
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Conversations.GetConversation(conversationID); err != nil {
		return
	}
	decision, err := s.Orchestrator.PrepareEmployerReply(conversationID)
	if err != nil {
		return
	}
	if err := s.Drafts.Save(); err != nil {
		return
	}
	if err := s.Clarifications.Save(); err != nil {
		return
	}
	s.decisions[conversationID] = decision
	s.invalidateViews()
}

func (s *DashboardServer) startBackgroundInboxRefresh(ctx context.Context, interval time.Duration) {
	if !s.BackgroundInboxRefresh || interval <= 0 || !s.backgroundRefreshStarted.CompareAndSwap(false, true) {
		return
	}
	go func() {
		_ = (scheduler.Ticker{
			Interval: interval,
			Clock:    scheduler.RealClock{},
		}).Run(ctx, func(ctx context.Context) {
			s.queueBackgroundInboxRefresh(ctx)
		})
	}()
}

func (s *DashboardServer) queueBackgroundInboxRefresh(ctx context.Context) {
	target := "inbox"
	s.statusMu.Lock()
	if s.asyncTargets == nil {
		s.asyncTargets = map[string]bool{}
	}
	if s.asyncTargets[target] || len(s.asyncTargets) >= 8 {
		s.statusMu.Unlock()
		return
	}
	s.asyncTargets[target] = true
	s.running.Store(true)
	s.statusMu.Unlock()
	go func() {
		defer func() {
			s.statusMu.Lock()
			delete(s.asyncTargets, target)
			s.running.Store(len(s.asyncTargets) > 0)
			s.statusMu.Unlock()
		}()
		_, _ = s.executeSync(withHHReadPriority(ctx, hhReadBackground), target)
	}()
}

// A sync job owns no Dashboard/store lock while waiting for HH. Imports acquire
// Dashboard.mu briefly through the service's commit mutex.
func (s *DashboardServer) handleSync(w http.ResponseWriter, r *http.Request, p []string) {
	if !decodeDashboardBody(w, r, &struct{}{}) {
		return
	}
	// Preserve the synchronous API's duplicate-operation contract. Browser
	// refreshes use Prefer: respond-async and are coalesced below; a legacy
	// synchronous caller must not deadlock behind an intentionally held local
	// operation lock.
	if s.running.Load() && !strings.Contains(r.Header.Get("Prefer"), "respond-async") {
		dashboardError(w, http.StatusConflict, "Local operation in progress; retry shortly")
		return
	}
	// Local store access is queued behind a writer/sync commit. The request is
	// never rejected merely because another local operation is in progress.
	s.mu.Lock()
	s.mu.Unlock()
	target := "all"
	if len(p) > 1 {
		target = p[1]
	}
	if p[0] == "conversations" {
		s.mu.Lock()
		err := s.refreshLocalFiles()
		c, e := s.Conversations.GetConversation(p[1])
		s.mu.Unlock()
		if err != nil || e != nil {
			dashboardError(w, 404, "Conversation unavailable")
			return
		}
		target = "conversation:" + c.HHConversationID
	}
	// The existing response is retained for CLI/API clients. Browser requests
	// explicitly opt into background execution using Prefer: respond-async.
	if strings.Contains(r.Header.Get("Prefer"), "respond-async") {
		s.statusMu.Lock()
		if s.asyncTargets == nil {
			s.asyncTargets = map[string]bool{}
		}
		if s.asyncTargets[target] {
			s.statusMu.Unlock()
			dashboardJSON(w, http.StatusAccepted, map[string]any{"running": true, "message": "Sync already in progress"})
			return
		}
		if len(s.asyncTargets) >= 8 {
			s.statusMu.Unlock()
			dashboardError(w, 409, "Too many sync operations in progress")
			return
		}
		s.asyncTargets[target] = true
		s.running.Store(true)
		s.statusMu.Unlock()
		ctx := s.backgroundContext
		if ctx == nil {
			ctx = context.Background()
		}
		go func() {
			defer func() {
				s.statusMu.Lock()
				delete(s.asyncTargets, target)
				s.running.Store(len(s.asyncTargets) > 0)
				s.statusMu.Unlock()
			}()
			s.executeSync(ctx, target)
		}()
		dashboardJSON(w, http.StatusAccepted, map[string]any{"running": true, "target": strings.Split(target, ":")[0]})
		return
	}
	result, err := s.executeSync(r.Context(), target)
	if err != nil {
		dashboardError(w, 500, "HH read-only sync failed")
		return
	}
	dashboardJSON(w, 200, result)
}
func (s *DashboardServer) executeSync(ctx context.Context, target string) (any, error) {
	if target == "inbox" && s.inboxRefresh != nil {
		iteration, err := s.inboxRefresh.Run(ctx, inboxrefresh.Input{Now: time.Now().UTC()})
		s.statusMu.Lock()
		s.syncState = s.Sync.SyncState()
		s.syncResult = iteration.Sync
		s.statusMu.Unlock()
		return iteration.Sync, err
	}
	var result any
	var err error
	if target == "all" {
		result, err = s.Sync.SyncAll(ctx)
	} else {
		result, err = s.Sync.syncRead(ctx, target)
	}
	s.mu.Lock()
	s.decisions = map[string]AIResponseDecision{}
	s.invalidateViews()
	reloadErr := s.refreshLocalFiles()
	if err == nil {
		err = reloadErr
	}
	if err == nil {
		// The network operation is complete. Reclassify locally, refresh
		// follow-up eligibility and notifications in one deterministic pass.
		// No HH request is made by inbox() or refreshDailyOperations().
		inbox, inboxErr := s.inbox()
		if inboxErr != nil {
			err = inboxErr
		} else {
			_, err = s.refreshDailyOperations(time.Now().UTC(), inbox, syncResultForTarget(result))
		}
	}
	s.mu.Unlock()
	s.statusMu.Lock()
	s.syncState = s.Sync.SyncState()
	s.syncResult = result
	s.statusMu.Unlock()
	return result, err
}

func syncResultForTarget(result any) SyncResult {
	switch value := result.(type) {
	case SyncResult:
		return value
	case HHSyncAllResult:
		return value.Conversations
	default:
		return SyncResult{}
	}
}

// Refresh one conversation before the serialized safety checks. Body/header
// validation still occurs in writeAPI; this helper has read-only capability.
func (s *DashboardServer) prepareTargetedPreflight(w http.ResponseWriter, r *http.Request, id string) bool {
	s.mu.Lock()
	if err := s.refreshLocalFiles(); err != nil {
		s.mu.Unlock()
		dashboardError(w, 500, "Cannot reload local data")
		return false
	}
	if s.WriteGateway == nil {
		s.mu.Unlock()
		return true
	}
	action, err := s.WriteGateway.Actions.Get(id)
	if err != nil {
		s.mu.Unlock()
		return true
	}
	if _, terminal := terminalHHWritePreflight(action); terminal {
		s.mu.Unlock()
		return true
	}
	c, err := s.Conversations.GetConversation(action.ConversationID)
	s.mu.Unlock()
	if err != nil {
		dashboardError(w, 404, "Conversation not found")
		return false
	}
	result, err := s.Sync.SyncConversation(c.HHConversationID, r.Context())
	if err != nil || len(result.Errors) > 0 {
		dashboardJSON(w, 409, map[string]any{"status": "BLOCKED", "safety": "BLOCKED", "reasons": append(result.Errors, safeSyncError(err)), "send_request_performed": false})
		return false
	}
	s.mu.Lock()
	s.invalidateViews()
	s.mu.Unlock()
	return true
}

func (s *DashboardServer) syncStatus() map[string]any {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	progress := s.Sync.Progress()
	elapsed := time.Duration(0)
	for _, p := range progress {
		end := p.FinishedAt
		if end.IsZero() {
			end = time.Now()
		}
		elapsed = max(elapsed, end.Sub(p.StartedAt))
	}
	return map[string]any{"state": s.Sync.SyncState(), "running": s.running.Load(), "last_result": s.syncResult, "progress": progress, "elapsed_ms": elapsed.Milliseconds(), "performance": perfSnapshot()}
}
