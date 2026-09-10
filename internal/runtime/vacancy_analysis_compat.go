package runtime

import (
	"encoding/json"
	"strings"

	vacancyanalysis "hh-ai-responder/internal/usecase/vacancyanalysis"
)

// These wrappers preserve root-package tests and legacy callers while the
// vacancy-analysis algorithm, prompt, schema, parsing, and validation live in
// the importable use case package.
func vacancyAnalysisInput(input vacancyEvaluationInput) vacancyanalysis.Input {
	return vacancyanalysis.Input{
		Candidate: vacancyanalysis.CandidateFacts{
			FullName: input.Candidate.FullName, ResumeTitle: input.Candidate.ResumeTitle, Salary: input.Candidate.Salary,
			Experience: input.Candidate.Experience, Skills: input.Candidate.Skills, Location: input.Candidate.Location,
			Contacts: input.Candidate.Contacts, EducationKnown: input.Candidate.EducationKnown, EducationLevel: input.Candidate.EducationLevel,
			EducationDetails: input.Candidate.EducationDetails, TotalExperienceMonthsKnown: input.Candidate.TotalExperienceMonthsKnown,
			TotalExperienceMonths: input.Candidate.TotalExperienceMonths, Profile: input.Candidate.Profile, SafeContext: input.Candidate.SafeContext,
		},
		Vacancy: input.Vacancy, Description: input.Description, Salary: input.Salary, Location: input.Location,
		WorkSchedule: input.WorkSchedule, IncludeKeywords: input.IncludeKeywords,
	}
}

func buildVacancyEvaluationPrompt(input vacancyEvaluationInput) (string, string) {
	return vacancyanalysis.BuildPrompt(vacancyAnalysisInput(input))
}

func vacancyEvaluationJSONSchema() *ChatJSONSchema {
	format := vacancyanalysis.JSONResponseFormat()
	var schema map[string]any
	if format.JSONSchema == nil || json.Unmarshal(format.JSONSchema.Schema, &schema) != nil {
		return nil
	}
	return &ChatJSONSchema{Name: format.JSONSchema.Name, Strict: format.JSONSchema.Strict, Schema: schema}
}

func parseVacancyEvaluationJSON(answer string) (VacancyEvaluationAIResponse, error) {
	return vacancyanalysis.ParseAIResponse(answer)
}

func validateVacancyEvaluationAI(value VacancyEvaluationAIResponse) error {
	return vacancyanalysis.ValidateAIResponse(value)
}

func validateVacancyEvaluation(value VacancyEvaluation) error {
	return vacancyanalysis.ValidateAssessment(value)
}

func deriveHardRequirements(candidate LegacyCandidateContext, value Vacancy, description string, candidates []HardRequirementCandidate) []HardRequirementEvaluation {
	return vacancyanalysis.DeriveHardRequirements(vacancyAnalysisInput(vacancyEvaluationInput{Candidate: candidate, Vacancy: value, Description: description}).Candidate, value, description, candidates)
}

func deriveHardRequirementStatus(candidate LegacyCandidateContext, value Vacancy, requirement HardRequirementCandidate) (string, string) {
	return vacancyanalysis.DeriveHardRequirementStatus(vacancyAnalysisInput(vacancyEvaluationInput{Candidate: candidate, Vacancy: value}).Candidate, requirement)
}

func validateHardRequirements(candidate LegacyCandidateContext, value Vacancy, evaluation VacancyEvaluation) error {
	return vacancyanalysis.ValidateHardRequirements(vacancyAnalysisInput(vacancyEvaluationInput{Candidate: candidate, Vacancy: value}).Candidate, value, evaluation)
}

func isOptionalRequirement(value HardRequirementEvaluation) bool {
	return vacancyanalysis.IsOptionalRequirement(value)
}

func locationRequirementMatchesCandidate(candidateLocation, requirement, evidence string) bool {
	return vacancyanalysis.LocationRequirementMatchesCandidate(candidateLocation, requirement, evidence)
}

func normalizeEvidenceText(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "–", "-"), "—", "-")
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func containsNormalizedText(haystack, needle string) bool {
	needle = normalizeEvidenceText(needle)
	return needle != "" && strings.Contains(normalizeEvidenceText(haystack), needle)
}
