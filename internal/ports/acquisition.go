package ports

import (
	"context"

	"hh-ai-responder/internal/usecase/candidateacquisition"
)

// CandidateClarificationBackend is the persistence contract shared by the
// existing JSON façade and the PostgreSQL adapter. It stores only clarification
// records; candidate knowledge remains owned by its own repository.
type CandidateClarificationBackend interface {
	Load(context.Context) ([]candidateacquisition.CandidateClarificationRequest, error)
	Save(context.Context, []candidateacquisition.CandidateClarificationRequest) error
	Create(context.Context, candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error)
	UpsertByIdentity(context.Context, candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, bool, error)
	Get(context.Context, string) (candidateacquisition.CandidateClarificationRequest, error)
	List(context.Context) ([]candidateacquisition.CandidateClarificationRequest, error)
	SetStatus(context.Context, string, candidateacquisition.CandidateClarificationStatus) error
	RecordAnswer(context.Context, string, candidateacquisition.CandidateAnswer) error
	RecordAnswerEvidence(context.Context, string, candidateacquisition.CandidateAnswer) error
	SetProposalIDs(context.Context, string, []string) error
	MarkResolved(context.Context, string, candidateacquisition.CandidateClarificationStatus, string) error
	Reopen(context.Context, string, string) error
	Replace(context.Context, candidateacquisition.CandidateClarificationRequest) error
}

// CandidateAcquisitionReader is the clarification lookup capability used by
// the acquisition orchestration flow.
type CandidateAcquisitionReader interface {
	Get(string) (candidateacquisition.CandidateClarificationRequest, error)
	List() ([]candidateacquisition.CandidateClarificationRequest, error)
}

// CandidateAcquisitionWriter persists clarification evidence and lifecycle
// transitions. It deliberately does not expose CandidateKnowledgeBase or any
// storage-specific aggregate.
type CandidateAcquisitionWriter interface {
	Create(candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error)
	RecordAnswerEvidence(string, candidateacquisition.CandidateAnswer) error
	SetProposalIDs(string, []string) error
	MarkResolved(string, candidateacquisition.CandidateClarificationStatus, string) error
	Reopen(string, string) error
}

// CandidateAcquisitionStore is the narrow compatibility contract needed by
// the current clarification workflow.
type CandidateAcquisitionStore interface {
	CandidateAcquisitionReader
	CandidateAcquisitionWriter
}
