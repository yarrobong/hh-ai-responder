package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	appconfig "hh-ai-responder/internal/config"
	vacancyanalysis "hh-ai-responder/internal/usecase/vacancyanalysis"
)

type HardRequirementCandidate = vacancyanalysis.HardRequirementCandidate
type HardRequirementEvaluation = vacancyanalysis.HardRequirementEvaluation
type VacancyEvaluationAIResponse = vacancyanalysis.AIResponse
type VacancyEvaluation = vacancyanalysis.Assessment

type vacancyEvaluationInput struct {
	Candidate       LegacyCandidateContext
	Vacancy         Vacancy
	Description     string
	Salary          string
	Location        string
	WorkSchedule    string
	IncludeKeywords []string
}

type VacancySkippedResult struct {
	Type                    string                      `json:"type"`
	VacancyID               int                         `json:"vacancy_id"`
	Name                    string                      `json:"name"`
	URL                     string                      `json:"url"`
	Reason                  string                      `json:"reason"`
	AttemptID               string                      `json:"attempt_id,omitempty"`
	AttemptState            string                      `json:"attempt_state,omitempty"`
	Score                   *int                        `json:"score,omitempty"`
	HardRequirementsMissing []string                    `json:"hard_requirements_missing,omitempty"`
	HardRequirements        []HardRequirementEvaluation `json:"hard_requirements,omitempty"`
	SearchProfiles          []string                    `json:"search_profiles,omitempty"`
}

type VacancyReviewRequiredResult struct {
	Type                    string                      `json:"type"`
	VacancyID               int                         `json:"vacancy_id"`
	Name                    string                      `json:"name"`
	URL                     string                      `json:"url"`
	Score                   int                         `json:"score"`
	Apply                   bool                        `json:"apply"`
	Recommendation          string                      `json:"recommendation,omitempty"`
	RecommendationReasons   []string                    `json:"recommendation_reasons,omitempty"`
	Reasons                 []string                    `json:"reasons"`
	Missing                 []string                    `json:"missing"`
	HardRequirementsUnknown []string                    `json:"hard_requirements_unknown"`
	HardRequirements        []HardRequirementEvaluation `json:"hard_requirements"`
	SearchProfiles          []string                    `json:"search_profiles,omitempty"`
}

type VacancyMatchResult struct {
	Type                    string                      `json:"type"`
	VacancyID               int                         `json:"vacancy_id"`
	Name                    string                      `json:"name"`
	URL                     string                      `json:"url"`
	Score                   int                         `json:"score"`
	Recommendation          string                      `json:"recommendation,omitempty"`
	RecommendationReasons   []string                    `json:"recommendation_reasons,omitempty"`
	Reasons                 []string                    `json:"reasons"`
	Missing                 []string                    `json:"missing"`
	HardRequirementsMissing []string                    `json:"hard_requirements_missing,omitempty"`
	HardRequirements        []HardRequirementEvaluation `json:"hard_requirements,omitempty"`
	SearchProfiles          []string                    `json:"search_profiles,omitempty"`
}

type SearchProfileSummary struct {
	ID                       string                        `json:"id,omitempty"`
	Name                     string                        `json:"name"`
	URL                      string                        `json:"url,omitempty"`
	Query                    string                        `json:"query,omitempty"`
	ProfileType              careeragent.SearchProfileType `json:"profile_type,omitempty"`
	RoleFamily               careeragent.RoleFamily        `json:"role_family,omitempty"`
	SourceResumeIDs          []string                      `json:"source_resume_ids,omitempty"`
	VacanciesFetched         int                           `json:"vacancies_fetched"`
	RawHits                  int                           `json:"raw_hits"`
	DistinctProfileVacancies int                           `json:"distinct_profile_vacancies"`
	ExclusiveVacancies       int                           `json:"exclusive_vacancies"`
	OverlapVacancies         int                           `json:"overlap_vacancies"`
	UnionNewContribution     int                           `json:"union_new_contribution"`
	ProcessedVacancies       int                           `json:"processed_vacancies,omitempty"`
	PagesFetched             int                           `json:"pages_fetched"`
	APIQueryParams           url.Values                    `json:"api_query_params,omitempty"`
	PlannerOnlyParams        url.Values                    `json:"planner_only_params,omitempty"`
	APIFound                 int                           `json:"api_found"`
	APIFoundKnown            bool                          `json:"api_found_known"`
	FirstVacancyID           int                           `json:"first_vacancy_id,omitempty"`
	LastVacancyID            int                           `json:"last_vacancy_id,omitempty"`
	Truncated                bool                          `json:"truncated,omitempty"`
	TruncationReason         string                        `json:"truncation_reason,omitempty"`
}

type RunSummaryResult struct {
	Type                             string                 `json:"type"`
	SearchProfiles                   []SearchProfileSummary `json:"search_profiles,omitempty"`
	RawHits                          int                    `json:"raw_hits"`
	DistinctDiscovered               int                    `json:"distinct_discovered"`
	ProcessedByRouter                int                    `json:"processed_by_router"`
	NotProcessedDueToRunCap          int                    `json:"not_processed_due_to_run_cap"`
	NotProcessedByOtherPreRouterGate int                    `json:"not_processed_by_other_pre_router_gate"`
	RouterOutcomeCounts              map[string]int         `json:"router_outcome_counts,omitempty"`
	FinalDecisionCounts              map[string]int         `json:"final_decision_counts,omitempty"`
	RouterYields                     map[string]float64     `json:"router_yields,omitempty"`
	DiscoveryTruncated               bool                   `json:"discovery_truncated"`
	DiscoveryComplete                bool                   `json:"discovery_complete"`
	SearchPagesFetched               int                    `json:"search_pages_fetched"`
	SearchPagesTruncated             int                    `json:"search_pages_truncated"`
	VacanciesSeen                    int                    `json:"vacancies_seen"`
	VacanciesFetched                 int                    `json:"vacancies_fetched"`
	VacanciesFetchedRaw              int                    `json:"vacancies_fetched_raw"`
	VacanciesAfterDedup              int                    `json:"vacancies_after_dedup"`
	DuplicatesSkipped                int                    `json:"duplicates_skipped"`
	VacanciesProcessed               int                    `json:"vacancies_processed"`
	PreviouslyRespondedSkipped       int                    `json:"previously_responded_skipped"`
	DeterministicSkipped             int                    `json:"deterministic_skipped"`
	AIEvaluated                      int                    `json:"ai_evaluated"`
	AIRejected                       int                    `json:"ai_rejected,omitempty"`
	AIApplyTrue                      int                    `json:"ai_apply_true,omitempty"`
	AIApplyFalse                     int                    `json:"ai_apply_false,omitempty"`
	AIRecommendationApply            int                    `json:"ai_recommendation_apply,omitempty"`
	AIRecommendationDoNotApply       int                    `json:"ai_recommendation_do_not_apply,omitempty"`
	AIRecommendationUncertain        int                    `json:"ai_recommendation_uncertain,omitempty"`
	AIAdvisoryOnlyConcerns           int                    `json:"ai_advisory_only_concerns,omitempty"`
	AIHardMissing                    int                    `json:"ai_hard_missing,omitempty"`
	AIHardUnknown                    int                    `json:"ai_hard_unknown,omitempty"`
	AIScoreBelowThreshold            int                    `json:"ai_score_below_threshold,omitempty"`
	AIMatched                        int                    `json:"ai_matched,omitempty"`
	AIReviewed                       int                    `json:"ai_reviewed,omitempty"`
	AIReasonCounts                   map[string]int         `json:"ai_reason_counts,omitempty"`
	Matched                          int                    `json:"matched"`
	Rejected                         int                    `json:"rejected"`
	ReviewRequired                   int                    `json:"review_required"`
	ReviewBeforeDetail               int                    `json:"review_before_detail"`
	ReviewAfterDetail                int                    `json:"review_after_detail"`
	WouldApply                       int                    `json:"would_apply"`
	Applied                          int                    `json:"applied"`
	Errors                           int                    `json:"errors"`
	VacancyLimitSkipped              int                    `json:"vacancy_limit_skipped,omitempty"`
	ApplicationLimitSkipped          int                    `json:"application_limit_skipped,omitempty"`
	DispatchAttempts                 int                    `json:"dispatch_attempts,omitempty"`
	BlockedAttempts                  int                    `json:"blocked_attempts,omitempty"`
	ReconciledConfirmed              int                    `json:"reconciled_confirmed,omitempty"`
	UnresolvedAttempts               int                    `json:"unresolved_attempts,omitempty"`
	TerminalOutcomes                 map[string]int         `json:"terminal_outcomes,omitempty"`
	TotalTerminal                    int                    `json:"total_terminal,omitempty"`
	AccountingPass                   bool                   `json:"accounting_pass"`
	ShadowWriteCount                 int                    `json:"shadow_write_count"`
	ResumeRouted                     int                    `json:"resume_routed,omitempty"`
	PreliminaryObviousRejects        int                    `json:"preliminary_obvious_rejects,omitempty"`
	PreliminaryClearRoute            int                    `json:"preliminary_clear_route,omitempty"`
	PreliminaryNeedsDetail           int                    `json:"preliminary_needs_detail,omitempty"`
	DetailRequested                  int                    `json:"detail_requested,omitempty"`
	DetailSucceeded                  int                    `json:"detail_succeeded,omitempty"`
	DetailFailed                     int                    `json:"detail_failed,omitempty"`
	FinalRouted                      int                    `json:"final_routed,omitempty"`
	FinalAmbiguous                   int                    `json:"final_ambiguous,omitempty"`
	RouteReasonCounts                map[string]int         `json:"route_reason_counts,omitempty"`
	StageStats                       map[string]StageStats  `json:"stage_stats,omitempty"`
	routerEnteredVacancies           map[int]struct{}       `json:"-"`
}

func recordRouterEntry(summary *RunSummaryResult, vacancy Vacancy, sources []careeragent.SearchProfileEvidence) {
	if summary == nil {
		return
	}
	if summary.routerEnteredVacancies == nil {
		summary.routerEnteredVacancies = map[int]struct{}{}
	}
	if _, exists := summary.routerEnteredVacancies[vacancy.ID]; exists {
		return
	}
	summary.routerEnteredVacancies[vacancy.ID] = struct{}{}
	summary.ProcessedByRouter++
	seenProfiles := map[string]struct{}{}
	for _, source := range sources {
		profileKey := source.ID
		if profileKey == "" {
			profileKey = source.Label
		}
		if profileKey == "" {
			continue
		}
		if _, exists := seenProfiles[profileKey]; exists {
			continue
		}
		seenProfiles[profileKey] = struct{}{}
		for index := range summary.SearchProfiles {
			profile := &summary.SearchProfiles[index]
			if profile.ID == source.ID || (profile.ID == "" && profile.Name == source.Label) {
				profile.ProcessedVacancies++
				break
			}
		}
	}
}

func recordRouterOutcome(summary *RunSummaryResult, routeCode string, finalDecision string) {
	if summary == nil {
		return
	}
	if strings.TrimSpace(routeCode) != "" {
		if summary.RouterOutcomeCounts == nil {
			summary.RouterOutcomeCounts = map[string]int{}
		}
		summary.RouterOutcomeCounts[routeCode]++
	}
	if strings.TrimSpace(finalDecision) != "" {
		if summary.FinalDecisionCounts == nil {
			summary.FinalDecisionCounts = map[string]int{}
		}
		summary.FinalDecisionCounts[finalDecision]++
	}
}

func finalizeDiscoveryCoverage(summary *RunSummaryResult) {
	if summary == nil {
		return
	}
	if summary.RawHits == 0 {
		summary.RawHits = summary.VacanciesFetchedRaw
	}
	if summary.DistinctDiscovered == 0 {
		summary.DistinctDiscovered = summary.VacanciesAfterDedup
	}
	if summary.NotProcessedDueToRunCap == 0 {
		summary.NotProcessedDueToRunCap = summary.VacancyLimitSkipped
	}
	remaining := summary.DistinctDiscovered - summary.ProcessedByRouter - summary.NotProcessedDueToRunCap
	if remaining < 0 {
		remaining = 0
	}
	summary.NotProcessedByOtherPreRouterGate = remaining
	summary.RouterYields = map[string]float64{}
	if summary.ProcessedByRouter > 0 {
		for reason, count := range summary.RouterOutcomeCounts {
			summary.RouterYields[reason] = float64(count) / float64(summary.ProcessedByRouter)
		}
	}
	if !summary.DiscoveryTruncated {
		summary.DiscoveryComplete = true
	}
}

type StageStats struct {
	Entered int            `json:"entered"`
	Exited  int            `json:"exited"`
	Reasons map[string]int `json:"reasons,omitempty"`
}

type CareerAgentVacancyResult struct {
	Type                    string                          `json:"type"`
	VacancyID               int                             `json:"vacancy_id"`
	Title                   string                          `json:"title"`
	Company                 string                          `json:"company,omitempty"`
	URL                     string                          `json:"url,omitempty"`
	FoundByProfiles         []string                        `json:"found_by_profiles,omitempty"`
	SearchCardEvidence      []string                        `json:"search_card_evidence,omitempty"`
	CheapFilterResult       string                          `json:"cheap_filter_result,omitempty"`
	CheapFilterReasons      []string                        `json:"cheap_filter_reasons,omitempty"`
	PreliminaryRoute        string                          `json:"preliminary_route,omitempty"`
	PreliminaryReasonCode   string                          `json:"preliminary_reason_code,omitempty"`
	DetailFetchStatus       string                          `json:"detail_fetch_status,omitempty"`
	DetailEvidence          []string                        `json:"detail_evidence,omitempty"`
	AIEvaluated             bool                            `json:"ai_evaluated"`
	AIScore                 *int                            `json:"ai_score,omitempty"`
	AIReasons               []string                        `json:"ai_reasons,omitempty"`
	AIApply                 *bool                           `json:"ai_apply,omitempty"`
	AIRecommendation        string                          `json:"ai_recommendation,omitempty"`
	AIRecommendationReasons []string                        `json:"ai_recommendation_reasons,omitempty"`
	AIHardRequirements      []HardRequirementEvaluation     `json:"ai_hard_requirements,omitempty"`
	AIDecision              string                          `json:"ai_decision,omitempty"`
	AIReasonCode            string                          `json:"ai_reason_code,omitempty"`
	AIHardMissing           []string                        `json:"ai_hard_missing,omitempty"`
	AIHardUnknown           []string                        `json:"ai_hard_unknown,omitempty"`
	AIThreshold             int                             `json:"ai_threshold,omitempty"`
	AICallReason            string                          `json:"ai_call_reason,omitempty"`
	ResumeCandidates        []careeragent.ResumeScore       `json:"resume_candidates,omitempty"`
	PreliminaryCandidates   []careeragent.ResumeScore       `json:"preliminary_candidates,omitempty"`
	SelectedResume          string                          `json:"selected_resume,omitempty"`
	SelectedResumeTitle     string                          `json:"selected_resume_title,omitempty"`
	ResumeConfidence        string                          `json:"resume_confidence,omitempty"`
	RoleEvidence            careeragent.VacancyRoleEvidence `json:"role_evidence,omitempty"`
	FinalRouteReasonCode    string                          `json:"final_route_reason_code,omitempty"`
	FinalReasonCode         string                          `json:"final_reason_code,omitempty"`
	LocalPolicyGate         string                          `json:"local_policy_gate,omitempty"`
	FinalDecision           string                          `json:"final_decision"`
	WouldApply              bool                            `json:"would_apply"`
	BlockedReason           string                          `json:"blocked_reason,omitempty"`
	CoverLetterGenerated    bool                            `json:"cover_letter_generated"`
	TerminalOutcome         string                          `json:"terminal_outcome"`
	ProcessedAt             time.Time                       `json:"processed_at"`
}

const (
	TerminalAlreadyResponded    = "ALREADY_RESPONDED"
	TerminalDeterministicReject = "DETERMINISTIC_REJECT"
	TerminalDetailFetchFailed   = "DETAIL_FETCH_FAILED"
	TerminalDetailNotRequired   = "DETAIL_NOT_REQUIRED"
	TerminalAIReject            = "AI_REJECT"
	TerminalAIMatch             = "AI_MATCH"
	TerminalReviewRequired      = "REVIEW_REQUIRED"
	TerminalVacancyLimit        = "VACANCY_LIMIT"
	TerminalApplicationLimit    = "APPLICATION_LIMIT"
	TerminalError               = "ERROR"
	TerminalAttemptBlocked      = "ATTEMPT_BLOCKED"
)

func newRunAccounting() (map[int]CareerAgentVacancyResult, map[string]int, map[string]StageStats) {
	return map[int]CareerAgentVacancyResult{}, map[string]int{}, map[string]StageStats{}
}

func recordStage(stats map[string]StageStats, stage, reason string, entered, exited bool) {
	value := stats[stage]
	if entered {
		value.Entered++
	}
	if exited {
		value.Exited++
	}
	if strings.TrimSpace(reason) != "" {
		if value.Reasons == nil {
			value.Reasons = map[string]int{}
		}
		value.Reasons[reason]++
	}
	stats[stage] = value
}

func validateCareerAgentAccounting(uniqueIDs []int, outcomes map[int]CareerAgentVacancyResult) error {
	if len(uniqueIDs) != len(outcomes) {
		return fmt.Errorf("unique=%d terminal_records=%d", len(uniqueIDs), len(outcomes))
	}
	seen := map[int]bool{}
	for _, id := range uniqueIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("unique vacancy id %d is duplicated or invalid", id)
		}
		seen[id] = true
		value, ok := outcomes[id]
		if !ok || !knownTerminalOutcome(value.TerminalOutcome) {
			return fmt.Errorf("vacancy %d has no terminal outcome", id)
		}
	}
	for id := range outcomes {
		if !seen[id] {
			return fmt.Errorf("terminal record %d is not in unique set", id)
		}
	}
	return nil
}

func knownTerminalOutcome(value string) bool {
	switch value {
	case TerminalAlreadyResponded, TerminalDeterministicReject, TerminalDetailFetchFailed, TerminalDetailNotRequired,
		TerminalAIReject, TerminalAIMatch, TerminalReviewRequired, TerminalVacancyLimit, TerminalApplicationLimit,
		TerminalError, TerminalAttemptBlocked:
		return true
	default:
		return false
	}
}

type VacancyDecision string

const (
	VacancyMatch          VacancyDecision = "MATCH"
	VacancyReject         VacancyDecision = "REJECT"
	VacancyReviewRequired VacancyDecision = "REVIEW_REQUIRED"
)

const (
	hardRequirementCategoryEducation       = vacancyanalysis.HardRequirementCategoryEducation
	hardRequirementCategoryLocation        = vacancyanalysis.HardRequirementCategoryLocation
	hardRequirementCategoryExperienceYears = vacancyanalysis.HardRequirementCategoryExperienceYears
	hardRequirementCategorySkill           = vacancyanalysis.HardRequirementCategorySkill
	hardRequirementCategoryLanguage        = vacancyanalysis.HardRequirementCategoryLanguage
	hardRequirementCategoryLicense         = vacancyanalysis.HardRequirementCategoryLicense
	hardRequirementCategoryCitizenship     = vacancyanalysis.HardRequirementCategoryCitizenship
	hardRequirementCategoryOther           = vacancyanalysis.HardRequirementCategoryOther

	hardRequirementStatusMet     = vacancyanalysis.HardRequirementStatusMet
	hardRequirementStatusMissing = vacancyanalysis.HardRequirementStatusMissing
	hardRequirementStatusUnknown = vacancyanalysis.HardRequirementStatusUnknown
)

const (
	// AIReasonApplyFalse is retained for readers of older reports. New traces
	// use the local reason codes below and never use this as a terminal blocker.
	AIReasonApplyFalse          = "AI_APPLY_FALSE"
	AIReasonHardMissing         = "HARD_REQUIREMENT_MISSING"
	AIReasonScoreBelowThreshold = "AI_SCORE_BELOW_THRESHOLD"
	AIReasonHardUnknown         = "HARD_REQUIREMENT_UNKNOWN"
	AIReasonMatch               = "AI_MATCH"
	ReasonMatchConfirmed        = "MATCH_CONFIRMED"
	ReasonHardMissing           = "HARD_REQUIREMENT_MISSING"
	ReasonHardUnknown           = "HARD_REQUIREMENT_UNKNOWN"
	ReasonScoreBelowThreshold   = "FIT_SCORE_BELOW_THRESHOLD"
	ReasonAIAdvisoryConcern     = "AI_ADVISORY_CONCERN"
)

func recordAIDecisionBreakdown(summary *RunSummaryResult, trace *CareerAgentVacancyResult, evaluation VacancyEvaluation, minScore int) {
	if summary == nil || trace == nil {
		return
	}
	apply := evaluation.Apply
	trace.AIApply = &apply
	trace.AIRecommendation = assessmentRecommendation(evaluation)
	trace.AIRecommendationReasons = append([]string(nil), evaluation.RecommendationReasons...)
	trace.AIHardRequirements = append([]HardRequirementEvaluation(nil), evaluation.HardRequirements...)
	trace.AIHardMissing = hardRequirementsMissing(evaluation)
	trace.AIHardUnknown = hardRequirementsUnknown(evaluation)
	trace.AIThreshold = minScore
	if summary.AIReasonCounts == nil {
		summary.AIReasonCounts = map[string]int{}
	}
	switch trace.AIRecommendation {
	case vacancyanalysis.RecommendationApply:
		summary.AIRecommendationApply++
	case vacancyanalysis.RecommendationDoNotApply:
		summary.AIRecommendationDoNotApply++
	default:
		summary.AIRecommendationUncertain++
	}
	if apply {
		summary.AIApplyTrue++
	} else {
		summary.AIApplyFalse++
	}

	if len(trace.AIHardMissing) > 0 {
		summary.AIHardMissing++
	}
	if len(trace.AIHardUnknown) > 0 {
		summary.AIHardUnknown++
	}
	if evaluation.Score < minScore {
		summary.AIScoreBelowThreshold++
	}
	decision, reasonCode, _ := vacancyDecisionWithReason(evaluation, minScore)
	trace.AIDecision, trace.AIReasonCode = string(decision), reasonCode
	trace.LocalPolicyGate = reasonCode
	switch decision {
	case VacancyMatch:
		summary.AIMatched++
	case VacancyReviewRequired:
		summary.AIReviewed++
		if reasonCode == ReasonAIAdvisoryConcern {
			summary.AIAdvisoryOnlyConcerns++
		}
	default:
		summary.AIRejected++
	}
	summary.AIReasonCounts[trace.AIReasonCode]++
}

func vacancyDecision(evaluation VacancyEvaluation, minScore int) VacancyDecision {
	decision, _, _ := vacancyDecisionWithReason(evaluation, minScore)
	return decision
}

func vacancyDecisionWithReason(evaluation VacancyEvaluation, minScore int) (VacancyDecision, string, string) {
	if len(hardRequirementsMissing(evaluation)) > 0 {
		return VacancyReject, ReasonHardMissing, "hard requirements not met: " + strings.Join(hardRequirementsMissing(evaluation), ", ")
	}
	if evaluation.Score < minScore {
		return VacancyReject, ReasonScoreBelowThreshold, fmt.Sprintf("AI score below threshold (%d/100, minimum %d)", evaluation.Score, minScore)
	}
	if len(hardRequirementsUnknown(evaluation)) > 0 {
		return VacancyReviewRequired, ReasonHardUnknown, "hard requirements could not be verified: " + strings.Join(hardRequirementsUnknown(evaluation), ", ")
	}
	if assessmentRecommendation(evaluation) != vacancyanalysis.RecommendationApply {
		return VacancyReviewRequired, ReasonAIAdvisoryConcern, "AI advisory recommendation requires review"
	}
	return VacancyMatch, ReasonMatchConfirmed, ""
}

func assessmentRecommendation(evaluation VacancyEvaluation) string {
	if evaluation.Recommendation != "" {
		return evaluation.Recommendation
	}
	if evaluation.Apply {
		return vacancyanalysis.RecommendationApply
	}
	return vacancyanalysis.RecommendationDoNotApply
}

func hardRequirementsMissing(evaluation VacancyEvaluation) []string {
	result := make([]string, 0)
	for _, requirement := range evaluation.HardRequirements {
		if requirement.Status == hardRequirementStatusMissing && !isOptionalRequirement(requirement) {
			result = append(result, strings.TrimSpace(requirement.Requirement))
		}
	}
	return result
}

func hardRequirementsUnknown(evaluation VacancyEvaluation) []string {
	result := make([]string, 0)
	for _, requirement := range evaluation.HardRequirements {
		if requirement.Status == hardRequirementStatusUnknown && !requirement.Soft && !isOptionalRequirement(requirement) {
			result = append(result, strings.TrimSpace(requirement.Requirement))
		}
	}
	return result
}

func (r *HHAIResponder) skipVacancy(vacancy Vacancy, vacancyURL, reason string, score *int) {
	r.skipVacancyWithHardRequirements(vacancy, vacancyURL, reason, score, nil)
}

func (r *HHAIResponder) skipVacancyWithHardRequirements(vacancy Vacancy, vacancyURL, reason string, score *int, hardRequirementsMissing []string) {
	r.skipVacancyWithEvaluation(vacancy, vacancyURL, reason, score, hardRequirementsMissing, nil)
}

func (r *HHAIResponder) skipVacancyWithEvaluation(vacancy Vacancy, vacancyURL, reason string, score *int, hardRequirementsMissing []string, hardRequirements []HardRequirementEvaluation) {
	if len(hardRequirementsMissing) > 0 && score != nil {
		logger.Info("SKIP — hard requirements not met (score %d/100): %s", *score, strings.Join(hardRequirementsMissing, ", "))
	} else {
		logger.Info("SKIP — vacancy %d: %s", vacancy.ID, reason)
	}
	r.writeEvent(VacancySkippedResult{
		Type:                    "vacancy_skipped",
		VacancyID:               vacancy.ID,
		Name:                    vacancy.Name,
		URL:                     vacancyURL,
		Reason:                  reason,
		Score:                   score,
		HardRequirementsMissing: hardRequirementsMissing,
		HardRequirements:        hardRequirements,
		SearchProfiles:          r.vacancySearchSources[vacancy.ID],
	})
}

func (r *HHAIResponder) skipVacancyForAttempt(vacancy Vacancy, vacancyURL, reason, attemptID, attemptState string) {
	logger.Info("SKIP — vacancy %d: %s", vacancy.ID, reason)
	r.writeEvent(VacancySkippedResult{Type: "vacancy_skipped", VacancyID: vacancy.ID, Name: vacancy.Name, URL: vacancyURL, Reason: reason, AttemptID: attemptID, AttemptState: attemptState})
}

func parseKeywordList(value string) []string {
	return appconfig.ParseKeywordList(value)
}

func normalizeSalaryCurrency(value string) (string, error) {
	return appconfig.NormalizeSalaryCurrency(value)
}

func canonicalContextJSON(context CandidateContext) string {
	raw, err := json.Marshal(context)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func locationsMatch(candidateValue, vacancyValue string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(value)
		value = strings.NewReplacer(",", " ", ".", " ", "(", " ", ")", " ").Replace(value)
		return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	}
	candidate := normalize(candidateValue)
	vacancy := normalize(vacancyValue)
	if candidate == vacancy {
		return true
	}
	containsAllWords := func(shorter, longer string) bool {
		longerWords := make(map[string]struct{})
		for _, word := range strings.Fields(longer) {
			longerWords[word] = struct{}{}
		}
		words := strings.Fields(shorter)
		if len(words) == 0 {
			return false
		}
		for _, word := range words {
			if _, ok := longerWords[word]; !ok {
				return false
			}
		}
		return true
	}
	return containsAllWords(candidate, vacancy) || containsAllWords(vacancy, candidate)
}

func finalApplyDecision(evaluation VacancyEvaluation, minScore int) bool {
	return vacancyDecision(evaluation, minScore) == VacancyMatch
}

func vacancyEvaluationRejectReason(evaluation VacancyEvaluation, minScore int) string {
	_, _, reason := vacancyDecisionWithReason(evaluation, minScore)
	if reason != "" {
		return reason
	}
	return "vacancy does not meet application criteria"
}

func (c *AIClient) EvaluateVacancy(input vacancyEvaluationInput) (VacancyEvaluation, error) {
	if c == nil {
		return VacancyEvaluation{}, errors.New("AI completion provider is not configured")
	}
	if err := c.ctx.Err(); err != nil {
		return VacancyEvaluation{}, err
	}
	service := vacancyanalysis.NewService(
		vacancyanalysis.Dependencies{Completion: c.provider},
		vacancyanalysis.Options{Model: c.model, Attempts: c.attempts, MaxTokens: 1024, Temperature: 0.1, SemanticRetryDelay: aiRetryDelay},
	)
	assessment, err := service.Analyze(c.ctx, vacancyanalysis.Input{
		Candidate: vacancyanalysis.CandidateFacts{
			FullName: input.Candidate.FullName, ResumeTitle: input.Candidate.ResumeTitle, Salary: input.Candidate.Salary,
			Experience: input.Candidate.Experience, Skills: input.Candidate.Skills, Location: input.Candidate.Location,
			Contacts: input.Candidate.Contacts, EducationKnown: input.Candidate.EducationKnown, EducationLevel: input.Candidate.EducationLevel,
			EducationDetails: input.Candidate.EducationDetails, TotalExperienceMonthsKnown: input.Candidate.TotalExperienceMonthsKnown,
			TotalExperienceMonths: input.Candidate.TotalExperienceMonths, Profile: input.Candidate.Profile, SafeContext: input.Candidate.SafeContext,
		},
		Vacancy: input.Vacancy, Description: input.Description, Salary: input.Salary, Location: input.Location,
		WorkSchedule: input.WorkSchedule, IncludeKeywords: input.IncludeKeywords,
	})
	if err != nil {
		return VacancyEvaluation{}, err
	}
	assessment.HardRequirements = mergeHardRequirements(
		localStructuredHardRequirements(VacancyPreflight{WorkExperience: input.Vacancy.WorkExperience, WorkExperienceKnown: strings.TrimSpace(input.Vacancy.WorkExperience) != ""}, input.Candidate),
		assessment.HardRequirements,
	)
	return assessment, nil
}

func (c *AIClient) GenerateLetterWithEvaluation(v Vacancy, vacancyDescription string, candidate LegacyCandidateContext, evaluation VacancyEvaluation, extraPrompt string) (string, error) {
	return c.GenerateLetterWithEvaluationAndSemantic(v, vacancyDescription, candidate, evaluation, extraPrompt, nil)
}

func (c *AIClient) GenerateLetterWithEvaluationAndSemantic(v Vacancy, vacancyDescription string, candidate LegacyCandidateContext, evaluation VacancyEvaluation, extraPrompt string, examples []SafeSemanticSelection) (string, error) {
	evaluationCopy := evaluation
	return c.generateCoverLetter(coverLetterInput(v, vacancyDescription, candidate, &evaluationCopy, extraPrompt, examples))
}

func parseMinSalary(value string) (int, error) {
	return appconfig.ParseMinSalary(value)
}

func parseNonNegativeInt(value, name string, fallback int) (int, error) {
	return appconfig.ParseNonNegativeInt(value, name, fallback)
}

func parseMinMatchScore(value string) (int, error) {
	return appconfig.ParseMinMatchScore(value)
}

func normalizeChatMode(mode string) (string, error) {
	return appconfig.NormalizeChatMode(mode)
}
