package applicationprocessing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/testanswer"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type Dependencies struct {
	Vacancies   VacancyReader
	Candidate   CandidateContextResolver
	Analyzer    VacancyAnalyzer
	CoverLetter CoverLetterGenerator
	TestAnswer  TestAnswerGenerator
	Semantic    SemanticHintProvider
	Clarifier   CandidateClarifier
	Policy      Policy
}

type Service struct {
	deps Dependencies
}

type Result struct {
	Outcome          Outcome
	Reason           string
	Vacancy          vacancy.Vacancy
	Description      string
	Applicability    *Applicability
	Analysis         *vacancyanalysis.Assessment
	CandidateContext candidatecontext.CandidateContext
	Prepared         *PreparedApplication
}

func NewService(dependencies Dependencies) *Service {
	return &Service{deps: dependencies}
}

func (s *Service) Prepare(ctx context.Context, input Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("application processing context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.deps.Vacancies == nil || s.deps.Candidate == nil || s.deps.Analyzer == nil || s.deps.Policy == nil {
		return Result{}, errors.New("application processing is not configured")
	}

	if reason := s.deps.Policy.EarlyReject(input.Vacancy); reason != "" {
		return Result{Outcome: OutcomeSkipped, Reason: reason, Vacancy: input.Vacancy}, nil
	}
	description, err := s.deps.Vacancies.ReadDescription(ctx, input.Vacancy.ID)
	if err != nil {
		return Result{}, fmt.Errorf("read vacancy description: %w", err)
	}
	result := Result{Vacancy: input.Vacancy, Description: description}
	if strings.TrimSpace(description) == "" {
		result.Outcome, result.Reason = OutcomeSkipped, "vacancy description is empty"
		return result, nil
	}
	if reason := s.deps.Policy.DescriptionReject(input.Vacancy, description); reason != "" {
		result.Outcome, result.Reason = OutcomeSkipped, reason
		return result, nil
	}

	resolved, err := s.deps.Candidate.ResolveForVacancy(ctx, input.Vacancy, description)
	if err != nil {
		return Result{}, fmt.Errorf("resolve candidate context: %w", err)
	}
	input.Candidate.SafeContext = resolved
	input.LetterFacts.SafeContext = resolved
	result.CandidateContext = resolved
	assessment, err := s.deps.Analyzer.Analyze(ctx, vacancyanalysis.Input{
		Candidate: input.Candidate, Vacancy: input.Vacancy, Description: description,
		Salary: vacancy.FormatCompensation(&input.Vacancy.Compensation), Location: input.Vacancy.Area.Name,
		WorkSchedule: input.Vacancy.WorkSchedule, IncludeKeywords: append([]string(nil), input.IncludeKeywords...),
	})
	if err != nil {
		return Result{}, fmt.Errorf("analyze vacancy: %w", err)
	}
	result.Analysis = &assessment

	decision, reason := s.deps.Policy.Decide(assessment)
	if decision == DecisionReviewRequired {
		s.createClarifications(ctx, input.Vacancy, assessment)
		return Result{Outcome: OutcomeNeedsCandidateInput, Reason: reason, Vacancy: input.Vacancy, Description: description, Analysis: &assessment, CandidateContext: resolved}, nil
	}
	if decision != DecisionMatch {
		return Result{Outcome: OutcomeSkipped, Reason: reason, Vacancy: input.Vacancy, Description: description, Analysis: &assessment, CandidateContext: resolved}, nil
	}

	applicability, err := s.deps.Vacancies.ReadApplicability(ctx, input.Vacancy)
	if err != nil {
		return Result{}, fmt.Errorf("read vacancy applicability: %w", err)
	}
	result.Applicability = &applicability
	assessment, decision, reason, err = s.deps.Policy.ReconcileApplicability(input.Vacancy, applicability, input.Candidate, assessment)
	if err != nil {
		return Result{}, fmt.Errorf("reconcile vacancy applicability: %w", err)
	}
	result.Analysis = &assessment
	if decision == DecisionReviewRequired {
		s.createClarifications(ctx, input.Vacancy, assessment)
		result.Outcome, result.Reason = OutcomeNeedsCandidateInput, reason
		return result, nil
	}
	if decision != DecisionMatch {
		result.Outcome, result.Reason = OutcomeSkipped, reason
		return result, nil
	}
	if input.ApplicationLimitReached {
		result.Outcome, result.Reason = OutcomeSkipped, "per-run application limit reached"
		return result, nil
	}

	letter := ""
	var letterResult coverletter.Result
	if applicability.LetterRequired || input.ForceLetter {
		if s.deps.CoverLetter == nil {
			return Result{}, errors.New("cover letter preparation is not configured")
		}
		semanticHints := append([]coverletter.SemanticHint(nil), input.SemanticHints...)
		if s.deps.Semantic != nil {
			if hints, hintErr := s.deps.Semantic.Hints(ctx, input.Vacancy, description, assessment); hintErr == nil {
				semanticHints = hints
			}
		}
		letterInput := coverletter.Input{
			Candidate: input.LetterFacts, Stories: append([]candidate.CandidateStory(nil), input.Stories...),
			Vacancy: input.Vacancy, Description: description, Assessment: &assessment,
			SemanticHints: semanticHints, ExtraPrompt: input.ExtraLetterPrompt,
		}
		var err error
		if fallbackGenerator, ok := s.deps.CoverLetter.(FallbackCoverLetterGenerator); ok {
			letterResult, err = fallbackGenerator.GenerateWithFallback(ctx, letterInput)
		} else {
			letterResult, err = s.deps.CoverLetter.Generate(ctx, letterInput)
		}
		if err != nil {
			return Result{}, fmt.Errorf("prepare cover letter: %w", err)
		}
		letter = letterResult.Letter
		if strings.TrimSpace(letter) == "" {
			return Result{}, errors.New("prepare cover letter returned empty letter")
		}
	}

	var preparedTest *PreparedTest
	if applicability.TestPresent {
		if s.deps.TestAnswer == nil {
			return Result{}, errors.New("test answer preparation is not configured")
		}
		snapshot, err := s.deps.Vacancies.ReadTest(ctx, input.Vacancy.ID)
		if err != nil {
			return Result{}, fmt.Errorf("read vacancy test: %w", err)
		}
		if len(snapshot.Tasks) == 0 {
			return Result{}, fmt.Errorf("vacancy marked with test but no tasks returned for vacancy %d", input.Vacancy.ID)
		}
		answers, err := s.deps.TestAnswer.Generate(ctx, testanswer.Input{Tasks: snapshot.Tasks, Contacts: input.Contacts, GitHubURL: input.GitHubURL, ExtraPrompt: input.ExtraTestPrompt})
		if err != nil {
			return Result{}, fmt.Errorf("prepare test answers: %w", err)
		}
		if len(answers.Answers) != len(snapshot.Tasks) {
			return Result{}, fmt.Errorf("incomplete test answers: got %d, expected %d", len(answers.Answers), len(snapshot.Tasks))
		}
		preparedTest = &PreparedTest{Metadata: snapshot.Metadata, Tasks: append([]testanswer.Task(nil), snapshot.Tasks...), Answers: append([]testanswer.ProposedAnswer(nil), answers.Answers...)}
	}

	result.Outcome = OutcomePrepared
	result.Prepared = &PreparedApplication{
		VacancyID: input.Vacancy.ID, Vacancy: input.Vacancy, ResumeID: input.ResumeID, ResumeTitle: input.ResumeTitle,
		CoverLetter: letter, CoverLetterStatus: letterResult.Status, CoverLetterEvidence: append([]coverletter.DraftEvidence(nil), letterResult.Evidence...), CoverLetterFallbackReason: letterResult.FallbackReason,
		Test: preparedTest, Analysis: assessment, CandidateContext: resolved,
	}
	if letterResult.Status == coverletter.DraftStatusReviewRequired {
		result.Outcome = OutcomeManualReview
		result.Reason = "cover letter requires manual review: " + letterResult.FallbackReason
		return result, nil
	}
	return result, nil
}

func (s *Service) createClarifications(ctx context.Context, vacancyValue vacancy.Vacancy, assessment vacancyanalysis.Assessment) {
	if s.deps.Clarifier == nil {
		return
	}
	for _, requirement := range assessment.HardRequirements {
		if requirement.Status != vacancyanalysis.HardRequirementStatusUnknown || requirement.Soft || vacancyanalysis.IsOptionalRequirement(requirement) {
			continue
		}
		// Clarification creation is intentionally best-effort, matching the
		// legacy review path: failure must never turn uncertainty into a write.
		_ = s.deps.Clarifier.Create(ctx, vacancyValue, requirement)
	}
}
