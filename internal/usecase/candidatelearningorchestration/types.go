package candidatelearningorchestration

import (
	"context"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/usecase/candidateinterpretation"
)

type ClarificationStore interface {
	Get(string) (candidateacquisition.CandidateClarificationRequest, error)
	List() ([]candidateacquisition.CandidateClarificationRequest, error)
	Create(candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error)
	RecordAnswerEvidence(string, candidateacquisition.CandidateAnswer) error
	SetProposalIDs(string, []string) error
	MarkResolved(string, candidateacquisition.CandidateClarificationStatus, string) error
}

// CandidateReader supplies a detached canonical snapshot for answer and
// confirmation-time checks. It is deliberately read-only.
type CandidateReader interface {
	CurrentCandidate(context.Context) (candidate.Candidate, error)
}

// Interpreter is implemented by candidateinterpretation.Service or a root
// adapter around it. It is the only AI boundary used by this package.
type Interpreter interface {
	Interpret(context.Context, candidateinterpretation.Input) (candidateinterpretation.Interpretation, error)
}

type UnknownInput struct {
	Candidate candidate.Candidate
	Gap       candidateacquisition.CandidateKnowledgeGap
	Request   candidateacquisition.CandidateClarificationRequest
}

type ChoiceInput struct {
	Candidate candidate.Candidate
	Request   candidateacquisition.CandidateClarificationRequest
	Answer    candidateacquisition.CandidateAnswer
}

type ProposalInput struct {
	Candidate candidate.Candidate
	Request   candidateacquisition.CandidateClarificationRequest
	Answer    candidateacquisition.CandidateAnswer
	Proposal  candidateacquisition.CandidateKnowledgeProposalDraft
}

type DismissInput struct {
	Request  candidateacquisition.CandidateClarificationRequest
	Evidence string
}

// Mutation is the sole write authority for Candidate truth and pending
// proposal/unknown state. Implementations may use JSON or PostgreSQL, but the
// orchestration package never branches on the backend.
type Mutation interface {
	CreateUnknown(context.Context, UnknownInput) (MutationResult, error)
	ApplyChoice(context.Context, ChoiceInput) (MutationResult, error)
	CreateProposal(context.Context, ProposalInput) (MutationResult, error)
	DismissUnknown(context.Context, DismissInput) error
	ConfirmProposal(context.Context, string) (MutationResult, error)
	RejectProposal(context.Context, string) error
}

type IDGenerator func(kind string) (string, error)

type Dependencies struct {
	Candidate      CandidateReader
	Clarifications ClarificationStore
	Mutation       Mutation
	Interpretation Interpreter
	GenerateID     IDGenerator
}

type CreateClarificationResult struct {
	Requests []candidateacquisition.CandidateClarificationRequest
}

type AnswerResult struct {
	ClarificationID string
	UnknownID       string
	ProposalIDs     []string
	Status          candidateacquisition.CandidateClarificationStatus
	Disposition     string
	Mutation        MutationResult
}

type MutationResult struct {
	EntityID             string
	ProposalID           string
	QuestionID           string
	SemanticIndexWarning string
}

type ProposalResult struct {
	ProposalID string
	Mutation   MutationResult
}
