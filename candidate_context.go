package main

// CandidateContext is a detached, query-specific employer-facing projection.
// Only these fields may be supplied by the future resolver integration to AI.
// MissingInformation is a request for confirmation, never a negative fact.
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

// Context DTOs intentionally omit sources, evidence, links and audit metadata.
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

func emptyCandidateContext() CandidateContext {
	return CandidateContext{
		AllowedFacts: []string{}, RelevantSkills: []string{},
		RelevantProjects: []CandidateContextProject{}, RelevantAchievements: []CandidateContextAchievement{},
		MissingInformation: []CandidateMissingInformation{}, ForbiddenClaims: []string{},
		ResolvedFacts: []ResolvedFact{}, PartiallyResolvedFacts: []ResolvedFact{},
		UnknownAtomicFacts: []ResolvedFact{}, RestrictedFacts: []ResolvedFact{},
		MessageIntent: "",
	}
}

// ResolvedFact is an atomic, employer-facing resolution. Evidence and
// allowed claims are deliberately rendered text, never storage metadata.
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
	CandidateContextStatusAnswerable               = "ANSWERABLE"
	CandidateContextStatusUserConfirmationRequired = "USER_CONFIRMATION_REQUIRED"
)

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
	ExternalActionRequired            = "EXTERNAL_ACTION_REQUIRED"
	ExternalActionInterviewInvitation = "INTERVIEW_INVITATION"
)

// ExternalActionRequirement is a structured display projection for messages
// that ask the candidate to act outside the ordinary HH text-reply flow.
// It is not candidate knowledge and never authorizes an external action.
type ExternalActionRequirement struct {
	Classification string `json:"classification"`
	Type           string `json:"type"`
	Destination    string `json:"destination,omitempty"`
	Duration       string `json:"duration,omitempty"`
	RequiredAction string `json:"required_action,omitempty"`
	ResultTiming   string `json:"result_timing,omitempty"`
	Aging          string `json:"aging,omitempty"`
}

// ConversationReplyRequirement is the deterministic action policy for the
// latest human employer message. It is separate from AIResponseAction: an
// optional reply is not an instruction to generate or send a courtesy text.
type ConversationReplyRequirement string

const (
	ReplyRequired ConversationReplyRequirement = "REPLY_REQUIRED"
	ReplyOptional ConversationReplyRequirement = "REPLY_OPTIONAL"
	NoReplyNeeded ConversationReplyRequirement = "NO_REPLY_NEEDED"
)

const (
	ConversationReplyRequired = ReplyRequired
	ConversationReplyOptional = ReplyOptional
	ConversationNoReplyNeeded = NoReplyNeeded
)

// RequiresCandidateInput gates unresolved atomic facts and explicit user
// confirmations. A partial answer is usable for a bounded reply and is not
// itself a blocker.
func (c CandidateContext) RequiresCandidateInput() bool {
	if c.UserConfirmationRequired || len(c.UnknownAtomicFacts) > 0 {
		return true
	}
	// Preserve the pre-Stage-15 contract for callers constructing a legacy
	// CandidateContext directly. Resolver-produced partial facts carry an
	// explicit intent and do not use this fallback.
	return len(c.MissingInformation) > 0 && c.MessageIntent == ""
}
