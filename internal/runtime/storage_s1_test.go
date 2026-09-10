package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

type s1VacancyRepository struct {
	values []vacancy.Vacancy
	reads  int
}

func (r *s1VacancyRepository) Get(_ context.Context, id int) (vacancy.Vacancy, error) {
	r.reads++
	for _, value := range r.values {
		if value.ID == id {
			return value, nil
		}
	}
	return vacancy.Vacancy{}, vacancy.ErrVacancyNotFound
}
func (r *s1VacancyRepository) GetByExternalID(_ context.Context, id string) (vacancy.Vacancy, error) {
	r.reads++
	for _, value := range r.values {
		if value.ExternalID == id {
			return value, nil
		}
	}
	return vacancy.Vacancy{}, vacancy.ErrVacancyNotFound
}
func (r *s1VacancyRepository) List(context.Context, ports.VacancyQuery) ([]vacancy.Vacancy, error) {
	r.reads++
	return append([]vacancy.Vacancy(nil), r.values...), nil
}
func (r *s1VacancyRepository) Create(_ context.Context, value vacancy.Vacancy) (vacancy.Vacancy, error) {
	r.values = append(r.values, value)
	return value, nil
}
func (r *s1VacancyRepository) Update(_ context.Context, value vacancy.Vacancy) error {
	for i := range r.values {
		if r.values[i].ID == value.ID {
			r.values[i] = value
			return nil
		}
	}
	return vacancy.ErrVacancyNotFound
}
func (*s1VacancyRepository) Save(context.Context) error { return nil }

type s1ApplicationRepository struct {
	values []application.JobApplication
	events []application.Event
	reads  int
}

func (r *s1ApplicationRepository) Get(_ context.Context, id string) (application.JobApplication, error) {
	r.reads++
	for _, value := range r.values {
		if value.ID == id {
			return value, nil
		}
	}
	return application.JobApplication{}, application.ErrApplicationNotFound
}
func (r *s1ApplicationRepository) GetByExternalID(_ context.Context, id string) (application.JobApplication, error) {
	r.reads++
	for _, value := range r.values {
		if value.ExternalID == id {
			return value, nil
		}
	}
	return application.JobApplication{}, application.ErrApplicationNotFound
}
func (r *s1ApplicationRepository) List(context.Context) ([]application.JobApplication, error) {
	r.reads++
	return append([]application.JobApplication(nil), r.values...), nil
}
func (r *s1ApplicationRepository) Timeline(_ context.Context, id string) ([]application.Event, error) {
	var result []application.Event
	for _, value := range r.events {
		if value.ApplicationID == id {
			result = append(result, value)
		}
	}
	return result, nil
}
func (r *s1ApplicationRepository) Create(_ context.Context, value application.JobApplication) (application.JobApplication, error) {
	if value.ID == "" {
		value.ID = "pg-application"
	}
	r.values = append(r.values, value)
	return value, nil
}
func (r *s1ApplicationRepository) Update(_ context.Context, value application.JobApplication) error {
	for i := range r.values {
		if r.values[i].ID == value.ID {
			r.values[i] = value
			return nil
		}
	}
	return application.ErrApplicationNotFound
}
func (r *s1ApplicationRepository) UpsertImported(context.Context, application.JobApplication) (application.JobApplication, bool, error) {
	return application.JobApplication{}, false, errors.New("not used")
}
func (*s1ApplicationRepository) AttachImportedConversation(context.Context, string, string) error {
	return errors.New("not used")
}
func (*s1ApplicationRepository) AttachConversation(context.Context, string, string) error {
	return errors.New("not used")
}
func (*s1ApplicationRepository) UpdateStatus(context.Context, string, application.Status) error {
	return errors.New("not used")
}
func (*s1ApplicationRepository) SetFollowUpState(context.Context, string, conversation.FollowUpState) error {
	return errors.New("not used")
}
func (*s1ApplicationRepository) SaveMatchResult(context.Context, string, vacancy.MatchResult) error {
	return errors.New("not used")
}
func (r *s1ApplicationRepository) AppendEvent(_ context.Context, id string, at time.Time, kind application.EventType, description string) error {
	r.events = append(r.events, application.Event{ID: "event", ApplicationID: id, Timestamp: at, Type: kind, Description: description})
	return nil
}
func (*s1ApplicationRepository) Save(context.Context) error { return nil }
func (r *s1ApplicationRepository) ListEvents(context.Context) ([]application.Event, error) {
	return append([]application.Event(nil), r.events...), nil
}

type s1ConversationRepository struct {
	values []conversation.EmployerConversation
	reads  int
}

func (r *s1ConversationRepository) Get(_ context.Context, id string) (conversation.EmployerConversation, error) {
	r.reads++
	for _, value := range r.values {
		if value.ID == id {
			return value, nil
		}
	}
	return conversation.EmployerConversation{}, conversation.ErrConversationNotFound
}
func (r *s1ConversationRepository) GetByHHConversationID(_ context.Context, id string) (conversation.EmployerConversation, error) {
	r.reads++
	for _, value := range r.values {
		if value.HHConversationID == id {
			return value, nil
		}
	}
	return conversation.EmployerConversation{}, conversation.ErrConversationNotFound
}
func (r *s1ConversationRepository) GetByVacancyID(_ context.Context, id int) ([]conversation.EmployerConversation, error) {
	r.reads++
	var result []conversation.EmployerConversation
	for _, value := range r.values {
		if value.VacancyID == id {
			result = append(result, value)
		}
	}
	return result, nil
}
func (r *s1ConversationRepository) List(context.Context) ([]conversation.EmployerConversation, error) {
	r.reads++
	return append([]conversation.EmployerConversation(nil), r.values...), nil
}
func (r *s1ConversationRepository) Timeline(_ context.Context, id string) ([]conversation.Message, error) {
	value, err := r.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return value.Messages, nil
}
func (*s1ConversationRepository) Upsert(context.Context, conversation.EmployerConversation) (conversation.EmployerConversation, error) {
	return conversation.EmployerConversation{}, errors.New("not used")
}
func (*s1ConversationRepository) AppendMessage(context.Context, string, conversation.Message) (conversation.Message, error) {
	return conversation.Message{}, errors.New("not used")
}
func (*s1ConversationRepository) UpdateState(context.Context, string, conversation.State) error {
	return errors.New("not used")
}
func (*s1ConversationRepository) UpdateSummary(context.Context, string, conversation.Summary) error {
	return errors.New("not used")
}
func (*s1ConversationRepository) AppendCandidateClaim(context.Context, string, conversation.Claim) error {
	return errors.New("not used")
}

func TestS1PostgresFacadesIgnoreLegacyJSONRepositories(t *testing.T) {
	vacancyRepo := &s1VacancyRepository{values: []vacancy.Vacancy{{ID: 42, ExternalID: "pg-42", Title: "Postgres vacancy"}}}
	applicationRepo := &s1ApplicationRepository{values: []application.JobApplication{{ID: "pg-app", VacancyID: 42, VacancyTitle: "Postgres application", Status: application.StatusApplied}}}
	conversationRepo := &s1ConversationRepository{values: []conversation.EmployerConversation{{ID: "pg-conv", HHConversationID: "hh-42", VacancyID: 42, Status: conversation.StatusApplied}}}
	vacancies := newVacancyStoreFromRepository(vacancyRepo)
	applications := newApplicationStoreFromRepository(applicationRepo)
	conversations := newConversationStoreFromRepository(conversationRepo)

	gotVacancies, err := vacancies.List()
	if err != nil || len(gotVacancies) != 1 || gotVacancies[0].ExternalID != "pg-42" {
		t.Fatalf("vacancy facade did not read selected repository: %#v, %v", gotVacancies, err)
	}
	gotApplications, err := applications.ListApplications()
	if err != nil || len(gotApplications) != 1 || gotApplications[0].ID != "pg-app" {
		t.Fatalf("application facade did not read selected repository: %#v, %v", gotApplications, err)
	}
	gotConversations, err := conversations.ListConversations()
	if err != nil || len(gotConversations) != 1 || gotConversations[0].ID != "pg-conv" {
		t.Fatalf("conversation facade did not read selected repository: %#v, %v", gotConversations, err)
	}
	if vacancyRepo.reads == 0 || applicationRepo.reads == 0 || conversationRepo.reads == 0 {
		t.Fatal("selected PostgreSQL repositories did not receive read calls")
	}
}

func TestS1SyncAndDashboardSnapshotsUseCareerBundle(t *testing.T) {
	vacancyRepo := &s1VacancyRepository{values: []vacancy.Vacancy{{ID: 7, ExternalID: "new", Title: "NEW from postgres"}}}
	applicationRepo := &s1ApplicationRepository{values: []application.JobApplication{{ID: "new-app", VacancyID: 7, VacancyTitle: "NEW application", Status: application.StatusApplied}}}
	conversationRepo := &s1ConversationRepository{values: []conversation.EmployerConversation{{ID: "new-conv", HHConversationID: "new-hh", VacancyID: 7, VacancyTitle: "NEW conversation", Status: conversation.StatusCandidateActionRequired}}}
	career := CareerRepositories{Vacancies: vacancyRepo, Applications: applicationRepo, Conversations: conversationRepo}
	service := NewHHReadSyncServiceWithRepositories(nil, career, HHReadSyncServiceOptions{})
	if service.vacancies.careerRepository != vacancyRepo || service.applications.careerRepository != applicationRepo || service.conversations.careerRepository != conversationRepo {
		t.Fatal("HH sync service did not retain the selected career repository bundle")
	}
	server := &DashboardServer{DashboardDependencies: DashboardDependencies{Vacancies: service.vacancies, Applications: service.applications, Conversations: service.conversations}}
	snapshot := server.careerSnapshotLocal()
	if len(snapshot.Vacancies) != 1 || snapshot.Vacancies[0].Title != "NEW from postgres" || len(snapshot.Applications) != 1 || len(snapshot.Conversations) != 1 {
		t.Fatalf("dashboard snapshot did not use selected career bundle: %#v", snapshot)
	}
	inbox, err := service.GetCandidateInbox()
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].Conversation.ID != "new-conv" {
		t.Fatalf("inbox did not use selected conversation repository: %#v, %v", inbox, err)
	}
}
