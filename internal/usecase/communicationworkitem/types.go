package communicationworkitem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/conversationpolicy"
)

type Type string

const (
	TypeInterview Type = "INTERVIEW"
	TypeTest      Type = "TEST_TASK"
	TypeOffer     Type = "OFFER"
	TypeRejection Type = "REJECTED"
	TypeFollowUp  Type = "FOLLOW_UP_DUE"
)

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusManualReview Status = "MANUAL_REVIEW"
	StatusDone         Status = "DONE"
)

type WorkItem struct {
	ID             string     `json:"id"`
	Key            string     `json:"key"`
	Type           Type       `json:"type"`
	Status         Status     `json:"status"`
	ConversationID string     `json:"conversation_id,omitempty"`
	ApplicationID  string     `json:"application_id,omitempty"`
	VacancyID      int        `json:"vacancy_id,omitempty"`
	MessageID      string     `json:"message_id,omitempty"`
	Evidence       []string   `json:"evidence,omitempty"`
	ScheduledDate  string     `json:"scheduled_date,omitempty"`
	ScheduledTime  string     `json:"scheduled_time,omitempty"`
	Timezone       string     `json:"timezone,omitempty"`
	Link           string     `json:"link,omitempty"`
	Deadline       string     `json:"deadline,omitempty"`
	MissingFields  []string   `json:"missing_fields,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	RequiresReview bool       `json:"requires_review"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Input struct {
	ConversationID string
	ApplicationID  string
	VacancyID      int
	MessageID      string
	MessageText    string
	Classification conversationpolicy.MessageClassification
	FollowUpDueAt  *time.Time
	Now            time.Time
}

var (
	datePattern     = regexp.MustCompile(`(?i)\b(\d{1,2})[./](\d{1,2})(?:[./](\d{2,4}))?\b`)
	timePattern     = regexp.MustCompile(`\b([01]?\d|2[0-3]):([0-5]\d)\b`)
	linkPattern     = regexp.MustCompile(`https?://[^\s<>]+`)
	deadlinePattern = regexp.MustCompile(`(?i)(?:до|deadline|дедлайн(?:а)?|срок)\s*[:\-]?\s*((?:\d{1,2}[./]\d{1,2}(?:[./]\d{2,4})?)|(?:\d{1,2}\s+(?:января|февраля|марта|апреля|мая|июня|июля|августа|сентября|октября|ноября|декабря)))`)
)

func Project(input Input) []WorkItem {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := make([]WorkItem, 0, 2)
	add := func(kind Type, evidence string) {
		key := deterministicKey(input, kind)
		item := WorkItem{ID: "communication-work-" + key[:20], Key: key, Type: kind, Status: StatusPending,
			ConversationID: input.ConversationID, ApplicationID: input.ApplicationID, VacancyID: input.VacancyID,
			MessageID: input.MessageID, Evidence: []string{evidence}, CreatedAt: now, UpdatedAt: now}
		if kind == TypeInterview {
			item.ScheduledDate, item.ScheduledTime = explicitDateAndTime(input.MessageText)
			if item.ScheduledDate == "" {
				item.MissingFields = append(item.MissingFields, "date")
			}
			if item.ScheduledTime == "" {
				item.MissingFields = append(item.MissingFields, "time")
			}
			item.MissingFields = append(item.MissingFields, "timezone")
			item.RequiresReview = true
			item.Status = StatusManualReview
		} else if kind == TypeTest {
			item.RequiresReview = true
			item.Status = StatusManualReview
			item.Link = firstLink(input.MessageText)
			item.Deadline = explicitDeadline(input.MessageText)
			if item.Link == "" {
				item.MissingFields = append(item.MissingFields, "link")
			}
			if item.Deadline == "" {
				item.MissingFields = append(item.MissingFields, "deadline")
			}
		} else if kind == TypeOffer || kind == TypeRejection {
			item.RequiresReview = true
			item.Status = StatusManualReview
		}
		result = append(result, item)
	}

	switch input.Classification.Type {
	case conversationpolicy.MessageTypeInterviewInvitation, conversationpolicy.MessageTypeInterviewScheduling:
		add(TypeInterview, "deterministic interview invitation classification")
	case conversationpolicy.MessageTypeTestAssignment:
		add(TypeTest, "deterministic test assignment classification")
	case conversationpolicy.MessageTypeOffer:
		add(TypeOffer, "deterministic offer classification")
	case conversationpolicy.MessageTypeRejection:
		add(TypeRejection, "deterministic rejection classification")
	}
	if input.FollowUpDueAt != nil {
		key := deterministicKey(input, TypeFollowUp)
		result = append(result, WorkItem{ID: "communication-work-" + key[:20], Key: key, Type: TypeFollowUp, Status: StatusPending,
			ConversationID: input.ConversationID, ApplicationID: input.ApplicationID, VacancyID: input.VacancyID,
			Evidence: []string{"existing follow-up policy marked this item due"}, DueAt: input.FollowUpDueAt,
			CreatedAt: now, UpdatedAt: now})
	}
	return result
}

func deterministicKey(input Input, kind Type) string {
	raw := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%s", input.ConversationID, input.ApplicationID, input.VacancyID, input.MessageID, kind, input.MessageText)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func explicitDateAndTime(text string) (string, string) {
	date := ""
	if match := datePattern.FindStringSubmatch(text); len(match) > 0 {
		date = match[0]
	}
	timeValue := ""
	if match := timePattern.FindStringSubmatch(text); len(match) > 0 {
		timeValue = match[0]
	}
	return date, timeValue
}

func firstLink(text string) string {
	return strings.TrimRight(linkPattern.FindString(text), ".,;)")
}

func explicitDeadline(text string) string {
	match := deadlinePattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
