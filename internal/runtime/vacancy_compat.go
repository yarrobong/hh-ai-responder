package runtime

import "hh-ai-responder/internal/vacancy"

// These aliases keep staged root-package consumers source-compatible while
// vacancy owns the single implementation. They are transitional and should
// disappear as later domain boundaries migrate callers.
type Vacancy = vacancy.Vacancy
type NamedObject = vacancy.NamedObject
type Company = vacancy.Company
type Compensation = vacancy.Compensation
type ChangeTime = vacancy.ChangeTime
type MatchResult = vacancy.MatchResult
type MatchRisk = vacancy.MatchRisk
type RecommendationDecision = vacancy.RecommendationDecision
type ApplicationRecommendation = vacancy.ApplicationRecommendation
type DataCompleteness = vacancy.DataCompleteness
type ReconciliationEvidence = vacancy.ReconciliationEvidence

const (
	RecommendationApply = vacancy.RecommendationApply
	RecommendationMaybe = vacancy.RecommendationMaybe
	RecommendationSkip  = vacancy.RecommendationSkip

	DataCompletenessFull    = vacancy.DataCompletenessFull
	DataCompletenessPartial = vacancy.DataCompletenessPartial
	DataCompletenessMinimal = vacancy.DataCompletenessMinimal
)

// FormatCompensation remains as a root compatibility wrapper for existing HH
// presentation callers; formatting itself is owned by internal/vacancy.
func FormatCompensation(c *Compensation) string {
	return vacancy.FormatCompensation(c)
}

func validateMatchResult(value MatchResult) error {
	return value.Validate()
}

func normalizeMatchResult(value *MatchResult) {
	value.Normalize()
}

func deterministicVacancyRejectReason(value Vacancy, description string, minSalary int, excludeKeywords []string) string {
	return vacancy.DeterministicRejectReason(value, description, minSalary, "RUR", excludeKeywords)
}

func deterministicVacancyRejectReasonWithCurrency(value Vacancy, description string, minSalary int, minSalaryCurrency string, excludeKeywords []string) string {
	return vacancy.DeterministicRejectReason(value, description, minSalary, minSalaryCurrency, excludeKeywords)
}
