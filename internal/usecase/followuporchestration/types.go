package followuporchestration

import (
	"context"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/employerreply"
	"hh-ai-responder/internal/usecase/followupdraft"
)

const (
	OutcomeDraftReady     = "DRAFT_READY"
	OutcomeNeedsCandidate = "NEEDS_CANDIDATE_INPUT"
	OutcomeManualReview   = "MANUAL_REVIEW"
	OutcomeReused         = "REUSED"
)

// LoadedSnapshot is the local, detached state used for one eligibility
// evaluation and one proposal. The loader owns application/conversation
// reads, CandidateContext resolution, and relevant knowledge selection.
type LoadedSnapshot struct {
	Generation        followupdraft.Input
	EligibilityInput  EligibilityInput
	ApplicationID     string
	ConversationID    string
	Fingerprint       string
	EmployerMessage   string
	EmployerMessageID string
	VacancyID         string
}

// EligibilityInput is the normalized state passed to the deterministic
// FollowUpEngine adapter. It contains no storage or transport capability.
type EligibilityInput struct {
	Application          application.JobApplication
	Conversation         conversation.EmployerConversation
	AppliedAt            *time.Time
	PreviousFollowUps    []time.Time
	PendingClarification bool
	Warnings             []string
	Dismissed            bool
}

type SnapshotLoader interface {
	Load(context.Context, string, time.Time) (LoadedSnapshot, error)
	Reload(context.Context, string, time.Time) (LoadedSnapshot, error)
}

type EligibilityEvaluator interface {
	Evaluate(LoadedSnapshot, time.Time) followupdraft.Eligibility
}

type Draft struct {
	ID               string
	ApplicationID    string
	ConversationID   string
	InputFingerprint string
	Text             string
	DecisionReason   string
	UsedFacts        []string
	Invalid          bool
}

type DraftStore interface {
	FindReusable(context.Context, string, string, employerreply.Context) (Draft, bool, error)
	Save(context.Context, Draft) error
}

type ClarificationInput struct {
	ConversationID    string
	ApplicationID     string
	VacancyID         string
	EmployerMessage   string
	EmployerMessageID string
	Reason            string
	Missing           []employerreply.MissingInformation
}

type ClarificationWriter interface {
	Persist(context.Context, ClarificationInput) error
}

type DraftPreparer interface {
	Prepare(context.Context, followupdraft.Input) (followupdraft.Result, error)
}

type Dependencies struct {
	Snapshots      SnapshotLoader
	Eligibility    EligibilityEvaluator
	FollowUp       DraftPreparer
	Drafts         DraftStore
	Clarifications ClarificationWriter
}

type Result struct {
	Decision employerreply.Decision
	Outcome  string
	Reused   bool
}
