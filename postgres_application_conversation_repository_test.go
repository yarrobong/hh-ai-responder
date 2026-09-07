package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// PostgreSQL contract tests are opt-in, like the vacancy contract test. They
// use only generated IDs and clean up their own rows, so ordinary unit tests
// never need a live database or HH credentials.
func TestPostgresCareerRepositoriesTransactionContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}

	stamp := time.Now().UTC()
	vacancyID := int(stamp.UnixNano() % 1_000_000_000)
	if vacancyID <= 0 {
		vacancyID = 1
	}
	applicationID := "pg-contract-application-" + stamp.Format("20060102150405.000000000")
	conversationID := "pg-contract-conversation-" + stamp.Format("20060102150405.000000000")
	messageID := "pg-contract-message-" + stamp.Format("20060102150405.000000000")
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM application_events WHERE application_id=$1`, applicationID)
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE id=$1`, applicationID)
		_, _ = pool.Exec(ctx, `DELETE FROM vacancies WHERE id=$1`, vacancyID)
	}()

	career := NewPostgresCareerStore(pool)
	createdAt := time.Date(2026, 9, 1, 10, 11, 12, 123456789, time.UTC)
	err = career.WithTx(ctx, func(tx CareerTx) error {
		if _, err := tx.Vacancies().Create(ctx, Vacancy{ID: vacancyID, ExternalID: "pg-contract-vacancy-" + applicationID, Name: "Python", CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			return err
		}
		if _, err := tx.Applications().Create(ctx, JobApplication{ID: applicationID, ExternalID: "pg-contract-external-" + applicationID, VacancyID: vacancyID, CompanyName: "Company", VacancyTitle: "Python", Source: ApplicationSourceHH, Status: ApplicationDiscovered, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			return err
		}
		if _, err := tx.Conversations().Upsert(ctx, EmployerConversation{ID: conversationID, VacancyID: vacancyID, ApplicationID: applicationID, CompanyName: "Company", VacancyTitle: "Python", CreatedAt: createdAt}); err != nil {
			return err
		}
		if _, err := tx.Conversations().AppendMessage(ctx, conversationID, ConversationMessage{ID: messageID, ExternalID: "pg-contract-external-message", Timestamp: createdAt.Add(time.Minute), Sender: ConversationSenderEmployer, Text: "Hello", Source: ConversationSourceHH, Direction: ConversationIncoming, Metadata: map[string]string{"kind": "inbox"}}); err != nil {
			return err
		}
		return tx.Applications().AppendEvent(ctx, applicationID, createdAt.Add(2*time.Minute), ApplicationEventMessageReceived, "received")
	})
	if err != nil {
		t.Fatal(err)
	}

	applications := NewPostgresApplicationRepository(pool)
	conversations := NewPostgresConversationRepository(pool)
	gotApplication, err := applications.Get(ctx, applicationID)
	if err != nil || gotApplication.VacancyID != vacancyID {
		t.Fatalf("application transaction result=%+v err=%v", gotApplication, err)
	}
	gotEvents, err := applications.Timeline(ctx, applicationID)
	if err != nil || len(gotEvents) != 2 || gotEvents[1].Type != ApplicationEventMessageReceived {
		t.Fatalf("application events=%+v err=%v", gotEvents, err)
	}
	gotConversation, err := conversations.Get(ctx, conversationID)
	if err != nil || len(gotConversation.Messages) != 1 || gotConversation.Messages[0].ID != messageID || gotConversation.Messages[0].Metadata["kind"] != "inbox" {
		t.Fatalf("conversation transaction result=%+v err=%v", gotConversation, err)
	}
	if _, err := conversations.AppendMessage(ctx, conversationID, ConversationMessage{ID: messageID, ExternalID: "pg-contract-external-message", Timestamp: createdAt.Add(time.Minute), Sender: ConversationSenderEmployer, Text: "rewritten", Source: ConversationSourceHH, Direction: ConversationIncoming}); err == nil {
		t.Fatal("conflicting duplicate message was accepted")
	}

	rollbackApplicationID := applicationID + "-rollback"
	rollbackVacancyID := vacancyID + 1
	err = career.WithTx(ctx, func(tx CareerTx) error {
		if _, err := tx.Vacancies().Create(ctx, Vacancy{ID: rollbackVacancyID, ExternalID: "pg-rollback-vacancy-" + rollbackApplicationID, Name: "Rollback", CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			return err
		}
		if _, err := tx.Applications().Create(ctx, JobApplication{ID: rollbackApplicationID, ExternalID: "pg-rollback-external-" + rollbackApplicationID, VacancyID: rollbackVacancyID, Source: ApplicationSourceHH, Status: ApplicationDiscovered, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			return err
		}
		return errors.New("intentional transaction rollback")
	})
	if err == nil {
		t.Fatal("transaction callback error was ignored")
	}
	if _, err := applications.Get(ctx, rollbackApplicationID); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("rolled back application is visible: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vacancies WHERE id=$1`, rollbackVacancyID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back vacancy count=%d", count)
	}
}

func TestCareerRepositorySelectionKeepsOneJSONOrPostgresBackend(t *testing.T) {
	ctx := context.Background()
	vacancies := NewVacancyStore("vacancies.json")
	applications := NewApplicationStore("applications.json")
	conversations := NewConversationStore("conversations.json")
	career, closeCareer, err := BuildCareerRepositories(ctx, Config{}, vacancies, applications, conversations)
	if err != nil {
		t.Fatal(err)
	}
	defer closeCareer()
	if _, ok := career.Vacancies.(*JSONVacancyRepository); !ok {
		t.Fatalf("vacancy repository type=%T", career.Vacancies)
	}
	if _, ok := career.Applications.(*JSONApplicationRepository); !ok {
		t.Fatalf("application repository type=%T", career.Applications)
	}
	if _, ok := career.Conversations.(*JSONConversationRepository); !ok {
		t.Fatalf("conversation repository type=%T", career.Conversations)
	}
	if career.Postgres != nil {
		t.Fatal("JSON career bundle unexpectedly exposes a PostgreSQL transaction store")
	}
	if _, _, err := BuildCareerRepositories(ctx, Config{StorageBackend: storageBackendPostgres}, vacancies, applications, conversations); err == nil {
		t.Fatal("postgres career backend accepted an empty database URL")
	}
}
