package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func reconciliationStores(t *testing.T) (*VacancyStore, *ApplicationStore, *ConversationStore) {
	t.Helper()
	dir := t.TempDir()
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	applications := NewApplicationStoreWithDependencies(filepath.Join(dir, JobApplicationsFilename), conversations, nil)
	vacancies := NewVacancyStore(filepath.Join(dir, VacanciesFilename))
	return vacancies, applications, conversations
}

func TestReconcileUsesStrongIDsAndPreservesProvenance(t *testing.T) {
	vacancies, applications, conversations := reconciliationStores(t)
	now := time.Now().UTC().Add(-time.Hour)
	if _, err := vacancies.Create(Vacancy{ID: 42, ExternalID: "42", Name: "Backend Developer", Title: "Backend Developer", Description: "Python integration API automation and support. This is sufficient trusted vacancy detail for matching.", Source: "hh", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := applications.CreateApplication(JobApplication{ID: "app-1", ExternalID: "neg-1", VacancyID: 42, CompanyName: "Company A", VacancyTitle: "Backend Developer", Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now, HHMetadata: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := conversations.UpsertConversation(EmployerConversation{ID: "conv-1", VacancyID: 42, HHConversationID: "chat-1", CompanyName: "Completely Different Name", VacancyTitle: "Unrelated title", CreatedAt: now, UpdatedAt: now, HHMetadata: map[string]string{"negotiation_id": "neg-1"}}); err != nil {
		t.Fatal(err)
	}
	r := NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), NewVacancyAnalyzer(), nil)
	result, err := r.Reconcile(false)
	if err != nil || result.LinksFound != 1 {
		t.Fatalf("reconcile result=%+v err=%v", result, err)
	}
	firstEventCount := len(applications.events)
	app, _ := applications.GetApplication("app-1")
	if app.ConversationID != "conv-1" || len(app.ReconciliationEvidence) != 1 || app.ReconciliationEvidence[0].Confidence != 1 {
		t.Fatalf("strong relation/provenance missing: %+v", app)
	}
	if _, err := r.Reconcile(false); err != nil {
		t.Fatal(err)
	}
	if len(applications.events) != firstEventCount {
		t.Fatalf("reconcile is not idempotent: %d events", len(applications.events))
	}
}

func TestReconcileDoesNotUseSimilarNames(t *testing.T) {
	vacancies, applications, conversations := reconciliationStores(t)
	now := time.Now().UTC()
	_, _ = applications.CreateApplication(JobApplication{ID: "app-1", ExternalID: "neg-1", VacancyID: 42, CompanyName: "Acme", VacancyTitle: "Python", Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now})
	_, _ = conversations.UpsertConversation(EmployerConversation{ID: "conv-1", VacancyID: 99, HHConversationID: "chat-1", CompanyName: "Acme", VacancyTitle: "Python", CreatedAt: now, UpdatedAt: now})
	result, err := NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), NewVacancyAnalyzer(), nil).Reconcile(true)
	if err != nil || len(result.UnresolvedRelations) != 2 || result.LinksFound != 0 {
		t.Fatalf("similar names incorrectly linked: %+v err=%v", result, err)
	}
}

func TestReconcileCreatesPartialNegotiationAndSkipsPartialMatch(t *testing.T) {
	vacancies, applications, conversations := reconciliationStores(t)
	now := time.Now().UTC()
	_, _ = conversations.UpsertConversation(EmployerConversation{ID: "conv-1", VacancyID: 7, HHConversationID: "chat-7", CompanyName: "Company", VacancyTitle: "Support", CreatedAt: now, UpdatedAt: now, HHMetadata: map[string]string{"negotiation_id": "neg-7"}})
	result, err := NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), NewVacancyAnalyzer(), nil).Reconcile(false)
	if err != nil || result.ApplicationsCreated != 1 {
		t.Fatalf("partial application not created: %+v err=%v", result, err)
	}
	app, err := applications.GetByExternalID("neg-7")
	if err != nil || !app.Partial || app.MatchResult != nil || len(app.ReconciliationEvidence) != 1 {
		t.Fatalf("partial application invalid: %+v err=%v", app, err)
	}
	if len(result.InsufficientMatchData) != 1 {
		t.Fatalf("partial match should remain insufficient: %+v", result)
	}
}

func TestReconcileBackfillsOnlyFullVacancy(t *testing.T) {
	vacancies, applications, conversations := reconciliationStores(t)
	now := time.Now().UTC()
	_, _ = vacancies.Create(Vacancy{ID: 1, Name: "Full", Description: "Python integration API automation and support. This description contains enough trusted source detail to run a local match.", Source: "hh", CreatedAt: now, UpdatedAt: now})
	_, _ = vacancies.Create(Vacancy{ID: 2, Name: "Partial", Source: "hh", CreatedAt: now, UpdatedAt: now})
	for i, vacancyID := range []int{1, 2} {
		id := "app-" + string(rune('1'+i))
		_, _ = applications.CreateApplication(JobApplication{ID: id, ExternalID: "neg-" + id, VacancyID: vacancyID, VacancyTitle: "Role", Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now})
	}
	result, err := NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), NewVacancyAnalyzer(), nil).Reconcile(false)
	if err != nil || result.MatchesBackfilled != 1 || len(result.InsufficientMatchData) != 1 {
		t.Fatalf("backfill result=%+v err=%v", result, err)
	}
	full, _ := applications.GetApplication("app-1")
	partial, _ := applications.GetApplication("app-2")
	if full.MatchResult == nil || partial.MatchResult != nil {
		t.Fatalf("match safety broken: full=%+v partial=%+v", full.MatchResult, partial.MatchResult)
	}
}

type monitorFixtureClient struct{ conversation HHConversationRecord }

func (c monitorFixtureClient) ReadVacancies(context.Context, string) (HHVacancyPage, error) {
	return HHVacancyPage{}, nil
}
func (c monitorFixtureClient) ReadApplications(context.Context, string) (HHApplicationPage, error) {
	return HHApplicationPage{}, nil
}
func (c monitorFixtureClient) ReadConversations(context.Context, string) (HHConversationPage, error) {
	return HHConversationPage{Items: []HHConversationRecord{c.conversation}}, nil
}

func TestCareerMonitorNotificationIsIdempotentAndReadOnly(t *testing.T) {
	vacancies, applications, conversations := reconciliationStores(t)
	now := time.Now().UTC().Add(-time.Hour)
	_, _ = applications.CreateApplication(JobApplication{ID: "app-1", ExternalID: "neg-1", VacancyID: 42, VacancyTitle: "Backend", Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now})
	notifications := NewNotificationStore(filepath.Join(t.TempDir(), NotificationEventsFilename))
	if err := notifications.Load(); err != nil {
		t.Fatal(err)
	}
	syncService := NewHHReadSyncServiceWithOptions(monitorFixtureClient{conversation: HHConversationRecord{ExternalID: "chat-1", VacancyID: 42, Company: "Company", VacancyTitle: "Backend", CreatedAt: now, UpdatedAt: now, Messages: []HHMessageRecord{{ExternalID: "message-1", Sender: "employer", Text: "Hello", Timestamp: now}}}}, HHReadSyncServiceOptions{Vacancies: vacancies, Applications: applications, Conversations: conversations, StatePath: filepath.Join(t.TempDir(), HHSyncStateFilename)})
	engine := NewCandidateNotificationEngine(notifications, applications, conversations, nil, DefaultFollowUpPolicy(), time.Minute)
	monitor := NewCareerMonitor(syncService, NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), NewVacancyAnalyzer(), nil), engine, applications, conversations, filepath.Join(t.TempDir(), CareerMonitorStateFilename), time.Hour)
	if err := monitor.RunOnce(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(notifications.notifications) == 0 {
		t.Fatalf("expected employer notification: %+v", notifications.notifications)
	}
	initialCount := len(notifications.notifications)
	if err := monitor.RunOnce(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(notifications.notifications) != initialCount {
		t.Fatalf("notification duplicated: %+v", notifications.notifications)
	}
}
