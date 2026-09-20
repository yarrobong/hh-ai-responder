package applicationsubmission

import (
	"context"

	"hh-ai-responder/internal/vacancy"
)

// Applicability is the typed, read-only vacancy state needed immediately
// before an automatic response. Known bits are distinct from false values.
type Applicability struct {
	Available             bool
	AvailableKnown        bool
	Archived              bool
	ArchivedKnown         bool
	AlreadyResponded      bool
	AlreadyRespondedKnown bool
	TestPresent           bool
	TestPresentKnown      bool
	LetterRequired        bool
	LetterRequiredKnown   bool
	CanApply              bool
	CanApplyKnown         bool
	ResponseURL           string
}

// TestMetadata contains only provider identities used for an exact
// prepared-versus-current comparison.
type TestMetadata struct {
	UIDPK     string
	GUID      string
	StartTime string
	Required  string
}

type PreparedTask struct {
	ID        int
	Open      string
	ChoiceIDs []string
}

type PreparedAnswer struct {
	TaskID    int
	ChoiceID  string
	Text      string
	HasChoice bool
}

type PreparedTest struct {
	Metadata TestMetadata
	Tasks    []PreparedTask
	Answers  []PreparedAnswer
}

// PreparedApplication is a detached equivalent of applicationprocessing's
// preparation value. It contains no approval, fresh-state proof, or delivery
// evidence. The exact cover letter and selected resume identity are carried
// through unchanged.
type PreparedApplication struct {
	VacancyID   int
	Vacancy     vacancy.Vacancy
	ResumeID    string
	ResumeTitle string
	CoverLetter string
	Test        *PreparedTest
}

type Input struct {
	Prepared PreparedApplication
	// CurrentResumeID binds the attempt to the resume selected by the caller.
	// When RequireCurrentResumeID is true, an empty or different value blocks
	// submission; the service never selects a fallback resume.
	CurrentResumeID        string
	RequireCurrentResumeID bool
	// ResponseURL is the exact vacancy-response page used as the provider
	// referer for an atomic test response. If empty, the executor adapter may
	// use the detached vacancy's desktop link.
	ResponseURL string
}

type VacancyReader interface {
	ReadApplicability(context.Context, vacancy.Vacancy) (Applicability, error)
}

type TestSnapshot struct {
	Metadata TestMetadata
	Tasks    []PreparedTask
}

type TestReader interface {
	ReadTest(context.Context, int) (TestSnapshot, error)
}

// ApplicationRequest is the semantic atomic application operation. Test
// answers are part of this one request; there is no separate test mutation.
type ApplicationRequest struct {
	VacancyID       int
	ResumeID        string
	Letter          string
	RefererURL      string
	IgnorePostponed string
	Test            *TestSubmission
}

type TestSubmission struct {
	UIDPK                   string
	GUID                    string
	StartTime               string
	Required                string
	Incomplete              string
	Lux                     string
	WithoutTest             string
	CountryIDs              string
	VisibleInVacancyCountry string
	Answers                 []TestAnswer
}

type TestAnswer struct {
	TaskID   int
	ChoiceID string
	Text     string
}

type ExecutionOutcome string

const (
	ExecutionAccepted          ExecutionOutcome = "accepted"
	ExecutionRejected          ExecutionOutcome = "rejected"
	ExecutionDeliveryUncertain ExecutionOutcome = "delivery_uncertain"
	ExecutionNotSent           ExecutionOutcome = "not_sent"
)

// ExecutionResult contains provider-neutral transport evidence returned by
// the R11 executor adapter. It is separate from local event/projection state.
type ExecutionResult struct {
	AttemptID      string
	Outcome        ExecutionOutcome
	ProviderID     string
	ProviderStatus int
	Metadata       map[string]string
	TransportTried bool
}

type ApplicationExecutor interface {
	SubmitApplication(context.Context, ApplicationRequest) (ExecutionResult, error)
}

type Dependencies struct {
	Vacancies VacancyReader
	Tests     TestReader
	Executor  ApplicationExecutor
}

type Options struct {
	WriteEnabled                bool
	DryRun                      bool
	RequireAvailabilityEvidence bool
}

type Status string

const (
	StatusPreview            Status = "PREVIEW"
	StatusSubmitted          Status = "SUBMITTED"
	StatusRejected           Status = "REJECTED"
	StatusDeliveryUncertain  Status = "DELIVERY_UNCERTAIN"
	StatusBlockedStale       Status = "BLOCKED_STALE"
	StatusBlockedUnavailable Status = "BLOCKED_UNAVAILABLE"
	StatusWriteDisabled      Status = "WRITE_DISABLED"
	StatusNotSent            Status = "NOT_SENT"
)

type Result struct {
	Status         Status
	VacancyID      int
	Reason         string
	Applicability  Applicability
	PreflightKnown bool
	Execution      ExecutionResult
}
