package applicationanswer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

func validateApplicationDraft(draft string, value candidatecontext.CandidateContext) error {
	lower := strings.ToLower(draft)
	// A partial/basic Docker fact is not evidence of production or commercial
	// Docker experience. Unknown and restricted facts are handled by the shared
	// employer-safe validator and by the resolver's missing-information gate.
	if candidatecontext.Mentions(lower, "docker") && strings.Contains(lower, "docker") && productionOrCommercialClaim(lower, "docker") && !answerableTechnology(value, "docker") {
		return errors.New("production/commercial Docker use is not confirmed")
	}
	return nil
}

func productionOrCommercialClaim(text, technology string) bool {
	position := strings.Index(text, technology)
	if position < 0 {
		return false
	}
	start := position - 80
	if start < 0 {
		start = 0
	}
	end := position + len(technology) + 80
	if end > len(text) {
		end = len(text)
	}
	window := text[start:end]
	return strings.Contains(window, "production") || strings.Contains(window, "продакш") || strings.Contains(window, "продакшен") || strings.Contains(window, "коммерч")
}

func answerableTechnology(value candidatecontext.CandidateContext, technology string) bool {
	for _, fact := range value.ResolvedFacts {
		if candidatecontext.Canonical(fact.Topic) == candidatecontext.Canonical(technology) && fact.Status == candidatecontext.ResolvedFactAnswerable {
			return true
		}
	}
	return false
}

var storyWordRegexp = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}+#.-]*`)
var storyNumberRegexp = regexp.MustCompile(`\b\d+(?:[.,]\d+)?\s*(?:%|лет|год|года|месяц(?:ев|а)?|years?|months?)?\b`)

// ValidateStoryClaims verifies only claims tied to stories selected for this
// application. Stories remain examples, not an independent source of truth.
func ValidateStoryClaims(draft string, input Input) error {
	if len(input.Stories) == 0 {
		return nil
	}
	allowed := strings.ToLower(strings.Join(input.CandidateContext.AllowedFacts, "\n"))
	for _, story := range selectRelevantStories(input.Stories, input.Application, input.VacancyDescription) {
		storyText := strings.Join([]string{story.Title, story.Situation, story.Context, story.Summary, story.Description, story.Story, story.Task, story.Problem, story.Action, story.Actions, story.Contribution, story.Result, story.Outcome, story.Achievement, story.Achievements}, " ")
		for _, number := range storyNumberRegexp.FindAllString(strings.ToLower(storyText), -1) {
			number = strings.TrimSpace(number)
			if number != "" && !strings.Contains(allowed, number) && strings.Contains(strings.ToLower(draft), number) {
				return errors.New("story number is not confirmed in CandidateContext")
			}
		}
		for _, term := range story.Technologies {
			if candidatecontext.Mentions(draft, term) && !candidatecontext.Mentions(allowed, term) {
				return fmt.Errorf("story technology %q is not confirmed in CandidateContext", term)
			}
		}
		for _, claim := range []string{story.Result, story.Outcome, story.Achievement, story.Achievements} {
			claim = strings.TrimSpace(claim)
			if claim != "" && candidatecontext.Mentions(draft, claim) && !candidatecontext.Mentions(allowed, claim) {
				return errors.New("story result is not confirmed in CandidateContext")
			}
		}
		for _, claim := range []string{story.Situation, story.Context, story.Summary, story.Description, story.Story, story.Task, story.Problem, story.Action, story.Actions, story.Contribution} {
			claim = strings.TrimSpace(claim)
			if len(storyTokens(claim)) >= 3 && candidatecontext.Mentions(draft, claim) && !candidatecontext.Mentions(allowed, claim) {
				return errors.New("story detail is not confirmed in CandidateContext")
			}
		}
	}
	return nil
}

func selectRelevantStories(stories []candidate.CandidateStory, app application.JobApplication, description string) []candidate.CandidateStory {
	vacancyText := strings.ToLower(strings.Join([]string{app.VacancyTitle, app.CompanyName, description}, "\n"))
	if strings.TrimSpace(vacancyText) == "" {
		return nil
	}
	type scored struct {
		story        candidate.CandidateStory
		score, index int
	}
	selected := make([]scored, 0, len(stories))
	for index, story := range stories {
		if score := storyRelevanceScore(story, vacancyText); score > 0 {
			selected = append(selected, scored{story: story, score: score, index: index})
		}
	}
	// Stable insertion order is the existing selection tie-breaker.
	for i := 1; i < len(selected); i++ {
		for j := i; j > 0 && (selected[j].score > selected[j-1].score); j-- {
			selected[j], selected[j-1] = selected[j-1], selected[j]
		}
	}
	if len(selected) > 2 {
		selected = selected[:2]
	}
	result := make([]candidate.CandidateStory, 0, len(selected))
	for _, item := range selected {
		result = append(result, item.story)
	}
	return result
}

func storyRelevanceScore(story candidate.CandidateStory, vacancyText string) int {
	terms := append([]string{}, story.Keywords...)
	terms = append(terms, story.Technologies...)
	terms = append(terms, story.Skills...)
	terms = append(terms, story.Tags...)
	terms = append(terms, story.Roles...)
	terms = append(terms, story.Relevance...)
	terms = append(terms, story.RelevantFor...)
	terms = append(terms, story.RelevantRoles...)
	if len(terms) == 0 {
		terms = append(terms, story.Title)
	}
	score := 0
	for _, term := range terms {
		words := storyTokens(term)
		if len(words) == 0 {
			continue
		}
		present := true
		for _, word := range words {
			if len(word) < 2 || !contains(storyTokens(vacancyText), word) {
				present = false
				break
			}
		}
		if present {
			score++
		}
	}
	return score
}

func storyTokens(value string) []string {
	raw := storyWordRegexp.FindAllString(strings.ToLower(value), -1)
	result := make([]string, 0, len(raw))
	for _, word := range raw {
		word = strings.Trim(word, ".-")
		if word != "" {
			result = append(result, word)
		}
	}
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
