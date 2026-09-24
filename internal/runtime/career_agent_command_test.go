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
	"hh-ai-responder/internal/usecase/coverletter"
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

func TestCareerAgentPreparationPersistsOptionalNoLetterWithCanonicalEmptyHash(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-optional-letter",
		careerAgentCandidateVersion: 5,
		careerAgentCandidateHash:    strings.Repeat("b", 64),
	}
	result := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID: 778, ResumeID: "resume-optional", ResumeTitle: "Технический специалист",
		CoverLetter: "", CoverLetterStatus: coverletter.DraftStatusHardInvalid,
	}}
	trace := CareerAgentVacancyResult{VacancyID: 778, SelectedResume: "resume-optional", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}
	if err := responder.persistCareerAgentPreparation(Vacancy{ID: 778}, ResumeItem{Hash: "resume-optional", ProviderID: "provider-optional"}, trace, result); err != nil {
		t.Fatal(err)
	}
	if len(store.preparations) != 1 {
		t.Fatalf("preparations=%d, want 1", len(store.preparations))
	}
	preparation := store.preparations[0]
	if preparation.CoverLetter != "" || preparation.CoverLetterHash != preparation.ContentHash() || preparation.CoverLetterHash != contentHash("") {
		t.Fatalf("optional no-letter hash was not canonical: %+v", preparation)
	}
	if err := preparation.Validate(); err != nil {
		t.Fatalf("optional no-letter preparation failed validation: %v", err)
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
	result := careeragent.DailyCareerAgentRun{Run: run, Summary: careeragent.DailyCareerAgentSummary{Result: careeragent.DailyResultPartialSuccess, Vacancy: careeragent.DailyVacancySummary{Scanned: 4, Found: 3, RawHitsKnown: true, DiagnosticsKnown: true, Matched: 1, ReviewRequired: 2}, Communication: careeragent.DailyCommunicationSummary{ConversationsSynced: 5, NewMessages: 7, RepliesNeeded: 2, Failures: 1}}, Attention: []careeragent.AttentionItem{{ID: "attention-1"}, {ID: "attention-2"}}}
	result.IdempotentReplay = true
	output := renderDailyCareerAgentStatus(result)
	for _, expected := range []string{"Career Agent daily: PARTIAL_SUCCESS", "Run: daily-career-agent-2026-09-24", "Vacancies: processed=4 raw_hits=3 matched=1 review=2", "AI reviewed: 0", "Prepared: 0", "Rejected: 0", "Route ambiguous: 0", "Low evidence: 0", "Role out of scope: 0", "Hard unknown: 0", "No suitable resume: 0", "Active attention: 0", "Historical/suppressed: 0", "Attention suppressed: none", "Attention breakdown: application_ready=0 vacancy_review=0 clarifications=0 needs_reply=0 interviews=0 tests=0 offers=0 follow_ups=0 other=0", "Communication: conversations=5 new_messages=7 replies_needed=2 failures=1", "Attention: 2", "HH writes: 0", "Replay: true"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("daily status missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderDailyCareerAgentStatusDoesNotInventLegacyDiagnostics(t *testing.T) {
	run := careeragent.NewAgentRun("daily-career-agent-legacy", careeragent.AgentRunStageCareerAgent, time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC))
	output := renderDailyCareerAgentStatus(careeragent.DailyCareerAgentRun{
		Run: run,
		Summary: careeragent.DailyCareerAgentSummary{
			Result:  careeragent.DailyResultSuccess,
			Vacancy: careeragent.DailyVacancySummary{Scanned: 32, ReviewRequired: 31, Rejected: 1},
		},
	})
	for _, expected := range []string{"raw_hits=unknown", "AI reviewed: unknown", "Prepared: unknown", "Rejected: 1", "Route ambiguous: unknown", "Low evidence: unknown", "Role out of scope: unknown", "Hard unknown: unknown", "No suitable resume: unknown"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("legacy daily status missing %q:\n%s", expected, output)
		}
	}
}

func TestCareerAgentUsageIncludesDailyCommand(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runCareerAgentCommand([]string{"--shadow", "unexpected"}, Config{}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "career-agent daily [--json]") {
		t.Fatalf("usage error=%v, want daily command in usage", err)
	}
}
