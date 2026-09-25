package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
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

func TestHHAPIApplyBatchDryRunProducesPlanWithoutProviderPost(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
			t.Fatalf("dry-run issued provider POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/vacancies/42", "/vacancies/43", "/vacancies/44":
			writeHHAPIJSON(t, w, map[string]any{
				"id": strings.TrimPrefix(r.URL.Path, "/vacancies/"), "type": map[string]any{"id": "open"}, "archived": false,
				"has_test": false, "response_letter_required": false,
				"apply_alternate_url": "https://hh.example/applicant/vacancy_response?vacancyId=42",
				"negotiations_url":    "/negotiations?vacancy_id=42", "suitable_resumes_url": "/resumes/suitable?vacancy_id=42",
			})
		case "/resumes/suitable":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1, "found": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")
	if err := hhapi.NewFileTokenStore(tokenPath).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, batchMaxApplications)
	for index, vacancyID := range []int{42, 43, 44} {
		approval := validAPIApplicationApproval(now)
		approval.VacancyID = vacancyID
		approval.Nonce = "batch-nonce-" + string(rune('a'+index))
		path := filepath.Join(dir, "approval-"+string(rune('a'+index))+".json")
		raw, err := json.Marshal(approval)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenPath, "https://operator.example/callback")
	cfg.HHTransport, cfg.DryRun, cfg.HHWriteEnabled = "api", true, false
	args := []string{"apply-batch"}
	for _, path := range paths {
		args = append(args, "--approval-file", path)
	}
	var output bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), args, cfg, nil, &output, nil, HHAPICommandDeps{HTTPClient: server.Client(), Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "BATCH_ITEM status=WOULD_ATTEMPT") != batchMaxApplications || postCount != 0 {
		t.Fatalf("output=%q post_count=%d", output.String(), postCount)
	}
	for _, path := range paths {
		approval, err := loadAPIApplicationApproval(path)
		if err != nil || approval.NonceUsedAt != nil {
			t.Fatalf("dry-run changed approval %s: %+v err=%v", filepath.Base(path), approval, err)
		}
	}
}
