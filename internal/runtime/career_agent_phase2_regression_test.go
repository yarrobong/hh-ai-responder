package runtime

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/careeragent"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

func TestCareerAgentPhase2DurableVerticalSliceAccountsPreparationAndTerminalItems(t *testing.T) {
	path := t.TempDir() + "/career-workflow.json"
	store := jsonstorage.NewCareerWorkflowRepository(path)
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	responder := &HHAIResponder{
		ctx:                         context.Background(),
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 4,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
	}
	report := CareerAgentRunReport{Run: careeragent.NewAgentRun("phase2-run-1", careeragent.AgentRunStageCareerAgent, started)}
	prepared := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID: 501, ResumeID: "resume-hash", ResumeTitle: "Python backend", CoverLetter: "Known candidate evidence only.",
		CandidateContext: candidatecontext.CandidateContext{MissingInformation: []candidatecontext.CandidateMissingInformation{{Question: "What is the confirmed Django experience?"}}},
	}}
	report.Vacancies = []CareerAgentVacancyResult{
		{VacancyID: 501, FinalDecision: string(VacancyReviewRequired), TerminalOutcome: TerminalReviewRequired, ResumeConfidence: "medium", ProcessedAt: started.Add(time.Second)},
		{VacancyID: 502, FinalDecision: string(VacancyReject), TerminalOutcome: TerminalDeterministicReject, ProcessedAt: started.Add(2 * time.Second)},
	}
	err := executeCareerAgentWorkflow(context.Background(), store, &report, func() error {
		return responder.persistCareerAgentPreparation(Vacancy{ID: 501}, ResumeItem{Hash: "resume-hash", ProviderID: "resume-provider"}, report.Vacancies[0], prepared)
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(context.Background(), report.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != careeragent.AgentRunStatusCompleted {
		t.Fatalf("run status=%s, want completed", run.Status)
	}
	items, err := store.ListRuns(context.Background(), careeragent.RunQuery{Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("runs=%+v err=%v", items, err)
	}
	preparations, err := store.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 10})
	if err != nil || len(preparations) != 1 || len(preparations[0].KnowledgeRequests) != 1 {
		t.Fatalf("preparations=%+v err=%v", preparations, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Items []careeragent.AgentRunItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Items) != 2 {
		t.Fatalf("terminal items=%+v", persisted.Items)
	}
	for _, item := range persisted.Items {
		if item.Status == careeragent.AgentRunItemStatusRunning {
			t.Fatalf("running item remained after completed run: %+v", item)
		}
	}
}
