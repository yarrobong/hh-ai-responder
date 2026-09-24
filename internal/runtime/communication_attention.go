package runtime

import (
	"encoding/json"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/usecase/communicationworkitem"
)

// CommunicationAttentionSuppressionReason is a derived read-model reason. It
// does not change the persisted work-item lifecycle or erase its history.
type CommunicationAttentionSuppressionReason string

const (
	CommunicationAttentionSuperseded           CommunicationAttentionSuppressionReason = "SUPERSEDED"
	CommunicationAttentionResponded            CommunicationAttentionSuppressionReason = "RESPONDED"
	CommunicationAttentionExpired              CommunicationAttentionSuppressionReason = "EXPIRED"
	CommunicationAttentionApplicationClosed    CommunicationAttentionSuppressionReason = "APPLICATION_CLOSED"
	CommunicationAttentionVacancyClosed        CommunicationAttentionSuppressionReason = "VACANCY_CLOSED"
	CommunicationAttentionResolved             CommunicationAttentionSuppressionReason = "RESOLVED"
	CommunicationAttentionConversationAdvanced CommunicationAttentionSuppressionReason = "CONVERSATION_ADVANCED"
)

type CommunicationWorkItemActionabilityInput struct {
	Item            communicationworkitem.WorkItem
	Conversation    EmployerConversation
	Application     JobApplication
	Vacancy         *Vacancy
	CurrentItems    []communicationworkitem.WorkItem
	SourceMessageAt *time.Time
	Now             time.Time
}

type CommunicationWorkItemActionability struct {
	Actionable        bool
	SuppressionReason CommunicationAttentionSuppressionReason
}

type communicationRunEvidence struct {
	Bucket          string     `json:"bucket"`
	Type            string     `json:"type"`
	MessageID       string     `json:"message_id,omitempty"`
	SourceMessageAt *time.Time `json:"source_message_at,omitempty"`
	RequiresReview  bool       `json:"requires_review"`
	WorkItemStatus  string     `json:"work_item_status,omitempty"`
	ScheduledDate   string     `json:"scheduled_date,omitempty"`
	ScheduledTime   string     `json:"scheduled_time,omitempty"`
	Timezone        string     `json:"timezone,omitempty"`
	DueAt           *time.Time `json:"due_at,omitempty"`
}

func communicationWorkItemFromRunItem(item careeragent.AgentRunItem) (communicationworkitem.WorkItem, *time.Time) {
	evidence := communicationRunEvidence{Type: item.DecisionCode}
	_ = json.Unmarshal(item.Evidence, &evidence)
	workItem := communicationworkitem.WorkItem{ID: item.TargetID, Type: communicationworkitem.Type(evidence.Type), Status: communicationworkitem.Status(evidence.WorkItemStatus), ConversationID: item.ConversationID, ApplicationID: item.ApplicationID, VacancyID: item.VacancyID, MessageID: evidence.MessageID, ScheduledDate: evidence.ScheduledDate, ScheduledTime: evidence.ScheduledTime, Timezone: evidence.Timezone, DueAt: evidence.DueAt, RequiresReview: evidence.RequiresReview, CreatedAt: item.CreatedAt, UpdatedAt: item.CreatedAt}
	if workItem.Type == "" {
		workItem.Type = communicationworkitem.Type(item.DecisionCode)
	}
	if workItem.Status == "" && item.Status == careeragent.AgentRunItemStatusReviewRequired {
		workItem.Status = communicationworkitem.StatusManualReview
	}
	return workItem, evidence.SourceMessageAt
}

// IsCommunicationWorkItemActionable derives the operator-facing active state
// from existing communication, application, and vacancy projections. It is
// deliberately pure: it neither mutates a work item nor creates a lifecycle
// state machine of its own.
func IsCommunicationWorkItemActionable(input CommunicationWorkItemActionabilityInput) CommunicationWorkItemActionability {
	item := input.Item
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if item.Status == communicationworkitem.StatusDone {
		return suppressedCommunicationWorkItem(CommunicationAttentionResolved)
	}
	if applicationClosed(input.Application) {
		return suppressedCommunicationWorkItem(CommunicationAttentionApplicationClosed)
	}
	if input.Conversation.Status == ConversationRejected || input.Conversation.Status == ConversationClosed {
		return suppressedCommunicationWorkItem(CommunicationAttentionApplicationClosed)
	}
	if input.Vacancy != nil && input.Vacancy.Archived {
		return suppressedCommunicationWorkItem(CommunicationAttentionVacancyClosed)
	}
	if item.Type == communicationworkitem.TypeFollowUp && item.DueAt != nil && input.Conversation.LastEmployerMessageAt != nil && input.Conversation.LastEmployerMessageAt.After(*item.DueAt) {
		return suppressedCommunicationWorkItem(CommunicationAttentionConversationAdvanced)
	}

	sourceAt := input.SourceMessageAt
	if sourceAt == nil && item.MessageID != "" {
		for _, message := range input.Conversation.Messages {
			if message.ID == item.MessageID {
				at := message.Timestamp
				sourceAt = &at
				break
			}
		}
	}
	if sourceAt != nil {
		for _, message := range input.Conversation.Messages {
			if message.Timestamp.IsZero() || !message.Timestamp.After(*sourceAt) {
				continue
			}
			switch message.Sender {
			case ConversationSenderCandidate:
				return suppressedCommunicationWorkItem(CommunicationAttentionResponded)
			case ConversationSenderEmployer:
				reason := CommunicationAttentionConversationAdvanced
				for _, current := range input.CurrentItems {
					if current.Type == item.Type && current.MessageID != item.MessageID {
						reason = CommunicationAttentionSuperseded
						break
					}
				}
				return suppressedCommunicationWorkItem(reason)
			}
		}
	}

	if expiry, ok := communicationWorkItemExpiry(item); ok && !input.Now.Before(expiry) {
		return suppressedCommunicationWorkItem(CommunicationAttentionExpired)
	}
	// A rejection is terminal evidence, not an action the candidate needs to
	// perform. Keep it in the conversation timeline, but do not leave it in the
	// active queue forever when no explicit resolution record exists.
	if item.Type == communicationworkitem.TypeRejection {
		return suppressedCommunicationWorkItem(CommunicationAttentionResolved)
	}
	return CommunicationWorkItemActionability{Actionable: true}
}

func suppressedCommunicationWorkItem(reason CommunicationAttentionSuppressionReason) CommunicationWorkItemActionability {
	return CommunicationWorkItemActionability{SuppressionReason: reason}
}

func applicationClosed(application JobApplication) bool {
	switch application.Status {
	case ApplicationRejected, ApplicationArchived:
		return true
	default:
		return false
	}
}

func communicationWorkItemExpiry(item communicationworkitem.WorkItem) (time.Time, bool) {
	if item.DueAt != nil && (item.Type == communicationworkitem.TypeInterview || item.Type == communicationworkitem.TypeTest) {
		return item.DueAt.UTC(), true
	}
	if item.Type != communicationworkitem.TypeInterview || strings.TrimSpace(item.ScheduledDate) == "" || strings.TrimSpace(item.ScheduledTime) == "" || strings.TrimSpace(item.Timezone) == "" {
		return time.Time{}, false
	}
	location, ok := communicationLocation(item.Timezone)
	if !ok {
		return time.Time{}, false
	}
	date, ok := communicationDate(item.ScheduledDate)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", date+" "+strings.TrimSpace(item.ScheduledTime), location)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func communicationDate(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"02.01.2006", "02/01/2006", "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format("2006-01-02"), true
		}
	}
	return "", false
}

func communicationLocation(value string) (*time.Location, bool) {
	value = strings.TrimSpace(value)
	if value == "UTC" || value == "Z" {
		return time.UTC, true
	}
	if len(value) == len("+05:00") && (value[0] == '+' || value[0] == '-') {
		hours := int(value[1]-'0')*10 + int(value[2]-'0')
		minutes := int(value[4]-'0')*10 + int(value[5]-'0')
		if value[3] == ':' && hours <= 23 && minutes <= 59 {
			offset := (hours*60 + minutes) * 60
			if value[0] == '-' {
				offset = -offset
			}
			return time.FixedZone(value, offset), true
		}
	}
	location, err := time.LoadLocation(value)
	return location, err == nil
}
