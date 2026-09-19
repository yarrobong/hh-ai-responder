package runtime

import (
	"testing"

	"hh-ai-responder/internal/careeragent"
)

func TestRunSummarySeparatesDiscoveryFromProcessedRouterPopulation(t *testing.T) {
	summary := RunSummaryResult{
		VacanciesFetchedRaw: 12,
		VacanciesAfterDedup: 8,
		VacancyLimitSkipped: 3,
	}
	summary.ProcessedByRouter = 4
	summary.RouterOutcomeCounts = map[string]int{
		careeragent.RouteReasonSelected:   1,
		careeragent.RouteReasonAmbiguous:  1,
		careeragent.RouteReasonNoSuitable: 1,
		careeragent.RouteReasonOutOfScope: 1,
	}
	finalizeDiscoveryCoverage(&summary)
	if summary.DistinctDiscovered != 8 || summary.ProcessedByRouter != 4 || summary.NotProcessedDueToRunCap != 3 {
		t.Fatalf("discovery and processed counters were mixed: %+v", summary)
	}
	if got := summary.RouterYields[careeragent.RouteReasonSelected]; got != 0.25 {
		t.Fatalf("selected yield used the wrong denominator: %v", got)
	}
}

func TestProcessedRouterOutcomesExcludeUnroutedVacancies(t *testing.T) {
	summary := RunSummaryResult{}
	recordRouterOutcome(&summary, careeragent.RouteReasonSelected, "MATCH")
	recordRouterOutcome(&summary, careeragent.RouteReasonAmbiguous, "REVIEW_REQUIRED")
	if summary.RouterOutcomeCounts[careeragent.RouteReasonSelected] != 1 || summary.FinalDecisionCounts["MATCH"] != 1 {
		t.Fatalf("router outcomes were not recorded: %+v", summary)
	}
	if summary.FinalDecisionCounts["REJECT"] != 0 || summary.FinalDecisionCounts["REVIEW_REQUIRED"] != 1 {
		t.Fatalf("unprocessed outcomes leaked into router counters: %+v", summary)
	}
}
