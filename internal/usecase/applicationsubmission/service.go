package applicationsubmission

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/usecase/hhwritepreflight"
)

var (
	ErrNotConfigured         = errors.New("application submission is not configured")
	ErrInvalidPrepared       = errors.New("prepared application is invalid")
	ErrFreshStateBlocked     = errors.New("fresh vacancy state blocked application")
	ErrFreshStateUnavailable = errors.New("fresh vacancy state is unavailable")
	ErrTestStale             = errors.New("prepared test metadata is stale")
	ErrResumeStale           = errors.New("prepared application resume identity is stale")
	ErrWriteDisabled         = errors.New("application write is disabled")
)

type Service struct {
	deps Dependencies
	opts Options
}

func NewService(deps Dependencies, opts Options) *Service {
	return &Service{deps: deps, opts: opts}
}

// Submit performs one synchronous submission attempt. Fresh vacancy state is
// read for every write-enabled or write-disabled invocation; dry-run retains
// the legacy preview short circuit and therefore performs no submission
// preflight or mutation.
func (s *Service) Submit(ctx context.Context, input Input) (Result, error) {
	result := Result{VacancyID: input.Prepared.VacancyID}
	if ctx == nil {
		return result, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		result.Status = StatusNotSent
		return result, err
	}
	if s == nil {
		return result, ErrNotConfigured
	}
	if err := validatePrepared(input.Prepared); err != nil {
		return result, err
	}
	if input.RequireCurrentResumeID && (strings.TrimSpace(input.CurrentResumeID) == "" || input.CurrentResumeID != input.Prepared.ResumeID) {
		result.Status = StatusBlockedStale
		result.Reason = ErrResumeStale.Error()
		return result, ErrResumeStale
	}
	if strings.TrimSpace(input.CurrentResumeID) != "" && input.CurrentResumeID != input.Prepared.ResumeID {
		result.Status = StatusBlockedStale
		result.Reason = ErrResumeStale.Error()
		return result, ErrResumeStale
	}

	// The legacy automatic dry-run path returns a preview before the final
	// write preflight. Keeping this gate first preserves its read count and
	// guarantees that no executor capability is reached.
	if s.opts.DryRun {
		result.Status = StatusPreview
		return result, nil
	}
	if s.deps.Vacancies == nil {
		return result, ErrNotConfigured
	}

	fresh, err := s.deps.Vacancies.ReadApplicability(ctx, input.Prepared.Vacancy)
	if err != nil {
		result.Status = StatusBlockedUnavailable
		result.Reason = "fresh vacancy applicability read failed"
		return result, fmt.Errorf("%w: %v", ErrFreshStateUnavailable, err)
	}
	result.Applicability = fresh
	preflight := hhwritepreflight.NewService(hhwritepreflight.Dependencies{
		Vacancies: applicabilityReader{value: fresh},
	}).PreflightVacancyResponse(ctx, hhwritepreflight.VacancyInput{
		VacancyID:           input.Prepared.VacancyID,
		ExpectedTestPresent: boolPointer(input.Prepared.Test != nil),
		ExpectedResponseURL: strings.TrimSpace(input.ResponseURL),
	})
	result.PreflightKnown = true
	if !preflight.Passed() {
		result.Status = statusForPreflight(preflight)
		result.Reason = firstReason(preflight.Reasons, "fresh vacancy preflight blocked application")
		if preflight.Err != nil {
			return result, fmt.Errorf("%w: %v", ErrFreshStateUnavailable, preflight.Err)
		}
		return result, fmt.Errorf("%w: %s", ErrFreshStateBlocked, result.Reason)
	}

	request, err := requestFromPrepared(input.Prepared, input.ResponseURL)
	if err != nil {
		return result, err
	}
	if input.Prepared.Test != nil {
		if s.deps.Tests == nil {
			result.Status = StatusBlockedStale
			result.Reason = "fresh vacancy test metadata reader is unavailable"
			return result, fmt.Errorf("%w: %s", ErrTestStale, result.Reason)
		}
		freshTest, readErr := s.deps.Tests.ReadTest(ctx, input.Prepared.VacancyID)
		if readErr != nil {
			result.Status = StatusBlockedStale
			result.Reason = "fresh vacancy test metadata read failed"
			return result, fmt.Errorf("%w: %v", ErrTestStale, readErr)
		}
		comparison := hhwritepreflight.CompareTestMetadata(input.Prepared.VacancyID, preparedTestMetadata(*input.Prepared.Test), snapshotMetadata(freshTest))
		if !comparison.Passed() {
			result.Status = StatusBlockedStale
			result.Reason = firstReason(comparison.Reasons, "vacancy test metadata changed before response")
			return result, fmt.Errorf("%w: %s", ErrTestStale, result.Reason)
		}
	}

	if !s.opts.WriteEnabled {
		result.Status = StatusWriteDisabled
		result.Reason = ErrWriteDisabled.Error()
		return result, ErrWriteDisabled
	}
	if s.deps.Executor == nil {
		return result, ErrNotConfigured
	}
	if err := ctx.Err(); err != nil {
		result.Status = StatusNotSent
		return result, err
	}
	execution, execErr := s.deps.Executor.SubmitApplication(ctx, request)
	result.Execution = execution
	result.Status = statusForExecution(execution.Outcome)
	if execErr != nil {
		result.Reason = execErr.Error()
	}
	return result, execErr
}

type applicabilityReader struct{ value Applicability }

func (r applicabilityReader) ReadVacancyResponseState(context.Context, int) (hhwritepreflight.VacancyResponseState, error) {
	return hhwritepreflight.VacancyResponseState{
		Archived: r.value.Archived, ArchivedKnown: r.value.ArchivedKnown,
		AlreadyResponded: r.value.AlreadyResponded, AlreadyRespondedKnown: r.value.AlreadyRespondedKnown,
		CanApply: r.value.CanApply, CanApplyKnown: r.value.CanApplyKnown,
		TestPresent: r.value.TestPresent, TestPresentKnown: r.value.TestPresentKnown,
		LetterRequired: r.value.LetterRequired, LetterRequiredKnown: r.value.LetterRequiredKnown,
		ResponseURL: r.value.ResponseURL,
	}, nil
}

func statusForPreflight(result hhwritepreflight.VacancyResult) Status {
	if result.Status == hhwritepreflight.StatusUnavailable || result.Status == hhwritepreflight.StatusRemoteNotFound {
		return StatusBlockedUnavailable
	}
	for _, reason := range result.Reasons {
		if strings.Contains(reason, "archived") || strings.Contains(reason, "already responded") || strings.Contains(reason, "does not allow") {
			return StatusBlockedUnavailable
		}
	}
	return StatusBlockedStale
}

func statusForExecution(outcome ExecutionOutcome) Status {
	switch outcome {
	case ExecutionAccepted:
		return StatusSubmitted
	case ExecutionRejected:
		return StatusRejected
	case ExecutionDeliveryUncertain:
		return StatusDeliveryUncertain
	default:
		return StatusNotSent
	}
}

func validatePrepared(value PreparedApplication) error {
	if value.VacancyID <= 0 || value.Vacancy.ID != 0 && value.Vacancy.ID != value.VacancyID || strings.TrimSpace(value.ResumeID) == "" {
		return fmt.Errorf("%w: vacancy and resume identity are required", ErrInvalidPrepared)
	}
	if value.Test == nil {
		return nil
	}
	if len(value.Test.Tasks) == 0 || len(value.Test.Answers) != len(value.Test.Tasks) {
		return fmt.Errorf("%w: test answers are incomplete", ErrInvalidPrepared)
	}
	tasks := make(map[int]PreparedTask, len(value.Test.Tasks))
	for _, task := range value.Test.Tasks {
		if task.ID <= 0 {
			return fmt.Errorf("%w: test task identity is invalid", ErrInvalidPrepared)
		}
		if _, exists := tasks[task.ID]; exists {
			return fmt.Errorf("%w: duplicate test task %d", ErrInvalidPrepared, task.ID)
		}
		tasks[task.ID] = task
	}
	seen := make(map[int]bool, len(value.Test.Answers))
	for _, answer := range value.Test.Answers {
		if answer.TaskID <= 0 || seen[answer.TaskID] {
			return fmt.Errorf("%w: duplicate or invalid test answer", ErrInvalidPrepared)
		}
		task, exists := tasks[answer.TaskID]
		if !exists {
			return fmt.Errorf("%w: answer targets unknown task %d", ErrInvalidPrepared, answer.TaskID)
		}
		seen[answer.TaskID] = true
		if answer.HasChoice {
			if strings.TrimSpace(answer.ChoiceID) == "" || !contains(task.ChoiceIDs, answer.ChoiceID) {
				return fmt.Errorf("%w: answer choice is not present in task %d", ErrInvalidPrepared, answer.TaskID)
			}
		} else if strings.TrimSpace(answer.Text) == "" {
			return fmt.Errorf("%w: open answer for task %d is empty", ErrInvalidPrepared, answer.TaskID)
		}
	}
	return nil
}

func requestFromPrepared(value PreparedApplication, responseURL string) (ApplicationRequest, error) {
	request := ApplicationRequest{VacancyID: value.VacancyID, ResumeID: value.ResumeID, Letter: value.CoverLetter, IgnorePostponed: "true", RefererURL: value.Vacancy.Links["desktop"]}
	if value.Test == nil {
		return request, nil
	}
	if strings.TrimSpace(responseURL) != "" {
		request.RefererURL = responseURL
	}
	test := &TestSubmission{
		UIDPK: value.Test.Metadata.UIDPK, GUID: value.Test.Metadata.GUID, StartTime: value.Test.Metadata.StartTime, Required: value.Test.Metadata.Required,
		Incomplete: "false", Lux: "true", WithoutTest: "no", CountryIDs: "[]", VisibleInVacancyCountry: "false",
		Answers: make([]TestAnswer, 0, len(value.Test.Answers)),
	}
	byTask := make(map[int]PreparedAnswer, len(value.Test.Answers))
	for _, answer := range value.Test.Answers {
		byTask[answer.TaskID] = answer
	}
	for _, task := range value.Test.Tasks {
		answer := byTask[task.ID]
		test.Answers = append(test.Answers, TestAnswer{TaskID: answer.TaskID, ChoiceID: answer.ChoiceID, Text: answer.Text})
	}
	request.Test = test
	return request, nil
}

func preparedTestMetadata(value PreparedTest) hhwritepreflight.TestMetadata {
	result := hhwritepreflight.TestMetadata{UIDPK: value.Metadata.UIDPK, GUID: value.Metadata.GUID, StartTime: value.Metadata.StartTime, Required: value.Metadata.Required, Tasks: make([]hhwritepreflight.TestTask, 0, len(value.Tasks))}
	for _, task := range value.Tasks {
		result.Tasks = append(result.Tasks, hhwritepreflight.TestTask{ID: task.ID, Open: task.Open, ChoiceIDs: append([]string(nil), task.ChoiceIDs...)})
	}
	return result
}

func snapshotMetadata(value TestSnapshot) hhwritepreflight.TestMetadata {
	return preparedTestMetadata(PreparedTest{Metadata: value.Metadata, Tasks: value.Tasks})
}

func boolPointer(value bool) *bool { return &value }

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstReason(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}
