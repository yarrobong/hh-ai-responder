package careeriteration

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/vacancy"
)

type Service struct {
	deps    Dependencies
	options Options
	state   State
}

func NewService(deps Dependencies, options Options) *Service {
	if options.Interval <= 0 {
		options.Interval = 15 * time.Minute
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps, options: options, state: deps.State}
}

type Result struct {
	RequestedConversationLimit int
	Sync                       SyncAllResult
	Reconciliation             ReconciliationResult
	Snapshot                   Snapshot
	Notifications              NotificationResult
	State                      State
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	if s == nil || s.deps.Inbox == nil || s.deps.Reconciler == nil || s.deps.Notifications == nil || s.deps.Vacancies == nil || s.deps.Applications == nil || s.deps.Conversations == nil || s.deps.Resolver == nil || s.deps.StateStore == nil {
		return Result{}, errors.New("career monitor is not configured")
	}
	if s.options.MaxConversations < 0 {
		return Result{}, errors.New("max conversations must be non-negative")
	}
	now := s.options.Now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.state.LastRunAt = now

	var syncResult SyncResult
	var syncErr error
	if s.options.MaxConversations > 0 {
		bounded, ok := s.deps.Inbox.(BoundedInboxRefresher)
		if !ok {
			return Result{RequestedConversationLimit: s.options.MaxConversations, State: s.state}, errors.New("bounded Career inbox refresh is unavailable")
		}
		syncResult, syncErr = bounded.RefreshInboxBounded(ctx, s.options.MaxConversations)
	} else {
		syncResult, syncErr = s.deps.Inbox.RefreshInbox(ctx)
	}
	allSync := SyncAllResult{Conversations: syncResult}
	baseResult := Result{RequestedConversationLimit: s.options.MaxConversations, Sync: allSync, State: s.state}
	s.state.LastSyncResult = &allSync
	if syncError := firstSyncError(append(append(allSync.Vacancies.Errors, allSync.Applications.Errors...), append(allSync.Conversations.Errors, safeError(syncErr))...)); syncError != "" {
		s.state.ConsecutiveFailures++
		s.state.NextRunAt = now.Add(s.options.Interval)
		if s.state.ConsecutiveFailures >= 3 {
			s.deps.Notifications.RecordSyncProblem(now)
			_ = s.deps.Notifications.Save()
		}
		_ = s.saveState()
		baseResult.State = s.state
		return baseResult, errors.New(syncError)
	}

	reconcileResult, err := s.deps.Reconciler.Reconcile(ctx, false)
	if err != nil {
		s.state.ConsecutiveFailures++
		_ = s.saveState()
		baseResult.State = s.state
		return baseResult, err
	}
	s.state.LastReconcileResult = &reconcileResult
	selectedIDs := conversationScope(allSync.Conversations, s.options.MaxConversations)
	if err := s.resolveStates(ctx, now, selectedIDs); err != nil {
		baseResult.Reconciliation = reconcileResult
		baseResult.State = s.state
		return baseResult, err
	}

	vacancies, _ := s.deps.Vacancies.List(ctx, ports.VacancyQuery{})
	applications, _ := s.deps.Applications.List(ctx)
	conversations, _ := s.deps.Conversations.List(ctx)
	if selectedIDs != nil {
		conversations = scopeConversations(conversations, selectedIDs)
		applications = scopeApplications(applications, conversations, selectedIDs)
		vacancies = scopeVacancies(vacancies, applications, conversations)
	}
	events := []application.Event{}
	for _, app := range applications {
		if timeline, timelineErr := s.deps.Applications.Timeline(ctx, app.ID); timelineErr == nil {
			events = append(events, timeline...)
		}
	}
	snapshot := Snapshot{Vacancies: vacancies, Applications: applications, Events: events, Conversations: conversations, Consistency: map[string][]string{}}
	if s.deps.Clarifications != nil {
		snapshot.Clarifications, _ = s.deps.Clarifications.List()
		if selectedIDs != nil {
			snapshot.Clarifications = scopeClarifications(snapshot.Clarifications, applications, conversations)
		}
	}
	if s.deps.Auditor != nil {
		s.state.LastAuditResult = s.deps.Auditor.Audit(snapshot, now)
	}

	var notificationResult NotificationResult
	if !quietHoursActive(s.options.QuietHours, now) {
		notificationResult, err = s.deps.Notifications.Calculate(snapshot, now)
		if err != nil {
			baseResult.Reconciliation, baseResult.Snapshot = reconcileResult, snapshot
			baseResult.State = s.state
			return baseResult, err
		}
		if err := s.deps.Notifications.Save(); err != nil {
			baseResult.Reconciliation, baseResult.Snapshot, baseResult.Notifications = reconcileResult, snapshot, notificationResult
			baseResult.State = s.state
			return baseResult, err
		}
	}

	s.state.LastSuccessAt, s.state.LastRunAt = now, now
	s.state.ConsecutiveFailures = 0
	s.state.NextRunAt = now.Add(s.options.Interval)
	result := Result{RequestedConversationLimit: s.options.MaxConversations, Sync: allSync, Reconciliation: reconcileResult, Snapshot: snapshot, Notifications: notificationResult, State: s.state}
	if err := s.saveState(); err != nil {
		return result, err
	}
	result.State = s.state
	return result, nil
}

func (s *Service) saveState() error {
	return s.deps.StateStore.Save(s.state)
}

func (s *Service) resolveStates(ctx context.Context, now time.Time, selectedIDs map[string]struct{}) error {
	conversations, err := s.deps.Conversations.List(ctx)
	if err != nil {
		return err
	}
	if selectedIDs != nil {
		conversations = scopeConversations(conversations, selectedIDs)
	}
	applications, err := s.deps.Applications.List(ctx)
	if err != nil {
		return err
	}
	for _, c := range conversations {
		var app application.JobApplication
		for _, item := range applications {
			if item.ConversationID == c.ID {
				app = item
				break
			}
		}
		pending := false
		if s.deps.Clarifications != nil {
			clarifications, _ := s.deps.Clarifications.List()
			for _, q := range clarifications {
				if q.Status == candidateacquisition.ClarificationPending && (q.ConversationID == c.ID || q.ApplicationID == app.ID) {
					pending = true
				}
			}
		}
		var events []application.Event
		if app.ID != "" {
			events, _ = s.deps.Applications.Timeline(ctx, app.ID)
		}
		var appliedAt *time.Time
		if s.deps.ApplicationTime != nil {
			appliedAt = s.deps.ApplicationTime(app, events)
		}
		state := s.deps.Resolver.Resolve(app, c, appliedAt, pending, nil, now)
		if state.Status != c.Status || !sameTime(state.WaitingSince, c.WaitingSince) {
			if err := s.deps.Conversations.UpdateState(ctx, c.ID, conversation.State{Status: state.Status, NextAction: c.NextAction, WaitingSince: state.WaitingSince, FollowUpState: c.FollowUpState}); err != nil {
				return err
			}
		}
	}
	return s.deps.Conversations.Save(ctx)
}

func conversationScope(sync SyncResult, maxConversations int) map[string]struct{} {
	if maxConversations <= 0 {
		return nil
	}
	selected := make(map[string]struct{}, len(sync.SelectedConversationIDs))
	for _, id := range sync.SelectedConversationIDs {
		if strings.TrimSpace(id) != "" {
			selected[id] = struct{}{}
		}
	}
	return selected
}

func scopeConversations(values []conversation.EmployerConversation, selected map[string]struct{}) []conversation.EmployerConversation {
	result := make([]conversation.EmployerConversation, 0, len(selected))
	for _, value := range values {
		if _, ok := selected[value.ID]; ok {
			result = append(result, value)
			continue
		}
		if value.HHConversationID != "" {
			if _, ok := selected[value.HHConversationID]; ok {
				result = append(result, value)
			}
		}
	}
	return result
}

func scopeApplications(values []application.JobApplication, conversations []conversation.EmployerConversation, selected map[string]struct{}) []application.JobApplication {
	conversationIDs := make(map[string]struct{}, len(conversations)*2)
	for _, value := range conversations {
		conversationIDs[value.ID] = struct{}{}
		if value.HHConversationID != "" {
			conversationIDs[value.HHConversationID] = struct{}{}
		}
	}
	result := make([]application.JobApplication, 0, len(conversations))
	for _, value := range values {
		if _, ok := conversationIDs[value.ConversationID]; ok {
			result = append(result, value)
			continue
		}
		if _, ok := selected[value.HHMetadata["conversation_external_id"]]; ok {
			result = append(result, value)
		}
	}
	return result
}

func scopeVacancies(values []vacancy.Vacancy, applications []application.JobApplication, conversations []conversation.EmployerConversation) []vacancy.Vacancy {
	ids := map[int]struct{}{}
	for _, value := range applications {
		if value.VacancyID > 0 {
			ids[value.VacancyID] = struct{}{}
		}
	}
	for _, value := range conversations {
		if value.VacancyID > 0 {
			ids[value.VacancyID] = struct{}{}
		}
	}
	result := make([]vacancy.Vacancy, 0, len(ids))
	for _, value := range values {
		if _, ok := ids[value.ID]; ok {
			result = append(result, value)
		}
	}
	return result
}

func scopeClarifications(values []candidateacquisition.CandidateClarificationRequest, applications []application.JobApplication, conversations []conversation.EmployerConversation) []candidateacquisition.CandidateClarificationRequest {
	applicationIDs := make(map[string]struct{}, len(applications))
	conversationIDs := make(map[string]struct{}, len(conversations))
	for _, value := range applications {
		applicationIDs[value.ID] = struct{}{}
	}
	for _, value := range conversations {
		conversationIDs[value.ID] = struct{}{}
		if value.HHConversationID != "" {
			conversationIDs[value.HHConversationID] = struct{}{}
		}
	}
	result := make([]candidateacquisition.CandidateClarificationRequest, 0, len(values))
	for _, value := range values {
		_, conversationMatch := conversationIDs[value.ConversationID]
		_, applicationMatch := applicationIDs[value.ApplicationID]
		if conversationMatch || applicationMatch {
			result = append(result, value)
		}
	}
	return result
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
}

func firstSyncError(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func quietHoursActive(spec string, now time.Time) bool {
	parts := strings.Split(strings.TrimSpace(spec), "-")
	if len(parts) != 2 {
		return false
	}
	parse := func(value string) (int, bool) {
		parsed, err := time.Parse("15:04", strings.TrimSpace(value))
		if err != nil {
			return 0, false
		}
		return parsed.Hour()*60 + parsed.Minute(), true
	}
	start, ok := parse(parts[0])
	if !ok {
		return false
	}
	end, ok := parse(parts[1])
	if !ok {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if start == end {
		return true
	}
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
