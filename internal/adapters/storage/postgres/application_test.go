package postgresstorage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/application"
	applicationattempt "hh-ai-responder/internal/applicationattempt"
	"hh-ai-responder/internal/vacancy"
)

type applicationAttemptRowFunc func(...any) error

func (f applicationAttemptRowFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestFindBlockingAttemptNormalizesOnlyTypedNoRows(t *testing.T) {
	got, err := findBlockingAttempt(applicationAttemptRowFunc(func(...any) error {
		return pgx.ErrNoRows
	}))
	if !errors.Is(err, applicationattempt.ErrAttemptNotFound) || err == nil {
		t.Fatalf("no rows = attempt=%+v err=%v, want canonical absence", got, err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("driver sentinel leaked through absence contract: %v", err)
	}

	databaseErr := errors.New("database connection failed")
	got, err = findBlockingAttempt(applicationAttemptRowFunc(func(...any) error {
		return fmt.Errorf("query: %w", databaseErr)
	}))
	if !errors.Is(err, databaseErr) || errors.Is(err, applicationattempt.ErrAttemptNotFound) || got != (applicationattempt.Attempt{}) {
		t.Fatalf("database failure was not preserved: attempt=%+v err=%v", got, err)
	}
}

// This is an opt-in read-only check against the real PostgreSQL database. It
// intentionally does not apply migrations or insert a placeholder attempt.
func TestPostgresApplicationAttemptRepositoryEmptyTableIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM automatic_application_attempts`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("integration fixture must start empty, got %d attempts", before)
	}
	var vacancyID int
	if err := pool.QueryRow(ctx, `SELECT id FROM vacancies ORDER BY id LIMIT 1`).Scan(&vacancyID); err != nil {
		t.Fatal(err)
	}
	repository := NewApplicationAttemptRepository(pool)
	got, err := repository.FindBlocking(ctx, vacancyID)
	if !errors.Is(err, applicationattempt.ErrAttemptNotFound) || got != (applicationattempt.Attempt{}) {
		t.Fatalf("empty authority lookup = attempt=%+v err=%v", got, err)
	}
	if _, err := repository.GetByID(ctx, "missing-attempt-for-empty-store-check"); !errors.Is(err, applicationattempt.ErrAttemptNotFound) {
		t.Fatalf("explicit missing attempt = %v, want canonical not-found", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM automatic_application_attempts`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("read-only lookup changed attempt count: before=%d after=%d", before, after)
	}
}

type applicationScanner struct {
	values []interface{}
}

func (s applicationScanner) Scan(dest ...interface{}) error {
	if len(dest) != len(s.values) {
		return errors.New("unexpected application scan shape")
	}
	for i, value := range s.values {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Ptr || target.IsNil() {
			return errors.New("application scan target is not a pointer")
		}
		if value == nil {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		target.Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

func TestScanPostgresApplicationPreservesRelationsJSONBAndNanoseconds(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 11, 12, 123456789, time.UTC)
	conversationID := "conversation-1"
	match := `{"score":82,"confidence":0.75,"matched_skills":["Python"]}`
	metadata := `{"negotiation_id":"hh-1"}`
	evidence := `[{"method":"hh_negotiation_id","source":"hh","confidence":1,"reconciled_at":"2026-09-01T10:11:12.123456789Z"}]`
	got, err := scanPostgresApplication(applicationScanner{values: []interface{}{
		"application-1", "hh-1", 42, &conversationID, "Company", "Python", "https://hh.example/1",
		application.SourceHH, application.StatusApplied, "applied", pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Timestamptz{Time: at, Valid: true},
		pgtype.Int8{Int64: at.UnixNano(), Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true},
		application.FollowUpEligible, "notes", "waiting_employer_reply", []byte(match), []byte(metadata), true,
		vacancy.DataCompletenessPartial, []byte(evidence),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "application-1" || got.ExternalID != "hh-1" || got.VacancyID != 42 || got.ConversationID != conversationID || !got.CreatedAt.Equal(at) || !got.UpdatedAt.Equal(at) || got.MatchResult == nil || got.MatchResult.Score != 82 || got.HHMetadata["negotiation_id"] != "hh-1" || !got.Partial || got.DataCompleteness != vacancy.DataCompletenessPartial || len(got.ReconciliationEvidence) != 1 {
		t.Fatalf("application scan lost persisted values: %+v", got)
	}
}

func TestScanPostgresApplicationNullConversationAndEvent(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 11, 12, 123456789, time.UTC)
	got, err := scanPostgresApplication(applicationScanner{values: []interface{}{
		"application-1", "", 0, nil, "Company", "Python", "", application.SourceManual, application.StatusDiscovered, "",
		pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true},
		application.FollowUpNone, "", "", nil, nil, false, vacancy.DataCompleteness(""), nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != "" || got.MatchResult != nil || got.HHMetadata != nil || got.ReconciliationEvidence != nil {
		t.Fatalf("NULL application values were not preserved as zero/nil: %+v", got)
	}
	event, err := scanPostgresApplicationEvent(applicationScanner{values: []interface{}{
		"event-1", "application-1", pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true}, application.EventMessageReceived, []byte(`{"description":"received"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "event-1" || event.ApplicationID != "application-1" || event.Timestamp.UnixNano() != at.UnixNano() || event.Type != application.EventMessageReceived || event.Description != "received" {
		t.Fatalf("event scan changed persisted history: %+v", event)
	}
}

func TestMapPostgresApplicationErrorsPreservesSemanticCauses(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "duplicate external", constraint: "applications_external_id_unique", want: ErrDuplicateExternalID},
		{name: "duplicate local", constraint: "applications_pkey", want: ErrRepositoryConflict},
		{name: "missing application", constraint: "application_events_application_fk", want: ErrApplicationNotFound},
		{name: "missing vacancy", constraint: "applications_vacancy_fk", want: vacancy.ErrVacancyNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapPostgresApplicationError("application test", &pgconn.PgError{Code: "23505", ConstraintName: test.constraint})
			if test.constraint == "application_events_application_fk" || test.constraint == "applications_vacancy_fk" {
				got = mapPostgresApplicationError("application test", &pgconn.PgError{Code: "23503", ConstraintName: test.constraint})
			}
			if !errors.Is(got, test.want) {
				t.Fatalf("error=%v does not preserve %v", got, test.want)
			}
		})
	}
}
