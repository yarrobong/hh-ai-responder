package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
)

type CommunicationRunReport struct {
	Run               careeragent.AgentRun `json:"run"`
	Sync              SyncResult           `json:"sync"`
	Conversations     int                  `json:"conversations"`
	Classified        int                  `json:"classified"`
	Drafts            int                  `json:"drafts"`
	Clarifications    int                  `json:"clarifications"`
	WorkItems         int                  `json:"work_items"`
	ReviewRequired    int                  `json:"review_required"`
	Failures          int                  `json:"failures"`
	HHWritesAttempted int                  `json:"hh_writes_attempted"`
	IdempotentReplay  bool                 `json:"idempotent_replay"`
}

// RunDailyCommunication performs one read-only communication pass. It uses
// the existing HH read sync and Inbox projections, persists only redacted
// local telemetry, and never receives HHWriteGateway or an HH write client.
func (s *DashboardServer) RunDailyCommunication(ctx context.Context, now time.Time) (CommunicationRunReport, error) {
	report, inbox, err := s.refreshDailyCommunicationSnapshot(ctx, now)
	if err != nil {
		return report, err
	}
	runID := communicationRunID(inbox)
	run := careeragent.NewAgentRun(runID, careeragent.AgentRunStageCommunication, now.UTC())
	run.RunType = "daily_communication"
	report.Run = run
	if existing, getErr := s.CareerWorkflow.GetRun(ctx, runID); getErr == nil {
		report.Run = existing
		report.IdempotentReplay = existing.Status != careeragent.AgentRunStatusRunning
		return report, nil
	} else if !errors.Is(getErr, careeragent.ErrAgentRunNotFound) {
		return report, getErr
	}
	if err := s.CareerWorkflow.StartRun(ctx, run); err != nil {
		return report, err
	}
	if err := persistCommunicationRunItems(ctx, s.CareerWorkflow, runID, inbox, now.UTC()); err != nil {
		report.Failures++
	}
	status, result := careeragent.AgentRunStatusCompleted, "communication scan completed"
	if report.Failures > 0 {
		status, result = careeragent.AgentRunStatusPartial, "communication scan completed with persistence errors"
	}
	if err := report.Run.Finish(status, result, now.UTC(), nil); err != nil {
		return report, err
	}
	if err := s.CareerWorkflow.FinishRun(ctx, report.Run); err != nil {
		return report, err
	}
	return report, nil
}

// refreshDailyCommunication is the shared read-only stage used by the
// unified daily Career Agent. It deliberately does not create a second
// communication AgentRun; the outer daily service owns that lifecycle.
func (s *DashboardServer) refreshDailyCommunication(ctx context.Context, now time.Time) (CommunicationRunReport, error) {
	report, _, err := s.refreshDailyCommunicationSnapshot(ctx, now)
	return report, err
}

func (s *DashboardServer) refreshDailyCommunicationSnapshot(ctx context.Context, now time.Time) (CommunicationRunReport, CandidateInbox, error) {
	if s == nil || s.Sync == nil || s.CareerWorkflow == nil {
		return CommunicationRunReport{}, CandidateInbox{}, errors.New("daily communication run is not configured")
	}
	if ctx == nil {
		return CommunicationRunReport{}, CandidateInbox{}, errors.New("daily communication context is nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	syncResult, syncErr := s.Sync.RefreshInbox(ctx)
	report := CommunicationRunReport{Sync: syncResult}
	if syncErr != nil {
		return report, CandidateInbox{}, syncErr
	}
	inbox, err := s.inbox()
	if err != nil {
		return report, CandidateInbox{}, err
	}
	report.Conversations = len(inbox.Items)
	for _, item := range inbox.Items {
		if item.LatestMessage != nil && item.LatestMessage.Sender == ConversationSenderEmployer {
			report.Classified++
		}
		report.Drafts += len(item.AIDrafts)
		report.Clarifications += len(item.PendingClarifications)
		report.WorkItems += len(item.Workflow.CommunicationItems)
		for _, workItem := range item.Workflow.CommunicationItems {
			if workItem.RequiresReview {
				report.ReviewRequired++
			}
		}
	}

	return report, inbox, nil
}

func communicationRunID(inbox CandidateInbox) string {
	keys := make([]string, 0, len(inbox.Items))
	for _, item := range inbox.Items {
		keys = append(keys, strings.Join([]string{item.Conversation.ID, item.Workflow.LastEmployerMessageHash, item.Bucket}, "\x00"))
		for _, workItem := range item.Workflow.CommunicationItems {
			keys = append(keys, workItem.Key)
		}
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\x00")))
	return "communication-" + hex.EncodeToString(sum[:])[:24]
}

func persistCommunicationRunItems(ctx context.Context, store ports.CareerWorkflowStore, runID string, inbox CandidateInbox, now time.Time) error {
	for _, item := range inbox.Items {
		workItems := make([]communicationTelemetryItem, 0, len(item.Workflow.CommunicationItems))
		for _, workItem := range item.Workflow.CommunicationItems {
			workItems = append(workItems, communicationTelemetryItem{ID: workItem.ID, Type: string(workItem.Type), MessageID: workItem.MessageID, RequiresReview: workItem.RequiresReview})
		}
		if len(workItems) == 0 {
			workItems = []communicationTelemetryItem{{ID: item.Conversation.ID, Type: "conversation", Bucket: item.Bucket}}
		}
		for _, workItem := range workItems {
			id, targetID, targetType, bucket, requiresReview := item.Conversation.ID, item.Conversation.ID, "communication_conversation", firstNonEmpty(workItem.Bucket, item.Bucket), false
			if workItem.ID != "" {
				id, targetID, targetType, bucket, requiresReview = workItem.ID, workItem.ID, "communication_work_item", workItem.Type, workItem.RequiresReview
			}
			evidence, err := json.Marshal(struct {
				Bucket         string `json:"bucket"`
				Type           string `json:"type"`
				MessageID      string `json:"message_id,omitempty"`
				RequiresReview bool   `json:"requires_review"`
			}{Bucket: bucket, Type: targetType, MessageID: workItem.MessageID, RequiresReview: requiresReview})
			if err != nil {
				return fmt.Errorf("encode communication telemetry: %w", err)
			}
			status := careeragent.AgentRunItemStatusPrepared
			if requiresReview {
				status = careeragent.AgentRunItemStatusReviewRequired
			}
			runItem := careeragent.AgentRunItem{ID: "communication-item-" + id, RunID: runID, TargetType: targetType, TargetID: targetID,
				ApplicationID: item.Conversation.ApplicationID, ConversationID: item.Conversation.ID, VacancyID: item.Conversation.VacancyID,
				Stage: careeragent.AgentRunStageCommunication, Status: status, DecisionCode: bucket, Evidence: evidence, CreatedAt: now}
			if err := store.UpsertRunItem(ctx, runItem); err != nil {
				return err
			}
		}
	}
	return nil
}

type communicationTelemetryItem struct {
	ID             string
	Type           string
	MessageID      string
	Bucket         string
	RequiresReview bool
}
