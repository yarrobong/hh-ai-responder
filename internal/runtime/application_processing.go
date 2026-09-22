package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	"hh-ai-responder/internal/careeragent"
	attemptpolicy "hh-ai-responder/internal/usecase/applicationattemptpolicy"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
)

// ApplyVacancies keeps batch cadence, counters, local events, and the legacy
// submission compatibility path in the root. Vacancy preparation itself is
// delegated to the importable applicationprocessing service.
func (r *HHAIResponder) ApplyVacancies() error {
	if r == nil {
		return errors.New("HH responder is not configured")
	}
	if r.careerAgentMode == "shadow" {
		// Shadow is a hard safety mode, not a presentation flag. Keep this
		// guard at the orchestration boundary so conflicting config flags
		// cannot reach any mutation-capable service.
		r.dryRun, r.hhWriteEnabled = true, false
		r.autoChat, r.autoTouch, r.autoJobStatus = false, false, false
		r.chatMode = "off"
	}
	if !r.autoApply {
		logger.Info("Automatic applications disabled by configuration")
		return nil
	}
	r.loadAlreadyRespondedState()
	r.clearVacancyPreflightCache()
	summary := RunSummaryResult{Type: "run_summary"}
	accountingRecords, terminalOutcomes, stageStats := newRunAccounting()
	uniqueIDs := []int{}
	finish := func(trace CareerAgentVacancyResult, outcome, reason string) {
		if _, exists := accountingRecords[trace.VacancyID]; exists {
			summary.Errors++
			return
		}
		trace.Type = "career_agent_vacancy"
		trace.TerminalOutcome = outcome
		trace.BlockedReason = firstNonEmpty(trace.BlockedReason, reason)
		if r.careerAgentMode != "" && !trace.AIEvaluated && strings.TrimSpace(trace.AICallReason) == "" {
			trace.AICallReason = "not called: terminal path " + outcome
		}
		if trace.CheapFilterResult == "REJECT" && len(trace.CheapFilterReasons) == 0 && strings.TrimSpace(reason) != "" {
			trace.CheapFilterReasons = []string{reason}
		}
		trace.ProcessedAt = time.Now().UTC()
		accountingRecords[trace.VacancyID] = trace
		if r.careerAgentMode != "" {
			if _, entered := summary.routerEnteredVacancies[trace.VacancyID]; entered && trace.FinalDecision != "" {
				recordRouterOutcome(&summary, "", trace.FinalDecision)
			}
		}
		terminalOutcomes[outcome]++
		r.writeEvent(trace)
	}
	defer func() {
		finalizeDiscoveryCoverage(&summary)
		summary.VacanciesSeen = summary.VacanciesProcessed
		r.writeEvent(summary)
	}()
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
	dispatchBlocked := false
	vacancies, err := r.fetchVacanciesFromSearchProfiles(&summary)
	if err != nil {
		summary.Errors++
		logger.Error("Failed to fetch vacancies: %v", err)
		return err
	}
	for _, value := range vacancies {
		uniqueIDs = append(uniqueIDs, value.ID)
		recordStage(stageStats, "unique_discovery", "deduplicated vacancy", true, true)
	}
	defer func() {
		summary.TerminalOutcomes = terminalOutcomes
		summary.TotalTerminal = len(accountingRecords)
		summary.AccountingPass = validateCareerAgentAccounting(uniqueIDs, accountingRecords) == nil
		summary.ShadowWriteCount = r.careerAgentWriteCount
		summary.StageStats = stageStats
	}()
	for _, profile := range summary.SearchProfiles {
		logger.Info("SEARCH PROFILE — %s: %d vacancies", profile.Name, profile.VacanciesFetched)
	}

	for _, value := range vacancies {
		trace := CareerAgentVacancyResult{VacancyID: value.ID, Title: firstNonEmpty(value.Title, value.Name), Company: value.Company.Name, URL: value.Links["desktop"], FoundByProfiles: append([]string(nil), r.vacancySearchSources[value.ID]...), SearchCardEvidence: careerAgentEvidence(value), CheapFilterResult: "PASS", DetailFetchStatus: "NOT_STARTED"}
		summary.VacanciesProcessed++
		if err := r.ctx.Err(); err != nil {
			summary.Errors++
			r.skipVacancy(value, value.Links["desktop"], "context canceled before processing", nil)
			finish(trace, TerminalError, "context canceled before processing")
			continue
		}
		if dispatchBlocked {
			summary.ApplicationLimitSkipped++
			r.skipVacancy(value, value.Links["desktop"], "dispatch blocked after unresolved submission result", nil)
			finish(trace, TerminalApplicationLimit, "dispatch blocked after unresolved submission result")
			continue
		}
		recordStage(stageStats, "attempt_gate", "entered", true, false)
		gate, gateErr := r.automaticApplicationGate(ctxOrBackground(r.ctx), value.ID)
		if gateErr != nil {
			recordStage(stageStats, "attempt_gate", "error", false, true)
			summary.Errors++
			r.skipVacancyForAttempt(value, value.Links["desktop"], "automatic application attempt authority unavailable: "+gate.Reason, "", "")
			finish(trace, TerminalError, "automatic application attempt authority unavailable: "+gate.Reason)
			logger.Warn("Could not read automatic application attempt state for vacancy %d: %v", value.ID, gateErr)
			continue
		}
		if gate.Classification == attemptpolicy.BlockingConfirmed || gate.Classification == attemptpolicy.BlockingUnresolved {
			recordStage(stageStats, "attempt_gate", gate.Reason, false, true)
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
			finish(trace, TerminalAttemptBlocked, gate.Reason)
			continue
		}
		recordStage(stageStats, "attempt_gate", "clear", false, true)
		if r.isAlreadyResponded(value.ID) {
			recordStage(stageStats, "cheap_filters", "already responded", true, true)
			summary.PreviouslyRespondedSkipped++
			r.skipVacancy(value, value.Links["desktop"], "previously confirmed already responded", nil)
			trace.CheapFilterResult, trace.DetailFetchStatus = "REJECT", "NOT_REQUIRED"
			trace.FinalDecision, trace.FinalReasonCode = string(VacancyReject), "ALREADY_RESPONDED"
			finish(trace, TerminalAlreadyResponded, "previously confirmed already responded")
			continue
		}
		recordStage(stageStats, "cheap_filters", "entered", true, false)
		// Keep the legacy vacancy-limit ordering: these checks happen before
		// counting a vacancy as eligible or reading its description.
		if reason := (rootApplicationPolicy{responder: r}).EarlyReject(value); reason != "" {
			recordStage(stageStats, "cheap_filters", reason, false, true)
			summary.DeterministicSkipped++
			summary.Rejected++
			r.skipVacancy(value, value.Links["desktop"], reason, nil)
			trace.CheapFilterResult, trace.DetailFetchStatus = "REJECT", "NOT_REQUIRED"
			finish(trace, TerminalDeterministicReject, reason)
			continue
		}
		recordStage(stageStats, "cheap_filters", "passed", false, true)
		vacancyURL := value.Links["desktop"]
		if r.maxVacanciesPerRun > 0 && eligibleVacancies >= r.maxVacanciesPerRun {
			summary.VacancyLimitSkipped++
			r.skipVacancy(value, vacancyURL, "per-run vacancy limit reached", nil)
			trace.CheapFilterResult, trace.DetailFetchStatus = "PASS", "NOT_REQUIRED"
			finish(trace, TerminalVacancyLimit, "per-run vacancy limit reached")
			continue
		}
		eligibleVacancies++
		applicationCount := successfulApplicationsInRun
		if r.dryRun {
			applicationCount = budget.plannedPreviews
		}
		if budget.reached(r.dryRun) {
			summary.ApplicationLimitSkipped++
			r.skipVacancy(value, vacancyURL, "per-run application dispatch limit reached", nil)
			trace.DetailFetchStatus = "NOT_REQUIRED"
			finish(trace, TerminalApplicationLimit, "per-run application dispatch limit reached")
			continue
		}

		selectedResume := *resume
		selectedCandidate, selectedResolver := baseCandidate, resolver
		if r.careerAgentMode != "" {
			recordRouterEntry(&summary, value, r.careerAgentSearchSources[value.ID])
			preliminary := r.preliminaryRouteForVacancy(value)
			trace.PreliminaryRoute, trace.PreliminaryReasonCode = preliminary.Status, preliminary.ReasonCode
			trace.PreliminaryCandidates = append([]careeragent.ResumeScore(nil), preliminary.TopCandidates...)
			r.writeEvent(careerAgentPreliminaryRouteEvent(preliminary))
			switch preliminary.Status {
			case careeragent.PreliminaryObviousReject:
				recordStage(stageStats, "preliminary_routing", preliminary.ReasonCode, true, true)
				summary.PreliminaryObviousRejects++
				summary.DeterministicSkipped++
				summary.Rejected++
				trace.CheapFilterResult, trace.DetailFetchStatus = "REJECT", "NOT_REQUIRED"
				reason := strings.Join(preliminary.Reasons, "; ")
				r.skipVacancy(value, vacancyURL, reason, nil)
				finish(trace, TerminalDeterministicReject, reason)
				continue
			case careeragent.PreliminaryNoResume:
				recordStage(stageStats, "preliminary_routing", preliminary.ReasonCode, true, true)
				summary.ReviewRequired++
				summary.ReviewBeforeDetail++
				reason := "no enabled resume is available"
				trace.AICallReason = "not called: " + reason
				r.skipVacancy(value, vacancyURL, reason, nil)
				trace.FinalDecision = string(VacancyReviewRequired)
				finish(trace, TerminalReviewRequired, reason)
				continue
			}
			if preliminary.Status == careeragent.PreliminaryClearRoute {
				summary.PreliminaryClearRoute++
			} else {
				summary.PreliminaryNeedsDetail++
			}
			recordStage(stageStats, "preliminary_routing", preliminary.ReasonCode, true, true)
			needsDetail := !preliminary.DetailAvailable || preliminary.Status == careeragent.PreliminaryNeedsDetail
			if needsDetail {
				summary.DetailRequested++
				recordStage(stageStats, "detail_fetch", "requested", true, false)
				enriched, detailErr := r.fetchCareerAgentDetail(ctxOrBackground(r.ctx), value)
				if detailErr != nil {
					summary.DetailFailed++
					recordStage(stageStats, "detail_fetch", "failed", false, true)
					trace.DetailFetchStatus = "FAILED"
					trace.AICallReason = "not called: vacancy detail fetch failed"
					r.skipVacancy(value, vacancyURL, "vacancy detail fetch failed: "+detailErr.Error(), nil)
					trace.FinalDecision = string(VacancyReviewRequired)
					finish(trace, TerminalDetailFetchFailed, "vacancy detail fetch failed: "+detailErr.Error())
					continue
				}
				value = enriched
				vacancyURL = value.Links["desktop"]
				trace.DetailEvidence = careerAgentEvidence(value)
				summary.DetailSucceeded++
				recordStage(stageStats, "detail_fetch", "succeeded", false, true)
				trace.DetailFetchStatus = "OK"
			} else {
				trace.DetailFetchStatus = "AVAILABLE"
				if r.careerAgentDetailCache == nil {
					r.careerAgentDetailCache = map[int]Vacancy{}
				}
				r.careerAgentDetailCache[value.ID] = value
				trace.DetailEvidence = careerAgentEvidence(value)
			}

			recordStage(stageStats, "resume_routing", "entered", true, false)
			route := r.routeResumeForVacancy(value)
			r.writeEvent(careerAgentRouteEvent(route))
			trace.ResumeCandidates = append([]careeragent.ResumeScore(nil), route.AlternativeScores...)
			trace.SelectedResume, trace.SelectedResumeTitle, trace.ResumeConfidence = route.SelectedResumeID, route.SelectedResumeTitle, route.Confidence
			trace.RoleEvidence = route.RoleEvidence
			trace.FinalRouteReasonCode = route.ReasonCode
			recordRouteReason(&summary, route.ReasonCode)
			recordRouterOutcome(&summary, route.ReasonCode, "")
			if !careerAgentRouteAllowsAI(route, r.careerAgentMode) {
				recordStage(stageStats, "resume_routing", strings.Join(route.Reasons, "; "), false, true)
				summary.ReviewRequired++
				summary.ReviewAfterDetail++
				if route.ReasonCode == careeragent.RouteReasonAmbiguous {
					summary.FinalAmbiguous++
				}
				trace.AICallReason = "not called: final resume route requires review (" + route.ReasonCode + ")"
				r.skipVacancy(value, vacancyURL, "resume routing requires review: "+strings.Join(careerAgentReasonList(route), "; "), nil)
				trace.FinalDecision = string(VacancyReviewRequired)
				finish(trace, TerminalReviewRequired, "resume routing requires review: "+strings.Join(careerAgentReasonList(route), "; "))
				continue
			}
			summary.ResumeRouted++
			summary.FinalRouted++
			recordStage(stageStats, "resume_routing", "selected", false, true)
			identifier := r.resumeIdentifierForProfile(route.SelectedResumeID)
			if identifier == "" {
				identifier = route.SelectedResumeID
			}
			if identifier != r.resumeIdentifierForValue(selectedResume) {
				var activateErr error
				selectedResume, selectedCandidate, selectedResolver, activateErr = r.activateResume(identifier)
				if activateErr != nil {
					summary.ReviewRequired++
					summary.ReviewAfterDetail++
					r.skipVacancy(value, vacancyURL, "selected resume facts could not be verified: "+activateErr.Error(), nil)
					trace.FinalDecision = string(VacancyReviewRequired)
					finish(trace, TerminalReviewRequired, "selected resume facts could not be verified: "+activateErr.Error())
					continue
				}
			}
		}

		if r.careerAgentMode == "" {
			recordStage(stageStats, "detail_fetch", "entered", true, false)
		}
		prep, prepErr := r.prepareApplication(value, selectedResume, selectedCandidate, selectedResolver, applicationCount)
		if prepErr != nil {
			if r.careerAgentMode == "" {
				recordStage(stageStats, "detail_fetch", "failed", false, true)
			}
			summary.Errors++
			trace.AICallReason = "not called: application preparation failed"
			r.skipVacancy(value, vacancyURL, prepErr.Error(), nil)
			trace.FinalDecision = string(VacancyReviewRequired)
			if r.careerAgentMode == "" || (trace.DetailFetchStatus != "OK" && trace.DetailFetchStatus != "AVAILABLE") {
				trace.DetailFetchStatus = "FAILED"
				finish(trace, TerminalDetailFetchFailed, prepErr.Error())
			} else {
				trace.AICallReason = "called or prepared after detail; preparation failed"
				finish(trace, TerminalError, prepErr.Error())
			}
			logger.Warn("Could not prepare vacancy %d: %v", value.ID, prepErr)
			continue
		}
		if r.careerAgentMode == "" {
			trace.DetailFetchStatus = "OK"
			recordStage(stageStats, "detail_fetch", "ok", false, true)
		}
		if prep.Analysis != nil {
			recordStage(stageStats, "ai_evaluation", "entered", true, false)
			summary.AIEvaluated++
			trace.AIEvaluated = true
			trace.AICallReason = "called: selected resume and provider vacancy detail available"
			score := prep.Analysis.Score
			trace.AIScore = &score
			trace.AIReasons = append([]string(nil), prep.Analysis.Reasons...)
			recordAIDecisionBreakdown(&summary, &trace, *prep.Analysis, r.minMatchScore)
			_, trace.FinalReasonCode, _ = vacancyDecisionWithReason(*prep.Analysis, r.minMatchScore)
			recordStage(stageStats, "ai_evaluation", "completed", false, true)
		}
		if r.careerAgentMode != "" && prep.Analysis == nil && trace.AICallReason == "" {
			trace.AICallReason = "not called: deterministic preparation path ended before AI"
		}
		if prep.Applicability != nil {
			preflight := applicationPreflight(value.ID, *prep.Applicability)
			r.writeEvent(preflight.event())
			r.rememberConfirmedPreflight(preflight)
		}

		switch prep.Outcome {
		case applicationprocessing.OutcomeNeedsCandidateInput:
			trace.FinalDecision = string(VacancyReviewRequired)
			trace.SelectedResume = firstNonEmpty(trace.SelectedResume, selectedResume.Hash)
			trace.SelectedResumeTitle = firstNonEmpty(trace.SelectedResumeTitle, selectedResume.Title)
			if prep.Analysis != nil && len(hardRequirementsUnknown(*prep.Analysis)) > 0 {
				trace.FinalRouteReasonCode = careeragent.RouteReasonUnknownHard
			}
			summary.ReviewRequired++
			r.writeApplicationReview(value, vacancyURL, prep, prep.Reason)
			finish(trace, TerminalReviewRequired, prep.Reason)
			continue
		case applicationprocessing.OutcomeSkipped, applicationprocessing.OutcomeAlreadyResponded, applicationprocessing.OutcomeUnavailable:
			if prep.Reason == "per-run application limit reached" && prep.Analysis != nil {
				evaluation := *prep.Analysis
				summary.Matched++
				r.writeEvent(VacancyMatchResult{Type: "vacancy_match", VacancyID: value.ID, Name: value.Name, URL: vacancyURL, Score: evaluation.Score, Recommendation: assessmentRecommendation(evaluation), RecommendationReasons: append([]string(nil), evaluation.RecommendationReasons...), Reasons: evaluation.Reasons, Missing: evaluation.Missing, HardRequirementsMissing: hardRequirementsMissing(evaluation), HardRequirements: evaluation.HardRequirements, SearchProfiles: r.vacancySearchSources[value.ID]})
				summary.ApplicationLimitSkipped++
				r.skipVacancy(value, vacancyURL, prep.Reason, &evaluation.Score)
				trace.FinalDecision = string(VacancyMatch)
				finish(trace, TerminalApplicationLimit, prep.Reason)
				continue
			}
			summary.DeterministicSkipped++
			if prep.Analysis != nil {
				lowerReason := strings.ToLower(prep.Reason)
				terminal := TerminalAIReject
				if strings.Contains(lowerReason, "already responded") {
					terminal = TerminalAlreadyResponded
					summary.PreviouslyRespondedSkipped++
				} else if strings.HasPrefix(lowerReason, "preflight:") {
					terminal = TerminalDeterministicReject
					summary.Rejected++
					trace.FinalReasonCode = preflightFinalReasonCode(prep.Reason)
				} else {
					summary.Rejected++
				}
				score := prep.Analysis.Score
				r.skipVacancyWithEvaluation(value, vacancyURL, prep.Reason, &score, hardRequirementsMissing(*prep.Analysis), prep.Analysis.HardRequirements)
				trace.FinalDecision = string(VacancyReject)
				finish(trace, terminal, prep.Reason)
			} else {
				summary.Rejected++
				r.skipVacancy(value, vacancyURL, prep.Reason, nil)
				trace.FinalDecision = string(VacancyReject)
				finish(trace, TerminalDetailNotRequired, prep.Reason)
			}
			continue
		case applicationprocessing.OutcomePrepared:
			// continue below
		default:
			summary.Errors++
			r.skipVacancy(value, vacancyURL, "unknown application preparation outcome", nil)
			finish(trace, TerminalError, "unknown application preparation outcome")
			continue
		}

		if prep.Analysis == nil || prep.Prepared == nil {
			summary.Errors++
			r.skipVacancy(value, vacancyURL, "application preparation returned no plan", nil)
			finish(trace, TerminalError, "application preparation returned no plan")
			continue
		}
		evaluation := *prep.Analysis
		summary.Matched++
		trace.FinalDecision = string(VacancyMatch)
		trace.SelectedResume = firstNonEmpty(trace.SelectedResume, selectedResume.Hash)
		trace.SelectedResumeTitle = firstNonEmpty(trace.SelectedResumeTitle, selectedResume.Title)
		trace.CoverLetterGenerated = strings.TrimSpace(prep.Prepared.CoverLetter) != ""
		if err := r.persistCareerAgentPreparation(value, selectedResume, trace, prep); err != nil {
			summary.Errors++
			logger.Warn("Could not persist Career Agent preparation for vacancy %d: %v", value.ID, err)
		}
		r.writeEvent(VacancyMatchResult{Type: "vacancy_match", VacancyID: value.ID, Name: value.Name, URL: vacancyURL, Score: evaluation.Score, Recommendation: assessmentRecommendation(evaluation), RecommendationReasons: append([]string(nil), evaluation.RecommendationReasons...), Reasons: evaluation.Reasons, Missing: evaluation.Missing, HardRequirementsMissing: hardRequirementsMissing(evaluation), HardRequirements: evaluation.HardRequirements, SearchProfiles: r.vacancySearchSources[value.ID]})
		logger.Info("MATCH — vacancy %d: %d/100", value.ID, evaluation.Score)

		submissionResult, solutions, sendErr := r.submitPreparedApplication(*prep.Prepared)
		if r.dryRun {
			if sendErr != nil {
				summary.Errors++
				r.writeApplicationError(value, vacancyURL, &selectedResume, sendErr)
				finish(trace, TerminalError, sendErr.Error())
				continue
			}
			budget.recordPreview()
			summary.WouldApply++
			r.writeApplicationPreview(value, vacancyURL, &selectedResume, prep.Prepared.CoverLetter, solutions)
			trace.WouldApply = true
			finish(trace, TerminalAIMatch, "dry-run application preview")
			continue
		}
		if budget.recordExecution(submissionResult.Execution) {
			summary.DispatchAttempts = budget.dispatchAttempts
		}
		if sendErr != nil {
			if strings.Contains(sendErr.Error(), "negotiations-limit-exceeded") {
				logger.Warn("Negotiations limit exceeded!")
				summary.Errors++
				r.writeApplicationError(value, vacancyURL, &selectedResume, sendErr)
				finish(trace, TerminalError, sendErr.Error())
				dispatchBlocked = true
				continue
			}
			summary.Errors++
			r.writeApplicationError(value, vacancyURL, &selectedResume, sendErr)
			finish(trace, TerminalError, sendErr.Error())
			continue
		}
		if submissionResult.Status == applicationsubmission.StatusSubmitted {
			successfulApplicationsInRun++
			summary.Applied++
			r.writeApplicationSuccess(value, vacancyURL, &selectedResume, prep.Prepared.CoverLetter, solutions)
			finish(trace, TerminalAIMatch, "application submitted")
			continue
		}
		summary.Errors++
		r.writeApplicationError(value, vacancyURL, &selectedResume, errors.New(submissionResult.Reason))
		finish(trace, TerminalError, submissionResult.Reason)
	}
	logger.Info("Finished processing!")
	return nil
}

func preflightFinalReasonCode(reason string) string {
	value := strings.ToLower(reason)
	switch {
	case strings.Contains(value, "already responded"):
		return "ALREADY_RESPONDED"
	case strings.Contains(value, "archived"):
		return "VACANCY_ARCHIVED"
	case strings.Contains(value, "can apply") || strings.Contains(value, "application"):
		return "APPLICATION_UNAVAILABLE"
	default:
		return "PREFLIGHT_BLOCKER"
	}
}

func routeRequirementsConfirmed(route careeragent.RouteDecision) bool {
	for _, requirement := range route.HardRequirements {
		if requirement.Status != "met" {
			return false
		}
	}
	return true
}

func careerAgentRouteAllowsAI(route careeragent.RouteDecision, mode string) bool {
	if route.Status != careeragent.RouteSelected || route.Confidence == careeragent.ConfidenceLow {
		return false
	}
	if mode == "canary" && (route.Confidence != careeragent.ConfidenceHigh || !routeRequirementsConfirmed(route)) {
		return false
	}
	return true
}

func recordRouteReason(summary *RunSummaryResult, reason string) {
	if summary == nil {
		return
	}
	switch reason {
	case careeragent.RouteReasonAmbiguous, careeragent.RouteReasonLowEvidence, careeragent.RouteReasonOutOfScope, careeragent.RouteReasonNoSuitable:
		if summary.RouteReasonCounts == nil {
			summary.RouteReasonCounts = map[string]int{}
		}
		summary.RouteReasonCounts[reason]++
	}
}

func applicationPreflight(id int, value applicationprocessing.Applicability) VacancyPreflight {
	return VacancyPreflight{VacancyID: id, Available: value.Available, Archived: value.Archived, ArchivedKnown: value.ArchivedKnown, AlreadyResponded: value.AlreadyResponded, AlreadyRespondedKnown: value.AlreadyRespondedKnown, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedValue(value.AlreadyRespondedValue), EvidenceCode: AlreadyRespondedEvidenceCode(value.AlreadyRespondedEvidenceCode)}, TestPresent: value.TestPresent, TestPresentKnown: value.TestPresentKnown, LetterRequired: value.LetterRequired, LetterRequiredKnown: value.LetterRequiredKnown, CanApply: value.CanApply, CanApplyKnown: value.CanApplyKnown, Area: value.Area, AreaKnown: value.AreaKnown, WorkSchedule: value.WorkSchedule, WorkScheduleKnown: value.WorkScheduleKnown, WorkExperience: value.WorkExperience, WorkExperienceKnown: value.WorkExperienceKnown, ResponseURL: value.ResponseURL}
}

func (r *HHAIResponder) writeApplicationReview(value Vacancy, url string, prep applicationprocessing.Result, reason string) {
	if prep.Analysis == nil {
		return
	}
	evaluation := *prep.Analysis
	r.writeEvent(VacancyReviewRequiredResult{Type: "vacancy_review_required", VacancyID: value.ID, Name: value.Name, URL: url, Score: evaluation.Score, Apply: evaluation.Apply, Recommendation: assessmentRecommendation(evaluation), RecommendationReasons: append([]string(nil), evaluation.RecommendationReasons...), Reasons: append(append([]string{}, evaluation.Reasons...), reason), Missing: evaluation.Missing, HardRequirementsUnknown: hardRequirementsUnknown(evaluation), HardRequirements: evaluation.HardRequirements, SearchProfiles: r.vacancySearchSources[value.ID]})
}

func (r *HHAIResponder) writeApplicationError(value Vacancy, url string, resume *ResumeItem, err error) {
	resumeHash := ""
	resumeTitle := ""
	if resume != nil {
		resumeHash = resume.Hash
		resumeTitle = resume.Title
	}
	r.writeEvent(ErrorResult{Type: "application_error", Context: map[string]any{"vacancy_id": value.ID, "vacancy_name": value.Name, "url": url, "resume": resumeHash, "resume_title": resumeTitle}, Error: err.Error(), Time: time.Now()})
}

func (r *HHAIResponder) writeApplicationPreview(value Vacancy, url string, resume *ResumeItem, letter string, solutions []QAPair) {
	var count *int
	if value.TotalResponsesCountKnown {
		v := value.TotalResponsesCount + 1
		count = &v
	}
	r.writeEvent(ApplyResult{Type: "application_preview", Resume: resume.Hash, ResumeTitle: resume.Title, VacancyID: value.ID, URL: url, Name: value.Name, Letter: letter, AppliedAt: time.Now(), ResponsesCount: count, TestSolutions: solutions})
}

func (r *HHAIResponder) writeApplicationSuccess(value Vacancy, url string, resume *ResumeItem, letter string, solutions []QAPair) {
	var count *int
	if value.TotalResponsesCountKnown {
		v := value.TotalResponsesCount + 1
		count = &v
	}
	r.writeEvent(ApplyResult{Type: "application", Resume: resume.Hash, ResumeTitle: resume.Title, VacancyID: value.ID, URL: url, Name: value.Name, Letter: letter, AppliedAt: time.Now(), ResponsesCount: count, TestSolutions: solutions})
}
