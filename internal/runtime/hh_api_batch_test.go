package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
	"hh-ai-responder/internal/usecase/applicationsubmission"
)

func TestParseHHAPIApplyBatchArgsRequiresOneToThreeExplicitFiles(t *testing.T) {
	if _, err := parseHHAPIApplyBatchArgs(nil); err == nil {
		t.Fatal("empty batch was accepted")
	}
	paths, err := parseHHAPIApplyBatchArgs([]string{"--approval-file", "one.json", "--approval-file=two.json", "--approval-file", "three.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"one.json", "two.json", "three.json"}) {
		t.Fatalf("paths=%v", paths)
	}
}

func TestParseHHAPIApplyBatchArgsRejectsDuplicatePathAndMoreThanThree(t *testing.T) {
	if _, err := parseHHAPIApplyBatchArgs([]string{"--approval-file", "./approval.json", "--approval-file", "approval.json"}); err == nil {
		t.Fatal("duplicate normalized approval path was accepted")
	}
	args := []string{}
	for _, path := range []string{"a", "b", "c", "d"} {
		args = append(args, "--approval-file", path)
	}
	if _, err := parseHHAPIApplyBatchArgs(args); err == nil {
		t.Fatal("more than three approval files were accepted")
	}
}

type batchExecutorFixture struct {
	calls   []string
	results map[string]controlledAPIApplicationResult
	errors  map[string]error
}

func (f *batchExecutorFixture) Execute(_ context.Context, path string, _ time.Time) (controlledAPIApplicationResult, error) {
	f.calls = append(f.calls, filepath.Base(path))
	return f.results[path], f.errors[path]
}

func batchResult(vacancyID int, transport bool, outcome APIApplicationFinalOutcome) controlledAPIApplicationResult {
	return controlledAPIApplicationResult{
		Approval:           APIApplicationApproval{VacancyID: vacancyID, ProviderResumeID: "resume-" + filepath.Base(string(rune(vacancyID)))},
		Submission:         applicationsubmission.Result{Execution: applicationsubmission.ExecutionResult{ApplicationClass: hhwrite.ApplicationResultSuccess}},
		TransportAttempted: transport,
		FinalOutcome:       outcome,
	}
}

func TestBatchStopsAfterUncertainTransport(t *testing.T) {
	fixture := &batchExecutorFixture{results: map[string]controlledAPIApplicationResult{}, errors: map[string]error{}}
	fixture.results["one"] = batchResult(1, true, APIApplicationFinalPostSuccessReconciled)
	fixture.results["two"] = batchResult(2, true, APIApplicationFinalPostSuccessUnconfirmed)
	fixture.results["three"] = batchResult(3, false, "")
	run, err := executeControlledApplicationBatch(context.Background(), []string{"one", "two", "three"}, time.Now(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != BatchRunStoppedUncertain || len(run.Items) != 2 {
		t.Fatalf("run=%+v", run)
	}
	if !reflect.DeepEqual(fixture.calls, []string{"one", "two"}) {
		t.Fatalf("provider execution order=%v", fixture.calls)
	}
}

func TestBatchContinuesAfterPreSendBlock(t *testing.T) {
	fixture := &batchExecutorFixture{results: map[string]controlledAPIApplicationResult{}, errors: map[string]error{}}
	fixture.results["one"] = batchResult(1, true, APIApplicationFinalPostSuccessReconciled)
	fixture.results["two"] = batchResult(2, false, "")
	fixture.errors["two"] = errors.New("fresh preflight blocked")
	fixture.results["three"] = batchResult(3, true, APIApplicationFinalPostSuccessReconciled)
	run, err := executeControlledApplicationBatch(context.Background(), []string{"one", "two", "three"}, time.Now(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != BatchRunCompleted || len(run.Items) != 3 {
		t.Fatalf("run=%+v", run)
	}
	if run.Items[1].Status != BatchItemBlockedPreSend || run.Items[2].Status != BatchItemAppliedReconciled {
		t.Fatalf("items=%+v", run.Items)
	}
	if !reflect.DeepEqual(fixture.calls, []string{"one", "two", "three"}) {
		t.Fatalf("execution order=%v", fixture.calls)
	}
}
