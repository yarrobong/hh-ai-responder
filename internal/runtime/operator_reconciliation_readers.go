package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	"hh-ai-responder/internal/usecase/autochatreconciliation"
)

// operatorVacancyPreflightReader is intentionally smaller than the responder
// and exposes only the fresh GET needed by application reconciliation.
type operatorVacancyPreflightReader interface {
	ReadVacancyPreflight(context.Context, int) (VacancyPreflight, error)
}

type operatorApplicationProviderReader interface {
	operatorVacancyPreflightReader
	ReadApplications(context.Context, string) (hhread.ApplicationPage, error)
}

type operatorApplicationEvidenceReader struct {
	source operatorApplicationProviderReader
}

func (r operatorApplicationEvidenceReader) ReadVacancyResponseEvidence(ctx context.Context, target applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error) {
	snapshot := applicationreconciliation.EvidenceSnapshot{VacancyID: target.VacancyID, ObservedAt: time.Now().UTC()}
	preflight, preflightErr := r.source.ReadVacancyPreflight(ctx, target.VacancyID)
	if preflightErr == nil {
		snapshot.PreflightAvailable = true
		snapshot.Preflight = applicationreconciliation.PreflightEvidence{Available: true, AlreadyResponded: preflight.AlreadyResponded, AlreadyRespondedKnown: preflight.AlreadyRespondedKnown, CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown}
	} else {
		snapshot.PreflightError = preflightErr.Error()
	}
	applications, applicationsErr := readOperatorApplicationEvidence(ctx, r.source, target.VacancyID)
	snapshot.Applications = applications
	if applicationsErr == nil {
		snapshot.ApplicationsAvailable = true
	} else {
		snapshot.ApplicationsError = applicationsErr.Error()
	}
	if preflightErr != nil && applicationsErr != nil {
		return snapshot, fmt.Errorf("fresh HH reconciliation reads failed: %v; %v", preflightErr, applicationsErr)
	}
	return snapshot, nil
}

func readOperatorApplicationEvidence(ctx context.Context, reader interface {
	ReadApplications(context.Context, string) (hhread.ApplicationPage, error)
}, vacancyID int) ([]applicationreconciliation.ProviderResponse, error) {
	const maxPages = 100
	var result []applicationreconciliation.ProviderResponse
	cursor := ""
	seen := map[string]struct{}{}
	for page := 0; page < maxPages; page++ {
		if _, exists := seen[cursor]; exists {
			return result, errors.New("HH application pagination repeated a cursor")
		}
		seen[cursor] = struct{}{}
		value, err := reader.ReadApplications(ctx, cursor)
		if err != nil {
			return result, err
		}
		for _, item := range value.Items {
			if item.VacancyID != vacancyID || item.Metadata["delivery_confirmed"] != "true" {
				continue
			}
			response := applicationreconciliation.ProviderResponse{VacancyID: item.VacancyID, NegotiationID: strings.TrimSpace(item.ExternalID), ConversationID: strings.TrimSpace(item.ConversationExternal), Source: "negotiations_page", ResponseByApplicant: true}
			if !item.UpdatedAt.IsZero() {
				at := item.UpdatedAt.UTC()
				response.ProviderResponseAt = &at
			} else if !item.CreatedAt.IsZero() {
				at := item.CreatedAt.UTC()
				response.ProviderResponseAt = &at
			}
			result = append(result, response)
		}
		if strings.TrimSpace(value.NextCursor) == "" {
			return result, nil
		}
		cursor = value.NextCursor
	}
	return result, errors.New("HH application pagination exceeded reconciliation bound")
}

type operatorAutoChatHistoryReader struct{ source HHConversationRecordReader }

func (r operatorAutoChatHistoryReader) ReadConversationHistory(ctx context.Context, conversationID string) (autochatreconciliation.HistorySnapshot, error) {
	record, err := r.source.ReadConversation(ctx, strings.TrimSpace(conversationID))
	if err != nil {
		return autochatreconciliation.HistorySnapshot{}, err
	}
	result := autochatreconciliation.HistorySnapshot{ConversationID: record.ExternalID, ObservedAt: record.UpdatedAt, Messages: make([]autochatreconciliation.HistoryMessage, 0, len(record.Messages))}
	for _, message := range record.Messages {
		result.Messages = append(result.Messages, autochatreconciliation.HistoryMessage{ProviderMessageID: message.ExternalID, Sender: message.Sender, Direction: message.Direction, Timestamp: message.Timestamp})
	}
	return result, nil
}
