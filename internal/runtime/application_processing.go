package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptpolicy "hh-ai-responder/internal/usecase/applicationattemptpolicy"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
)

// ApplyVacancies keeps batch cadence, counters, local events, and the legacy
// submission compatibility path in the root. Vacancy preparation itself is
// delegated to the importable applicationprocessing service.
func (r *HHAIResponder) ApplyVacancies() error {
	if !r.autoApply {
		logger.Info("Automatic applications disabled by configuration")
		return nil
	}
	r.loadAlreadyRespondedState()
	r.clearVacancyPreflightCache()
	summary := RunSummaryResult{Type: "run_summary"}
	defer func() { summary.VacanciesSeen = summary.VacanciesProcessed; r.writeEvent(summary) }()
	if r.attemptStoreInitErr != nil {
		summary.Errors++
		return fmt.Errorf("automatic application attempt store unavailable: %w", r.attemptStoreInitErr)
	}

	resume := r.GetCurrentResume()
	if resume == nil {
		return errors.New("resume not found")
	}
	baseCandidate, resolver, err := r.canonicalCandidateContext(*resume)
	if err != nil {
		return fmt.Errorf("canonical candidate read failed: %w", err)
	}
	successfulApplicationsInRun := 0
	budget := newAutomaticApplicationBudget(r.maxApplicationsPerRun)
	eligibleVacancies := 0
	vacancies, err := r.fetchVacanciesFromSearchProfiles(&summary)
	if err != nil {
		summary.Errors++
		logger.Error("Failed to fetch vacancies: %v", err)
		return err
	}
	for _, profile := range summary.SearchProfiles {
		logger.Info("SEARCH PROFILE — %s: %d vacancies", profile.Name, profile.VacanciesFetched)
	}

	for _, value := range vacancies {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		summary.VacanciesProcessed++
		gate, gateErr := r.automaticApplicationGate(ctxOrBackground(r.ctx), value.ID)
		if gateErr != nil {
			summary.Errors++
			r.skipVacancyForAttempt(value, value.Links["desktop"], "automatic application attempt authority unavailable: "+gate.Reason, "", "")
			logger.Warn("Could not read automatic application attempt state for vacancy %d: %v", value.ID, gateErr)
			continue
		}
		if gate.Classification == attemptpolicy.BlockingConfirmed || gate.Classification == attemptpolicy.BlockingUnresolved {
			summary.BlockedAttempts++
			attemptID, attemptState := "", ""
			if gate.Attempt != nil {
				attemptID, attemptState = gate.Attempt.AttemptID, string(gate.Attempt.State)
			}
			if gate.Classification == attemptpolicy.BlockingUnresolved && gate.Attempt != nil {
				reconciliationResult, reconcileErr := r.reconcileAutomaticAttempt(ctxOrBackground(r.ctx), gate)
				if reconcileErr != nil {
					summary.Errors++
					logger.Warn("Could not reconcile automatic application attempt for vacancy %d: %v", value.ID, reconcileErr)
				} else if reconciliationResult.Status == applicationreconciliation.StatusConfirmed {
					summary.ReconciledConfirmed++
				}
				if reconciliationResult.Attempt.State != "" {
					attemptState = string(reconciliationResult.Attempt.State)
				}
				if reconcileErr != nil || reconciliationResult.Attempt.State != domain.StateTargetResponseConfirmed {
					summary.UnresolvedAttempts++
				}
			}
			r.skipVacancyForAttempt(value, value.Links["desktop"], gate.Reason, attemptID, attemptState)
			continue
		}
		if r.isAlreadyResponded(value.ID) {
			summary.PreviouslyRespondedSkipped++
			r.skipVacancy(value, value.Links["desktop"], "previously confirmed already responded", nil)
			continue
		}
		// Keep the legacy vacancy-limit ordering: these checks happen before
		// counting a vacancy as eligible or reading its description.
		if reason := (rootApplicationPolicy{responder: r}).EarlyReject(value); reason != "" {
			summary.DeterministicSkipped++
			r.skipVacancy(value, value.Links["desktop"], reason, nil)
			continue
		}
		vacancyURL := value.Links["desktop"]
		if r.maxVacanciesPerRun > 0 && eligibleVacancies >= r.maxVacanciesPerRun {
			summary.VacancyLimitSkipped++
			r.skipVacancy(value, vacancyURL, "per-run vacancy limit reached", nil)
			return nil
		}
		eligibleVacancies++
		applicationCount := successfulApplicationsInRun
		if r.dryRun {
			applicationCount = budget.plannedPreviews
		}
		if budget.reached(r.dryRun) {
			summary.ApplicationLimitSkipped++
			r.skipVacancy(value, vacancyURL, "per-run application dispatch limit reached", nil)
			return nil
		}

		prep, prepErr := r.prepareApplication(value, *resume, baseCandidate, resolver, applicationCount)
		if prepErr != nil {
			summary.Errors++
			r.skipVacancy(value, vacancyURL, prepErr.Error(), nil)
			logger.Warn("Could not prepare vacancy %d: %v", value.ID, prepErr)
			continue
		}
		if prep.Analysis != nil {
			summary.AIEvaluated++
		}
		if prep.Applicability != nil {
			preflight := applicationPreflight(value.ID, *prep.Applicability)
			r.writeEvent(preflight.event())
			r.rememberConfirmedPreflight(preflight)
		}

		switch prep.Outcome {
		case applicationprocessing.OutcomeNeedsCandidateInput:
			summary.ReviewRequired++
			r.writeApplicationReview(value, vacancyURL, prep, prep.Reason)
			continue
		case applicationprocessing.OutcomeSkipped, applicationprocessing.OutcomeAlreadyResponded, applicationprocessing.OutcomeUnavailable:
			if prep.Reason == "per-run application limit reached" && prep.Analysis != nil {
				evaluation := *prep.Analysis
				summary.Matched++
				r.writeEvent(VacancyMatchResult{Type: "vacancy_match", VacancyID: value.ID, Name: value.Name, URL: vacancyURL, Score: evaluation.Score, Reasons: evaluation.Reasons, Missing: evaluation.Missing, HardRequirementsMissing: hardRequirementsMissing(evaluation), HardRequirements: evaluation.HardRequirements})
				summary.ApplicationLimitSkipped++
				r.skipVacancy(value, vacancyURL, prep.Reason, &evaluation.Score)
				return nil
			}
			summary.DeterministicSkipped++
			if prep.Analysis != nil {
				score := prep.Analysis.Score
				r.skipVacancyWithEvaluation(value, vacancyURL, prep.Reason, &score, hardRequirementsMissing(*prep.Analysis), prep.Analysis.HardRequirements)
			} else {
				r.skipVacancy(value, vacancyURL, prep.Reason, nil)
			}
			continue
		case applicationprocessing.OutcomePrepared:
			// continue below
		default:
			summary.Errors++
			r.skipVacancy(value, vacancyURL, "unknown application preparation outcome", nil)
			continue
		}

		if prep.Analysis == nil || prep.Prepared == nil {
			summary.Errors++
			r.skipVacancy(value, vacancyURL, "application preparation returned no plan", nil)
			continue
		}
		evaluation := *prep.Analysis
		summary.Matched++
		r.writeEvent(VacancyMatchResult{Type: "vacancy_match", VacancyID: value.ID, Name: value.Name, URL: vacancyURL, Score: evaluation.Score, Reasons: evaluation.Reasons, Missing: evaluation.Missing, HardRequirementsMissing: hardRequirementsMissing(evaluation), HardRequirements: evaluation.HardRequirements})
		logger.Info("MATCH — vacancy %d: %d/100", value.ID, evaluation.Score)

		submissionResult, solutions, sendErr := r.submitPreparedApplication(*prep.Prepared)
		if r.dryRun {
			if sendErr != nil {
				summary.Errors++
				r.writeApplicationError(value, vacancyURL, resume, sendErr)
				continue
			}
			budget.recordPreview()
			summary.WouldApply++
			r.writeApplicationPreview(value, vacancyURL, resume, prep.Prepared.CoverLetter, solutions)
			continue
		}
		if budget.recordExecution(submissionResult.Execution) {
			summary.DispatchAttempts = budget.dispatchAttempts
		}
		if sendErr != nil {
			if strings.Contains(sendErr.Error(), "negotiations-limit-exceeded") {
				logger.Warn("Negotiations limit exceeded!")
				return nil
			}
			summary.Errors++
			r.writeApplicationError(value, vacancyURL, resume, sendErr)
			continue
		}
		if submissionResult.Status == applicationsubmission.StatusSubmitted {
			successfulApplicationsInRun++
			summary.Applied++
			r.writeApplicationSuccess(value, vacancyURL, resume, prep.Prepared.CoverLetter, solutions)
			continue
		}
		summary.Errors++
		r.writeApplicationError(value, vacancyURL, resume, errors.New(submissionResult.Reason))
	}
	logger.Info("Finished processing!")
	return nil
}

func applicationPreflight(id int, value applicationprocessing.Applicability) VacancyPreflight {
	return VacancyPreflight{VacancyID: id, Available: value.Available, Archived: value.Archived, ArchivedKnown: value.ArchivedKnown, AlreadyResponded: value.AlreadyResponded, AlreadyRespondedKnown: value.AlreadyRespondedKnown, TestPresent: value.TestPresent, TestPresentKnown: value.TestPresentKnown, LetterRequired: value.LetterRequired, LetterRequiredKnown: value.LetterRequiredKnown, CanApply: value.CanApply, CanApplyKnown: value.CanApplyKnown, Area: value.Area, AreaKnown: value.AreaKnown, WorkSchedule: value.WorkSchedule, WorkScheduleKnown: value.WorkScheduleKnown, WorkExperience: value.WorkExperience, WorkExperienceKnown: value.WorkExperienceKnown, ResponseURL: value.ResponseURL}
}

func (r *HHAIResponder) writeApplicationReview(value Vacancy, url string, prep applicationprocessing.Result, reason string) {
	if prep.Analysis == nil {
		return
	}
	evaluation := *prep.Analysis
	r.writeEvent(VacancyReviewRequiredResult{Type: "vacancy_review_required", VacancyID: value.ID, Name: value.Name, URL: url, Score: evaluation.Score, Apply: evaluation.Apply, Reasons: append(append([]string{}, evaluation.Reasons...), reason), Missing: evaluation.Missing, HardRequirementsUnknown: hardRequirementsUnknown(evaluation), HardRequirements: evaluation.HardRequirements})
}

func (r *HHAIResponder) writeApplicationError(value Vacancy, url string, resume *ResumeItem, err error) {
	r.writeEvent(ErrorResult{Type: "application_error", Context: map[string]any{"vacancy_id": value.ID, "vacancy_name": value.Name, "url": url, "resume": r.resumeHash, "resume_title": resume.Title}, Error: err.Error(), Time: time.Now()})
}

func (r *HHAIResponder) writeApplicationPreview(value Vacancy, url string, resume *ResumeItem, letter string, solutions []QAPair) {
	var count *int
	if value.TotalResponsesCountKnown {
		v := value.TotalResponsesCount + 1
		count = &v
	}
	r.writeEvent(ApplyResult{Type: "application_preview", Resume: r.resumeHash, ResumeTitle: resume.Title, VacancyID: value.ID, URL: url, Name: value.Name, Letter: letter, AppliedAt: time.Now(), ResponsesCount: count, TestSolutions: solutions})
}

func (r *HHAIResponder) writeApplicationSuccess(value Vacancy, url string, resume *ResumeItem, letter string, solutions []QAPair) {
	var count *int
	if value.TotalResponsesCountKnown {
		v := value.TotalResponsesCount + 1
		count = &v
	}
	r.writeEvent(ApplyResult{Type: "application", Resume: r.resumeHash, ResumeTitle: resume.Title, VacancyID: value.ID, URL: url, Name: value.Name, Letter: letter, AppliedAt: time.Now(), ResponsesCount: count, TestSolutions: solutions})
}
