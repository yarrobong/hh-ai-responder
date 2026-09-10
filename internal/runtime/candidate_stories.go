package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	domaincandidate "hh-ai-responder/internal/candidate"
)

// CandidateStory is an example of work experience. It is deliberately kept
// separate from CandidateProfile: stories help the model choose an example,
// but they are not used for vacancy matching or as a source of candidate
// facts.
type CandidateStory = domaincandidate.CandidateStory
type CandidateStories = jsonstorage.CandidateStories

var storyWordRegexp = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}+#.-]*`)

func LoadCandidateStories(path string) (CandidateStories, error) {
	return jsonstorage.NewCandidateStoryStore(path).Load()
}

func validateCandidateStory(story CandidateStory) error {
	return story.Validate()
}

func defaultCandidateStoriesPath() string {
	if value := strings.TrimSpace(os.Getenv("HH_CANDIDATE_STORIES")); value != "" {
		return value
	}
	wd, err := os.Getwd()
	if err != nil {
		return "candidate_stories.json"
	}
	return filepath.Join(wd, "candidate_stories.json")
}

func formatCandidateStories(stories CandidateStories) (string, error) {
	return jsonstorage.FormatCandidateStories(stories)
}

func selectRelevantCandidateStories(stories []CandidateStory, vacancy Vacancy, description string) []CandidateStory {
	vacancyText := strings.ToLower(strings.Join([]string{vacancy.Name, vacancy.Company.Name, description}, "\n"))
	if strings.TrimSpace(vacancyText) == "" {
		return nil
	}
	type scoredStory struct {
		story CandidateStory
		score int
		index int
	}
	selected := make([]scoredStory, 0, len(stories))
	for index, story := range stories {
		score := storyRelevanceScore(story, vacancyText)
		if score > 0 {
			selected = append(selected, scoredStory{story: story, score: score, index: index})
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].score != selected[j].score {
			return selected[i].score > selected[j].score
		}
		return selected[i].index < selected[j].index
	})
	if len(selected) > 2 {
		selected = selected[:2]
	}
	result := make([]CandidateStory, 0, len(selected))
	for _, item := range selected {
		result = append(result, item.story)
	}
	return result
}

func storyRelevanceScore(story CandidateStory, vacancyText string) int {
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
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || len(storyTokens(term)) == 0 {
			continue
		}
		// A multi-word term can be represented with different punctuation in
		// HH text; require all meaningful words instead of guessing synonyms.
		allWordsPresent := true
		for _, word := range storyTokens(term) {
			if len(word) < 2 || !slicesContains(storyTokens(vacancyText), word) {
				allWordsPresent = false
				break
			}
		}
		if allWordsPresent {
			score++
		}
	}
	return score
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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

func candidateStoriesPrompt(stories []CandidateStory) string {
	if len(stories) == 0 {
		return ""
	}
	if len(stories) > 2 {
		stories = stories[:2]
	}
	var builder strings.Builder
	builder.WriteString("\n\nРелевантные примеры опыта из candidate_stories.json (не источник новых фактов):")
	for i, story := range stories {
		builder.WriteString(fmt.Sprintf("\n\nКейс %d: %s", i+1, strings.TrimSpace(story.Title)))
		writeStoryField(&builder, "Контекст", firstNonEmpty(story.Situation, story.Context, story.Summary, story.Description, story.Story))
		writeStoryField(&builder, "Задача", firstNonEmpty(story.Task, story.Problem))
		writeStoryField(&builder, "Личный вклад", firstNonEmpty(story.Action, story.Actions, story.Contribution))
		writeStoryField(&builder, "Результат", firstNonEmpty(story.Result, story.Outcome, story.Achievement, story.Achievements))
	}
	builder.WriteString("\nИспользуй не более 1–2 кейсов и только если они прямо помогают ответить на требования вакансии. Не добавляй достижения, цифры, технологии или результат, которых нет в candidate_profile.json или в подтверждённом тексте кейса; при сомнении пропусти кейс.")
	return builder.String()
}

func writeStoryField(builder *strings.Builder, label, value string) {
	if value = strings.TrimSpace(value); value != "" {
		builder.WriteString("\n" + label + ": " + value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
