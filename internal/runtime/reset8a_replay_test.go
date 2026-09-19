package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

type reset8AReplayRequirement struct {
	Requirement                       string `json:"requirement"`
	Category                          string `json:"category"`
	VacancyEvidence                   string `json:"vacancy_evidence"`
	SourceField                       string `json:"source_field"`
	SourceContext                     string `json:"source_context"`
	RelevantWorkModeLocationField     string `json:"relevant_work_mode_location_field,omitempty"`
	RelevantStructuredExperienceField string `json:"relevant_structured_experience_field,omitempty"`
}

type reset8AReplayCandidateFacts struct {
	Location                   string `json:"location"`
	Skills                     string `json:"skills"`
	Experience                 string `json:"experience"`
	TotalExperienceMonthsKnown bool   `json:"total_experience_months_known"`
	TotalExperienceMonths      int    `json:"total_experience_months"`
}

type reset8AReplayCase struct {
	VacancyID                         int                         `json:"vacancy_id"`
	Title                             string                      `json:"title"`
	RoleFamily                        string                      `json:"role_family"`
	Description                       string                      `json:"description"`
	RelevantWorkModeLocationField     string                      `json:"relevant_work_mode_location_field"`
	RelevantStructuredExperienceField string                      `json:"relevant_structured_experience_field"`
	SelectedResumeID                  string                      `json:"selected_resume_id"`
	SelectedResumeTitle               string                      `json:"selected_resume_title"`
	RouterScore                       int                         `json:"router_score"`
	RunnerUpScore                     int                         `json:"runner_up_score"`
	RouterConfidence                  string                      `json:"router_confidence"`
	RouterReason                      string                      `json:"router_reason"`
	AIScore                           int                         `json:"ai_score"`
	AIRecommendation                  string                      `json:"ai_recommendation"`
	AIRecommendationReasons           []string                    `json:"ai_recommendation_reasons"`
	AIReasons                         []string                    `json:"ai_reasons"`
	HardRequirementCandidates         []reset8AReplayRequirement  `json:"hard_requirement_candidates"`
	TrustedCandidateFacts             reset8AReplayCandidateFacts `json:"trusted_candidate_facts"`
	OldHardStatuses                   map[string]string           `json:"old_hard_statuses"`
	OldLocalGate                      string                      `json:"old_local_gate"`
	OldFinalDecision                  string                      `json:"old_final_decision"`
	OldFinalReason                    string                      `json:"old_final_reason"`
}

func loadReset8AReplayCases(t *testing.T) []reset8AReplayCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/reset8a/reset8-selected.json")
	if err != nil {
		t.Fatalf("read RESET-8A replay fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cases []reset8AReplayCase
	if err := decoder.Decode(&cases); err != nil {
		t.Fatalf("decode RESET-8A replay fixture: %v", err)
	}
	if cases == nil {
		t.Fatal("RESET-8A replay fixture must be an array")
	}
	for _, item := range cases {
		if item.VacancyID <= 0 || item.SelectedResumeID == "" || item.AIRecommendation == "" {
			t.Fatalf("incomplete replay case %d", item.VacancyID)
		}
		if len([]rune(item.Title)) == 0 || len([]rune(item.RoleFamily)) == 0 {
			t.Fatalf("replay case %d lacks title or role family", item.VacancyID)
		}
		if len(item.HardRequirementCandidates) == 0 && item.OldHardStatuses == nil {
			t.Fatalf("replay case %d lacks hard-requirement state", item.VacancyID)
		}
		for _, requirement := range item.HardRequirementCandidates {
			if len([]rune(requirement.SourceContext)) > 240 {
				t.Fatalf("replay case %d requirement %q has unbounded source context", item.VacancyID, requirement.Requirement)
			}
		}
	}
	return cases
}

func TestReset8AReplayFixture(t *testing.T) {
	cases := loadReset8AReplayCases(t)
	if len(cases) != 12 {
		t.Fatalf("replay cases=%d, want 12", len(cases))
	}
	if cases[0].VacancyID != 137546982 || cases[11].VacancyID != 137500466 {
		t.Fatalf("unexpected replay ordering: first=%d last=%d", cases[0].VacancyID, cases[11].VacancyID)
	}
	for _, item := range cases {
		if item.AIScore < 0 || item.AIScore > 100 {
			t.Fatalf("replay case %d has invalid AI score %d", item.VacancyID, item.AIScore)
		}
		if item.TrustedCandidateFacts.TotalExperienceMonthsKnown && item.TrustedCandidateFacts.TotalExperienceMonths < 0 {
			t.Fatalf("replay case %d has invalid total experience", item.VacancyID)
		}
	}
}

func TestReset8AReplayTransitions(t *testing.T) {
	cases := loadReset8AReplayCases(t)
	for _, item := range cases {
		candidate := LegacyCandidateContext{
			Location:                   item.TrustedCandidateFacts.Location,
			Skills:                     item.TrustedCandidateFacts.Skills,
			Experience:                 item.TrustedCandidateFacts.Experience,
			TotalExperienceMonthsKnown: item.TrustedCandidateFacts.TotalExperienceMonthsKnown,
			TotalExperienceMonths:      item.TrustedCandidateFacts.TotalExperienceMonths,
		}
		value := Vacancy{
			ID:             item.VacancyID,
			Name:           item.Title,
			Description:    item.Description,
			WorkSchedule:   item.RelevantWorkModeLocationField,
			WorkExperience: item.RelevantStructuredExperienceField,
		}
		derived := deriveHardRequirements(candidate, value, item.Description, replayRequirementCandidates(item.HardRequirementCandidates))
		assessment := VacancyEvaluation{
			Score:            item.AIScore,
			Apply:            item.AIRecommendation == "APPLY",
			Recommendation:   item.AIRecommendation,
			HardRequirements: derived,
		}
		decision, reasonCode, _ := vacancyDecisionWithReason(assessment, 65)

		gotStatuses := make(map[string]string, len(derived))
		for _, requirement := range derived {
			gotStatuses[requirement.Requirement] = requirement.Status
		}
		expectedStatuses := cloneStringMap(item.OldHardStatuses)
		expectedReason := item.OldFinalReason
		expectedDecision := item.OldFinalDecision
		switch item.VacancyID {
		case 137531969:
			expectedStatuses["2 года опыта в AI/ML/NLP"] = hardRequirementStatusUnknown
			expectedReason = ReasonScoreBelowThreshold
			expectedDecision = string(VacancyReject)
		case 136577315:
			delete(expectedStatuses, "Москва (офис)")
			expectedReason = ReasonAIAdvisoryConcern
			expectedDecision = string(VacancyReviewRequired)
		}
		if !mapsEqual(gotStatuses, expectedStatuses) || string(decision) != expectedDecision || reasonCode != expectedReason {
			t.Errorf("vacancy %d transition old=%v/%s/%s/%s got=%v/%s/%s/%s expected=%v/%s/%s", item.VacancyID, item.OldHardStatuses, item.OldLocalGate, item.OldFinalDecision, item.OldFinalReason, gotStatuses, reasonCode, decision, reasonCode, expectedStatuses, expectedReason, expectedDecision)
		}
	}
}

func replayRequirementCandidates(values []reset8AReplayRequirement) []HardRequirementCandidate {
	result := make([]HardRequirementCandidate, 0, len(values))
	for _, value := range values {
		result = append(result, HardRequirementCandidate{Requirement: value.Requirement, Category: value.Category, VacancyEvidence: value.VacancyEvidence})
	}
	return result
}

func cloneStringMap(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
