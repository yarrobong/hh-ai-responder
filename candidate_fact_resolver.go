package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var questionMarkerRE = regexp.MustCompile(`[?？]`)
var externalURLRE = regexp.MustCompile(`https?://[^\s<>]+`)
var interviewDurationRE = regexp.MustCompile(`(?i)около\s+([0-9]+\s*[–-]\s*[0-9]+\s*минут)`)

func instructionNeedsUserConfirmation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lower, "ознаком") && strings.Contains(lower, "услов")
}

func userConfirmationQuestion(text string) string {
	if instructionNeedsUserConfirmation(text) {
		return "Ты ознакомился с условиями вакансии? Если да, можно ответить ровно «Да»."
	}
	return "Подтверди, пожалуйста, что именно нужно ответить работодателю."
}

func externalInterviewAction(text string, at, now time.Time) *ExternalActionRequirement {
	if classifyEmployerMessage(text) != EmployerMessageIntentInterviewInvitation {
		return nil
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "getprofi") && !strings.Contains(lower, "interview.getprofi") && !strings.Contains(lower, "http://") && !strings.Contains(lower, "https://") {
		return nil
	}
	action := &ExternalActionRequirement{
		Classification: ExternalActionRequired,
		Type:           ExternalActionInterviewInvitation,
		RequiredAction: "Перейти по внешней ссылке и пройти голосовое интервью.",
	}
	if destination := externalURLRE.FindString(text); destination != "" {
		action.Destination = strings.TrimRight(destination, ".,;:!?)]}")
	}
	if match := interviewDurationRE.FindStringSubmatch(text); len(match) == 2 {
		action.Duration = strings.ReplaceAll(match[1], "-", "–")
	}
	if strings.Contains(lower, "7–10 дней") || strings.Contains(lower, "7-10 дней") {
		action.ResultTiming = "Результат обещан в личном кабинете HH в течение 7–10 дней."
	}
	if !at.IsZero() && !now.IsZero() {
		action.Aging = pilotFreshness(at, now)
	}
	return action
}

func classifyEmployerMessage(text string) EmployerMessageIntent {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return EmployerMessageIntentGeneralMessage
	}
	if terminalEmployerMessage(lower) {
		return EmployerMessageIntentTerminal
	}
	if candidateContainsAny(lower, "к сожалению", "не готовы", "не можем предложить", "выбрали другого", "другому кандидату", "отказ") {
		return EmployerMessageIntentRejection
	}
	if candidateContainsAny(lower, "приглашаем на собеседование", "приглашаем на интервью", "интервью", "собеседовани", "следующий этап") && !candidateContainsAny(lower, "есть ли", "имеете ли", "какой опыт", "сколько лет", "как работали") {
		return EmployerMessageIntentInterviewInvitation
	}
	if candidateContainsAny(lower, "ответьте", "напишите", "заполните", "пройдите тест", "подтвердите получение", "ознакомились", "зарегистрируйтесь", "запишитесь") && !looksLikeFactualQuestion(lower) {
		return EmployerMessageIntentInstruction
	}
	if candidateContainsAny(lower, "спасибо", "благодарю", "понятно", "хорошо", "принято") && len(strings.Fields(lower)) <= 8 {
		return EmployerMessageIntentAcknowledgement
	}
	// Status/courtesy messages take precedence over the broad factual-topic
	// vocabulary below. For example, "если навыки и опыт подойдут, мы
	// свяжемся" contains the word "опыт", but it does not ask the candidate
	// for any information or action.
	if isEmployerStatusOrCourtesyMessage(lower) {
		return EmployerMessageIntentStatusMessage
	}
	if looksLikeFactualQuestion(lower) {
		if countQuestionTopics(lower) > 1 {
			return EmployerMessageIntentCompound
		}
		return EmployerMessageIntentFactualQuestion
	}
	if countQuestionTopics(lower) > 1 || questionMarkerRE.MatchString(lower) && strings.Contains(lower, " и ") {
		return EmployerMessageIntentCompound
	}
	return EmployerMessageIntentGeneralMessage
}

func isEmployerStatusOrCourtesyMessage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" || questionMarkerRE.MatchString(lower) {
		return false
	}
	if candidateContainsAny(lower, "ответьте", "напишите", "заполните", "пройдите тест", "подтвердите получение", "ознакомились", "зарегистрируйтесь", "запишитесь") {
		return false
	}
	return candidateContainsAny(lower,
		"рассмотрим ваше резюме", "рассмотрим резюме", "если навыки и опыт подойдут",
		"если навыки и опыт подойдут для позиции", "если подойдете, свяжемся", "если подойдёте, свяжемся",
		"если ваш опыт подойдет", "если ваш опыт подойдёт", "свяжемся с вами", "коллеги свяжутся",
		"спасибо за отклик", "спасибо за интерес к вакансии", "взяли резюме", "взяли ваше резюме",
		"на рассмотрении", "направлена на рассмотрение", "временно недоступна", "позиция приостановлена",
		"сообщим о решении", "положительного решения", "вернемся с обратной связью", "вернёмся с обратной связью")
}

// terminalEmployerMessage is intentionally deterministic and shared by all
// read/reply surfaces. AI intent classification cannot reopen a conversation
// that HH or the employer has already closed.
func terminalEmployerMessage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.NewReplacer("\u00a0", " ", "\u202f", " ").Replace(lower)
	return candidateContainsAny(lower,
		"вакансия закрыта", "вакансия закрыта для откликов", "позиция закрыта",
		"уже закрыли эту позицию", "эту позицию уже закрыли", "позиция уже закрыта",
		"набор завершен", "набор завершён", "найм завершен", "найм завершён",
		"подбор завершен", "подбор завершён", "место уже занято", "вакансия снята",
		"к сожалению, не готовы", "к сожалению, не можем продолжить", "не готовы продолжить",
		"выбрали другого кандидата", "другому кандидату сделали предложение",
		"position is closed", "vacancy is closed", "hiring completed", "role has been filled",
		"we have filled the position", "the position has been filled", "we decided not to proceed",
		"we will not proceed", "we chose another candidate")
}

func terminalConversationState(c EmployerConversation) (ConversationStatus, bool) {
	if c.Status == ConversationRejected || c.Status == ConversationClosed {
		return c.Status, true
	}
	if status, known := MapHHApplicationStatus(c.RawStatus); known {
		switch status {
		case ApplicationRejected:
			return ConversationRejected, true
		case ApplicationArchived:
			return ConversationClosed, true
		}
	}
	if latest := latestDeliveredMessage(c); latest != nil && latest.Sender == ConversationSenderEmployer && terminalEmployerMessage(latest.Text) {
		lower := strings.ToLower(latest.Text)
		if candidateContainsAny(lower, "не готовы", "не можем", "выбрали другого", "другому кандидату", "отказ", "position has been filled", "role has been filled", "not proceed", "another candidate") {
			return ConversationRejected, true
		}
		return ConversationClosed, true
	}
	return "", false
}

func conversationReplyRequirement(c EmployerConversation, latest *ConversationMessage, intent EmployerMessageIntent) ConversationReplyRequirement {
	if _, terminal := terminalConversationState(c); terminal {
		return NoReplyNeeded
	}
	if latest == nil || latest.Sender != ConversationSenderEmployer {
		return NoReplyNeeded
	}
	if intent == EmployerMessageIntentTerminal || intent == EmployerMessageIntentRejection {
		return NoReplyNeeded
	}
	if intent == EmployerMessageIntentInterviewInvitation && latest != nil && (strings.Contains(strings.ToLower(latest.Text), "getprofi") || strings.Contains(strings.ToLower(latest.Text), "http://") || strings.Contains(strings.ToLower(latest.Text), "https://")) {
		// A link-based invitation is an external action flow. Unless the
		// employer explicitly asks for confirmation, it does not require a
		// candidate text reply in HH.
		lower := strings.ToLower(latest.Text)
		if !candidateContainsAny(lower, "подтверд", "ответьте", "напишите", "соглас") {
			return NoReplyNeeded
		}
	}
	if intent == EmployerMessageIntentAcknowledgement || intent == EmployerMessageIntentStatusMessage {
		return ReplyOptional
	}
	return ReplyRequired
}

func looksLikeFactualQuestion(text string) bool {
	return candidateContainsAny(text, "есть ли", "имеете ли", "какой опыт", "какие навыки", "работали ли", "что делали", "что именно", "сколько лет", "сколько месяцев", "какая зарплата", "зарплат", "релокац", "переезд", "переехать", "удалён", "удален", "гибрид", "формат работы", "офис", "командиров", "образован", "английск", "english", "сертификат", "опыт с ", "опыт работы", "опыт ")
}

func countQuestionTopics(text string) int {
	count := 0
	for _, marker := range []string{"djang", "python", "docker", "kubernetes", "k8s", "react", "postgres", "sql", "английск", "зарплат", "релокац", "переезд", "образован", "командиров", "офис", "удалён", "удален", "сколько лет", "сколько месяцев"} {
		if strings.Contains(text, marker) {
			count++
		}
	}
	return count
}

func candidateContainsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func resolvedFact(topic, requested string, status ResolvedFactStatus, value string, evidence ...string) ResolvedFact {
	claims := append([]string{}, evidence...)
	if strings.TrimSpace(value) != "" {
		claims = append([]string{value}, claims...)
	}
	return ResolvedFact{Topic: topic, RequestedFact: requested, Status: status, Value: value, Evidence: append([]string{}, evidence...), AllowedClaims: claims}
}

func profileSkillForTopic(view EmployerSafeCandidateKnowledge, topic string) (CandidateSkill, bool) {
	for _, skill := range view.Profile.Skills {
		if contextCanonical(skill.Name) == contextCanonical(topic) {
			return skill, true
		}
	}
	return CandidateSkill{}, false
}

func detailedSkillForTopic(view EmployerSafeCandidateKnowledge, topic string) (CandidateSkillDetailed, bool) {
	for _, skill := range view.Skills {
		if contextCanonical(skill.Name) == contextCanonical(topic) {
			return skill, true
		}
	}
	return CandidateSkillDetailed{}, false
}

func resolveAtomicFacts(query string, view EmployerSafeCandidateKnowledge) []ResolvedFact {
	lower := strings.ToLower(query)
	result := []ResolvedFact{}
	add := func(value ResolvedFact) { result = append(result, value) }
	if candidateContainsAny(lower, "зарплат", "salary", "оплата", "доход") {
		value := view.Profile.SalaryPreference
		if value == "" && view.Profile.SalaryMinimum != nil {
			value = fmt.Sprintf("минимум %d", *view.Profile.SalaryMinimum)
		}
		if value == "" {
			add(resolvedFact("salary", "зарплатные ожидания", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("salary", "зарплатные ожидания", ResolvedFactAnswerable, value, "Подтвержденные зарплатные предпочтения кандидата"))
		}
	}
	if candidateContainsAny(lower, "релокац", "переезд", "переехать", "переезжать") {
		if view.Profile.Relocation == "" {
			add(resolvedFact("relocation", "готовность к релокации", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("relocation", "готовность к релокации", ResolvedFactAnswerable, view.Profile.Relocation, "Подтвержденная настройка релокации"))
		}
	}
	if candidateContainsAny(lower, "удалён", "удален", "гибрид", "формат работы", "офис") {
		if view.Profile.WorkMode == "" {
			add(resolvedFact("work_mode", "предпочтительный формат работы", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("work_mode", "предпочтительный формат работы", ResolvedFactAnswerable, view.Profile.WorkMode, "Подтвержденные предпочтения формата работы"))
		}
	}
	if candidateContainsAny(lower, "командиров") {
		if view.Profile.BusinessTrips == "" {
			add(resolvedFact("business_trips", "готовность к командировкам", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("business_trips", "готовность к командировкам", ResolvedFactAnswerable, view.Profile.BusinessTrips, "Подтвержденная настройка командировок"))
		}
	}
	if candidateContainsAny(lower, "образован", "диплом", "среднее профессиональ") || contextMentions(lower, "спо") {
		if len(view.Profile.Education) == 0 {
			add(resolvedFact("education", "образование", ResolvedFactUnknown, ""))
		} else {
			for _, education := range view.Profile.Education {
				value := strings.Trim(strings.Join([]string{education.Level, education.Specialty, education.Institution, education.Details}, ", "), ", ")
				add(resolvedFact("education", "образование", ResolvedFactAnswerable, value, "Подтвержденное образование кандидата"))
			}
		}
	}
	if candidateContainsAny(lower, "высш", "бакалавр", "магистр") {
		found := false
		for _, education := range view.Profile.Education {
			if candidateContainsAny(strings.ToLower(education.Level), "высш", "бакалавр", "магистр") {
				found = true
			}
		}
		if found {
			add(resolvedFact("higher_education", "высшее образование", ResolvedFactAnswerable, "подтверждено", "Подтвержденное высшее образование"))
		} else {
			value := ""
			if len(view.Profile.Education) > 0 {
				value = "СПО подтверждено; наличие высшего образования не подтверждено"
			}
			add(resolvedFact("higher_education", "высшее образование", ResolvedFactUnknown, value, "СПО нельзя использовать как доказательство высшего образования"))
		}
	}
	if candidateContainsAny(lower, "сколько лет", "сколько месяцев", "стаж", "общий опыт", "общий профессиональный опыт") {
		if view.Profile.TotalExperienceMonths == nil {
			add(resolvedFact("total_experience", "общий подтвержденный опыт", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("total_experience", "общий подтвержденный опыт", ResolvedFactAnswerable, fmt.Sprintf("%d месяцев", *view.Profile.TotalExperienceMonths), "Подтвержденный общий опыт кандидата"))
		}
	}
	if candidateContainsAny(lower, "английск", "english") {
		found := false
		for _, language := range view.Profile.Languages {
			if candidateContainsAny(strings.ToLower(language.Name), "англ", "english") && strings.TrimSpace(language.Level) != "" {
				add(resolvedFact("english", "уровень английского", ResolvedFactAnswerable, language.Level, "Подтвержденный уровень языка"))
				found = true
			}
		}
		if !found {
			add(resolvedFact("english", "уровень английского", ResolvedFactUnknown, ""))
		}
	}
	if candidateContainsAny(lower, "какая должност", "какие роли", "роль", "позици") {
		value := strings.Trim(strings.Join([]string{view.Profile.PrimaryRoles, view.Profile.SecondaryRoles, view.Profile.PreferredRoles}, "; "), "; ")
		if value == "" {
			add(resolvedFact("roles", "целевые роли", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("roles", "целевые роли", ResolvedFactAnswerable, value, "Подтвержденные предпочтительные роли"))
		}
	}
	if candidateContainsAny(lower, "сертификат", "сертификац") {
		add(resolvedFact("certifications", "сертификаты", ResolvedFactUnknown, ""))
	}

	topicNames := append([]string{}, contextTechnologyNames...)
	for _, skill := range view.Skills {
		topicNames = contextAppendUnique(topicNames, skill.Name)
	}
	for _, project := range view.Projects {
		for _, technology := range project.Technologies {
			topicNames = contextAppendUnique(topicNames, technology)
		}
	}
	for _, skill := range view.Profile.Skills {
		topicNames = contextAppendUnique(topicNames, skill.Name)
	}
	for _, project := range view.Profile.Projects {
		for _, technology := range project.Technologies {
			topicNames = contextAppendUnique(topicNames, technology)
		}
	}
	topics := contextMatchingNames(query, topicNames)
	for _, topic := range extractExplicitQuestionTopics(query) {
		if !contextHasName(topics, topic) {
			topics = append(topics, topic)
		}
	}
	for _, topic := range topics {
		result = append(result, resolveTechnologyFact(query, view, topic))
	}
	return deduplicateResolvedFacts(result)
}

func resolveTechnologyFact(query string, view EmployerSafeCandidateKnowledge, topic string) ResolvedFact {
	lower := strings.ToLower(query)
	requested := "наличие опыта с " + topic
	if skill, ok := detailedSkillForTopic(view, topic); ok && skill.Negative {
		return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Подтверждено отсутствие навыка "+skill.Name)
	}
	if skill, ok := profileSkillForTopic(view, topic); ok && skill.Negative {
		return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Подтверждено отсутствие навыка "+skill.Name)
	}
	if candidateContainsAny(lower, "production", "продакш", "проде", "коммерческ", "commercial", "сколько лет", "сколько месяцев", "лет опыта", "месяцев опыта") {
		if contextCanonical(topic) == "kubernetes" {
			if explicitAvoidsKubernetesProduction(view) {
				return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Кандидат не разрешает утверждать опыт Kubernetes production")
			}
			return resolvedFact(topic, requested, ResolvedFactUnknown, "")
		}
		if _, ok := detailedSkillForTopic(view, topic); ok || func() bool { _, ok := profileSkillForTopic(view, topic); return ok }() {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "базовый опыт подтвержден", "Подтверждено владение технологией; требуемый production/commercial-контекст не подтвержден")
			fact.MissingPart = "production/commercial-контекст и длительность"
			return fact
		}
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if contextCanonical(topic) == "kubernetes" {
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if candidateContainsAny(lower, "что делали", "что именно", "какие задачи", "подробнее") {
		if hasProjectEvidence(view, topic) {
			return resolvedFact(topic, requested, ResolvedFactAnswerable, "подтверждено проектами и задачами", "Подтвержденные проекты содержат задачи, технологии или результаты")
		}
		if skill, ok := detailedSkillForTopic(view, topic); ok && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "наличие навыка подтверждено; детализация не найдена", "Базовый навык подтвержден, подробности не подтверждены")
			fact.MissingPart = "подробности задач и результатов"
			return fact
		}
		if skill, ok := profileSkillForTopic(view, topic); ok && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "наличие навыка подтверждено; детализация не найдена", "Базовый навык подтвержден, подробности не подтверждены")
			fact.MissingPart = "подробности задач и результатов"
			return fact
		}
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if skill, ok := detailedSkillForTopic(view, topic); ok && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
		return resolvedFact(topic, requested, ResolvedFactAnswerable, string(skill.Level), "Подтвержденный навык "+skill.Name)
	}
	if skill, ok := profileSkillForTopic(view, topic); ok && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
		return resolvedFact(topic, requested, ResolvedFactAnswerable, string(skill.Level), "Подтвержденный навык "+skill.Name)
	}
	return resolvedFact(topic, requested, ResolvedFactUnknown, "")
}

func explicitAvoidsKubernetesProduction(view EmployerSafeCandidateKnowledge) bool {
	for _, claim := range view.Profile.AvoidClaiming {
		if strings.Contains(strings.ToLower(claim), "kubernetes") && strings.Contains(strings.ToLower(claim), "production") {
			return true
		}
	}
	for _, skill := range view.Skills {
		if contextCanonical(skill.Name) == "kubernetes" {
			for _, claim := range skill.CannotClaim {
				if strings.Contains(strings.ToLower(claim), "production") {
					return true
				}
			}
		}
	}
	return false
}

func extractExplicitQuestionTopic(query string) string {
	topics := extractExplicitQuestionTopics(query)
	if len(topics) > 0 {
		return topics[0]
	}
	return ""
}

func extractExplicitQuestionTopics(query string) []string {
	lower := strings.ToLower(query)
	for _, marker := range []string{"с ", "with ", "про ", "по ", "использовали ", "опыт работы с ", "опыт с ", "опыт "} {
		index := strings.Index(lower, marker)
		if index < 0 {
			continue
		}
		candidate := strings.Trim(strings.TrimSpace(query[index+len(marker):]), " \t\r\n?!.:,;()[]{}")
		if marker == "с " && candidateContainsAny(strings.ToLower(candidate), "зарплат", "образован", "релокац", "переезд", "формат работы", "английск", "english") {
			continue
		}
		if candidate == "" || strings.EqualFold(candidate, "этим") || strings.EqualFold(candidate, "вами") {
			continue
		}
		parts := strings.Split(candidate, " и ")
		result := []string{}
		for _, part := range parts {
			part = strings.Trim(strings.TrimSpace(part), " \t\r\n?!.:,;()[]{}")
			if part != "" && len(strings.Fields(part)) <= 2 {
				result = append(result, part)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return []string{}
}

func hasProjectEvidence(view EmployerSafeCandidateKnowledge, topic string) bool {
	for _, project := range view.Projects {
		if contextHasName(project.Technologies, topic) || contextHasName(project.RelatedSkills, topic) {
			return len(project.Tasks)+len(project.Results) > 0 || strings.TrimSpace(project.Description) != ""
		}
	}
	for _, project := range view.Profile.Projects {
		if contextHasName(project.Technologies, topic) && (strings.TrimSpace(project.Description) != "" || strings.TrimSpace(project.BusinessImpact) != "") {
			return true
		}
	}
	return false
}

func deduplicateResolvedFacts(values []ResolvedFact) []ResolvedFact {
	result := []ResolvedFact{}
	seen := map[string]bool{}
	for _, value := range values {
		key := value.Topic + "\x00" + value.RequestedFact
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}
