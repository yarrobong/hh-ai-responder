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

	domain "hh-ai-responder/internal/applicationattempt"
	"hh-ai-responder/internal/platform"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

const ApplicationAttemptsFilename = "application_attempts.json"

type applicationAttemptStoreFile struct {
	Version  int              `json:"version"`
	Attempts []domain.Attempt `json:"attempts"`
}

// ApplicationAttemptRepository is a separate write-safety store. It does not
// reuse job_applications.json or the application read model.
type ApplicationAttemptRepository struct {
	mu       sync.RWMutex
	path     string
	attempts []domain.Attempt
}

func NewApplicationAttemptRepository(path string) *ApplicationAttemptRepository {
	if strings.TrimSpace(path) == "" {
		path = ApplicationAttemptsFilename
	}
	return &ApplicationAttemptRepository{path: path, attempts: []domain.Attempt{}}
}

func (r *ApplicationAttemptRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application attempt store requires a path")
	}
	attempts, err := readApplicationAttempts(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.attempts = attempts
	r.mu.Unlock()
	return nil
}

func (r *ApplicationAttemptRepository) Reserve(ctx context.Context, value domain.Attempt) (attemptport.ReserveResult, error) {
	if err := attemptContextError(ctx); err != nil {
		return attemptport.ReserveResult{}, err
	}
	if err := value.Validate(); err != nil || value.State != domain.StateSending {
		return attemptport.ReserveResult{}, fmt.Errorf("reserve application attempt: %w", domain.ErrInvalidAttempt)
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return attemptport.ReserveResult{}, errors.New("application attempt store requires a path")
	}
	var result attemptport.ReserveResult
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		for _, existing := range attempts {
			if existing.VacancyID == value.VacancyID && domain.IsBlocking(existing.State) {
				copy := existing
				result.Existing = &copy
				return fmt.Errorf("%w: attempt %s", domain.ErrTargetBlocked, existing.AttemptID)
			}
		}
		attempts = append(attempts, value)
		if err := writeApplicationAttempts(r.path, attempts); err != nil {
			return err
		}
		result = attemptport.ReserveResult{Reserved: true, Attempt: value}
		r.mu.Lock()
		r.attempts = attempts
		r.mu.Unlock()
		return nil
	})
	return result, err
}

func (r *ApplicationAttemptRepository) RecordOutcome(ctx context.Context, attemptID string, state domain.State, updatedAt time.Time, providerStatus int, errorClass string) error {
	if err := attemptContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if !domain.IsReplayable(state) && !domain.IsBlocking(state) {
		return fmt.Errorf("%w: unknown state %s", domain.ErrInvalidTransition, state)
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application attempt store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		for index, existing := range attempts {
			if existing.AttemptID != attemptID {
				continue
			}
			updated, transitionErr := existing.WithOutcome(state, updatedAt, providerStatus, errorClass)
			if transitionErr != nil {
				return transitionErr
			}
			attempts[index] = updated
			if err := writeApplicationAttempts(r.path, attempts); err != nil {
				return err
			}
			r.mu.Lock()
			r.attempts = attempts
			r.mu.Unlock()
			return nil
		}
		return domain.ErrAttemptNotFound
	})
}

func (r *ApplicationAttemptRepository) FindBlocking(ctx context.Context, vacancyID int) (domain.Attempt, error) {
	if err := attemptContextError(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if vacancyID <= 0 {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return domain.Attempt{}, errors.New("application attempt store requires a path")
	}
	var result domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		for _, value := range attempts {
			if value.VacancyID == vacancyID && domain.IsBlocking(value.State) {
				result = value
				r.mu.Lock()
				r.attempts = attempts
				r.mu.Unlock()
				return nil
			}
		}
		return domain.ErrAttemptNotFound
	})
	return result, err
}

func (r *ApplicationAttemptRepository) RecordReconciliation(ctx context.Context, attemptID string, evidence domain.ReconciliationEvidence, now time.Time) error {
	if err := attemptContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application attempt store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		for index, existing := range attempts {
			if existing.AttemptID != attemptID {
				continue
			}
			updated, transitionErr := existing.WithReconciliation(evidence, now)
			if transitionErr != nil {
				return transitionErr
			}
			attempts[index] = updated
			if err := writeApplicationAttempts(r.path, attempts); err != nil {
				return err
			}
			r.mu.Lock()
			r.attempts = attempts
			r.mu.Unlock()
			return nil
		}
		return domain.ErrAttemptNotFound
	})
}

func (r *ApplicationAttemptRepository) Get(ctx context.Context, attemptID string) (domain.Attempt, error) {
	if err := attemptContextError(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return domain.Attempt{}, errors.New("application attempt store requires a path")
	}
	var result domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		for _, value := range attempts {
			if value.AttemptID == attemptID {
				result = value
				r.mu.Lock()
				r.attempts = attempts
				r.mu.Unlock()
				return nil
			}
		}
		return domain.ErrAttemptNotFound
	})
	return result, err
}

func (r *ApplicationAttemptRepository) GetByID(ctx context.Context, attemptID string) (domain.Attempt, error) {
	return r.Get(ctx, attemptID)
}

func (r *ApplicationAttemptRepository) List(ctx context.Context, query attemptport.ReadQuery) ([]domain.Attempt, error) {
	if err := attemptContextError(ctx); err != nil {
		return nil, err
	}
	if query.Limit <= 0 || query.Limit > 100 {
		return nil, errors.New("application attempt read limit must be between 1 and 100")
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return nil, errors.New("application attempt store requires a path")
	}
	var result []domain.Attempt
	err := platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		attempts, err := readApplicationAttempts(r.path)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.attempts = append([]domain.Attempt(nil), attempts...)
		r.mu.Unlock()
		filtered := make([]domain.Attempt, 0, len(attempts))
		for _, value := range attempts {
			if query.VacancyID != nil && value.VacancyID != *query.VacancyID || !applicationStateSelected(value.State, query.States) {
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

func applicationStateSelected(state domain.State, states []domain.State) bool {
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

func readApplicationAttempts(path string) ([]domain.Attempt, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Attempt{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read application attempt store: %w", err)
	}
	var file applicationAttemptStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.Decode(new(struct{})) != io.EOF || file.Version != 1 || file.Attempts == nil {
		return nil, errors.New("invalid application attempt store: expected version 1 and attempts array")
	}
	if err := validateApplicationAttempts(file.Attempts); err != nil {
		return nil, err
	}
	return append([]domain.Attempt(nil), file.Attempts...), nil
}

func writeApplicationAttempts(path string, attempts []domain.Attempt) error {
	sorted := append([]domain.Attempt(nil), attempts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].AttemptID < sorted[j].AttemptID
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(applicationAttemptStoreFile{Version: 1, Attempts: sorted}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode application attempt store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create application attempt directory: %w", err)
	}
	return platform.WritePrivateFileAtomic(path, append(raw, '\n'), ".application-attempts-*.tmp")
}

func validateApplicationAttempts(attempts []domain.Attempt) error {
	seen := make(map[string]struct{}, len(attempts))
	activeVacancies := make(map[int]struct{})
	for _, value := range attempts {
		if err := value.Validate(); err != nil {
			return err
		}
		if _, exists := seen[value.AttemptID]; exists {
			return fmt.Errorf("duplicate application attempt id %q", value.AttemptID)
		}
		seen[value.AttemptID] = struct{}{}
		if domain.IsBlocking(value.State) {
			if _, exists := activeVacancies[value.VacancyID]; exists {
				return fmt.Errorf("multiple blocking application attempts for vacancy %d", value.VacancyID)
			}
			activeVacancies[value.VacancyID] = struct{}{}
		}
	}
	return nil
}

func attemptContextError(ctx context.Context) error {
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

var _ attemptport.Store = (*ApplicationAttemptRepository)(nil)
var _ attemptport.BlockingReader = (*ApplicationAttemptRepository)(nil)
var _ attemptport.Reader = (*ApplicationAttemptRepository)(nil)
