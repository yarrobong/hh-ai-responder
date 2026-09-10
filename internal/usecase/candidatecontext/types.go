package candidatecontext

// CandidateContext is a detached, query-specific employer-facing projection.
// It contains only safe claims and explicit resolution outcomes.
type CandidateContext struct {
	AllowedFacts             []string                      `json:"allowed_facts"`
	RelevantSkills           []string                      `json:"relevant_skills"`
	RelevantProjects         []CandidateContextProject     `json:"relevant_projects"`
	RelevantAchievements     []CandidateContextAchievement `json:"relevant_achievements"`
	MissingInformation       []CandidateMissingInformation `json:"missing_information"`
	ForbiddenClaims          []string                      `json:"forbidden_claims"`
	ResolvedFacts            []ResolvedFact                `json:"resolved_facts,omitempty"`
	PartiallyResolvedFacts   []ResolvedFact                `json:"partially_resolved_facts,omitempty"`
	UnknownAtomicFacts       []ResolvedFact                `json:"unknown_atomic_facts,omitempty"`
	RestrictedFacts          []ResolvedFact                `json:"restricted_facts,omitempty"`
	MessageIntent            EmployerMessageIntent         `json:"message_intent,omitempty"`
	UnsupportedIntent        bool                          `json:"unsupported_intent,omitempty"`
	UserConfirmationRequired bool                          `json:"user_confirmation_required,omitempty"`
	UserConfirmationQuestion string                        `json:"user_confirmation_question,omitempty"`
}

type CandidateContextProject struct {
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	Description  string   `json:"description,omitempty"`
	Technologies []string `json:"technologies"`
	Tasks        []string `json:"tasks,omitempty"`
	Results      []string `json:"results,omitempty"`
}

type CandidateContextAchievement struct {
	Title        string   `json:"title"`
	Problem      string   `json:"problem,omitempty"`
	Solution     []string `json:"solution,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Result       []string `json:"result,omitempty"`
	Technologies []string `json:"technologies"`
}

type CandidateMissingInformation struct {
	Question string `json:"question"`
}

type ResolvedFact struct {
	Topic         string             `json:"topic"`
	RequestedFact string             `json:"requested_fact"`
	Status        ResolvedFactStatus `json:"status"`
	Value         string             `json:"value,omitempty"`
	Evidence      []string           `json:"evidence,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	AllowedClaims []string           `json:"allowed_claims,omitempty"`
	MissingPart   string             `json:"missing_part,omitempty"`
}

type ResolvedFactStatus string

const (
	ResolvedFactAnswerable          ResolvedFactStatus = "ANSWERABLE"
	ResolvedFactPartiallyAnswerable ResolvedFactStatus = "PARTIALLY_ANSWERABLE"
	ResolvedFactUnknown             ResolvedFactStatus = "UNKNOWN"
	ResolvedFactRestricted          ResolvedFactStatus = "RESTRICTED"
)

type EmployerMessageIntent string

const (
	EmployerMessageIntentFactualQuestion     EmployerMessageIntent = "FACTUAL_QUESTION"
	EmployerMessageIntentInstruction         EmployerMessageIntent = "INSTRUCTION"
	EmployerMessageIntentInterviewInvitation EmployerMessageIntent = "INTERVIEW_INVITATION"
	EmployerMessageIntentStatusMessage       EmployerMessageIntent = "STATUS_MESSAGE"
	EmployerMessageIntentRejection           EmployerMessageIntent = "REJECTION"
	EmployerMessageIntentAcknowledgement     EmployerMessageIntent = "ACKNOWLEDGEMENT"
	EmployerMessageIntentGeneralMessage      EmployerMessageIntent = "GENERAL_MESSAGE"
	EmployerMessageIntentCompound            EmployerMessageIntent = "COMPOUND"
	EmployerMessageIntentTerminal            EmployerMessageIntent = "TERMINAL"
)

const (
	CandidateContextStatusAnswerable               = "ANSWERABLE"
	CandidateContextStatusUserConfirmationRequired = "USER_CONFIRMATION_REQUIRED"
)

type HistoryMessage struct {
	Text   string
	Hidden bool
}

type ResolveInput struct {
	Query           string
	History         []HistoryMessage
	EmployerMessage bool
}

func Empty() CandidateContext {
	return CandidateContext{
		AllowedFacts: []string{}, RelevantSkills: []string{},
		RelevantProjects: []CandidateContextProject{}, RelevantAchievements: []CandidateContextAchievement{},
		MissingInformation: []CandidateMissingInformation{}, ForbiddenClaims: []string{},
		ResolvedFacts: []ResolvedFact{}, PartiallyResolvedFacts: []ResolvedFact{},
		UnknownAtomicFacts: []ResolvedFact{}, RestrictedFacts: []ResolvedFact{},
	}
}

func (c CandidateContext) RequiresCandidateInput() bool {
	if c.UserConfirmationRequired || len(c.UnknownAtomicFacts) > 0 {
		return true
	}
	return len(c.MissingInformation) > 0 && c.MessageIntent == ""
}

func (c *CandidateContext) AddMissing(question string) {
	for _, existing := range c.MissingInformation {
		if existing.Question == question {
			return
		}
	}
	c.MissingInformation = append(c.MissingInformation, CandidateMissingInformation{Question: question})
}
