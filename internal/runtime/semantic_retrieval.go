package runtime

import (
	"sort"
	"strings"

	candidatesemanticsearch "hh-ai-responder/internal/usecase/candidatesemanticsearch"
)

// These aliases preserve the root orchestration API while the retrieval
// capability and its implementation live in an importable use case.
type SemanticRetrievalPurpose = candidatesemanticsearch.Purpose

const (
	SemanticRetrievalPurposeEmployerReply = candidatesemanticsearch.PurposeEmployerReply
	SemanticRetrievalPurposeCoverLetter   = candidatesemanticsearch.PurposeCoverLetter
	SemanticRetrievalPurposeVacancy       = candidatesemanticsearch.PurposeVacancy
)

type SemanticRetrievalRequest = candidatesemanticsearch.RetrievalRequest
type CandidateSemanticRetriever = candidatesemanticsearch.Retriever

const (
	semanticRetrievalTopK            = 5
	semanticContextLimit             = 3
	semanticSelectionMaxRunes        = 800
	semanticContextMaxRunes          = 2400
	semanticQueryMaxRunes            = 900
	semanticVacancyTextMaxRunes      = 500
	semanticRetrievalDefaultMinScore = candidatesemanticsearch.DefaultMinScore
)

// ShouldUseCandidateSemanticRetrieval is deliberately conservative. It uses
// only the message and already-resolved structured context; it never invokes
// an LLM intent classifier.
func ShouldUseCandidateSemanticRetrieval(message string, resolved CandidateContext) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" || isOperationalConversationMessage(text) {
		return false
	}
	markers := []string{
		"расскажите", "расскажи", "пример", "кейс", "опыт", "проект", "достиж", "автоматиз",
		"сложн задач", "рутин", "клиент", "что вы делали", "что удалось", "реальные",
		"tell me", "example", "case", "experience", "project", "achievement", "automated",
		"challenging", "clients", "what did you do",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	// A short follow-up is eligible only when the existing resolver has already
	// established a topic. A vector result must never establish that topic.
	if contextFollowUp(message) || conversationDetailFollowUp(message) {
		return len(resolved.RelevantSkills) > 0 || len(resolved.RelevantProjects) > 0 || len(resolved.ResolvedFacts) > 0 || len(resolved.PartiallyResolvedFacts) > 0
	}
	return false
}

func isOperationalConversationMessage(text string) bool {
	for _, marker := range []string{
		"здравствуйте", "добрый день", "добрый вечер", "спасибо", "привет", "hello", "hi", "thanks",
		"какая зарплата", "сколько зарплата", "зарплат", "salary", "какой график", "график работы",
		"когда готовы выйти", "готовы выйти", "готовы к офису", "офис", "релокац", "переезд",
		"пришлите телефон", "телефон", "номер телефона", "собеседован", "интервью", "документ",
		"договор", "контракт", "паспорт", "банков", "парол",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func buildEmployerSemanticQuery(message, vacancyTitle string, resolved CandidateContext) string {
	query := strings.TrimSpace(message)
	if (contextFollowUp(message) || conversationDetailFollowUp(message)) && (len(resolved.RelevantSkills) > 0 || len(resolved.RelevantProjects) > 0) {
		topics := append([]string{}, resolved.RelevantSkills...)
		for _, project := range resolved.RelevantProjects {
			topics = append(topics, project.Name)
		}
		query += " Реальный опыт, проекты и достижения по теме: " + strings.Join(uniqueStrings(topics), ", ")
	}
	if strings.TrimSpace(query) == "" {
		query = strings.TrimSpace(vacancyTitle)
	}
	return truncateRunes(query, semanticQueryMaxRunes)
}

func buildCoverLetterSemanticQuery(title, description string, match MatchResult) string {
	parts := []string{strings.TrimSpace(title)}
	if len(match.MatchedSkills) > 0 {
		parts = append(parts, "Ключевые навыки: "+strings.Join(uniqueStrings(match.MatchedSkills), ", "))
	}
	if len(match.MatchedProjects) > 0 {
		parts = append(parts, "Связанные задачи: "+strings.Join(uniqueStrings(match.MatchedProjects), ", "))
	}
	if compact := compactVacancyDescription(description); compact != "" {
		parts = append(parts, compact)
	}
	return truncateRunes(strings.Join(parts, ". "), semanticQueryMaxRunes)
}

func compactVacancyDescription(description string) string {
	parts := strings.FieldsFunc(description, func(r rune) bool { return r == '.' || r == '\n' || r == ';' })
	selected := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if len(selected) < 3 || strings.Contains(lower, "обязан") || strings.Contains(lower, "требован") || strings.Contains(lower, "автомат") || strings.Contains(lower, "api") || strings.Contains(lower, "python") || strings.Contains(lower, "postgres") {
			selected = append(selected, part)
		}
		if len(selected) >= 5 {
			break
		}
	}
	return truncateRunes(strings.Join(selected, ". "), semanticVacancyTextMaxRunes)
}

// SafeSemanticSelection is prompt data rebuilt from the current canonical
// entity. Indexed content is never copied into this type.
type SafeSemanticSelection struct {
	EntityType     CandidateSemanticEntityType    `json:"entity_type"`
	EntityID       string                         `json:"entity_id"`
	Score          float64                        `json:"score"`
	Title          string                         `json:"title"`
	Text           string                         `json:"text"`
	ContentHash    string                         `json:"content_hash"`
	EmbeddingModel string                         `json:"embedding_model"`
	EvidenceRefs   []CandidateSemanticEvidenceRef `json:"evidence_refs,omitempty"`
}

// BuildSafeSemanticContext repeats current-entity validation and rebuilds the
// context from canonical values. It is pure and does not perform retrieval.
func BuildSafeSemanticContext(candidate Candidate, results []CandidateSemanticResult) []SafeSemanticSelection {
	if len(results) == 0 {
		return []SafeSemanticSelection{}
	}
	safe, err := CanonicalEmployerSafeProjection(candidate)
	if err != nil {
		return []SafeSemanticSelection{}
	}
	drafts, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		return []SafeSemanticSelection{}
	}
	byKey := map[string]CandidateSemanticDocumentDraft{}
	for _, draft := range drafts {
		if draft.Eligible {
			byKey[semanticDocumentKey(candidate.ID, draft.EntityType, draft.EntityID)] = draft
		}
	}
	byProject := map[string]CanonicalCandidateProject{}
	byStory := map[string]CanonicalCandidateStory{}
	byAchievement := map[string]CandidateAchievement{}
	for _, item := range candidate.Projects {
		byProject[item.ID] = item
	}
	for _, item := range candidate.Stories {
		byStory[item.ID] = item
	}
	for _, item := range candidate.Achievements {
		byAchievement[item.ID] = item
	}

	allowedTechnologies := map[string]bool{}
	for _, skill := range safe.Skills {
		if !skill.Negative && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
			allowedTechnologies[contextCanonical(skill.Name)] = true
		}
	}
	makeText := func(fields ...string) string {
		values := []string{}
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" || containsUnsupportedTechnology(field, allowedTechnologies) {
				continue
			}
			values = append(values, field)
		}
		return truncateRunes(strings.Join(values, "\n"), semanticSelectionMaxRunes)
	}
	selection := func(result CandidateSemanticResult, title, text string, refs []CandidateSemanticEvidenceRef) SafeSemanticSelection {
		return SafeSemanticSelection{EntityType: result.EntityType, EntityID: result.EntityID, Score: result.Score, Title: truncateRunes(title, 160), Text: text, ContentHash: result.ContentHash, EmbeddingModel: result.Model, EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, refs...)}
	}
	candidates := []SafeSemanticSelection{}
	for _, result := range results {
		if result.Score < semanticRetrievalDefaultMinScore {
			continue
		}
		draft, ok := byKey[semanticDocumentKey(candidate.ID, result.EntityType, result.EntityID)]
		if !ok || draft.ContentHash != result.ContentHash || strings.TrimSpace(result.Model) == "" {
			continue // stale, ineligible or malformed retrieval result
		}
		var text string
		switch result.EntityType {
		case CandidateSemanticEntityStory:
			story, exists := byStory[result.EntityID]
			if !exists {
				continue
			}
			text = makeText("Title: "+story.Title, "Situation: "+story.Situation, "Context: "+story.Context, "Summary: "+story.Summary, "Task: "+story.Task, "Action: "+story.Action, "Result: "+story.Result, "Achievement: "+story.Achievement)
			if strings.TrimSpace(text) != "" {
				candidates = append(candidates, selection(result, story.Title, text, draft.EvidenceRefs))
			}
		case CandidateSemanticEntityProject:
			project, exists := byProject[result.EntityID]
			if !exists {
				continue
			}
			text = makeText("Project: "+project.Name, "Role: "+project.Role, "Description: "+project.Description, "Technologies: "+strings.Join(safeProjectTechnologies(project.Technologies, allowedTechnologies), ", "), "Tasks: "+strings.Join(project.Tasks, "; "), "Results: "+strings.Join(project.Results, "; "))
			if strings.TrimSpace(text) != "" {
				candidates = append(candidates, selection(result, project.Name, text, draft.EvidenceRefs))
			}
		case CandidateSemanticEntityAchievement:
			achievement, exists := byAchievement[result.EntityID]
			if !exists {
				continue
			}
			text = makeText("Achievement: "+achievement.Title, "Problem: "+achievement.Problem, "Solution: "+strings.Join(achievement.Solution, "; "), "Actions: "+strings.Join(achievement.Actions, "; "), "Result: "+strings.Join(achievement.Result, "; "), "Technologies: "+strings.Join(safeProjectTechnologies(achievement.Technologies, allowedTechnologies), ", "))
			if strings.TrimSpace(text) != "" {
				candidates = append(candidates, selection(result, achievement.Title, text, draft.EvidenceRefs))
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	selected := []SafeSemanticSelection{}
	for _, candidate := range candidates {
		duplicate := false
		for _, old := range selected {
			if semanticSelectionsDuplicate(old, candidate) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		selected = append(selected, candidate)
		if len(selected) >= semanticContextLimit || totalSemanticRunes(selected) >= semanticContextMaxRunes {
			break
		}
	}
	return selected
}

func safeProjectTechnologies(values []string, allowed map[string]bool) []string {
	result := []string{}
	for _, value := range values {
		if allowed[contextCanonical(value)] {
			result = append(result, value)
		}
	}
	return uniqueStrings(result)
}

func containsUnsupportedTechnology(text string, allowed map[string]bool) bool {
	for _, technology := range contextTechnologyNames {
		if contextMentions(text, technology) && !allowed[contextCanonical(technology)] {
			return true
		}
	}
	return false
}

func semanticSelectionsDuplicate(left, right SafeSemanticSelection) bool {
	for _, leftRef := range left.EvidenceRefs {
		for _, rightRef := range right.EvidenceRefs {
			if leftRef.EntityType == rightRef.EntityType && leftRef.EntityID == rightRef.EntityID && leftRef.EntityID != "" {
				return true
			}
		}
	}
	leftTokens, rightTokens := semanticTokens(left.Text), semanticTokens(right.Text)
	if len(leftTokens) < 3 || len(rightTokens) < 3 {
		return false
	}
	common := 0
	for token := range leftTokens {
		if rightTokens[token] {
			common++
		}
	}
	minSize := len(leftTokens)
	if len(rightTokens) < minSize {
		minSize = len(rightTokens)
	}
	return float64(common)/float64(minSize) >= 0.7
}

func semanticTokens(text string) map[string]bool {
	result := map[string]bool{}
	for _, token := range contextTokens(text) {
		if len(token) >= 4 {
			result[contextCanonical(token)] = true
		}
	}
	return result
}

func totalSemanticRunes(values []SafeSemanticSelection) int {
	total := 0
	for _, value := range values {
		total += len([]rune(value.Text))
	}
	return total
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

func semanticEntityTypesForPurpose(purpose SemanticRetrievalPurpose) []CandidateSemanticEntityType {
	switch purpose {
	case SemanticRetrievalPurposeEmployerReply, SemanticRetrievalPurposeCoverLetter:
		return []CandidateSemanticEntityType{CandidateSemanticEntityStory, CandidateSemanticEntityProject, CandidateSemanticEntityAchievement}
	default:
		return []CandidateSemanticEntityType{CandidateSemanticEntityStory, CandidateSemanticEntityProject, CandidateSemanticEntityAchievement}
	}
}
