package runtime

import (
	"context"
	"encoding/json"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/careeriteration"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

// careerInboxRefresher is the root compatibility adapter for the established
// HHReadSyncService façade. The extracted use case sees only RefreshInbox.
type careerInboxRefresher struct {
	service *HHReadSyncService
}

func (r careerInboxRefresher) RefreshInbox(ctx context.Context) (careeriteration.SyncResult, error) {
	value, err := r.service.RefreshInbox(ctx)
	return careerSyncResult(value), err
}

func (r careerInboxRefresher) RefreshInboxBounded(ctx context.Context, maxConversations int) (careeriteration.SyncResult, error) {
	value, err := r.service.RefreshInboxBounded(ctx, maxConversations)
	return careerSyncResult(value), err
}

func careerSyncResult(value SyncResult) careeriteration.SyncResult {
	return careeriteration.SyncResult{
		Performance: careeriteration.OperationPerformance{
			Duration: value.Performance.Duration, NetworkDuration: value.Performance.NetworkDuration,
			RateWait: value.Performance.RateWait, DiskDuration: value.Performance.DiskDuration,
			ComputeDuration: value.Performance.ComputeDuration, Requests: value.Performance.Requests,
			CacheHits: value.Performance.CacheHits, CacheMisses: value.Performance.CacheMisses,
		},
		Fetched: value.Fetched, Created: value.Created, Updated: value.Updated, Unchanged: value.Unchanged,
		Skipped: value.Skipped, MetadataChecked: value.MetadataChecked, HistoryReused: value.HistoryReused,
		DetailedChatsFetched: value.DetailedChatsFetched, SelectedConversationIDs: append([]string{}, value.SelectedConversationIDs...), ChangedConversationIDs: append([]string{}, value.ChangedConversationIDs...),
		Errors: append([]string{}, value.Errors...), Warnings: append([]string{}, value.Warnings...),
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
	}
}

type careerReconciler struct {
	reconciler *CareerDataReconciler
}

func (r careerReconciler) Reconcile(ctx context.Context, dryRun bool) (careeriteration.ReconciliationResult, error) {
	value, err := r.reconciler.ReconcileContext(ctx, dryRun)
	result := careeriteration.ReconciliationResult{
		DryRun: value.DryRun, LinksFound: value.LinksFound, ApplicationsCreated: value.ApplicationsCreated,
		VacanciesEnriched: value.VacanciesEnriched, MatchesBackfilled: value.MatchesBackfilled,
		EventsCreated: value.EventsCreated, InsufficientMatchData: append([]string{}, value.InsufficientMatchData...),
	}
	result.UnresolvedRelations = make([]careeriteration.ReconcileIssue, 0, len(value.UnresolvedRelations))
	for _, issue := range value.UnresolvedRelations {
		result.UnresolvedRelations = append(result.UnresolvedRelations, careeriteration.ReconcileIssue{Relation: issue.Relation, RecordID: issue.RecordID, Reason: issue.Reason})
	}
	return result, err
}

type careerStateResolver struct{}

func (careerStateResolver) Resolve(a application.JobApplication, c conversation.EmployerConversation, appliedAt *time.Time, pending bool, warnings []string, now time.Time) conversationpolicy.Resolution {
	return (ConversationStateResolver{}).Resolve(a, c, appliedAt, pending, warnings, now)
}

type careerApplicationReader struct {
	store *ApplicationStore
}

func (r careerApplicationReader) List(ctx context.Context) ([]application.JobApplication, error) {
	if err := careerContextError(ctx); err != nil {
		return nil, err
	}
	return r.store.ListApplications()
}

func (r careerApplicationReader) Timeline(ctx context.Context, id string) ([]application.Event, error) {
	if err := careerContextError(ctx); err != nil {
		return nil, err
	}
	return r.store.GetApplicationTimeline(id)
}

type careerConversationStore struct {
	store *ConversationStore
}

func (r careerConversationStore) List(ctx context.Context) ([]conversation.EmployerConversation, error) {
	if err := careerContextError(ctx); err != nil {
		return nil, err
	}
	return r.store.ListConversations()
}

func (r careerConversationStore) UpdateState(ctx context.Context, id string, state conversation.State) error {
	if err := careerContextError(ctx); err != nil {
		return err
	}
	return r.store.UpdateConversationState(id, state)
}

func (r careerConversationStore) Save(ctx context.Context) error {
	if err := careerContextError(ctx); err != nil {
		return err
	}
	return r.store.Save()
}

func careerContextError(ctx context.Context) error {
	if ctx != nil {
		return ctx.Err()
	}
	return nil
}

type careerNotificationProcessor struct {
	engine *CandidateNotificationEngine
}

func (n careerNotificationProcessor) Calculate(snapshot careeriteration.Snapshot, now time.Time) (careeriteration.NotificationResult, error) {
	_, err := n.engine.Calculate(CareerSnapshot{
		Vacancies: snapshot.Vacancies, Applications: snapshot.Applications, Conversations: snapshot.Conversations,
		Events: snapshot.Events, Clarifications: snapshot.Clarifications, Consistency: snapshot.Consistency,
	}, now)
	return careeriteration.NotificationResult{Created: n.engine.LastCreated, Resolved: n.engine.LastResolved, Deduplicated: n.engine.LastDeduplicated}, err
}

func (n careerNotificationProcessor) RecordSyncProblem(now time.Time) {
	n.engine.add(NotificationSyncProblem, "", "", "HH read-only sync недоступна; проверьте подключение и состояние локальных данных.", "sync-problem", now)
}

func (n careerNotificationProcessor) Save() error {
	return n.engine.Store.Save()
}

type careerAuditor struct {
	policy FollowUpPolicy
}

func (a careerAuditor) Audit(snapshot careeriteration.Snapshot, now time.Time) json.RawMessage {
	report := BuildCareerAuditReport(CareerSnapshot{
		Vacancies: snapshot.Vacancies, Applications: snapshot.Applications, Conversations: snapshot.Conversations,
		Events: snapshot.Events, Clarifications: snapshot.Clarifications, Consistency: snapshot.Consistency,
	}, a.policy, now)
	raw, err := json.Marshal(report)
	if err != nil {
		return nil
	}
	return json.RawMessage(raw)
}

type careerStateStore struct {
	monitor *CareerMonitor
}

func (s careerStateStore) Save(state careeriteration.State) error {
	s.monitor.applyCareerIterationState(state)
	return s.monitor.saveState()
}

func (m *CareerMonitor) applyCareerIterationState(state careeriteration.State) {
	m.State.LastRunAt = state.LastRunAt
	m.State.LastSuccessAt = state.LastSuccessAt
	m.State.ConsecutiveFailures = state.ConsecutiveFailures
	m.State.NextRunAt = state.NextRunAt
	if state.LastSyncResult != nil {
		m.State.LastSyncResult = *state.LastSyncResult
	}
	if state.LastReconcileResult != nil {
		m.State.LastReconcileResult = *state.LastReconcileResult
	}
	if len(state.LastAuditResult) > 0 {
		var value any
		if err := json.Unmarshal(state.LastAuditResult, &value); err == nil {
			m.State.LastAuditResult = value
		}
	}
}

func (m *CareerMonitor) iterationService(now time.Time, maxConversations int) *careeriteration.Service {
	deps := careeriteration.Dependencies{
		State:      careeriteration.State{LastRunAt: m.State.LastRunAt, LastSuccessAt: m.State.LastSuccessAt, ConsecutiveFailures: m.State.ConsecutiveFailures, NextRunAt: m.State.NextRunAt},
		StateStore: careerStateStore{monitor: m},
		Resolver:   careerStateResolver{},
		ApplicationTime: func(a application.JobApplication, events []application.Event) *time.Time {
			return knownApplicationTime(a, events)
		},
	}
	if m.Sync != nil {
		deps.Inbox = careerInboxRefresher{service: m.Sync}
	}
	if m.Reconciler != nil {
		deps.Reconciler = careerReconciler{reconciler: m.Reconciler}
		deps.Vacancies = m.Reconciler.Vacancies
	}
	if m.Applications != nil {
		deps.Applications = careerApplicationReader{store: m.Applications}
	}
	if m.Conversations != nil {
		deps.Conversations = careerConversationStore{store: m.Conversations}
	}
	if m.Notifications != nil {
		deps.Notifications = careerNotificationProcessor{engine: m.Notifications}
		deps.Auditor = careerAuditor{policy: m.Notifications.Policy}
		if m.Notifications.Clarifications != nil {
			deps.Clarifications = m.Notifications.Clarifications
		}
	}
	return careeriteration.NewService(deps, careeriteration.Options{Interval: m.Interval, QuietHours: m.QuietHours, MaxConversations: maxConversations, Now: func() time.Time { return now }})
}
