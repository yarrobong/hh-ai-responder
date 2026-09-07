package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type syncFakeReadClient struct {
	vacancies       []HHVacancyRecord
	applications    []HHApplicationRecord
	conversations   []HHConversationRecord
	vacancyErr      error
	applicationErr  error
	conversationErr error
}

type targetedSyncFakeReadClient struct {
	syncFakeReadClient
}

func (f *targetedSyncFakeReadClient) ReadConversation(_ context.Context, externalID string) (HHConversationRecord, error) {
	for _, value := range f.conversations {
		if value.ExternalID == externalID {
			return value, nil
		}
	}
	return HHConversationRecord{}, errors.New("not found")
}

func (f *syncFakeReadClient) ReadVacancies(_ context.Context, cursor string) (HHVacancyPage, error) {
	if f.vacancyErr != nil {
		return HHVacancyPage{}, f.vacancyErr
	}
	if cursor != "" {
		return HHVacancyPage{}, nil
	}
	return HHVacancyPage{Items: f.vacancies}, nil
}

func (f *syncFakeReadClient) ReadApplications(_ context.Context, cursor string) (HHApplicationPage, error) {
	if f.applicationErr != nil {
		return HHApplicationPage{}, f.applicationErr
	}
	if cursor != "" {
		return HHApplicationPage{}, nil
	}
	return HHApplicationPage{Items: f.applications}, nil
}

func (f *syncFakeReadClient) ReadConversations(_ context.Context, cursor string) (HHConversationPage, error) {
	if f.conversationErr != nil {
		return HHConversationPage{}, f.conversationErr
	}
	if cursor != "" {
		return HHConversationPage{}, nil
	}
	return HHConversationPage{Items: f.conversations}, nil
}

type syncAnalyzerSpy struct{ calls int }

func (a *syncAnalyzerSpy) Analyze(_ Vacancy, _ any) MatchResult {
	a.calls++
	recommendation := ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "fixture analysis"}
	return MatchResult{Score: 77, Confidence: .8, MatchedSkills: []string{}, UnknownSkills: []string{}, MatchedRoles: []string{}, MatchedProjects: []string{}, MissingSkills: []string{}, Risks: []string{}, Recommendations: []string{"fixture analysis"}, Recommendation: &recommendation}
}

func newSyncStores(t *testing.T) (*VacancyStore, *ApplicationStore, *ConversationStore, string) {
	t.Helper()
	dir := t.TempDir()
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	applications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename), conversations)
	vacancies := NewVacancyStore(filepath.Join(dir, VacanciesFilename))
	return vacancies, applications, conversations, filepath.Join(dir, HHSyncStateFilename)
}

func syncFixtureVacancy(description string) HHVacancyRecord {
	return HHVacancyRecord{ExternalID: "hh-v-1", ID: 42, Title: "Python developer", Company: "Fixture", Description: description,
		Requirements: []string{"Python"}, KeySkills: []string{"Python", "Django"}, Salary: "100000", Currency: "RUR", Location: "Екатеринбург", URL: "https://hh.ru/vacancy/42"}
}

func TestHHReadSyncImportsAndUpdatesVacancyAndAnalyzes(t *testing.T) {
	vacancies, applications, conversations, statePath := newSyncStores(t)
	spy := &syncAnalyzerSpy{}
	fake := &syncFakeReadClient{vacancies: []HHVacancyRecord{syncFixtureVacancy("first")}}
	service := NewHHReadSyncService(fake, vacancies, applications, conversations, spy, statePath)
	result, err := service.SyncVacancies(context.Background())
	if err != nil || result.Created != 1 || spy.calls != 1 {
		t.Fatalf("first sync: %+v calls=%d err=%v", result, spy.calls, err)
	}
	value, err := vacancies.GetByExternalID("hh-v-1")
	if err != nil || value.ID != 42 || value.Source != "hh" || value.MatchResult == nil || value.ApplicationRecommendation == nil {
		t.Fatalf("vacancy was not imported/analyzed: %+v %v", value, err)
	}

	result, err = service.SyncVacancies(context.Background())
	if err != nil || result.Unchanged != 1 || result.Created != 0 || spy.calls != 1 {
		t.Fatalf("duplicate sync: %+v calls=%d err=%v", result, spy.calls, err)
	}
	fake.vacancies[0].Description = "changed"
	result, err = service.SyncVacancies(context.Background())
	if err != nil || result.Updated != 1 || spy.calls != 2 {
		t.Fatalf("changed sync: %+v calls=%d err=%v", result, spy.calls, err)
	}
	value, _ = vacancies.GetByExternalID("hh-v-1")
	if value.Description != "changed" {
		t.Fatalf("vacancy description was not updated: %+v", value)
	}
}

func TestHHReadSyncRefreshesOneConversation(t *testing.T) {
	_, applications, conversations, statePath := newSyncStores(t)
	fake := &targetedSyncFakeReadClient{syncFakeReadClient: syncFakeReadClient{conversations: []HHConversationRecord{
		{ExternalID: "5599440665", Company: "Sber2B", VacancyTitle: "Fullstack", Status: "RESPONSE", Messages: []HHMessageRecord{{ExternalID: "m-1", Sender: "employer", Direction: "incoming", Text: "Укажите зарплату", Timestamp: time.Unix(1, 0).UTC()}}, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()},
		{ExternalID: "5599440666", Company: "Other", VacancyTitle: "Other", Status: "RESPONSE", Messages: []HHMessageRecord{{ExternalID: "m-2", Sender: "employer", Direction: "incoming", Text: "Другой чат", Timestamp: time.Unix(2, 0).UTC()}}, CreatedAt: time.Unix(2, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC()},
	}}}
	service := NewHHReadSyncService(fake, applications, conversations, statePath)
	result, err := service.SyncConversation("5599440665", context.Background())
	if err != nil || result.Fetched != 1 || result.Created != 1 || result.Errors != nil {
		t.Fatalf("targeted sync failed: %+v err=%v", result, err)
	}
	if _, err := conversations.GetByHHConversationID("5599440666"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("targeted sync imported unrelated conversation: %v", err)
	}
}

func TestHHReadSyncImportsApplicationAndUnknownStatusSafely(t *testing.T) {
	vacancies, applications, conversations, statePath := newSyncStores(t)
	_, err := vacancies.Create(Vacancy{ID: 42, ExternalID: "hh-v-1", Name: "Python developer"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &syncFakeReadClient{applications: []HHApplicationRecord{{ExternalID: "hh-response-1", VacancyExternalID: "hh-v-1", Status: "new_status", Company: "Fixture", VacancyTitle: "Python developer"}}}
	service := NewHHReadSyncService(fake, vacancies, applications, conversations, statePath)
	result, err := service.SyncApplications(context.Background())
	if err != nil || result.Created != 1 || len(result.Warnings) != 1 {
		t.Fatalf("application sync: %+v err=%v", result, err)
	}
	items, _ := applications.ListApplications()
	if len(items) != 1 || items[0].Status != ApplicationUnknown || items[0].RawStatus != "new_status" || items[0].VacancyID != 42 {
		t.Fatalf("unknown HH status was not fail-safe: %+v", items)
	}
	result, _ = service.SyncApplications(context.Background())
	if result.Unchanged != 1 || len(items) != 1 {
		t.Fatalf("duplicate application import: %+v", result)
	}
}

func TestHHReadSyncImportsConversationMessagesAndUpdatesApplication(t *testing.T) {
	vacancies, applications, conversations, statePath := newSyncStores(t)
	_, _ = vacancies.Create(Vacancy{ID: 42, ExternalID: "hh-v-1", Name: "Python developer"})
	application, err := applications.CreateApplication(JobApplication{ExternalID: "hh-response-1", VacancyID: 42, Source: ApplicationSourceHH, Status: ApplicationApplied})
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	fake := &syncFakeReadClient{conversations: []HHConversationRecord{{Status: "RESPONSE", ExternalID: "hh-chat-1", VacancyExternalID: "hh-v-1", Company: "Fixture", VacancyTitle: "Python developer", Messages: []HHMessageRecord{
		{ExternalID: "m-2", Sender: "employer", Text: "Приглашаем на интервью", Timestamp: t2},
		{ExternalID: "m-1", Sender: "candidate", Text: "Спасибо", Timestamp: t1},
	}}}}
	service := NewHHReadSyncService(fake, vacancies, applications, conversations, statePath)
	result, err := service.SyncEmployerConversations(context.Background())
	if err != nil || result.Created != 1 {
		t.Fatalf("conversation sync: %+v err=%v", result, err)
	}
	items, _ := conversations.ListConversations()
	if len(items) != 1 || len(items[0].Messages) != 2 || items[0].Messages[0].ExternalID != "m-1" || items[0].Status != ConversationCandidateActionRequired {
		t.Fatalf("conversation/messages were not imported in order: %+v", items)
	}
	updated, _ := applications.GetApplication(application.ID)
	if updated.ConversationID != items[0].ID || updated.Status != ApplicationEmployerReplied {
		t.Fatalf("application was not linked/updated: %+v", updated)
	}
	result, err = service.SyncEmployerConversations(context.Background())
	if err != nil || result.Unchanged != 1 || len(items[0].Messages) != 2 {
		t.Fatalf("duplicate conversation sync: %+v err=%v", result, err)
	}
}

func TestLatestHHConversationMessageIDIgnoresSystemEventsAndUsesLocalID(t *testing.T) {
	messages, warnings := mapHHMessages([]HHMessageRecord{
		{ExternalID: "system-2", SystemEvent: true, Sender: "system", Timestamp: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		{ExternalID: "employer-1", Sender: "employer", Text: "Вопрос", Timestamp: time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)},
	})
	if len(warnings) != 0 {
		t.Fatalf("unexpected HH message warnings: %v", warnings)
	}
	if got := latestHHConversationMessageID(messages); got != "hh-message-employer-1" {
		t.Fatalf("preflight last message ID mismatch: got %q", got)
	}
}

func TestHHReadSyncContextAndInboxAreReadOnly(t *testing.T) {
	_, _, conversations, statePath := newSyncStores(t)
	timeValue := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	fake := &syncFakeReadClient{conversations: []HHConversationRecord{{Status: "RESPONSE", ExternalID: "chat-1", VacancyID: 42, Company: "Fixture", VacancyTitle: "Support", Messages: []HHMessageRecord{{ExternalID: "m-1", Sender: "employer", Text: "Расскажите о Python", Timestamp: timeValue}}}}}
	service := NewHHReadSyncService(fake, conversations, statePath)
	if _, err := service.SyncEmployerConversations(context.Background()); err != nil {
		t.Fatal(err)
	}
	conversationList, err := conversations.ListConversations()
	if err != nil || len(conversationList) != 1 {
		t.Fatalf("conversation was not stored: %+v err=%v", conversationList, err)
	}
	kb := NewCandidateKnowledgeBase(filepath.Join(t.TempDir(), "candidate_profile.json"))
	builder := NewConversationContextBuilder(conversations, NewCandidateContextResolver(kb))
	contextValue, err := builder.BuildForReply(conversationList[0].ID)
	if err != nil || len(contextValue.RecentMessages) != 1 || contextValue.RecentMessages[0].Text != "Расскажите о Python" {
		t.Fatalf("context builder did not use imported HH history: %+v err=%v", contextValue, err)
	}
	inbox, err := NewHHReadSyncService(nil, conversations).GetCandidateInbox()
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].LatestMessage == nil {
		t.Fatalf("inbox did not expose candidate action: %+v err=%v", inbox, err)
	}
}

func TestHHReadSyncErrorsDoNotOverwriteStore(t *testing.T) {
	vacancies, applications, conversations, statePath := newSyncStores(t)
	created, err := vacancies.Create(Vacancy{ExternalID: "keep", Name: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := vacancies.Save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(filepath.Dir(statePath), VacanciesFilename))
	service := NewHHReadSyncService(&syncFakeReadClient{vacancyErr: errors.New("401 Unauthorized")}, vacancies, applications, conversations, statePath)
	result, _ := service.SyncVacancies(context.Background())
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "401") {
		t.Fatalf("HH error not reflected: %+v", result)
	}
	after, _ := os.ReadFile(filepath.Join(filepath.Dir(statePath), VacanciesFilename))
	if string(before) != string(after) {
		t.Fatalf("read error damaged local store")
	}
	if _, err := vacancies.Get(created.ID); err != nil {
		t.Fatal("existing vacancy disappeared after read error")
	}
}

func TestHHStatusMapperUnknownIsNotGuessed(t *testing.T) {
	mapper := NewHHStatusMapper()
	status, known := mapper.Map("brand_new_hh_state")
	if known || status != ApplicationUnknown || mapper.Warning("brand_new_hh_state") == "" {
		t.Fatalf("unknown status was not fail-safe: %q %v", status, known)
	}
}
