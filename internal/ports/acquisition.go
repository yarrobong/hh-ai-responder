package ports

import (
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

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
}

// CandidateAcquisitionStore is the narrow compatibility contract needed by
// the current clarification workflow.
type CandidateAcquisitionStore interface {
	CandidateAcquisitionReader
	CandidateAcquisitionWriter
}
