package hhread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/vacancy"
)

type rawChatList struct {
	NextFrom string    `json:"nextFrom"`
	Page     int       `json:"page"`
	PerPage  int       `json:"per_page"`
	Pages    int       `json:"pages"`
	Items    []rawChat `json:"items"`
}
type rawChatListResponse struct {
	Chats     rawChatList      `json:"chats"`
	Resources rawChatResources `json:"resources"`
}
type rawChat struct {
	ID                   int64       `json:"id"`
	Resources            rawChatRefs `json:"resources"`
	CurrentParticipantID string      `json:"currentParticipantId"`
	LastMessage          *rawMessage `json:"lastMessage,omitempty"`
	LastActivityTime     time.Time   `json:"lastActivityTime"`
}
type rawChatRefs struct {
	Vacancy          []string `json:"VACANCY"`
	NegotiationTopic []string `json:"NEGOTIATION_TOPIC"`
	Resume           []string `json:"RESUME"`
}
type rawChatResources struct {
	Vacancies         map[string]rawChatVacancy  `json:"vacancies"`
	NegotiationTopics map[string]rawTopic        `json:"negotiation_topics"`
	Participants      map[string]rawParticipant  `json:"participants"`
	Resumes           map[string]json.RawMessage `json:"resumes"`
}
type rawChatVacancy struct {
	VacancyID              int64            `json:"vacancyId"`
	Name                   string           `json:"name"`
	Company                rawCompany       `json:"company"`
	Links                  rawLinks         `json:"links"`
	Description            string           `json:"description"`
	WorkSchedule           string           `json:"@workSchedule"`
	WorkExperience         string           `json:"workExperience"`
	ResponseLetterRequired bool             `json:"@responseLetterRequired"`
	UserTestPresent        bool             `json:"userTestPresent"`
	Archived               rawChatArchived  `json:"archived"`
	Compensation           *rawCompensation `json:"compensation,omitempty"`
}

// rawChatArchived represents the provider's historical and current wire
// shapes without projecting the current resource metadata object into the
// normalized vacancy/conversation model. Conversation mapping does not use
// this field: in particular, the current {"@hidden": bool} object is not an
// authoritative archived status.
type rawChatArchived struct {
	kind      rawChatArchivedKind
	boolValue *bool
	hidden    bool
}

type rawChatArchivedKind uint8

const (
	rawChatArchivedMissing rawChatArchivedKind = iota
	rawChatArchivedNull
	rawChatArchivedBool
	rawChatArchivedObject
)

type providerFieldDecodeError struct {
	field  string
	reason string
}

func (e *providerFieldDecodeError) Error() string {
	return fmt.Sprintf("HH provider field %s: %s", e.field, e.reason)
}

func (value *rawChatArchived) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if !json.Valid(data) {
		return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "invalid JSON"}
	}
	if bytes.Equal(data, []byte("null")) {
		value.kind = rawChatArchivedNull
		value.boolValue = nil
		value.hidden = false
		return nil
	}
	switch data[0] {
	case 't', 'f':
		var parsed bool
		if err := json.Unmarshal(data, &parsed); err != nil {
			return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "invalid boolean"}
		}
		value.kind = rawChatArchivedBool
		value.boolValue = &parsed
		value.hidden = false
		return nil
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "invalid object"}
		}
		hidden, ok := object["@hidden"]
		if !ok || len(object) != 1 {
			return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "unsupported object shape"}
		}
		var parsedHidden bool
		if err := json.Unmarshal(hidden, &parsedHidden); err != nil {
			return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "@hidden must be boolean"}
		}
		value.kind = rawChatArchivedObject
		value.boolValue = nil
		value.hidden = parsedHidden
		return nil
	default:
		return &providerFieldDecodeError{field: "resources.vacancies.archived", reason: "unsupported JSON shape"}
	}
}

type rawCompany struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	SiteURL string `json:"companySiteUrl"`
}
type rawLinks struct {
	Desktop string `json:"desktop"`
	Mobile  string `json:"mobile"`
}
type rawCompensation struct {
	From     *int   `json:"from,omitempty"`
	To       *int   `json:"to,omitempty"`
	Currency string `json:"currencyCode,omitempty"`
}
type rawTopic struct {
	CurrentApplicantState string `json:"currentApplicantState"`
}
type rawMessageList struct {
	Items   []rawMessage `json:"items"`
	HasMore bool         `json:"hasMore"`
}
type rawChatDetail struct {
	ID                   int64          `json:"id"`
	Resources            rawChatRefs    `json:"resources"`
	Messages             rawMessageList `json:"messages"`
	CurrentParticipantID string         `json:"currentParticipantId"`
	LastActivityTime     time.Time      `json:"lastActivityTime"`
	WritePossibility     struct {
		Allowed bool `json:"allowed"`
	} `json:"writePossibility"`
}
type rawChatData struct {
	Chat      rawChatDetail    `json:"chat"`
	Resources rawChatResources `json:"resources"`
}
type rawMessage struct {
	ID                 int64     `json:"id"`
	CreationTime       time.Time `json:"creationTime"`
	Text               string    `json:"text"`
	Type               string    `json:"type"`
	HasContent         bool      `json:"hasContent"`
	ParticipantID      string    `json:"participantId"`
	ParticipantDisplay struct {
		Name string `json:"name"`
	} `json:"participantDisplay"`
	WorkflowTransition *rawWorkflow `json:"workflowTransition"`
	Actions            *rawActions  `json:"actions,omitempty"`
}
type rawWorkflow struct {
	ApplicantState string `json:"applicantState"`
}
type rawActions struct {
	TextButtons []rawButton `json:"text_buttons"`
}
type rawButton struct {
	Size string `json:"size"`
	Text string `json:"text"`
}
type rawParticipant struct {
	Type string `json:"type"`
}

func parseChatList(data []byte) (rawChatListResponse, error) {
	var value rawChatListResponse
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("invalid HH chats response: %w", err)
	}
	if value.Chats.Items == nil {
		return value, errors.New("invalid HH chats response")
	}
	return value, nil
}

func parseChatData(data []byte, id int64) (rawChatData, error) {
	var value rawChatData
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("invalid HH chat detail response: %w", err)
	}
	if value.Chat.ID != id || value.Chat.Messages.Items == nil {
		return value, errors.New("invalid HH chat detail identity or messages")
	}
	return value, nil
}

func (c *Client) readChatList(ctx context.Context, cursor string) (rawChatListResponse, error) {
	query := url.Values{"filterUnread": {"false"}, "filterHasTextMessage": {"false"}, "do_not_track_session_events": {"true"}}
	if cursor != "" {
		query.Set("from", cursor)
	}
	body, err := c.get(ctx, c.chatURL, "/chatik/api/chats", query, map[string]string{"Accept": "application/json"})
	if err != nil {
		return rawChatListResponse{}, err
	}
	return parseChatList(body)
}

func (c *Client) readChatData(ctx context.Context, id int64) (rawChatData, error) {
	return c.readChatDataForUser(ctx, id, c.userID)
}

func (c *Client) readChatDataForUser(ctx context.Context, id, userID int64) (rawChatData, error) {
	query := url.Values{"chatId": {strconv.FormatInt(id, 10)}, "applicantId": {strconv.FormatInt(c.userID, 10)}, "do_not_track_session_events": {"true"}}
	query.Set("applicantId", strconv.FormatInt(userID, 10))
	body, err := c.get(ctx, c.chatURL, "/chatik/api/chat_data", query, map[string]string{"Accept": "application/json"})
	if err != nil {
		return rawChatData{}, err
	}
	return parseChatData(body, id)
}

// ReadChatList is the neutral list projection retained for the legacy
// automatic-chat reader. It is still a GET-only capability.
func (c *Client) ReadChatList(ctx context.Context, cursor string) (hhread.ChatListPage, error) {
	if err := c.validate(); err != nil {
		return hhread.ChatListPage{}, err
	}
	response, err := c.readChatList(ctx, cursor)
	if err != nil {
		return hhread.ChatListPage{}, err
	}
	page := hhread.ChatListPage{
		NextFrom: response.Chats.NextFrom, Page: response.Chats.Page, PerPage: response.Chats.PerPage,
		Pages: response.Chats.Pages, Items: make([]hhread.ChatSummary, 0, len(response.Chats.Items)),
		Vacancies: map[string]hhread.ChatVacancySummary{}, ResumeIDs: map[string]struct{}{},
	}
	for id := range response.Resources.Resumes {
		page.ResumeIDs[id] = struct{}{}
	}
	for id, value := range response.Resources.Vacancies {
		page.Vacancies[id] = hhread.ChatVacancySummary{
			ID: value.VacancyID, Name: value.Name, Company: value.Company.Name, URL: value.Links.Desktop,
			SalaryFrom: cloneInt(value.Compensation, true), SalaryTo: cloneInt(value.Compensation, false), SalaryCurrency: compensationCurrency(value.Compensation),
		}
	}
	for _, value := range response.Chats.Items {
		summary := hhread.ChatSummary{ID: value.ID, VacancyIDs: append([]string(nil), value.Resources.Vacancy...), ResumeIDs: append([]string(nil), value.Resources.Resume...), LastActivityTime: value.LastActivityTime}
		if value.LastMessage != nil {
			summary.LastMessage = mapRawMessage(*value.LastMessage)
		}
		page.Items = append(page.Items, summary)
	}
	return page, nil
}

// ReadChatHistory is the neutral history projection retained by the legacy
// chat responder. It only reports whether HH allows writing; it cannot write.
func (c *Client) ReadChatHistory(ctx context.Context, chatID, applicantID int64) (hhread.ChatHistory, error) {
	if err := c.validate(); err != nil {
		return hhread.ChatHistory{}, err
	}
	data, err := c.readChatDataForUser(ctx, chatID, applicantID)
	if err != nil {
		return hhread.ChatHistory{}, err
	}
	history := hhread.ChatHistory{ID: data.Chat.ID, WriteAllowed: data.Chat.WritePossibility.Allowed, Messages: make([]hhread.MessageRecord, 0, len(data.Chat.Messages.Items))}
	for _, value := range data.Chat.Messages.Items {
		history.Messages = append(history.Messages, *mapRawMessage(value))
	}
	return history, nil
}

func mapRawMessage(value rawMessage) *hhread.MessageRecord {
	result := &hhread.MessageRecord{ExternalID: "", Text: value.Text, Timestamp: value.CreationTime, Sender: "unknown", ParticipantID: value.ParticipantID, ParticipantName: value.ParticipantDisplay.Name}
	if value.ID > 0 {
		result.ExternalID = strconv.FormatInt(value.ID, 10)
	}
	if value.WorkflowTransition != nil {
		result.WorkflowApplicantState = value.WorkflowTransition.ApplicantState
	}
	if value.Actions != nil {
		for _, button := range value.Actions.TextButtons {
			result.Actions = append(result.Actions, hhread.Action{Kind: "text_button", Label: button.Text, Metadata: map[string]string{"size": button.Size}})
		}
	}
	return result
}

func cloneInt(value *rawCompensation, from bool) *int {
	if value == nil {
		return nil
	}
	source := value.To
	if from {
		source = value.From
	}
	if source == nil {
		return nil
	}
	result := *source
	return &result
}

func compensationCurrency(value *rawCompensation) string {
	if value == nil {
		return ""
	}
	return value.Currency
}

func vacancyRecord(value vacancy.Vacancy, description string) hhread.VacancyRecord {
	return hhread.VacancyRecord{
		ExternalID: value.ExternalID, ID: value.ID, Title: firstNonEmpty(value.Title, value.Name), Company: value.Company.Name,
		Description: description, Requirements: append([]string(nil), value.Requirements...), KeySkills: append([]string(nil), value.Skills...),
		Salary: value.Salary, Currency: firstNonEmpty(value.SalaryCurrency, value.Compensation.Currency), Location: firstNonEmpty(value.Location, value.Area.Name),
		WorkFormat: value.WorkFormat, Experience: value.WorkExperience, EmploymentType: value.EmploymentType, Schedule: value.WorkSchedule,
		URL: value.Links["desktop"], PublishedAt: value.PublishedAt, UpdatedAt: value.HHUpdatedAt,
		TotalResponsesCount: value.TotalResponsesCount, TotalResponsesCountKnown: value.TotalResponsesCountKnown, Archived: value.Archived,
		ResponseLetterRequired: value.ResponseLetterRequired, UserTestPresent: value.UserTestPresent, ResponseURL: value.ResponseURL,
		Metadata: cloneStringMap(value.HHMetadata),
	}
}

func mapConversation(chat rawChat, data rawChatData, list rawChatListResponse) hhread.ConversationRecord {
	record := hhread.ConversationRecord{ExternalID: strconv.FormatInt(chat.ID, 10), UpdatedAt: data.Chat.LastActivityTime, Metadata: map[string]string{}, Messages: []hhread.MessageRecord{}}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = chat.LastActivityTime
	}
	if data.Chat.Messages.HasMore {
		record.Metadata["history_incomplete"] = "true"
	}
	if len(chat.Resources.Vacancy) > 0 {
		record.VacancyExternalID = chat.Resources.Vacancy[0]
	}
	if value, ok := data.Resources.Vacancies[record.VacancyExternalID]; ok {
		applyChatVacancy(&record, value)
	}
	if value, ok := list.Resources.Vacancies[record.VacancyExternalID]; ok {
		applyChatVacancy(&record, value)
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
	participants := data.Resources.Participants
	for _, message := range data.Chat.Messages.Items {
		sender := ""
		switch message.Type {
		case "PARTICIPANT_JOINED", "PARTICIPANT_LEFT", "WORKFLOW_TRANSITION":
			sender = "system"
		case "SIMPLE":
			if message.ParticipantID != "" && current != "" && message.ParticipantID == current {
				sender = "candidate"
			} else if participant, ok := participants[message.ParticipantID]; ok && participant.Type == "EMPLOYER_USER" {
				sender = "employer"
			}
		}
		systemEvent := message.WorkflowTransition != nil && !message.HasContent && strings.TrimSpace(message.Text) == ""
		if systemEvent {
			sender = "system"
		}
		if sender == "" {
			record.Metadata["warning_unknown_message_sender_or_type"] = "true"
			sender = "unknown"
		}
		unavailable := strings.TrimSpace(message.Text) == "" && sender != "system"
		if unavailable {
			record.Metadata["warning_message_content_unavailable"] = "true"
		}
		direction := ""
		switch sender {
		case "candidate":
			direction = "outgoing"
		case "employer":
			direction = "incoming"
		}
		mapped := hhread.MessageRecord{SystemEvent: systemEvent, ContentUnavailable: unavailable, ExternalID: strconv.FormatInt(message.ID, 10), Sender: sender, Direction: direction, Text: message.Text, Timestamp: message.CreationTime}
		if message.Actions != nil {
			for _, button := range message.Actions.TextButtons {
				mapped.Actions = append(mapped.Actions, hhread.Action{Kind: "text_button", Label: button.Text, Metadata: map[string]string{"size": button.Size}})
			}
		}
		record.Messages = append(record.Messages, mapped)
		if record.CreatedAt.IsZero() || message.CreationTime.Before(record.CreatedAt) {
			record.CreatedAt = message.CreationTime
		}
	}
	if chat.LastMessage != nil {
		found := false
		for _, message := range record.Messages {
			if message.ExternalID == strconv.FormatInt(chat.LastMessage.ID, 10) {
				found = true
				break
			}
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

func applyChatVacancy(record *hhread.ConversationRecord, value rawChatVacancy) {
	record.VacancyID = int(value.VacancyID)
	record.Company = value.Company.Name
	record.VacancyTitle = value.Name
	record.VacancyDescription = value.Description
}
func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseNegotiations(data []byte) ([]hhread.ApplicationRecord, string, bool, error) {
	raw := []byte(html.UnescapeString(string(data)))
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		i := bytes.Index(raw, []byte(`{"redirectConfig":`))
		if i < 0 {
			return nil, "", false, nil
		}
		if err := json.NewDecoder(bytes.NewReader(raw[i:])).Decode(&root); err != nil {
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
		Values []vacancy.Vacancy `json:"vacanciesList"`
	}
	_ = json.Unmarshal(root["vacanciesShort"], &vacancies)
	byID := map[int]vacancy.Vacancy{}
	for _, value := range vacancies.Values {
		byID[value.ID] = value
	}
	result := make([]hhread.ApplicationRecord, 0, len(page.Topics))
	for _, value := range page.Topics {
		id := stringValue(value, "id")
		vacancyID := intValue(value, "vacancyId")
		if id == "" || vacancyID <= 0 {
			return nil, "", true, errors.New("invalid HH negotiation identity")
		}
		linked, found := byID[vacancyID]
		metadata := map[string]string{"hh_id": id}
		if stringValue(value, "initialTopicType") == "RESPONSE_BY_APPLICANT" {
			metadata["delivery_confirmed"] = "true"
		}
		if archived, ok := value["archived"].(bool); ok && archived {
			metadata["warning_archived"] = "true"
		}
		if pending, ok := value["applicantQuestionState"].(bool); ok && pending {
			metadata["warning_candidate_action"] = "true"
		}
		var linkedPtr *vacancy.Vacancy
		if found {
			copy := linked
			linkedPtr = &copy
		}
		result = append(result, hhread.ApplicationRecord{Vacancy: linkedPtr, ExternalID: id, VacancyID: vacancyID, Company: linked.Company.Name, VacancyTitle: linked.Name, VacancyURL: linked.Links["desktop"], Status: stringValue(value, "lastState"), CreatedAt: timeValue(value, "creationTime"), UpdatedAt: timeValue(value, "lastModified"), ConversationExternal: stringValue(value, "chatId"), Metadata: metadata})
	}
	next := ""
	if !page.Paging.Next.Disabled && len(result) > 0 {
		next = strconv.Itoa(page.Paging.Next.Page)
	}
	return result, next, true, nil
}

func parseNormalizedApplications(data []byte) ([]hhread.ApplicationRecord, error) {
	var root struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(data, &root) != nil || root.Items == nil {
		return nil, errors.New("unrecognized HH negotiation response; no records imported")
	}
	result := make([]hhread.ApplicationRecord, 0, len(root.Items))
	seen := map[string]bool{}
	for _, value := range root.Items {
		id := stringValue(value, "id", "negotiationId", "responseId", "externalId", "external_id")
		vacancyID := intValue(value, "vacancyId", "vacancy_id")
		if id == "" || vacancyID <= 0 || seen[id] {
			return nil, errors.New("invalid or duplicate HH negotiation identity")
		}
		seen[id] = true
		result = append(result, hhread.ApplicationRecord{ExternalID: id, VacancyID: vacancyID, Company: stringValue(value, "companyName", "employerName"), VacancyTitle: stringValue(value, "vacancyTitle", "title"), Status: stringValue(value, "status", "state", "applicantState"), CreatedAt: timeValue(value, "createdAt", "creationTime", "created_at"), UpdatedAt: timeValue(value, "updatedAt", "lastChangeTime", "updated_at"), ConversationExternal: stringValue(value, "chatId", "conversationId")})
	}
	return result, nil
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if item, ok := value[key]; ok {
			switch result := item.(type) {
			case string:
				return strings.TrimSpace(result)
			case json.Number:
				return result.String()
			case float64:
				return strconv.FormatInt(int64(result), 10)
			}
		}
	}
	return ""
}
func intValue(value map[string]any, keys ...string) int {
	return int(parseInt64(stringValue(value, keys...)))
}
func parseInt64(value string) int64 {
	result, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return result
}
func timeValue(value map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		if text, ok := value[key].(string); ok {
			if result, err := time.Parse(time.RFC3339, text); err == nil {
				return result
			}
		}
	}
	return time.Time{}
}

func parseVacancies(data []byte, baseURL *url.URL) ([]vacancy.Vacancy, error) {
	var values []vacancy.Vacancy
	if err := decodeEmbedded(data, `,"vacancies":`, &values); err != nil {
		if values, htmlErr := parseVacancySearchHTML(data, baseURL); htmlErr == nil {
			return values, nil
		}
		// Search responses in tests and older HH pages use the embedded marker;
		// accepting a direct list keeps decoding tolerant of provider wrappers.
		var root struct {
			Vacancies []vacancy.Vacancy `json:"vacancies"`
		}
		if json.Unmarshal(data, &root) != nil || root.Vacancies == nil {
			return nil, fmt.Errorf("unable to parse HH vacancy search response: %w", err)
		}
		values = root.Vacancies
	}
	for i := range values {
		if values[i].ID == 0 {
			return nil, errors.New("HH vacancy has no id")
		}
		if values[i].Links == nil {
			values[i].Links = map[string]string{}
		}
		if values[i].Links["desktop"] == "" && baseURL != nil {
			values[i].Links["desktop"] = baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("/vacancy/%d", values[i].ID)}).String()
		}
	}
	return values, nil
}
