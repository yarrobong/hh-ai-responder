package runtime

import (
	"context"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func (s *DashboardServer) buildAttentionQueue(snapshot dashboardSnapshot) ([]careeragent.AttentionItem, error) {
	items := make([]careeragent.AttentionItem, 0)
	for _, clarification := range snapshot.clarifications {
		if clarification.Status != ClarificationPending {
			continue
		}
		at := clarification.CreatedAt
		items = append(items, careeragent.AttentionItem{
			ID: "clarification:" + clarification.ID, Type: "candidate_clarification", Priority: 0,
			ApplicationID: clarification.ApplicationID, ConversationID: clarification.ConversationID,
			Title: clarification.Topic, Summary: clarification.Question, Reason: clarification.Reason,
			Risk: "candidate_fact_unknown", NextAction: "Ответить на уточнение вручную", CreatedAt: at, UpdatedAt: at,
		})
	}
	if s.Notifications != nil {
		for _, notification := range s.Notifications.List() {
			if notification.Lifecycle == NotificationResolved || notification.Lifecycle == NotificationDismissed {
				continue
			}
			priority := 3
			switch notification.Priority {
			case NotificationPriorityCritical:
				priority = 0
			case NotificationPriorityHigh:
				priority = 1
			case NotificationPriorityMedium:
				priority = 2
			}
			items = append(items, careeragent.AttentionItem{
				ID: "notification:" + notification.ID, Type: string(notification.Type), Priority: priority,
				VacancyID: notification.RelatedVacancyID, ApplicationID: notification.RelatedApplicationID, ConversationID: notification.RelatedConversationID,
				Title: string(notification.Type), Summary: notification.Message, Reason: notification.Fingerprint,
				Risk: notificationRisk(notification.Type), NextAction: notification.DetailPath, CreatedAt: notification.CreatedAt, UpdatedAt: notification.CreatedAt,
			})
		}
	}
	if reader, ok := s.CareerWorkflow.(interface {
		ListRunItems(context.Context, string, int) ([]careeragent.AgentRunItem, error)
	}); ok {
		for _, run := range snapshot.careerRuns {
			if run.RunType != "daily_career_agent" {
				continue
			}
			runItems, err := reader.ListRunItems(context.Background(), run.ID, 1000)
			if err != nil {
				return nil, err
			}
			items = append(items, attentionFromRunItems(runItems)...)
			break
		}
	}
	return careeragent.BuildAttentionQueue(items), nil
}

func attentionFromRunItems(runItems []careeragent.AgentRunItem) []careeragent.AttentionItem {
	result := make([]careeragent.AttentionItem, 0)
	for _, item := range runItems {
		if item.Status != careeragent.AgentRunItemStatusReviewRequired && item.Status != careeragent.AgentRunItemStatusFailed {
			continue
		}
		priority := 1
		if item.Status == careeragent.AgentRunItemStatusFailed {
			priority = 0
		}
		title := "Career Agent item"
		summary := strings.TrimSpace(item.DecisionCode)
		if item.TargetType == "communication_work_item" || item.TargetType == "communication_conversation" {
			title = "Communication item"
		} else if item.VacancyID > 0 {
			title = "Vacancy " + strconv.Itoa(item.VacancyID)
		}
		if summary == "" {
			summary = string(item.Status)
		}
		result = append(result, careeragent.AttentionItem{
			ID: "run-item:" + item.RunID + ":" + item.TargetKey(), Type: string(item.Status), Priority: priority,
			VacancyID: item.VacancyID, ApplicationID: item.ApplicationID, ConversationID: item.ConversationID,
			Title: title, Summary: summary, Reason: item.ErrorCode, Risk: attentionRisk(item.Status),
			NextAction: "Открыть и проверить вручную", CreatedAt: item.CreatedAt, UpdatedAt: item.CreatedAt,
		})
	}
	return result
}

func notificationRisk(value CandidateNotificationType) string {
	if value == NotificationDeliveryUncertain || value == NotificationExternalActionRequired || value == NotificationOfferDetected {
		return "high"
	}
	return "medium"
}

func attentionRisk(value careeragent.AgentRunItemStatus) string {
	if value == careeragent.AgentRunItemStatusFailed {
		return "high"
	}
	return "medium"
}

func attentionQueueJSON(items []careeragent.AttentionItem) map[string]any {
	return map[string]any{"items": items, "count": len(items), "generated_at": time.Now().UTC()}
}
