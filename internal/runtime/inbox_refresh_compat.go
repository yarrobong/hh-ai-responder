package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"hh-ai-responder/internal/usecase/inboxrefresh"
)

// dashboardInboxSyncer adapts the existing R9 HH read façade to the narrow
// capability consumed by inboxrefresh. It has no write methods.
type dashboardInboxSyncer struct {
	service *HHReadSyncService
}

func (s dashboardInboxSyncer) RefreshInbox(ctx context.Context) (inboxrefresh.SyncResult, error) {
	if s.service == nil {
		return inboxrefresh.SyncResult{}, errors.New("inbox sync is not configured")
	}
	value, err := s.service.RefreshInbox(ctx)
	return inboxSyncResult(value), err
}

func inboxSyncResult(value SyncResult) inboxrefresh.SyncResult {
	return inboxrefresh.SyncResult{
		Performance: inboxrefresh.OperationPerformance{
			Duration: value.Performance.Duration, NetworkDuration: value.Performance.NetworkDuration,
			RateWait: value.Performance.RateWait, DiskDuration: value.Performance.DiskDuration,
			ComputeDuration: value.Performance.ComputeDuration, Requests: value.Performance.Requests,
			CacheHits: value.Performance.CacheHits, CacheMisses: value.Performance.CacheMisses,
		},
		Fetched: value.Fetched, Created: value.Created, Updated: value.Updated, Unchanged: value.Unchanged,
		Skipped: value.Skipped, MetadataChecked: value.MetadataChecked, HistoryReused: value.HistoryReused,
		DetailedChatsFetched: value.DetailedChatsFetched, ChangedConversationIDs: append([]string{}, value.ChangedConversationIDs...),
		Errors: append([]string{}, value.Errors...), Warnings: append([]string{}, value.Warnings...),
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
	}
}

type dashboardInboxProjection struct {
	server *DashboardServer
	mu     sync.Mutex
	inbox  CandidateInbox
}

func (p *dashboardInboxProjection) Reload(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	p.server.mu.Lock()
	defer p.server.mu.Unlock()
	p.server.decisions = map[string]AIResponseDecision{}
	p.server.invalidateViews()
	return p.server.refreshLocalFiles()
}

func (p *dashboardInboxProjection) Build(ctx context.Context, now time.Time) (inboxrefresh.Projection, error) {
	if err := contextErr(ctx); err != nil {
		return inboxrefresh.Projection{}, err
	}
	p.server.mu.Lock()
	inbox, err := p.server.inbox()
	p.server.mu.Unlock()
	if err != nil {
		return inboxrefresh.Projection{}, err
	}
	p.mu.Lock()
	p.inbox = inbox
	p.mu.Unlock()
	return inboxrefresh.Projection{
		ConversationCount: len(inbox.Items),
		WorkflowItems:     len(inbox.Items),
		FollowUpsEligible: len(inbox.FollowUps),
	}, nil
}

func (p *dashboardInboxProjection) currentInbox() (CandidateInbox, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inbox.Items == nil {
		return CandidateInbox{}, false
	}
	return p.inbox, true
}

type dashboardInboxNotifications struct {
	server *DashboardServer
}

func (n dashboardInboxNotifications) Refresh(ctx context.Context, now time.Time) (inboxrefresh.NotificationResult, error) {
	if err := contextErr(ctx); err != nil {
		return inboxrefresh.NotificationResult{}, err
	}
	n.server.mu.Lock()
	defer n.server.mu.Unlock()
	if err := n.server.refreshNotifications(now); err != nil {
		return inboxrefresh.NotificationResult{}, err
	}
	return inboxrefresh.NotificationResult{Created: n.server.lastNotificationStats.Created, Resolved: n.server.lastNotificationStats.Resolved, Deduplicated: n.server.lastNotificationStats.Deduplicated}, nil
}

type dashboardInboxDailyState struct {
	server     *DashboardServer
	projection *dashboardInboxProjection
}

func (d dashboardInboxDailyState) Save(ctx context.Context, input inboxrefresh.DailyStateInput) (inboxrefresh.DailyStateResult, error) {
	if err := contextErr(ctx); err != nil {
		return inboxrefresh.DailyStateResult{}, err
	}
	inbox, ok := d.projection.currentInbox()
	if !ok {
		return inboxrefresh.DailyStateResult{}, errors.New("inbox projection is not available")
	}
	d.server.mu.Lock()
	defer d.server.mu.Unlock()
	if _, err := d.server.persistDailyOperations(input.Now, inbox, rootSyncResult(input.Sync)); err != nil {
		return inboxrefresh.DailyStateResult{}, err
	}
	return inboxrefresh.DailyStateResult{Persisted: true}, nil
}

func rootSyncResult(value inboxrefresh.SyncResult) SyncResult {
	return SyncResult{
		Performance: OperationPerformance{
			Duration: value.Performance.Duration, NetworkDuration: value.Performance.NetworkDuration,
			RateWait: value.Performance.RateWait, DiskDuration: value.Performance.DiskDuration,
			ComputeDuration: value.Performance.ComputeDuration, Requests: value.Performance.Requests,
			CacheHits: value.Performance.CacheHits, CacheMisses: value.Performance.CacheMisses,
		},
		Fetched: value.Fetched, Created: value.Created, Updated: value.Updated, Unchanged: value.Unchanged,
		Skipped: value.Skipped, MetadataChecked: value.MetadataChecked, HistoryReused: value.HistoryReused,
		DetailedChatsFetched: value.DetailedChatsFetched, ChangedConversationIDs: append([]string{}, value.ChangedConversationIDs...),
		Errors: append([]string{}, value.Errors...), Warnings: append([]string{}, value.Warnings...),
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
	}
}

func (s *DashboardServer) newInboxRefreshService() *inboxrefresh.Service {
	projection := &dashboardInboxProjection{server: s}
	return inboxrefresh.NewService(inboxrefresh.Dependencies{
		Inbox:         dashboardInboxSyncer{service: s.Sync},
		Projection:    projection,
		Notifications: dashboardInboxNotifications{server: s},
		DailyState:    dashboardInboxDailyState{server: s, projection: projection},
	}, inboxrefresh.Options{Now: func() time.Time { return time.Now().UTC() }})
}
