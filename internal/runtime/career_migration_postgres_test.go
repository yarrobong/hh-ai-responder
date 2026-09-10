package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Opt-in integration coverage for the real PostgreSQL repositories. Ordinary
// unit runs do not need a database or perform any external writes.
func TestCareerMigrationPostgresIntegration(t *testing.T) {
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

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	source := migrationFixture()
	source.Vacancies[0].ID = int(time.Now().UnixNano()%900_000_000 + 100_000_000)
	source.Vacancies[0].ExternalID += "-" + suffix
	source.Vacancies[0].TotalResponsesCountKnown = true
	source.Applications[0].VacancyID = source.Vacancies[0].ID
	source.Applications[0].ID += "-" + suffix
	source.Applications[0].ExternalID += "-" + suffix
	source.Conversations[0].ApplicationID = source.Applications[0].ID
	source.ApplicationEvents[0].ID += "-" + suffix
	source.ApplicationEvents[0].ApplicationID = source.Applications[0].ID
	source.Conversations[0].ID += "-" + suffix
	source.Conversations[0].HHConversationID += "-" + suffix
	source.Conversations[0].VacancyID = source.Vacancies[0].ID
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id=$1`, source.Conversations[0].ID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, source.Conversations[0].ID)
		_, _ = pool.Exec(ctx, `DELETE FROM application_events WHERE application_id=$1`, source.Applications[0].ID)
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE id=$1`, source.Applications[0].ID)
		_, _ = pool.Exec(ctx, `DELETE FROM vacancies WHERE id=$1`, source.Vacancies[0].ID)
	}
	cleanup()
	defer cleanup()

	career := CareerRepositories{Vacancies: NewPostgresVacancyRepository(pool), Applications: NewPostgresApplicationRepository(pool), Conversations: NewPostgresConversationRepository(pool), Postgres: NewPostgresCareerStore(pool)}
	destination, err := readCareerMigrationDestination(ctx, career)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildCareerMigrationPlan(source, destination)
	if err != nil || !plan.Report.SafeToApply {
		t.Fatalf("fresh migration plan=%+v err=%v", plan.Report, err)
	}
	if err := ApplyCareerMigration(ctx, career.Postgres, plan); err != nil {
		t.Fatal(err)
	}
	verification, err := verifyCareerMigration(ctx, career, source)
	if err != nil || verification.Messages != 1 || verification.ApplicationEvents != 1 {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}

	destination, err = readCareerMigrationDestination(ctx, career)
	if err != nil {
		t.Fatal(err)
	}
	rerun, err := BuildCareerMigrationPlan(source, destination)
	if err != nil || !rerun.Report.SafeToApply || rerun.Report.Vacancies.AlreadyPresent != 1 || rerun.Report.Messages.AlreadyPresent != 1 || rerun.Report.Messages.New != 0 {
		t.Fatalf("rerun plan=%+v err=%v", rerun.Report, err)
	}

	conflictSource := source
	conflictSource.Applications = append([]JobApplication{}, source.Applications...)
	conflictSource.Applications[0].Status = ApplicationRejected
	conflictPlan, err := BuildCareerMigrationPlan(conflictSource, destination)
	if err != nil || conflictPlan.Report.SafeToApply {
		t.Fatalf("existing destination conflict was not blocked: plan=%+v err=%v", conflictPlan.Report, err)
	}

	rollbackVacancy := source.Vacancies[0].ID + 1
	rollbackApplication := source.Applications[0].ID + "-rollback"
	err = career.Postgres.WithTx(ctx, func(tx CareerTx) error {
		vacancies := tx.Vacancies().(*PostgresVacancyRepository)
		applications := tx.Applications().(*PostgresApplicationRepository)
		value := source.Vacancies[0]
		value.ID, value.ExternalID = rollbackVacancy, value.ExternalID+"-rollback"
		if err := vacancies.Import(ctx, value); err != nil {
			return err
		}
		application := source.Applications[0]
		application.ID, application.ExternalID = rollbackApplication, application.ExternalID+"-rollback"
		application.VacancyID = rollbackVacancy
		if err := applications.Import(ctx, application); err != nil {
			return err
		}
		return errors.New("intentional migration rollback")
	})
	if err == nil {
		t.Fatal("rollback error was ignored")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vacancies WHERE id=$1`, rollbackVacancy).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left vacancy count=%d", count)
	}
}
