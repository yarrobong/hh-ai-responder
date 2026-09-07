package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestApplicationStorageContract_RestartPreservesIDsExternalIDsMatchAndEventHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), JobApplicationsFilename)
	createdAt := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	store := NewApplicationStore(path)
	input := JobApplication{
		ID: "application-fixed", VacancyID: 42, ExternalID: "hh-negotiation-42", CompanyName: "Fixture Company",
		VacancyTitle: "Python/Django", VacancyURL: "https://hh.example/vacancy/42", Source: ApplicationSourceHH,
		CreatedAt: createdAt, UpdatedAt: updatedAt, Status: ApplicationDiscovered, RawStatus: "response",
		HHMetadata: map[string]string{"negotiation_id": "hh-negotiation-42", "state": "new"}, Partial: true,
		DataCompleteness:       DataCompletenessPartial,
		ReconciliationEvidence: []ReconciliationEvidence{{Method: "hh_negotiation_id", Source: "hh", Confidence: 1, ReconciledAt: createdAt}},
		MatchResult: &MatchResult{
			Score: 82, Confidence: 0.75, MatchedSkills: []string{"Python"}, UnknownSkills: []string{"Kubernetes"},
			MatchedRoles: []string{"integration"}, MatchedProjects: []string{"API tool"}, MissingSkills: []string{"Kafka"},
			Risks: []string{"unknown Kubernetes experience"}, RiskDetails: []MatchRisk{{Type: "unknown", Description: "Kubernetes is unconfirmed"}},
			ExperienceNote: "fixture evidence only", Explanation: "strong partial match", Recommendations: []string{"review unknowns"},
			Recommendation: &ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "manual review required"},
		},
	}
	created, err := store.CreateApplication(input)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != input.ID || created.ExternalID != input.ExternalID || !created.CreatedAt.Equal(createdAt) || !created.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("application identity/timestamps changed on create: %+v", created)
	}
	if err := store.SaveMatchResult(created.ID, *created.MatchResult); err != nil {
		// The existing implementation treats an equivalent result as idempotent;
		// this branch documents that the operation remains safe either way.
		t.Fatal(err)
	}
	if err := store.UpdateStatus(created.ID, ApplicationApplied); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEvent(created.ID, time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC), ApplicationEventMessageReceived, "fixture message received"); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetApplication(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantEvents, err := store.GetApplicationTimeline(created.ID)
	if err != nil || len(wantEvents) != 3 {
		t.Fatalf("unexpected application event history: %+v err=%v", wantEvents, err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	restarted := NewApplicationStore(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetApplication("application-fixed")
	if err != nil {
		t.Fatal(err)
	}
	byExternal, err := restarted.GetByExternalID("hh-negotiation-42")
	if err != nil {
		t.Fatal(err)
	}
	gotEvents, err := restarted.GetApplicationTimeline("application-fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, current) || !reflect.DeepEqual(byExternal, current) || !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("application restart changed domain state: got=%+v current=%+v events=%+v want=%+v", got, current, gotEvents, wantEvents)
	}
	if got.MatchResult == nil || len(got.MatchResult.UnknownSkills) != 1 || got.HHMetadata["negotiation_id"] != "hh-negotiation-42" ||
		got.DataCompleteness != DataCompletenessPartial || !got.Partial || got.Status != ApplicationApplied {
		t.Fatalf("application match/provenance/status state changed: %+v", got)
	}
}

func TestApplicationStorageContract_MissingCorruptAndUnknownVersionPreserveMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, JobApplicationsFilename)
	missing := NewApplicationStore(path)
	if err := missing.Load(); err != nil {
		t.Fatal(err)
	}
	applications, err := missing.ListApplications()
	if err != nil || len(applications) != 0 {
		t.Fatalf("missing application file did not load as empty: %+v err=%v", applications, err)
	}
	if NewApplicationStore("").Load() == nil || NewApplicationStore("").Save() == nil {
		t.Fatal("empty application path was accepted")
	}

	store := NewApplicationStore(path)
	keep, err := store.CreateApplication(JobApplication{ID: "keep", VacancyID: 1, ExternalID: "keep-ext", Source: ApplicationSourceManual, Status: ApplicationDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"corrupt":  "{",
		"version":  `{"version":2,"applications":[],"events":[]}`,
		"null":     `{"version":1,"applications":null,"events":[]}`,
		"extra":    `{"version":1,"applications":[],"events":[],"extra":true}`,
		"orphan":   `{"version":1,"applications":[],"events":[{"id":"e","application_id":"missing","timestamp":"2026-09-01T00:00:00Z","type":"created","description":"orphan"}]}`,
		"trailing": `{"version":1,"applications":[],"events":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Load(); err == nil {
				t.Fatal("invalid application store was accepted")
			}
			got, err := store.GetApplication(keep.ID)
			if err != nil || got.ExternalID != keep.ExternalID {
				t.Fatalf("failed load replaced memory: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestStorageContract_VacancyApplicationConversationRelationSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	vacancies := NewVacancyStore(filepath.Join(dir, VacanciesFilename))
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	applications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename), conversations)
	if err := vacancies.Load(); err != nil {
		t.Fatal(err)
	}
	if err := conversations.Load(); err != nil {
		t.Fatal(err)
	}
	if err := applications.Load(); err != nil {
		t.Fatal(err)
	}
	vacancy, err := vacancies.Create(Vacancy{ID: 42, ExternalID: "hh-vacancy-42", Name: "Python/Django"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := conversations.UpsertConversation(EmployerConversation{ID: "conversation-fixed", VacancyID: vacancy.ID, HHConversationID: "hh-chat-42", CompanyName: "Fixture Company", VacancyTitle: vacancy.Name})
	if err != nil {
		t.Fatal(err)
	}
	application, err := applications.CreateApplication(JobApplication{ID: "application-fixed", VacancyID: vacancy.ID, ExternalID: "hh-negotiation-42", CompanyName: vacancy.Company.Name, VacancyTitle: vacancy.Name, Source: ApplicationSourceHH, Status: ApplicationDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if err := applications.AttachConversation(application.ID, conversation.ID); err != nil {
		t.Fatal(err)
	}
	if err := vacancies.Save(); err != nil {
		t.Fatal(err)
	}
	if err := applications.Save(); err != nil {
		t.Fatal(err)
	}
	if err := conversations.Save(); err != nil {
		t.Fatal(err)
	}

	restartedConversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	restartedApplications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename), restartedConversations)
	restartedVacancies := NewVacancyStore(filepath.Join(dir, VacanciesFilename))
	if err := restartedVacancies.Load(); err != nil {
		t.Fatal(err)
	}
	if err := restartedConversations.Load(); err != nil {
		t.Fatal(err)
	}
	if err := restartedApplications.Load(); err != nil {
		t.Fatal(err)
	}
	loadedVacancy, err := restartedVacancies.GetByExternalID("hh-vacancy-42")
	if err != nil {
		t.Fatal(err)
	}
	loadedApplication, err := restartedApplications.GetByExternalID("hh-negotiation-42")
	if err != nil {
		t.Fatal(err)
	}
	loadedConversation, err := restartedConversations.GetByHHConversationID("hh-chat-42")
	if err != nil {
		t.Fatal(err)
	}
	if loadedVacancy.ID != loadedApplication.VacancyID || loadedApplication.VacancyID != loadedConversation.VacancyID ||
		loadedApplication.ConversationID != loadedConversation.ID || loadedApplication.ID != application.ID || loadedConversation.ID != conversation.ID {
		t.Fatalf("vacancy/application/conversation relation changed: vacancy=%+v application=%+v conversation=%+v", loadedVacancy, loadedApplication, loadedConversation)
	}
}
