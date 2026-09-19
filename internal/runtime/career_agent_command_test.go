package runtime

import (
	"strings"
	"testing"
)

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
