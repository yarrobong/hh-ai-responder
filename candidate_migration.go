package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const candidateMigrationConflict MigrationStatus = "conflict"

type CandidateMigrationSource struct {
	Candidate   Candidate
	Diagnostics CanonicalCandidateDiagnostics
}

type CandidateMigrationReport struct {
	Mode                  string                          `json:"mode"`
	SourceFingerprint     string                          `json:"source_fingerprint"`
	SourceFilesUnmodified bool                            `json:"source_files_unmodified"`
	SafeToApply           bool                            `json:"safe_to_apply"`
	Applied               bool                            `json:"applied"`
	Status                MigrationStatus                 `json:"status"`
	Warnings              []MigrationWarning              `json:"warnings,omitempty"`
	Conflicts             []MigrationConflict             `json:"conflicts,omitempty"`
	Diagnostics           CanonicalCandidateDiagnostics   `json:"diagnostics"`
	Verification          *CandidateMigrationVerification `json:"verification,omitempty"`
	Error                 string                          `json:"error,omitempty"`
	StartedAt             time.Time                       `json:"started_at"`
	CompletedAt           time.Time                       `json:"completed_at,omitempty"`
}

type CandidateMigrationVerification struct {
	CandidateID string `json:"candidate_id"`
	Equivalent  bool   `json:"equivalent"`
}

type CandidateMigrationPlan struct {
	Source CandidateMigrationSource
	Report CandidateMigrationReport
}

func LoadCandidateMigrationSource(dir, candidateID, contacts, githubURL string) (CandidateMigrationSource, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	profilePath := filepath.Join(dir, "candidate_profile.json")
	kb := NewCandidateKnowledgeBase(profilePath)
	if err := kb.Load(); err != nil {
		return CandidateMigrationSource{}, fmt.Errorf("load candidate legacy source: %w", err)
	}
	stories, err := LoadCandidateStories(filepath.Join(dir, "candidate_stories.json"))
	if err != nil {
		return CandidateMigrationSource{}, err
	}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{
		Profile: kb.Profile, Knowledge: *kb, Stories: stories.Stories,
		CandidateID: candidateID, Contacts: contacts, GitHubURL: githubURL,
	})
	if err != nil {
		return CandidateMigrationSource{}, err
	}
	return CandidateMigrationSource{Candidate: candidate, Diagnostics: diagnostics}, nil
}

func BuildCandidateMigrationPlan(source CandidateMigrationSource, destination *Candidate) (CandidateMigrationPlan, error) {
	fingerprint, err := candidateFingerprint(source.Candidate)
	if err != nil {
		return CandidateMigrationPlan{}, fmt.Errorf("fingerprint candidate source: %w", err)
	}
	report := CandidateMigrationReport{
		Mode: "dry_run", SourceFingerprint: fingerprint, SourceFilesUnmodified: true,
		SafeToApply: true, Status: MigrationNew, Diagnostics: source.Diagnostics,
		Warnings: []MigrationWarning{}, Conflicts: []MigrationConflict{}, StartedAt: time.Now().UTC(),
	}
	for _, value := range source.Diagnostics.LossyMappings {
		report.Warnings = append(report.Warnings, MigrationWarning{Entity: "candidate", ID: source.Candidate.ID, Field: "mapping", Detail: value})
	}
	critical := func(field, detail string) {
		report.Conflicts = append(report.Conflicts, MigrationConflict{Entity: "candidate", ID: source.Candidate.ID, Field: field, Severity: migrationCritical, Source: detail})
		report.SafeToApply = false
	}
	for _, value := range source.Diagnostics.Conflicts {
		critical("conflict", value)
	}
	for _, value := range source.Diagnostics.AmbiguousAliases {
		critical("ambiguous_alias", value)
	}
	for _, value := range source.Diagnostics.UnresolvedReferences {
		critical("unresolved_reference", value)
	}
	for _, value := range source.Diagnostics.UnsupportedStoryRefs {
		critical("unsupported_story_reference", value)
	}
	if destination != nil {
		if destination.ID != source.Candidate.ID {
			critical("identity", "destination contains a different candidate identity")
		} else if reflectCandidateEqual(*destination, source.Candidate) {
			report.Status = MigrationAlreadyPresent
		} else {
			report.Status = candidateMigrationConflict
			critical("aggregate", "destination candidate differs; overwrite is not supported")
		}
	}
	report.CompletedAt = time.Now().UTC()
	return CandidateMigrationPlan{Source: source, Report: report}, nil
}

func ApplyCandidateMigration(ctx context.Context, store *PostgresCandidateStore, plan CandidateMigrationPlan) error {
	if store == nil {
		return errors.New("postgres candidate store is required")
	}
	if !plan.Report.SafeToApply {
		return errors.New("candidate migration has critical diagnostics; apply blocked")
	}
	if plan.Report.Status == MigrationAlreadyPresent {
		return nil
	}
	return store.WithTx(ctx, func(tx CandidateTx) error {
		repo := tx.Repository()
		return repo.ImportCandidate(ctx, plan.Source.Candidate)
	})
}

func VerifyCandidateMigration(ctx context.Context, repo CandidateRepository, source Candidate) (CandidateMigrationVerification, error) {
	got, err := repo.CurrentCandidate(ctx)
	if err != nil {
		return CandidateMigrationVerification{}, err
	}
	verification := CandidateMigrationVerification{CandidateID: source.ID, Equivalent: reflectCandidateEqual(got, source)}
	if !verification.Equivalent {
		return verification, errors.New("post-migration candidate verification found a semantic difference")
	}
	return verification, nil
}

func reflectCandidateEqual(left, right Candidate) bool {
	// time.Time carries an implementation-level location pointer for zero and
	// UTC values. Canonical JSON is the domain representation used by the
	// migration fingerprint, so compare that representation rather than Go
	// pointer identity.
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftRaw) == string(rightRaw)
}

func writeCandidateMigrationReport(path string, report CandidateMigrationReport) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write candidate migration report: %w", err)
	}
	return nil
}

func printCandidateMigrationReport(out io.Writer, report CandidateMigrationReport) error {
	_, err := fmt.Fprintf(out, "Candidate migration plan: %s\nSource fingerprint: %s\nStatus: %s\nSafe to apply: %t\n", report.Mode, report.SourceFingerprint, report.Status, report.SafeToApply)
	if err != nil {
		return err
	}
	if len(report.Conflicts) > 0 {
		_, err = fmt.Fprintf(out, "Critical conflicts: %d\n", len(report.Conflicts))
	}
	return err
}
