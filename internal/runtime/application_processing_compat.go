package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/testanswer"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type rootApplicationReader struct{ responder *HHAIResponder }

func (r rootApplicationReader) ReadDescription(ctx context.Context, id int) (string, error) {
	if r.responder == nil {
		return "", errors.New("HH responder is not configured")
	}
	if cached, ok := r.responder.careerAgentDetailCache[id]; ok && cached.Description != "" {
		return cached.Description, nil
	}
	return r.responder.getVacancyDescriptionContext(ctx, id)
}

func (r rootApplicationReader) ReadApplicability(ctx context.Context, value vacancy.Vacancy) (applicationprocessing.Applicability, error) {
	if r.responder == nil {
		return applicationprocessing.Applicability{}, errors.New("HH responder is not configured")
	}
	preflight, err := r.responder.getVacancyPreflightContext(ctx, value)
	if err != nil {
		return applicationprocessing.Applicability{}, err
	}
	return applicationprocessing.Applicability{
		Available: preflight.Available, Archived: preflight.Archived, ArchivedKnown: preflight.ArchivedKnown,
		AlreadyResponded: preflight.AlreadyResponded, AlreadyRespondedKnown: preflight.AlreadyRespondedKnown,
		TestPresent: preflight.TestPresent, TestPresentKnown: preflight.TestPresentKnown,
		LetterRequired: preflight.LetterRequired, LetterRequiredKnown: preflight.LetterRequiredKnown,
		CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown,
		Area: preflight.Area, AreaKnown: preflight.AreaKnown, WorkSchedule: preflight.WorkSchedule,
		WorkScheduleKnown: preflight.WorkScheduleKnown, WorkExperience: preflight.WorkExperience,
		WorkExperienceKnown: preflight.WorkExperienceKnown, ResponseURL: preflight.ResponseURL,
	}, nil
}

func (r rootApplicationReader) ReadTest(ctx context.Context, id int) (applicationprocessing.TestSnapshot, error) {
	if r.responder == nil {
		return applicationprocessing.TestSnapshot{}, errors.New("HH responder is not configured")
	}
	responseURL := r.responder.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", id))
	tests, err := r.responder.getVacancyTestsContext(ctx, responseURL)
	if err != nil {
		return applicationprocessing.TestSnapshot{}, err
	}
	test, ok := tests[fmt.Sprint(id)]
	if !ok {
		return applicationprocessing.TestSnapshot{}, fmt.Errorf("vacancy marked with test but no test data found for vacancy %d", id)
	}
	return applicationprocessing.TestSnapshot{
		Metadata: applicationprocessing.TestMetadata{UIDPK: test.UIDPk, GUID: test.GUID, StartTime: test.StartTime, Required: test.Required},
		Tasks:    legacyTestTasks(test.Tasks),
	}, nil
}

type rootApplicationCandidate struct{ resolver *CandidateContextResolver }

func (r rootApplicationCandidate) ResolveForVacancy(ctx context.Context, value vacancy.Vacancy, description string) (candidatecontext.CandidateContext, error) {
	if ctx == nil {
		return candidatecontext.CandidateContext{}, errors.New("candidate context is nil")
	}
	if err := ctx.Err(); err != nil {
		return candidatecontext.CandidateContext{}, err
	}
	if r.resolver == nil {
		return candidatecontext.CandidateContext{}, errors.New("candidate resolver is not configured")
	}
	return r.resolver.ResolveForVacancy(value, description)
}

type rootApplicationAnalyzer struct{ client *AIClient }

func (a rootApplicationAnalyzer) Analyze(ctx context.Context, input vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	if a.client == nil {
		return vacancyanalysis.Assessment{}, errors.New("AI completion provider is not configured")
	}
	service := vacancyanalysis.NewService(vacancyanalysis.Dependencies{Completion: a.client.provider}, vacancyanalysis.Options{
		Model: a.client.model, Attempts: a.client.attempts, MaxTokens: 1024, Temperature: 0.1, SemanticRetryDelay: aiRetryDelay,
	})
	return service.Analyze(ctx, input)
}

type rootApplicationCoverLetter struct{ client *AIClient }

func (a rootApplicationCoverLetter) Generate(ctx context.Context, input coverletter.Input) (coverletter.Result, error) {
	if a.client == nil {
		return coverletter.Result{}, errors.New("AI completion provider is not configured")
	}
	return coverletter.NewService(coverletter.Dependencies{Completion: a.client.provider}, coverletter.Options{Model: a.client.model, MaxTokens: 512, Temperature: 0.5}).Generate(ctx, input)
}

type rootApplicationTestAnswer struct{ client *AIClient }

func (a rootApplicationTestAnswer) Generate(ctx context.Context, input testanswer.Input) (testanswer.Result, error) {
	if a.client == nil {
		return testanswer.Result{}, errors.New("AI completion provider is not configured")
	}
	return testanswer.NewService(testanswer.Dependencies{Completion: a.client.provider}, testanswer.Options{
		Model: a.client.model, Attempts: a.client.attempts, Temperature: 0.2, SemanticRetryDelay: aiRetryDelay,
	}).Generate(ctx, input)
}

type rootApplicationSemanticHints struct {
	responder *HHAIResponder
	resolver  *CandidateContextResolver
}

func (s rootApplicationSemanticHints) Hints(_ context.Context, value vacancy.Vacancy, description string, assessment vacancyanalysis.Assessment) ([]coverletter.SemanticHint, error) {
	if s.responder == nil {
		return nil, nil
	}
	return applicationSemanticHints(s.responder.coverLetterSemanticExamples(value, description, assessment, s.resolver)), nil
}

type rootApplicationClarifier struct{ responder *HHAIResponder }

func (c rootApplicationClarifier) Create(_ context.Context, value vacancy.Vacancy, requirement vacancyanalysis.HardRequirementEvaluation) error {
	if c.responder == nil {
		return errors.New("candidate clarification is not configured")
	}
	// The root adapter is temporary compatibility glue. The candidate-learning
	// lifecycle remains the only intended owner for future clarification wiring;
	// this call preserves the existing persisted question behavior in R12.4a.
	c.responder.addPendingQuestionForRequirement(value, requirement)
	return nil
}

type rootApplicationPolicy struct{ responder *HHAIResponder }

func (p rootApplicationPolicy) EarlyReject(value vacancy.Vacancy) string {
	if len(value.UserLabels) > 0 || value.Archived || value.ResponseURL != "" {
		return "already labeled, archived, or already responded"
	}
	if p.responder != nil && p.responder.maxResponses > 0 && value.TotalResponsesCount > p.responder.maxResponses {
		return "response count exceeds configured maximum"
	}
	if value.Links["desktop"] == "" {
		return "vacancy has no desktop link"
	}
	return ""
}

func (p rootApplicationPolicy) DescriptionReject(value vacancy.Vacancy, description string) string {
	if p.responder == nil {
		return ""
	}
	return deterministicVacancyRejectReasonWithCurrency(value, description, p.responder.minSalary, p.responder.minSalaryCurrency, p.responder.excludeKeywords)
}

func (p rootApplicationPolicy) Decide(assessment vacancyanalysis.Assessment) (applicationprocessing.Decision, string) {
	decision := vacancyDecision(assessment, p.responder.minMatchScore)
	switch decision {
	case VacancyMatch:
		return applicationprocessing.DecisionMatch, ""
	case VacancyReviewRequired:
		return applicationprocessing.DecisionReviewRequired, vacancyEvaluationRejectReason(assessment, p.responder.minMatchScore)
	default:
		return applicationprocessing.DecisionReject, vacancyEvaluationRejectReason(assessment, p.responder.minMatchScore)
	}
}

func (p rootApplicationPolicy) ReconcileApplicability(value vacancy.Vacancy, app applicationprocessing.Applicability, candidateFacts vacancyanalysis.CandidateFacts, assessment vacancyanalysis.Assessment) (vacancyanalysis.Assessment, applicationprocessing.Decision, string, error) {
	preflight := VacancyPreflight{
		VacancyID: value.ID, Available: app.Available, Archived: app.Archived, ArchivedKnown: app.ArchivedKnown,
		AlreadyResponded: app.AlreadyResponded, AlreadyRespondedKnown: app.AlreadyRespondedKnown,
		TestPresent: app.TestPresent, TestPresentKnown: app.TestPresentKnown,
		LetterRequired: app.LetterRequired, LetterRequiredKnown: app.LetterRequiredKnown,
		CanApply: app.CanApply, CanApplyKnown: app.CanApplyKnown, ResponseURL: app.ResponseURL,
		Area: app.Area, AreaKnown: app.AreaKnown, WorkSchedule: app.WorkSchedule, WorkScheduleKnown: app.WorkScheduleKnown,
		WorkExperience: app.WorkExperience, WorkExperienceKnown: app.WorkExperienceKnown,
	}
	decision, reason := vacancyPreflightDecision(preflight)
	if decision != VacancyMatch {
		return assessment, mapVacancyDecision(decision), "preflight: " + reason, nil
	}
	structured := value
	if app.AreaKnown {
		structured.Area.Name = app.Area
	}
	if app.WorkScheduleKnown {
		structured.WorkSchedule = app.WorkSchedule
	}
	if app.WorkExperienceKnown {
		structured.WorkExperience = app.WorkExperience
	}
	legacy := legacyCandidateFacts(candidateFacts)
	assessment.HardRequirements = mergeHardRequirements(localStructuredHardRequirements(preflight, legacy), assessment.HardRequirements)
	if err := validateHardRequirements(legacy, structured, assessment); err != nil {
		return assessment, applicationprocessing.DecisionReviewRequired, "structured preflight requirements could not be validated: " + err.Error(), nil
	}
	decision = vacancyDecision(assessment, p.responder.minMatchScore)
	return assessment, mapVacancyDecision(decision), vacancyEvaluationRejectReason(assessment, p.responder.minMatchScore), nil
}

func mapVacancyDecision(value VacancyDecision) applicationprocessing.Decision {
	switch value {
	case VacancyMatch:
		return applicationprocessing.DecisionMatch
	case VacancyReviewRequired:
		return applicationprocessing.DecisionReviewRequired
	default:
		return applicationprocessing.DecisionReject
	}
}

func legacyCandidateFacts(value vacancyanalysis.CandidateFacts) LegacyCandidateContext {
	return LegacyCandidateContext{
		FullName: value.FullName, ResumeTitle: value.ResumeTitle, Salary: value.Salary, Experience: value.Experience,
		Skills: value.Skills, Location: value.Location, Contacts: value.Contacts, EducationKnown: value.EducationKnown,
		EducationLevel: value.EducationLevel, EducationDetails: value.EducationDetails, TotalExperienceMonthsKnown: value.TotalExperienceMonthsKnown,
		TotalExperienceMonths: value.TotalExperienceMonths, Profile: value.Profile, SafeContext: value.SafeContext,
	}
}

func (r *HHAIResponder) applicationProcessingService(candidateResolver *CandidateContextResolver) *applicationprocessing.Service {
	return applicationprocessing.NewService(applicationprocessing.Dependencies{
		Vacancies: rootApplicationReader{responder: r}, Candidate: rootApplicationCandidate{resolver: candidateResolver},
		Analyzer: rootApplicationAnalyzer{client: r.ai}, CoverLetter: rootApplicationCoverLetter{client: r.ai}, TestAnswer: rootApplicationTestAnswer{client: r.ai},
		Semantic:  rootApplicationSemanticHints{responder: r, resolver: candidateResolver},
		Clarifier: rootApplicationClarifier{responder: r}, Policy: rootApplicationPolicy{responder: r},
	})
}

func (r *HHAIResponder) applicationProcessingRequest(value vacancy.Vacancy, resume ResumeItem, candidate LegacyCandidateContext, applicationsInRun int) applicationprocessing.Request {
	// The selected HH resume is the factual scope for resume routing and AI
	// evaluation. Keep common confirmed candidate knowledge, but do not pass
	// the previously active resume's title, salary, or skill list.
	selected := candidate
	selected.ResumeTitle = resume.Title
	if strings.TrimSpace(resume.Salary) != "" {
		selected.Salary = resume.Salary
	}
	if strings.TrimSpace(resume.Skills) != "" {
		selected.Skills = resume.Skills
	}
	if strings.TrimSpace(resume.Area) != "" {
		selected.Location = resume.Area
	}
	if r != nil && strings.TrimSpace(resume.Hash) != "" && r.resumeFactsByHash != nil {
		if facts, ok := r.resumeFactsByHash[resume.Hash]; ok && strings.TrimSpace(facts.ExperienceText) != "" {
			selected.Experience = facts.ExperienceText
		}
	}
	return applicationprocessing.Request{
		Vacancy: value, ResumeID: resume.Hash, ResumeTitle: resume.Title,
		Candidate:   vacancyanalysis.CandidateFacts{FullName: selected.FullName, ResumeTitle: selected.ResumeTitle, Salary: selected.Salary, Experience: selected.Experience, Skills: selected.Skills, Location: selected.Location, Contacts: selected.Contacts, EducationKnown: selected.EducationKnown, EducationLevel: selected.EducationLevel, EducationDetails: selected.EducationDetails, TotalExperienceMonthsKnown: selected.TotalExperienceMonthsKnown, TotalExperienceMonths: selected.TotalExperienceMonths, Profile: selected.Profile, SafeContext: selected.SafeContext},
		LetterFacts: coverLetterCandidateFacts(selected), Stories: append([]CandidateStory(nil), selected.Stories...), Contacts: r.contacts, GitHubURL: r.githubURL,
		ExtraLetterPrompt: r.extraLetterPrompt, ExtraTestPrompt: r.extraTestSolutionPrompt, ForceLetter: r.forceLetter, IncludeKeywords: append([]string(nil), r.includeKeywords...),
		ApplicationLimitReached: r.maxApplicationsPerRun > 0 && applicationsInRun >= r.maxApplicationsPerRun,
	}
}

func (r *HHAIResponder) prepareApplication(value vacancy.Vacancy, resume ResumeItem, candidate LegacyCandidateContext, resolver *CandidateContextResolver, applicationsInRun int) (applicationprocessing.Result, error) {
	if resolver == nil {
		return applicationprocessing.Result{}, errors.New("candidate resolver is not configured")
	}
	request := r.applicationProcessingRequest(value, resume, candidate, applicationsInRun)
	return r.applicationProcessingService(resolver).Prepare(r.ctx, request)
}

var _ applicationprocessing.Policy = rootApplicationPolicy{}
