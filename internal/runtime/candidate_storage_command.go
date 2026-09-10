package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
)

func runCandidateCommand(args []string, cfg Config, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: candidate migrate-postgres ... | status | semantic status | semantic reindex [--dry-run|--apply] | semantic search QUERY")
	}
	if args[0] == "semantic" {
		return runCandidateSemanticCommand(args[1:], cfg, out)
	}
	if args[0] == "status" {
		return runCandidateStatusCommand(args[1:], cfg, out)
	}
	if args[0] != "migrate-postgres" {
		return errors.New("usage: candidate migrate-postgres ... | status | semantic status | semantic reindex [--dry-run|--apply] | semantic search QUERY")
	}
	fs := flag.NewFlagSet("candidate-migrate-postgres", flag.ContinueOnError)
	fs.SetOutput(out)
	sourceDir := "."
	reportPath := ""
	databaseURL := cfg.DatabaseURL
	candidateID := "candidate-local"
	contacts := cfg.Contacts
	githubURL := cfg.GithubURL
	apply := false
	dryRun := true
	fs.StringVar(&sourceDir, "source-dir", sourceDir, "directory containing legacy candidate JSON")
	fs.StringVar(&reportPath, "report", reportPath, "write a machine-readable candidate migration report")
	fs.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	fs.StringVar(&candidateID, "candidate-id", candidateID, "stable canonical candidate ID")
	fs.StringVar(&contacts, "contacts", contacts, "legacy configured candidate contacts")
	fs.StringVar(&githubURL, "github-url", githubURL, "legacy configured candidate GitHub URL")
	fs.BoolVar(&apply, "apply", false, "explicitly write the canonical candidate")
	fs.BoolVar(&dryRun, "dry-run", true, "build and report the plan without writes")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments after candidate migration flags")
	}
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("DATABASE_URL is required for candidate PostgreSQL migration")
	}
	if strings.TrimSpace(candidateID) == "" {
		return errors.New("candidate-id must not be empty")
	}
	_ = dryRun // retained to make the read-only default explicit

	source, err := LoadCandidateMigrationSource(sourceDir, candidateID, contacts, githubURL)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	defer pool.Close()
	// Dry-run does not create or modify destination schema. Apply performs the
	// pending versioned migrations before the candidate transaction.
	if apply {
		if err := ApplyPostgresMigrations(ctx, pool); err != nil {
			return err
		}
	}
	// Read the destination without narrowing to the requested ID so an existing
	// different candidate is classified as a conflict instead of being hidden.
	repo := NewPostgresCandidateRepository(pool)
	var destination *Candidate
	if current, err := repo.CurrentCandidate(ctx); err == nil {
		destination = &current
	} else if !errors.Is(err, ErrCandidateNotFound) && !isMissingCandidateSchema(err) {
		return fmt.Errorf("read PostgreSQL candidate destination: %w", err)
	}
	plan, err := BuildCandidateMigrationPlan(source, destination)
	if err != nil {
		return err
	}
	plan.Report.Mode = "dry_run"
	if apply {
		plan.Report.Mode = "apply"
	}
	if err := writeCandidateMigrationReport(reportPath, plan.Report); err != nil {
		return err
	}
	if err := printCandidateMigrationReport(out, plan.Report); err != nil {
		return err
	}
	if !apply {
		return nil
	}
	if !plan.Report.SafeToApply {
		return errors.New("candidate migration apply refused because the plan contains critical diagnostics")
	}
	if err := ApplyCandidateMigration(ctx, NewPostgresCandidateStoreForID(pool, candidateID), plan); err != nil {
		plan.Report.Error = err.Error()
		_ = writeCandidateMigrationReport(reportPath, plan.Report)
		return fmt.Errorf("apply candidate PostgreSQL migration: %w", err)
	}
	verification, err := VerifyCandidateMigration(ctx, repo, source.Candidate)
	plan.Report.Applied = true
	plan.Report.Verification = &verification
	plan.Report.CompletedAt = nowUTC()
	if err != nil {
		plan.Report.Error = err.Error()
		_ = writeCandidateMigrationReport(reportPath, plan.Report)
		return err
	}
	if err := writeCandidateMigrationReport(reportPath, plan.Report); err != nil {
		return err
	}
	return printCandidateMigrationReport(out, plan.Report)
}

func runCandidateSemanticCommand(args []string, cfg Config, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: candidate semantic status | reindex [--dry-run|--apply] | search QUERY")
	}
	fs := flag.NewFlagSet("candidate-semantic", flag.ContinueOnError)
	fs.SetOutput(out)
	databaseURL, candidateID := cfg.DatabaseURL, firstNonEmpty(cfg.CandidateID, "candidate-local")
	apply, dryRun := false, true
	limit := 5
	var minScore float64
	var minScoreSet bool
	fs.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	fs.StringVar(&candidateID, "candidate-id", candidateID, "stable canonical candidate ID")
	fs.BoolVar(&apply, "apply", false, "explicitly apply semantic index changes")
	fs.BoolVar(&dryRun, "dry-run", true, "show the semantic index plan without writes")
	fs.IntVar(&limit, "limit", limit, "maximum semantic results")
	fs.Func("min-score", "minimum cosine similarity score", func(value string) error {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		minScore, minScoreSet = parsed, true
		return nil
	})
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("DATABASE_URL is required for candidate semantic commands")
	}
	if strings.TrimSpace(candidateID) == "" {
		return errors.New("candidate-id must not be empty")
	}
	ctx := context.Background()
	p, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	defer p.Close()

	subcommand := args[0]
	var provider EmbeddingProvider
	if strings.TrimSpace(cfg.EmbeddingProvider) != "" {
		provider, err = configuredEmbeddingProvider(cfg)
		if err != nil {
			return err
		}
	}
	if subcommand == "status" {
		if fs.NArg() != 0 {
			return errors.New("unexpected arguments after candidate semantic status flags")
		}
		return printCandidateSemanticStatus(ctx, p, candidateID, cfg, provider, out)
	}
	repo := NewPostgresCandidateRepositoryForID(p, candidateID)
	candidate, err := repo.CurrentCandidate(ctx)
	if err != nil {
		return fmt.Errorf("read canonical candidate: %w", err)
	}
	semanticRepo := NewPostgresCandidateSemanticRepository(p)
	service := NewCandidateSemanticIndexService(semanticRepo, provider)
	switch subcommand {
	case "reindex":
		if fs.NArg() != 0 {
			return errors.New("unexpected arguments after candidate semantic reindex flags")
		}
		if apply {
			if err := ApplyPostgresMigrations(ctx, p); err != nil {
				return fmt.Errorf("apply semantic PostgreSQL migration: %w", err)
			}
		}
		report, err := service.Reindex(ctx, candidate, apply)
		if err != nil {
			return err
		}
		return printCandidateSemanticIndexReport(report, out)
	case "search":
		query := strings.TrimSpace(strings.Join(fs.Args(), " "))
		if query == "" {
			return errors.New("semantic search query is required")
		}
		if limit <= 0 {
			return errors.New("semantic search limit must be positive")
		}
		var threshold *float64
		if minScoreSet {
			threshold = &minScore
		}
		search := NewCandidateSemanticSearchService(semanticRepo, provider, repo)
		results, err := search.Search(ctx, CandidateSemanticQuery{CandidateID: candidateID, Query: query, Limit: limit, MinScore: threshold})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Query: %s\n", query)
		for i, result := range results {
			fmt.Fprintf(out, "%d. %s:%s\n   score: %.4f\n   %q\n", i+1, result.EntityType, result.EntityID, result.Score, result.Title)
		}
		if len(results) == 0 {
			fmt.Fprintln(out, "No semantic results.")
		}
		return nil
	default:
		return errors.New("usage: candidate semantic status | reindex [--dry-run|--apply] | search QUERY")
	}
}

func printCandidateSemanticStatus(ctx context.Context, pool *pgxpool.Pool, candidateID string, cfg Config, provider EmbeddingProvider, out io.Writer) error {
	repo := NewPostgresCandidateRepositoryForID(pool, candidateID)
	candidate, err := repo.CurrentCandidate(ctx)
	if err != nil {
		return err
	}
	drafts, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		return err
	}
	documents, err := NewPostgresCandidateSemanticRepository(pool).ListDocuments(ctx, candidateID)
	if err != nil {
		return err
	}
	byKey := map[string]CandidateSemanticDocument{}
	for _, document := range documents {
		byKey[semanticDocumentKey(candidateID, document.EntityType, document.EntityID)] = document
	}
	type semanticStatusCount struct {
		eligible, indexed, stale, ineligible int
	}
	counts := map[CandidateSemanticEntityType]semanticStatusCount{}
	for _, draft := range drafts {
		key := semanticDocumentKey(candidateID, draft.EntityType, draft.EntityID)
		value := counts[draft.EntityType]
		if !draft.Eligible {
			value.ineligible++
		} else {
			value.eligible++
			old, ok := byKey[key]
			if !ok {
				value.stale++
			} else {
				value.indexed++
				if old.ContentHash != draft.ContentHash {
					value.stale++
				}
			}
		}
		counts[draft.EntityType] = value
	}
	fmt.Fprintln(out, "Semantic index:")
	for _, entityType := range []CandidateSemanticEntityType{CandidateSemanticEntityStory, CandidateSemanticEntityProject, CandidateSemanticEntityAchievement} {
		value := counts[entityType]
		fmt.Fprintf(out, "%s: eligible=%d indexed=%d stale=%d\n", entityType, value.eligible, value.indexed, value.stale)
	}
	status := "DISABLED"
	model := "not indexed"
	dimensions := 0
	spaceID := ""
	if provider != nil {
		contract, contractErr := ports.EmbeddingContractFor(provider)
		if contractErr != nil {
			return contractErr
		}
		model, dimensions, spaceID = contract.Model, contract.Dimensions, contract.SpaceID
		status = "READY"
		if hasSemanticStatusStale(drafts, byKey, candidateID, contract) {
			status = "REINDEX_REQUIRED"
		}
	}
	_ = cfg
	fmt.Fprintf(out, "Embedding provider: %s\nEmbedding model: %s\nEmbedding dimensions: %d\nEmbedding space: %s\nIndex status: %s\n", firstNonEmpty(cfg.EmbeddingProvider, "disabled"), model, dimensions, firstNonEmpty(spaceID, "not configured"), status)
	return nil
}

func hasSemanticStatusStale(drafts []CandidateSemanticDocumentDraft, documents map[string]CandidateSemanticDocument, candidateID string, contract semantic.EmbeddingContract) bool {
	seen := map[string]bool{}
	for _, draft := range drafts {
		key := semanticDocumentKey(candidateID, draft.EntityType, draft.EntityID)
		seen[key] = true
		old, ok := documents[key]
		if !draft.Eligible && ok {
			return true
		}
		if draft.Eligible && (!ok || old.ContentHash != draft.ContentHash || old.EmbeddingProvider != contract.Provider || old.EmbeddingModel != contract.Model || old.EmbeddingDimensions != contract.Dimensions || old.EmbeddingSpaceID != contract.SpaceID) {
			return true
		}
	}
	for key := range documents {
		if !seen[key] {
			return true
		}
	}
	for _, document := range documents {
		if document.EmbeddingProvider != contract.Provider || document.EmbeddingModel != contract.Model || document.EmbeddingDimensions != contract.Dimensions || document.EmbeddingSpaceID != contract.SpaceID {
			return true
		}
	}
	return false
}

func printCandidateSemanticIndexReport(report CandidateSemanticIndexReport, out io.Writer) error {
	fmt.Fprintf(out, "Semantic reindex: new=%d changed=%d unchanged=%d stale=%d ineligible=%d applied=%t\n", report.New, report.Changed, report.Unchanged, report.Stale, report.Ineligible, report.Applied)
	return nil
}

func runCandidateStatusCommand(args []string, cfg Config, out io.Writer) error {
	fs := flag.NewFlagSet("candidate-status", flag.ContinueOnError)
	fs.SetOutput(out)
	databaseURL, candidateID := cfg.DatabaseURL, firstNonEmpty(cfg.CandidateID, "candidate-local")
	fs.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	fs.StringVar(&candidateID, "candidate-id", candidateID, "stable canonical candidate ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments after candidate status flags")
	}
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("DATABASE_URL is required for candidate status")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := NewPostgresCandidateRepositoryForID(pool, candidateID)
	candidate, err := repo.CurrentCandidate(ctx)
	if err != nil {
		return err
	}
	pendingQuestions, pendingProposals := 0, 0
	for _, unknown := range candidate.Unknowns {
		if unknown.Status == CandidateUnknownNeedsConfirmation {
			pendingQuestions++
		}
	}
	for _, proposal := range candidate.Proposals {
		if proposal.Status == KnowledgeProposalPending {
			pendingProposals++
		}
	}
	return json.NewEncoder(out).Encode(map[string]any{"candidate_storage": "postgres", "candidate_id": candidate.ID, "schema": "ready", "candidate_version": candidate.Version, "legacy_json": "compatibility_only", "pending_knowledge_questions": pendingQuestions, "pending_proposals": pendingProposals})
}

func isMissingCandidateSchema(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

func nowUTC() time.Time { return time.Now().UTC() }
