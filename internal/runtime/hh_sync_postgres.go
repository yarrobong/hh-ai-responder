package runtime

import (
	"context"
	"errors"
	"time"

	"hh-ai-responder/internal/usecase/hhreadsync"
)

// commitPostgresReadBatch keeps only the backend transaction lifecycle in the
// root. The typed use case performs the ordered import through transaction-
// bound domain ports.
func (s *HHReadSyncService) commitPostgresReadBatch(ctx context.Context, vacancies []HHVacancyRecord, applications []HHApplicationRecord, conversations []HHConversationRecord, result *SyncResult) error {
	if s == nil || s.career == nil || s.career.Postgres == nil {
		return errors.New("postgres career repositories are not configured")
	}
	return s.career.Postgres.WithTx(ctx, func(tx CareerTx) error {
		prepared, err := s.prepareVacancyAnalysisAgainst(ctx, tx.Vacancies(), vacancies)
		if err != nil {
			return err
		}
		deps := hhreadsync.Dependencies{
			Vacancies: tx.Vacancies(), Applications: tx.Applications(), Conversations: tx.Conversations(),
			MapStatus: s.statusMapper.Map, WarnStatus: s.statusMapper.Warning,
			ResolveState: func(a JobApplication, c EmployerConversation, appliedAt *time.Time, pending bool, warnings []string, now time.Time) hhreadsync.ConversationResolution {
				resolved := (ConversationStateResolver{}).Resolve(a, c, appliedAt, pending, warnings, now)
				return hhreadsync.ConversationResolution{Status: resolved.Status, WaitingSince: resolved.WaitingSince}
			},
		}
		if s.clarifications != nil {
			deps.PendingState = func(conversationID, applicationID string) (bool, error) {
				values, err := s.clarifications.List()
				if err != nil {
					return false, err
				}
				for _, value := range values {
					if value.Status == ClarificationPending && ((conversationID != "" && value.ConversationID == conversationID) || (applicationID != "" && value.ApplicationID == applicationID)) {
						return true, nil
					}
				}
				return false, nil
			}
		}
		imported, err := hhreadsync.NewService(deps).ImportBatch(ctx, hhreadsync.Batch{
			Vacancies: vacancies, Applications: applications, Conversations: conversations,
		}, hhreadsync.ImportOptions{FailFast: true})
		result.Created += imported.Created
		result.Updated += imported.Updated
		result.Unchanged += imported.Unchanged
		result.Skipped += imported.Skipped
		result.Warnings = append(result.Warnings, imported.Warnings...)
		result.Errors = append(result.Errors, imported.Errors...)
		if err == nil {
			err = s.applyPreparedVacancyAnalysis(ctx, tx.Vacancies(), vacancies, prepared)
		}
		return err
	})
}
