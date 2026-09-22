package careeragent

import "testing"

func TestAdvisoryResumeProfileSelectsOnlyEnabledAmbiguousCandidate(t *testing.T) {
	route := RouteDecision{
		Status:     RouteReviewRequired,
		ReasonCode: RouteReasonAmbiguous,
		AlternativeScores: []ResumeScore{
			{ResumeID: "blocked", HardBlockers: []string{"unsupported"}},
			{ResumeID: "support", Score: 71},
		},
	}
	profiles := []ResumeProfile{
		{ID: "blocked", Hash: "hash-blocked", Enabled: true},
		{ID: "support", ProviderID: "provider-support", Enabled: true, Title: "Technical support"},
	}
	selected, ok := AdvisoryResumeProfile(route, profiles)
	if !ok || selected.ID != "support" {
		t.Fatalf("selected=%+v ok=%t", selected, ok)
	}
}

func TestAdvisoryResumeProfileRejectsUnsafeRouteOrIdentity(t *testing.T) {
	for name, route := range map[string]RouteDecision{
		"low evidence": {Status: RouteReviewRequired, ReasonCode: RouteReasonLowEvidence, AlternativeScores: []ResumeScore{{ResumeID: "support"}}},
		"out of scope": {Status: RouteReviewRequired, ReasonCode: RouteReasonOutOfScope, AlternativeScores: []ResumeScore{{ResumeID: "support"}}},
		"selected":     {Status: RouteSelected, ReasonCode: RouteReasonSelected, AlternativeScores: []ResumeScore{{ResumeID: "support"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := AdvisoryResumeProfile(route, []ResumeProfile{{ID: "support", Enabled: true, Title: "Technical support"}}); ok {
				t.Fatal("unsafe route produced an advisory candidate")
			}
		})
	}
	ambiguous := RouteDecision{Status: RouteReviewRequired, ReasonCode: RouteReasonAmbiguous, AlternativeScores: []ResumeScore{{ResumeID: "support"}}}
	if _, ok := AdvisoryResumeProfile(ambiguous, []ResumeProfile{{ID: "support", Enabled: true}}); ok {
		t.Fatal("profile without provider identity was selected")
	}
}
