package runtime

import (
	"testing"

	"hh-ai-responder/internal/careeragent"
)

func TestBuildCareerAgentSearchProfilesPreservesPlannerMetadata(t *testing.T) {
	base := mustURL(t, "https://hh.example")
	responder := &HHAIResponder{
		baseURL: base, careerAgentMaxSearchProfiles: 4, searchPeriodDays: 7,
	}
	responder.rebuildCareerAgentSearchProfiles([]careeragent.ResumeProfile{{ID: "support", Title: "Техническая поддержка", Enabled: true}})
	if len(responder.searchProfiles) == 0 || len(responder.careerAgentProfiles) == 0 {
		t.Fatalf("planner profiles were not rebuilt: %+v", responder.searchProfiles)
	}
	planned := responder.careerAgentProfiles[0]
	actual := responder.searchProfiles[0]
	if actual.ID != planned.ID || actual.Query != planned.Query || actual.ProfileType != planned.ProfileType || actual.RoleFamily != planned.RoleFamily {
		t.Fatalf("planner metadata was not copied: planned=%+v actual=%+v", planned, actual)
	}
	if len(actual.SourceResumeIDs) == 0 || len(actual.EligibilityEvidence) == 0 {
		t.Fatalf("planner provenance was not copied: %+v", actual)
	}
}
