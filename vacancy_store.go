package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const VacanciesFilename = "vacancies.json"

var (
	ErrVacancyNotFound          = errors.New("vacancy not found")
	ErrDuplicateVacancyID       = errors.New("duplicate vacancy id")
	ErrDuplicateVacancyExternal = errors.New("duplicate vacancy external id")
)

type vacancyStoreFile struct {
	Version   int       `json:"version"`
	Vacancies []Vacancy `json:"vacancies"`
}

// VacancyStore is a local, detached store. Mutations affect memory only;
// callers explicitly call Save. It has no HH, AI, or application side effects.
type VacancyStore struct {
	path      string
	vacancies []Vacancy
}

func NewVacancyStore(paths ...string) *VacancyStore {
	path := VacanciesFilename
	if len(paths) > 0 && strings.TrimSpace(paths[0]) != "" {
		path = paths[0]
	}
	return &VacancyStore{path: path, vacancies: []Vacancy{}}
}

func (s *VacancyStore) Load() error {
	start := time.Now()
	defer perfRecord("store.VacancyStore.Load", start, 1)
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("vacancy store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.vacancies = []Vacancy{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read vacancy store: %w", err)
	}
	if profileContainsSecret(raw) {
		return errors.New("vacancy store contains a forbidden secret marker")
	}
	var file vacancyStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Vacancies == nil {
		return errors.New("invalid vacancy store: expected version 1 and vacancies array")
	}
	if err := validateVacancies(file.Vacancies); err != nil {
		return err
	}
	s.vacancies = append([]Vacancy{}, file.Vacancies...)
	return nil
}

func (s *VacancyStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("vacancy store requires a path")
	}
	if err := validateVacancies(s.vacancies); err != nil {
		return err
	}
	return withStoreLock(s.path, func() error { return s.saveUnlocked() })
}

func (s *VacancyStore) saveUnlocked() error {
	start := time.Now()
	defer perfRecord("store.VacancyStore.saveUnlocked", start, 1)
	values := s.vacancies
	if values == nil {
		values = []Vacancy{}
	}
	raw, err := json.MarshalIndent(vacancyStoreFile{Version: 1, Vacancies: values}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode vacancy store: %w", err)
	}
	if profileContainsSecret(raw) {
		return errors.New("vacancy store would contain a forbidden secret marker")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create vacancy directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vacancies-*.tmp")
	if err != nil {
		return fmt.Errorf("stage vacancy store: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(append(raw, '\n')); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("write vacancy store: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close vacancy store: %w", closeErr)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace vacancy store: %w", err)
	}
	return nil
}

func validateVacancy(value Vacancy) error {
	if value.ID < 0 {
		return errors.New("vacancy id must not be negative")
	}
	if value.CreatedAt.IsZero() != value.UpdatedAt.IsZero() {
		return errors.New("vacancy timestamps must be supplied together")
	}
	if !value.CreatedAt.IsZero() && value.UpdatedAt.Before(value.CreatedAt) {
		return errors.New("vacancy timestamps are out of order")
	}
	return nil
}

func validateVacancies(values []Vacancy) error {
	ids := make(map[int]bool, len(values))
	externalIDs := make(map[string]bool, len(values))
	for i, value := range values {
		if err := validateVacancy(value); err != nil {
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

func (s *VacancyStore) Create(value Vacancy) (Vacancy, error) {
	if s == nil {
		return Vacancy{}, errors.New("vacancy store is nil")
	}
	if value.ID == 0 {
		value.ID = s.nextID()
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if err := validateVacancy(value); err != nil {
		return Vacancy{}, err
	}
	for _, existing := range s.vacancies {
		if existing.ID == value.ID {
			return Vacancy{}, ErrDuplicateVacancyID
		}
		if strings.TrimSpace(value.ExternalID) != "" && existing.ExternalID == value.ExternalID {
			return Vacancy{}, ErrDuplicateVacancyExternal
		}
	}
	copy, err := cloneKnowledge(value)
	if err != nil {
		return Vacancy{}, err
	}
	s.vacancies = append(s.vacancies, copy)
	return cloneKnowledge(copy)
}

func (s *VacancyStore) nextID() int {
	next := 1
	for _, value := range s.vacancies {
		if value.ID >= next {
			next = value.ID + 1
		}
	}
	return next
}

// Get accepts the local numeric ID or an external ID. The any parameter keeps
// the store convenient for both HH numeric IDs and imported string IDs.
func (s *VacancyStore) Get(identifier any) (Vacancy, error) {
	if s == nil {
		return Vacancy{}, ErrVacancyNotFound
	}
	for _, value := range s.vacancies {
		if vacancyIdentifierMatches(value, identifier) {
			return cloneKnowledge(value)
		}
	}
	return Vacancy{}, ErrVacancyNotFound
}

func (s *VacancyStore) GetByExternalID(externalID string) (Vacancy, error) {
	return s.Get(externalID)
}

func vacancyIdentifierMatches(value Vacancy, identifier any) bool {
	switch id := identifier.(type) {
	case int:
		return value.ID == id
	case int64:
		return int64(value.ID) == id
	case string:
		if value.ExternalID == id {
			return true
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(id))
		return err == nil && value.ID == parsed
	default:
		return false
	}
}

func (s *VacancyStore) List() ([]Vacancy, error) {
	if s == nil {
		return []Vacancy{}, nil
	}
	return cloneKnowledge(append([]Vacancy{}, s.vacancies...))
}

func (s *VacancyStore) Update(value Vacancy) error {
	if s == nil {
		return errors.New("vacancy store is nil")
	}
	if value.ID == 0 {
		return errors.New("vacancy update requires an id")
	}
	for i, existing := range s.vacancies {
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
		for j, other := range s.vacancies {
			if i != j && strings.TrimSpace(value.ExternalID) != "" && other.ExternalID == value.ExternalID {
				return ErrDuplicateVacancyExternal
			}
		}
		if err := validateVacancy(value); err != nil {
			return err
		}
		copy, err := cloneKnowledge(value)
		if err != nil {
			return err
		}
		next := append([]Vacancy{}, s.vacancies...)
		next[i] = copy
		s.vacancies = next
		return nil
	}
	return ErrVacancyNotFound
}

func (s *VacancyStore) Delete(identifier any) error {
	if s == nil {
		return errors.New("vacancy store is nil")
	}
	for i, value := range s.vacancies {
		if !vacancyIdentifierMatches(value, identifier) {
			continue
		}
		s.vacancies = append(append([]Vacancy{}, s.vacancies[:i]...), s.vacancies[i+1:]...)
		return nil
	}
	return ErrVacancyNotFound
}
