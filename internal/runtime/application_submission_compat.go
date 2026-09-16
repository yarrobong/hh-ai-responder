package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	hhwriteport "hh-ai-responder/internal/ports/hhwrite"
	automaticapplicationattempt "hh-ai-responder/internal/usecase/applicationattempt"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	applicationsubmission "hh-ai-responder/internal/usecase/applicationsubmission"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
	"hh-ai-responder/internal/usecase/testanswer"
	"hh-ai-responder/internal/vacancy"
)

// applyPreparedApplication remains the legacy root compatibility shape. All
// submission choreography is delegated to applicationsubmission; this method
// only converts the typed result back to the historical map and QAPair values.
func (r *HHAIResponder) applyPreparedApplication(prepared applicationprocessing.PreparedApplication) (map[string]any, []QAPair, error) {
	result, solutions, err := r.submitPreparedApplication(prepared)
	if err != nil {
		reason := result.Reason
		if strings.TrimSpace(reason) == "" {
			reason = err.Error()
		}
		return map[string]any{"error": reason}, solutions, err
	}
	switch result.Status {
	case applicationsubmission.StatusPreview:
		return map[string]any{"dry_run": true}, solutions, nil
	case applicationsubmission.StatusSubmitted:
		return map[string]any{"success": "true", "status": result.Execution.ProviderStatus, "metadata": result.Execution.Metadata}, solutions, nil
	default:
		reason := result.Reason
		if strings.TrimSpace(reason) == "" {
			reason = string(result.Status)
		}
		return map[string]any{"error": reason}, solutions, errors.New(reason)
	}
}

func (r *HHAIResponder) submitPreparedApplication(prepared applicationprocessing.PreparedApplication) (applicationsubmission.Result, []QAPair, error) {
	if r == nil {
		return applicationsubmission.Result{}, nil, errors.New("HH responder is not configured")
	}
	input := applicationsubmission.Input{
		Prepared:               applicationSubmissionPrepared(prepared),
		CurrentResumeID:        r.resumeHash,
		RequireCurrentResumeID: true,
		ResponseURL:            r.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", prepared.VacancyID)),
	}
	result, err := r.applicationSubmissionService().Submit(ctxOrBackground(r.ctx), input)
	solutions := preparedTestSolutions(prepared)
	if err == nil && result.Status == applicationsubmission.StatusSubmitted {
		if projectionErr := r.persistApplicationProjection(ctxOrBackground(r.ctx), prepared, result.Execution); projectionErr != nil {
			return result, solutions, fmt.Errorf("application accepted by HH but local application projection failed: %w", projectionErr)
		}
	}
	return result, solutions, err
}

// persistApplicationProjection records the local career aggregate after an
// accepted HH response. In PostgreSQL mode this is the canonical application
// state; the event writer remains an operational/audit projection only.
func (r *HHAIResponder) persistApplicationProjection(ctx context.Context, prepared applicationprocessing.PreparedApplication, execution applicationsubmission.ExecutionResult) error {
	if r == nil || r.careerRepositories.Applications == nil {
		return nil
	}
	now := time.Now().UTC()
	metadata := map[string]string{"resume_id": prepared.ResumeID}
	for key, value := range execution.Metadata {
		metadata[key] = value
	}
	if execution.ProviderID != "" {
		metadata["provider_application_id"] = execution.ProviderID
	}
	applications, err := r.careerRepositories.Applications.List(ctx)
	if err != nil {
		return err
	}
	for _, existing := range applications {
		if existing.VacancyID == prepared.VacancyID && existing.HHMetadata["resume_id"] == prepared.ResumeID && existing.Status == ApplicationApplied {
			return nil
		}
	}
	application, err := r.careerRepositories.Applications.Create(ctx, JobApplication{
		VacancyID: prepared.VacancyID, CompanyName: prepared.Vacancy.Company.Name,
		VacancyTitle: prepared.Vacancy.Title, VacancyURL: prepared.Vacancy.Links["desktop"],
		Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now,
		MatchResult: prepared.Vacancy.MatchResult, HHMetadata: metadata,
	})
	if err != nil {
		return err
	}
	return r.careerRepositories.Applications.AppendEvent(ctx, application.ID, now, ApplicationEventApplied, "application accepted by HH")
}

func (r *HHAIResponder) applicationSubmissionService() *applicationsubmission.Service {
	reader := rootApplicationSubmissionReader{responder: r}
	reconciler := r.applicationReconciliationService()
	executor := automaticapplicationattempt.NewExecutorWithBlockedHandlerAndNotifications(r.applicationAttempts, rootApplicationExecutor{responder: r}, nil, func(ctx context.Context, vacancyID int) error {
		if reconciler == nil {
			return nil
		}
		_, err := reconciler.ReconcileVacancy(ctx, vacancyID)
		return err
	}, r.reliabilityNotifications)
	return applicationsubmission.NewService(applicationsubmission.Dependencies{
		Vacancies: reader,
		Tests:     reader,
		Executor:  executor,
	}, applicationsubmission.Options{WriteEnabled: r.hhWriteEnabled, DryRun: r.dryRun})
}

func (r *HHAIResponder) applicationReconciliationService() *applicationreconciliation.Service {
	if r == nil || r.applicationAttempts == nil {
		return nil
	}
	store, ok := r.applicationAttempts.(applicationreconciliation.AttemptStore)
	if !ok {
		return nil
	}
	return applicationreconciliation.NewService(applicationreconciliation.Dependencies{
		Attempts:      store,
		Reader:        rootApplicationEvidenceReader{responder: r},
		Notifications: r.reliabilityNotifications,
	})
}

type rootApplicationSubmissionReader struct{ responder *HHAIResponder }

func (r rootApplicationSubmissionReader) ReadApplicability(ctx context.Context, value vacancy.Vacancy) (applicationsubmission.Applicability, error) {
	if r.responder == nil {
		return applicationsubmission.Applicability{}, errors.New("HH responder is not configured")
	}
	state, err := (rootApplicationReader{responder: r.responder}).ReadApplicability(ctx, value)
	if err != nil {
		return applicationsubmission.Applicability{}, err
	}
	return applicationsubmission.Applicability{
		Archived: state.Archived, ArchivedKnown: state.ArchivedKnown,
		AlreadyResponded: state.AlreadyResponded, AlreadyRespondedKnown: state.AlreadyRespondedKnown,
		TestPresent: state.TestPresent, TestPresentKnown: state.TestPresentKnown,
		LetterRequired: state.LetterRequired, LetterRequiredKnown: state.LetterRequiredKnown,
		CanApply: state.CanApply, CanApplyKnown: state.CanApplyKnown, ResponseURL: state.ResponseURL,
	}, nil
}

func (r rootApplicationSubmissionReader) ReadTest(ctx context.Context, id int) (applicationsubmission.TestSnapshot, error) {
	if r.responder == nil {
		return applicationsubmission.TestSnapshot{}, errors.New("HH responder is not configured")
	}
	snapshot, err := (rootApplicationReader{responder: r.responder}).ReadTest(ctx, id)
	if err != nil {
		return applicationsubmission.TestSnapshot{}, err
	}
	return applicationsubmission.TestSnapshot{
		Metadata: applicationsubmission.TestMetadata{UIDPK: snapshot.Metadata.UIDPK, GUID: snapshot.Metadata.GUID, StartTime: snapshot.Metadata.StartTime, Required: snapshot.Metadata.Required},
		Tasks:    detachedPreparedTasks(snapshot.Tasks),
	}, nil
}

type rootApplicationExecutor struct{ responder *HHAIResponder }

func (e rootApplicationExecutor) SubmitApplication(ctx context.Context, request applicationsubmission.ApplicationRequest) (applicationsubmission.ExecutionResult, error) {
	if e.responder == nil {
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, errors.New("HH responder is not configured")
	}
	e.responder.careerAgentWriteCount++
	service, err := e.responder.newLegacyWriteService()
	if err != nil {
		return applicationsubmission.ExecutionResult{Outcome: applicationsubmission.ExecutionNotSent}, err
	}
	result, submitErr := service.SubmitVacancyResponse(ctx, hhwritegateway.VacancyResponseRequest{
		VacancyID: request.VacancyID, ResumeHash: request.ResumeID, Letter: request.Letter,
		RefererURL: request.RefererURL, IgnorePostponed: request.IgnorePostponed,
		Test: applicationTestSubmission(request.Test),
	})
	execution := applicationsubmission.ExecutionResult{
		ProviderID: result.ProviderID, ProviderStatus: result.ProviderStatus, Metadata: result.Metadata,
		TransportTried: result.TransportAttempted,
	}
	switch result.Outcome {
	case hhwritegateway.OutcomeAccepted:
		execution.Outcome = applicationsubmission.ExecutionAccepted
	case hhwritegateway.OutcomeRejected:
		execution.Outcome = applicationsubmission.ExecutionRejected
	case hhwritegateway.OutcomeDeliveryUncertain, hhwritegateway.OutcomePersistenceUncertain:
		execution.Outcome = applicationsubmission.ExecutionDeliveryUncertain
	default:
		execution.Outcome = applicationsubmission.ExecutionNotSent
	}
	return execution, submitErr
}

func applicationTestSubmission(value *applicationsubmission.TestSubmission) *hhwriteport.VacancyTestSubmission {
	if value == nil {
		return nil
	}
	result := &hhwriteport.VacancyTestSubmission{
		UIDPK: value.UIDPK, GUID: value.GUID, StartTime: value.StartTime, Required: value.Required,
		Incomplete: value.Incomplete, Lux: value.Lux, WithoutTest: value.WithoutTest,
		CountryIDs: value.CountryIDs, VisibleInVacancyCountry: value.VisibleInVacancyCountry,
		Answers: make([]hhwriteport.VacancyTestAnswer, 0, len(value.Answers)),
	}
	for _, answer := range value.Answers {
		result.Answers = append(result.Answers, hhwriteport.VacancyTestAnswer{TaskID: answer.TaskID, ChoiceID: answer.ChoiceID, Text: answer.Text})
	}
	return result
}

func applicationSubmissionPrepared(value applicationprocessing.PreparedApplication) applicationsubmission.PreparedApplication {
	result := applicationsubmission.PreparedApplication{
		VacancyID: value.VacancyID, Vacancy: value.Vacancy, ResumeID: value.ResumeID, ResumeTitle: value.ResumeTitle, CoverLetter: value.CoverLetter,
	}
	if value.Test == nil {
		return result
	}
	result.Test = &applicationsubmission.PreparedTest{
		Metadata: applicationsubmission.TestMetadata{UIDPK: value.Test.Metadata.UIDPK, GUID: value.Test.Metadata.GUID, StartTime: value.Test.Metadata.StartTime, Required: value.Test.Metadata.Required},
		Tasks:    detachedPreparedTasks(value.Test.Tasks),
		Answers:  make([]applicationsubmission.PreparedAnswer, 0, len(value.Test.Answers)),
	}
	for _, answer := range value.Test.Answers {
		result.Test.Answers = append(result.Test.Answers, applicationsubmission.PreparedAnswer{TaskID: answer.TaskID, ChoiceID: strconv.Itoa(answer.SolutionID), Text: answer.TextSolution, HasChoice: answer.HasChoice})
	}
	return result
}

func detachedPreparedTasks(values []testanswer.Task) []applicationsubmission.PreparedTask {
	result := make([]applicationsubmission.PreparedTask, 0, len(values))
	for _, task := range values {
		item := applicationsubmission.PreparedTask{ID: task.ID, Open: task.Open, ChoiceIDs: make([]string, 0, len(task.CandidateSolutions))}
		for _, option := range task.CandidateSolutions {
			item.ChoiceIDs = append(item.ChoiceIDs, option.ID)
		}
		result = append(result, item)
	}
	return result
}

func preparedTestSolutions(value applicationprocessing.PreparedApplication) []QAPair {
	if value.Test == nil {
		return nil
	}
	answers := make(map[int]SolutionFields, len(value.Test.Answers))
	for _, answer := range value.Test.Answers {
		answers[answer.TaskID] = SolutionFields{SolutionID: answer.SolutionID, TextSolution: answer.TextSolution, HasChoice: answer.HasChoice}
	}
	return buildReadableTestSolutionsFromPrepared(value.Test.Tasks, answers)
}

func buildReadableTestSolutionsFromPrepared(tasks []testanswer.Task, answers map[int]SolutionFields) []QAPair {
	legacy := make([]Task, len(tasks))
	for i, task := range tasks {
		legacy[i] = Task{ID: task.ID, Description: task.Description, CandidateSolutions: make([]Solution, 0, len(task.CandidateSolutions))}
		for _, option := range task.CandidateSolutions {
			legacy[i].CandidateSolutions = append(legacy[i].CandidateSolutions, Solution{ID: option.ID, Text: option.Text, Title: option.Title, Value: option.Value})
		}
	}
	return buildReadableTestSolutions(legacy, answers)
}
