package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/careeragent"
)

func TestDashboardCareerReadOnlyAPIs(t *testing.T) {
	server := dashboardTestServer(t)
	workflow := jsonstorage.NewCareerWorkflowRepository(t.TempDir() + "/career-workflow.json")
	server.CareerWorkflow = workflow
	_, _ = server.Vacancies.Create(Vacancy{ID: 42, Title: "Python developer", Company: Company{Name: "Fixture"}, DataCompleteness: DataCompletenessFull})

	now := time.Now().UTC()
	run := careeragent.NewAgentRun("run-dashboard-1", careeragent.AgentRunStageCareerAgent, now)
	if err := workflow.StartRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := run.Finish(careeragent.AgentRunStatusFailed, "failed", now.Add(time.Minute), errors.New("authorization=private")); err != nil {
		t.Fatal(err)
	}
	if err := workflow.FinishRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	queue := dashboardRequest(server, "GET", "/api/career/review-queue", "")
	if queue.Code != 200 || !strings.Contains(queue.Body.String(), `"vacancy_id":42`) {
		t.Fatalf("review queue response=%d body=%s", queue.Code, queue.Body.String())
	}
	detail := dashboardRequest(server, "GET", "/api/career/vacancies/42", "")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"id":42`) {
		t.Fatalf("vacancy workspace response=%d body=%s", detail.Code, detail.Body.String())
	}
	runs := dashboardRequest(server, "GET", "/api/career/runs", "")
	if runs.Code != 200 || strings.Contains(runs.Body.String(), "authorization=private") || !strings.Contains(runs.Body.String(), "error redacted") {
		t.Fatalf("run timeline response=%d body=%s", runs.Code, runs.Body.String())
	}
	metrics := dashboardRequest(server, "GET", "/api/career/metrics", "")
	if metrics.Code != 200 || !strings.Contains(metrics.Body.String(), `"new"`) || !strings.Contains(metrics.Body.String(), `"run_status":"failed"`) {
		t.Fatalf("career metrics response=%d body=%s", metrics.Code, metrics.Body.String())
	}
	post := dashboardRequest(server, "POST", "/api/career/review-queue", "{}")
	if post.Code != 405 || post.Header().Get("Allow") != "GET" {
		t.Fatalf("career endpoint accepted unsupported method: status=%d allow=%q", post.Code, post.Header().Get("Allow"))
	}
}

func TestDashboardCareerAPIsReportMissingWorkflowData(t *testing.T) {
	server := dashboardTestServer(t)
	server.CareerWorkflow = nil
	response := dashboardRequest(server, "GET", "/api/career/review-queue", "")
	if response.Code != 503 || !strings.Contains(response.Body.String(), "CAREER_WORKFLOW_UNAVAILABLE") {
		t.Fatalf("missing workflow data response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardCareerRouteClassificationIsReadOnly(t *testing.T) {
	for _, path := range [][]string{{"career", "review-queue"}, {"career", "runs"}, {"career", "vacancies", "42"}} {
		if got := dashboardRouteMethod(path); got != "GET" || !dashboardReadOnlyPath(path) {
			t.Fatalf("path=%v method=%q read_only=%t", path, got, dashboardReadOnlyPath(path))
		}
	}
}
