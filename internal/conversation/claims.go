package conversation

import "time"

// Claim records an exact candidate statement observed in the conversation.
// It is an aggregate annotation, not candidate knowledge or a truth decision.
type Claim struct {
	Text           string           `json:"text"`
	RelatedSkill   string           `json:"related_skill,omitempty"`
	RelatedProject string           `json:"related_project,omitempty"`
	MessageID      string           `json:"message_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Experience     *ExperienceClaim `json:"experience,omitempty"`
}

// ExperienceClaim is an optional structured annotation attached to a claim.
type ExperienceClaim struct {
	Months int `json:"months"`
	// "total_professional" is comparable to the legacy structured total.
	// Other scopes are retained but require manual verification.
	Scope string `json:"scope"`
}

// Summary is persisted conversation data. Summary generation and all
// candidate-consistency checks remain outside this package.
type Summary struct {
	TopicsDiscussed      []string `json:"topics_discussed"`
	EmployerQuestions    []string `json:"employer_questions"`
	CandidateAnswers     []string `json:"candidate_answers"`
	CandidateClaims      []Claim  `json:"candidate_claims"`
	EmployerRequirements []string `json:"employer_requirements"`
	PendingQuestions     []string `json:"pending_questions"`
	Commitments          []string `json:"commitments"`
	ImportantFacts       []string `json:"important_facts"`
}
