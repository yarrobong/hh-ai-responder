package runtime

import (
	"context"
	"errors"
	"strconv"
	"strings"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

const VacanciesFilename = jsonstorage.VacanciesFilename

var (
	ErrVacancyNotFound          = vacancy.ErrVacancyNotFound
	ErrDuplicateVacancyID       = vacancy.ErrDuplicateVacancyID
	ErrDuplicateVacancyExternal = vacancy.ErrDuplicateVacancyExternal
)

// VacancyStore is the legacy root-package façade for the JSON vacancy
// adapter. The adapter owns all persistence, indexes, validation and state.
type VacancyStore struct {
	path       string
	repository *jsonstorage.VacancyRepository
	// careerRepository is set only by the runtime composition root for the
	// PostgreSQL backend. The legacy façade methods below then operate on the
	// selected repository without loading or mirroring vacancies.json.
	careerRepository ports.VacancyStore
}

func NewVacancyStore(paths ...string) *VacancyStore {
	path := VacanciesFilename
	if len(paths) > 0 && strings.TrimSpace(paths[0]) != "" {
		path = paths[0]
	}
	return &VacancyStore{path: path, repository: jsonstorage.NewVacancyRepository(path)}
}

func newVacancyStoreFromRepository(repository ports.VacancyStore) *VacancyStore {
	return &VacancyStore{careerRepository: repository}
}

func (s *VacancyStore) selectedRepository() ports.VacancyStore {
	if s == nil {
		return nil
	}
	if s.careerRepository != nil {
		return s.careerRepository
	}
	return s.repository
}

func (s *VacancyStore) Load() error {
	if s != nil && s.careerRepository != nil {
		return nil
	}
	if s == nil || s.repository == nil {
		return errors.New("vacancy store requires a path")
	}
	return s.repository.Load()
}

func (s *VacancyStore) Save() error {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Save(context.Background())
	}
	if s == nil || s.repository == nil {
		return errors.New("vacancy store requires a path")
	}
	return s.repository.Save(context.Background())
}

// saveUnlocked preserves the existing batch-sync lock choreography. The
// outer root code acquires the process lock before calling this façade.
func (s *VacancyStore) saveUnlocked() error {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Save(context.Background())
	}
	if s == nil || s.repository == nil {
		return errors.New("vacancy store requires a path")
	}
	return s.repository.SaveUnlocked(context.Background())
}

func (s *VacancyStore) Create(value Vacancy) (Vacancy, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return Vacancy{}, errors.New("vacancy store is nil")
	}
	// Preserve the historical root façade behavior for locally-created
	// vacancies. HH synchronization calls the typed repository directly and
	// therefore preserves an actually unknown provider count.
	value.TotalResponsesCountKnown = true
	created, err := repository.Create(context.Background(), value)
	if err != nil {
		return Vacancy{}, err
	}
	return created, nil
}

// Get accepts the local numeric ID or an external ID for compatibility with
// the original root store API.
func (s *VacancyStore) Get(identifier any) (Vacancy, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return Vacancy{}, ErrVacancyNotFound
	}
	switch id := identifier.(type) {
	case int:
		return repository.Get(context.Background(), id)
	case int64:
		return repository.Get(context.Background(), int(id))
	case string:
		value, err := repository.GetByExternalID(context.Background(), id)
		if err == nil {
			return value, nil
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(id))
		if parseErr == nil {
			return repository.Get(context.Background(), parsed)
		}
		return Vacancy{}, err
	default:
		return Vacancy{}, ErrVacancyNotFound
	}
}

func (s *VacancyStore) GetByExternalID(externalID string) (Vacancy, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return Vacancy{}, ErrVacancyNotFound
	}
	return repository.GetByExternalID(context.Background(), externalID)
}

func (s *VacancyStore) List() ([]Vacancy, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return []Vacancy{}, nil
	}
	return repository.List(context.Background(), ports.VacancyQuery{})
}

func (s *VacancyStore) Update(value Vacancy) error {
	repository := s.selectedRepository()
	if repository == nil {
		return errors.New("vacancy store is nil")
	}
	value.TotalResponsesCountKnown = true
	return repository.Update(context.Background(), value)
}

func (s *VacancyStore) Delete(identifier any) error {
	if s != nil && s.careerRepository != nil {
		return errors.New("vacancy delete is not supported by the career repository")
	}
	if s == nil || s.repository == nil {
		return errors.New("vacancy store is nil")
	}
	value, err := s.Get(identifier)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(context.Background(), value.ID); err != nil {
		return err
	}
	return nil
}

func (s *VacancyStore) cloneForSync() (*VacancyStore, error) {
	if s != nil && s.careerRepository != nil {
		return nil, errors.New("vacancy sync clone is not supported by the career repository")
	}
	if s == nil || s.repository == nil {
		return nil, errors.New("vacancy store is nil")
	}
	repository, err := s.repository.Clone()
	if err != nil {
		return nil, err
	}
	return &VacancyStore{path: s.path, repository: repository}, nil
}
