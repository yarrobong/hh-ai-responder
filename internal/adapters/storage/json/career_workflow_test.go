package jsonstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func TestCareerWorkflowJSONUpsertsItemsAndPreparationsIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career-workflow.json")
	store := NewCareerWorkflowRepository(path)
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	run := careeragent.NewAgentRun("run-1", careeragent.AgentRunStageCareerAgent, started)
	if err := store.StartRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	item := careeragent.AgentRunItem{ID: "item-1", RunID: run.ID, VacancyID: 42, Stage: careeragent.AgentRunStageAnalysis, Status: careeragent.AgentRunItemStatusCompleted, CreatedAt: started}
	if err := store.UpsertRunItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	item.DecisionCode = "MATCH"
	if err := store.UpsertRunItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	preparation := jsonPreparationFixture(started)
	if err := store.UpsertPreparation(context.Background(), preparation); err != nil {
		t.Fatal(err)
	}
	preparation.UpdatedAt = started.Add(time.Second)
	if err := store.UpsertPreparation(context.Background(), preparation); err != nil {
		t.Fatal(err)
	}

	preparations, err := store.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(preparations) != 1 || preparations[0].UpdatedAt != preparation.UpdatedAt || preparations[0].Status != careeragent.PreparationStatusReady {
		t.Fatalf("preparations=%+v", preparations)
	}
}

func TestCareerWorkflowJSONRejectsMalformedStoredEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career-workflow.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"runs":[],"items":[{"id":"item","run_id":"run","vacancy_id":42,"stage":"analysis","status":"completed","evidence_json":{"broken":},"created_at":"2026-09-22T10:00:00Z"}],"preparations":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewCareerWorkflowRepository(path)
	if err := store.Load(); err == nil {
		t.Fatal("malformed stored evidence was accepted")
	}
}

func TestCareerWorkflowJSONRecoversInterruptedRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career-workflow.json")
	store := NewCareerWorkflowRepository(path)
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	if err := store.StartRun(context.Background(), careeragent.NewAgentRun("run-interrupted", careeragent.AgentRunStageCareerAgent, started)); err != nil {
		t.Fatal(err)
	}
	recoveredAt := started.Add(2 * time.Minute)
	if err := store.RecoverInterruptedRuns(context.Background(), recoveredAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(context.Background(), "run-interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != careeragent.AgentRunStatusFailed || run.ResultCode != careeragent.AgentRunResultInterrupted {
		t.Fatalf("run=%+v", run)
	}
}

func TestCareerWorkflowJSONListsRunsAndPreparationsDeterministically(t *testing.T) {
	store := NewCareerWorkflowRepository(filepath.Join(t.TempDir(), "career-workflow.json"))
	base := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	for _, run := range []careeragent.AgentRun{
		careeragent.NewAgentRun("run-b", careeragent.AgentRunStageCareerAgent, base),
		careeragent.NewAgentRun("run-a", careeragent.AgentRunStageCareerAgent, base),
		careeragent.NewAgentRun("run-c", careeragent.AgentRunStageCareerAgent, base.Add(time.Minute)),
	} {
		if err := store.StartRun(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := store.ListRuns(context.Background(), careeragent.RunQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{runs[0].ID, runs[1].ID, runs[2].ID}
	if strings.Join(got, ",") != "run-c,run-a,run-b" {
		t.Fatalf("run order=%v", got)
	}
}

func jsonPreparationFixture(at time.Time) careeragent.ApplicationPreparation {
	letter := "Known candidate evidence only."
	hash := sha256.Sum256([]byte(letter))
	return careeragent.ApplicationPreparation{
		ID: "prep-1", VacancyID: 42, ResumeID: "resume-1", CandidateID: "candidate-1", CandidateVersion: 1,
		CandidateSnapshotHash: "candidate-hash", RouteStatus: careeragent.ResumeRouteMatch,
		CoverLetter: letter, CoverLetterHash: hex.EncodeToString(hash[:]), InputFingerprint: strings.Repeat("a", 64),
		Status: careeragent.PreparationStatusReady, CreatedAt: at, UpdatedAt: at,
	}
}
