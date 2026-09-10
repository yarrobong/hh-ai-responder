package careeriteration

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/usecase/conversationpolicy"
	"hh-ai-responder/internal/vacancy"
)

type fakeInbox struct {
	result       SyncResult
	err          error
	calls        int
	boundedCalls int
	boundedLimit int
	log          *[]string
}

func (f *fakeInbox) RefreshInbox(context.Context) (SyncResult, error) {
	f.calls++
	*f.log = append(*f.log, "refresh")
	return f.result, f.err
}

func (f *fakeInbox) RefreshInboxBounded(_ context.Context, limit int) (SyncResult, error) {
	f.boundedCalls++
	f.boundedLimit = limit
	*f.log = append(*f.log, "refresh-bounded")
	return f.result, f.err
}

type fakeReconciler struct {
	result ReconciliationResult
	err    error
	log    *[]string
}

func (f fakeReconciler) Reconcile(context.Context, bool) (ReconciliationResult, error) {
	*f.log = append(*f.log, "reconcile")
	return f.result, f.err
}

type fakeVacancies struct {
	values []vacancy.Vacancy
	err    error
	log    *[]string
}

func (f fakeVacancies) List(context.Context, ports.VacancyQuery) ([]vacancy.Vacancy, error) {
	*f.log = append(*f.log, "vacancies")
	return f.values, f.err
}

type fakeApplications struct {
	values      []application.JobApplication
	listCalls   int
	firstErr    error
	listErr     error
	log         *[]string
	timelineErr error
}

func (f *fakeApplications) List(context.Context) ([]application.JobApplication, error) {
	f.listCalls++
	*f.log = append(*f.log, "applications")
	if f.listCalls == 1 && f.firstErr != nil {
		return nil, f.firstErr
	}
	if f.listCalls > 1 && f.listErr != nil {
		return nil, f.listErr
	}
	return f.values, nil
}

func (f *fakeApplications) Timeline(context.Context, string) ([]application.Event, error) {
	*f.log = append(*f.log, "timeline")
	return nil, f.timelineErr
}

type fakeConversations struct {
	values    []conversation.EmployerConversation
	listCalls int
	firstErr  error
	listErr   error
	updateErr error
	saveErr   error
	log       *[]string
}

func (f *fakeConversations) List(context.Context) ([]conversation.EmployerConversation, error) {
	f.listCalls++
	*f.log = append(*f.log, "conversations")
	if f.listCalls == 1 && f.firstErr != nil {
		return nil, f.firstErr
	}
	if f.listCalls > 1 && f.listErr != nil {
		return nil, f.listErr
	}
	return f.values, nil
}

func (f *fakeConversations) UpdateState(context.Context, string, conversation.State) error {
	*f.log = append(*f.log, "update-state")
	return f.updateErr
}

func (f *fakeConversations) Save(context.Context) error {
	*f.log = append(*f.log, "conversation-save")
	return f.saveErr
}

type fakeClarifications struct {
	values []candidateacquisition.CandidateClarificationRequest
	err    error
	log    *[]string
}

func (f fakeClarifications) List() ([]candidateacquisition.CandidateClarificationRequest, error) {
	*f.log = append(*f.log, "clarifications")
	return f.values, f.err
}

type fakeResolver struct {
	log *[]string
}

func (f fakeResolver) Resolve(_ application.JobApplication, c conversation.EmployerConversation, _ *time.Time, _ bool, _ []string, _ time.Time) conversationpolicy.Resolution {
	*f.log = append(*f.log, "resolve")
	return conversationpolicy.Resolution{Status: c.Status, WaitingSince: c.WaitingSince}
}

type fakeNotifications struct {
	result       NotificationResult
	calculateErr error
	saveErr      error
	records      int
	log          *[]string
}

func (f *fakeNotifications) Calculate(Snapshot, time.Time) (NotificationResult, error) {
	*f.log = append(*f.log, "notify-calculate")
	return f.result, f.calculateErr
}

func (f *fakeNotifications) RecordSyncProblem(time.Time) {
	f.records++
	*f.log = append(*f.log, "sync-problem")
}

func (f *fakeNotifications) Save() error {
	*f.log = append(*f.log, "notify-save")
	return f.saveErr
}

type fakeAuditor struct {
	log *[]string
}

func (f fakeAuditor) Audit(Snapshot, time.Time) json.RawMessage {
	*f.log = append(*f.log, "audit")
	return json.RawMessage(`{"audit":true}`)
}

type fakeStateStore struct {
	values []State
	err    error
	log    *[]string
}

func (f *fakeStateStore) Save(value State) error {
	f.values = append(f.values, value)
	*f.log = append(*f.log, "state-save")
	return f.err
}

func newFixture() (Dependencies, *[]string, *fakeInbox, *fakeReconciler, *fakeApplications, *fakeConversations, *fakeNotifications, *fakeStateStore) {
	log := []string{}
	inbox := &fakeInbox{log: &log}
	reconciler := &fakeReconciler{log: &log}
	applications := &fakeApplications{log: &log}
	conversations := &fakeConversations{log: &log}
	notifications := &fakeNotifications{log: &log}
	state := &fakeStateStore{log: &log}
	deps := Dependencies{
		Inbox: inbox, Reconciler: reconciler, Vacancies: fakeVacancies{log: &log}, Applications: applications,
		Conversations: conversations, Resolver: fakeResolver{log: &log}, Notifications: notifications,
		Auditor: fakeAuditor{log: &log}, StateStore: state,
	}
	return deps, &log, inbox, reconciler, applications, conversations, notifications, state
}

func fixtureService(deps Dependencies, now time.Time) *Service {
	return NewService(deps, Options{Interval: time.Hour, Now: func() time.Time { return now }})
}

func TestServiceRunSuccessfulSequenceAndResult(t *testing.T) {
	deps, log, inbox, reconciler, applications, conversations, notifications, state := newFixture()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	inbox.result.Fetched = 1
	reconciler.result.LinksFound = 2
	notifications.result = NotificationResult{Created: 1, Resolved: 2, Deduplicated: 3}
	applications.values = []application.JobApplication{{ID: "app-1", VacancyID: 1, Status: application.StatusApplied}}
	conversations.values = []conversation.EmployerConversation{{ID: "conv-1", VacancyID: 1, Status: conversation.StatusApplied}}

	result, err := fixtureService(deps, now).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantLog := []string{"refresh", "reconcile", "conversations", "applications", "resolve", "conversation-save", "vacancies", "applications", "conversations", "timeline", "audit", "notify-calculate", "notify-save", "state-save"}
	if !reflect.DeepEqual(*log, wantLog) {
		t.Fatalf("sequence=%v, want %v", *log, wantLog)
	}
	if result.Sync.Conversations.Fetched != 1 || result.Reconciliation.LinksFound != 2 || result.Notifications.Created != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(state.values) != 1 || state.values[0].ConsecutiveFailures != 0 || !state.values[0].LastSuccessAt.Equal(now) {
		t.Fatalf("unexpected saved state: %+v", state.values)
	}
}

func TestServiceRunBoundedUsesBoundedInboxCapability(t *testing.T) {
	deps, log, inbox, _, _, _, _, _ := newFixture()
	inbox.result.Fetched = 3
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	result, err := fixtureService(deps, now).withMaxConversations(3).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inbox.boundedCalls != 1 || inbox.boundedLimit != 3 || inbox.calls != 0 || result.RequestedConversationLimit != 3 {
		t.Fatalf("bounded run did not use bounded capability: result=%+v calls=%d bounded=%d/%d", result, inbox.calls, inbox.boundedCalls, inbox.boundedLimit)
	}
	if len(*log) == 0 || (*log)[0] != "refresh-bounded" {
		t.Fatalf("unexpected bounded sequence: %v", *log)
	}
}

func TestServiceRunBoundedScopesCareerEvaluationToSelectedProviderOrder(t *testing.T) {
	deps, _, inbox, _, applications, conversations, _, _ := newFixture()
	inbox.result.SelectedConversationIDs = []string{"hh-chat-2"}
	applications.values = []application.JobApplication{
		{ID: "app-1", ConversationID: "conv-1", VacancyID: 1, Status: application.StatusApplied},
		{ID: "app-2", ConversationID: "conv-2", VacancyID: 2, Status: application.StatusApplied},
	}
	conversations.values = []conversation.EmployerConversation{
		{ID: "conv-1", HHConversationID: "hh-chat-1", VacancyID: 1, Status: conversation.StatusApplied},
		{ID: "conv-2", HHConversationID: "hh-chat-2", VacancyID: 2, Status: conversation.StatusApplied},
	}

	result, err := fixtureService(deps, time.Now().UTC()).withMaxConversations(1).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Conversations) != 1 || result.Snapshot.Conversations[0].HHConversationID != "hh-chat-2" || len(result.Snapshot.Applications) != 1 || result.Snapshot.Applications[0].ID != "app-2" {
		t.Fatalf("bounded Career snapshot was not scoped: conversations=%+v applications=%+v", result.Snapshot.Conversations, result.Snapshot.Applications)
	}
}

func (s *Service) withMaxConversations(limit int) *Service {
	s.options.MaxConversations = limit
	return s
}

func TestServiceSyncFailureThresholdRecoveryAndReset(t *testing.T) {
	deps, log, inbox, _, _, _, notifications, state := newFixture()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	inbox.err = errors.New("read failed")
	service := fixtureService(deps, now)
	for i := 1; i <= 3; i++ {
		if _, err := service.Run(context.Background()); err == nil || err.Error() != "read failed" {
			t.Fatalf("run %d error=%v", i, err)
		}
	}
	if notifications.records != 1 || len(state.values) != 3 {
		t.Fatalf("threshold behavior records=%d saves=%d", notifications.records, len(state.values))
	}
	inbox.err = nil
	if _, err := service.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state.values[len(state.values)-1].ConsecutiveFailures; got != 0 {
		t.Fatalf("failure counter was not reset: %d", got)
	}
	if !reflect.DeepEqual(*log, []string{"refresh", "state-save", "refresh", "state-save", "refresh", "sync-problem", "notify-save", "state-save", "refresh", "reconcile", "conversations", "applications", "conversation-save", "vacancies", "applications", "conversations", "audit", "notify-calculate", "notify-save", "state-save"}) {
		t.Fatalf("unexpected failure/recovery sequence: %v", *log)
	}
}

func TestServiceReconciliationFailureStopsIteration(t *testing.T) {
	deps, log, _, reconciler, _, _, notifications, state := newFixture()
	reconcileErr := errors.New("reconcile failed")
	reconciler.err = reconcileErr
	result, err := fixtureService(deps, time.Now().UTC()).Run(context.Background())
	if !errors.Is(err, reconcileErr) || result.Snapshot.Conversations != nil {
		t.Fatalf("unexpected reconciliation failure result=%+v err=%v", result, err)
	}
	if notifications.records != 0 || len(state.values) != 1 || (*log)[len(*log)-1] != "state-save" {
		t.Fatalf("post-reconciliation stages ran: log=%v state=%d", *log, len(state.values))
	}
}

func TestServiceResolutionReadFailuresAreFatal(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*fakeApplications, *fakeConversations, error)
	}{
		{name: "conversations", set: func(_ *fakeApplications, conversations *fakeConversations, err error) { conversations.firstErr = err }},
		{name: "applications", set: func(applications *fakeApplications, _ *fakeConversations, err error) { applications.firstErr = err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps, log, _, _, applications, conversations, _, state := newFixture()
			readErr := errors.New("resolution read failed")
			test.set(applications, conversations, readErr)
			_, err := fixtureService(deps, time.Now().UTC()).Run(context.Background())
			if !errors.Is(err, readErr) || len(state.values) != 0 {
				t.Fatalf("error=%v state saves=%d log=%v", err, len(state.values), *log)
			}
		})
	}
}

func TestServicePreservesIgnoredSnapshotReadErrors(t *testing.T) {
	deps, log, _, _, applications, conversations, _, state := newFixture()
	readErr := errors.New("snapshot read failed")
	deps.Vacancies = fakeVacancies{err: readErr, log: log}
	applications.values = []application.JobApplication{{ID: "app-1", Status: application.StatusApplied}}
	applications.listErr = readErr
	applications.timelineErr = readErr
	conversations.values = []conversation.EmployerConversation{{ID: "conv-1", Status: conversation.StatusApplied}}
	conversations.listErr = readErr
	deps.Clarifications = fakeClarifications{err: readErr, log: log}
	result, err := fixtureService(deps, time.Now().UTC()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Vacancies) != 0 || len(result.Snapshot.Applications) != 0 || len(result.Snapshot.Conversations) != 0 || len(result.Snapshot.Events) != 0 || len(result.Snapshot.Clarifications) != 0 {
		t.Fatalf("ignored read errors did not produce the established empty/partial snapshot: %+v", result.Snapshot)
	}
	if len(state.values) != 1 {
		t.Fatalf("iteration did not complete after ignored reads: %v", *log)
	}
}

func TestServiceNotificationAndStateFailures(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name       string
		notifyErr  error
		saveErr    error
		wantErr    error
		wantStates int
	}{
		{name: "calculate", notifyErr: errors.New("calculate failed"), wantErr: errors.New("calculate failed")},
		{name: "save", saveErr: errors.New("notification save failed"), wantErr: errors.New("notification save failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps, _, _, _, _, _, notifications, state := newFixture()
			notifications.calculateErr = test.notifyErr
			notifications.saveErr = test.saveErr
			_, err := fixtureService(deps, now).Run(context.Background())
			if err == nil || err.Error() != test.wantErr.Error() {
				t.Fatalf("error=%v, want %v", err, test.wantErr)
			}
			if len(state.values) != test.wantStates {
				t.Fatalf("state saves=%d, want %d", len(state.values), test.wantStates)
			}
		})
	}

	deps, _, _, _, _, _, _, state := newFixture()
	state.err = errors.New("monitor state save failed")
	_, err := fixtureService(deps, now).Run(context.Background())
	if err == nil || err.Error() != state.err.Error() {
		t.Fatalf("state error=%v, want %v", err, state.err)
	}
}

func TestServiceQuietHoursSkipNotificationCalculation(t *testing.T) {
	deps, log, _, _, _, _, _, state := newFixture()
	now := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	if _, err := NewService(deps, Options{Interval: time.Hour, QuietHours: "23:00-07:00", Now: func() time.Time { return now }}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*log, []string{"refresh", "reconcile", "conversations", "applications", "conversation-save", "vacancies", "applications", "conversations", "audit", "state-save"}) {
		t.Fatalf("quiet-hours sequence=%v", *log)
	}
	if len(state.values) != 1 {
		t.Fatalf("quiet-hours iteration did not save monitor state")
	}
}

func TestServiceCancellationStopsAtRefresh(t *testing.T) {
	deps, log, inbox, _, _, _, _, state := newFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inbox.err = ctx.Err()
	_, err := fixtureService(deps, time.Now().UTC()).Run(ctx)
	if err == nil || err.Error() != context.Canceled.Error() {
		t.Fatalf("cancellation error=%v", err)
	}
	if !reflect.DeepEqual(*log, []string{"refresh", "state-save"}) || len(state.values) != 1 {
		t.Fatalf("cancellation did not preserve sync-failure behavior: log=%v", *log)
	}
}
