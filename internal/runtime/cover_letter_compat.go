package runtime

import (
	"errors"
	"strings"

	coverletter "hh-ai-responder/internal/usecase/coverletter"
	vacancyanalysis "hh-ai-responder/internal/usecase/vacancyanalysis"
)

type CoverLetterResult = coverletter.Result

func coverLetterCandidateFacts(value LegacyCandidateContext) coverletter.CandidateFacts {
	return coverletter.CandidateFacts{
		FullName: value.FullName, ResumeTitle: value.ResumeTitle, Salary: value.Salary,
		Experience: value.Experience, Skills: value.Skills, Location: value.Location,
		Contacts: value.Contacts, EducationKnown: value.EducationKnown,
		EducationLevel: value.EducationLevel, EducationDetails: value.EducationDetails,
		TotalExperienceMonthsKnown: value.TotalExperienceMonthsKnown,
		TotalExperienceMonths:      value.TotalExperienceMonths, Profile: value.Profile,
		SafeContext: value.SafeContext, SafeKnowledge: value.SafeKnowledge,
	}
}

func coverLetterInput(value Vacancy, description string, candidate LegacyCandidateContext, evaluation *VacancyEvaluation, extraPrompt string, examples []SafeSemanticSelection) coverletter.Input {
	hints := make([]coverletter.SemanticHint, 0, len(examples))
	for _, example := range examples {
		hints = append(hints, coverletter.SemanticHint{
			EntityType: string(example.EntityType), EntityID: example.EntityID,
			Score: example.Score, Title: example.Title, Text: example.Text,
			ContentHash: example.ContentHash, EmbeddingModel: example.EmbeddingModel,
		})
	}
	return coverletter.Input{
		Candidate: coverLetterCandidateFacts(candidate), Stories: append([]CandidateStory(nil), candidate.Stories...),
		Vacancy: value, Description: description, Assessment: evaluation,
		SemanticHints: hints, ExtraPrompt: extraPrompt,
	}
}

func coverLetterInputFromApplication(value ApplicationContext, stories []CandidateStory, extraPrompt string) coverletter.Input {
	assessment := &vacancyanalysis.Assessment{
		Score: value.MatchResult.Score, Apply: true,
		Reasons:     []string{value.MatchResult.Explanation},
		Missing:     append([]string{}, value.MatchResult.MissingSkills...),
		StrongMatch: append(append([]string{}, value.MatchResult.MatchedSkills...), value.MatchResult.MatchedProjects...),
	}
	if strings.TrimSpace(assessment.Reasons[0]) == "" {
		assessment.Reasons = nil
	}
	legacy := LegacyCandidateContext{
		Skills:      strings.Join(value.CandidateContext.RelevantSkills, ", "),
		Experience:  strings.Join(value.CandidateContext.AllowedFacts, "\n"),
		SafeContext: value.CandidateContext, Stories: append([]CandidateStory(nil), stories...),
	}
	return coverletter.Input{
		Candidate: coverLetterCandidateFacts(legacy), Stories: legacy.Stories,
		Vacancy:     Vacancy{ID: value.Application.VacancyID, Name: value.Application.VacancyTitle, Company: Company{Name: value.Application.CompanyName}},
		Description: value.Conversation.VacancyDescription, Assessment: assessment,
		MatchContext:  &coverletter.MatchContext{MatchedSkills: append([]string{}, value.MatchResult.MatchedSkills...), MatchedProjects: append([]string{}, value.MatchResult.MatchedProjects...), MissingSkills: append([]string{}, value.MatchResult.MissingSkills...)},
		SemanticHints: applicationSemanticHints(value.RelevantExamples), ExtraPrompt: extraPrompt,
	}
}

func applicationSemanticHints(values []SafeSemanticSelection) []coverletter.SemanticHint {
	result := make([]coverletter.SemanticHint, 0, len(values))
	for _, value := range values {
		result = append(result, coverletter.SemanticHint{EntityType: string(value.EntityType), EntityID: value.EntityID, Score: value.Score, Title: value.Title, Text: value.Text, ContentHash: value.ContentHash, EmbeddingModel: value.EmbeddingModel})
	}
	return result
}

func (c *AIClient) generateCoverLetter(input coverletter.Input) (string, error) {
	if c == nil {
		return "", errors.New("AI client is nil")
	}
	service := coverletter.NewService(coverletter.Dependencies{Completion: c.provider}, coverletter.Options{Model: c.model, MaxTokens: 512, Temperature: 0.5})
	result, err := service.Generate(c.ctx, input)
	if err != nil {
		return "", err
	}
	return result.Letter, nil
}

func buildLetterSystemPrompt(candidate LegacyCandidateContext, extraPrompt string) string {
	return coverletter.SystemPromptWithStories(coverLetterCandidateFacts(candidate), candidate.Stories, extraPrompt)
}

func (c *AIClient) GenerateLetter(v Vacancy, vacancyDescription string, candidate LegacyCandidateContext, extraPrompt string) (string, error) {
	return c.generateCoverLetter(coverLetterInput(v, vacancyDescription, candidate, nil, extraPrompt, nil))
}
