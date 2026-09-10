package hhwritepreflight

import (
	"context"
	"time"

	"hh-ai-responder/internal/usecase/writeapproval"
)

// Status describes the result of a fresh provider-state proof. Passed is only
// evidence that the observed state matched the requested operation; it is not
// write authorization and does not imply delivery.
type Status string

const (
	StatusPassed         Status = "passed"
	StatusStale          Status = "stale"
	StatusBlocked        Status = "blocked"
	StatusManualReview   Status = "manual_review"
	StatusRemoteNotFound Status = "remote_not_found"
	StatusRemoteChanged  Status = "remote_changed"
	StatusUnavailable    Status = "remote_unavailable"
)

const (
	OperationChatMessage      = "chat_message"
	OperationVacancyResponse  = "vacancy_response"
	ReplyRequirementRequired  = "REPLY_REQUIRED"
	ReplyRequirementNotNeeded = "NO_REPLY_NEEDED"
)

// ChatState is the narrow, neutral projection returned by a targeted HH chat
// read. It deliberately contains no provider response or write affordance.
type ChatState struct {
	ExternalID       string
	LastMessageID    string
	State            string
	ReplyRequirement string
	MessageCount     int
	Warnings         []string
	MessageIDs       []string
}

// ChatStateReader is a read-only capability. Implementations may use the HH
// read adapter, but the preflight use case does not know how authentication or
// HTTP transport works.
type ChatStateReader interface {
	ReadChatState(context.Context, string) (ChatState, error)
}

// ChatInput contains the exact local authorization evidence and the target
// identity needed to compare a fresh remote state. Validation of the evidence
// belongs to writeapproval and must happen before this method is called.
type ChatInput struct {
	Authorization      writeapproval.AuthorizationEvidence
	ActionType         writeapproval.ActionType
	ExpectedExternalID string
	LocalMessageCount  int
}

// Evidence is short-lived observation evidence. It is intentionally not
// persisted or treated as a durable send grant by this package.
type Evidence struct {
	Operation          string
	TargetID           string
	ObservedExternalID string
	ObservedState      string
	ObservedMessageID  string
	MessageCount       int
	ObservedAt         time.Time
}

type Result struct {
	Status    Status
	Operation string
	TargetID  string
	Evidence  Evidence
	Reasons   []string
	Err       error
}

func (r Result) Passed() bool { return r.Status == StatusPassed }

// VacancyResponseState is the current, typed portion of the legacy vacancy
// response preflight contract. The known bits are significant: an unknown
// critical state must not be treated as false.
type VacancyResponseState struct {
	VacancyID             int
	Archived              bool
	ArchivedKnown         bool
	AlreadyResponded      bool
	AlreadyRespondedKnown bool
	CanApply              bool
	CanApplyKnown         bool
	TestPresent           bool
	TestPresentKnown      bool
	LetterRequired        bool
	LetterRequiredKnown   bool
	ResponseURL           string
}

type VacancyResponseStateReader interface {
	ReadVacancyResponseState(context.Context, int) (VacancyResponseState, error)
}

type VacancyInput struct {
	VacancyID           int
	ExpectedTestPresent *bool
	ExpectedResponseURL string
}

type VacancyEvidence struct {
	Operation             string
	VacancyID             int
	ResponseURL           string
	Archived              bool
	ArchivedKnown         bool
	AlreadyResponded      bool
	AlreadyRespondedKnown bool
	CanApply              bool
	CanApplyKnown         bool
	TestPresent           bool
	TestPresentKnown      bool
	LetterRequired        bool
	LetterRequiredKnown   bool
	ObservedAt            time.Time
}

type VacancyResult struct {
	Status    Status
	Operation string
	VacancyID int
	Evidence  VacancyEvidence
	Reasons   []string
	Err       error
}

func (r VacancyResult) Passed() bool { return r.Status == StatusPassed }

// TestMetadata is the provider-neutral identity of the test embedded in an
// atomic vacancy response. It is intentionally limited to fields needed to
// prove that previously prepared answers still target the current test.
type TestMetadata struct {
	UIDPK     string
	GUID      string
	StartTime string
	Required  string
	Tasks     []TestTask
}

type TestTask struct {
	ID        int
	Open      string
	ChoiceIDs []string
}

type TestResult struct {
	Status    Status
	Operation string
	VacancyID int
	Reasons   []string
}

func (r TestResult) Passed() bool { return r.Status == StatusPassed }
