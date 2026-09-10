package jsonstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
	"hh-ai-responder/internal/platform"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

const AutoChatAttemptsFilename = "autochat_attempts.json"

type autoChatAttemptStoreFile struct {
	Version  int              `json:"version"`
	Attempts []domain.Attempt `json:"attempts"`
}

// AutoChatAttemptRepository is a small, file-backed durable authority. Every
// read-modify-write operation reloads under the process lock so separate
// processes cannot reserve the same trigger concurrently.
type AutoChatAttemptRepository struct {
	mu       sync.RWMutex
	path     string
	attempts []domain.Attempt
}

func NewAutoChatAttemptRepository(path string) *AutoChatAttemptRepository {
	if strings.TrimSpace(path) == "" {
		path = AutoChatAttemptsFilename
	}
	return &AutoChatAttemptRepository{path: path, attempts: []domain.Attempt{}}
}

func (r *AutoChatAttemptRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("auto-chat attempt store requires a path")
	}
	attempts, err := readAutoChatAttempts(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.attempts = attempts
	r.mu.Unlock()
	return nil
}

func (r *AutoChatAttemptRepository) FindBlockingForTrigger(ctx context.Context, conversationID, triggerMessageID string) (domain.Attempt, error) {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(triggerMessageID) == "" {
		return domain.Attempt{}, fmt.Errorf("%w: conversation and trigger IDs are required", domain.ErrInvalidAttempt)
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return domain.Attempt{}, errors.New("auto-chat attempt store requires a path")
	}
	var found domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		r.replaceMemory(attempts)
		for _, value := range attempts {
			if value.ConversationID == conversationID && value.TriggerMessageID == triggerMessageID && domain.IsBlocking(value.State) {
				found = value
				return nil
			}
		}
		return domain.ErrAttemptNotFound
	})
	return found, err
}

func (r *AutoChatAttemptRepository) Reserve(ctx context.Context, value domain.Attempt) (autochatport.ReserveResult, error) {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return autochatport.ReserveResult{}, err
	}
	if err := value.Validate(); err != nil || value.State != domain.StateSending {
		return autochatport.ReserveResult{}, fmt.Errorf("reserve auto-chat attempt: %w", domain.ErrInvalidAttempt)
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return autochatport.ReserveResult{}, errors.New("auto-chat attempt store requires a path")
	}
	var result autochatport.ReserveResult
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		for _, existing := range attempts {
			if domain.ConflictKey(existing.ConversationID, existing.TriggerMessageID) == domain.ConflictKey(value.ConversationID, value.TriggerMessageID) && domain.IsBlocking(existing.State) {
				copy := existing
				result.Existing = &copy
				return fmt.Errorf("%w: attempt %s", domain.ErrTriggerBlocked, existing.AttemptID)
			}
		}
		attempts = append(attempts, value)
		if err := writeAutoChatAttempts(r.path, attempts); err != nil {
			return err
		}
		r.replaceMemory(attempts)
		result = autochatport.ReserveResult{Reserved: true, Attempt: value}
		return nil
	})
	return result, err
}

func (r *AutoChatAttemptRepository) RecordOutcome(ctx context.Context, attemptID string, state domain.State, updatedAt time.Time, providerID string, providerStatus int, errorClass string) error {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("auto-chat attempt store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		for index, existing := range attempts {
			if existing.AttemptID != attemptID {
				continue
			}
			updated, transitionErr := existing.WithOutcome(state, updatedAt, providerID, providerStatus, errorClass)
			if transitionErr != nil {
				return transitionErr
			}
			attempts[index] = updated
			if err := writeAutoChatAttempts(r.path, attempts); err != nil {
				return err
			}
			r.replaceMemory(attempts)
			return nil
		}
		return domain.ErrAttemptNotFound
	})
}

func (r *AutoChatAttemptRepository) RecordReconciliation(ctx context.Context, attemptID string, evidence domain.ReconciliationEvidence, updatedAt time.Time) error {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("auto-chat attempt store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		for index, existing := range attempts {
			if existing.AttemptID != attemptID {
				continue
			}
			updated, transitionErr := existing.WithReconciliation(evidence, updatedAt)
			if transitionErr != nil {
				return transitionErr
			}
			attempts[index] = updated
			if err := writeAutoChatAttempts(r.path, attempts); err != nil {
				return err
			}
			r.replaceMemory(attempts)
			return nil
		}
		return domain.ErrAttemptNotFound
	})
}

func (r *AutoChatAttemptRepository) GetByID(ctx context.Context, attemptID string) (domain.Attempt, error) {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return domain.Attempt{}, errors.New("auto-chat attempt store requires a path")
	}
	var result domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		r.replaceMemory(attempts)
		for _, value := range attempts {
			if value.AttemptID == attemptID {
				result = value
				return nil
			}
		}
		return domain.ErrAttemptNotFound
	})
	return result, err
}

func (r *AutoChatAttemptRepository) List(ctx context.Context, query autochatport.ReadQuery) ([]domain.Attempt, error) {
	if err := autoChatAttemptContextError(ctx); err != nil {
		return nil, err
	}
	if query.Limit <= 0 || query.Limit > 100 {
		return nil, errors.New("auto-chat attempt read limit must be between 1 and 100")
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return nil, errors.New("auto-chat attempt store requires a path")
	}
	var result []domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readAutoChatAttempts(r.path)
		if err != nil {
			return err
		}
		r.replaceMemory(attempts)
		filtered := make([]domain.Attempt, 0, len(attempts))
		for _, value := range attempts {
			if query.ConversationID != nil && value.ConversationID != *query.ConversationID || !autoChatStateSelected(value.State, query.States) {
				continue
			}
			if query.ActionType != nil && value.ActionType != *query.ActionType {
				continue
			}
			filtered = append(filtered, value)
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			if !filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
				return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
			}
			return filtered[i].AttemptID < filtered[j].AttemptID
		})
		if len(filtered) > query.Limit {
			filtered = filtered[:query.Limit]
		}
		result = append([]domain.Attempt(nil), filtered...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func autoChatStateSelected(state domain.State, states []domain.State) bool {
	if len(states) == 0 {
		return true
	}
	for _, selected := range states {
		if state == selected {
			return true
		}
	}
	return false
}

func (r *AutoChatAttemptRepository) replaceMemory(attempts []domain.Attempt) {
	r.mu.Lock()
	r.attempts = append([]domain.Attempt(nil), attempts...)
	r.mu.Unlock()
}

func readAutoChatAttempts(path string) ([]domain.Attempt, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Attempt{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read auto-chat attempt store: %w", err)
	}
	var file autoChatAttemptStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.Decode(new(struct{})) != io.EOF || file.Version != 1 || file.Attempts == nil {
		return nil, errors.New("invalid auto-chat attempt store: expected version 1 and attempts array")
	}
	if err := validateAutoChatAttempts(file.Attempts); err != nil {
		return nil, err
	}
	return append([]domain.Attempt(nil), file.Attempts...), nil
}

func writeAutoChatAttempts(path string, attempts []domain.Attempt) error {
	if err := validateAutoChatAttempts(attempts); err != nil {
		return err
	}
	sorted := append([]domain.Attempt(nil), attempts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].AttemptID < sorted[j].AttemptID
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(autoChatAttemptStoreFile{Version: 1, Attempts: sorted}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auto-chat attempt store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auto-chat attempt directory: %w", err)
	}
	return platform.WritePrivateFileAtomic(path, append(raw, '\n'), ".autochat-attempts-*.tmp")
}

func validateAutoChatAttempts(attempts []domain.Attempt) error {
	seen := make(map[string]struct{}, len(attempts))
	blocking := make(map[string]struct{})
	for _, value := range attempts {
		if err := value.Validate(); err != nil {
			return err
		}
		if _, exists := seen[value.AttemptID]; exists {
			return fmt.Errorf("duplicate auto-chat attempt ID %q", value.AttemptID)
		}
		seen[value.AttemptID] = struct{}{}
		if domain.IsBlocking(value.State) {
			key := domain.ConflictKey(value.ConversationID, value.TriggerMessageID)
			if _, exists := blocking[key]; exists {
				return fmt.Errorf("multiple blocking auto-chat attempts for trigger %q", key)
			}
			blocking[key] = struct{}{}
		}
	}
	return nil
}

func autoChatAttemptContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

var _ autochatport.Store = (*AutoChatAttemptRepository)(nil)
var _ autochatport.Reader = (*AutoChatAttemptRepository)(nil)
var _ autochatport.ReconciliationWriter = (*AutoChatAttemptRepository)(nil)
