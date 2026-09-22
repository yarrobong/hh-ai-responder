package careeragent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAgentRunItemRequiresStableIdentityAndBoundedEvidence(t *testing.T) {
	item := AgentRunItem{
		ID:        "run-item-1",
		RunID:     "run-1",
		VacancyID: 42,
		Stage:     AgentRunStageAnalysis,
		Status:    AgentRunItemStatusCompleted,
		Evidence:  json.RawMessage(`{"reason":"deterministic match"}`),
		CreatedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}

	item.Evidence = json.RawMessage(`{"api_key":"secret"}`)
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("secret evidence accepted: %v", err)
	}
}

func TestApplicationPreparationRequiresExactCoverLetterHash(t *testing.T) {
	letter := "Здравствуйте! Готов обсудить интеграции."
	hash := sha256.Sum256([]byte(letter))
	preparation := ApplicationPreparation{
		ID:                    "prep-1",
		VacancyID:             42,
		ResumeID:              "resume-support",
		CandidateID:           "candidate-local",
		CandidateVersion:      3,
		CandidateSnapshotHash: "candidate-snapshot-hash",
		RouteStatus:           ResumeRouteMatch,
		CoverLetter:           letter,
		CoverLetterHash:       hex.EncodeToString(hash[:]),
		InputFingerprint:      strings.Repeat("a", 64),
		Status:                PreparationStatusReady,
		CreatedAt:             time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt:             time.Date(2026, 9, 22, 10, 0, 1, 0, time.UTC),
	}
	if err := preparation.Validate(); err != nil {
		t.Fatal(err)
	}

	preparation.CoverLetter += " Изменение."
	if err := preparation.Validate(); err == nil || !errors.Is(err, ErrPreparationContentHash) {
		t.Fatalf("changed cover letter was accepted: %v", err)
	}
}

func TestApplicationPreparationRejectsMalformedEvidenceAndTracksStaleState(t *testing.T) {
	preparation := validPreparationFixture()
	preparation.Evidence = json.RawMessage(`{"unclosed":`)
	if err := preparation.Validate(); err == nil {
		t.Fatal("malformed evidence was accepted")
	}

	preparation = validPreparationFixture()
	preparation.Status = PreparationStatusStale
	preparation.StaleReason = "candidate knowledge changed"
	if err := preparation.Validate(); err != nil {
		t.Fatal(err)
	}
	if !preparation.IsStale() {
		t.Fatal("stale preparation did not report stale state")
	}
}

func TestAgentRunRecoveryCannotClaimSuccess(t *testing.T) {
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	run := NewAgentRun("run-interrupted", AgentRunStageCareerAgent, started)
	if err := run.RecoverInterrupted(started.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if run.Status != AgentRunStatusFailed || run.ResultCode != AgentRunResultInterrupted || run.FinishedAt == nil {
		t.Fatalf("run recovery=%+v", run)
	}
	if err := run.Finish(AgentRunStatusCompleted, "success", started.Add(2*time.Minute), nil); err == nil {
		t.Fatal("recovered run was allowed to become successful")
	}
}

func validPreparationFixture() ApplicationPreparation {
	letter := "Known candidate evidence only."
	hash := sha256.Sum256([]byte(letter))
	return ApplicationPreparation{
		ID:                    "prep-1",
		VacancyID:             42,
		ResumeID:              "resume-1",
		CandidateID:           "candidate-1",
		CandidateVersion:      1,
		CandidateSnapshotHash: "candidate-hash",
		RouteStatus:           ResumeRouteReviewRequired,
		Evidence:              json.RawMessage(`{"route":"ambiguous"}`),
		CoverLetter:           letter,
		CoverLetterHash:       hex.EncodeToString(hash[:]),
		InputFingerprint:      strings.Repeat("b", 64),
		Status:                PreparationStatusReviewRequired,
		CreatedAt:             time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt:             time.Date(2026, 9, 22, 10, 0, 1, 0, time.UTC),
	}
}
