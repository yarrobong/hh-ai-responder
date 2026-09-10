package candidateacquisition

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

// DetectCandidateKnowledgeGaps consumes a prepared Candidate snapshot and an
// employer question. It performs no vacancy lookup and never treats an
// unknown vacancy requirement as an automatic candidate question.
func DetectCandidateKnowledgeGaps(value candidate.Candidate, employerQuestion string) ([]CandidateKnowledgeGap, error) {
	if _, err := candidate.CanonicalEmployerSafeProjection(value); err != nil {
		return nil, err
	}
	resolved, err := candidatecontext.NewResolver(value).Resolve(candidatecontext.ResolveInput{
		Query: employerQuestion, EmployerMessage: true,
	})
	if err != nil {
		return nil, err
	}
	if resolved.MessageIntent != candidatecontext.EmployerMessageIntentFactualQuestion && resolved.MessageIntent != candidatecontext.EmployerMessageIntentCompound {
		return []CandidateKnowledgeGap{}, nil
	}

	result := make([]CandidateKnowledgeGap, 0)
	lowerQuestion := strings.ToLower(employerQuestion)
	if containsAny(lowerQuestion, "конфликт", "сложн", "трудн") && containsAny(lowerQuestion, "клиент", "заказчик", "ситуац") && !hasCandidateStory(value, "customer_conflict") {
		result = append(result, knowledgeGapForFact(value, employerQuestion, candidatecontext.ResolvedFact{Topic: "customer_conflict", RequestedFact: "реальный пример конфликтного общения"}))
	}
	if containsAny(lowerQuestion, "сколько человек", "подчин") {
		result = append(result, knowledgeGapForFact(value, employerQuestion, candidatecontext.ResolvedFact{Topic: "team", RequestedFact: "размер команды"}))
	}
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, resolved.UnknownAtomicFacts...), resolved.PartiallyResolvedFacts...) {
		if !acquisitionFactAllowed(fact.Topic) {
			continue
		}
		gap := knowledgeGapForFact(value, employerQuestion, fact)
		if gap.DeterministicKey != "" {
			result = append(result, gap)
		}
	}
	return DeduplicateGaps(result), nil
}

func GapKey(candidateID, subjectType, subject, field string) string {
	raw := strings.Join([]string{candidateID, subjectType, canonical(subject), canonical(field)}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "gap-" + hex.EncodeToString(sum[:16])
}

func DeduplicateGaps(values []CandidateKnowledgeGap) []CandidateKnowledgeGap {
	result := make([]CandidateKnowledgeGap, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.DeterministicKey) == "" || seen[value.DeterministicKey] {
			continue
		}
		seen[value.DeterministicKey] = true
		result = append(result, value)
	}
	return result
}

// FilterKnownGaps prevents reopening a resolved CandidateUnknown. Pending
// unknowns are retained as existing work so an adapter can return the current
// clarification rather than creating another record.
func FilterKnownGaps(value candidate.Candidate, gaps []CandidateKnowledgeGap) []CandidateKnowledgeGap {
	result := make([]CandidateKnowledgeGap, 0, len(gaps))
	for _, gap := range DeduplicateGaps(gaps) {
		resolved := false
		for _, unknown := range value.Unknowns {
			matchesGap := unknown.GapKey != "" && unknown.GapKey == gap.DeterministicKey
			matchesEntity := unknown.RelatedEntity != "" && (unknown.RelatedEntity == gap.SubjectID || unknown.RelatedEntity == gap.Subject)
			if matchesGap || matchesEntity {
				if unknown.Status != candidate.CandidateUnknownNeedsConfirmation {
					resolved = true
				}
			}
		}
		if !resolved {
			result = append(result, gap)
		}
	}
	return result
}

func acquisitionFactAllowed(topic string) bool {
	lower := strings.ToLower(topic)
	return !containsAny(lower, "salary", "зарплат", "relocation", "релокац", "work_mode", "формат", "office", "офис", "availability", "доступност", "командиров")
}

func knowledgeGapForFact(value candidate.Candidate, original string, fact candidatecontext.ResolvedFact) CandidateKnowledgeGap {
	topic := strings.TrimSpace(fact.Topic)
	subjectType, field := KnowledgeSubjectSkill, "usage_context"
	subjectID := ""
	for _, skill := range value.Skills {
		if canonical(skill.Name) == canonical(topic) {
			subjectID = skill.ID
			break
		}
	}
	lowerQuestion := strings.ToLower(original)
	if containsAny(lowerQuestion, "что делали", "что именно", "какие задачи", "подробнее") {
		field = "usage_details"
	}
	if containsAny(lowerQuestion, "сколько человек", "подчин") {
		subjectType, field = KnowledgeSubjectExperience, "team_size"
	}
	if containsAny(lowerQuestion, "конфликт", "сложн", "трудн") && containsAny(lowerQuestion, "клиент", "заказчик", "ситуац") {
		subjectType, field = KnowledgeSubjectStory, "customer_conflict"
	}
	if subjectType == KnowledgeSubjectSkill && topic == "" {
		return CandidateKnowledgeGap{}
	}
	return CandidateKnowledgeGap{
		CandidateID: value.ID, SubjectType: subjectType, Subject: topic, SubjectID: subjectID,
		Field: field, Question: QuestionForGap(CandidateKnowledgeGap{SubjectType: subjectType, Subject: topic, Field: field}),
		Context: original, Priority: GapBlockingEmployerReply,
		DeterministicKey: GapKey(value.ID, subjectType, firstNonEmpty(subjectID, topic), field),
	}
}

func QuestionForGap(gap CandidateKnowledgeGap) string {
	if gap.SubjectType == KnowledgeSubjectStory {
		return "Был ли у тебя реальный случай сложного или конфликтного общения с клиентом? Кратко расскажи, что произошло, что ты сделал и чем закончилось."
	}
	if gap.SubjectType == KnowledgeSubjectExperience && gap.Field == "team_size" {
		return "Сколько человек было у тебя в подчинении? Если прямого управления не было, так и напиши."
	}
	if gap.Field == "usage_details" {
		return "Какие задачи ты выполнял с " + gap.Subject + "? Укажи только то, что действительно делал."
	}
	return "Работал ли ты с " + gap.Subject + "? Выбери точный вариант: коммерчески, в собственном проекте, в учебном проекте, только изучал или не использовал."
}

func ClarificationForKnowledgeGap(gap CandidateKnowledgeGap, refs CandidateKnowledgeGapContext) CandidateClarificationRequest {
	category := ClarificationSkillExperience
	shape := CandidateClarificationAnswerShape{Kind: "choice", Options: []CandidateClarificationOption{
		{ID: "commercial", Label: "Да, коммерчески"},
		{ID: "pet_project", Label: "Использовал в собственном проекте"},
		{ID: "educational", Label: "Использовал в учебном проекте"},
		{ID: "studied_only", Label: "Только изучал"},
		{ID: "explicitly_not_used", Label: "Не использовал"},
		{ID: "free_text", Label: "Другое — напишу сам"},
		{ID: "dismissed", Label: "Не хочу отвечать / не сохранять"},
	}}
	if gap.SubjectType == KnowledgeSubjectStory {
		category, shape = ClarificationBehavioralStory, CandidateClarificationAnswerShape{Kind: "free_text", Prompt: gap.Question}
	}
	if gap.SubjectType == KnowledgeSubjectExperience {
		category, shape = ClarificationWorkExperienceDetail, CandidateClarificationAnswerShape{Kind: "free_text", Prompt: gap.Question}
	}
	return CandidateClarificationRequest{
		ConversationID: refs.ConversationID, ApplicationID: refs.ApplicationID, VacancyID: refs.VacancyID, EmployerMessageID: refs.EmployerMessageID,
		Topic: gap.Subject, Question: gap.Question,
		Reason:    "Работодатель задал factual-вопрос, а canonical Candidate не содержит достаточного подтверждённого ответа.",
		UnknownID: "unknown-" + strings.TrimPrefix(gap.DeterministicKey, "gap-"), GapKey: gap.DeterministicKey,
		Category: category, OriginalEmployerQuestion: firstNonEmpty(gap.Context, gap.Question), SuggestedAnswerShape: shape,
	}
}

func hasCandidateStory(value candidate.Candidate, topic string) bool {
	for _, story := range value.Stories {
		text := strings.ToLower(strings.Join(append([]string{story.Title, story.Situation, story.Summary}, story.Tags...), " "))
		if topic == "customer_conflict" && containsAny(text, "конфликт", "сложн", "клиент", "заказчик") {
			return true
		}
	}
	return false
}

func containsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func canonical(value string) string { return candidatecontext.Canonical(value) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var ErrInvalidGap = errors.New("invalid candidate knowledge gap")
