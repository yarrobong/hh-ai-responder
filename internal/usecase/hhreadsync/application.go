package hhreadsync

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/application"
	domainconversation "hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/vacancy"
)

func (s *Service) importApplication(ctx context.Context, record hhread.ApplicationRecord) (outcome, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" {
		return skipped, errors.New("HH application has no external id")
	}
	if record.Vacancy != nil && s.deps.Vacancies != nil {
		if _, err := s.deps.Vacancies.Get(ctx, record.Vacancy.ID); errors.Is(err, vacancy.ErrVacancyNotFound) {
			value := *record.Vacancy
			value.Source = "hh"
			if value.ExternalID == "" {
				value.ExternalID = strconvForID(value.ID)
			}
			value.HHMetadata = map[string]string{"detail_partial": "true"}
			value.DataCompleteness = inferCompleteness(value)
			if _, err := s.deps.Vacancies.Create(ctx, value); err != nil {
				return skipped, err
			}
		} else if err != nil {
			return skipped, err
		}
	}
	status, _ := s.mapStatus(record.Status)
	vacancyID := record.VacancyID
	if vacancyID == 0 && strings.TrimSpace(record.VacancyExternalID) != "" && s.deps.Vacancies != nil {
		if value, err := s.deps.Vacancies.GetByExternalID(ctx, record.VacancyExternalID); err == nil {
			vacancyID = value.ID
		} else if !errors.Is(err, vacancy.ErrVacancyNotFound) {
			return skipped, err
		}
	}
	value := application.JobApplication{ExternalID: externalID, VacancyID: vacancyID, CompanyName: record.Company, VacancyTitle: record.VacancyTitle,
		VacancyURL: record.VacancyURL, Source: application.SourceHH, Status: status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		RawStatus: record.Status, HHMetadata: copyStringMap(record.Metadata), Partial: record.Vacancy == nil, DataCompleteness: vacancy.DataCompletenessPartial}
	if record.Vacancy != nil {
		value.DataCompleteness = record.Vacancy.DataCompleteness
		if value.DataCompleteness == "" {
			value.DataCompleteness = inferCompleteness(*record.Vacancy)
		}
		value.Partial = value.DataCompleteness != vacancy.DataCompletenessFull
	}
	if !record.CreatedAt.IsZero() && value.HHMetadata["delivery_confirmed"] == "true" {
		value.HHMetadata["applied_at"] = record.CreatedAt.Format(time.RFC3339Nano)
	}
	if record.ConversationExternal != "" {
		value.HHMetadata["conversation_external_id"] = record.ConversationExternal
		if s.deps.Conversations != nil {
			if found, err := s.deps.Conversations.GetByHHConversationID(ctx, record.ConversationExternal); err == nil {
				value.ConversationID = found.ID
			} else if !errors.Is(err, domainconversation.ErrConversationNotFound) {
				return skipped, err
			}
		}
	}
	old, oldErr := s.deps.Applications.GetByExternalID(ctx, externalID)
	if oldErr == nil {
		if value.Status == application.StatusApplied && old.Status == application.StatusEmployerReplied {
			value.Status = old.Status
		}
		value.MatchResult, value.Notes, value.NextAction, value.FollowUpState = old.MatchResult, old.Notes, old.NextAction, old.FollowUpState
		value.ReconciliationEvidence = old.ReconciliationEvidence
		if value.ConversationID == "" {
			value.ConversationID = old.ConversationID
		}
	} else if !errors.Is(oldErr, application.ErrApplicationNotFound) {
		return skipped, oldErr
	}
	_, _, err := s.deps.Applications.UpsertImported(ctx, value)
	if err != nil {
		return skipped, err
	}
	if oldErr != nil {
		return created, nil
	}
	imported, err := s.deps.Applications.GetByExternalID(ctx, externalID)
	if err != nil {
		return skipped, err
	}
	if imported.ConversationID == "" && value.ConversationID != "" {
		if err := s.deps.Applications.AttachImportedConversation(ctx, externalID, value.ConversationID); err != nil {
			return skipped, err
		}
		imported, err = s.deps.Applications.GetByExternalID(ctx, externalID)
		if err != nil {
			return skipped, err
		}
	}
	if applicationEquivalent(old, imported) {
		return unchanged, nil
	}
	return updated, nil
}

func (s *Service) mapStatus(raw string) (application.Status, bool) {
	if s.deps.MapStatus != nil {
		return s.deps.MapStatus(raw)
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "response", "responded", "applied", "sent", "viewed", "considered":
		return application.StatusApplied, true
	case "invited", "interview":
		return application.StatusInterview, true
	case "offer", "hired":
		return application.StatusOffer, true
	case "rejected", "declined", "discard", "discarded":
		return application.StatusRejected, true
	case "cancelled", "closed", "canceled", "archived":
		return application.StatusArchived, true
	default:
		return application.StatusUnknown, false
	}
}

func applicationEquivalent(a, b application.JobApplication) bool {
	return a.FollowUpState == b.FollowUpState && a.ID == b.ID && a.VacancyID == b.VacancyID && a.ExternalID == b.ExternalID &&
		a.CompanyName == b.CompanyName && a.VacancyTitle == b.VacancyTitle && a.VacancyURL == b.VacancyURL && a.Source == b.Source &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && a.Status == b.Status && a.RawStatus == b.RawStatus &&
		valueMapsEqual(a.HHMetadata, b.HHMetadata) && a.Partial == b.Partial && a.DataCompleteness == b.DataCompleteness &&
		a.ConversationID == b.ConversationID && a.Notes == b.Notes && a.NextAction == b.NextAction &&
		reflect.DeepEqual(a.MatchResult, b.MatchResult) && reflect.DeepEqual(a.ReconciliationEvidence, b.ReconciliationEvidence)
}

func valueMapsEqual(a, b map[string]string) bool { return stringMapsEqual(a, b) }

func strconvForID(value int) string {
	if value == 0 {
		return "0"
	}
	return strconv.Itoa(value)
}
