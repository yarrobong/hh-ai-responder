package applicationprocessing

import (
	"context"
	"errors"
	"testing"

	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/testanswer"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type fakeReader struct {
	description                                     string
	applicability                                   Applicability
	test                                            TestSnapshot
	descriptionCalls, applicabilityCalls, testCalls int
}

func (f *fakeReader) ReadDescription(context.Context, int) (string, error) {
	f.descriptionCalls++
	return f.description, nil
}
func (f *fakeReader) ReadApplicability(context.Context, vacancy.Vacancy) (Applicability, error) {
	f.applicabilityCalls++
	return f.applicability, nil
}
func (f *fakeReader) ReadTest(context.Context, int) (TestSnapshot, error) {
	f.testCalls++
	return f.test, nil
}

type fakeCandidate struct {
	calls int
	err   error
}

func (f *fakeCandidate) ResolveForVacancy(context.Context, vacancy.Vacancy, string) (candidatecontext.CandidateContext, error) {
	f.calls++
	return candidatecontext.CandidateContext{AllowedFacts: []string{"known"}}, f.err
}

type fakeAnalyzer struct {
	assessment vacancyanalysis.Assessment
	calls      int
	err        error
}

func (f *fakeAnalyzer) Analyze(context.Context, vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	f.calls++
	return f.assessment, f.err
}

type fakeLetter struct {
	calls int
	err   error
}

func (f *fakeLetter) Generate(context.Context, coverletter.Input) (coverletter.Result, error) {
	f.calls++
	return coverletter.Result{Letter: "truthful letter"}, f.err
}

type fallbackFakeLetter struct {
	result coverletter.Result
	calls  int
}

func (f *fallbackFakeLetter) Generate(context.Context, coverletter.Input) (coverletter.Result, error) {
	return coverletter.Result{}, errors.New("primary generator should not be selected")
}

func (f *fallbackFakeLetter) GenerateWithFallback(context.Context, coverletter.Input) (coverletter.Result, error) {
	f.calls++
	return f.result, nil
}

type fakeTestAnswer struct {
	calls  int
	result testanswer.Result
	err    error
}

func (f *fakeTestAnswer) Generate(context.Context, testanswer.Input) (testanswer.Result, error) {
	f.calls++
	return f.result, f.err
}

type fakeClarifier struct {
	calls int
	err   error
}

func (f *fakeClarifier) Create(context.Context, vacancy.Vacancy, vacancyanalysis.HardRequirementEvaluation) error {
	f.calls++
	return f.err
}

type fakePolicy struct {
	early, detail string
	decision      Decision
	reason        string
	reconcile     AssessmentReconcile
}
type AssessmentReconcile struct {
	assessment vacancyanalysis.Assessment
	decision   Decision
	reason     string
	err        error
}

func (p fakePolicy) EarlyReject(vacancy.Vacancy) string               { return p.early }
func (p fakePolicy) DescriptionReject(vacancy.Vacancy, string) string { return p.detail }
func (p fakePolicy) Decide(vacancyanalysis.Assessment) (Decision, string) {
	return p.decision, p.reason
}
func (p fakePolicy) ReconcileApplicability(_ vacancy.Vacancy, _ Applicability, _ vacancyanalysis.CandidateFacts, _ vacancyanalysis.Assessment) (vacancyanalysis.Assessment, Decision, string, error) {
	return p.reconcile.assessment, p.reconcile.decision, p.reconcile.reason, p.reconcile.err
}

func baseRequest() Request {
	return Request{Vacancy: vacancy.Vacancy{ID: 7, Name: "Backend developer"}, ResumeID: "resume-1", ResumeTitle: "Backend"}
}
func baseDeps(reader *fakeReader, analyzer *fakeAnalyzer, policy fakePolicy, candidate *fakeCandidate) Dependencies {
	return Dependencies{Vacancies: reader, Candidate: candidate, Analyzer: analyzer, Policy: policy}
}

func TestPrepareEarlyRejectDoesNotCallAIOrPreparationLeaves(t *testing.T) {
	reader := &fakeReader{description: "description"}
	analyzer := &fakeAnalyzer{}
	letter := &fakeLetter{}
	answers := &fakeTestAnswer{}
	service := NewService(Dependencies{Vacancies: reader, Candidate: &fakeCandidate{}, Analyzer: analyzer, CoverLetter: letter, TestAnswer: answers, Policy: fakePolicy{early: "excluded role"}})
	result, err := service.Prepare(context.Background(), baseRequest())
	if err != nil || result.Outcome != OutcomeSkipped || analyzer.calls != 0 || letter.calls != 0 || answers.calls != 0 || reader.descriptionCalls != 0 {
		t.Fatalf("early reject = %#v, err=%v, calls description=%d ai=%d letter=%d test=%d", result, err, reader.descriptionCalls, analyzer.calls, letter.calls, answers.calls)
	}
}

func TestPrepareAIHardFailureCannotPrepare(t *testing.T) {
	reader := &fakeReader{description: "description"}
	analyzer := &fakeAnalyzer{assessment: vacancyanalysis.Assessment{Apply: true, Score: 99, HardRequirements: []vacancyanalysis.HardRequirementEvaluation{{Requirement: "Kubernetes", Status: vacancyanalysis.HardRequirementStatusMissing}}}}
	service := NewService(baseDeps(reader, analyzer, fakePolicy{decision: DecisionReject, reason: "hard requirements not met"}, &fakeCandidate{}))
	result, err := service.Prepare(context.Background(), baseRequest())
	if err != nil || result.Outcome != OutcomeSkipped || result.Prepared != nil {
		t.Fatalf("hard failure = %#v, err=%v", result, err)
	}
}

func TestPrepareUnknownCreatesCandidateInputWithoutPreparation(t *testing.T) {
	reader := &fakeReader{description: "description"}
	analyzer := &fakeAnalyzer{assessment: vacancyanalysis.Assessment{Apply: true, Score: 90, HardRequirements: []vacancyanalysis.HardRequirementEvaluation{{Requirement: "Redis", Category: vacancyanalysis.HardRequirementCategorySkill, Status: vacancyanalysis.HardRequirementStatusUnknown}}}}
	clarifier := &fakeClarifier{}
	service := NewService(Dependencies{Vacancies: reader, Candidate: &fakeCandidate{}, Analyzer: analyzer, Clarifier: clarifier, Policy: fakePolicy{decision: DecisionReviewRequired, reason: "hard requirement unknown"}})
	result, err := service.Prepare(context.Background(), baseRequest())
	if err != nil || result.Outcome != OutcomeNeedsCandidateInput || result.Prepared != nil || clarifier.calls != 1 {
		t.Fatalf("unknown requirement = %#v, err=%v, clarifications=%d", result, err, clarifier.calls)
	}
}

func TestPrepareRequiredLetterAndTestPreservesProviderIdentities(t *testing.T) {
	task := testanswer.Task{ID: 11, CandidateSolutions: []testanswer.Option{{ID: "101", Text: "yes"}}}
	reader := &fakeReader{description: "description", applicability: Applicability{LetterRequired: true, TestPresent: true}, test: TestSnapshot{Metadata: TestMetadata{UIDPK: "uid", GUID: "guid"}, Tasks: []testanswer.Task{task}}}
	analyzer := &fakeAnalyzer{assessment: vacancyanalysis.Assessment{Apply: true, Score: 90}}
	letter := &fakeLetter{}
	answers := &fakeTestAnswer{result: testanswer.Result{Answers: []testanswer.ProposedAnswer{{TaskID: 11, SolutionID: 101, HasChoice: true}}}}
	service := NewService(Dependencies{Vacancies: reader, Candidate: &fakeCandidate{}, Analyzer: analyzer, CoverLetter: letter, TestAnswer: answers, Policy: fakePolicy{decision: DecisionMatch, reconcile: AssessmentReconcile{assessment: analyzer.assessment, decision: DecisionMatch}}})
	result, err := service.Prepare(context.Background(), baseRequest())
	if err != nil || result.Outcome != OutcomePrepared || result.Prepared == nil || letter.calls != 1 || answers.calls != 1 {
		t.Fatalf("prepared = %#v, err=%v, letter=%d test=%d", result, err, letter.calls, answers.calls)
	}
	if result.Prepared.Test == nil || result.Prepared.Test.Tasks[0].CandidateSolutions[0].ID != "101" || result.Prepared.Test.Answers[0].SolutionID != 101 {
		t.Fatalf("provider identities changed: %#v", result.Prepared.Test)
	}
}

func TestPrepareFallbackLetterRemainsReviewRequiredAndKeepsEvidence(t *testing.T) {
	reader := &fakeReader{description: "description", applicability: Applicability{LetterRequired: true}}
	analyzer := &fakeAnalyzer{assessment: vacancyanalysis.Assessment{Apply: true, Score: 90}}
	letter := &fallbackFakeLetter{result: coverletter.Result{
		Letter: "Здравствуйте! Меня заинтересовала вакансия.", Status: coverletter.DraftStatusReviewRequired,
		Evidence: []coverletter.DraftEvidence{{Claim: "vacancy scope", Kind: "vacancy", Reference: "vacancy:7"}},
	}}
	service := NewService(Dependencies{Vacancies: reader, Candidate: &fakeCandidate{}, Analyzer: analyzer, CoverLetter: letter, Policy: fakePolicy{decision: DecisionMatch, reconcile: AssessmentReconcile{assessment: analyzer.assessment, decision: DecisionMatch}}})
	result, err := service.Prepare(context.Background(), baseRequest())
	if err != nil || result.Outcome != OutcomeManualReview || result.Prepared == nil || letter.calls != 1 {
		t.Fatalf("fallback preparation=%+v err=%v calls=%d", result, err, letter.calls)
	}
	if len(result.Prepared.CoverLetterEvidence) != 1 || result.Prepared.CoverLetterEvidence[0].Reference != "vacancy:7" {
		t.Fatalf("evidence=%+v", result.Prepared.CoverLetterEvidence)
	}
}

func TestPreparePropagatesCancellationAndLeafErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewService(Dependencies{})
	if _, err := service.Prepare(ctx, baseRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
