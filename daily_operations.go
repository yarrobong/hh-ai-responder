package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DailyRefreshStateFilename = "daily_refresh_state.json"

type TodayChange struct {
	Kind           string    `json:"kind"`
	ConversationID string    `json:"conversation_id,omitempty"`
	ApplicationID  string    `json:"application_id,omitempty"`
	CompanyName    string    `json:"company_name,omitempty"`
	VacancyTitle   string    `json:"vacancy_title,omitempty"`
	Description    string    `json:"description"`
	At             time.Time `json:"at"`
}

type DailySummary struct {
	NeedsReply  int    `json:"needs_reply"`
	NeedsAction int    `json:"needs_action"`
	Interviews  int    `json:"interviews"`
	FollowUps   int    `json:"follow_ups"`
	NewChanges  int    `json:"new_changes"`
	Text        string `json:"text"`
}

type DailyRefreshState struct {
	LastRefreshAt     time.Time                        `json:"last_refresh_at,omitempty"`
	WorkflowStates    map[string]string                `json:"workflow_states,omitempty"`
	MessageHashes     map[string]string                `json:"message_hashes,omitempty"`
	FollowUpEligible  map[string]bool                  `json:"follow_up_eligible,omitempty"`
	NotificationState map[string]NotificationLifecycle `json:"notification_state,omitempty"`
	IrrelevantItems   map[string]string                `json:"irrelevant_items,omitempty"`
	Changes           []TodayChange                    `json:"changes,omitempty"`
	Summary           DailySummary                     `json:"summary"`
	LastRefresh       *DailyRefreshRun                 `json:"last_refresh,omitempty"`
}

type DailyNotificationStats struct {
	Created      int `json:"created"`
	Resolved     int `json:"resolved"`
	Deduplicated int `json:"deduplicated"`
	Snoozed      int `json:"snoozed"`
}

type DailyRefreshRun struct {
	RefreshedAt             time.Time              `json:"refreshed_at"`
	Inbox                   SyncResult             `json:"inbox"`
	TargetedConversationIDs []string               `json:"targeted_conversation_ids,omitempty"`
	WorkflowItems           int                    `json:"workflow_items"`
	FollowUpsEligible       int                    `json:"follow_ups_eligible"`
	Notifications           DailyNotificationStats `json:"notifications"`
	Summary                 DailySummary           `json:"summary"`
}

type TodayView struct {
	GeneratedAt   time.Time               `json:"generated_at"`
	LastRefreshAt time.Time               `json:"last_refresh_at,omitempty"`
	NeedsReply    []CandidateInboxItem    `json:"needs_reply"`
	NeedsAction   []CandidateInboxItem    `json:"needs_action"`
	Interviews    []CandidateInboxItem    `json:"interviews"`
	FollowUps     []CandidateInboxItem    `json:"follow_ups"`
	NewChanges    []TodayChange           `json:"new_changes"`
	Notifications []CandidateNotification `json:"notifications"`
	Summary       DailySummary            `json:"summary"`
}

func (s *DailyRefreshState) normalize() {
	if s.WorkflowStates == nil {
		s.WorkflowStates = map[string]string{}
	}
	if s.MessageHashes == nil {
		s.MessageHashes = map[string]string{}
	}
	if s.FollowUpEligible == nil {
		s.FollowUpEligible = map[string]bool{}
	}
	if s.NotificationState == nil {
		s.NotificationState = map[string]NotificationLifecycle{}
	}
	if s.IrrelevantItems == nil {
		s.IrrelevantItems = map[string]string{}
	}
	if s.Changes == nil {
		s.Changes = []TodayChange{}
	}
}

func loadDailyRefreshState(path string) (DailyRefreshState, error) {
	state := DailyRefreshState{}
	if strings.TrimSpace(path) == "" {
		state.normalize()
		return state, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		state.normalize()
		return state, nil
	}
	if err != nil {
		return state, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return state, err
	}
	state.normalize()
	return state, nil
}

func saveDailyRefreshState(path string, state DailyRefreshState) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	state.normalize()
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateStoreWrite(path, raw, ".daily-refresh-*.tmp")
}

func dailySummary(items []CandidateInboxItem, changes int) DailySummary {
	summary := DailySummary{NewChanges: changes}
	for _, item := range items {
		switch item.Workflow.State {
		case WorkflowNeedsReply:
			summary.NeedsReply++
		case WorkflowNeedsUserAction, WorkflowNeedsClarification:
			summary.NeedsAction++
		case WorkflowInterview, WorkflowExternalAction:
			summary.Interviews++
		}
		if item.Workflow.FollowUp != nil && item.Workflow.FollowUp.Eligible && item.Workflow.State == WorkflowWaitingForEmployer {
			summary.FollowUps++
		}
	}
	summary.Text = fmt.Sprintf("Сегодня: %d нужно ответить, %d действия, %d follow-up, %d интервью.", summary.NeedsReply, summary.NeedsAction, summary.FollowUps, summary.Interviews)
	return summary
}

func buildTodayView(inbox CandidateInbox, notifications []CandidateNotification, state DailyRefreshState, now time.Time) TodayView {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	view := TodayView{GeneratedAt: now, LastRefreshAt: state.LastRefreshAt, NeedsReply: []CandidateInboxItem{}, NeedsAction: []CandidateInboxItem{}, Interviews: []CandidateInboxItem{}, FollowUps: []CandidateInboxItem{}, NewChanges: append([]TodayChange{}, state.Changes...), Notifications: []CandidateNotification{}}
	for _, item := range inbox.Items {
		if hash := state.IrrelevantItems[item.Conversation.ID]; hash != "" && hash == item.Workflow.LastEmployerMessageHash {
			continue
		}
		switch item.Workflow.State {
		case WorkflowNeedsReply:
			view.NeedsReply = append(view.NeedsReply, item)
		case WorkflowNeedsUserAction, WorkflowNeedsClarification:
			view.NeedsAction = append(view.NeedsAction, item)
		case WorkflowInterview, WorkflowExternalAction:
			view.Interviews = append(view.Interviews, item)
		case WorkflowWaitingForEmployer:
			if item.Workflow.FollowUp != nil && item.Workflow.FollowUp.Eligible {
				view.FollowUps = append(view.FollowUps, item)
			}
		}
	}
	for _, n := range notifications {
		if n.Lifecycle == NotificationDismissed || n.Lifecycle == NotificationResolved || n.Lifecycle == NotificationSnoozed {
			continue
		}
		view.Notifications = append(view.Notifications, n)
	}
	filtered := append([]CandidateInboxItem{}, view.NeedsReply...)
	filtered = append(filtered, view.NeedsAction...)
	filtered = append(filtered, view.Interviews...)
	filtered = append(filtered, view.FollowUps...)
	view.Summary = dailySummary(filtered, len(view.NewChanges))
	return view
}

func dailyChanges(previous DailyRefreshState, inbox CandidateInbox, notifications []CandidateNotification, at time.Time) []TodayChange {
	changes := []TodayChange{}
	seen := map[string]bool{}
	add := func(key string, change TodayChange) {
		if !seen[key] {
			seen[key] = true
			changes = append(changes, change)
		}
	}
	for _, item := range inbox.Items {
		c := item.Conversation
		messageHash := item.Workflow.LastEmployerMessageHash
		if old := previous.MessageHashes[c.ID]; old != "" && messageHash != "" && old != messageHash {
			add("message/"+c.ID+"/"+messageHash, TodayChange{Kind: "new_employer_message", ConversationID: c.ID, CompanyName: c.CompanyName, VacancyTitle: c.VacancyTitle, Description: "Новое сообщение работодателя.", At: at})
		}
		if old := previous.WorkflowStates[c.ID]; old != "" && old != string(item.Workflow.State) {
			kind := "status_changed"
			description := fmt.Sprintf("Состояние диалога изменилось: %s → %s.", workflowLabel(CareerWorkflowState(old)), item.Workflow.Label)
			if item.Workflow.State == WorkflowInterview || item.Workflow.State == WorkflowExternalAction {
				kind, description = "new_interview", "Появился новый этап интервью или внешнее действие."
			}
			add("state/"+c.ID+"/"+old+"/"+string(item.Workflow.State), TodayChange{Kind: kind, ConversationID: c.ID, CompanyName: c.CompanyName, VacancyTitle: c.VacancyTitle, Description: description, At: at})
		}
		eligible := item.Workflow.FollowUp != nil && item.Workflow.FollowUp.Eligible
		if eligible && !previous.FollowUpEligible[c.ID] {
			add("follow-up/"+c.ID, TodayChange{Kind: "new_follow_up", ConversationID: c.ID, CompanyName: c.CompanyName, VacancyTitle: c.VacancyTitle, Description: "Follow-up стал доступен.", At: at})
		}
	}
	for _, n := range notifications {
		if n.Lifecycle == NotificationResolved && previous.NotificationState[n.ID] != NotificationResolved {
			add("resolved/"+n.ID, TodayChange{Kind: "resolved", ConversationID: n.RelatedConversationID, ApplicationID: n.RelatedApplicationID, Description: "Элемент автоматически закрыт.", At: at})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].At.Before(changes[j].At) })
	return changes
}

func (s *DashboardServer) dailyRefreshStateFile() string {
	if s == nil {
		return ""
	}
	return s.DailyRefreshStatePath
}

func (s *DashboardServer) refreshDailyOperations(now time.Time, inbox CandidateInbox, syncResult SyncResult) (DailyRefreshRun, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := s.refreshNotifications(now); err != nil {
		return DailyRefreshRun{}, err
	}
	state := s.DailyRefreshState
	state.normalize()
	notifications := s.Notifications.List()
	changes := dailyChanges(state, inbox, notifications, now)
	workflowStates, messageHashes, followUps := map[string]string{}, map[string]string{}, map[string]bool{}
	for _, item := range inbox.Items {
		workflowStates[item.Conversation.ID] = string(item.Workflow.State)
		messageHashes[item.Conversation.ID] = item.Workflow.LastEmployerMessageHash
		followUps[item.Conversation.ID] = item.Workflow.FollowUp != nil && item.Workflow.FollowUp.Eligible
	}
	notificationState := map[string]NotificationLifecycle{}
	for _, n := range notifications {
		notificationState[n.ID] = n.Lifecycle
	}
	run := DailyRefreshRun{RefreshedAt: now, Inbox: syncResult, TargetedConversationIDs: append([]string{}, syncResult.ChangedConversationIDs...), WorkflowItems: len(inbox.Items), Notifications: s.lastNotificationStats, Summary: dailySummary(inbox.Items, len(changes))}
	for _, n := range notifications {
		if n.Lifecycle == NotificationResolved && previousNotificationLifecycle(state.NotificationState, n.ID) != NotificationResolved {
			run.Notifications.Resolved++
		}
		if n.Lifecycle == NotificationSnoozed && previousNotificationLifecycle(state.NotificationState, n.ID) != NotificationSnoozed {
			run.Notifications.Snoozed++
		}
	}
	state.LastRefreshAt, state.WorkflowStates, state.MessageHashes, state.FollowUpEligible, state.NotificationState, state.Changes = now, workflowStates, messageHashes, followUps, notificationState, changes
	run.FollowUpsEligible = run.Summary.FollowUps
	state.Summary, state.LastRefresh = run.Summary, &run
	s.DailyRefreshState = state
	return run, saveDailyRefreshState(s.dailyRefreshStateFile(), state)
}

func previousNotificationLifecycle(states map[string]NotificationLifecycle, id string) NotificationLifecycle {
	if states == nil {
		return ""
	}
	return states[id]
}

func (s *DashboardServer) today() (TodayView, error) {
	inbox, err := s.inbox()
	if err != nil {
		return TodayView{}, err
	}
	notifications := []CandidateNotification{}
	if s.Notifications != nil {
		notifications = s.Notifications.List()
	}
	view := buildTodayView(inbox, notifications, s.DailyRefreshState, time.Now().UTC())
	for _, notification := range view.Notifications {
		_ = s.recordNotificationFeedback(notification, "shown")
	}
	return view, nil
}

func dailyRefreshStatePathFor(wd string) string { return filepath.Join(wd, DailyRefreshStateFilename) }
