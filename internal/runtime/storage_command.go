package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// runStorageCommand is intentionally separate from application startup and
// from backend selection. STORAGE_BACKEND is not used as the migration
// source: this command always reads legacy JSON and writes only PostgreSQL.
func runStorageCommand(args []string, cfg Config, out io.Writer) error {
	if len(args) == 0 || args[0] != "migrate-postgres" {
		return errors.New("usage: storage migrate-postgres [--dry-run] [--apply] [--source-dir DIR] [--report FILE]")
	}
	wd, err := osGetwd()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("migrate-postgres", flag.ContinueOnError)
	fs.SetOutput(out)
	sourceDir := wd
	reportPath := ""
	databaseURL := cfg.DatabaseURL
	apply := false
	dryRun := true
	fs.StringVar(&sourceDir, "source-dir", sourceDir, "directory containing legacy JSON career stores")
	fs.StringVar(&reportPath, "report", "", "write a machine-readable migration report to this file")
	fs.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	fs.BoolVar(&apply, "apply", false, "explicitly apply the safe migration plan")
	fs.BoolVar(&dryRun, "dry-run", true, "build and report the plan without data writes")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments after storage migrate-postgres flags")
	}
	if err := ensureMigrationReportIsNotSource(reportPath, sourceDir); err != nil {
		return err
	}
	// --apply is the only write authorization. This keeps even
	// --dry-run=false read-only unless the user explicitly chose --apply.
	mode := "dry_run"
	if apply {
		mode = "apply"
	}
	_ = dryRun // retained as an explicit, documented safety flag

	started := time.Now().UTC()
	source, err := LoadCareerMigrationSource(sourceDir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("DATABASE_URL is required for PostgreSQL migration")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	defer pool.Close()
	// Dry-run must not initialize or alter the destination schema. The
	// PostgreSQL foundation is expected to exist already; apply may finish any
	// pending foundation migrations before opening the data transaction.
	if apply {
		if err := ApplyPostgresMigrations(ctx, pool); err != nil {
			return err
		}
	}
	career := CareerRepositories{
		Vacancies:     NewPostgresVacancyRepository(pool),
		Applications:  NewPostgresApplicationRepository(pool),
		Conversations: NewPostgresConversationRepository(pool),
		Postgres:      NewPostgresCareerStore(pool),
	}
	destination, err := readCareerMigrationDestination(ctx, career)
	if err != nil {
		return fmt.Errorf("read PostgreSQL destination: %w", err)
	}
	plan, err := BuildCareerMigrationPlan(source, destination)
	if err != nil {
		return err
	}
	plan.Report.Mode = mode
	plan.Report.StartedAt = started
	plan.Report.CompletedAt = time.Now().UTC()
	if err := writeCareerMigrationReport(reportPath, plan.Report); err != nil {
		return err
	}
	if err := printCareerMigrationReport(out, plan.Report); err != nil {
		return err
	}
	if !apply {
		return nil
	}
	if !plan.Report.SafeToApply {
		return errors.New("migration apply refused because the plan contains unresolved conflicts")
	}
	if err := ApplyCareerMigration(ctx, career.Postgres, plan); err != nil {
		plan.Report.Error = err.Error()
		plan.Report.CompletedAt = time.Now().UTC()
		_ = writeCareerMigrationReport(reportPath, plan.Report)
		return fmt.Errorf("apply PostgreSQL migration: %w", err)
	}
	verification, err := verifyCareerMigration(ctx, career, source)
	plan.Report.Applied = true
	plan.Report.Verification = &verification
	plan.Report.CompletedAt = time.Now().UTC()
	if err != nil {
		plan.Report.Error = err.Error()
		_ = writeCareerMigrationReport(reportPath, plan.Report)
		return err
	}
	if err := writeCareerMigrationReport(reportPath, plan.Report); err != nil {
		return err
	}
	return printCareerMigrationReport(out, plan.Report)
}

func ensureMigrationReportIsNotSource(reportPath, sourceDir string) error {
	if strings.TrimSpace(reportPath) == "" {
		return nil
	}
	reportAbs, err := filepath.Abs(reportPath)
	if err != nil {
		return err
	}
	sourceAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	for _, name := range []string{VacanciesFilename, JobApplicationsFilename, EmployerConversationsFilename} {
		if reportAbs == filepath.Join(sourceAbs, name) {
			return fmt.Errorf("migration report must not overwrite source file %s", name)
		}
	}
	return nil
}
