package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

func TestCareerAgentRunReportCarriesTypedRunTelemetry(t *testing.T) {
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	run := careeragent.NewAgentRun("run-report-1", careeragent.AgentRunStageCareerAgent, started)
	finished := started.Add(time.Second)
	if err := run.Finish(careeragent.AgentRunStatusCompleted, "completed", finished, nil); err != nil {
		t.Fatal(err)
	}
	report := CareerAgentRunReport{Run: run}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"run"`) || !strings.Contains(string(raw), `"run-report-1"`) {
		t.Fatalf("run telemetry missing from report: %s", raw)
	}
}

type durableWorkflowStoreFixture struct {
	ports.CareerWorkflowStore
	events       []string
	started      []careeragent.AgentRun
	finished     []careeragent.AgentRun
	items        []careeragent.AgentRunItem
	preparations []careeragent.ApplicationPreparation
	recoverCalls int
}

func (s *durableWorkflowStoreFixture) StartRun(_ context.Context, run careeragent.AgentRun) error {
	s.events = append(s.events, "start")
	s.started = append(s.started, run)
	return nil
}

func (s *durableWorkflowStoreFixture) FinishRun(_ context.Context, run careeragent.AgentRun) error {
	s.events = append(s.events, "finish")
	s.finished = append(s.finished, run)
	return nil
}

func (s *durableWorkflowStoreFixture) UpsertRunItem(_ context.Context, item careeragent.AgentRunItem) error {
	s.events = append(s.events, "item")
	s.items = append(s.items, item)
	return nil
}

func (s *durableWorkflowStoreFixture) UpsertPreparation(_ context.Context, preparation careeragent.ApplicationPreparation) error {
	s.events = append(s.events, "preparation")
	s.preparations = append(s.preparations, preparation)
	return nil
}

func (s *durableWorkflowStoreFixture) RecoverInterruptedRuns(context.Context, time.Time) error {
	s.recoverCalls++
	return nil
}

func TestCareerAgentDurableLifecycleStartsBeforeProcessingAndAccountsTerminalVacancies(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	report := CareerAgentRunReport{
		Run: careeragent.NewAgentRun("durable-run-1", careeragent.AgentRunStageCareerAgent, started),
		Vacancies: []CareerAgentVacancyResult{
			{VacancyID: 101, FinalDecision: string(VacancyMatch), TerminalOutcome: TerminalAIMatch, ProcessedAt: started.Add(time.Second)},
			{VacancyID: 102, FinalDecision: string(VacancyReviewRequired), TerminalOutcome: TerminalReviewRequired, ProcessedAt: started.Add(2 * time.Second)},
		},
	}
	processed := false
	err := executeCareerAgentWorkflow(context.Background(), store, &report, func() error {
		if len(store.started) != 1 {
			t.Fatal("processing started before durable run was persisted")
		}
		processed = true
		return nil
	})
	if err != nil || !processed {
		t.Fatalf("durable lifecycle err=%v processed=%t", err, processed)
	}
	if len(store.items) != 2 || len(store.finished) != 1 || store.finished[0].Status == careeragent.AgentRunStatusRunning {
		t.Fatalf("durable records: starts=%d items=%d finishes=%d run=%+v", len(store.started), len(store.items), len(store.finished), store.finished)
	}
	if strings.Join(store.events, ",") != "start,item,item,finish" {
		t.Fatalf("durable event order=%v", store.events)
	}
}

func TestCareerAgentDurableLifecyclePersistsFailedRun(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	started := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	report := CareerAgentRunReport{Run: careeragent.NewAgentRun("durable-run-2", careeragent.AgentRunStageCareerAgent, started)}
	processErr := errors.New("provider unavailable")
	if err := executeCareerAgentWorkflow(context.Background(), store, &report, func() error { return processErr }); !errors.Is(err, processErr) {
		t.Fatalf("process error=%v, want %v", err, processErr)
	}
	if len(store.finished) != 1 || store.finished[0].Status != careeragent.AgentRunStatusFailed || store.finished[0].ResultCode != careeragent.AgentRunResultFailed {
		t.Fatalf("failed durable run=%+v", store.finished)
	}
}

func TestCareerAgentPreparationIsPersistedBeforeSubmissionDataLeavesProcessing(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
	}
	result := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID:        777,
		ResumeID:         "resume-1",
		ResumeTitle:      "Python backend",
		CoverLetter:      "Known candidate evidence only.",
		CandidateContext: candidatecontext.CandidateContext{MissingInformation: []candidatecontext.CandidateMissingInformation{{Question: "What is the confirmed Django experience?"}}},
	}}
	trace := CareerAgentVacancyResult{VacancyID: 777, SelectedResume: "resume-1", SelectedResumeTitle: "Python backend", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}
	if err := responder.persistCareerAgentPreparation(Vacancy{ID: 777}, ResumeItem{Hash: "resume-1", ProviderID: "provider-resume-1"}, trace, result); err != nil {
		t.Fatal(err)
	}
	if len(store.preparations) != 1 {
		t.Fatalf("preparations=%d, want 1", len(store.preparations))
	}
	preparation := store.preparations[0]
	if preparation.Status != careeragent.PreparationStatusReady || preparation.CoverLetterHash != preparation.ContentHash() || len(preparation.KnowledgeRequests) != 1 || preparation.ResumeProviderID != "provider-resume-1" {
		t.Fatalf("preparation=%+v", preparation)
	}
}

func TestRenderCareerAgentHumanReportRouteCategories(t *testing.T) {
	report := CareerAgentRunReport{
		Mode: "shadow",
		Summary: RunSummaryResult{
			RouteReasonCounts: map[string]int{
				"ROUTE_AMBIGUOUS":    3,
				"NO_SUITABLE_RESUME": 2,
				"ROLE_OUT_OF_SCOPE":  4,
				"ROUTE_LOW_EVIDENCE": 5,
			},
		},
		Vacancies: []CareerAgentVacancyResult{{
			VacancyID:            801,
			Title:                "Системный администратор",
			FinalRouteReasonCode: "ROLE_OUT_OF_SCOPE",
			TerminalOutcome:      TerminalReviewRequired,
			BlockedReason:        "resume routing requires review",
		}},
	}

	output := renderCareerAgentHumanReport(report)
	for _, expected := range []string{
		"Route ambiguous: 3",
		"No suitable resume: 2",
		"Role out of scope: 4",
		"Route low evidence: 5",
		"ROLE_OUT_OF_SCOPE",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("human report missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderDailyCareerAgentStatusIsStableAndExplicitlyReadOnly(t *testing.T) {
	started := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	run := careeragent.NewAgentRun("daily-career-agent-2026-09-24", careeragent.AgentRunStageCareerAgent, started)
	result := careeragent.DailyCareerAgentRun{Run: run, Summary: careeragent.DailyCareerAgentSummary{Result: careeragent.DailyResultPartialSuccess, Vacancy: careeragent.DailyVacancySummary{Scanned: 4, Found: 3, Matched: 1, ReviewRequired: 2}, Communication: careeragent.DailyCommunicationSummary{ConversationsSynced: 5, NewMessages: 7, RepliesNeeded: 2, Failures: 1}}, Attention: []careeragent.AttentionItem{{ID: "attention-1"}, {ID: "attention-2"}}}
	result.IdempotentReplay = true
	output := renderDailyCareerAgentStatus(result)
	for _, expected := range []string{"Career Agent daily: PARTIAL_SUCCESS", "Run: daily-career-agent-2026-09-24", "Vacancies: scanned=4 found=3 matched=1 review=2", "Communication: conversations=5 new_messages=7 replies_needed=2 failures=1", "Attention: 2", "HH writes: 0", "Replay: true"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("daily status missing %q:\n%s", expected, output)
		}
	}
}
