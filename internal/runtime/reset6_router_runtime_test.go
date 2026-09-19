package runtime

import (
	"testing"

	"hh-ai-responder/internal/careeragent"
)

func TestCareerAgentDetailEvidenceFeedsFinalRouter(t *testing.T) {
	responder := &HHAIResponder{careerAgentResumes: []careeragent.ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"Technical Support"}, Enabled: true},
	}}

	card := Vacancy{ID: 701, Name: "Backend specialist", DataCompleteness: DataCompletenessMinimal}
	preliminary := responder.preliminaryRouteForVacancy(card)
	if preliminary.Status != careeragent.PreliminaryNeedsDetail {
		t.Fatalf("incomplete card did not defer to detail: %+v", preliminary)
	}

	detail := card
	detail.Description = "Python backend service development"
	detail.Skills = []string{"Python", "Django"}
	detail.DataCompleteness = DataCompletenessFull
	route := responder.routeResumeForVacancy(detail)
	if route.Status != careeragent.RouteSelected || route.SelectedResumeID != "python" {
		t.Fatalf("detail evidence did not feed final route: %+v", route)
	}
}

func TestCareerAgentUnsafeRouteStopsBeforeAI(t *testing.T) {
	for _, reason := range []string{
		careeragent.RouteReasonAmbiguous,
		careeragent.RouteReasonLowEvidence,
		careeragent.RouteReasonOutOfScope,
		careeragent.RouteReasonNoSuitable,
	} {
		route := careeragent.RouteDecision{Status: careeragent.RouteReviewRequired, ReasonCode: reason, Confidence: careeragent.ConfidenceLow}
		if careerAgentRouteAllowsAI(route, "shadow") {
			t.Fatalf("unsafe route %q was allowed to AI", reason)
		}
	}
	selected := careeragent.RouteDecision{Status: careeragent.RouteSelected, ReasonCode: careeragent.RouteReasonSelected, Confidence: careeragent.ConfidenceMedium}
	if !careerAgentRouteAllowsAI(selected, "shadow") {
		t.Fatal("selected route was blocked from AI")
	}
}

func TestCareerAgentRouteReasonCountersAreMutuallyExclusive(t *testing.T) {
	summary := &RunSummaryResult{}
	for _, reason := range []string{
		careeragent.RouteReasonAmbiguous,
		careeragent.RouteReasonLowEvidence,
		careeragent.RouteReasonOutOfScope,
		careeragent.RouteReasonNoSuitable,
	} {
		recordRouteReason(summary, reason)
	}
	recordRouteReason(summary, careeragent.RouteReasonSelected)
	recordRouteReason(summary, "UNKNOWN")

	if len(summary.RouteReasonCounts) != 4 {
		t.Fatalf("unexpected route reason categories: %+v", summary.RouteReasonCounts)
	}
	for reason, count := range summary.RouteReasonCounts {
		if count != 1 {
			t.Fatalf("reason %q counted %d times: %+v", reason, count, summary.RouteReasonCounts)
		}
	}
}

func TestCareerAgentRouteEventCarriesFullTelemetry(t *testing.T) {
	decision := careeragent.RouteDecision{
		VacancyID:         702,
		Status:            careeragent.RouteReviewRequired,
		ReasonCode:        careeragent.RouteReasonAmbiguous,
		RoleEvidence:      careeragent.VacancyRoleEvidence{EvidenceAvailable: true, StrongFamilies: []careeragent.RoleFamily{careeragent.RoleFamilyPythonBackend}},
		AlternativeScores: []careeragent.ResumeScore{{ResumeID: "python", MatchedRoleFamilies: []careeragent.RoleFamily{careeragent.RoleFamilyPythonBackend}, SpecificEvidenceCount: 2, GenericEvidenceRatio: 0.2}},
	}
	event := careerAgentRouteEvent(decision)
	if !event.RoleEvidence.EvidenceAvailable || len(event.AlternativeScores) != 1 {
		t.Fatalf("route event lost role telemetry: %+v", event)
	}
}
