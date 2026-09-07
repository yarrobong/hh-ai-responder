package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	NotificationEventsFilename = "notification_events.json"
	CareerMonitorStateFilename = "career_monitor_state.json"
)

type CandidateNotificationType string

const (
	NotificationNewEmployerMessage      CandidateNotificationType = "new_employer_message"
	NotificationCandidateActionRequired CandidateNotificationType = "candidate_action_required"
	NotificationClarificationRequired   CandidateNotificationType = "clarification_required"
	NotificationManualReviewRequired    CandidateNotificationType = "manual_review_required"
	NotificationFollowUpAvailable       CandidateNotificationType = "follow_up_available"
	NotificationInterviewDetected       CandidateNotificationType = "interview_detected"
	NotificationExternalActionRequired  CandidateNotificationType = "external_action_required"
	NotificationDeliveryUncertain       CandidateNotificationType = "delivery_uncertain"
	NotificationOfferDetected           CandidateNotificationType = "offer_detected"
	NotificationSyncProblem             CandidateNotificationType = "sync_problem"
	NotificationStatusChanged           CandidateNotificationType = "status_changed"
)

// NotificationLifecycle is deliberately local state. It never authorizes an
// HH write and survives a process restart through notification_events.json.
type NotificationLifecycle string

const (
	NotificationNew       NotificationLifecycle = "new"
	NotificationSeen      NotificationLifecycle = "seen"
	NotificationDismissed NotificationLifecycle = "dismissed"
	NotificationSnoozed   NotificationLifecycle = "snoozed"
	NotificationResolved  NotificationLifecycle = "resolved"
)

type NotificationPriority string

const (
	NotificationPriorityCritical NotificationPriority = "CRITICAL"
	NotificationPriorityHigh     NotificationPriority = "HIGH"
	NotificationPriorityMedium   NotificationPriority = "MEDIUM"
	NotificationPriorityLow      NotificationPriority = "LOW"
)

type CandidateNotification struct {
	ID                    string                    `json:"id"`
	Type                  CandidateNotificationType `json:"type"`
	RelatedApplicationID  string                    `json:"related_application_id,omitempty"`
	RelatedConversationID string                    `json:"related_conversation_id,omitempty"`
	Fingerprint           string                    `json:"fingerprint"`
	Message               string                    `json:"message"`
	CreatedAt             time.Time                 `json:"created_at"`
	Priority              NotificationPriority      `json:"priority"`
	Lifecycle             NotificationLifecycle     `json:"lifecycle"`
	SeenAt                *time.Time                `json:"seen_at,omitempty"`
	AcknowledgedAt        *time.Time                `json:"acknowledged_at,omitempty"`
	SnoozedUntil          *time.Time                `json:"snoozed_until,omitempty"`
	ResolvedAt            *time.Time                `json:"resolved_at,omitempty"`
}

type notificationStoreFile struct {
	Version        int                     `json:"version"`
	Notifications  []CandidateNotification `json:"notifications"`
	WorkflowStates map[string]string       `json:"workflow_states,omitempty"`
}

type NotificationStore struct {
	path           string
	notifications  []CandidateNotification
	workflowStates map[string]string
}

func NewNotificationStore(paths ...string) *NotificationStore {
	path := NotificationEventsFilename
	if len(paths) > 0 && strings.TrimSpace(paths[0]) != "" {
		path = paths[0]
	}
	return &NotificationStore{path: path, notifications: []CandidateNotification{}, workflowStates: map[string]string{}}
}

func (s *NotificationStore) Load() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("notification store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.notifications = []CandidateNotification{}
		return nil
	}
	if err != nil {
		return err
	}
	var file notificationStoreFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || file.Version != 1 || file.Notifications == nil {
		return errors.New("invalid notification store")
	}
	for _, n := range file.Notifications {
		if strings.TrimSpace(n.ID) == "" || n.Type == "" || strings.TrimSpace(n.Fingerprint) == "" || n.CreatedAt.IsZero() {
			return errors.New("invalid notification")
		}
	}
	s.notifications = file.Notifications
	s.workflowStates = file.WorkflowStates
	if s.workflowStates == nil {
		s.workflowStates = map[string]string{}
	}
	// Stage 23 notification files predate explicit lifecycle and priority.
	// Normalize them instead of invalidating a useful local history.
	for i := range s.notifications {
		n := &s.notifications[i]
		if n.Lifecycle == "" {
			switch {
			case n.AcknowledgedAt != nil:
				n.Lifecycle = NotificationDismissed
			case n.SnoozedUntil != nil:
				n.Lifecycle = NotificationSnoozed
			default:
				n.Lifecycle = NotificationNew
			}
		}
		if n.Priority == "" {
			n.Priority = notificationPriority(n.Type, n.Fingerprint)
		}
	}
	// A crash or an older pre-fingerprint implementation may have left
	// duplicate records behind. Collapse them on load so a restart cannot
	// surface notification spam; the more advanced lifecycle wins.
	unique := make([]CandidateNotification, 0, len(s.notifications))
	positions := map[string]int{}
	for _, notification := range s.notifications {
		if at, exists := positions[notification.Fingerprint]; exists {
			if notificationLifecycleRank(notification.Lifecycle) > notificationLifecycleRank(unique[at].Lifecycle) {
				unique[at] = notification
			}
			continue
		}
		positions[notification.Fingerprint] = len(unique)
		unique = append(unique, notification)
	}
	s.notifications = unique
	return nil
}

func notificationLifecycleRank(value NotificationLifecycle) int {
	switch value {
	case NotificationResolved:
		return 5
	case NotificationDismissed:
		return 4
	case NotificationSnoozed:
		return 3
	case NotificationSeen:
		return 2
	default:
		return 1
	}
}

func (s *NotificationStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("notification store requires a path")
	}
	raw, err := json.MarshalIndent(notificationStoreFile{Version: 1, Notifications: s.notifications, WorkflowStates: s.workflowStates}, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateStoreWrite(s.path, raw, ".notifications-*.tmp")
}

func (s *NotificationStore) List() []CandidateNotification {
	if s == nil {
		return []CandidateNotification{}
	}
	return append([]CandidateNotification{}, s.notifications...)
}

func (s *NotificationStore) FindFingerprint(fingerprint string) (CandidateNotification, bool) {
	for _, n := range s.notifications {
		if n.Fingerprint == fingerprint {
			return n, true
		}
	}
	return CandidateNotification{}, false
}

func (s *NotificationStore) Acknowledge(id string, snooze *time.Time) error {
	if snooze != nil {
		return s.Snooze(id, *snooze)
	}
	return s.Dismiss(id)
}

func (s *NotificationStore) transition(id string, lifecycle NotificationLifecycle, until *time.Time) error {
	for i := range s.notifications {
		if s.notifications[i].ID != id {
			continue
		}
		now := time.Now().UTC()
		n := &s.notifications[i]
		n.Lifecycle = lifecycle
		switch lifecycle {
		case NotificationSeen:
			n.SeenAt = &now
		case NotificationDismissed:
			n.AcknowledgedAt = &now
		case NotificationSnoozed:
			n.SnoozedUntil = until
		case NotificationResolved:
			n.ResolvedAt = &now
		}
		return s.Save()
	}
	return errors.New("notification not found")
}

func (s *NotificationStore) MarkSeen(id string) error { return s.transition(id, NotificationSeen, nil) }
func (s *NotificationStore) Dismiss(id string) error {
	return s.transition(id, NotificationDismissed, nil)
}
func (s *NotificationStore) Resolve(id string) error {
	return s.transition(id, NotificationResolved, nil)
}
func (s *NotificationStore) Snooze(id string, until time.Time) error {
	if !until.After(time.Now().UTC()) {
		return errors.New("snooze time must be in the future")
	}
	return s.transition(id, NotificationSnoozed, &until)
}

type CandidateNotificationEngine struct {
	Store            *NotificationStore
	Applications     *ApplicationStore
	Conversations    *ConversationStore
	Clarifications   *CandidateClarificationStore
	Policy           FollowUpPolicy
	Cooldown         time.Duration
	LastCreated      int
	LastResolved     int
	LastDeduplicated int
}

func NewCandidateNotificationEngine(store *NotificationStore, applications *ApplicationStore, conversations *ConversationStore, clarifications *CandidateClarificationStore, policy FollowUpPolicy, cooldown time.Duration) *CandidateNotificationEngine {
	if policy == (FollowUpPolicy{}) {
		policy = DefaultFollowUpPolicy()
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}
	return &CandidateNotificationEngine{Store: store, Applications: applications, Conversations: conversations, Clarifications: clarifications, Policy: policy, Cooldown: cooldown}
}

func (e *CandidateNotificationEngine) Calculate(snapshot CareerSnapshot, now time.Time) ([]CandidateNotification, error) {
	if e == nil || e.Store == nil {
		return nil, errors.New("notification engine is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	e.LastCreated, e.LastResolved, e.LastDeduplicated = 0, 0, 0
	if err := e.resolve(snapshot, now); err != nil {
		return nil, err
	}
	apps := map[string]JobApplication{}
	for _, a := range snapshot.Applications {
		apps[a.ID] = a
	}
	for _, c := range snapshot.Conversations {
		var application JobApplication
		for _, a := range snapshot.Applications {
			if a.ConversationID == c.ID {
				application = a
				break
			}
		}
		workflow := classifyCareerWorkflow(application, c, snapshot.Clarifications, now, nil, nil)
		if previous := e.Store.workflowStates[c.ID]; previous != "" && previous != string(workflow.State) && meaningfulWorkflowChange(previous, workflow.State) {
			e.add(NotificationStatusChanged, application.ID, c.ID, fmt.Sprintf("Изменилось состояние диалога по вакансии %s: %s → %s.", firstNonEmpty(c.VacancyTitle, "без названия"), workflowLabel(CareerWorkflowState(previous)), workflow.Label), "state-change/"+c.ID+"/"+previous+"/"+string(workflow.State), now)
		}
		e.Store.workflowStates[c.ID] = string(workflow.State)
		if workflow.State == WorkflowNeedsReply {
			last := latestDeliveredMessage(c)
			if last != nil {
				e.add(NotificationNewEmployerMessage, application.ID, c.ID, fmt.Sprintf("Работодатель написал по вакансии %s.", firstNonEmpty(c.VacancyTitle, "без названия")), "employer-message/"+c.ID+"/"+last.ID, now)
			}
		}
		if workflow.State == WorkflowNeedsUserAction {
			e.add(NotificationCandidateActionRequired, application.ID, c.ID, fmt.Sprintf("По вакансии %s требуется ваш ответ.", firstNonEmpty(c.VacancyTitle, "без названия")), "candidate-action/"+c.ID+"/"+messageFingerprint(c), now)
		}
		if workflow.State == WorkflowNeedsClarification {
			if !hasPendingClarification(snapshot.Clarifications, c.ID, application.ID) {
				e.add(NotificationClarificationRequired, application.ID, c.ID, "Нужно уточнение: в подтверждённых данных кандидата не хватает информации для безопасного ответа.", "clarification-conversation/"+c.ID+"/"+messageFingerprint(c), now)
			}
		}
		if workflow.State == WorkflowNeedsClarification || workflow.State == WorkflowNeedsUserAction {
			// The state-specific notification above is the actionable one.
		}
		if workflow.State == WorkflowInterview {
			fingerprint := "interview/" + c.ID
			if last := latestEmployerMessage(c.Messages); last != nil && urgentInterviewDeadline(last.Text, now) {
				fingerprint = "urgent-interview/" + c.ID + "/" + workflow.LastEmployerMessageHash
			}
			e.add(NotificationInterviewDetected, application.ID, c.ID, fmt.Sprintf("Приглашение на интервью по вакансии %s.", firstNonEmpty(c.VacancyTitle, "без названия")), fingerprint, now)
		}
		if workflow.State == WorkflowExternalAction {
			e.add(NotificationExternalActionRequired, application.ID, c.ID, fmt.Sprintf("По вакансии %s нужно открыть внешнее интервью или тест.", firstNonEmpty(c.VacancyTitle, "без названия")), "external-action/"+c.ID+"/"+workflow.LastEmployerMessageHash, now)
		}
		state := snapshot.ResolveConversation(c, now)
		if state.Status == ConversationManualReview && workflow.State != WorkflowNeedsClarification {
			e.add(NotificationManualReviewRequired, application.ID, c.ID, fmt.Sprintf("Диалог по вакансии %s требует ручной проверки.", firstNonEmpty(c.VacancyTitle, "без названия")), "manual-review/"+c.ID+"/"+strings.Join(state.Warnings, ","), now)
		}
		if application.Status == ApplicationInterview || c.Status == ConversationInterview {
			e.add(NotificationInterviewDetected, application.ID, c.ID, fmt.Sprintf("Обнаружено собеседование по вакансии %s.", firstNonEmpty(c.VacancyTitle, "без названия")), "interview/"+c.ID, now)
		}
		if application.Status == ApplicationOffer || c.Status == ConversationOffer {
			e.add(NotificationOfferDetected, application.ID, c.ID, fmt.Sprintf("Обнаружено предложение по вакансии %s.", firstNonEmpty(c.VacancyTitle, "без названия")), "offer/"+c.ID, now)
		}
	}
	for _, q := range snapshot.Clarifications {
		if q.Status == ClarificationPending {
			// One pending clarification is one notification. The conversation
			// projection above covers summary-only clarifications.
			e.add(NotificationClarificationRequired, q.ApplicationID, q.ConversationID, "Нужно уточнение по сохранённому контексту кандидата.", "clarification/"+q.ID, now)
		}
	}
	for _, followUp := range snapshot.FollowUps(e.Policy, now) {
		if followUp.Status != FollowUpEligible {
			continue
		}
		e.add(NotificationFollowUpAvailable, followUp.ApplicationID, followUp.ConversationID, fmt.Sprintf("По вакансии %s можно сделать follow-up после %.0f дней ожидания.", firstNonEmpty(followUp.VacancyTitle, "без названия"), followUp.DaysWaiting), "follow-up/"+followUp.ApplicationID+"/"+followUpFingerprint(snapshot.input(apps[followUp.ApplicationID])), now)
	}
	return e.Store.List(), nil
}

func hasPendingClarification(values []CandidateClarificationRequest, conversationID, applicationID string) bool {
	for _, q := range values {
		if q.Status != ClarificationPending {
			continue
		}
		if conversationID != "" && q.ConversationID == conversationID {
			return true
		}
		if applicationID != "" && q.ApplicationID == applicationID {
			return true
		}
	}
	return false
}

func urgentInterviewDeadline(text string, now time.Time) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "дедлайн") || strings.Contains(lower, "срок") || strings.Contains(lower, "до ") {
		return strings.Contains(lower, "сегодня") || strings.Contains(lower, "завтра") || strings.Contains(lower, "24 час") || strings.Contains(lower, "48 час")
	}
	if strings.Contains(lower, "интервью сегодня") || strings.Contains(lower, "интервью завтра") || strings.Contains(lower, "собеседование сегодня") || strings.Contains(lower, "собеседование завтра") {
		return true
	}
	_ = now
	return false
}

func (e *CandidateNotificationEngine) add(kind CandidateNotificationType, applicationID, conversationID, message, fingerprint string, now time.Time) {
	if _, exists := e.Store.FindFingerprint(fingerprint); exists {
		e.LastDeduplicated++
		return
	}
	for _, existing := range e.Store.notifications {
		if existing.Type == kind && existing.RelatedApplicationID == applicationID && now.Sub(existing.CreatedAt) < e.Cooldown && existing.SnoozedUntil == nil {
			e.LastDeduplicated++
			return
		}
	}
	id, err := newKnowledgeID("notification")
	if err != nil {
		return
	}
	e.Store.notifications = append(e.Store.notifications, CandidateNotification{ID: id, Type: kind, RelatedApplicationID: applicationID, RelatedConversationID: conversationID, Fingerprint: fingerprint, Message: message, CreatedAt: now, Priority: notificationPriority(kind, fingerprint), Lifecycle: NotificationNew})
	e.LastCreated++
}

func notificationPriority(kind CandidateNotificationType, fingerprint string) NotificationPriority {
	if kind == NotificationDeliveryUncertain || strings.HasPrefix(fingerprint, "urgent-interview/") {
		return NotificationPriorityCritical
	}
	switch kind {
	case NotificationNewEmployerMessage, NotificationCandidateActionRequired, NotificationInterviewDetected, NotificationExternalActionRequired, NotificationOfferDetected:
		return NotificationPriorityHigh
	case NotificationClarificationRequired, NotificationFollowUpAvailable, NotificationManualReviewRequired:
		return NotificationPriorityMedium
	default:
		return NotificationPriorityLow
	}
}

func meaningfulWorkflowChange(previous string, current CareerWorkflowState) bool {
	if current == WorkflowNoReplyNeeded || current == WorkflowWaitingForEmployer && previous == string(WorkflowNeedsReply) {
		return false
	}
	return previous != string(current)
}

func (e *CandidateNotificationEngine) resolve(snapshot CareerSnapshot, now time.Time) error {
	if e == nil || e.Store == nil {
		return errors.New("notification engine is not configured")
	}
	conversations := map[string]EmployerConversation{}
	applications := map[string]JobApplication{}
	for _, c := range snapshot.Conversations {
		conversations[c.ID] = c
	}
	for _, a := range snapshot.Applications {
		applications[a.ID] = a
	}
	followUps := map[string]bool{}
	for _, f := range snapshot.FollowUps(e.Policy, now) {
		followUps[f.ConversationID] = f.Status == FollowUpEligible
	}
	pending := func(n CandidateNotification) bool {
		for _, q := range snapshot.Clarifications {
			if q.Status == ClarificationPending && (q.ID == strings.TrimPrefix(n.Fingerprint, "clarification/") || (q.ConversationID != "" && q.ConversationID == n.RelatedConversationID) || (q.ApplicationID != "" && q.ApplicationID == n.RelatedApplicationID)) {
				return true
			}
		}
		if c, ok := conversations[n.RelatedConversationID]; ok {
			return len(c.Summary.PendingQuestions) > 0
		}
		return false
	}
	for i := range e.Store.notifications {
		n := &e.Store.notifications[i]
		if n.Lifecycle == NotificationDismissed || n.Lifecycle == NotificationResolved {
			continue
		}
		c, hasConversation := conversations[n.RelatedConversationID]
		terminal := hasConversation && func() bool { _, ok := terminalConversationState(c); return ok }()
		candidateReplied := false
		if hasConversation {
			latestEmployer := latestEmployerMessage(c.Messages)
			latestCandidate := latestCandidateMessage(c.Messages)
			candidateReplied = latestCandidate != nil && (latestEmployer == nil || latestCandidate.Timestamp.After(latestEmployer.Timestamp))
		}
		shouldResolve := terminal
		switch n.Type {
		case NotificationClarificationRequired:
			shouldResolve = terminal || !pending(*n)
		case NotificationFollowUpAvailable:
			shouldResolve = terminal || !followUps[n.RelatedConversationID]
		case NotificationNewEmployerMessage, NotificationCandidateActionRequired:
			shouldResolve = candidateReplied || terminal
		case NotificationInterviewDetected, NotificationExternalActionRequired:
			shouldResolve = candidateReplied || terminal
		case NotificationStatusChanged:
			parts := strings.Split(n.Fingerprint, "/")
			if len(parts) >= 4 && hasConversation {
				shouldResolve = string(classifyCareerWorkflow(notificationApplicationForConversation(applications, c), c, snapshot.Clarifications, now, nil, nil).State) != parts[3]
			}
		}
		if shouldResolve {
			n.Lifecycle = NotificationResolved
			resolved := now
			n.ResolvedAt = &resolved
			e.LastResolved++
		}
	}
	return nil
}

func notificationApplicationForConversation(applications map[string]JobApplication, c EmployerConversation) JobApplication {
	for _, a := range applications {
		if a.ConversationID == c.ID {
			return a
		}
	}
	return JobApplication{}
}

func latestCandidateMessage(messages []ConversationMessage) *ConversationMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Sender == ConversationSenderCandidate && messages[i].Source != ConversationSourceAIDraft && !messages[i].HHSystemEvent {
			value := messages[i]
			return &value
		}
	}
	return nil
}

type CareerMonitorState struct {
	LastRunAt           time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	LastSyncResult      any       `json:"last_sync_result,omitempty"`
	LastReconcileResult any       `json:"last_reconcile_result,omitempty"`
	LastAuditResult     any       `json:"last_audit_result,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	NextRunAt           time.Time `json:"next_run_at,omitempty"`
}

type CareerMonitor struct {
	Sync          *HHReadSyncService
	Reconciler    *CareerDataReconciler
	Notifications *CandidateNotificationEngine
	Applications  *ApplicationStore
	Conversations *ConversationStore
	StatePath     string
	State         CareerMonitorState
	Interval      time.Duration
	QuietHours    string
}

func NewCareerMonitor(syncService *HHReadSyncService, reconciler *CareerDataReconciler, notifications *CandidateNotificationEngine, applications *ApplicationStore, conversations *ConversationStore, statePath string, interval time.Duration) *CareerMonitor {
	if statePath == "" {
		statePath = CareerMonitorStateFilename
	}
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &CareerMonitor{Sync: syncService, Reconciler: reconciler, Notifications: notifications, Applications: applications, Conversations: conversations, StatePath: statePath, Interval: interval}
}

func (m *CareerMonitor) LoadState() error {
	if m == nil {
		return errors.New("monitor is nil")
	}
	raw, err := os.ReadFile(m.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		m.State = CareerMonitorState{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &m.State)
}

func (m *CareerMonitor) saveState() error {
	raw, err := json.MarshalIndent(m.State, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateStoreWrite(m.StatePath, raw, ".career-monitor-state-*.tmp")
}

func (m *CareerMonitor) RunOnce(ctx context.Context, now time.Time) error {
	if m == nil || m.Sync == nil || m.Reconciler == nil || m.Notifications == nil {
		return errors.New("career monitor is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	m.State.LastRunAt = now
	conversationSync, syncErr := m.Sync.RefreshInbox(ctx)
	all := HHSyncAllResult{Conversations: conversationSync}
	m.State.LastSyncResult = all
	if syncError := firstSyncError(append(append(all.Vacancies.Errors, all.Applications.Errors...), append(all.Conversations.Errors, safeSyncError(syncErr))...)); syncError != "" {
		m.State.ConsecutiveFailures++
		m.State.NextRunAt = now.Add(m.Interval)
		if m.State.ConsecutiveFailures >= 3 {
			m.Notifications.add(NotificationSyncProblem, "", "", "HH read-only sync недоступна; проверьте подключение и состояние локальных данных.", "sync-problem", now)
			_ = m.Notifications.Store.Save()
		}
		_ = m.saveState()
		return errors.New(syncError)
	}
	reconcileResult, err := m.Reconciler.Reconcile(false)
	if err != nil {
		m.State.ConsecutiveFailures++
		_ = m.saveState()
		return err
	}
	m.State.LastReconcileResult = reconcileResult
	if err := m.resolveStates(now); err != nil {
		return err
	}
	vacancies, _ := m.Reconciler.Vacancies.List(ctx, VacancyQuery{})
	applications, _ := m.Applications.ListApplications()
	conversations, _ := m.Conversations.ListConversations()
	events := []ApplicationEvent{}
	for _, application := range applications {
		if timeline, timelineErr := m.Applications.GetApplicationTimeline(application.ID); timelineErr == nil {
			events = append(events, timeline...)
		}
	}
	snapshot := CareerSnapshot{Vacancies: vacancies, Applications: applications, Events: events, Conversations: conversations, Consistency: map[string][]string{}}
	if m.Notifications.Clarifications != nil {
		snapshot.Clarifications = m.Notifications.Clarifications.clarifications
	}
	m.State.LastAuditResult = BuildCareerAuditReport(snapshot, m.Notifications.Policy, now)
	if !quietHoursActive(m.QuietHours, now) {
		if _, err := m.Notifications.Calculate(snapshot, now); err != nil {
			return err
		}
		if err := m.Notifications.Store.Save(); err != nil {
			return err
		}
	}
	m.State.LastSuccessAt, m.State.LastRunAt = now, now
	m.State.ConsecutiveFailures = 0
	m.State.NextRunAt = now.Add(m.Interval)
	return m.saveState()
}

func (m *CareerMonitor) resolveStates(now time.Time) error {
	conversations, err := m.Conversations.ListConversations()
	if err != nil {
		return err
	}
	applications, err := m.Applications.ListApplications()
	if err != nil {
		return err
	}
	for _, c := range conversations {
		var a JobApplication
		for _, item := range applications {
			if item.ConversationID == c.ID {
				a = item
				break
			}
		}
		pending := false
		if m.Notifications.Clarifications != nil {
			for _, q := range m.Notifications.Clarifications.clarifications {
				if q.Status == ClarificationPending && (q.ConversationID == c.ID || q.ApplicationID == a.ID) {
					pending = true
				}
			}
		}
		var events []ApplicationEvent
		if a.ID != "" {
			events, _ = m.Applications.GetApplicationTimeline(a.ID)
		}
		state := (ConversationStateResolver{}).Resolve(a, c, knownApplicationTime(a, events), pending, nil, now)
		if state.Status != c.Status || !sameConversationTime(state.WaitingSince, c.WaitingSince) {
			if err := m.Conversations.UpdateConversationState(c.ID, ConversationState{Status: state.Status, NextAction: c.NextAction, WaitingSince: state.WaitingSince, FollowUpState: c.FollowUpState}); err != nil {
				return err
			}
		}
	}
	return m.Conversations.Save()
}

func (m *CareerMonitor) Run(ctx context.Context) error {
	if err := m.LoadState(); err != nil {
		return err
	}
	for {
		if err := m.RunOnce(ctx, time.Now().UTC()); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(m.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func latestDeliveredMessage(c EmployerConversation) *ConversationMessage {
	messages := deliveredMessages(c.Messages)
	if len(messages) == 0 {
		return nil
	}
	return &messages[len(messages)-1]
}

func messageFingerprint(c EmployerConversation) string {
	if message := latestDeliveredMessage(c); message != nil {
		return message.ID
	}
	return c.ID + "/empty"
}

func monitorStatePath(wd string) string { return filepath.Join(wd, CareerMonitorStateFilename) }

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

func validQuietHours(spec string) bool {
	parts := strings.Split(strings.TrimSpace(spec), "-")
	if len(parts) != 2 {
		return false
	}
	for _, value := range parts {
		if _, err := time.Parse("15:04", strings.TrimSpace(value)); err != nil {
			return false
		}
	}
	return true
}
