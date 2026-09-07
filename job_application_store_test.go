package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func applicationTestStore(t *testing.T) *ApplicationStore {
	t.Helper()
	store := NewApplicationStore(filepath.Join(t.TempDir(), JobApplicationsFilename))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

func applicationTestValue() JobApplication {
	return JobApplication{
		VacancyID: 123, ExternalID: "hh-response-1", CompanyName: "Fixture Company",
		VacancyTitle: "Python Django developer", VacancyURL: "https://example.test/vacancy/123",
		Source: ApplicationSourceHH, Status: ApplicationDiscovered,
	}
}

func TestApplicationCreateSaveLoadAndDuplicateExternalID(t *testing.T) {
	s := applicationTestStore(t)
	application, err := s.CreateApplication(applicationTestValue())
	if err != nil {
		t.Fatal(err)
	}
	if application.ID == "" || application.Status != ApplicationDiscovered || application.CreatedAt.IsZero() || application.UpdatedAt.IsZero() {
		t.Fatalf("invalid application: %+v", application)
	}
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("in-memory creation wrote a file")
	}
	if _, err := s.CreateApplication(applicationTestValue()); !errors.Is(err, ErrDuplicateExternalID) {
		t.Fatalf("duplicate external id: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private application store mode: %v %v", info, err)
		}
	}
	loaded := NewApplicationStore(s.path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := loaded.GetApplication(application.ID)
	if err != nil || !reflect.DeepEqual(application, got) {
		t.Fatalf("roundtrip changed application: %v %+v", err, got)
	}
}

func TestApplicationStatusAndMatchCreateTimelineEvents(t *testing.T) {
	s := applicationTestStore(t)
	application, err := s.CreateApplication(applicationTestValue())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMatchResult(application.ID, MatchResult{Score: 87, MatchedSkills: []string{"Python", "Django"}, MatchedProjects: []string{"BizonVR"}, MissingSkills: []string{"Kubernetes"}, Risks: []string{"требуется 3 года опыта"}, Explanation: "strong match"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStatus(application.ID, ApplicationApplied); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStatus(application.ID, ApplicationInterview); err != nil {
		t.Fatal(err)
	}
	match, err := s.GetApplication(application.ID)
	if err != nil || match.MatchResult == nil || match.MatchResult.Score != 87 {
		t.Fatalf("match result was not saved: %+v %v", match, err)
	}
	events, err := s.GetApplicationTimeline(application.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("unexpected application timeline: %v %+v", err, events)
	}
	if events[0].Type != ApplicationEventCreated || events[1].Type != ApplicationEventMatched || events[2].Type != ApplicationEventApplied || events[3].Type != ApplicationEventInterviewScheduled {
		t.Fatalf("unexpected event types: %+v", events)
	}
	if events[2].ApplicationID != application.ID || events[2].Description == "" {
		t.Fatalf("event is incomplete: %+v", events[2])
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationConversationLinkAndContext(t *testing.T) {
	dir := t.TempDir()
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	if err := conversations.Load(); err != nil {
		t.Fatal(err)
	}
	conversation, err := conversations.UpsertConversation(EmployerConversation{VacancyID: 123, CompanyName: "Fixture Company", VacancyTitle: "Python Django developer"})
	if err != nil {
		t.Fatal(err)
	}
	kb := contextTestKnowledge()
	applications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename), conversations, NewCandidateContextResolver(kb))
	if err := applications.Load(); err != nil {
		t.Fatal(err)
	}
	application, err := applications.CreateApplication(applicationTestValue())
	if err != nil {
		t.Fatal(err)
	}
	if err := applications.AttachConversation(application.ID, conversation.ID); err != nil {
		t.Fatal(err)
	}
	if err := applications.SaveMatchResult(application.ID, MatchResult{Score: 80, MatchedSkills: []string{"Python"}}); err != nil {
		t.Fatal(err)
	}
	context, err := applications.GetApplicationContext(application.ID)
	if err != nil {
		t.Fatal(err)
	}
	if context.Application.ID != application.ID || context.MatchResult.Score != 80 || context.Conversation.ID != conversation.ID || len(context.Timeline) != 2 {
		t.Fatalf("context links are incomplete: %+v", context)
	}
	if !contextHasName(context.CandidateContext.RelevantSkills, "Python") {
		t.Fatalf("candidate context was not resolved: %+v", context.CandidateContext)
	}
	context.Conversation.CompanyName = "changed"
	stored, err := conversations.GetConversation(conversation.ID)
	if err != nil || stored.CompanyName != "Fixture Company" {
		t.Fatal("application context changed conversation memory")
	}
}

func TestApplicationStatistics(t *testing.T) {
	s := applicationTestStore(t)
	statuses := []ApplicationStatus{ApplicationDiscovered, ApplicationApplied, ApplicationApplied, ApplicationEmployerReplied, ApplicationInterview, ApplicationOffer, ApplicationRejected}
	for i, status := range statuses {
		application, err := s.CreateApplication(JobApplication{VacancyID: i + 1, ExternalID: "external-" + string(rune('a'+i)), Source: ApplicationSourceManual, Status: ApplicationDiscovered})
		if err != nil {
			t.Fatal(err)
		}
		if status != ApplicationDiscovered {
			if err := s.UpdateStatus(application.ID, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	stats := s.GetApplicationStats()
	if stats.Total != 7 || stats.Discovered != 1 || stats.Applied != 6 || stats.WaitingReply != 2 || stats.Interviews != 1 || stats.Offers != 1 || stats.Rejected != 1 {
		t.Fatalf("unexpected application stats: %+v", stats)
	}
	if stats.ResponseRate != 66.66666666666667 || stats.ConversionRate != 16.666666666666668 {
		t.Fatalf("unexpected application rates: %+v", stats)
	}
}

func TestApplicationValidationAndAtomicFailure(t *testing.T) {
	s := applicationTestStore(t)
	if _, err := s.CreateApplication(JobApplication{VacancyID: 1, Source: ApplicationSourceHH, Status: ApplicationDiscovered, MatchResult: &MatchResult{Score: 101}}); err == nil {
		t.Fatal("invalid match score accepted")
	}
	if err := s.UpdateStatus("missing", ApplicationApplied); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("missing status update: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := NewApplicationStore(s.path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if len(loaded.applications) != 0 || len(loaded.events) != 0 {
		t.Fatal("failed mutation changed application store")
	}
	if _, err := s.GetApplicationTimeline("missing"); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("missing timeline: %v", err)
	}
}
