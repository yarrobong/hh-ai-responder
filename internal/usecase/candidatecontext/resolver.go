package candidatecontext

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	domaincandidate "hh-ai-responder/internal/candidate"
)

// Resolver is a deterministic, read-only candidate-context use case. The
// candidate is a prepared snapshot; the resolver never loads or persists it.
type Resolver struct {
	candidate *domaincandidate.Candidate
}

func NewResolver(value domaincandidate.Candidate) *Resolver {
	copy := value
	return &Resolver{candidate: &copy}
}

func NewEmptyResolver() *Resolver { return &Resolver{} }

func (r *Resolver) Resolve(input ResolveInput) (CandidateContext, error) {
	result := Empty()
	var view domaincandidate.EmployerSafeCandidateKnowledge
	if r != nil && r.candidate != nil {
		var err error
		view, err = domaincandidate.CanonicalEmployerSafeProjection(*r.candidate)
		if err != nil {
			return Empty(), errors.New("candidate context: unsafe canonical projection; context withheld")
		}
	}

	if input.EmployerMessage {
		result.MessageIntent = ClassifyEmployerMessage(input.Query)
	}
	for _, claim := range view.Profile.AvoidClaiming {
		result.ForbiddenClaims = appendUnique(result.ForbiddenClaims, claim)
	}
	for _, skill := range view.Skills {
		for _, claim := range skill.CannotClaim {
			result.ForbiddenClaims = appendUnique(result.ForbiddenClaims, claim)
		}
	}

	names := topicNames(view)
	topics := MatchingNames(input.Query, names)
	searchText := input.Query
	if input.EmployerMessage && strings.TrimSpace(input.Query) != "" && len(topics) == 0 && FollowUp(input.Query) {
		for i := len(input.History) - 1; i >= 0; i-- {
			if input.History[i].Hidden || strings.TrimSpace(input.History[i].Text) == "" {
				continue
			}
			topics = MatchingNames(input.History[i].Text, names)
			searchText += "\n" + input.History[i].Text
			break
		}
	}

	selectedSkills := map[string]bool{}
	knownTopics := map[string]bool{}
	negativeTopics := map[string]bool{}
	for _, skill := range view.Skills {
		if skill.Negative {
			negativeTopics[Canonical(skill.Name)] = true
		}
	}
	for _, skill := range view.Skills {
		if !skill.Negative && negativeTopics[Canonical(skill.Name)] {
			return Empty(), errors.New("candidate context: conflicting skill assertions")
		}
	}
	for _, project := range view.Projects {
		for _, name := range project.Technologies {
			if negativeTopics[Canonical(name)] {
				return Empty(), errors.New("candidate context: project conflicts with a negative skill assertion")
			}
		}
	}
	for _, achievement := range view.Achievements {
		for _, name := range achievement.Technologies {
			if negativeTopics[Canonical(name)] {
				return Empty(), errors.New("candidate context: achievement conflicts with a negative skill assertion")
			}
		}
	}

	for _, skill := range view.Skills {
		key := Canonical(skill.Name)
		if !HasName(topics, skill.Name) {
			continue
		}
		if negativeTopics[key] {
			result.ForbiddenClaims = appendUnique(result.ForbiddenClaims, "Опыт работы с "+skill.Name)
			result.AllowedFacts = appendUnique(result.AllowedFacts, "Подтверждено отсутствие навыка: "+skill.Name)
			knownTopics[key] = true
			continue
		}
		if skill.Level == domaincandidate.SkillLevelUnknown || skill.Level == domaincandidate.SkillLevelHeardOf {
			continue
		}
		knownTopics[key] = true
		selectedSkills[skill.ID] = true
		selectedSkills[key] = true
		result.RelevantSkills = appendUnique(result.RelevantSkills, skill.Name)
		result.AllowedFacts = appendUnique(result.AllowedFacts, "Навык: "+skill.Name+"; уровень: "+string(skill.Level))
		for _, wording := range skill.CanDo {
			result.AllowedFacts = appendUnique(result.AllowedFacts, skill.Name+": "+wording)
		}
	}
	for _, skill := range view.Profile.Skills {
		key := Canonical(skill.Name)
		if !HasName(topics, skill.Name) || selectedSkills[key] {
			continue
		}
		if skill.Negative {
			result.ForbiddenClaims = appendUnique(result.ForbiddenClaims, "Опыт работы с "+skill.Name)
			result.AllowedFacts = appendUnique(result.AllowedFacts, "Подтверждено отсутствие навыка: "+skill.Name)
			knownTopics[key] = true
			continue
		}
		selectedSkills[key] = true
		result.RelevantSkills = appendUnique(result.RelevantSkills, skill.Name)
		if skill.Level == domaincandidate.SkillLevelUnknown || skill.Level == domaincandidate.SkillLevelHeardOf {
			continue
		}
		knownTopics[key] = true
		result.AllowedFacts = appendUnique(result.AllowedFacts, "Навык: "+skill.Name+"; уровень: "+string(skill.Level))
	}

	selectedProjects := map[string]bool{}
	for _, project := range view.Projects {
		relevant := Mentions(searchText, project.Name)
		for _, name := range append(append([]string{}, project.Technologies...), project.RelatedSkills...) {
			relevant = relevant || HasName(topics, name) || selectedSkills[name] || selectedSkills[Canonical(name)]
		}
		for _, skill := range view.Skills {
			if selectedSkills[skill.ID] && (HasName(skill.Projects, project.ID) || HasName(skill.Projects, project.Name)) {
				relevant = true
			}
		}
		if !relevant {
			continue
		}
		selectedProjects[project.ID] = true
		knownTopics[Canonical(project.Name)] = true
		technologies := relevantTechnologies(project.Technologies, topics, negativeTopics)
		for _, technology := range technologies {
			knownTopics[Canonical(technology)] = true
			result.AllowedFacts = appendUnique(result.AllowedFacts, technology+" использовался в проекте "+project.Name)
		}
		result.RelevantProjects = append(result.RelevantProjects, CandidateContextProject{
			Name: project.Name, Role: project.Role, Description: project.Description,
			Technologies: cloneStrings(technologies), Tasks: cloneStrings(project.Tasks), Results: cloneStrings(project.Results),
		})
	}
	for _, project := range view.Profile.Projects {
		duplicate := false
		for _, existing := range view.Projects {
			if Canonical(existing.Name) == Canonical(project.Name) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		relevant := Mentions(searchText, project.Name)
		for _, name := range project.Technologies {
			relevant = relevant || HasName(topics, name) || selectedSkills[Canonical(name)]
		}
		if !relevant {
			continue
		}
		knownTopics[Canonical(project.Name)] = true
		technologies := relevantTechnologies(project.Technologies, topics, negativeTopics)
		result.RelevantProjects = append(result.RelevantProjects, CandidateContextProject{
			Name: project.Name, Role: project.Role, Description: project.Description,
			Technologies: cloneStrings(technologies), Results: nonEmptyStrings(project.BusinessImpact),
		})
		for _, technology := range technologies {
			knownTopics[Canonical(technology)] = true
			result.AllowedFacts = appendUnique(result.AllowedFacts, technology+" использовался в проекте "+project.Name)
		}
	}
	for _, achievement := range view.Achievements {
		technologies := relevantTechnologies(achievement.Technologies, topics, negativeTopics)
		if !selectedProjects[achievement.ProjectID] && len(technologies) == 0 && !Mentions(searchText, achievement.Title) {
			continue
		}
		knownTopics[Canonical(achievement.Title)] = true
		for _, technology := range technologies {
			knownTopics[Canonical(technology)] = true
		}
		result.RelevantAchievements = append(result.RelevantAchievements, CandidateContextAchievement{
			Title: achievement.Title, Problem: achievement.Problem, Solution: cloneStrings(achievement.Solution),
			Actions: cloneStrings(achievement.Actions), Result: cloneStrings(achievement.Result), Technologies: cloneStrings(technologies),
		})
	}

	for _, topic := range topics {
		if !knownTopics[Canonical(topic)] {
			result.AddMissing("Есть ли подтверждённый опыт «" + topic + "»?")
		}
	}
	if !input.EmployerMessage && strings.TrimSpace(input.Query) != "" && len(topics) == 0 {
		result.AddMissing("Какие подтверждённые сведения кандидата отвечают на этот запрос?")
	}
	if !input.EmployerMessage && NeedsDetails(input.Query) {
		result.AddMissing("Подтвердите запрошенные условия, длительность или уровень опыта: наличие навыка само по себе их не доказывает.")
	}
	if input.EmployerMessage {
		for _, fact := range ResolveAtomicFacts(searchText, view) {
			switch fact.Status {
			case ResolvedFactAnswerable:
				result.ResolvedFacts = append(result.ResolvedFacts, fact)
				result.AllowedFacts = appendFactClaims(result.AllowedFacts, fact)
			case ResolvedFactPartiallyAnswerable:
				result.PartiallyResolvedFacts = append(result.PartiallyResolvedFacts, fact)
				result.AllowedFacts = appendFactClaims(result.AllowedFacts, fact)
				if fact.MissingPart != "" {
					result.AddMissing("Не подтверждено: " + fact.MissingPart + " для " + fact.Topic)
				}
			case ResolvedFactRestricted:
				result.RestrictedFacts = append(result.RestrictedFacts, fact)
				result.ForbiddenClaims = appendUnique(result.ForbiddenClaims, fact.Topic)
			case ResolvedFactUnknown:
				result.UnknownAtomicFacts = append(result.UnknownAtomicFacts, fact)
				if fact.RequestedFact != "" {
					result.AddMissing("Уточните подтвержденные сведения: " + fact.RequestedFact)
				}
			}
		}
		if result.MessageIntent != EmployerMessageIntentFactualQuestion && result.MessageIntent != EmployerMessageIntentCompound {
			result.MissingInformation = []CandidateMissingInformation{}
			result.UnknownAtomicFacts = []ResolvedFact{}
		}
		if (result.MessageIntent == EmployerMessageIntentFactualQuestion || result.MessageIntent == EmployerMessageIntentCompound) &&
			len(result.ResolvedFacts) == 0 && len(result.PartiallyResolvedFacts) == 0 && len(result.UnknownAtomicFacts) == 0 && len(result.RestrictedFacts) == 0 {
			result.UnsupportedIntent = true
		}
		if result.MessageIntent == EmployerMessageIntentInstruction && InstructionNeedsUserConfirmation(input.Query) {
			result.UserConfirmationRequired = true
			result.UserConfirmationQuestion = UserConfirmationQuestion(input.Query)
			result.MissingInformation = []CandidateMissingInformation{{Question: result.UserConfirmationQuestion}}
		}
	}
	if raw, err := json.Marshal(result); err != nil || containsSecretMarker(raw) {
		return Empty(), errors.New("candidate context: unsafe content; context withheld")
	}
	return result, nil
}

func appendFactClaims(values []string, fact ResolvedFact) []string {
	for _, claim := range append(append([]string{}, fact.AllowedClaims...), fact.Value) {
		values = appendUnique(values, claim)
	}
	return values
}

var technologyNames = []string{"Python", "Django", "FastAPI", "Flask", "Kubernetes", "Docker", "Linux", "React", "JavaScript", "TypeScript", "Go", "Java", "C", "C++", "C#", ".NET", "SQL", "PostgreSQL", "MySQL", "Redis", "Kafka", "Celery", "Git", "GitHub Actions", "Jenkins", "REST API", "XML", "AWS", "Terraform", "Ansible", "RabbitMQ"}

func TechnologyNames() []string { return append([]string{}, technologyNames...) }
func Tokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#' && r != '.'
	})
}

func topicNames(view domaincandidate.EmployerSafeCandidateKnowledge) []string {
	names := append([]string{}, technologyNames...)
	for _, skill := range view.Skills {
		names = appendUnique(names, skill.Name)
	}
	for _, project := range view.Projects {
		names = appendUnique(names, project.Name)
		for _, name := range project.Technologies {
			names = appendUnique(names, name)
		}
	}
	for _, achievement := range view.Achievements {
		names = appendUnique(names, achievement.Title)
		for _, name := range achievement.Technologies {
			names = appendUnique(names, name)
		}
	}
	return names
}

func relevantTechnologies(names, topics []string, negative map[string]bool) []string {
	result := []string{}
	for _, name := range names {
		if HasName(topics, name) && !negative[Canonical(name)] {
			result = appendUnique(result, name)
		}
	}
	return result
}

func cloneStrings(values []string) []string { return append([]string{}, values...) }
func nonEmptyStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}
func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value != "" && !HasName(values, value) {
		values = append(values, value)
	}
	return values
}
func HasName(names []string, name string) bool {
	want := Canonical(name)
	for _, existing := range names {
		if Canonical(existing) == want && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}
func MatchingNames(query string, names []string) []string {
	result := []string{}
	normalized := mentionText(query)
	for _, name := range names {
		if want := Canonical(name); want != "" && strings.Contains(normalized, " "+want+" ") {
			result = appendUnique(result, name)
		}
	}
	return result
}
func Mentions(text, name string) bool {
	want := Canonical(name)
	return want != "" && strings.Contains(mentionText(text), " "+want+" ")
}
func Canonical(value string) string {
	tokens := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#' && r != '.'
	})
	for i := range tokens {
		tokens[i] = strings.TrimRight(tokens[i], ".")
	}
	result := strings.Join(tokens, " ")
	switch result {
	case "golang":
		return "go"
	case "k8s":
		return "kubernetes"
	case "postgres":
		return "postgresql"
	}
	return result
}
func mentionText(text string) string {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#' && r != '.'
	})
	for i, token := range tokens {
		tokens[i] = Canonical(strings.TrimRight(token, "."))
	}
	return " " + strings.Join(tokens, " ") + " "
}
func ContainsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
func FollowUp(query string) bool {
	for _, marker := range []string{"а с этим", "с ним", "с ней", "с ними", "а сколько", "как долго", "подробнее", "расскажите об этом", "what about it", "how long", "tell me more"} {
		if Mentions(query, marker) {
			return true
		}
	}
	return false
}
func NeedsDetails(query string) bool {
	text := strings.ToLower(query)
	for _, marker := range []string{"production", "продакш", "проде", "коммерческ", "commercial", "senior", "highload", "лет", "года", "год ", "месяц", "years", "months", "сколько", "зарплат", "salary", "релокац", "переезд", "собеседован", "interview", "документ", "documents", "договор", "контракт", "банк", "bank", "паспорт", "credentials", "парол", "выйти на работу", "доступност"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
func TopicNames(view domaincandidate.EmployerSafeCandidateKnowledge) []string {
	return topicNames(view)
}
func RelevantTechnologies(names, topics []string, negative map[string]bool) []string {
	return relevantTechnologies(names, topics, negative)
}
func SimpleSkillQuestion(query string, topics []string) bool {
	if len(topics) != 1 {
		return false
	}
	tokens := strings.Fields(mentionText(query))
	normalized := strings.TrimSpace(strings.Join(tokens, " "))
	name := Canonical(topics[0])
	for _, prefix := range []string{"работали ли вы с ", "есть опыт ", "есть ли опыт ", "есть ли у вас опыт ", "есть ли опыт работы с ", "есть ли у вас опыт работы с ", "do you have experience with ", "have you worked with "} {
		if normalized == prefix+name {
			return true
		}
	}
	return false
}

func containsSecretMarker(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
