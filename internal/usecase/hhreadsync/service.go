package hhreadsync

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
)

// Service is one-run HH synchronization policy. It is safe to construct per
// execution; process locks, recurrence, and durable cursor files belong to
// the caller.
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

// ReadBatch performs one sequential provider read and returns the normalized
// records without touching persistence. A provider error ends pagination but
// leaves already read records available to the caller, matching the existing
// partial-read behavior. Cursor state is intentionally returned only through
// the source's next-cursor contract; durable cursor ownership remains outside
// the use case.
func (s *Service) ReadBatch(ctx context.Context, target Target, options ReadOptions) (batch Batch, result Result, err error) {
	if s == nil || s.deps.Source == nil {
		return batch, Result{Errors: []string{"HH sync is not configured"}}, nil
	}
	if ctx == nil {
		return batch, Result{Errors: []string{"HH sync context is nil"}}, errors.New("HH sync context is nil")
	}
	result.StartedAt = time.Now().UTC()
	defer func() { result.FinishedAt = time.Now().UTC() }()
	if options.MaxConversations < 0 {
		return batch, result, errors.New("HH conversation limit must be non-negative")
	}

	cursor := ""
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return batch, result, err
		}
		if seen[cursor] {
			result.Errors = append(result.Errors, "HH pagination repeated cursor; sync incomplete")
			break
		}
		seen[cursor] = true
		var next string
		var readErr error
		switch target {
		case TargetVacancies:
			var page hhread.VacancyPage
			page, readErr = s.deps.Source.ReadVacancies(ctx, cursor)
			batch.Vacancies = append(batch.Vacancies, page.Items...)
			result.Fetched += len(page.Items)
			next = page.NextCursor
		case TargetApplications:
			var page hhread.ApplicationPage
			page, readErr = s.deps.Source.ReadApplications(ctx, cursor)
			batch.Applications = append(batch.Applications, page.Items...)
			result.Fetched += len(page.Items)
			next = page.NextCursor
		case TargetConversations, TargetInbox:
			var page hhread.ConversationPage
			remaining := options.MaxConversations - result.Fetched
			if options.MaxConversations > 0 && remaining <= 0 {
				return batch, result, nil
			}
			if options.MaxConversations > 0 {
				bounded, ok := s.deps.Source.(hhreadport.BoundedConversationReadSource)
				if !ok {
					return batch, result, errors.New("bounded HH conversation read is unavailable")
				}
				page, readErr = bounded.ReadConversationsBounded(ctx, cursor, remaining)
			} else {
				page, readErr = s.deps.Source.ReadConversations(ctx, cursor)
			}
			batch.Conversations = append(batch.Conversations, page.Items...)
			if options.MaxConversations > 0 {
				for _, record := range page.Items {
					if strings.TrimSpace(record.ExternalID) != "" {
						result.SelectedConversationIDs = append(result.SelectedConversationIDs, record.ExternalID)
					}
				}
			}
			result.Fetched += len(page.Items)
			result.MetadataChecked += page.MetadataChecked
			result.HistoryReused += page.HistoryReused
			result.DetailedChatsFetched += page.DetailedChatsFetched
			next = page.NextCursor
		default:
			reader, ok := s.deps.Source.(TargetedConversationSource)
			if !ok {
				return batch, result, errors.New("targeted HH conversation read is unavailable")
			}
			id := strings.TrimPrefix(string(target), "conversation:")
			if strings.TrimSpace(id) == "" {
				return batch, result, errors.New("HH conversation id is required")
			}
			var record hhread.ConversationRecord
			record, readErr = reader.ReadConversation(ctx, id)
			if readErr == nil && strings.TrimSpace(record.ExternalID) != id {
				readErr = errors.New("targeted HH conversation identity mismatch")
			}
			if readErr == nil {
				if record.Metadata == nil {
					record.Metadata = map[string]string{}
				}
				record.Metadata["sync_detail_at"] = time.Now().UTC().Format(time.RFC3339Nano)
				batch.Conversations = append(batch.Conversations, record)
				result.Fetched = 1
			}
		}
		if readErr != nil {
			result.Errors = append(result.Errors, safeError(readErr))
			break
		}
		if options.OnPage != nil {
			options.OnPage(result)
		}
		if options.MaxConversations > 0 && result.Fetched >= options.MaxConversations {
			break
		}
		cursor = strings.TrimSpace(next)
		if cursor == "" {
			break
		}
	}
	return batch, result, nil
}

// Sync reads one stream sequentially and imports it through the supplied
// domain ports. It does not sleep, spawn goroutines, persist cursor state, or
// flush backend storage; the caller owns that backend-specific lifecycle.
func (s *Service) Sync(ctx context.Context, target Target) (result Result, err error) {
	if s == nil || s.deps.Source == nil {
		return Result{Errors: []string{"HH sync is not configured"}}, nil
	}
	if ctx == nil {
		return Result{Errors: []string{"HH sync context is nil"}}, errors.New("HH sync context is nil")
	}
	result = Result{StartedAt: time.Now().UTC()}
	defer func() { result.FinishedAt = time.Now().UTC() }()

	batch, readResult, err := s.ReadBatch(ctx, target, ReadOptions{})
	result.Fetched = readResult.Fetched
	result.MetadataChecked = readResult.MetadataChecked
	result.HistoryReused = readResult.HistoryReused
	result.DetailedChatsFetched = readResult.DetailedChatsFetched
	result.Errors = append(result.Errors, readResult.Errors...)
	result.Warnings = append(result.Warnings, readResult.Warnings...)
	if err != nil {
		return result, err
	}

	imported, err := s.ImportBatch(ctx, batch, ImportOptions{})
	result.Created += imported.Created
	result.Updated += imported.Updated
	result.Unchanged += imported.Unchanged
	result.Skipped += imported.Skipped
	result.Errors = append(result.Errors, imported.Errors...)
	result.Warnings = append(result.Warnings, imported.Warnings...)
	result.ChangedConversationIDs = append(result.ChangedConversationIDs, imported.ChangedConversationIDs...)
	if err != nil {
		return result, err
	}
	return result, nil
}

// ImportBatch applies the authoritative import policy. It intentionally does
// not call Save: callers may be inside a backend-specific staged batch or
// transaction and decide durability at that boundary.
func (s *Service) ImportBatch(ctx context.Context, batch Batch, options ImportOptions) (Result, error) {
	if ctx == nil {
		return Result{Errors: []string{"HH sync context is nil"}}, errors.New("HH sync context is nil")
	}
	result := Result{}
	apply := func(outcome outcome, err error) error {
		if err != nil {
			result.Skipped++
			result.Errors = append(result.Errors, safeError(err))
			if options.FailFast {
				return err
			}
			return nil
		}
		switch outcome {
		case created:
			result.Created++
		case updated:
			result.Updated++
		case unchanged:
			result.Unchanged++
		default:
			result.Skipped++
		}
		return nil
	}
	if len(batch.Vacancies) > 0 && s.deps.Vacancies == nil || len(batch.Applications) > 0 && s.deps.Applications == nil || len(batch.Conversations) > 0 && s.deps.Conversations == nil {
		return result, errors.New("HH sync store unavailable")
	}
	for _, record := range batch.Vacancies {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := apply(s.importVacancy(ctx, record)); err != nil {
			return result, err
		}
	}
	for _, record := range batch.Applications {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := apply(s.importApplication(ctx, record)); err != nil {
			return result, err
		}
		if s.deps.WarnStatus != nil {
			if warning := s.deps.WarnStatus(record.Status); warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
		}
	}
	for _, record := range batch.Conversations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if strings.TrimSpace(record.ExternalID) != "" && !record.MetadataUnchanged {
			result.ChangedConversationIDs = append(result.ChangedConversationIDs, record.ExternalID)
		}
		if record.MetadataUnchanged {
			result.Unchanged++
			continue
		}
		if err := apply(s.importConversation(ctx, record)); err != nil {
			return result, err
		}
	}
	return result, nil
}

type outcome uint8

const (
	skipped outcome = iota
	created
	updated
	unchanged
)

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
