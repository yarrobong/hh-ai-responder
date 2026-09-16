package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

type inboxDraftJob struct {
	target            string
	conversationID    string
	conversation      EmployerConversation
	resolver          *CandidateContextResolver
	semanticRetriever CandidateSemanticRetriever
	conversationToken string
	knowledgeToken    string
}

// snapshotInboxDraft captures only detached inputs while DashboardServer.mu
// is held. Context assembly and semantic retrieval happen later against these
// copies, so neither local store reads nor external embedding work extend the
// shared dashboard critical section.
func (s *DashboardServer) snapshotInboxDraft(item CandidateInboxItem) (inboxDraftJob, error) {
	conversation, err := s.Conversations.GetConversation(item.Conversation.ID)
	if err != nil {
		return inboxDraftJob{}, err
	}
	resolver, err := cloneCandidateContextResolver(s.Resolver)
	if err != nil {
		return inboxDraftJob{}, err
	}
	retriever := s.Orchestrator.semanticRetriever
	if retriever == nil && s.Orchestrator.conversationBuilder != nil {
		retriever = s.Orchestrator.conversationBuilder.semanticRetriever
	}
	return inboxDraftJob{
		target:            "draft:" + conversation.ID,
		conversationID:    conversation.ID,
		conversation:      conversation,
		resolver:          resolver,
		semanticRetriever: retriever,
		conversationToken: draftConversationToken(conversation),
		knowledgeToken:    draftResolverToken(s.Resolver),
	}, nil
}

func cloneCandidateContextResolver(value *CandidateContextResolver) (*CandidateContextResolver, error) {
	if value == nil {
		return nil, errors.New("candidate resolver is not configured")
	}
	if value.candidate != nil {
		candidate, err := cloneKnowledge(*value.candidate)
		if err != nil {
			return nil, err
		}
		return NewCandidateContextResolverFromCandidate(candidate), nil
	}
	if value.kb != nil {
		kb, err := cloneKnowledge(*value.kb)
		if err != nil {
			return nil, err
		}
		return NewCandidateContextResolver(&kb), nil
	}
	return NewCandidateContextResolver(nil), nil
}

func draftConversationToken(value EmployerConversation) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func draftResolverToken(value *CandidateContextResolver) string {
	if value == nil {
		return ""
	}
	var raw []byte
	if value.candidate != nil {
		raw, _ = json.Marshal(*value.candidate)
	} else if value.kb != nil {
		raw, _ = json.Marshal(*value.kb)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func latestDraftForFingerprint(values []AIDraft, conversationID, fingerprint string) bool {
	for _, draft := range values {
		if draft.Type == AIDraftEmployerReply && draft.Status == AIDraftGenerated && draft.ConversationID == conversationID && draft.InputFingerprint == fingerprint {
			return true
		}
	}
	return false
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
		job, err := s.snapshotInboxDraft(item)
		if err != nil {
			s.statusMu.Lock()
			delete(s.draftTargets, target)
			s.statusMu.Unlock()
			continue
		}
		go s.generateInboxDraft(job)
	}
}

// scheduleInboxDraftsDeferred preserves Overview's existing draft scheduling
// behavior without making the HTTP read wait for complete-history snapshots.
// The snapshot still takes DashboardServer.mu before reading local state, and
// generateInboxDraft keeps the existing freshness and deduplication checks.
func (s *DashboardServer) scheduleInboxDraftsDeferred(items []CandidateInboxItem) {
	if s == nil || s.backgroundContext == nil || s.Orchestrator == nil {
		return
	}
	for _, item := range items {
		if item.Workflow.State != WorkflowNeedsReply || item.LatestMessage == nil || item.LatestMessage.Sender != ConversationSenderEmployer {
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
		go func(item CandidateInboxItem, target string) {
			waitStart := time.Now()
			s.mu.Lock()
			perfRecord("dashboard.mutex_wait", waitStart, 1)
			perfRecord("dashboard.inbox_draft.mutex_wait", waitStart, 1)
			holdStart := time.Now()
			job, err := s.snapshotInboxDraft(item)
			perfRecord("dashboard.mutex_hold", holdStart, 1)
			perfRecord("dashboard.inbox_draft.mutex_hold", holdStart, 1)
			s.mu.Unlock()
			if err != nil {
				s.statusMu.Lock()
				delete(s.draftTargets, target)
				s.statusMu.Unlock()
				return
			}
			go s.generateInboxDraft(job)
		}(item, target)
	}
}

func (s *DashboardServer) generateInboxDraft(job inboxDraftJob) {
	workerStart := time.Now()
	s.draftWorkersStarted.Add(1)
	active := s.draftWorkersActive.Add(1)
	for {
		max := s.draftWorkersMaxActive.Load()
		if active <= max || s.draftWorkersMaxActive.CompareAndSwap(max, active) {
			break
		}
	}
	defer func() {
		s.statusMu.Lock()
		delete(s.draftTargets, job.target)
		s.statusMu.Unlock()
		s.draftWorkersActive.Add(-1)
		perfRecord("dashboard.inbox_draft.total", workerStart, 1)
	}()
	select {
	case <-s.backgroundContext.Done():
		return
	default:
	}
	// Keep the former effective one-worker bound without serializing dashboard
	// reads. The expensive preparation and AI call may wait on this narrow
	// worker lock, but DashboardServer.mu remains available to HTTP handlers.
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	// Build the complete AI input from detached conversation and resolver
	// snapshots. This phase may perform local computation and semantic
	// retrieval; DashboardServer.mu is intentionally not held. The AI call
	// below uses the same detached context.
	builder := NewConversationContextBuilder(nil, job.resolver, job.semanticRetriever)
	preparationStart := time.Now()
	value, err := builder.BuildForReplySnapshot(job.conversation, job.conversation.Messages)
	if err != nil {
		return
	}
	messageHash, knowledgeHash, cacheKey := employerDraftMetadata(job.resolver, value)
	perfRecord("dashboard.inbox_draft.prepare", preparationStart, 1)
	if err := s.backgroundContext.Err(); err != nil {
		return
	}
	// A fresh draft may have been committed by another local path while this
	// detached preparation was running. Check that case before invoking AI.
	if !s.inboxDraftCommitStillCurrent(job, cacheKey) {
		return
	}
	decision, err := s.Orchestrator.PrepareEmployerReplyFromContext(s.backgroundContext, value)
	if err != nil {
		return
	}
	_ = s.commitInboxDraft(job, value, messageHash, knowledgeHash, cacheKey, decision)
}

func (s *DashboardServer) lockInboxDraft() func() {
	waitStart := time.Now()
	s.mu.Lock()
	perfRecord("dashboard.mutex_wait", waitStart, 1)
	perfRecord("dashboard.inbox_draft.mutex_wait", waitStart, 1)
	holdStart := time.Now()
	return func() {
		perfRecord("dashboard.mutex_hold", holdStart, 1)
		perfRecord("dashboard.inbox_draft.mutex_hold", holdStart, 1)
		s.mu.Unlock()
	}
}

func (s *DashboardServer) inboxDraftCommitStillCurrent(job inboxDraftJob, cacheKey string) bool {
	unlock := s.lockInboxDraft()
	defer unlock()
	if err := s.refreshLocalFiles(); err != nil || draftResolverToken(s.Resolver) != job.knowledgeToken {
		return false
	}
	conversation, err := s.Conversations.GetConversation(job.conversationID)
	if err != nil || draftConversationToken(conversation) != job.conversationToken {
		return false
	}
	drafts, err := s.Drafts.List()
	return err == nil && !latestDraftForFingerprint(drafts, job.conversationID, cacheKey)
}

func (s *DashboardServer) commitInboxDraft(job inboxDraftJob, value ConversationContext, messageHash, knowledgeHash, cacheKey string, decision AIResponseDecision) error {
	unlock := s.lockInboxDraft()
	defer unlock()
	if err := s.refreshLocalFiles(); err != nil {
		return err
	}
	if draftResolverToken(s.Resolver) != job.knowledgeToken {
		return nil
	}
	conversation, err := s.Conversations.GetConversation(job.conversationID)
	if err != nil || draftConversationToken(conversation) != job.conversationToken {
		return nil
	}
	drafts, err := s.Drafts.List()
	if err != nil {
		return err
	}
	if latestDraftForFingerprint(drafts, job.conversationID, cacheKey) {
		return nil
	}
	if err := s.Orchestrator.PersistPreparedEmployerReply(s.backgroundContext, value, messageHash, knowledgeHash, cacheKey, decision); err != nil {
		return err
	}
	if err := s.Drafts.Save(); err != nil {
		return err
	}
	if err := s.Clarifications.Save(); err != nil {
		return err
	}
	s.decisions[job.conversationID] = decision
	s.invalidateViews()
	return nil
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
