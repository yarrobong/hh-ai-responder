package runtime

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/careeragent"
)

func TestCareerWorkflowParityJSONNormalizedReplayAndOrdering(t *testing.T) {
	firstPath := t.TempDir() + "/first.json"
	secondPath := t.TempDir() + "/second.json"
	first := jsonstorage.NewCareerWorkflowRepository(firstPath)
	second := jsonstorage.NewCareerWorkflowRepository(secondPath)
	writeCareerWorkflowParityFixture(t, first)
	writeCareerWorkflowParityFixture(t, second)

	firstRuns, err := first.ListRuns(context.Background(), careeragent.RunQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	secondRuns, err := second.ListRuns(context.Background(), careeragent.RunQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstRuns, secondRuns) {
		t.Fatalf("run read models diverged:\nfirst=%+v\nsecond=%+v", firstRuns, secondRuns)
	}
	firstPreparations, err := first.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	secondPreparations, err := second.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstPreparations, secondPreparations) {
		t.Fatalf("preparation read models diverged:\nfirst=%+v\nsecond=%+v", firstPreparations, secondPreparations)
	}

	// Replaying the exact records must keep one item and one preparation in
	// each versioned store file; the JSON adapter is the compatibility model
	// used by the default runtime path.
	raw, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Items        []careeragent.AgentRunItem           `json:"items"`
		Preparations []careeragent.ApplicationPreparation `json:"preparations"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Items) != 1 || len(file.Preparations) != 1 {
		t.Fatalf("replay created duplicate records: %+v", file)
	}
}

func writeCareerWorkflowParityFixture(t *testing.T, store *jsonstorage.CareerWorkflowRepository) {
	t.Helper()
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	run := careeragent.NewAgentRun("parity-run", careeragent.AgentRunStageCareerAgent, now)
	if err := store.StartRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	item := careeragent.AgentRunItem{ID: "parity-run-vacancy-42", RunID: run.ID, VacancyID: 42, Stage: careeragent.AgentRunStageReview, Status: careeragent.AgentRunItemStatusReviewRequired, Evidence: []byte(`{"route":"review_required"}`), CreatedAt: now}
	if err := store.UpsertRunItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	letter := "Known candidate evidence only."
	preparation := careeragent.ApplicationPreparation{
		ID: "parity-preparation", VacancyID: 42, ResumeID: "resume-1", ResumeProviderID: "provider-1",
		CandidateID: "candidate-1", CandidateVersion: 1, CandidateSnapshotHash: "snapshot-1",
		RouteStatus: careeragent.ResumeRouteReviewRequired, Evidence: []byte(`{"route":"ambiguous"}`), CoverLetter: letter,
		Status: careeragent.PreparationStatusReviewRequired, CreatedAt: now, UpdatedAt: now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)
	if err := store.UpsertPreparation(context.Background(), preparation); err != nil {
		t.Fatal(err)
	}
	if err := run.Finish(careeragent.AgentRunStatusCompleted, "completed", now.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRunItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertPreparation(context.Background(), preparation); err != nil {
		t.Fatal(err)
	}
}
