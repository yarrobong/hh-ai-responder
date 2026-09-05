package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CandidateStory is an example of work experience. It is deliberately kept
// separate from CandidateProfile: stories help the model choose an example,
// but they are not used for vacancy matching or as a source of candidate
// facts.
type CandidateStory struct {
	ID            string   `json:"id,omitempty"`
	Title         string   `json:"title"`
	Situation     string   `json:"situation,omitempty"`
	Context       string   `json:"context,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Description   string   `json:"description,omitempty"`
	Story         string   `json:"story,omitempty"`
	Task          string   `json:"task,omitempty"`
	Problem       string   `json:"problem,omitempty"`
	Action        string   `json:"action,omitempty"`
	Actions       string   `json:"actions,omitempty"`
	Contribution  string   `json:"contribution,omitempty"`
	Result        string   `json:"result,omitempty"`
	Outcome       string   `json:"outcome,omitempty"`
	Achievement   string   `json:"achievement,omitempty"`
	Achievements  string   `json:"achievements,omitempty"`
	Technologies  []string `json:"technologies,omitempty"`
	Skills        []string `json:"skills,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Relevance     []string `json:"relevance,omitempty"`
	RelevantFor   []string `json:"relevant_for,omitempty"`
	RelevantRoles []string `json:"relevant_roles,omitempty"`
	ProfileRefs   []string `json:"profile_refs,omitempty"`
}

type CandidateStories struct {
	Version int              `json:"version,omitempty"`
	Stories []CandidateStory `json:"stories"`
}

var storyWordRegexp = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}+#.-]*`)

func LoadCandidateStories(path string) (CandidateStories, error) {
	if strings.TrimSpace(path) == "" {
		return CandidateStories{Version: 1, Stories: []CandidateStory{}}, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return CandidateStories{Version: 1, Stories: []CandidateStory{}}, nil
	}
	if err != nil {
		return CandidateStories{}, fmt.Errorf("read candidate stories: %w", err)
	}
	if profileContainsSecret(raw) {
		return CandidateStories{}, errors.New("candidate stories contain a forbidden secret field")
	}
	stories, err := decodeCandidateStories(raw)
	if err != nil {
		return CandidateStories{}, fmt.Errorf("decode candidate stories: %w", err)
	}
	if stories.Version == 0 {
		stories.Version = 1
	}
	for i := range stories.Stories {
		if err := validateCandidateStory(stories.Stories[i]); err != nil {
			return CandidateStories{}, fmt.Errorf("candidate story %d: %w", i+1, err)
		}
	}
	return stories, nil
}

func decodeCandidateStories(raw []byte) (CandidateStories, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return CandidateStories{}, errors.New("file is empty")
	}
	if trimmed[0] == '[' {
		var stories []CandidateStory
		if err := json.Unmarshal(trimmed, &stories); err != nil {
			return CandidateStories{}, err
		}
		return CandidateStories{Version: 1, Stories: stories}, nil
	}
	var file CandidateStories
	if err := json.Unmarshal(trimmed, &file); err != nil {
		return CandidateStories{}, err
	}
	if file.Stories == nil {
		return CandidateStories{}, errors.New("stories must be an array")
	}
	if file.Version != 0 && file.Version != 1 {
		return CandidateStories{}, fmt.Errorf("unsupported stories schema version %d", file.Version)
	}
	return file, nil
}

func validateCandidateStory(story CandidateStory) error {
	if strings.TrimSpace(story.Title) == "" {
		return errors.New("title must not be empty")
	}
	if strings.TrimSpace(story.Situation+story.Context+story.Summary+story.Description+story.Story+story.Task+story.Problem+story.Action+story.Actions+story.Contribution+story.Result+story.Outcome+story.Achievement+story.Achievements) == "" {
		return errors.New("story must contain experience details")
	}
	return nil
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
	if stories.Version == 0 {
		stories.Version = 1
	}
	if stories.Stories == nil {
		stories.Stories = []CandidateStory{}
	}
	encoded, err := json.MarshalIndent(stories, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
