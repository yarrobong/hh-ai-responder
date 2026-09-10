package applicationreconciliation

import (
	"context"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
)

type Target struct {
	VacancyID int
	ResumeID  string
}

type PreflightEvidence struct {
	Available             bool
	AlreadyResponded      bool
	AlreadyRespondedKnown bool
	CanApply              bool
	CanApplyKnown         bool
}

// ProviderResponse is a fresh HH negotiation/application identity. HH's
// current read contract does not expose resume identity, so this value is
// intentionally vacancy-wide rather than resume-specific.
type ProviderResponse struct {
	VacancyID           int
	ApplicationID       string
	NegotiationID       string
	ResponseByApplicant bool
	ProviderResponseAt  *time.Time
}

type EvidenceSnapshot struct {
	VacancyID             int
	ObservedAt            time.Time
	Preflight             PreflightEvidence
	PreflightAvailable    bool
	PreflightError        string
	Applications          []ProviderResponse
	ApplicationsAvailable bool
	ApplicationsError     string
}

// EvidenceReader is the complete read-only provider capability required by
// reconciliation. It contains no writer, generic requester, or scheduler.
type EvidenceReader interface {
	ReadVacancyResponseEvidence(context.Context, Target) (EvidenceSnapshot, error)
}

type AttemptStore interface {
	Get(context.Context, string) (domain.Attempt, error)
	FindBlocking(context.Context, int) (domain.Attempt, error)
	RecordReconciliation(context.Context, string, domain.ReconciliationEvidence, time.Time) error
}

type Status string

const (
	StatusConfirmed     Status = "CONFIRMED_RESPONSE_EXISTS"
	StatusInsufficient  Status = "INSUFFICIENT_EVIDENCE"
	StatusUnavailable   Status = "EVIDENCE_UNAVAILABLE"
	StatusConflicting   Status = "CONFLICTING_EVIDENCE"
	StatusNotApplicable Status = "NOT_APPLICABLE"
)

type Result struct {
	Status        Status
	Attempt       domain.Attempt
	PreviousState domain.State
	Evidence      domain.ReconciliationEvidence
	ReadAttempted bool
	Reason        string
}
