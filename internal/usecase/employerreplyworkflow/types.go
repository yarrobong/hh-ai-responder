package employerreplyworkflow

import (
	"context"

	"hh-ai-responder/internal/usecase/employerreply"
)

const (
	OutcomeDraftReady     = "DRAFT_READY"
	OutcomeNeedsCandidate = "NEEDS_CANDIDATE_INPUT"
	OutcomeNoReply        = "NO_REPLY"
	OutcomeManualReview   = "MANUAL_REVIEW"
	OutcomeReused         = "REUSED"
)

// Input identifies one conversation preparation target. Context assembly is
// deliberately owned by ConversationLoader, not by callers.
type Input struct {
	ConversationID string
	Task           string
}

// LoadedContext is the detached snapshot needed by the employer-reply leaf.
// Metadata is supplied by the loader so the workflow does not recreate the
// repository's existing draft identity rules.
type LoadedContext struct {
	Input             employerreply.Input
	ConversationID    string
	ApplicationID     string
	VacancyID         string
	EmployerMessage   string
	EmployerMessageID string
	MessageHash       string
	KnowledgeHash     string
	InputFingerprint  string
}

type ConversationLoader interface {
	Load(context.Context, string) (LoadedContext, error)
}

type ReplyPreparer interface {
	Prepare(context.Context, employerreply.Input) (employerreply.Decision, error)
}

type Draft struct {
	ID                    string
	ConversationID        string
	ApplicationID         string
	InputMessageID        string
	InputFingerprint      string
	PromptVersion         string
	EmployerMessageHash   string
	RelevantKnowledgeHash string
	Text                  string
	DecisionReason        string
	UsedFacts             []string
}

type DraftStore interface {
	FindReusable(context.Context, string, string) (Draft, bool, error)
	Save(context.Context, Draft) error
}

// ClarificationInput is the typed leaf outcome plus the loaded conversation
// references. A writer may route it through the candidate-acquisition
// deduplication path used by the legacy store adapter.
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

type Dependencies struct {
	Conversations  ConversationLoader
	Reply          ReplyPreparer
	Drafts         DraftStore
	Clarifications ClarificationWriter
}

type Options struct {
	PromptVersion string
}

type Result struct {
	Decision employerreply.Decision
	Outcome  string
	Reused   bool
}
