package applicationprocessing

import (
	"context"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/testanswer"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type Outcome string

const (
	OutcomePrepared            Outcome = "PREPARED"
	OutcomeSkipped             Outcome = "SKIPPED"
	OutcomeNeedsCandidateInput Outcome = "NEEDS_CANDIDATE_INPUT"
	OutcomeManualReview        Outcome = "MANUAL_REVIEW"
	OutcomeAlreadyResponded    Outcome = "ALREADY_RESPONDED"
	OutcomeUnavailable         Outcome = "UNAVAILABLE"
)

type Decision string

const (
	DecisionMatch          Decision = "MATCH"
	DecisionReject         Decision = "REJECT"
	DecisionReviewRequired Decision = "REVIEW_REQUIRED"
)

// Applicability is an early, read-only snapshot. It controls preparation
// choices, but it is not an authorization to submit and is never a durable
// write grant.
type Applicability struct {
	Available             bool
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
	Area                  string
	AreaKnown             bool
	WorkSchedule          string
	WorkScheduleKnown     bool
	WorkExperience        string
	WorkExperienceKnown   bool
	ResponseURL           string
}

type TestMetadata struct {
	UIDPK     string
	GUID      string
	StartTime string
	Required  string
}

type TestSnapshot struct {
	Metadata TestMetadata
	Tasks    []testanswer.Task
}

type PreparedTest struct {
	Metadata TestMetadata
	Tasks    []testanswer.Task
	Answers  []testanswer.ProposedAnswer
}

// PreparedApplication is local preparation data only. In particular it does
// not contain approval, current-state proof, transport status, or delivery
// evidence. R11/R12.4b must preflight again immediately before a write.
type PreparedApplication struct {
	VacancyID        int
	Vacancy          vacancy.Vacancy
	ResumeID         string
	ResumeTitle      string
	CoverLetter      string
	Test             *PreparedTest
	Analysis         vacancyanalysis.Assessment
	CandidateContext candidatecontext.CandidateContext
}

type Request struct {
	Vacancy                 vacancy.Vacancy
	ResumeID                string
	ResumeTitle             string
	Candidate               vacancyanalysis.CandidateFacts
	LetterFacts             coverletter.CandidateFacts
	Stories                 []candidate.CandidateStory
	SemanticHints           []coverletter.SemanticHint
	Contacts                string
	GitHubURL               string
	ExtraLetterPrompt       string
	ExtraTestPrompt         string
	ForceLetter             bool
	IncludeKeywords         []string
	ApplicationLimitReached bool
}

type VacancyReader interface {
	ReadDescription(context.Context, int) (string, error)
	ReadApplicability(context.Context, vacancy.Vacancy) (Applicability, error)
	ReadTest(context.Context, int) (TestSnapshot, error)
}

type CandidateContextResolver interface {
	ResolveForVacancy(context.Context, vacancy.Vacancy, string) (candidatecontext.CandidateContext, error)
}

type VacancyAnalyzer interface {
	Analyze(context.Context, vacancyanalysis.Input) (vacancyanalysis.Assessment, error)
}

type CoverLetterGenerator interface {
	Generate(context.Context, coverletter.Input) (coverletter.Result, error)
}

type TestAnswerGenerator interface {
	Generate(context.Context, testanswer.Input) (testanswer.Result, error)
}

type SemanticHintProvider interface {
	Hints(context.Context, vacancy.Vacancy, string, vacancyanalysis.Assessment) ([]coverletter.SemanticHint, error)
}

type CandidateClarifier interface {
	Create(context.Context, vacancy.Vacancy, vacancyanalysis.HardRequirementEvaluation) error
}

// Policy keeps repository-specific deterministic rules out of this package.
// It is the authoritative owner of hard filtering and final decisions; AI is
// only an input to these methods.
type Policy interface {
	EarlyReject(vacancy.Vacancy) string
	DescriptionReject(vacancyValue vacancy.Vacancy, description string) string
	Decide(vacancyanalysis.Assessment) (Decision, string)
	ReconcileApplicability(vacancy.Vacancy, Applicability, vacancyanalysis.CandidateFacts, vacancyanalysis.Assessment) (vacancyanalysis.Assessment, Decision, string, error)
}
