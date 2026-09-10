package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/hhreadsync"
)

// Parse only the observed negotiation container; unrelated page IDs are never applications.
func parseHHNegotiations(data []byte) ([]HHApplicationRecord, string, bool, error) {
	raw := []byte(html.UnescapeString(string(data)))
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		i := bytes.Index(raw, []byte(`{"redirectConfig":`))
		if i < 0 {
			return nil, "", false, nil
		}
		if json.NewDecoder(bytes.NewReader(raw[i:])).Decode(&root) != nil {
			return nil, "", true, errors.New("invalid HH page state")
		}
	}
	container, ok := root["applicantNegotiations"]
	if !ok {
		return nil, "", false, nil
	}
	var page struct {
		Topics []map[string]any `json:"topicList"`
		Paging struct {
			Next struct {
				Page     int  `json:"page"`
				Disabled bool `json:"disabled"`
			} `json:"next"`
		} `json:"paging"`
	}
	if json.Unmarshal(container, &page) != nil || page.Topics == nil {
		return nil, "", true, errors.New("invalid HH negotiation list")
	}
	var vacancies struct {
		Values []Vacancy `json:"vacanciesList"`
	}
	_ = json.Unmarshal(root["vacanciesShort"], &vacancies)
	byID := map[int]Vacancy{}
	for _, v := range vacancies.Values {
		byID[v.ID] = v
	}
	result := []HHApplicationRecord{}
	for _, v := range page.Topics {
		id := stringValue(v, "id")
		vid := intValue(v, "vacancyId")
		if id == "" || vid <= 0 {
			return nil, "", true, errors.New("invalid HH negotiation identity")
		}
		vacancy := byID[vid]
		metadata := map[string]string{"hh_id": id}
		if stringValue(v, "initialTopicType") == "RESPONSE_BY_APPLICANT" {
			metadata["delivery_confirmed"] = "true"
		}
		var linkedVacancy *Vacancy
		if vacancy.ID == vid {
			linkedVacancy = &vacancy
		}
		if archived, ok := v["archived"].(bool); ok && archived {
			metadata["warning_archived"] = "true"
		}
		if pending, ok := v["applicantQuestionState"].(bool); ok && pending {
			metadata["warning_candidate_action"] = "true"
		}
		result = append(result, HHApplicationRecord{Vacancy: linkedVacancy, ExternalID: id, VacancyID: vid, Company: vacancy.Company.Name, VacancyTitle: vacancy.Name, VacancyURL: vacancy.Links["desktop"], Status: stringValue(v, "lastState"), CreatedAt: timeValue(v, "creationTime"), UpdatedAt: timeValue(v, "lastModified"), ConversationExternal: stringValue(v, "chatId"), Metadata: metadata})
	}
	next := ""
	if !page.Paging.Next.Disabled && len(result) > 0 {
		next = strconv.Itoa(page.Paging.Next.Page)
	}
	return result, next, true, nil
}

func (c *HHAIResponderReadClient) readChats(cursor string) (*ChatsResponse, error) {
	return c.readChatsContext(context.Background(), cursor)
}
func (c *HHAIResponderReadClient) readChatsContext(ctx context.Context, cursor string) (*ChatsResponse, error) {
	start := time.Now()
	defer perfRecord("hh.chat_list", start, 1)
	adapter, err := c.readAdapter()
	if err != nil {
		return nil, err
	}
	page, err := adapter.ReadChatList(ctx, cursor)
	if err != nil {
		return nil, err
	}
	return legacyChatsResponse(page), nil
}
func hhChatRecord(chat ChatListItem, data *ChatDataResponse, list *ChatsResponse) HHConversationRecord {
	record := HHConversationRecord{ExternalID: strconv.FormatInt(chat.Id, 10), UpdatedAt: data.Chat.LastActivityTime, Metadata: map[string]string{}, Messages: []HHMessageRecord{}}
	if data.Chat.Messages.HasMore {
		record.Metadata["history_incomplete"] = "true"
	}
	if len(chat.Resources.Vacancy) > 0 {
		record.VacancyExternalID = chat.Resources.Vacancy[0]
	}
	if vacancy, ok := data.Resources.Vacancies[record.VacancyExternalID]; ok {
		record.VacancyID = int(vacancy.VacancyID)
		record.Company = vacancy.Company.Name
		record.VacancyTitle = vacancy.Name
	}
	if vacancy, ok := list.Resources.Vacancies[record.VacancyExternalID]; ok {
		record.VacancyID = int(vacancy.VacancyID)
		record.Company = vacancy.Company.Name
		record.VacancyTitle = vacancy.Name
	}
	for _, id := range chat.Resources.NegotiationTopic {
		record.Metadata["topic_id"] = id
		if topic, ok := data.Resources.NegotiationTopics[id]; ok {
			if record.Status != "" && record.Status != topic.CurrentApplicantState {
				record.Metadata["warning_conflicting_topics"] = "true"
			}
			record.Status = topic.CurrentApplicantState
		}
	}
	current := data.Chat.CurrentParticipantID
	if current == "" {
		current = chat.CurrentParticipantID
	}
	for _, m := range data.Chat.Messages.Items {
		sender := ""
		switch m.Type {
		case "PARTICIPANT_JOINED", "PARTICIPANT_LEFT", "WORKFLOW_TRANSITION":
			sender = "system"
		case "SIMPLE":
			if m.ParticipantID != "" && current != "" && m.ParticipantID == current {
				sender = "candidate"
			} else if p, ok := data.Resources.Participants[m.ParticipantID]; ok && p.Type == "EMPLOYER_USER" {
				sender = "employer"
			}
		}
		systemEvent := m.WorkflowTransition != nil && !m.HasContent && strings.TrimSpace(m.Text) == ""
		if systemEvent {
			sender = "system"
		}
		if sender == "" {
			record.Metadata["warning_unknown_message_sender_or_type"] = "true"
			sender = "unknown"
		}
		unavailable := strings.TrimSpace(m.Text) == "" && sender != "system"
		if unavailable {
			record.Metadata["warning_message_content_unavailable"] = "true"
		}
		record.Messages = append(record.Messages, HHMessageRecord{SystemEvent: systemEvent, ContentUnavailable: unavailable, ExternalID: strconv.FormatInt(m.ID, 10), Sender: sender, Text: m.Text, Timestamp: m.CreationTime})
		if record.CreatedAt.IsZero() || m.CreationTime.Before(record.CreatedAt) {
			record.CreatedAt = m.CreationTime
		}
	}
	if chat.LastMessage != nil {
		found := false
		for _, m := range record.Messages {
			found = found || m.ExternalID == strconv.FormatInt(chat.LastMessage.ID, 10)
		}
		if !found {
			record.Metadata["warning_missing_latest_message"] = "true"
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = record.UpdatedAt
	}
	return record
}

// Merge by HH identity while retaining the original local evidence and annotations.
// Changed upstream content/direction is a visible warning, not a silent rewrite.
func mergeHHConversation(old EmployerConversation, incoming *EmployerConversation) {
	hhreadsync.MergeConversation(old, incoming)
}

func knownApplicationTime(a JobApplication, events []ApplicationEvent) *time.Time {
	var result *time.Time
	for _, e := range events {
		if e.ApplicationID == a.ID && e.Type == ApplicationEventApplied && !e.Timestamp.IsZero() {
			t := e.Timestamp
			if result == nil || t.Before(*result) {
				result = &t
			}
		}
	}
	if result == nil && a.HHMetadata["applied_at"] != "" {
		if t, err := time.Parse(time.RFC3339Nano, a.HHMetadata["applied_at"]); err == nil {
			result = &t
		}
	}
	return result
}
