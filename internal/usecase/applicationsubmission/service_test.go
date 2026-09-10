package applicationsubmission

import (
	"context"
	"errors"
	"testing"

	"hh-ai-responder/internal/vacancy"
)

type fakeVacancyReader struct {
	state Applicability
	err   error
	calls int
}

func (f *fakeVacancyReader) ReadApplicability(context.Context, vacancy.Vacancy) (Applicability, error) {
	f.calls++
	return f.state, f.err
}

type fakeTestReader struct {
	snapshot TestSnapshot
	err      error
	calls    int
}

func (f *fakeTestReader) ReadTest(context.Context, int) (TestSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

type fakeExecutor struct {
	result ExecutionResult
	err    error
	calls  int
	last   ApplicationRequest
}

func (f *fakeExecutor) SubmitApplication(_ context.Context, request ApplicationRequest) (ExecutionResult, error) {
	f.calls++
	f.last = request
	return f.result, f.err
}

func validApplicability(test bool) Applicability {
	return Applicability{
		ArchivedKnown: true, AlreadyRespondedKnown: true,
		TestPresent: test, TestPresentKnown: true,
		LetterRequiredKnown: true, CanApply: true, CanApplyKnown: true,
	}
}

func preparedApplication(withTest bool) PreparedApplication {
	value := PreparedApplication{
		VacancyID: 7, Vacancy: vacancy.Vacancy{ID: 7, Links: map[string]string{"desktop": "https://hh.example/vacancy/7"}},
		ResumeID: "resume-1", ResumeTitle: "Backend", CoverLetter: "exact prepared letter",
	}
	if withTest {
		value.Test = &PreparedTest{
			Metadata: TestMetadata{UIDPK: "uid", GUID: "guid", StartTime: "start", Required: "true"},
			Tasks:    []PreparedTask{{ID: 11, Open: "false", ChoiceIDs: []string{"101"}}},
			Answers:  []PreparedAnswer{{TaskID: 11, ChoiceID: "101", HasChoice: true}},
		}
	}
	return value
}

func matchingTestSnapshot() TestSnapshot {
	return TestSnapshot{
		Metadata: TestMetadata{UIDPK: "uid", GUID: "guid", StartTime: "start", Required: "true"},
		Tasks:    []PreparedTask{{ID: 11, Open: "false", ChoiceIDs: []string{"101"}}},
	}
}

func newSubmissionFixture(withTest bool, outcome ExecutionOutcome) (*Service, *fakeVacancyReader, *fakeTestReader, *fakeExecutor) {
	vacancies := &fakeVacancyReader{state: validApplicability(withTest)}
	tests := &fakeTestReader{snapshot: matchingTestSnapshot()}
	executor := &fakeExecutor{result: ExecutionResult{Outcome: outcome, ProviderStatus: 200}}
	service := NewService(Dependencies{Vacancies: vacancies, Tests: tests, Executor: executor}, Options{WriteEnabled: true})
	return service, vacancies, tests, executor
}

func TestSubmitFreshPassCallsExecutorExactlyOnce(t *testing.T) {
	service, vacancies, tests, executor := newSubmissionFixture(false, ExecutionAccepted)
	result, err := service.Submit(context.Background(), Input{Prepared: preparedApplication(false)})
	if err != nil || result.Status != StatusSubmitted || vacancies.calls != 1 || tests.calls != 0 || executor.calls != 1 {
		t.Fatalf("fresh submission = %+v err=%v reads=%d/%d executor=%d", result, err, vacancies.calls, tests.calls, executor.calls)
	}
	if executor.last.ResumeID != "resume-1" || executor.last.Letter != "exact prepared letter" || executor.last.Test != nil {
		t.Fatalf("prepared identity/content changed: %+v", executor.last)
	}
}

func TestSubmitAlreadyRespondedAndUnavailableDoNotExecute(t *testing.T) {
	tests := []struct {
		name       string
		state      Applicability
		expect     string
		wantStatus Status
	}{
		{name: "already responded", state: func() Applicability { value := validApplicability(false); value.AlreadyResponded = true; return value }(), expect: "already responded", wantStatus: StatusBlockedUnavailable},
		{name: "archived", state: func() Applicability { value := validApplicability(false); value.Archived = true; return value }(), expect: "archived", wantStatus: StatusBlockedUnavailable},
		{name: "cannot apply", state: func() Applicability { value := validApplicability(false); value.CanApply = false; return value }(), expect: "does not allow", wantStatus: StatusBlockedUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vacancies := &fakeVacancyReader{state: test.state}
			executor := &fakeExecutor{result: ExecutionResult{Outcome: ExecutionAccepted}}
			service := NewService(Dependencies{Vacancies: vacancies, Executor: executor}, Options{WriteEnabled: true})
			result, err := service.Submit(context.Background(), Input{Prepared: preparedApplication(false)})
			if err == nil || result.Status != test.wantStatus || executor.calls != 0 || vacancies.calls != 1 || !containsString(result.Reason, test.expect) {
				t.Fatalf("blocked submission = %+v err=%v reads=%d executor=%d", result, err, vacancies.calls, executor.calls)
			}
		})
	}
}

func TestSubmitTestMetadataChangeBlocksWithoutExecution(t *testing.T) {
	service, vacancies, tests, executor := newSubmissionFixture(true, ExecutionAccepted)
	tests.snapshot.Tasks[0].ChoiceIDs = []string{"999"}
	result, err := service.Submit(context.Background(), Input{Prepared: preparedApplication(true)})
	if err == nil || !errors.Is(err, ErrTestStale) || result.Status != StatusBlockedStale || vacancies.calls != 1 || tests.calls != 1 || executor.calls != 0 {
		t.Fatalf("stale test was submitted: %+v err=%v reads=%d/%d executor=%d", result, err, vacancies.calls, tests.calls, executor.calls)
	}
}

func TestSubmitResumeIdentityMismatchBlocksWithoutExecution(t *testing.T) {
	service, vacancies, _, executor := newSubmissionFixture(false, ExecutionAccepted)
	result, err := service.Submit(context.Background(), Input{
		Prepared: preparedApplication(false), CurrentResumeID: "resume-2", RequireCurrentResumeID: true,
	})
	if err == nil || !errors.Is(err, ErrResumeStale) || result.Status != StatusBlockedStale || vacancies.calls != 0 || executor.calls != 0 {
		t.Fatalf("resume identity mismatch reached submission: %+v err=%v reads=%d executor=%d", result, err, vacancies.calls, executor.calls)
	}
}

func TestSubmitTestApplicabilityChangesBlockWithoutReadingOrSubmittingTest(t *testing.T) {
	for _, test := range []struct {
		name         string
		preparedTest bool
		freshTest    bool
		wantReason   string
	}{
		{name: "test newly required", preparedTest: false, freshTest: true, wantReason: "applicability changed"},
		{name: "test disappeared", preparedTest: true, freshTest: false, wantReason: "applicability changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			vacancies := &fakeVacancyReader{state: validApplicability(test.freshTest)}
			testReader := &fakeTestReader{snapshot: matchingTestSnapshot()}
			executor := &fakeExecutor{result: ExecutionResult{Outcome: ExecutionAccepted}}
			service := NewService(Dependencies{Vacancies: vacancies, Tests: testReader, Executor: executor}, Options{WriteEnabled: true})
			result, err := service.Submit(context.Background(), Input{Prepared: preparedApplication(test.preparedTest)})
			if err == nil || result.Status != StatusBlockedStale || executor.calls != 0 || !containsString(result.Reason, test.wantReason) {
				t.Fatalf("test applicability change was not blocked: %+v err=%v executor=%d", result, err, executor.calls)
			}
		})
	}
}

func TestSubmitDryRunAndWriteDisabledNeverExecute(t *testing.T) {
	for _, test := range []struct {
		name   string
		opts   Options
		status Status
		err    bool
	}{
		{name: "dry run", opts: Options{WriteEnabled: true, DryRun: true}, status: StatusPreview},
		{name: "write disabled", opts: Options{WriteEnabled: false}, status: StatusWriteDisabled, err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			vacancies := &fakeVacancyReader{state: validApplicability(false)}
			executor := &fakeExecutor{result: ExecutionResult{Outcome: ExecutionAccepted}}
			service := NewService(Dependencies{Vacancies: vacancies, Executor: executor}, test.opts)
			result, err := service.Submit(context.Background(), Input{Prepared: preparedApplication(false)})
			if result.Status != test.status || (err != nil) != test.err || executor.calls != 0 {
				t.Fatalf("unsafe policy behavior: %+v err=%v reads=%d executor=%d", result, err, vacancies.calls, executor.calls)
			}
		})
	}
}

func TestSubmitOutcomeMappingMakesOneExecutorCallAndDoesNotRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		outcome    ExecutionOutcome
		wantStatus Status
	}{
		{name: "accepted", outcome: ExecutionAccepted, wantStatus: StatusSubmitted},
		{name: "rejected", outcome: ExecutionRejected, wantStatus: StatusRejected},
		{name: "ambiguous", outcome: ExecutionDeliveryUncertain, wantStatus: StatusDeliveryUncertain},
		{name: "429 rejected", outcome: ExecutionRejected, wantStatus: StatusRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, _, executor := newSubmissionFixture(false, test.outcome)
			result, _ := service.Submit(context.Background(), Input{Prepared: preparedApplication(false)})
			if result.Status != test.wantStatus || executor.calls != 1 {
				t.Fatalf("outcome = %+v calls=%d, want %s and one call", result, executor.calls, test.wantStatus)
			}
		})
	}
}

func TestSubmitCancellationBeforeExecutionDoesNotCallExecutor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service, vacancies, _, executor := newSubmissionFixture(false, ExecutionAccepted)
	result, err := service.Submit(ctx, Input{Prepared: preparedApplication(false)})
	if !errors.Is(err, context.Canceled) || result.Status != StatusNotSent || vacancies.calls != 0 || executor.calls != 0 {
		t.Fatalf("cancellation reached submission: %+v err=%v reads=%d executor=%d", result, err, vacancies.calls, executor.calls)
	}
}

func containsString(value, target string) bool {
	for i := 0; i+len(target) <= len(value); i++ {
		if value[i:i+len(target)] == target {
			return true
		}
	}
	return false
}
