package runtime

import (
	"testing"
	"time"
)

func migrationFixture() CareerMigrationSource {
	at := time.Date(2026, 9, 1, 10, 0, 0, 123456789, time.UTC)
	message := ConversationMessage{ID: "message-1", ExternalID: "hh-message-1", Timestamp: at.Add(time.Minute), Sender: ConversationSenderEmployer, Text: "Добрый день", Source: ConversationSourceHH, Direction: ConversationIncoming}
	lastMessageAt := message.Timestamp
	conversation := EmployerConversation{ID: "conversation-1", HHConversationID: "hh-conversation-1", VacancyID: 10, CompanyName: "Company", VacancyTitle: "Python", Status: ConversationInterview, FollowUpState: ConversationFollowUpNone, CreatedAt: at, UpdatedAt: message.Timestamp, LastEmployerMessageAt: &lastMessageAt, LastActivityAt: &lastMessageAt, Messages: []ConversationMessage{message}}
	return CareerMigrationSource{
		Vacancies:         []Vacancy{{ID: 10, ExternalID: "hh-vacancy-10", Name: "Python backend", CreatedAt: at, UpdatedAt: at}},
		Applications:      []JobApplication{{ID: "application-1", ExternalID: "hh-application-1", VacancyID: 10, CompanyName: "Company", VacancyTitle: "Python", Source: ApplicationSourceHH, Status: ApplicationInterview, CreatedAt: at, UpdatedAt: at}},
		ApplicationEvents: []ApplicationEvent{{ID: "event-1", ApplicationID: "application-1", Timestamp: at, Type: ApplicationEventCreated, Description: "application created"}},
		Conversations:     []EmployerConversation{conversation},
	}
}

func TestBuildCareerMigrationPlanEmpty(t *testing.T) {
	plan, err := BuildCareerMigrationPlan(CareerMigrationSource{}, CareerMigrationDestination{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Report.SafeToApply || plan.Report.Vacancies.Source != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("empty plan=%+v", plan.Report)
	}
}

func TestBuildCareerMigrationPlanFreshAndRerun(t *testing.T) {
	source := migrationFixture()
	fresh, err := BuildCareerMigrationPlan(source, CareerMigrationDestination{})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Report.SafeToApply || fresh.Report.Vacancies.New != 1 || fresh.Report.Applications.New != 1 || fresh.Report.ApplicationEvents.New != 1 || fresh.Report.Conversations.New != 1 || fresh.Report.Messages.New != 1 {
		t.Fatalf("fresh plan=%+v", fresh.Report)
	}
	destination := CareerMigrationDestination{Vacancies: source.Vacancies, Applications: source.Applications, ApplicationEvents: source.ApplicationEvents, Conversations: source.Conversations}
	rerun, err := BuildCareerMigrationPlan(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !rerun.Report.SafeToApply || rerun.Report.Vacancies.AlreadyPresent != 1 || rerun.Report.Applications.AlreadyPresent != 1 || rerun.Report.ApplicationEvents.AlreadyPresent != 1 || rerun.Report.Conversations.AlreadyPresent != 1 || rerun.Report.Messages.AlreadyPresent != 1 || len(rerun.Conflicts) != 0 {
		t.Fatalf("rerun plan=%+v", rerun.Report)
	}
}

func TestBuildCareerMigrationPlanIdentityAndRelationConflicts(t *testing.T) {
	source := migrationFixture()
	destination := CareerMigrationDestination{Vacancies: []Vacancy{{ID: 10, ExternalID: "different-hh-id", Name: "Python backend", CreatedAt: source.Vacancies[0].CreatedAt, UpdatedAt: source.Vacancies[0].UpdatedAt}}}
	plan, err := BuildCareerMigrationPlan(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.SafeToApply || !hasMigrationConflict(plan.Conflicts, "vacancy", "external_id", migrationCritical) {
		t.Fatalf("expected vacancy identity conflict: %+v", plan.Conflicts)
	}
	broken := migrationFixture()
	broken.Applications[0].VacancyID = 999
	plan, err = BuildCareerMigrationPlan(broken, CareerMigrationDestination{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.SafeToApply || len(plan.RelationErrors) == 0 {
		t.Fatalf("expected relation conflict: %+v", plan.Report)
	}
}

func TestBuildCareerMigrationPlanAppendOnlyDuplicateClassification(t *testing.T) {
	source := migrationFixture()
	conversation := source.Conversations[0]
	conversation.Messages = append([]ConversationMessage{}, conversation.Messages...)
	destination := CareerMigrationDestination{Vacancies: source.Vacancies, Applications: source.Applications, ApplicationEvents: source.ApplicationEvents, Conversations: []EmployerConversation{conversation}}
	destination.Conversations[0].Messages[0].Text = "изменено"
	plan, err := BuildCareerMigrationPlan(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.SafeToApply || !hasMigrationConflict(plan.Conflicts, "message", "payload", migrationCritical) {
		t.Fatalf("expected immutable message conflict: %+v", plan.Conflicts)
	}

	destination.Conversations[0].Messages[0].Text = source.Conversations[0].Messages[0].Text
	destination.Conversations[0].Messages = append(destination.Conversations[0].Messages, ConversationMessage{ID: "message-2", Timestamp: source.Conversations[0].Messages[0].Timestamp.Add(time.Minute), Sender: ConversationSenderEmployer, Text: "ещё", Source: ConversationSourceHH, Direction: ConversationIncoming})
	plan, err = BuildCareerMigrationPlan(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Report.SafeToApply || plan.Report.Conversations.Compatible != 1 || plan.Report.Messages.AlreadyPresent != 1 || plan.Report.Messages.New != 0 {
		t.Fatalf("expected compatible append-only rerun: %+v", plan.Report)
	}
}

func hasMigrationConflict(conflicts []MigrationConflict, entity, field, severity string) bool {
	for _, conflict := range conflicts {
		if conflict.Entity == entity && conflict.Field == field && conflict.Severity == severity {
			return true
		}
	}
	return false
}
