package runtime

import (
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func TestDailyVacancyStageMapsExistingCareerAgentReport(t *testing.T) {
	now := time.Date(2026, 9, 24, 9, 3, 0, 0, time.UTC)
	report := CareerAgentRunReport{
		Summary: RunSummaryResult{
			VacanciesFetchedRaw: 4, VacanciesAfterDedup: 3, Matched: 1, Rejected: 1,
			ReviewRequired: 1, AIEvaluated: 2, Errors: 1, AIHardUnknown: 1,
			RouteReasonCounts: map[string]int{
				careeragent.RouteReasonAmbiguous:   2,
				careeragent.RouteReasonLowEvidence: 3,
				careeragent.RouteReasonNoSuitable:  4,
			},
		},
		Vacancies: []CareerAgentVacancyResult{
			{VacancyID: 101, TerminalOutcome: TerminalAIMatch, ProcessedAt: now},
			{VacancyID: 102, TerminalOutcome: TerminalAIReject, ProcessedAt: now},
			{VacancyID: 103, TerminalOutcome: TerminalReviewRequired, ProcessedAt: now},
		},
	}
	result, err := dailyStageResultFromCareerAgentReport(report, "daily-run", now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Vacancy.Found != 4 || !result.Summary.Vacancy.RawHitsKnown || !result.Summary.Vacancy.DiagnosticsKnown || result.Summary.Vacancy.New != 3 || result.Summary.Vacancy.Matched != 1 || result.Summary.Vacancy.ReviewRequired != 1 || result.Summary.AI.Requested != 2 || result.Summary.Vacancy.RouteAmbiguous != 2 || result.Summary.Vacancy.RouteLowEvidence != 3 || result.Summary.Vacancy.NoSuitableResume != 4 || result.Summary.Vacancy.HardUnknown != 1 {
		t.Fatalf("summary=%+v", result.Summary)
	}
	if len(result.Items) != 3 || result.Items[0].RunID != "daily-run" {
		t.Fatalf("items=%+v", result.Items)
	}
	if len(result.Summary.Failures) != 1 {
		t.Fatalf("failures=%v", result.Summary.Failures)
	}
	if len(result.Attention) != 1 || result.Attention[0].VacancyID != 103 {
		t.Fatalf("attention=%+v", result.Attention)
	}
}

func TestDailyCommunicationStageMapsReadOnlyReport(t *testing.T) {
	report := CommunicationRunReport{
		Conversations: 8, Classified: 5, Drafts: 2, Clarifications: 1, WorkItems: 4,
		ReviewRequired: 1, Failures: 1,
		Sync: SyncResult{Created: 2, Updated: 3},
	}
	result := dailyStageResultFromCommunicationReport(report)
	if result.Summary.Communication.ConversationsSynced != 8 || result.Summary.Communication.NewMessages != 5 || result.Summary.Communication.RepliesNeeded != 2 || result.Summary.Communication.Failures != 1 {
		t.Fatalf("summary=%+v", result.Summary)
	}
	if result.Summary.Vacancy != (careeragent.DailyVacancySummary{}) {
		t.Fatalf("communication stage modified vacancy summary=%+v", result.Summary.Vacancy)
	}
}
