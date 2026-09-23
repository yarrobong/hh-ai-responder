package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
)

func dailyStageResultFromCareerAgentReport(report CareerAgentRunReport, runID string, now time.Time) (careeragent.DailyStageResult, error) {
	if runID == "" {
		return careeragent.DailyStageResult{}, errors.New("daily vacancy stage requires a run id")
	}
	result := careeragent.DailyStageResult{Items: []careeragent.AgentRunItem{}}
	result.Summary.Vacancy = careeragent.DailyVacancySummary{
		Scanned: report.Summary.VacanciesProcessed, Found: report.Summary.VacanciesFetchedRaw,
		New: report.Summary.VacanciesAfterDedup, Rejected: report.Summary.Rejected,
		Matched: report.Summary.Matched, ReviewRequired: report.Summary.ReviewRequired,
		AIReviewed: report.Summary.AIEvaluated, Prepared: report.Summary.WouldApply,
	}
	result.Summary.AI = careeragent.AIBudgetSummary{Requested: report.Summary.AIEvaluated, Succeeded: report.Summary.AIEvaluated - report.Summary.Errors, Failed: report.Summary.Errors}
	if result.Summary.AI.Succeeded < 0 {
		result.Summary.AI.Succeeded = 0
	}
	if report.Summary.Errors > 0 {
		result.Summary.Failures = append(result.Summary.Failures, fmt.Sprintf("vacancy stage reported %d item errors", report.Summary.Errors))
	}
	for _, vacancy := range report.Vacancies {
		item, err := careerAgentRunItem(vacancy, runID)
		if err != nil {
			return careeragent.DailyStageResult{}, fmt.Errorf("build daily vacancy item: %w", err)
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		result.Items = append(result.Items, item)
	}
	// Vacancy review/failure items are part of the same derived attention
	// projection as communication work items. Keep the stage result useful to
	// CLI callers immediately; the dashboard rebuilds the queue from durable
	// run items after restart.
	result.Attention = append(result.Attention, attentionFromRunItems(result.Items)...)
	return result, nil
}

func dailyStageResultFromCommunicationReport(report CommunicationRunReport) careeragent.DailyStageResult {
	result := careeragent.DailyStageResult{}
	result.Summary.Communication = careeragent.DailyCommunicationSummary{
		ConversationsSynced: report.Conversations,
		NewMessages:         report.Sync.Created + report.Sync.Updated,
		RepliesNeeded:       report.Drafts,
		Failures:            report.Failures,
	}
	if report.Failures > 0 {
		result.Summary.Failures = append(result.Summary.Failures, fmt.Sprintf("communication stage reported %d persistence failures", report.Failures))
	}
	return result
}

func dailyCommunicationStageResult(report CommunicationRunReport, inbox CandidateInbox, runID string, now time.Time) (careeragent.DailyStageResult, error) {
	result := dailyStageResultFromCommunicationReport(report)
	if strings.TrimSpace(runID) == "" {
		return result, errors.New("daily communication stage requires a run id")
	}
	for _, item := range inbox.Items {
		workItems := item.Workflow.CommunicationItems
		if len(workItems) == 0 {
			result.Items = append(result.Items, careeragent.AgentRunItem{ID: "communication-item-" + item.Conversation.ID, RunID: runID, TargetType: "communication_conversation", TargetID: item.Conversation.ID, ApplicationID: item.Conversation.ApplicationID, ConversationID: item.Conversation.ID, VacancyID: item.Conversation.VacancyID, Stage: careeragent.AgentRunStageCommunication, Status: careeragent.AgentRunItemStatusPrepared, DecisionCode: item.Bucket, CreatedAt: now})
			continue
		}
		for _, workItem := range workItems {
			id := firstNonEmpty(workItem.ID, item.Conversation.ID)
			status := careeragent.AgentRunItemStatusPrepared
			if workItem.RequiresReview {
				status = careeragent.AgentRunItemStatusReviewRequired
			}
			result.Items = append(result.Items, careeragent.AgentRunItem{ID: "communication-item-" + id, RunID: runID, TargetType: "communication_work_item", TargetID: id, ApplicationID: item.Conversation.ApplicationID, ConversationID: item.Conversation.ID, VacancyID: item.Conversation.VacancyID, Stage: careeragent.AgentRunStageCommunication, Status: status, DecisionCode: string(workItem.Type), CreatedAt: now})
			if workItem.RequiresReview {
				result.Attention = append(result.Attention, careeragent.AttentionItem{ID: "communication:" + id, Type: string(workItem.Type), Priority: 0, ApplicationID: item.Conversation.ApplicationID, ConversationID: item.Conversation.ID, VacancyID: item.Conversation.VacancyID, Title: "Требуется проверка сообщения", Summary: string(workItem.Type), Reason: "communication_work_item_requires_review", Risk: "manual_reply", NextAction: "Проверить диалог вручную", CreatedAt: now, UpdatedAt: now})
			}
		}
	}
	return result, nil
}

// NewRuntimeDailyCareerAgentService composes the shared application service
// from existing read-only runtime use cases. It deliberately does not accept
// HHWriteGateway, HHWriteClient, approvals, or send callbacks.
func NewRuntimeDailyCareerAgentService(responder *HHAIResponder, dashboard *DashboardServer, workflow ports.CareerWorkflowStore) (*DailyCareerAgentService, error) {
	if workflow == nil && responder != nil {
		workflow = responder.careerWorkflowStore
	}
	if workflow == nil && dashboard != nil {
		workflow = dashboard.CareerWorkflow
	}
	deps := DailyCareerAgentDependencies{Workflow: workflow}
	if responder != nil {
		deps.Vacancy = responder.dailyVacancyStage()
	}
	if dashboard != nil {
		deps.Communication = dashboard.dailyCommunicationStage()
	}
	return NewDailyCareerAgentService(deps)
}

func (r *HHAIResponder) dailyVacancyStage() DailyCareerAgentStage {
	return func(ctx context.Context, now time.Time, runID string) (careeragent.DailyStageResult, error) {
		if r == nil {
			return careeragent.DailyStageResult{}, errors.New("daily vacancy responder is nil")
		}
		oldContext, oldMode, oldAutoApply := r.ctx, r.careerAgentMode, r.autoApply
		oldDryRun, oldWriteEnabled := r.dryRun, r.hhWriteEnabled
		oldAutoChat, oldAutoTouch, oldAutoStatus, oldChatMode := r.autoChat, r.autoTouch, r.autoJobStatus, r.chatMode
		oldWriter := r.eventWriter
		defer func() {
			r.ctx, r.careerAgentMode, r.autoApply = oldContext, oldMode, oldAutoApply
			r.dryRun, r.hhWriteEnabled = oldDryRun, oldWriteEnabled
			r.autoChat, r.autoTouch, r.autoJobStatus, r.chatMode = oldAutoChat, oldAutoTouch, oldAutoStatus, oldChatMode
			r.eventWriter = oldWriter
		}()
		r.ctx = ctx
		r.careerAgentMode, r.autoApply = "shadow", true
		r.dryRun, r.hhWriteEnabled = true, false
		r.autoChat, r.autoTouch, r.autoJobStatus, r.chatMode = false, false, false, "off"
		var events bytes.Buffer
		r.eventWriter = &events
		err := r.ApplyVacancies()
		report := CareerAgentRunReport{Summary: RunSummaryResult{Type: "run_summary"}, Vacancies: []CareerAgentVacancyResult{}, Events: []json.RawMessage{}}
		collectCareerAgentEvents(&report, events.String())
		result, mapErr := dailyStageResultFromCareerAgentReport(report, runID, now)
		if mapErr != nil {
			return result, mapErr
		}
		return result, err
	}
}

func (s *DashboardServer) dailyCommunicationStage() DailyCareerAgentStage {
	return func(ctx context.Context, now time.Time, runID string) (careeragent.DailyStageResult, error) {
		if s == nil {
			return careeragent.DailyStageResult{}, errors.New("daily communication dashboard is nil")
		}
		report, inbox, err := s.refreshDailyCommunicationSnapshot(ctx, now)
		result, mapErr := dailyCommunicationStageResult(report, inbox, runID, now)
		if err != nil {
			return result, err
		}
		return result, mapErr
	}
}
