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

func TestDashboardManualDailyRunUsesConfiguredService(t *testing.T) {
	server := dashboardTestServer(t)
	workflow := newDailyWorkflowStoreFixture()
	var calls int
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{
		Workflow: workflow,
		Vacancy: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			calls++
			return careeragent.DailyStageResult{Summary: careeragent.DailyStageSummary{Vacancy: careeragent.DailyVacancySummary{Scanned: 1}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Daily = service
	response := dashboardRequest(server, "POST", "/api/career/run", "{}")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"result":"SUCCESS"`) {
		t.Fatalf("manual run response=%d body=%s", response.Code, response.Body.String())
	}
	if calls != 1 || workflow.starts != 1 || workflow.finishes != 1 {
		t.Fatalf("manual run calls=%d starts=%d finishes=%d", calls, workflow.starts, workflow.finishes)
	}
}

func TestDashboardCareerAttentionAPIIsDerivedAndStable(t *testing.T) {
	server := dashboardTestServer(t)
	workflow := newDailyWorkflowStoreFixture()
	server.CareerWorkflow = workflow
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	run := careeragent.NewAgentRun(careeragent.DailyRunID(now), careeragent.AgentRunStageCareerAgent, now)
	run.RunType = "daily_career_agent"
	if err := workflow.StartRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := workflow.UpsertRunItem(context.Background(), careeragent.AgentRunItem{ID: "review-1", RunID: run.ID, VacancyID: 42, Stage: careeragent.AgentRunStageReview, Status: careeragent.AgentRunItemStatusReviewRequired, DecisionCode: "unknown_requirement", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := run.Finish(careeragent.AgentRunStatusPartial, "partial", now.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	if err := workflow.FinishRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	response := dashboardRequest(server, "GET", "/api/career/attention", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"vacancy_id":42`) || !strings.Contains(response.Body.String(), `"review_required"`) {
		t.Fatalf("attention response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardCareerRouteClassificationIsReadOnly(t *testing.T) {
	for _, path := range [][]string{{"career", "review-queue"}, {"career", "runs"}, {"career", "vacancies", "42"}} {
		if got := dashboardRouteMethod(path); got != "GET" || !dashboardReadOnlyPath(path) {
			t.Fatalf("path=%v method=%q read_only=%t", path, got, dashboardReadOnlyPath(path))
		}
	}
}
