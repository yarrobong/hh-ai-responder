package followupdraft

import (
	"time"

	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

const (
	EligibilityStatusEligible = "eligible"
)

// Eligibility is the deterministic result produced by the root follow-up
// policy. This package consumes it but never derives or overrides it.
type Eligibility struct {
	ApplicationID     string     `json:"application_id"`
	ConversationID    string     `json:"conversation_id"`
	CompanyName       string     `json:"company_name"`
	VacancyTitle      string     `json:"vacancy_title"`
	Reason            string     `json:"reason"`
	EligibleAt        *time.Time `json:"eligible_at"`
	WaitingSince      *time.Time `json:"waiting_since"`
	DaysWaiting       float64    `json:"days_waiting"`
	PreviousFollowUps int        `json:"previous_followups"`
	Status            string     `json:"status"`
	Warnings          []string   `json:"warnings"`
}

// Input is a detached snapshot assembled by the higher-level workflow. The
// employer-reply Context is reused as a pure prompt/value projection; the
// follow-up eligibility and generation semantics remain owned here.
type Input struct {
	Context            employerreply.Context
	Eligibility        Eligibility
	ConfirmedFollowUps []time.Time
}

// Result is a proposed follow-up decision. It contains no send, approval,
// preflight, persistence, nonce, or delivery capability.
type Result struct {
	Decision employerreply.Decision
}
