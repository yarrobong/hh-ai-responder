package runtime

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConversationBoundary_ApplicationRelationSurvivesRootStores(t *testing.T) {
	created := time.Date(2026, 8, 6, 11, 0, 0, 0, time.UTC)
	applicationPath := filepath.Join(t.TempDir(), JobApplicationsFilename)
	applicationStore := NewApplicationStore(applicationPath)
	application, err := applicationStore.CreateApplication(JobApplication{
		ID: "application-fixed", VacancyID: 42, CompanyName: "Fixture Company", VacancyTitle: "Python/Django",
		Source: ApplicationSourceHH, Status: ApplicationApplied, ConversationID: "conversation-fixed", CreatedAt: created, UpdatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applicationStore.Save(); err != nil {
		t.Fatal(err)
	}

	conversationPath := filepath.Join(t.TempDir(), EmployerConversationsFilename)
	conversationStore := NewConversationStore(conversationPath)
	conversation, err := conversationStore.UpsertConversation(EmployerConversation{
		ID: "conversation-fixed", ApplicationID: application.ID, VacancyID: application.VacancyID, HHConversationID: "hh-chat-42",
		CompanyName: application.CompanyName, VacancyTitle: application.VacancyTitle, Status: ConversationApplied,
		FollowUpState: ConversationFollowUpNone, CreatedAt: created, UpdatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversationStore.Save(); err != nil {
		t.Fatal(err)
	}

	reloadedApplications := NewApplicationStore(applicationPath)
	if err := reloadedApplications.Load(); err != nil {
		t.Fatal(err)
	}
	reloadedApplication, err := reloadedApplications.GetApplication(application.ID)
	if err != nil {
		t.Fatal(err)
	}
	reloadedConversations := NewConversationStore(conversationPath)
	if err := reloadedConversations.Load(); err != nil {
		t.Fatal(err)
	}
	reloadedConversation, err := reloadedConversations.GetConversation(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedApplication.ConversationID != reloadedConversation.ID || reloadedConversation.ApplicationID != reloadedApplication.ID || reloadedConversation.VacancyID != reloadedApplication.VacancyID {
		t.Fatalf("application/conversation relation changed: application=%+v conversation=%+v", application, conversation)
	}
}
