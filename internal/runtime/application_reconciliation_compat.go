package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

// rootApplicationEvidenceReader is compatibility composition glue. The
// reconciliation usecase sees only this read-only port; it cannot reach the
// HH writer or the legacy requester.
type rootApplicationEvidenceReader struct{ responder *HHAIResponder }

func (r rootApplicationEvidenceReader) ReadVacancyResponseEvidence(ctx context.Context, target applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error) {
	if r.responder == nil {
		return applicationreconciliation.EvidenceSnapshot{}, errors.New("HH responder is not configured")
	}
	snapshot := applicationreconciliation.EvidenceSnapshot{VacancyID: target.VacancyID, ObservedAt: time.Now().UTC()}
	preflight, preflightErr := r.responder.getVacancyPreflightContext(ctx, Vacancy{ID: target.VacancyID})
	if preflightErr != nil {
		snapshot.PreflightError = preflightErr.Error()
	} else {
		snapshot.PreflightAvailable = true
		snapshot.Preflight = applicationreconciliation.PreflightEvidence{
			Available: true, AlreadyResponded: preflight.AlreadyResponded, AlreadyRespondedKnown: preflight.AlreadyRespondedKnown,
			CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown,
		}
	}

	applications, applicationsErr := r.readApplicationEvidence(ctx, target.VacancyID)
	snapshot.Applications = applications
	if applicationsErr != nil {
		snapshot.ApplicationsError = applicationsErr.Error()
	} else {
		snapshot.ApplicationsAvailable = true
	}
	if preflightErr != nil && applicationsErr != nil {
		return snapshot, fmt.Errorf("fresh HH reconciliation reads failed: %v; %v", preflightErr, applicationsErr)
	}
	return snapshot, nil
}

func (r rootApplicationEvidenceReader) readApplicationEvidence(ctx context.Context, vacancyID int) ([]applicationreconciliation.ProviderResponse, error) {
	reader := r.responder.hhReadClient()
	if reader == nil {
		return nil, errors.New("HH application read client is unavailable")
	}
	const maxPages = 100
	result := []applicationreconciliation.ProviderResponse{}
	cursor := ""
	seenCursors := map[string]struct{}{}
	for page := 0; page < maxPages; page++ {
		if _, exists := seenCursors[cursor]; exists {
			return result, errors.New("HH application pagination repeated a cursor")
		}
		seenCursors[cursor] = struct{}{}
		value, err := reader.ReadApplications(ctx, cursor)
		if err != nil {
			return result, err
		}
		for _, item := range value.Items {
			if item.VacancyID != vacancyID {
				continue
			}
			result = append(result, applicationreconciliation.ProviderResponse{
				VacancyID: item.VacancyID, NegotiationID: strings.TrimSpace(item.ExternalID), ConversationID: strings.TrimSpace(item.ConversationExternal), Source: "negotiations_page",
				ResponseByApplicant: item.Metadata["delivery_confirmed"] == "true",
				ProviderResponseAt:  providerResponseTime(item),
			})
		}
		if strings.TrimSpace(value.NextCursor) == "" {
			return result, nil
		}
		cursor = value.NextCursor
	}
	return result, errors.New("HH application pagination exceeded reconciliation bound")
}

func providerResponseTime(value hhread.ApplicationRecord) *time.Time {
	if !value.UpdatedAt.IsZero() {
		at := value.UpdatedAt.UTC()
		return &at
	}
	if !value.CreatedAt.IsZero() {
		at := value.CreatedAt.UTC()
		return &at
	}
	return nil
}
