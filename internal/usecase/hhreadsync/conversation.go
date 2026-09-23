package hhreadsync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/vacancy"
)

func (s *Service) importConversation(ctx context.Context, record hhread.ConversationRecord) (outcome, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" {
		return skipped, errors.New("HH conversation has no external id")
	}
	vacancyID := record.VacancyID
	if vacancyID == 0 && s.deps.Vacancies != nil && strings.TrimSpace(record.VacancyExternalID) != "" {
		value, err := s.deps.Vacancies.GetByExternalID(ctx, record.VacancyExternalID)
		if err == nil {
			vacancyID = value.ID
		} else if !errors.Is(err, vacancy.ErrVacancyNotFound) {
			// A conversation can arrive before its independent vacancy stream;
			// preserve the historical unresolved relation in that case.
		}
	}
	messages, warnings := MapMessages(record.Messages)
	old, oldErr := s.deps.Conversations.GetByHHConversationID(ctx, externalID)
	if oldErr != nil && !errors.Is(oldErr, conversation.ErrConversationNotFound) {
		return skipped, oldErr
	}
	value := conversation.EmployerConversation{VacancyID: vacancyID, HHConversationID: externalID, CompanyName: record.Company,
		VacancyTitle: record.VacancyTitle, VacancyDescription: record.VacancyDescription, Status: importedConversationStatus(record.Status, messages),
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, HHUpdatedAt: record.UpdatedAt, Messages: messages, RawStatus: record.Status,
		HHMetadata: copyStringMap(record.Metadata)}
	if value.HHMetadata == nil {
		value.HHMetadata = map[string]string{}
	}
	if len(warnings) > 0 {
		value.HHMetadata["warning_skipped_messages"] = "true"
	}
	if oldErr == nil {
		MergeConversation(old, &value)
	}

	applications, err := s.listApplications(ctx)
	if err != nil {
		return skipped, err
	}
	matchedApplication := application.JobApplication{}
	exactApplications := exactApplicationMatches(value, applications)
	if len(exactApplications) == 1 {
		matchedApplication = exactApplications[0]
		value.ApplicationID = matchedApplication.ID
	}
	if len(exactApplications) > 1 {
		value.HHMetadata["warning_conflicting_application_relation"] = "true"
	}
	var appliedAt *time.Time
	if matchedApplication.ID != "" {
		if events, timelineErr := s.deps.Applications.Timeline(ctx, matchedApplication.ID); timelineErr == nil {
			appliedAt = applicationTime(matchedApplication, events)
		}
	}
	pending := false
	if s.deps.PendingState != nil {
		pending, err = s.deps.PendingState(value.ID, matchedApplication.ID)
		if err != nil {
			return skipped, err
		}
	}
	if s.deps.ResolveState != nil {
		// The legacy resolver receives no parser warnings; the warning is still
		// retained in provider metadata for observability without changing the
		// established conversation-state decision.
		resolved := s.deps.ResolveState(matchedApplication, value, appliedAt, pending, nil, time.Now().UTC())
		value.Status, value.WaitingSince = resolved.Status, resolved.WaitingSince
	} else {
		value.Status, value.WaitingSince = importedConversationStatus(record.Status, messages), nil
	}
	value.RefreshActivity()
	if oldErr != nil || !ConversationEquivalent(old, value) {
		if _, err := s.deps.Conversations.Upsert(ctx, value); err != nil {
			return skipped, err
		}
	}
	saved, err := s.deps.Conversations.GetByHHConversationID(ctx, externalID)
	if err != nil {
		return skipped, err
	}
	if err := s.linkApplications(ctx, saved, applications); err != nil {
		return skipped, err
	}
	if oldErr != nil {
		return created, nil
	}
	if ConversationEquivalent(old, saved) {
		return unchanged, nil
	}
	return updated, nil
}

func (s *Service) listApplications(ctx context.Context) ([]application.JobApplication, error) {
	if s.deps.Applications == nil {
		return []application.JobApplication{}, nil
	}
	return s.deps.Applications.List(ctx)
}

func (s *Service) linkApplications(ctx context.Context, saved conversation.EmployerConversation, applications []application.JobApplication) error {
	exactApplications := exactApplicationMatches(saved, applications)
	if len(exactApplications) != 1 {
		if saved.HHMetadata == nil {
			saved.HHMetadata = map[string]string{}
		}
		if len(exactApplications) > 1 {
			saved.HHMetadata["warning_conflicting_application_relation"] = "true"
		} else {
			saved.HHMetadata["warning_unresolved_application_relation"] = "true"
		}
		_, err := s.deps.Conversations.Upsert(ctx, saved)
		return err
	}
	for _, candidate := range exactApplications {
		if saved.Status == conversation.StatusCandidateActionRequired && candidate.Status == application.StatusApplied {
			if err := s.deps.Applications.UpdateStatus(ctx, candidate.ID, application.StatusEmployerReplied); err != nil {
				return err
			}
		}
		if candidate.ConversationID == "" {
			if err := s.deps.Applications.AttachConversation(ctx, candidate.ID, saved.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func exactApplicationMatches(value conversation.EmployerConversation, applications []application.JobApplication) []application.JobApplication {
	result := make([]application.JobApplication, 0, 2)
	for _, candidate := range applications {
		if strings.TrimSpace(value.ApplicationID) != "" && candidate.ID == value.ApplicationID ||
			strings.TrimSpace(candidate.ConversationID) != "" && candidate.ConversationID == value.ID ||
			strings.TrimSpace(value.HHConversationID) != "" && candidate.HHMetadata["conversation_external_id"] == value.HHConversationID {
			result = append(result, candidate)
		}
	}
	return result
}

// MapMessages performs the established protocol-to-domain mapping. Sender
// direction is trusted from the normalized HH record and is never inferred
// from message text.
func MapMessages(records []hhread.MessageRecord) ([]conversation.Message, []string) {
	indexed := append([]hhread.MessageRecord{}, records...)
	sort.SliceStable(indexed, func(i, j int) bool { return indexed[i].Timestamp.Before(indexed[j].Timestamp) })
	result, warnings := []conversation.Message{}, []string{}
	for _, record := range indexed {
		sender, direction := MapMessageSender(record)
		if sender == "" || record.Timestamp.IsZero() || (strings.TrimSpace(record.Text) == "" && sender != conversation.SenderSystem && sender != conversation.SenderUnknown && !record.ContentUnavailable) {
			warnings = append(warnings, "malformed HH message skipped")
			continue
		}
		externalID := strings.TrimSpace(record.ExternalID)
		if externalID == "" {
			externalID = fmt.Sprintf("%d:%x", record.Timestamp.UnixNano(), hashText(record.Text))
		}
		result = append(result, conversation.Message{HHSystemEvent: record.SystemEvent, ContentUnavailable: record.ContentUnavailable, Metadata: optionalStringMap(record.Metadata),
			ID: "hh-message-" + externalID, ExternalID: externalID, Timestamp: record.Timestamp, Sender: sender, Text: record.Text, Source: conversation.SourceHH, Direction: direction})
	}
	return result, warnings
}

func optionalStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	return copyStringMap(value)
}

func MapMessageSender(record hhread.MessageRecord) (conversation.Sender, conversation.Direction) {
	sender := strings.ToLower(strings.TrimSpace(record.Sender))
	if sender == "unknown" {
		return conversation.SenderUnknown, conversation.DirectionUnknown
	}
	if sender == "system" {
		return conversation.SenderSystem, conversation.DirectionIncoming
	}
	direction := strings.ToLower(strings.TrimSpace(record.Direction))
	if sender == "candidate" || sender == "applicant" || sender == "me" || sender == "current_user" {
		return conversation.SenderCandidate, conversation.DirectionOutgoing
	}
	if sender == "employer" || sender == "manager" || sender == "recruiter" || sender == "opponent" {
		return conversation.SenderEmployer, conversation.DirectionIncoming
	}
	if direction == "incoming" {
		return conversation.SenderEmployer, conversation.DirectionIncoming
	}
	if direction == "outgoing" {
		return conversation.SenderCandidate, conversation.DirectionOutgoing
	}
	return "", ""
}

func MergeConversation(old conversation.EmployerConversation, incoming *conversation.EmployerConversation) {
	if incoming.VacancyID == 0 {
		incoming.VacancyID = old.VacancyID
	}
	if incoming.VacancyTitle == "" {
		incoming.VacancyTitle = old.VacancyTitle
	}
	if incoming.CompanyName == "" {
		incoming.CompanyName = old.CompanyName
	}
	if incoming.VacancyDescription == "" {
		incoming.VacancyDescription = old.VacancyDescription
	}
	incoming.ID, incoming.CreatedAt = old.ID, old.CreatedAt
	if incoming.ApplicationID == "" {
		incoming.ApplicationID = old.ApplicationID
	}
	incoming.Summary, incoming.FollowUpState, incoming.NextAction = old.Summary, old.FollowUpState, old.NextAction
	merged := append([]conversation.Message{}, old.Messages...)
	for _, message := range incoming.Messages {
		found := false
		for i, prior := range old.Messages {
			if message.Source == prior.Source && message.ExternalID != "" && message.ExternalID == prior.ExternalID {
				found = true
				if message.HHSystemEvent && prior.Text == "" && message.Timestamp.Equal(prior.Timestamp) {
					merged[i].HHSystemEvent = true
				} else if !conversation.SameMessage(message, prior) {
					incoming.HHMetadata["warning_message_content_conflict"] = "true"
				}
				break
			}
		}
		if !found {
			merged = append(merged, message)
		}
	}
	incoming.Messages = merged
	for key, value := range old.HHMetadata {
		if (key == "warning_message_content_conflict" || key == "sync_list_fingerprint") && incoming.HHMetadata[key] == "" {
			incoming.HHMetadata[key] = value
		}
	}
}

func ConversationEquivalent(a, b conversation.EmployerConversation) bool {
	left, right := a, b
	if !stringMapsEqual(left.HHMetadata, right.HHMetadata) {
		return false
	}
	left.HHMetadata, right.HHMetadata = nil, nil
	left.ID, right.ID = "", ""
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func importedConversationStatus(raw string, messages []conversation.Message) conversation.Status {
	if latest := latestMeaningful(messages); latest != nil {
		if latest.Sender == conversation.SenderEmployer {
			return conversation.StatusEmployerReplied
		}
		return conversation.StatusWaitingEmployer
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rejected", "declined", "discarded":
		return conversation.StatusRejected
	case "offer", "hired":
		return conversation.StatusOffer
	case "interview", "invited":
		return conversation.StatusInterview
	case "closed", "cancelled", "canceled", "archived":
		return conversation.StatusClosed
	default:
		return conversation.StatusApplied
	}
}

// ImportedConversationStatus exposes the provider-state fallback used by
// compatibility callers that need to inspect a normalized message snapshot.
func ImportedConversationStatus(raw string, messages []conversation.Message) conversation.Status {
	return importedConversationStatus(raw, messages)
}

func latestMeaningful(messages []conversation.Message) *conversation.Message {
	values := make([]conversation.Message, 0, len(messages))
	for _, value := range messages {
		if value.HHSystemEvent || value.Sender == conversation.SenderSystem || value.Sender == conversation.SenderUnknown {
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].Timestamp.Before(values[j].Timestamp) })
	value := values[len(values)-1]
	return &value
}

func applicationTime(value application.JobApplication, events []application.Event) *time.Time {
	var result *time.Time
	for _, event := range events {
		if event.ApplicationID == value.ID && event.Type == application.EventApplied && !event.Timestamp.IsZero() && (result == nil || event.Timestamp.Before(*result)) {
			at := event.Timestamp
			result = &at
		}
	}
	if result == nil && value.HHMetadata["applied_at"] != "" {
		if at, err := time.Parse(time.RFC3339Nano, value.HHMetadata["applied_at"]); err == nil {
			result = &at
		}
	}
	return result
}

func hashText(value string) uint64 {
	var result uint64 = 14695981039346656037
	for _, character := range []byte(value) {
		result ^= uint64(character)
		result *= 1099511628211
	}
	return result
}
