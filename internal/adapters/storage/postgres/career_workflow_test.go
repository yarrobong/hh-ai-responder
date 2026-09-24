package postgresstorage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/careeragent"
)

type careerWorkflowScanner struct {
	values []any
}

func (s careerWorkflowScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return errors.New("unexpected career workflow scan shape")
	}
	for index, value := range s.values {
		switch target := dest[index].(type) {
		case *string:
			*target = value.(string)
		case *time.Time:
			*target = value.(time.Time)
		case *pgtype.Timestamptz:
			*target = value.(pgtype.Timestamptz)
		case *pgtype.Text:
			*target = value.(pgtype.Text)
		case *pgtype.Float8:
			*target = value.(pgtype.Float8)
		case *int:
			*target = value.(int)
		case *[]byte:
			if value != nil {
				*target = value.([]byte)
			}
		default:
			return errors.New("unsupported career workflow scan target")
		}
	}
	return nil
}

func TestScanCareerAgentRunPreservesRedactedTerminalState(t *testing.T) {
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	confidence := 0.8
	run, err := scanCareerAgentRun(careerWorkflowScanner{values: []any{
		"run-1", "career_agent", "career_agent", "failed", started,
		pgtype.Timestamptz{Time: finished, Valid: true}, pgtype.Text{String: "failed", Valid: true}, pgtype.Text{String: "interrupted run", Valid: true}, nil,
		pgtype.Text{String: "INTERRUPTED", Valid: true},
		pgtype.Text{String: "run interrupted before completion", Valid: true}, pgtype.Float8{Float64: confidence, Valid: true}, started,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != careeragent.AgentRunStatusFailed || run.ResultCode != careeragent.AgentRunResultFailed || run.ErrorCode != "INTERRUPTED" || run.FinishedAt == nil {
		t.Fatalf("run=%+v", run)
	}
}

func TestPostgresCareerWorkflowRepositoryListIntegration(t *testing.T) {
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
	var table *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('agent_runs')::text`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table == nil {
		t.Skip("career workflow migration is not installed in POSTGRES_TEST_DATABASE_URL")
	}
	repository := NewCareerWorkflowRepository(pool)
	if _, err := repository.ListRuns(ctx, careeragent.RunQuery{Limit: 5}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}
