package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Reasons                 []string                    `json:"reasons"`
	Missing                 []string                    `json:"missing"`
	HardRequirementsMissing []string                    `json:"hard_requirements_missing,omitempty"`
	HardRequirements        []HardRequirementEvaluation `json:"hard_requirements,omitempty"`
	SearchProfiles          []string                    `json:"search_profiles,omitempty"`
}

type SearchProfileSummary struct {
	Name             string `json:"name"`
	URL              string `json:"url,omitempty"`
	VacanciesFetched int    `json:"vacancies_fetched"`
}

type RunSummaryResult struct {
	Type                       string                 `json:"type"`
	SearchProfiles             []SearchProfileSummary `json:"search_profiles,omitempty"`
	VacanciesSeen              int                    `json:"vacancies_seen"`
	VacanciesFetched           int                    `json:"vacancies_fetched"`
	VacanciesFetchedRaw        int                    `json:"vacancies_fetched_raw"`
	VacanciesAfterDedup        int                    `json:"vacancies_after_dedup"`
	DuplicatesSkipped          int                    `json:"duplicates_skipped"`
	VacanciesProcessed         int                    `json:"vacancies_processed"`
	PreviouslyRespondedSkipped int                    `json:"previously_responded_skipped"`
	DeterministicSkipped       int                    `json:"deterministic_skipped"`
	AIEvaluated                int                    `json:"ai_evaluated"`
	AIRejected                 int                    `json:"ai_rejected,omitempty"`
	Matched                    int                    `json:"matched"`
	Rejected                   int                    `json:"rejected"`
	ReviewRequired             int                    `json:"review_required"`
	ReviewBeforeDetail         int                    `json:"review_before_detail"`
	ReviewAfterDetail          int                    `json:"review_after_detail"`
	WouldApply                 int                    `json:"would_apply"`
	Applied                    int                    `json:"applied"`
	Errors                     int                    `json:"errors"`
	VacancyLimitSkipped        int                    `json:"vacancy_limit_skipped,omitempty"`
	ApplicationLimitSkipped    int                    `json:"application_limit_skipped,omitempty"`
	DispatchAttempts           int                    `json:"dispatch_attempts,omitempty"`
	BlockedAttempts            int                    `json:"blocked_attempts,omitempty"`
	ReconciledConfirmed        int                    `json:"reconciled_confirmed,omitempty"`
	UnresolvedAttempts         int                    `json:"unresolved_attempts,omitempty"`
	TerminalOutcomes           map[string]int         `json:"terminal_outcomes,omitempty"`
	TotalTerminal              int                    `json:"total_terminal,omitempty"`
	AccountingPass             bool                   `json:"accounting_pass"`
	ShadowWriteCount           int                    `json:"shadow_write_count"`
	ResumeRouted               int                    `json:"resume_routed,omitempty"`
	PreliminaryObviousRejects  int                    `json:"preliminary_obvious_rejects,omitempty"`
	PreliminaryClearRoute      int                    `json:"preliminary_clear_route,omitempty"`
	PreliminaryNeedsDetail     int                    `json:"preliminary_needs_detail,omitempty"`
	DetailRequested            int                    `json:"detail_requested,omitempty"`
	DetailSucceeded            int                    `json:"detail_succeeded,omitempty"`
	DetailFailed               int                    `json:"detail_failed,omitempty"`
	FinalRouted                int                    `json:"final_routed,omitempty"`
	FinalAmbiguous             int                    `json:"final_ambiguous,omitempty"`
	StageStats                 map[string]StageStats  `json:"stage_stats,omitempty"`
}

type StageStats struct {
	Entered int            `json:"entered"`
	Exited  int            `json:"exited"`
	Reasons map[string]int `json:"reasons,omitempty"`
}

type CareerAgentVacancyResult struct {
	Type                  string                    `json:"type"`
	VacancyID             int                       `json:"vacancy_id"`
	Title                 string                    `json:"title"`
	Company               string                    `json:"company,omitempty"`
	URL                   string                    `json:"url,omitempty"`
	FoundByProfiles       []string                  `json:"found_by_profiles,omitempty"`
	SearchCardEvidence    []string                  `json:"search_card_evidence,omitempty"`
	CheapFilterResult     string                    `json:"cheap_filter_result,omitempty"`
	CheapFilterReasons    []string                  `json:"cheap_filter_reasons,omitempty"`
	PreliminaryRoute      string                    `json:"preliminary_route,omitempty"`
	PreliminaryReasonCode string                    `json:"preliminary_reason_code,omitempty"`
	DetailFetchStatus     string                    `json:"detail_fetch_status,omitempty"`
	DetailEvidence        []string                  `json:"detail_evidence,omitempty"`
	AIEvaluated           bool                      `json:"ai_evaluated"`
	AIScore               *int                      `json:"ai_score,omitempty"`
	AIReasons             []string                  `json:"ai_reasons,omitempty"`
	AICallReason          string                    `json:"ai_call_reason,omitempty"`
	ResumeCandidates      []careeragent.ResumeScore `json:"resume_candidates,omitempty"`
	PreliminaryCandidates []careeragent.ResumeScore `json:"preliminary_candidates,omitempty"`
	SelectedResume        string                    `json:"selected_resume,omitempty"`
	ResumeConfidence      string                    `json:"resume_confidence,omitempty"`
	FinalRouteReasonCode  string                    `json:"final_route_reason_code,omitempty"`
	FinalDecision         string                    `json:"final_decision"`
	WouldApply            bool                      `json:"would_apply"`
	BlockedReason         string                    `json:"blocked_reason,omitempty"`
	CoverLetterGenerated  bool                      `json:"cover_letter_generated"`
	TerminalOutcome       string                    `json:"terminal_outcome"`
	ProcessedAt           time.Time                 `json:"processed_at"`
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

func vacancyDecision(evaluation VacancyEvaluation, minScore int) VacancyDecision {
	if len(hardRequirementsMissing(evaluation)) > 0 {
		return VacancyReject
	}
	if !evaluation.Apply {
		return VacancyReject
	}
	if evaluation.Score < minScore {
		return VacancyReject
	}
	if len(hardRequirementsUnknown(evaluation)) > 0 {
		return VacancyReviewRequired
	}
	return VacancyMatch
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
	missing := hardRequirementsMissing(evaluation)
	if len(missing) > 0 {
		return "hard requirements not met: " + strings.Join(missing, ", ")
	}
	if !evaluation.Apply {
		return fmt.Sprintf("AI recommended not applying (%d/100)", evaluation.Score)
	}
	if evaluation.Score < minScore {
		return fmt.Sprintf("AI score below threshold (%d/100, minimum %d)", evaluation.Score, minScore)
	}
	unknown := hardRequirementsUnknown(evaluation)
	if len(unknown) > 0 {
		return "hard requirements could not be verified: " + strings.Join(unknown, ", ")
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
