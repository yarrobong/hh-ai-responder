package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// commitPostgresReadBatch is intentionally separate from the JSON staged
// commit. It performs the same import decisions through repository contracts,
// but all career-data writes share the CareerRepositories transaction.
func (s *HHReadSyncService) commitPostgresReadBatch(ctx context.Context, vacancies []HHVacancyRecord, applications []HHApplicationRecord, conversations []HHConversationRecord, result *SyncResult) error {
	if s == nil || s.career == nil || s.career.Postgres == nil {
		return errors.New("postgres career repositories are not configured")
	}
	return s.career.Postgres.WithTx(ctx, func(tx CareerTx) error {
		for _, record := range vacancies {
			outcome, err := importPostgresVacancy(ctx, tx.Vacancies(), record, s.analyzer, s.candidate)
			if err != nil {
				return err
			}
			countSyncOutcome(result, outcome)
		}
		for _, record := range applications {
			outcome, err := importPostgresApplication(ctx, tx, record, s.statusMapper)
			if err != nil {
				return err
			}
			countSyncOutcome(result, outcome)
			if warning := s.statusMapper.Warning(record.Status); warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
		}
		for _, record := range conversations {
			if record.MetadataUnchanged {
				result.Unchanged++
				continue
			}
			outcome, err := importPostgresConversation(ctx, tx, record, s.clarifications)
			if err != nil {
				return err
			}
			countSyncOutcome(result, outcome)
		}
		return nil
	})
}

func countSyncOutcome(result *SyncResult, outcome syncOutcome) {
	switch outcome {
	case syncCreated:
		result.Created++
	case syncUpdated:
		result.Updated++
	case syncUnchanged:
		result.Unchanged++
	default:
		result.Skipped++
	}
}

func importPostgresVacancy(ctx context.Context, repo VacancyRepository, record HHVacancyRecord, analyzer interface {
	Analyze(Vacancy, any) MatchResult
}, candidate any) (syncOutcome, error) {
	vacancy, err := mapHHVacancy(record)
	if err != nil {
		return syncSkipped, err
	}
	old, getErr := repo.GetByExternalID(ctx, vacancy.ExternalID)
	if getErr == nil {
		vacancy.ID, vacancy.CreatedAt = old.ID, old.CreatedAt
		if vacancy.UpdatedAt.IsZero() || vacancy.UpdatedAt.Before(vacancy.CreatedAt) {
			vacancy.UpdatedAt = old.UpdatedAt
		}
		if vacancySourceEquivalent(old, vacancy) {
			return syncUnchanged, nil
		}
		if analyzer != nil {
			match := analyzer.Analyze(vacancy, candidate)
			vacancy.MatchResult = &match
			vacancy.ApplicationRecommendation = match.Recommendation
		}
		return syncUpdated, repo.Update(ctx, vacancy)
	}
	if !errors.Is(getErr, ErrVacancyNotFound) {
		return syncSkipped, getErr
	}
	if analyzer != nil {
		match := analyzer.Analyze(vacancy, candidate)
		vacancy.MatchResult = &match
		vacancy.ApplicationRecommendation = match.Recommendation
	}
	_, err = repo.Create(ctx, vacancy)
	return syncCreated, err
}

func importPostgresApplication(ctx context.Context, tx CareerTx, record HHApplicationRecord, mapper HHStatusMapper) (syncOutcome, error) {
	if strings.TrimSpace(record.ExternalID) == "" {
		return syncSkipped, errors.New("HH application has no external id")
	}
	if record.Vacancy != nil {
		if _, err := tx.Vacancies().Get(ctx, record.Vacancy.ID); errors.Is(err, ErrVacancyNotFound) {
			vacancy := *record.Vacancy
			vacancy.Source = "hh"
			if vacancy.ExternalID == "" {
				vacancy.ExternalID = strconvItoa(vacancy.ID)
			}
			vacancy.HHMetadata = map[string]string{"detail_partial": "true"}
			vacancy.DataCompleteness = inferVacancyCompleteness(vacancy)
			if _, err := tx.Vacancies().Create(ctx, vacancy); err != nil {
				return syncSkipped, err
			}
		} else if err != nil {
			return syncSkipped, err
		}
	}
	status, _ := mapper.Map(record.Status)
	vacancyID := record.VacancyID
	if vacancyID == 0 && record.VacancyExternalID != "" {
		vacancy, err := tx.Vacancies().GetByExternalID(ctx, record.VacancyExternalID)
		if err != nil {
			return syncSkipped, err
		}
		vacancyID = vacancy.ID
	}
	value := JobApplication{ExternalID: strings.TrimSpace(record.ExternalID), VacancyID: vacancyID, CompanyName: record.Company, VacancyTitle: record.VacancyTitle, VacancyURL: record.VacancyURL, Source: ApplicationSourceHH, Status: status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, RawStatus: record.Status, HHMetadata: copyStringMap(record.Metadata), Partial: record.Vacancy == nil, DataCompleteness: DataCompletenessPartial}
	if record.Vacancy != nil {
		value.DataCompleteness = record.Vacancy.DataCompleteness
		if value.DataCompleteness == "" {
			value.DataCompleteness = inferVacancyCompleteness(*record.Vacancy)
		}
		value.Partial = value.DataCompleteness != DataCompletenessFull
	}
	if !record.CreatedAt.IsZero() && value.HHMetadata["delivery_confirmed"] == "true" {
		value.HHMetadata["applied_at"] = record.CreatedAt.Format(time.RFC3339Nano)
	}
	if record.ConversationExternal != "" {
		value.HHMetadata["conversation_external_id"] = record.ConversationExternal
		if conversation, err := tx.Conversations().GetByHHConversationID(ctx, record.ConversationExternal); err == nil {
			value.ConversationID, value.HHMetadata["conversation_external_id"] = conversation.ID, record.ConversationExternal
		}
	}
	old, oldErr := tx.Applications().GetByExternalID(ctx, value.ExternalID)
	if oldErr == nil {
		if value.Status == ApplicationApplied && old.Status == ApplicationEmployerReplied {
			value.Status = old.Status
		}
		value.MatchResult, value.Notes, value.NextAction, value.FollowUpState = old.MatchResult, old.Notes, old.NextAction, old.FollowUpState
		if value.ConversationID == "" {
			value.ConversationID = old.ConversationID
		}
	} else if !errors.Is(oldErr, ErrApplicationNotFound) {
		return syncSkipped, oldErr
	}
	_, _, err := tx.Applications().UpsertImported(ctx, value)
	if err != nil {
		return syncSkipped, err
	}
	if oldErr == nil {
		imported, err := tx.Applications().GetByExternalID(ctx, value.ExternalID)
		if err != nil {
			return syncSkipped, err
		}
		if imported.ConversationID == "" && value.ConversationID != "" {
			if err := tx.Applications().AttachImportedConversation(ctx, value.ExternalID, value.ConversationID); err != nil {
				return syncSkipped, err
			}
			imported, err = tx.Applications().GetByExternalID(ctx, value.ExternalID)
			if err != nil {
				return syncSkipped, err
			}
		}
		if applicationEquivalent(old, imported) {
			return syncUnchanged, nil
		}
		return syncUpdated, nil
	}
	return syncCreated, nil
}

func importPostgresConversation(ctx context.Context, tx CareerTx, record HHConversationRecord, clarifications *CandidateClarificationStore) (syncOutcome, error) {
	if strings.TrimSpace(record.ExternalID) == "" {
		return syncSkipped, errors.New("HH conversation has no external id")
	}
	vacancyID := record.VacancyID
	if vacancyID == 0 && record.VacancyExternalID != "" {
		vacancy, err := tx.Vacancies().GetByExternalID(ctx, record.VacancyExternalID)
		if err != nil {
			return syncSkipped, err
		}
		vacancyID = vacancy.ID
	}
	messages, warnings := mapHHMessages(record.Messages)
	old, oldErr := tx.Conversations().GetByHHConversationID(ctx, record.ExternalID)
	value := EmployerConversation{VacancyID: vacancyID, HHConversationID: strings.TrimSpace(record.ExternalID), CompanyName: record.Company, VacancyTitle: record.VacancyTitle, VacancyDescription: record.VacancyDescription, Status: conversationStatusForImport(record.Status, messages), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, HHUpdatedAt: record.UpdatedAt, Messages: messages, RawStatus: record.Status, HHMetadata: copyStringMap(record.Metadata)}
	if len(warnings) > 0 {
		value.HHMetadata["warning_skipped_messages"] = "true"
	}
	if oldErr == nil {
		value.ID = old.ID
		mergeHHConversation(old, &value)
	} else if !errors.Is(oldErr, ErrConversationNotFound) {
		return syncSkipped, oldErr
	}
	applications, err := tx.Applications().List(ctx)
	if err != nil {
		return syncSkipped, err
	}
	application := JobApplication{}
	for _, candidate := range applications {
		if candidate.HHMetadata["conversation_external_id"] == record.ExternalID {
			application = candidate
			value.ApplicationID = candidate.ID
			break
		}
	}
	var appliedAt *time.Time
	if application.ID != "" {
		if events, eventsErr := tx.Applications().Timeline(ctx, application.ID); eventsErr == nil {
			appliedAt = knownApplicationTime(application, events)
		}
	}
	pending := false
	if clarifications != nil {
		for _, question := range clarifications.clarifications {
			if question.Status == ClarificationPending && ((value.ID != "" && question.ConversationID == value.ID) || (application.ID != "" && question.ApplicationID == application.ID)) {
				pending = true
			}
		}
	}
	resolved := (ConversationStateResolver{}).Resolve(application, value, appliedAt, pending, nil, time.Now())
	value.Status, value.WaitingSince = resolved.Status, resolved.WaitingSince
	value.refreshActivity()
	if _, err := tx.Conversations().Upsert(ctx, value); err != nil {
		return syncSkipped, err
	}
	saved, err := tx.Conversations().GetByHHConversationID(ctx, record.ExternalID)
	if err != nil {
		return syncSkipped, err
	}
	applications, err = tx.Applications().List(ctx)
	if err != nil {
		return syncSkipped, err
	}
	exactMatch := false
	for _, candidate := range applications {
		if candidate.HHMetadata["conversation_external_id"] == saved.HHConversationID {
			exactMatch = true
			break
		}
	}
	vacancyMatches := 0
	for _, candidate := range applications {
		if candidate.VacancyID == saved.VacancyID {
			vacancyMatches++
		}
	}
	for _, candidate := range applications {
		if exactMatch && candidate.HHMetadata["conversation_external_id"] != saved.HHConversationID {
			continue
		}
		if !exactMatch && (saved.VacancyID <= 0 || vacancyMatches != 1 || candidate.VacancyID != saved.VacancyID) {
			continue
		}
		if saved.Status == ConversationCandidateActionRequired && candidate.Status == ApplicationApplied {
			if err := tx.Applications().UpdateStatus(ctx, candidate.ID, ApplicationEmployerReplied); err != nil {
				return syncSkipped, err
			}
		}
		if candidate.ConversationID == "" {
			if err := tx.Applications().AttachConversation(ctx, candidate.ID, saved.ID); err != nil {
				return syncSkipped, err
			}
		}
	}
	if oldErr != nil {
		return syncCreated, nil
	}
	if conversationEquivalent(old, saved) {
		return syncUnchanged, nil
	}
	return syncUpdated, nil
}

// Kept local so the repository-backed sync does not depend on an incidental
// strconv import in this file's public surface.
func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buf := [24]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
