package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"
)

// CanonicalCandidateProvider builds the in-memory read model from already
// loaded legacy values. It deliberately has no persistence or network
// behavior.
type CanonicalCandidateProvider struct {
	input CanonicalCandidateInput
}

func NewCanonicalCandidateProvider(input CanonicalCandidateInput) *CanonicalCandidateProvider {
	return &CanonicalCandidateProvider{input: input}
}

func (p *CanonicalCandidateProvider) Candidate() (Candidate, error) {
	candidate, _, err := p.CandidateWithDiagnostics()
	return candidate, err
}

func (p *CanonicalCandidateProvider) CandidateWithDiagnostics() (Candidate, CanonicalCandidateDiagnostics, error) {
	if p == nil {
		return Candidate{}, CanonicalCandidateDiagnostics{}, errors.New("canonical candidate provider is nil")
	}
	return BuildCanonicalCandidate(p.input)
}

// CandidateContextResolver performs local reads only. The legacy constructor
// remains for compatibility, but every resolution first builds a canonical
// candidate and then projects employer-safe knowledge from it. As with the
// knowledge base, the caller must serialize access if another goroutine
// updates it.
type CandidateContextResolver struct {
	kb        *CandidateKnowledgeBase // compatibility source; never read directly by resolve
	candidate *Candidate
}

func NewCandidateContextResolver(kb *CandidateKnowledgeBase) *CandidateContextResolver {
	return &CandidateContextResolver{kb: kb}
}

// NewCandidateContextResolverFromCandidate is the canonical production
// boundary for callers that already have an immutable read snapshot.
func NewCandidateContextResolverFromCandidate(candidate Candidate) *CandidateContextResolver {
	copy, err := cloneKnowledge(candidate)
	if err != nil {
		// Candidate is an in-memory value produced by BuildCanonicalCandidate;
		// retain a detached value even if defensive cloning ever fails.
		copy = candidate
	}
	return &CandidateContextResolver{candidate: &copy}
}

func (r *CandidateContextResolver) canonicalCandidate() (Candidate, CanonicalCandidateDiagnostics, error) {
	if r == nil {
		return Candidate{}, CanonicalCandidateDiagnostics{}, nil
	}
	if r.candidate != nil {
		copy, err := cloneKnowledge(*r.candidate)
		if err != nil {
			return Candidate{}, CanonicalCandidateDiagnostics{}, errors.New("candidate context: canonical candidate unavailable")
		}
		return copy, CanonicalCandidateDiagnostics{}, nil
	}
	if r.kb == nil {
		return Candidate{}, CanonicalCandidateDiagnostics{}, nil
	}
	// Rebuild on each logical read so legacy writers remain authoritative and
	// updates made after resolver construction are visible without a cache.
	knowledge := *r.kb
	// Unknowns, proposals and events are never employer-facing. Keep malformed
	// legacy audit material from blocking an otherwise valid safe projection;
	// their own storage validation remains unchanged.
	knowledge.Unknowns = nil
	knowledge.Proposals = nil
	knowledge.Events = nil
	return NewCanonicalCandidateProvider(CanonicalCandidateInput{
		Profile:   r.kb.Profile,
		Knowledge: knowledge,
	}).CandidateWithDiagnostics()
}

func (r *CandidateContextResolver) employerSafeView() (EmployerSafeCandidateKnowledge, error) {
	candidate, diagnostics, err := r.canonicalCandidate()
	if err != nil {
		return EmployerSafeCandidateKnowledge{}, errors.New("candidate context: canonical candidate unavailable")
	}
	if len(diagnostics.Conflicts) > 0 {
		return EmployerSafeCandidateKnowledge{}, errors.New("candidate context: critical candidate conflict; context withheld")
	}
	view, err := CanonicalEmployerSafeProjection(candidate)
	if err != nil {
		return EmployerSafeCandidateKnowledge{}, errors.New("candidate context: unsafe canonical projection; context withheld")
	}
	// Preserve the old detailed-store display order only as a compatibility
	// presentation detail. Canonical identity, claims and conflict resolution
	// remain independent of source collection ordering.
	if r.kb != nil && len(view.Skills) > 1 {
		order := make(map[string]int, len(r.kb.Skills))
		for index, skill := range r.kb.Skills {
			order[contextCanonical(skill.Name)] = index
		}
		sort.SliceStable(view.Skills, func(i, j int) bool {
			return order[contextCanonical(view.Skills[i].Name)] < order[contextCanonical(view.Skills[j].Name)]
		})
	}
	return view, nil
}

func (r *CandidateContextResolver) employerSafeKnowledge() (EmployerSafeKnowledge, error) {
	view, err := r.employerSafeView()
	if err != nil {
		return EmployerSafeKnowledge{}, err
	}
	return EmployerSafeKnowledge{Skills: view.Skills, Projects: view.Projects, Achievements: view.Achievements}, nil
}

func (r *CandidateContextResolver) canonicalTotalExperience() (ProfileIntFact, bool, error) {
	candidate, diagnostics, err := r.canonicalCandidate()
	if err != nil {
		return ProfileIntFact{}, false, err
	}
	if len(diagnostics.Conflicts) > 0 {
		return ProfileIntFact{}, false, errors.New("candidate context: critical candidate conflict; context withheld")
	}
	return candidate.Profile.TotalExperienceMonths, safeProfileFact(candidate.Profile.TotalExperienceMonths.ProfileFact), nil
}

// Vacancy has no description field. Optional texts carry an already-read
// description and/or requirements; the resolver never fetches HH data itself.
func (r *CandidateContextResolver) ResolveForVacancy(v Vacancy, descriptionAndRequirements ...string) (CandidateContext, error) {
	parts := append([]string{v.Name, v.WorkExperience, v.WorkSchedule}, descriptionAndRequirements...)
	return r.GetEmployerSafeContext(strings.Join(parts, "\n"))
}

// ResolveForVacancyContext is used for conversation/application context. A
// vacancy requirement is not a question from the employer and must never by
// itself create a candidate clarification. Matching can still inspect the
// returned allowed/restricted facts.
func (r *CandidateContextResolver) ResolveForVacancyContext(v Vacancy, descriptionAndRequirements ...string) (CandidateContext, error) {
	parts := append([]string{v.Name, v.WorkExperience, v.WorkSchedule}, descriptionAndRequirements...)
	result, err := r.resolve(strings.Join(parts, "\n"), nil, false)
	if err != nil {
		return CandidateContext{}, err
	}
	result.MissingInformation = []CandidateMissingInformation{}
	result.UnknownAtomicFacts = []ResolvedFact{}
	return result, nil
}

// History is only a relevance hint, never evidence. Use its most recent visible
// text only for follow-ups without an explicit topic in the current message.
func (r *CandidateContextResolver) ResolveForEmployerMessage(message string, history []ChatMessage) (CandidateContext, error) {
	return r.resolve(message, history, true)
}

// GetEmployerSafeContext selects from the unified validated projection. It
// never reads unknowns, proposals, events, derived values or stories.
func (r *CandidateContextResolver) GetEmployerSafeContext(query string) (CandidateContext, error) {
	return r.resolve(query, nil, false)
}

func (r *CandidateContextResolver) resolve(query string, history []ChatMessage, employerMessage bool) (CandidateContext, error) {
	result := emptyCandidateContext()
	var safe EmployerSafeKnowledge
	var view EmployerSafeCandidateKnowledge
	if r != nil && (r.kb != nil || r.candidate != nil) {
		var err error
		view, err = r.employerSafeView()
		if err != nil {
			return emptyCandidateContext(), errors.New("candidate context: invalid knowledge; context withheld")
		}
		safe = EmployerSafeKnowledge{Skills: view.Skills, Projects: view.Projects, Achievements: view.Achievements}
		for _, claim := range view.Profile.AvoidClaiming {
			result.ForbiddenClaims = contextAppendUnique(result.ForbiddenClaims, claim)
		}
	}
	if employerMessage {
		result.MessageIntent = classifyEmployerMessage(query)
	}
	// Restrictions survive relevance filtering, so a narrow query cannot drop
	// an explicit prohibition. No unconfirmed restriction is read from the KB.
	for _, skill := range safe.Skills {
		for _, claim := range skill.CannotClaim {
			result.ForbiddenClaims = contextAppendUnique(result.ForbiddenClaims, claim)
		}
	}

	names := contextTopicNames(safe)
	for _, skill := range view.Profile.Skills {
		names = contextAppendUnique(names, skill.Name)
	}
	for _, project := range view.Profile.Projects {
		names = contextAppendUnique(names, project.Name)
		for _, technology := range project.Technologies {
			names = contextAppendUnique(names, technology)
		}
	}
	topics := contextMatchingNames(query, names)
	searchText := query
	if employerMessage && strings.TrimSpace(query) != "" && len(topics) == 0 && contextFollowUp(query) {
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Hidden || strings.TrimSpace(history[i].Text) == "" {
				continue
			}
			topics = contextMatchingNames(history[i].Text, names)
			searchText += "\n" + history[i].Text
			break
		}
	}
	selectedSkills := map[string]bool{}
	knownTopics := map[string]bool{}
	negativeTopics := map[string]bool{}
	for _, skill := range safe.Skills {
		if skill.Negative {
			negativeTopics[contextCanonical(skill.Name)] = true
		}
	}
	// Contradictory trusted records are not resolved by source order. Withhold
	// the whole context rather than exposing incompatible claims to the AI.
	for _, skill := range safe.Skills {
		if !skill.Negative && negativeTopics[contextCanonical(skill.Name)] {
			return emptyCandidateContext(), errors.New("candidate context: conflicting skill assertions")
		}
	}
	for _, project := range safe.Projects {
		for _, name := range project.Technologies {
			if negativeTopics[contextCanonical(name)] {
				return emptyCandidateContext(), errors.New("candidate context: project conflicts with a negative skill assertion")
			}
		}
	}
	for _, achievement := range safe.Achievements {
		for _, name := range achievement.Technologies {
			if negativeTopics[contextCanonical(name)] {
				return emptyCandidateContext(), errors.New("candidate context: achievement conflicts with a negative skill assertion")
			}
		}
	}
	for _, skill := range safe.Skills {
		key := contextCanonical(skill.Name)
		if !contextHasName(topics, skill.Name) {
			continue
		}
		if negativeTopics[key] {
			result.ForbiddenClaims = contextAppendUnique(result.ForbiddenClaims, "Опыт работы с "+skill.Name)
			result.AllowedFacts = contextAppendUnique(result.AllowedFacts, "Подтверждено отсутствие навыка: "+skill.Name)
			knownTopics[key] = true
			continue
		}
		if skill.Level == SkillLevelUnknown || skill.Level == SkillLevelHeardOf {
			continue
		}
		knownTopics[key] = true
		selectedSkills[skill.ID] = true
		selectedSkills[key] = true
		result.RelevantSkills = contextAppendUnique(result.RelevantSkills, skill.Name)
		result.AllowedFacts = contextAppendUnique(result.AllowedFacts, "Навык: "+skill.Name+"; уровень: "+string(skill.Level))
		for _, wording := range skill.CanDo {
			result.AllowedFacts = contextAppendUnique(result.AllowedFacts, skill.Name+": "+wording)
		}
	}
	// Legacy profile skills are a trusted fallback when detailed migration has
	// not run yet. This is a projection only; it does not mutate the KB.
	for _, skill := range view.Profile.Skills {
		key := contextCanonical(skill.Name)
		if !contextHasName(topics, skill.Name) || selectedSkills[key] {
			continue
		}
		if skill.Negative {
			result.ForbiddenClaims = contextAppendUnique(result.ForbiddenClaims, "Опыт работы с "+skill.Name)
			result.AllowedFacts = contextAppendUnique(result.AllowedFacts, "Подтверждено отсутствие навыка: "+skill.Name)
			knownTopics[key] = true
			continue
		}
		selectedSkills[key] = true
		result.RelevantSkills = contextAppendUnique(result.RelevantSkills, skill.Name)
		if skill.Level == SkillLevelUnknown || skill.Level == SkillLevelHeardOf {
			continue
		}
		knownTopics[key] = true
		result.AllowedFacts = contextAppendUnique(result.AllowedFacts, "Навык: "+skill.Name+"; уровень: "+string(skill.Level))
	}
	selectedProjects := map[string]bool{}
	for _, project := range safe.Projects {
		relevant := contextMentions(searchText, project.Name)
		for _, name := range append(append([]string{}, project.Technologies...), project.RelatedSkills...) {
			relevant = relevant || contextHasName(topics, name) || selectedSkills[name] || selectedSkills[contextCanonical(name)]
		}
		for _, skill := range safe.Skills {
			if selectedSkills[skill.ID] && (contextHasName(skill.Projects, project.ID) || contextHasName(skill.Projects, project.Name)) {
				relevant = true
			}
		}
		if !relevant {
			continue
		}
		selectedProjects[project.ID] = true
		knownTopics[contextCanonical(project.Name)] = true
		technologies := contextRelevantTechnologies(project.Technologies, topics, negativeTopics)
		for _, technology := range technologies {
			knownTopics[contextCanonical(technology)] = true
			result.AllowedFacts = contextAppendUnique(result.AllowedFacts, technology+" использовался в проекте "+project.Name)
		}
		result.RelevantProjects = append(result.RelevantProjects, CandidateContextProject{
			Name: project.Name, Role: project.Role, Description: project.Description,
			Technologies: technologies, Tasks: project.Tasks, Results: project.Results,
		})
	}
	for _, project := range view.Profile.Projects {
		duplicate := false
		for _, existing := range safe.Projects {
			if contextCanonical(existing.Name) == contextCanonical(project.Name) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		relevant := contextMentions(searchText, project.Name)
		for _, name := range project.Technologies {
			relevant = relevant || contextHasName(topics, name) || selectedSkills[contextCanonical(name)]
		}
		if !relevant {
			continue
		}
		knownTopics[contextCanonical(project.Name)] = true
		technologies := contextRelevantTechnologies(project.Technologies, topics, negativeTopics)
		result.RelevantProjects = append(result.RelevantProjects, CandidateContextProject{
			Name: project.Name, Role: project.Role, Description: project.Description,
			Technologies: technologies, Results: []string{project.BusinessImpact},
		})
		for _, technology := range technologies {
			knownTopics[contextCanonical(technology)] = true
			result.AllowedFacts = contextAppendUnique(result.AllowedFacts, technology+" использовался в проекте "+project.Name)
		}
	}
	for _, achievement := range safe.Achievements {
		technologies := contextRelevantTechnologies(achievement.Technologies, topics, negativeTopics)
		if !selectedProjects[achievement.ProjectID] && len(technologies) == 0 && !contextMentions(searchText, achievement.Title) {
			continue
		}
		knownTopics[contextCanonical(achievement.Title)] = true
		for _, technology := range technologies {
			knownTopics[contextCanonical(technology)] = true
		}
		result.RelevantAchievements = append(result.RelevantAchievements, CandidateContextAchievement{
			Title: achievement.Title, Problem: achievement.Problem, Solution: achievement.Solution,
			Actions: achievement.Actions, Result: achievement.Result, Technologies: technologies,
		})
	}
	for _, topic := range topics {
		if !knownTopics[contextCanonical(topic)] {
			result.addMissing("Есть ли подтверждённый опыт «" + topic + "»?")
		}
	}
	if !employerMessage && strings.TrimSpace(query) != "" && len(topics) == 0 {
		result.addMissing("Какие подтверждённые сведения кандидата отвечают на этот запрос?")
	}
	// A technology name does not prove production use, duration, seniority,
	// availability or any other requested condition. Keep these unresolved.
	if !employerMessage && contextNeedsDetails(query) {
		result.addMissing("Подтвердите запрошенные условия, длительность или уровень опыта: наличие навыка само по себе их не доказывает.")
	}
	if employerMessage {
		facts := resolveAtomicFacts(searchText, view)
		for _, fact := range facts {
			switch fact.Status {
			case ResolvedFactAnswerable:
				result.ResolvedFacts = append(result.ResolvedFacts, fact)
				result.AllowedFacts = appendFactClaims(result.AllowedFacts, fact)
			case ResolvedFactPartiallyAnswerable:
				result.PartiallyResolvedFacts = append(result.PartiallyResolvedFacts, fact)
				result.AllowedFacts = appendFactClaims(result.AllowedFacts, fact)
				if fact.MissingPart != "" {
					result.addMissing("Не подтверждено: " + fact.MissingPart + " для " + fact.Topic)
				}
			case ResolvedFactRestricted:
				result.RestrictedFacts = append(result.RestrictedFacts, fact)
				result.ForbiddenClaims = contextAppendUnique(result.ForbiddenClaims, fact.Topic)
			case ResolvedFactUnknown:
				result.UnknownAtomicFacts = append(result.UnknownAtomicFacts, fact)
				if fact.RequestedFact != "" {
					result.addMissing("Уточните подтвержденные сведения: " + fact.RequestedFact)
				}
			}
		}
		// Instructions, status messages and invitations do not ask for a
		// candidate fact, even if their wording contains technology names.
		if result.MessageIntent != EmployerMessageIntentFactualQuestion && result.MessageIntent != EmployerMessageIntentCompound {
			result.MissingInformation = []CandidateMissingInformation{}
			result.UnknownAtomicFacts = []ResolvedFact{}
		}
		if (result.MessageIntent == EmployerMessageIntentFactualQuestion || result.MessageIntent == EmployerMessageIntentCompound) &&
			len(result.ResolvedFacts) == 0 && len(result.PartiallyResolvedFacts) == 0 && len(result.UnknownAtomicFacts) == 0 && len(result.RestrictedFacts) == 0 {
			result.UnsupportedIntent = true
		}
		if result.MessageIntent == EmployerMessageIntentInstruction && instructionNeedsUserConfirmation(query) {
			result.UserConfirmationRequired = true
			result.UserConfirmationQuestion = userConfirmationQuestion(query)
			result.MissingInformation = []CandidateMissingInformation{{Question: result.UserConfirmationQuestion}}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil || profileContainsSecret(raw) {
		return emptyCandidateContext(), errors.New("candidate context: unsafe content; context withheld")
	}
	return result, nil
}

func appendFactClaims(values []string, fact ResolvedFact) []string {
	for _, claim := range append(append([]string{}, fact.AllowedClaims...), fact.Value) {
		values = contextAppendUnique(values, claim)
	}
	return values
}

func (c *CandidateContext) addMissing(question string) {
	for _, existing := range c.MissingInformation {
		if existing.Question == question {
			return
		}
	}
	c.MissingInformation = append(c.MissingInformation, CandidateMissingInformation{Question: question})
}

// This is a vocabulary for recognizing questions, not candidate knowledge and
// not an implication graph (Python never implies Django, Docker never K8s).
var contextTechnologyNames = []string{
	"Python", "Django", "FastAPI", "Flask", "Kubernetes", "Docker", "Linux", "React",
	"JavaScript", "TypeScript", "Go", "Java", "C", "C++", "C#", ".NET", "SQL",
	"PostgreSQL", "MySQL", "Redis", "Kafka", "Celery", "Git", "GitHub Actions",
	"Jenkins", "REST API", "XML", "AWS", "Terraform", "Ansible", "RabbitMQ",
}

func contextTopicNames(safe EmployerSafeKnowledge) []string {
	names := append([]string{}, contextTechnologyNames...)
	for _, skill := range safe.Skills {
		names = contextAppendUnique(names, skill.Name)
	}
	for _, project := range safe.Projects {
		names = contextAppendUnique(names, project.Name)
		for _, name := range project.Technologies {
			names = contextAppendUnique(names, name)
		}
	}
	for _, achievement := range safe.Achievements {
		names = contextAppendUnique(names, achievement.Title)
		for _, name := range achievement.Technologies {
			names = contextAppendUnique(names, name)
		}
	}
	return names
}

func contextTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#' && r != '.'
	})
}

func contextCanonical(value string) string { return cachedContextCanonical(value) }

func uncachedContextCanonical(value string) string {
	tokens := contextTokens(strings.TrimSpace(value))
	for i := range tokens {
		tokens[i] = strings.TrimRight(tokens[i], ".")
	}
	value = strings.Join(tokens, " ")
	switch value {
	case "golang":
		return "go"
	case "k8s":
		return "kubernetes"
	case "postgres":
		return "postgresql"
	}
	return value
}

func contextMentions(text, name string) bool {
	want := contextCanonical(name)
	if want == "" {
		return false
	}
	return strings.Contains(contextMentionText(text), " "+want+" ")
}

func contextMentionText(text string) string {
	tokens := contextTokens(text)
	for i, token := range tokens {
		tokens[i] = contextCanonical(strings.TrimRight(token, "."))
	}
	return " " + strings.Join(tokens, " ") + " "
}

func contextHasName(names []string, name string) bool {
	want := contextCanonical(name)
	for _, existing := range names {
		if contextCanonical(existing) == want && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func contextAppendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value != "" && !contextHasName(values, value) {
		values = append(values, value)
	}
	return values
}

func contextMatchingNames(query string, names []string) []string {
	result := []string{}
	normalized := contextMentionText(query)
	for _, name := range names {
		want := contextCanonical(name)
		if want != "" && strings.Contains(normalized, " "+want+" ") {
			result = contextAppendUnique(result, name)
		}
	}
	return result
}

func contextRelevantTechnologies(names, topics []string, negative map[string]bool) []string {
	result := []string{}
	for _, name := range names {
		if contextHasName(topics, name) && !negative[contextCanonical(name)] {
			result = contextAppendUnique(result, name)
		}
	}
	return result
}

func contextFollowUp(query string) bool {
	for _, marker := range []string{"а с этим", "с ним", "с ней", "с ними", "а сколько", "как долго", "подробнее", "расскажите об этом", "what about it", "how long", "tell me more"} {
		if contextMentions(query, marker) {
			return true
		}
	}
	return false
}

func contextNeedsDetails(query string) bool {
	text := strings.ToLower(query)
	for _, marker := range []string{"production", "продакш", "проде", "коммерческ", "commercial", "senior", "highload", "лет", "года", "год ", "месяц", "years", "months", "сколько", "зарплат", "salary", "релокац", "переезд", "собеседован", "interview", "документ", "documents", "договор", "контракт", "банк", "bank", "паспорт", "credentials", "парол", "выйти на работу", "доступност"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// Only a narrow question about a single skill can be covered by its presence.
// Everything more complex keeps an explicit gap, including mixed questions
// with terms outside our vocabulary. This is intentionally conservative.
func contextSimpleSkillQuestion(query string, topics []string) bool {
	if len(topics) != 1 {
		return false
	}
	tokens := contextTokens(query)
	for i, token := range tokens {
		tokens[i] = contextCanonical(strings.TrimRight(token, "."))
	}
	normalized := strings.Join(tokens, " ")
	name := contextCanonical(topics[0])
	for _, prefix := range []string{"работали ли вы с ", "есть опыт ", "есть ли опыт ", "есть ли у вас опыт ", "есть ли опыт работы с ", "есть ли у вас опыт работы с ", "do you have experience with ", "have you worked with "} {
		if normalized == prefix+name {
			return true
		}
	}
	return false
}
