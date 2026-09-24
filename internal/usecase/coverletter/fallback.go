package coverletter

import (
	"fmt"
	"sort"
	"strings"
)

func deterministicFallback(input Input) string {
	title := strings.TrimSpace(input.Vacancy.Name)
	if title == "" {
		title = strings.TrimSpace(input.Vacancy.Title)
	}
	if title == "" {
		title = "вакансия"
	}
	skills := fallbackRelevantSkills(input)
	if len(skills) > 0 {
		return fmt.Sprintf("Здравствуйте! Меня заинтересовала вакансия «%s». Подтверждённый опыт работы с %s может быть релевантен её задачам. Готов обсудить детали.", title, strings.Join(skills, ", "))
	}
	return fmt.Sprintf("Здравствуйте! Меня заинтересовала вакансия «%s». Готов обсудить задачи вакансии и формат работы.", title)
}

func fallbackRelevantSkills(input Input) []string {
	vacancyText := strings.ToLower(strings.Join([]string{input.Vacancy.Name, input.Vacancy.Title, input.Description, strings.Join(input.Vacancy.Requirements, " "), strings.Join(input.Vacancy.Skills, " ")}, "\n"))
	values := make([]string, 0, len(input.Candidate.SafeContext.RelevantSkills))
	seen := map[string]bool{}
	for _, value := range input.Candidate.SafeContext.RelevantSkills {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] || !strings.Contains(vacancyText, key) {
			continue
		}
		seen[key] = true
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
