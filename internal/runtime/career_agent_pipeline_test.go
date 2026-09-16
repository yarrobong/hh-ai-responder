package runtime

import (
	"context"
	"strings"
	"testing"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/applicationsubmission"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type routePipelineReader struct {
	applicability applicationprocessing.Applicability
	description   string
	appCalls      int
}

func (r *routePipelineReader) ReadDescription(context.Context, int) (string, error) {
	return r.description, nil
}
func (r *routePipelineReader) ReadApplicability(context.Context, vacancy.Vacancy) (applicationprocessing.Applicability, error) {
	r.appCalls++
	return r.applicability, nil
}
func (r *routePipelineReader) ReadTest(context.Context, int) (applicationprocessing.TestSnapshot, error) {
	return applicationprocessing.TestSnapshot{}, nil
}

type routePipelineCandidate struct{}

func (routePipelineCandidate) ResolveForVacancy(context.Context, vacancy.Vacancy, string) (candidatecontext.CandidateContext, error) {
	return candidatecontext.CandidateContext{}, nil
}

type routePipelineAnalyzer struct{}

func (routePipelineAnalyzer) Analyze(context.Context, vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	return vacancyanalysis.Assessment{Apply: true, Score: 90}, nil
}

type routePipelineLetter struct {
	resumeTitle string
}

func (l *routePipelineLetter) Generate(_ context.Context, input coverletter.Input) (coverletter.Result, error) {
	l.resumeTitle = input.Candidate.ResumeTitle
	return coverletter.Result{Letter: "letter for " + input.Candidate.ResumeTitle}, nil
}

type routePipelinePolicy struct{}

func (routePipelinePolicy) EarlyReject(vacancy.Vacancy) string               { return "" }
func (routePipelinePolicy) DescriptionReject(vacancy.Vacancy, string) string { return "" }
func (routePipelinePolicy) Decide(vacancyanalysis.Assessment) (applicationprocessing.Decision, string) {
	return applicationprocessing.DecisionMatch, ""
}
func (routePipelinePolicy) ReconcileApplicability(vacancy.Vacancy, applicationprocessing.Applicability, vacancyanalysis.CandidateFacts, vacancyanalysis.Assessment) (vacancyanalysis.Assessment, applicationprocessing.Decision, string, error) {
	return vacancyanalysis.Assessment{Apply: true, Score: 90}, applicationprocessing.DecisionMatch, "", nil
}

type routePipelineExecutor struct {
	last applicationsubmission.ApplicationRequest
}

func (e *routePipelineExecutor) SubmitApplication(_ context.Context, request applicationsubmission.ApplicationRequest) (applicationsubmission.ExecutionResult, error) {
	e.last = request
	return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionAccepted, TransportTried: true}, nil
}

func TestCareerAgentRoutePropagatesSelectedResumeThroughPreparationAndSubmission(t *testing.T) {
	responder := &HHAIResponder{careerAgentResumes: []careeragent.ResumeProfile{
		{ID: "resume-a", Hash: "hash-a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		{ID: "resume-b", Hash: "hash-b", Title: "Integration specialist", Skills: []string{"API", "SQL"}, Enabled: true},
	}}
	v := Vacancy{ID: 700, Name: "Integration specialist API", Description: "API SQL integration"}
	route := responder.routeResumeForVacancy(v)
	if route.Status != careeragent.RouteSelected || route.SelectedResumeID != "resume-b" {
		t.Fatalf("router did not select Resume B: %+v", route)
	}
	if len(route.AlternativeScores) != 2 {
		t.Fatalf("router did not expose both resume scores: %+v", route.AlternativeScores)
	}

	reader := &routePipelineReader{description: "API SQL integration", applicability: applicationprocessing.Applicability{CanApply: true, CanApplyKnown: true, AlreadyRespondedKnown: true, LetterRequired: true, LetterRequiredKnown: true}}
	letter := &routePipelineLetter{}
	service := applicationprocessing.NewService(applicationprocessing.Dependencies{
		Vacancies: reader, Candidate: routePipelineCandidate{}, Analyzer: routePipelineAnalyzer{}, CoverLetter: letter, Policy: routePipelinePolicy{},
	})
	request := applicationprocessing.Request{
		Vacancy: v, ResumeID: "hash-b", ResumeTitle: "Integration specialist",
		Candidate:   vacancyanalysis.CandidateFacts{ResumeTitle: "Integration specialist", Skills: "API, SQL"},
		LetterFacts: coverletter.CandidateFacts{ResumeTitle: "Integration specialist", Skills: "API, SQL"},
	}
	prepared, err := service.Prepare(context.Background(), request)
	if err != nil || prepared.Prepared == nil {
		t.Fatalf("preparation failed: result=%+v err=%v", prepared, err)
	}
	if prepared.Prepared.ResumeID != "hash-b" || letter.resumeTitle != "Integration specialist" {
		t.Fatalf("selected resume was lost during preparation: prepared=%+v letter=%q", prepared.Prepared, letter.resumeTitle)
	}

	executor := &routePipelineExecutor{}
	submission := applicationsubmission.NewService(applicationsubmission.Dependencies{Vacancies: routeSubmissionReader{state: applicationsubmission.Applicability{ArchivedKnown: true, AlreadyRespondedKnown: true, TestPresentKnown: true, LetterRequiredKnown: true, CanApply: true, CanApplyKnown: true}}, Executor: executor}, applicationsubmission.Options{WriteEnabled: true})
	result, err := submission.Submit(context.Background(), applicationsubmission.Input{Prepared: applicationsubmission.PreparedApplication{VacancyID: 700, Vacancy: vacancy.Vacancy{ID: 700, Links: map[string]string{"desktop": "https://hh.example/vacancy/700"}}, ResumeID: prepared.Prepared.ResumeID, ResumeTitle: prepared.Prepared.ResumeTitle, CoverLetter: prepared.Prepared.CoverLetter}, CurrentResumeID: "hash-b", RequireCurrentResumeID: true})
	if err != nil || result.Status != applicationsubmission.StatusSubmitted || executor.last.ResumeID != "hash-b" {
		t.Fatalf("submission lost selected resume: result=%+v err=%v request=%+v", result, err, executor.last)
	}
	if reader.appCalls != 1 {
		t.Fatalf("preparation did not perform read-only applicability check: %d", reader.appCalls)
	}
}

func TestSelectedResumeProjectionReachesAIInput(t *testing.T) {
	responder := &HHAIResponder{resumeFactsByHash: map[string]ResumeFacts{
		"hash-b": {ExperienceText: "B-specific experience"},
	}}
	request := responder.applicationProcessingRequest(vacancy.Vacancy{ID: 701}, ResumeItem{
		Hash: "hash-b", Title: "Resume B", Skills: "API, SQL", Area: "Екатеринбург", Salary: "80000 руб",
	}, LegacyCandidateContext{
		ResumeTitle: "Resume A", Skills: "Python, Django", Experience: "A-specific experience", Location: "Москва", Salary: "120000 руб",
	}, 0)
	if request.Candidate.ResumeTitle != "Resume B" || request.Candidate.Skills != "API, SQL" || request.Candidate.Experience != "B-specific experience" {
		t.Fatalf("selected resume facts did not reach AI candidate input: %+v", request.Candidate)
	}
	if request.Candidate.ResumeTitle == "Resume A" || request.Candidate.Skills == "Python, Django" {
		t.Fatalf("previous resume leaked into AI candidate input: %+v", request.Candidate)
	}
	_, userPrompt := vacancyanalysis.BuildPrompt(vacancyanalysis.Input{
		Candidate:   request.Candidate,
		Vacancy:     vacancy.Vacancy{ID: 701, Name: "Integration specialist"},
		Description: "API SQL integration",
	})
	if !strings.Contains(userPrompt, "Название резюме: Resume B") || !strings.Contains(userPrompt, "Навыки: API, SQL") || !strings.Contains(userPrompt, "B-specific experience") {
		t.Fatalf("selected resume projection did not reach AI prompt: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "Resume A") || strings.Contains(userPrompt, "Python, Django") || strings.Contains(userPrompt, "A-specific experience") {
		t.Fatalf("previous resume leaked into AI prompt: %s", userPrompt)
	}
}

func TestAIDecisionBreakdownKeepsPrimarySafetyReason(t *testing.T) {
	summary := RunSummaryResult{}
	missing := VacancyEvaluation{Score: 90, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "Kafka", Status: hardRequirementStatusMissing}}}
	unknown := VacancyEvaluation{Score: 90, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "FastAPI", Status: hardRequirementStatusUnknown}}}
	applyFalse := VacancyEvaluation{Score: 40, Apply: false}
	match := VacancyEvaluation{Score: 90, Apply: true}
	for _, evaluation := range []VacancyEvaluation{missing, unknown, applyFalse, match} {
		trace := CareerAgentVacancyResult{}
		recordAIDecisionBreakdown(&summary, &trace, evaluation, 65)
	}
	if summary.AIApplyTrue != 3 || summary.AIApplyFalse != 1 || summary.AIHardMissing != 1 || summary.AIHardUnknown != 1 || summary.AIMatched != 1 || summary.AIRejected != 2 || summary.AIReviewed != 1 {
		t.Fatalf("unexpected AI breakdown: %+v", summary)
	}
	if summary.AIReasonCounts[AIReasonHardMissing] != 1 || summary.AIReasonCounts[AIReasonHardUnknown] != 1 || summary.AIReasonCounts[AIReasonApplyFalse] != 1 || summary.AIReasonCounts[AIReasonMatch] != 1 {
		t.Fatalf("primary AI reason counts are wrong: %+v", summary.AIReasonCounts)
	}
}

type routeSubmissionReader struct {
	state applicationsubmission.Applicability
}

func (r routeSubmissionReader) ReadApplicability(context.Context, vacancy.Vacancy) (applicationsubmission.Applicability, error) {
	return r.state, nil
}
