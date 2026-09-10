package runtime

import (
	"errors"
	"sort"
	"strings"

	domaincandidate "hh-ai-responder/internal/candidate"
	candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
)

var contextTechnologyNames = candidatecontext.TechnologyNames()

// CanonicalCandidateProvider builds the in-memory read model from already
// loaded legacy values. It deliberately has no persistence or network
// behavior.
type CanonicalCandidateProvider struct{ input CanonicalCandidateInput }

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

// CandidateContextResolver is a thin root compatibility adapter. It assembles
// the canonical candidate from legacy storage-compatible inputs; deterministic
// context policy lives in internal/usecase/candidatecontext.
type CandidateContextResolver struct {
	kb        *CandidateKnowledgeBase
	candidate *Candidate
}

func NewCandidateContextResolver(kb *CandidateKnowledgeBase) *CandidateContextResolver {
	return &CandidateContextResolver{kb: kb}
}

func NewCandidateContextResolverFromCandidate(value Candidate) *CandidateContextResolver {
	copy, err := cloneKnowledge(value)
	if err != nil {
		copy = value
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
	knowledge := *r.kb
	knowledge.Unknowns, knowledge.Proposals, knowledge.Events = nil, nil, nil
	return NewCanonicalCandidateProvider(CanonicalCandidateInput{Profile: r.kb.Profile, Knowledge: knowledge}).CandidateWithDiagnostics()
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

func (r *CandidateContextResolver) resolve(query string, history []ChatMessage, employerMessage bool) (CandidateContext, error) {
	candidate, diagnostics, err := r.canonicalCandidate()
	if err != nil {
		return emptyCandidateContext(), errors.New("candidate context: canonical candidate unavailable")
	}
	if len(diagnostics.Conflicts) > 0 {
		return emptyCandidateContext(), errors.New("candidate context: critical candidate conflict; context withheld")
	}
	if r != nil && r.kb != nil && len(candidate.Skills) > 1 {
		order := make(map[string]int, len(r.kb.Skills))
		for index, skill := range r.kb.Skills {
			order[contextCanonical(skill.Name)] = index
		}
		sort.SliceStable(candidate.Skills, func(i, j int) bool {
			left, right := candidate.Skills[i].Name, candidate.Skills[j].Name
			if left == "" {
				left = candidate.Skills[i].DisplayName
			}
			if right == "" {
				right = candidate.Skills[j].DisplayName
			}
			return order[contextCanonical(left)] < order[contextCanonical(right)]
		})
	}
	input := candidatecontext.ResolveInput{Query: query, EmployerMessage: employerMessage}
	for _, message := range history {
		input.History = append(input.History, candidatecontext.HistoryMessage{Text: message.Text, Hidden: message.Hidden})
	}
	return candidatecontext.NewResolver(candidate).Resolve(input)
}

// Vacancy has no description field. Optional texts carry already-read
// description/requirements; no HH request is made here.
func (r *CandidateContextResolver) ResolveForVacancy(v Vacancy, descriptionAndRequirements ...string) (CandidateContext, error) {
	parts := append([]string{v.Name, v.WorkExperience, v.WorkSchedule}, descriptionAndRequirements...)
	return r.GetEmployerSafeContext(strings.Join(parts, "\n"))
}

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

func (r *CandidateContextResolver) ResolveForEmployerMessage(message string, history []ChatMessage) (CandidateContext, error) {
	return r.resolve(message, history, true)
}
func (r *CandidateContextResolver) GetEmployerSafeContext(query string) (CandidateContext, error) {
	return r.resolve(query, nil, false)
}

// The following functions are compatibility shims for root consumers that
// still use the old helper names. They contain no candidate-resolution policy.
func contextCanonical(value string) string            { return cachedContextCanonical(value) }
func uncachedContextCanonical(value string) string    { return candidatecontext.Canonical(value) }
func contextMentions(text, name string) bool          { return candidatecontext.Mentions(text, name) }
func contextHasName(names []string, name string) bool { return candidatecontext.HasName(names, name) }
func contextAppendUnique(values []string, value string) []string {
	return appendUniqueContext(values, value)
}
func appendUniqueContext(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value != "" && !contextHasName(values, value) {
		values = append(values, value)
	}
	return values
}
func contextMatchingNames(query string, names []string) []string {
	return candidatecontext.MatchingNames(query, names)
}
func contextFollowUp(query string) bool     { return candidatecontext.FollowUp(query) }
func contextNeedsDetails(query string) bool { return candidatecontext.NeedsDetails(query) }
func contextSimpleSkillQuestion(query string, topics []string) bool {
	return candidatecontext.SimpleSkillQuestion(query, topics)
}
func contextTokens(value string) []string { return candidatecontext.Tokens(value) }
func looksLikeFactualQuestion(text string) bool {
	return candidatecontext.LooksLikeFactualQuestion(text)
}
func detailedSkillForTopic(view EmployerSafeCandidateKnowledge, topic string) (CandidateSkillDetailed, bool) {
	for _, skill := range view.Skills {
		if contextCanonical(skill.Name) == contextCanonical(topic) {
			return skill, true
		}
	}
	return CandidateSkillDetailed{}, false
}

func contextTopicNames(safe EmployerSafeKnowledge) []string {
	return candidatecontext.TopicNames(domaincandidate.EmployerSafeCandidateKnowledge{Skills: safe.Skills, Projects: safe.Projects, Achievements: safe.Achievements})
}
func contextRelevantTechnologies(names, topics []string, negative map[string]bool) []string {
	return candidatecontext.RelevantTechnologies(names, topics, negative)
}
func appendFactClaims(values []string, fact ResolvedFact) []string {
	for _, claim := range append(append([]string{}, fact.AllowedClaims...), fact.Value) {
		values = contextAppendUnique(values, claim)
	}
	return values
}
