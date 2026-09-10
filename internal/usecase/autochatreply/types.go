package autochatreply

import (
	"time"

	"hh-ai-responder/internal/usecase/candidatecontext"
)

// CandidateContext is the bounded employer-safe candidate projection used by
// the existing candidate-context package. It is kept as an alias in Input so
// callers cannot accidentally pass a root or storage object.
type CandidateContext = candidatecontext.CandidateContext

// Candidate contains the legacy prompt fields as detached values. The
// canonical CandidateContext remains the authority for factual validation.
type Candidate struct {
	FirstName       string
	LastName        string
	ResumeTitle     string
	Salary          string
	Skills          string
	Experience      string
	AlwaysEmphasize string
	AvoidClaiming   string
	Context         CandidateContext
}

// HistoryMessage is one already-normalized HH chat message. Author and time
// are presentation data; the use case never sees a provider DTO.
type HistoryMessage struct {
	ID        string
	Timestamp time.Time
	Author    string
	Text      string
}

// Button is the complete button model currently exposed by the HH read path.
// The provider currently supplies text and size only; no action/callback ID
// exists to preserve here.
type Button struct {
	Text string
	Size string
}

type ChatState string

const (
	ChatStateActive           ChatState = "active"
	ChatStateTerminal         ChatState = "terminal"
	ChatStateIneligible       ChatState = "ineligible"
	ChatStateDiscarded        ChatState = "discarded"
	ChatStateCandidateReplied ChatState = "candidate_replied"
)

// Input is a detached snapshot of the legacy chat decision. It contains no
// raw HH response, config, repository, transport, or write capability.
type Input struct {
	ChatID               int64
	ContactName          string
	EmployerMessage      string
	VacancyName          string
	VacancyURL           string
	CompanyName          string
	VacancyCompensation  string
	Candidate            Candidate
	History              []HistoryMessage
	Buttons              []Button
	CommunicationProfile string
	GitHubURL            string
	Contacts             string
	ExtraPrompt          string
	State                ChatState
}

type Outcome string

const (
	OutcomeReply            Outcome = "reply"
	OutcomeNoReply          Outcome = "no_reply"
	OutcomeManualReview     Outcome = "manual_review"
	OutcomeLeaveRecommended Outcome = "leave_recommended"
)

// ProposedResult is a proposal only. Send/leave, freshness, dry-run, and
// write-side reconciliation remain outside this package.
type ProposedResult struct {
	Outcome      Outcome
	Text         string
	ReviewReason string
}
