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
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

const VacanciesFilename = "vacancies.json"

var (
	ErrVacancyNotFound          = vacancy.ErrVacancyNotFound
	ErrDuplicateVacancyID       = vacancy.ErrDuplicateVacancyID
	ErrDuplicateVacancyExternal = vacancy.ErrDuplicateVacancyExternal
)

type vacancyStoreFile struct {
	Version   int               `json:"version"`
	Vacancies []vacancy.Vacancy `json:"vacancies"`
}

// VacancyRepository is the JSON-backed local vacancy store. Mutations affect
// memory only; callers explicitly call Save to make them durable.
type VacancyRepository struct {
	mu        sync.RWMutex
	path      string
	vacancies []vacancy.Vacancy
}

func NewVacancyRepository(path string) *VacancyRepository {
	if strings.TrimSpace(path) == "" {
		path = VacanciesFilename
	}
	return &VacancyRepository{path: path, vacancies: []vacancy.Vacancy{}}
}

func (r *VacancyRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("vacancy store requires a path")
	}
	raw, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.mu.Lock()
		r.vacancies = []vacancy.Vacancy{}
		r.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("read vacancy store: %w", err)
	}
	if containsVacancySecret(raw) {
		return errors.New("vacancy store contains a forbidden secret marker")
	}

	var file vacancyStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.Decode(new(struct{})) != io.EOF || file.Version != 1 || file.Vacancies == nil {
		return errors.New("invalid vacancy store: expected version 1 and vacancies array")
	}
	if err := ValidateVacancies(file.Vacancies); err != nil {
		return err
	}

	r.mu.Lock()
	r.vacancies = append([]vacancy.Vacancy{}, file.Vacancies...)
	r.mu.Unlock()
	return nil
}

func (r *VacancyRepository) Save(ctx context.Context) error {
	if err := vacancyContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("vacancy store requires a path")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return fmt.Errorf("create vacancy directory: %w", err)
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		return r.SaveUnlocked(ctx)
	})
}

// SaveUnlocked writes the current in-memory snapshot without acquiring the
// process lock. It is used by the existing batch-sync lock choreography.
func (r *VacancyRepository) SaveUnlocked(ctx context.Context) error {
	if err := vacancyContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("vacancy store requires a path")
	}
	r.mu.RLock()
	values := append([]vacancy.Vacancy{}, r.vacancies...)
	r.mu.RUnlock()
	if err := ValidateVacancies(values); err != nil {
		return err
	}
	raw, err := marshalVacancyStore(values)
	if err != nil {
		return fmt.Errorf("encode vacancy store: %w", err)
	}
	if containsVacancySecret(raw) {
		return errors.New("vacancy store would contain a forbidden secret marker")
	}
	return platform.WritePrivateFileAtomic(r.path, append(raw, '\n'), ".vacancies-*.tmp")
}

// marshalVacancyStore keeps the provider response-count tri-state durable.
// Vacancy.MarshalJSON emits the legacy numeric field for compatibility, so an
// adapter-level envelope pass removes it when the source explicitly said the
// count was unknown.
func marshalVacancyStore(values []vacancy.Vacancy) ([]byte, error) {
	items := make([]json.RawMessage, len(values))
	for i, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if !value.TotalResponsesCountKnown {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return nil, err
			}
			delete(fields, "totalResponsesCount")
			raw, err = json.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
		items[i] = raw
	}
	return json.MarshalIndent(struct {
		Version   int               `json:"version"`
		Vacancies []json.RawMessage `json:"vacancies"`
	}{Version: 1, Vacancies: items}, "", "  ")
}

func (r *VacancyRepository) Get(ctx context.Context, id int) (vacancy.Vacancy, error) {
	if err := vacancyContextError(ctx); err != nil {
		return vacancy.Vacancy{}, err
	}
	if r == nil {
		return vacancy.Vacancy{}, ErrVacancyNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, value := range r.vacancies {
		if value.ID == id {
			return cloneVacancy(value)
		}
	}
	return vacancy.Vacancy{}, ErrVacancyNotFound
}

func (r *VacancyRepository) GetByExternalID(ctx context.Context, externalID string) (vacancy.Vacancy, error) {
	if err := vacancyContextError(ctx); err != nil {
		return vacancy.Vacancy{}, err
	}
	if r == nil {
		return vacancy.Vacancy{}, ErrVacancyNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, value := range r.vacancies {
		if value.ExternalID == externalID {
			return cloneVacancy(value)
		}
	}
	return vacancy.Vacancy{}, ErrVacancyNotFound
}

func (r *VacancyRepository) List(ctx context.Context, query ports.VacancyQuery) ([]vacancy.Vacancy, error) {
	if err := vacancyContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return []vacancy.Vacancy{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	filtered := make([]vacancy.Vacancy, 0, len(r.vacancies))
	for _, value := range r.vacancies {
		if query.ID != nil && value.ID != *query.ID {
			continue
		}
		if strings.TrimSpace(query.ExternalID) != "" && value.ExternalID != query.ExternalID {
			continue
		}
		copy, err := cloneVacancy(value)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, copy)
	}
	return filtered, nil
}

func (r *VacancyRepository) Create(ctx context.Context, value vacancy.Vacancy) (vacancy.Vacancy, error) {
	if err := vacancyContextError(ctx); err != nil {
		return vacancy.Vacancy{}, err
	}
	if r == nil {
		return vacancy.Vacancy{}, errors.New("vacancy repository is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.ID == 0 {
		value.ID = r.nextIDLocked()
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if err := vacancy.Validate(value); err != nil {
		return vacancy.Vacancy{}, err
	}
	for _, existing := range r.vacancies {
		if existing.ID == value.ID {
			return vacancy.Vacancy{}, ErrDuplicateVacancyID
		}
		if strings.TrimSpace(value.ExternalID) != "" && existing.ExternalID == value.ExternalID {
			return vacancy.Vacancy{}, ErrDuplicateVacancyExternal
		}
	}
	copy, err := cloneVacancy(value)
	if err != nil {
		return vacancy.Vacancy{}, err
	}
	r.vacancies = append(r.vacancies, copy)
	return cloneVacancy(copy)
}

func (r *VacancyRepository) Update(ctx context.Context, value vacancy.Vacancy) error {
	if err := vacancyContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("vacancy repository is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.ID == 0 {
		return errors.New("vacancy update requires an id")
	}
	for i, existing := range r.vacancies {
		if existing.ID != value.ID {
			continue
		}
		if value.CreatedAt.IsZero() {
			value.CreatedAt = existing.CreatedAt
		}
		if value.UpdatedAt.IsZero() {
			value.UpdatedAt = time.Now().UTC()
		}
		if value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = value.CreatedAt
		}
		for j, other := range r.vacancies {
			if i != j && strings.TrimSpace(value.ExternalID) != "" && other.ExternalID == value.ExternalID {
				return ErrDuplicateVacancyExternal
			}
		}
		if err := vacancy.Validate(value); err != nil {
			return err
		}
		copy, err := cloneVacancy(value)
		if err != nil {
			return err
		}
		next := append([]vacancy.Vacancy{}, r.vacancies...)
		next[i] = copy
		r.vacancies = next
		return nil
	}
	return ErrVacancyNotFound
}

// Delete is retained for the root compatibility store; it is not part of the
// staged VacancyWriter port.
func (r *VacancyRepository) Delete(ctx context.Context, id int) error {
	if err := vacancyContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("vacancy store is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, value := range r.vacancies {
		if value.ID == id {
			r.vacancies = append(append([]vacancy.Vacancy{}, r.vacancies[:i]...), r.vacancies[i+1:]...)
			return nil
		}
	}
	return ErrVacancyNotFound
}

// Clone returns an independent in-memory repository with the same path and
// values. It supports the existing read-sync staging workflow.
func (r *VacancyRepository) Clone() (*VacancyRepository, error) {
	if r == nil {
		return nil, errors.New("vacancy repository is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]vacancy.Vacancy, 0, len(r.vacancies))
	for _, value := range r.vacancies {
		copy, err := cloneVacancy(value)
		if err != nil {
			return nil, err
		}
		values = append(values, copy)
	}
	return &VacancyRepository{path: r.path, vacancies: values}, nil
}

func (r *VacancyRepository) nextIDLocked() int {
	next := 1
	for _, value := range r.vacancies {
		if value.ID >= next {
			next = value.ID + 1
		}
	}
	return next
}

func ValidateVacancies(values []vacancy.Vacancy) error {
	ids := make(map[int]bool, len(values))
	externalIDs := make(map[string]bool, len(values))
	for i, value := range values {
		if err := vacancy.Validate(value); err != nil {
			return fmt.Errorf("vacancy %d: %w", i+1, err)
		}
		if value.ID != 0 {
			if ids[value.ID] {
				return ErrDuplicateVacancyID
			}
			ids[value.ID] = true
		}
		externalID := strings.TrimSpace(value.ExternalID)
		if externalID != "" {
			if externalIDs[externalID] {
				return ErrDuplicateVacancyExternal
			}
			externalIDs[externalID] = true
		}
	}
	return nil
}

func cloneVacancy(value vacancy.Vacancy) (vacancy.Vacancy, error) {
	known := value.TotalResponsesCountKnown
	raw, err := json.Marshal(value)
	if err != nil {
		return vacancy.Vacancy{}, err
	}
	var copy vacancy.Vacancy
	if err := json.Unmarshal(raw, &copy); err != nil {
		return vacancy.Vacancy{}, err
	}
	copy.TotalResponsesCountKnown = known
	return copy, nil
}

func containsVacancySecret(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func vacancyContextError(ctx context.Context) error {
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

var (
	_ ports.VacancyReader = (*VacancyRepository)(nil)
	_ ports.VacancyWriter = (*VacancyRepository)(nil)
	_ ports.VacancyStore  = (*VacancyRepository)(nil)
)

var (
	_ ports.VacancyReader = (*VacancyRepository)(nil)
	_ ports.VacancyWriter = (*VacancyRepository)(nil)
	_ ports.VacancyStore  = (*VacancyRepository)(nil)
)
