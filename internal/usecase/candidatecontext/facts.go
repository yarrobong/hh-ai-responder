package candidatecontext

import (
	"fmt"
	"regexp"
	"strings"

	domaincandidate "hh-ai-responder/internal/candidate"
)

var questionMarkerRE = regexp.MustCompile(`[?？]`)

func InstructionNeedsUserConfirmation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lower, "ознаком") && strings.Contains(lower, "услов")
}

func UserConfirmationQuestion(text string) string {
	if InstructionNeedsUserConfirmation(text) {
		return "Ты ознакомился с условиями вакансии? Если да, можно ответить ровно «Да»."
	}
	return "Подтверди, пожалуйста, что именно нужно ответить работодателю."
}

func ClassifyEmployerMessage(text string) EmployerMessageIntent {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return EmployerMessageIntentGeneralMessage
	}
	if ContainsAny(lower, "вакансия закрыта", "вакансия закрыта для откликов", "позиция закрыта", "уже закрыли эту позицию", "эту позицию уже закрыли", "позиция уже закрыта", "набор завершен", "набор завершён", "найм завершен", "найм завершён", "подбор завершен", "подбор завершён", "место уже занято", "вакансия снята", "к сожалению, не готовы", "к сожалению, не можем продолжить", "не готовы продолжить", "выбрали другого кандидата", "другому кандидату сделали предложение", "position is closed", "vacancy is closed", "hiring completed", "role has been filled", "we have filled the position", "the position has been filled", "we decided not to proceed", "we will not proceed", "we chose another candidate") {
		return EmployerMessageIntentTerminal
	}
	if ContainsAny(lower, "к сожалению", "не готовы", "не можем предложить", "выбрали другого", "другому кандидату", "отказ") {
		return EmployerMessageIntentRejection
	}
	if ContainsAny(lower, "приглашаем на собеседование", "приглашаем на интервью", "интервью", "собеседовани", "следующий этап") && !ContainsAny(lower, "есть ли", "имеете ли", "какой опыт", "сколько лет", "как работали") {
		return EmployerMessageIntentInterviewInvitation
	}
	if ContainsAny(lower, "ответьте", "напишите", "заполните", "пройдите тест", "подтвердите получение", "ознакомились", "зарегистрируйтесь", "запишитесь") && !LooksLikeFactualQuestion(lower) {
		return EmployerMessageIntentInstruction
	}
	if ContainsAny(lower, "спасибо", "благодарю", "понятно", "хорошо", "принято") && len(strings.Fields(lower)) <= 8 {
		return EmployerMessageIntentAcknowledgement
	}
	if statusOrCourtesy(lower) {
		return EmployerMessageIntentStatusMessage
	}
	if LooksLikeFactualQuestion(lower) {
		if CountQuestionTopics(lower) > 1 {
			return EmployerMessageIntentCompound
		}
		return EmployerMessageIntentFactualQuestion
	}
	if CountQuestionTopics(lower) > 1 || questionMarkerRE.MatchString(lower) && strings.Contains(lower, " и ") {
		return EmployerMessageIntentCompound
	}
	return EmployerMessageIntentGeneralMessage
}

func statusOrCourtesy(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" || questionMarkerRE.MatchString(lower) || ContainsAny(lower, "ответьте", "напишите", "заполните", "пройдите тест", "подтвердите получение", "ознакомились", "зарегистрируйтесь", "запишитесь") {
		return false
	}
	return ContainsAny(lower, "рассмотрим ваше резюме", "рассмотрим резюме", "если навыки и опыт подойдут", "если навыки и опыт подойдут для позиции", "если подойдете, свяжемся", "если подойдёте, свяжемся", "если ваш опыт подойдет", "если ваш опыт подойдёт", "свяжемся с вами", "коллеги свяжутся", "спасибо за отклик", "спасибо за интерес к вакансии", "взяли резюме", "взяли ваше резюме", "на рассмотрении", "направлена на рассмотрение", "временно недоступна", "позиция приостановлена", "сообщим о решении", "положительного решения", "вернемся с обратной связью", "вернёмся с обратной связью")
}

func LooksLikeFactualQuestion(text string) bool {
	return ContainsAny(text, "есть ли", "имеете ли", "какой опыт", "какие навыки", "работали ли", "что делали", "что именно", "сколько лет", "сколько месяцев", "какая зарплата", "зарплат", "релокац", "переезд", "переехать", "удалён", "удален", "гибрид", "формат работы", "офис", "командиров", "образован", "английск", "english", "сертификат", "опыт с ", "опыт работы", "опыт ")
}
func CountQuestionTopics(text string) int {
	count := 0
	for _, marker := range []string{"djang", "python", "docker", "kubernetes", "k8s", "react", "postgres", "sql", "английск", "зарплат", "релокац", "переезд", "образован", "командиров", "офис", "удалён", "удален", "сколько лет", "сколько месяцев"} {
		if strings.Contains(text, marker) {
			count++
		}
	}
	return count
}

func resolvedFact(topic, requested string, status ResolvedFactStatus, value string, evidence ...string) ResolvedFact {
	claims := append([]string{}, evidence...)
	if strings.TrimSpace(value) != "" {
		claims = append([]string{value}, claims...)
	}
	return ResolvedFact{Topic: topic, RequestedFact: requested, Status: status, Value: value, Evidence: append([]string{}, evidence...), AllowedClaims: claims}
}

func profileSkill(view domaincandidate.EmployerSafeCandidateKnowledge, topic string) (domaincandidate.CandidateSkill, bool) {
	for _, skill := range view.Profile.Skills {
		if Canonical(skill.Name) == Canonical(topic) {
			return skill, true
		}
	}
	return domaincandidate.CandidateSkill{}, false
}
func detailedSkill(view domaincandidate.EmployerSafeCandidateKnowledge, topic string) (domaincandidate.CandidateSkillDetailed, bool) {
	for _, skill := range view.Skills {
		if Canonical(skill.Name) == Canonical(topic) {
			return skill, true
		}
	}
	return domaincandidate.CandidateSkillDetailed{}, false
}

func ResolveAtomicFacts(query string, view domaincandidate.EmployerSafeCandidateKnowledge) []ResolvedFact {
	lower := strings.ToLower(query)
	result := []ResolvedFact{}
	add := func(value ResolvedFact) { result = append(result, value) }
	if ContainsAny(lower, "зарплат", "salary", "оплата", "доход") {
		value := view.Profile.SalaryPreference
		if value == "" && view.Profile.SalaryMinimum != nil {
			value = fmt.Sprintf("минимум %d", *view.Profile.SalaryMinimum)
		}
		if value == "" {
			add(resolvedFact("salary", "зарплатные ожидания", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("salary", "зарплатные ожидания", ResolvedFactAnswerable, value, "Подтвержденные зарплатные предпочтения"))
		}
	}
	if ContainsAny(lower, "релокац", "переезд", "переехать", "переезжать") {
		if view.Profile.Relocation == "" {
			add(resolvedFact("relocation", "готовность к релокации", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("relocation", "готовность к релокации", ResolvedFactAnswerable, view.Profile.Relocation, "Подтвержденная настройка релокации"))
		}
	}
	if ContainsAny(lower, "удалён", "удален", "гибрид", "формат работы", "офис") {
		if view.Profile.WorkMode == "" {
			add(resolvedFact("work_mode", "предпочтительный формат работы", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("work_mode", "предпочтительный формат работы", ResolvedFactAnswerable, view.Profile.WorkMode, "Подтвержденные предпочтения формата работы"))
		}
	}
	if ContainsAny(lower, "командиров") {
		if view.Profile.BusinessTrips == "" {
			add(resolvedFact("business_trips", "готовность к командировкам", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("business_trips", "готовность к командировкам", ResolvedFactAnswerable, view.Profile.BusinessTrips, "Подтвержденная настройка командировок"))
		}
	}
	if ContainsAny(lower, "образован", "диплом", "среднее профессиональ") || Mentions(lower, "спо") {
		if len(view.Profile.Education) == 0 {
			add(resolvedFact("education", "образование", ResolvedFactUnknown, ""))
		} else {
			for _, education := range view.Profile.Education {
				value := strings.Trim(strings.Join([]string{education.Level, education.Specialty, education.Institution, education.Details}, ", "), ", ")
				add(resolvedFact("education", "образование", ResolvedFactAnswerable, value, "Подтвержденное образование кандидата"))
			}
		}
	}
	if ContainsAny(lower, "высш", "бакалавр", "магистр") {
		found := false
		for _, education := range view.Profile.Education {
			if ContainsAny(strings.ToLower(education.Level), "высш", "бакалавр", "магистр") {
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
	if ContainsAny(lower, "сколько лет", "сколько месяцев", "стаж", "общий опыт", "общий профессиональный опыт") {
		if view.Profile.TotalExperienceMonths == nil {
			add(resolvedFact("total_experience", "общий подтвержденный опыт", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("total_experience", "общий подтвержденный опыт", ResolvedFactAnswerable, fmt.Sprintf("%d месяцев", *view.Profile.TotalExperienceMonths), "Подтвержденный общий опыт кандидата"))
		}
	}
	if ContainsAny(lower, "английск", "english") {
		found := false
		for _, language := range view.Profile.Languages {
			if ContainsAny(strings.ToLower(language.Name), "англ", "english") && strings.TrimSpace(language.Level) != "" {
				add(resolvedFact("english", "уровень английского", ResolvedFactAnswerable, language.Level, "Подтвержденный уровень языка"))
				found = true
			}
		}
		if !found {
			add(resolvedFact("english", "уровень английского", ResolvedFactUnknown, ""))
		}
	}
	if ContainsAny(lower, "какая должност", "какие роли", "роль", "позици") {
		value := strings.Trim(strings.Join([]string{view.Profile.PrimaryRoles, view.Profile.SecondaryRoles, view.Profile.PreferredRoles}, "; "), "; ")
		if value == "" {
			add(resolvedFact("roles", "целевые роли", ResolvedFactUnknown, ""))
		} else {
			add(resolvedFact("roles", "целевые роли", ResolvedFactAnswerable, value, "Подтвержденные предпочтительные роли"))
		}
	}
	if ContainsAny(lower, "сертификат", "сертификац") {
		add(resolvedFact("certifications", "сертификаты", ResolvedFactUnknown, ""))
	}
	topicNames := append([]string{}, technologyNames...)
	for _, skill := range view.Skills {
		topicNames = appendUnique(topicNames, skill.Name)
	}
	for _, project := range view.Projects {
		for _, technology := range project.Technologies {
			topicNames = appendUnique(topicNames, technology)
		}
	}
	for _, skill := range view.Profile.Skills {
		topicNames = appendUnique(topicNames, skill.Name)
	}
	for _, project := range view.Profile.Projects {
		for _, technology := range project.Technologies {
			topicNames = appendUnique(topicNames, technology)
		}
	}
	topics := MatchingNames(query, topicNames)
	for _, topic := range explicitQuestionTopics(query) {
		if !HasName(topics, topic) {
			topics = append(topics, topic)
		}
	}
	for _, topic := range topics {
		result = append(result, resolveTechnologyFact(query, view, topic))
	}
	return deduplicateResolvedFacts(result)
}

func resolveTechnologyFact(query string, view domaincandidate.EmployerSafeCandidateKnowledge, topic string) ResolvedFact {
	lower := strings.ToLower(query)
	requested := "наличие опыта с " + topic
	if skill, ok := detailedSkill(view, topic); ok && skill.Negative {
		return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Подтверждено отсутствие навыка "+skill.Name)
	}
	if skill, ok := profileSkill(view, topic); ok && skill.Negative {
		return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Подтверждено отсутствие навыка "+skill.Name)
	}
	if skill, ok := detailedSkill(view, topic); ok {
		for _, use := range skill.Uses {
			if use.Context == domaincandidate.CanonicalSkillUsageExplicitlyNotUsed {
				return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Кандидат явно указал, что не использовал "+skill.Name)
			}
			if ContainsAny(lower, "коммерческ", "commercial") && use.Context == domaincandidate.CanonicalSkillUsageCommercial {
				return resolvedFact(topic, requested, ResolvedFactAnswerable, "коммерческий опыт подтверждён", "Явно подтверждённый контекст использования: commercial")
			}
			if !ContainsAny(lower, "production", "продакш", "проде", "коммерческ", "commercial", "сколько", "что делали", "что именно", "какие задачи", "подробнее") {
				return resolvedFact(topic, requested, ResolvedFactAnswerable, string(use.Context), "Явно подтверждённый контекст использования")
			}
		}
	}
	if ContainsAny(lower, "production", "продакш", "проде", "коммерческ", "commercial", "сколько лет", "сколько месяцев", "лет опыта", "месяцев опыта") {
		if Canonical(topic) == "kubernetes" {
			if avoidsKubernetesProduction(view) {
				return resolvedFact(topic, requested, ResolvedFactRestricted, "", "Кандидат не разрешает утверждать опыт Kubernetes production")
			}
			return resolvedFact(topic, requested, ResolvedFactUnknown, "")
		}
		if _, ok := detailedSkill(view, topic); ok {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "базовый опыт подтвержден", "Подтверждено владение технологией; требуемый production/commercial-контекст не подтвержден")
			fact.MissingPart = "production/commercial-контекст и длительность"
			return fact
		}
		if _, ok := profileSkill(view, topic); ok {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "базовый опыт подтвержден", "Подтверждено владение технологией; требуемый production/commercial-контекст не подтвержден")
			fact.MissingPart = "production/commercial-контекст и длительность"
			return fact
		}
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if Canonical(topic) == "kubernetes" {
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if ContainsAny(lower, "что делали", "что именно", "какие задачи", "подробнее") {
		if hasProjectEvidence(view, topic) {
			return resolvedFact(topic, requested, ResolvedFactAnswerable, "подтверждено проектами и задачами", "Подтвержденные проекты содержат задачи, технологии или результаты")
		}
		if skill, ok := detailedSkill(view, topic); ok && skill.Level != domaincandidate.SkillLevelUnknown && skill.Level != domaincandidate.SkillLevelHeardOf {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "наличие навыка подтверждено; детализация не найдена", "Базовый навык подтвержден, подробности не подтверждены")
			fact.MissingPart = "подробности задач и результатов"
			return fact
		}
		if skill, ok := profileSkill(view, topic); ok && skill.Level != domaincandidate.SkillLevelUnknown && skill.Level != domaincandidate.SkillLevelHeardOf {
			fact := resolvedFact(topic, requested, ResolvedFactPartiallyAnswerable, "наличие навыка подтверждено; детализация не найдена", "Базовый навык подтвержден, подробности не подтверждены")
			fact.MissingPart = "подробности задач и результатов"
			return fact
		}
		return resolvedFact(topic, requested, ResolvedFactUnknown, "")
	}
	if skill, ok := detailedSkill(view, topic); ok && skill.Level != domaincandidate.SkillLevelUnknown && skill.Level != domaincandidate.SkillLevelHeardOf {
		return resolvedFact(topic, requested, ResolvedFactAnswerable, string(skill.Level), "Подтвержденный навык "+skill.Name)
	}
	if skill, ok := profileSkill(view, topic); ok && skill.Level != domaincandidate.SkillLevelUnknown && skill.Level != domaincandidate.SkillLevelHeardOf {
		return resolvedFact(topic, requested, ResolvedFactAnswerable, string(skill.Level), "Подтвержденный навык "+skill.Name)
	}
	return resolvedFact(topic, requested, ResolvedFactUnknown, "")
}

func avoidsKubernetesProduction(view domaincandidate.EmployerSafeCandidateKnowledge) bool {
	for _, claim := range view.Profile.AvoidClaiming {
		lower := strings.ToLower(claim)
		if strings.Contains(lower, "kubernetes") && strings.Contains(lower, "production") {
			return true
		}
	}
	for _, skill := range view.Skills {
		if Canonical(skill.Name) == "kubernetes" {
			for _, claim := range skill.CannotClaim {
				if strings.Contains(strings.ToLower(claim), "production") {
					return true
				}
			}
		}
	}
	return false
}
func explicitQuestionTopics(query string) []string {
	lower := strings.ToLower(query)
	for _, marker := range []string{"с ", "with ", "про ", "по ", "использовали ", "опыт работы с ", "опыт с ", "опыт "} {
		index := strings.Index(lower, marker)
		if index < 0 {
			continue
		}
		value := strings.Trim(strings.TrimSpace(query[index+len(marker):]), " \t\r\n?!.:,;()[]{}")
		if marker == "с " && ContainsAny(strings.ToLower(value), "зарплат", "образован", "релокац", "переезд", "формат работы", "английск", "english") {
			continue
		}
		if value == "" || strings.EqualFold(value, "этим") || strings.EqualFold(value, "вами") {
			continue
		}
		parts := strings.Split(value, " и ")
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
func hasProjectEvidence(view domaincandidate.EmployerSafeCandidateKnowledge, topic string) bool {
	for _, project := range view.Projects {
		if HasName(project.Technologies, topic) || HasName(project.RelatedSkills, topic) {
			return len(project.Tasks)+len(project.Results) > 0 || strings.TrimSpace(project.Description) != ""
		}
	}
	for _, project := range view.Profile.Projects {
		if HasName(project.Technologies, topic) && (strings.TrimSpace(project.Description) != "" || strings.TrimSpace(project.BusinessImpact) != "") {
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
