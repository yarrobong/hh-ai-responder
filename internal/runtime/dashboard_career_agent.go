package runtime

import (
	"net/http"
	"strconv"

	"hh-ai-responder/internal/careeragent"
)

func (s *DashboardServer) readCareerAPI(w http.ResponseWriter, r *http.Request, p []string) {
	snapshot, err := s.loadDashboardSnapshot()
	if err != nil {
		dashboardError(w, http.StatusInternalServerError, "Career Agent workspace unavailable")
		return
	}
	if snapshot.careerError != "" {
		dashboardJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "Career Agent workflow data unavailable",
			"code":    "CAREER_WORKFLOW_UNAVAILABLE",
			"details": snapshot.careerError,
		})
		return
	}
	switch {
	case len(p) == 2 && p[1] == "review-queue":
		dashboardJSON(w, http.StatusOK, map[string]any{"items": snapshot.careerQueue, "count": len(snapshot.careerQueue)})
	case len(p) == 2 && p[1] == "runs":
		dashboardJSON(w, http.StatusOK, map[string]any{"runs": snapshot.careerRuns, "count": len(snapshot.careerRuns)})
	case len(p) == 2 && p[1] == "metrics":
		dashboardJSON(w, http.StatusOK, careerAgentDashboardMetrics(snapshot.careerQueue, snapshot.careerRuns))
	case len(p) == 3 && p[1] == "vacancies":
		id, parseErr := strconv.Atoi(p[2])
		if parseErr != nil || id <= 0 {
			dashboardError(w, http.StatusNotFound, "Career vacancy not found")
			return
		}
		for _, item := range snapshot.careerQueue {
			if item.VacancyID == id {
				dashboardJSON(w, http.StatusOK, item.Snapshot)
				return
			}
		}
		dashboardError(w, http.StatusNotFound, "Career vacancy not found")
	default:
		dashboardError(w, http.StatusNotFound, "Career Agent endpoint not found")
	}
}

func careerAgentDashboardMetrics(queue []careeragent.ReviewQueueItem, runs []careeragent.AgentRun) map[string]any {
	counts := map[string]int{}
	for _, item := range queue {
		counts[string(item.PipelineState)]++
	}
	runStatus := ""
	if len(runs) > 0 {
		runStatus = string(runs[0].Status)
	}
	return map[string]any{
		"new":               counts[string(careeragent.ReviewStateNew)],
		"analyzing":         counts[string(careeragent.ReviewStateAIReview)],
		"matched":           counts[string(careeragent.ReviewStateMatched)],
		"review_required":   counts[string(careeragent.ReviewStateClarificationNeeded)] + counts[string(careeragent.ReviewStateAIReview)],
		"ready":             counts[string(careeragent.ReviewStateReady)],
		"applied":           counts[string(careeragent.ReviewStateApplied)],
		"interview":         counts[string(careeragent.ReviewStateInterview)],
		"run_status":        runStatus,
		"review_queue_size": len(queue),
	}
}

func applyCareerAgentMetrics(metrics *DashboardMetrics, queue []careeragent.ReviewQueueItem, runs []careeragent.AgentRun) {
	if metrics == nil {
		return
	}
	counts := map[string]int{}
	for _, item := range queue {
		counts[string(item.PipelineState)]++
	}
	metrics.CareerNew = counts[string(careeragent.ReviewStateNew)]
	metrics.CareerAnalyzing = counts[string(careeragent.ReviewStateAIReview)]
	metrics.CareerMatched = counts[string(careeragent.ReviewStateMatched)]
	metrics.CareerReviewRequired = counts[string(careeragent.ReviewStateClarificationNeeded)] + metrics.CareerAnalyzing
	metrics.CareerReady = counts[string(careeragent.ReviewStateReady)]
	metrics.CareerApplied = counts[string(careeragent.ReviewStateApplied)]
	metrics.CareerInterview = counts[string(careeragent.ReviewStateInterview)]
	if len(runs) > 0 {
		metrics.CareerRunStatus = string(runs[0].Status)
	}
}
