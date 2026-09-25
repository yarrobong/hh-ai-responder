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

func TestCommunicationAgentRunItemUsesStableTargetIdentity(t *testing.T) {
	item := AgentRunItem{ID: "item-communication", RunID: "run-1", TargetType: "communication_work_item", TargetID: "work-1", ConversationID: "conversation-1", Stage: AgentRunStageCommunication, Status: AgentRunItemStatusReviewRequired, Evidence: []byte(`{"type":"INTERVIEW","requires_review":true}`), CreatedAt: time.Now().UTC()}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	if item.TargetKey() != "communication_work_item:work-1" {
		t.Fatalf("unexpected target key: %q", item.TargetKey())
	}
	item.NormalizeTarget()
	if item.TargetType != "communication_work_item" || item.TargetID != "work-1" {
		t.Fatal("normalization overwrote communication target")
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

func TestApplicationPreparationAllowsSafelyOmittedOptionalCoverLetter(t *testing.T) {
	preparation := validPreparationFixture()
	preparation.CoverLetter = ""
	preparation.CoverLetterHash = preparation.ContentHash()
	if preparation.CoverLetterHash != contentHash("") {
		t.Fatalf("empty optional letter did not receive deterministic empty-content hash: %q", preparation.CoverLetterHash)
	}
	if err := preparation.Validate(); err != nil {
		t.Fatalf("optional no-letter preparation rejected: %v", err)
	}
}

func TestApplicationPreparationHashOwnershipIsDeterministicForFinalContent(t *testing.T) {
	first := validPreparationFixture()
	first.CoverLetter = "Final validated letter."
	first.CoverLetterHash = first.ContentHash()
	first.InputFingerprint = PreparationInputFingerprint(first)
	second := first
	second.CoverLetterHash = second.ContentHash()
	second.InputFingerprint = PreparationInputFingerprint(second)
	if first.CoverLetterHash != second.CoverLetterHash || first.InputFingerprint != second.InputFingerprint {
		t.Fatalf("identical final content was not deterministic: first=%+v second=%+v", first, second)
	}
	second.CoverLetter = "Transformed final validated letter."
	second.CoverLetterHash = second.ContentHash()
	second.InputFingerprint = PreparationInputFingerprint(second)
	if first.CoverLetterHash == second.CoverLetterHash || first.InputFingerprint == second.InputFingerprint {
		t.Fatal("content transformation did not change canonical hashes")
	}
}

func TestPreparationInputFingerprintBindsBrowserResumeHash(t *testing.T) {
	first := validPreparationFixture()
	first.BrowserResumeHash = "browser-hash-a"
	second := first
	if got, want := PreparationInputFingerprint(first), PreparationInputFingerprint(second); got != want {
		t.Fatalf("same browser hash is not deterministic: %q != %q", got, want)
	}
	second.BrowserResumeHash = "browser-hash-b"
	if PreparationInputFingerprint(first) == PreparationInputFingerprint(second) {
		t.Fatal("changed browser resume hash did not invalidate preparation fingerprint")
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
