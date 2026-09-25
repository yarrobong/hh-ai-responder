package runtime

import (
	"context"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/usecase/communicationworkitem"
)

const (
	dailyAttentionApplicationReady = "application_ready"
	dailyAttentionVacancyReview    = "vacancy_review"
	dailyAttentionClarifications   = "clarifications"
	dailyAttentionNeedsReply       = "needs_reply"
	dailyAttentionInterviews       = "interviews"
	dailyAttentionTests            = "tests"
	dailyAttentionOffers           = "offers"
	dailyAttentionFollowUps        = "follow_ups"
	dailyAttentionOther            = "other"
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
		for _, notification := range deduplicateAttentionNotifications(s.Notifications.List()) {
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
	for _, preparation := range snapshot.preparations {
		if preparation.Status != careeragent.PreparationStatusReady {
			continue
		}
		if !preparationAttentionActionable(preparation, snapshot.applications) {
			continue
		}
		items = append(items, careeragent.AttentionItem{
			ID: "preparation:" + preparation.ID, Type: "application_ready", Priority: 1,
			VacancyID: preparation.VacancyID, Title: "Application preparation ready",
			Summary: "Read-only application preparation is ready for manual review.",
			Reason:  "preparation_ready", Risk: "manual_approval_required",
			NextAction: "Review preparation before any explicit send", CreatedAt: preparation.CreatedAt, UpdatedAt: preparation.UpdatedAt,
		})
	}
	if reader, ok := s.CareerWorkflow.(interface {
		ListRunItems(context.Context, string, int) ([]careeragent.AgentRunItem, error)
	}); ok {
		var latest *careeragent.AgentRun
		for _, run := range snapshot.careerRuns {
			if run.RunType != "daily_career_agent" {
				continue
			}
			candidate := run
			if latest == nil || candidate.StartedAt.After(latest.StartedAt) {
				latest = &candidate
			}
		}
		if latest != nil {
			runItems, err := reader.ListRunItems(context.Background(), latest.ID, 1000)
			if err != nil {
				return nil, err
			}
			items = append(items, attentionFromRunItemsForSnapshot(runItems, snapshot, time.Now().UTC())...)
		}
	}
	return careeragent.BuildAttentionQueue(items), nil
}

// preparationAttentionActionable keeps a ready preparation visible until the
// application projection contains authoritative provider evidence. A local
// JobApplication row, an external id, or an applied status alone is not proof
// that HH accepted the response and therefore must not suppress attention.
func preparationAttentionActionable(preparation careeragent.ApplicationPreparation, applications []JobApplication) bool {
	for _, application := range applications {
		if application.VacancyID != preparation.VacancyID ||
			application.Source != ApplicationSourceHH ||
			application.Partial ||
			strings.TrimSpace(application.ExternalID) == "" ||
			!applicationStatusConfirmsResponse(application.Status) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(application.HHMetadata["delivery_confirmed"]), "true") || hasAuthoritativeHHReconciliation(application.ReconciliationEvidence) {
			return false
		}
	}
	return true
}

func applicationStatusConfirmsResponse(status ApplicationStatus) bool {
	switch status {
	case ApplicationApplied, ApplicationEmployerReplied, ApplicationInterview, ApplicationOffer, ApplicationRejected, ApplicationArchived:
		return true
	default:
		return false
	}
}

func hasAuthoritativeHHReconciliation(evidence []ReconciliationEvidence) bool {
	for _, item := range evidence {
		if strings.EqualFold(strings.TrimSpace(item.Source), "hh") &&
			strings.TrimSpace(item.Method) != "" &&
			item.Confidence >= 1 {
			return true
		}
	}
	return false
}

// deduplicateAttentionNotifications collapses repeated notification records
// only in the derived attention read model. The notification store remains the
// source of truth for immutable history, fingerprints, and lifecycle changes.
// A newer notification for the same conversation/type supersedes an older
// notification without hiding distinct notification types or unrelated items.
func deduplicateAttentionNotifications(notifications []CandidateNotification) []CandidateNotification {
	latest := make(map[string]CandidateNotification, len(notifications))
	order := make([]string, 0, len(notifications))
	for _, notification := range notifications {
		identity := attentionNotificationIdentity(notification)
		previous, exists := latest[identity]
		if !exists {
			order = append(order, identity)
			latest[identity] = notification
			continue
		}
		if !notification.CreatedAt.Before(previous.CreatedAt) {
			latest[identity] = notification
		}
	}
	result := make([]CandidateNotification, 0, len(order))
	for _, identity := range order {
		result = append(result, latest[identity])
	}
	return result
}

func attentionNotificationIdentity(notification CandidateNotification) string {
	typeName := string(notification.Type)
	switch {
	case strings.TrimSpace(notification.RelatedConversationID) != "":
		return "conversation:" + notification.RelatedConversationID + ":" + typeName
	case strings.TrimSpace(notification.RelatedApplicationID) != "":
		return "application:" + notification.RelatedApplicationID + ":" + typeName
	case notification.RelatedVacancyID > 0:
		return "vacancy:" + strconv.Itoa(notification.RelatedVacancyID) + ":" + typeName
	default:
		return "fingerprint:" + notification.Fingerprint
	}
}

func dailyAttentionBreakdown(items []careeragent.AttentionItem) map[string]int {
	result := map[string]int{
		dailyAttentionApplicationReady: 0,
		dailyAttentionVacancyReview:    0,
		dailyAttentionClarifications:   0,
		dailyAttentionNeedsReply:       0,
		dailyAttentionInterviews:       0,
		dailyAttentionTests:            0,
		dailyAttentionOffers:           0,
		dailyAttentionFollowUps:        0,
		dailyAttentionOther:            0,
	}
	for _, item := range careeragent.BuildAttentionQueue(items) {
		result[dailyAttentionCategory(item)]++
	}
	return result
}

func dailyAttentionCategory(item careeragent.AttentionItem) string {
	communicationType := strings.ToUpper(strings.TrimSpace(item.Summary))
	if strings.Contains(item.ID, "communication_work_item:") {
		switch communicationType {
		case "INTERVIEW":
			return dailyAttentionInterviews
		case "TEST_TASK":
			return dailyAttentionTests
		case "OFFER":
			return dailyAttentionOffers
		case "FOLLOW_UP_DUE":
			return dailyAttentionFollowUps
		}
	}
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "candidate_clarification", "clarification_required":
		return dailyAttentionClarifications
	case "review_required", "failed":
		if strings.Contains(item.ID, ":vacancy:") && item.VacancyID > 0 {
			return dailyAttentionVacancyReview
		}
		return dailyAttentionOther
	case "new_employer_message", "candidate_action_required", "needs_reply":
		return dailyAttentionNeedsReply
	case "interview_detected", "interview":
		return dailyAttentionInterviews
	case "external_action_required", "test_task", "test":
		return dailyAttentionTests
	case "offer_detected", "offer":
		return dailyAttentionOffers
	case "follow_up_available", "follow_up_due":
		return dailyAttentionFollowUps
	case "application_ready", "prepared":
		return dailyAttentionApplicationReady
	default:
		return dailyAttentionOther
	}
}

func attentionFromRunItems(runItems []careeragent.AgentRunItem) []careeragent.AttentionItem {
	return attentionFromRunItemsForSnapshot(runItems, dashboardSnapshot{}, time.Now().UTC())
}

func attentionFromRunItemsForSnapshot(runItems []careeragent.AgentRunItem, snapshot dashboardSnapshot, now time.Time) []careeragent.AttentionItem {
	applications := make(map[string]JobApplication, len(snapshot.applications))
	for _, application := range snapshot.applications {
		applications[application.ID] = application
		if application.ConversationID != "" {
			applications[application.ConversationID] = application
		}
	}
	vacancies := make(map[int]Vacancy, len(snapshot.vacancies))
	for _, vacancy := range snapshot.vacancies {
		vacancies[vacancy.ID] = vacancy
	}
	conversations := make(map[string]EmployerConversation, len(snapshot.conversations))
	currentItems := make(map[string][]communicationworkitem.WorkItem, len(snapshot.conversations))
	for _, conversation := range snapshot.conversations {
		conversations[conversation.ID] = conversation
		application := applications[conversation.ID]
		projection := classifyCareerWorkflow(application, conversation, nil, now, nil, nil)
		currentItems[conversation.ID] = projection.CommunicationItems
	}
	result := make([]careeragent.AttentionItem, 0)
	for _, item := range runItems {
		communication := item.TargetType == "communication_work_item" || item.TargetType == "communication_conversation"
		workItem, sourceMessageAt := communicationWorkItemFromRunItem(item)
		if communication {
			if item.TargetType != "communication_work_item" || (item.Status != careeragent.AgentRunItemStatusReviewRequired && item.Status != careeragent.AgentRunItemStatusFailed && workItem.Type != communicationworkitem.TypeFollowUp) {
				continue
			}
			conversation := conversations[item.ConversationID]
			application := applications[item.ApplicationID]
			vacancy := vacancies[item.VacancyID]
			var vacancyPtr *Vacancy
			if vacancy.ID != 0 {
				vacancyCopy := vacancy
				vacancyPtr = &vacancyCopy
			}
			decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: workItem, Conversation: conversation, Application: application, Vacancy: vacancyPtr, CurrentItems: currentItems[item.ConversationID], SourceMessageAt: sourceMessageAt, Now: now})
			if !decision.Actionable {
				continue
			}
			result = append(result, careeragent.AttentionItem{
				ID: "communication_work_item:" + item.TargetID, Type: string(workItem.Type), Priority: 0,
				VacancyID: item.VacancyID, ApplicationID: item.ApplicationID, ConversationID: item.ConversationID,
				Title: "Communication item", Summary: string(workItem.Type), Reason: "communication_work_item_requires_review", Risk: "manual_reply",
				NextAction: "Открыть и проверить вручную", CreatedAt: item.CreatedAt, UpdatedAt: item.CreatedAt,
			})
			continue
		}
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
