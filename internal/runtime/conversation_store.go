package runtime

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/ports"
)

const EmployerConversationsFilename = jsonstorage.EmployerConversationsFilename

var ErrConversationNotFound = jsonstorage.ErrConversationNotFound

// conversationStoreFile is a compatibility alias used by the diagnostic
// audit and legacy tests. The JSON envelope is owned by jsonstorage.
type conversationStoreFile = jsonstorage.ConversationStoreFile

// ConversationStore is the legacy root façade. JSON persistence, identity
// indexes, validation, history semantics, and durability belong to repository.
// conversations is a deprecated read/mutation mirror for old root workflows;
// syncCompatibilityState makes repository authoritative before every façade
// operation and rejects direct mirror changes that fail adapter validation.
type ConversationStore struct {
	path             string
	repository       *jsonstorage.ConversationRepository
	careerRepository ports.ConversationStore
	conversations    []EmployerConversation

	compatibilityMu     sync.Mutex
	mirrorConversations []EmployerConversation
}

func NewConversationStore(path string) *ConversationStore {
	return &ConversationStore{
		path:                path,
		repository:          jsonstorage.NewConversationRepository(path),
		conversations:       []EmployerConversation{},
		mirrorConversations: []EmployerConversation{},
	}
}

func newConversationStoreFromRepository(repository ports.ConversationStore) *ConversationStore {
	return &ConversationStore{careerRepository: repository, conversations: []EmployerConversation{}, mirrorConversations: []EmployerConversation{}}
}

func (s *ConversationStore) selectedRepository() ports.ConversationStore {
	if s == nil {
		return nil
	}
	if s.careerRepository != nil {
		return s.careerRepository
	}
	return s.repository
}

func (s *ConversationStore) Load() error {
	if s != nil && s.careerRepository != nil {
		return s.refreshCompatibilityMirror()
	}
	if s == nil || s.repository == nil {
		return errors.New("conversation store requires a path")
	}
	s.repository.SetPath(s.path)
	if err := s.repository.Load(); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ConversationStore) Save() error {
	if s != nil && s.careerRepository != nil {
		if saver, ok := s.careerRepository.(interface{ Save(context.Context) error }); ok {
			return saver.Save(context.Background())
		}
		return nil
	}
	if s == nil || s.repository == nil {
		return errors.New("conversation store requires a path")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	return s.repository.Save(context.Background())
}

// saveUnlocked preserves the existing batch-sync lock choreography. The
// caller already holds the process lock when this method is used.
func (s *ConversationStore) saveUnlocked() error {
	if s != nil && s.careerRepository != nil {
		if saver, ok := s.careerRepository.(interface{ Save(context.Context) error }); ok {
			return saver.Save(context.Background())
		}
		return nil
	}
	start := time.Now()
	defer perfRecord("store.ConversationStore.saveUnlocked", start, 1)
	if s == nil || s.repository == nil {
		return errors.New("conversation store requires a path")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	return s.repository.SaveUnlocked(context.Background())
}

func (s *ConversationStore) syncCompatibilityState() error {
	if s != nil && s.careerRepository != nil {
		return s.refreshCompatibilityMirror()
	}
	if s == nil || s.repository == nil {
		return errors.New("conversation store is not configured")
	}
	s.compatibilityMu.Lock()
	defer s.compatibilityMu.Unlock()
	s.repository.SetPath(s.path)
	values, err := s.repository.Snapshot(nil)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(s.conversations, s.mirrorConversations) {
		if err := s.repository.ReplaceSnapshot(nil, s.conversations); err != nil {
			return err
		}
		values, err = s.repository.Snapshot(nil)
		if err != nil {
			return err
		}
	}
	s.conversations = values
	s.mirrorConversations, err = cloneKnowledge(values)
	if err != nil {
		return err
	}
	return nil
}

func (s *ConversationStore) refreshCompatibilityMirror() error {
	if s != nil && s.careerRepository != nil {
		values, err := s.careerRepository.List(nil)
		if err != nil {
			return err
		}
		mirror, err := cloneKnowledge(values)
		if err != nil {
			return err
		}
		s.compatibilityMu.Lock()
		s.conversations, s.mirrorConversations = values, mirror
		s.compatibilityMu.Unlock()
		return nil
	}
	if s == nil || s.repository == nil {
		return errors.New("conversation store is not configured")
	}
	values, err := s.repository.Snapshot(nil)
	if err != nil {
		return err
	}
	mirror, err := cloneKnowledge(values)
	if err != nil {
		return err
	}
	s.compatibilityMu.Lock()
	s.conversations = values
	s.mirrorConversations = mirror
	s.compatibilityMu.Unlock()
	return nil
}

func (s *ConversationStore) GetConversation(id string) (EmployerConversation, error) {
	if s == nil {
		return EmployerConversation{}, ErrConversationNotFound
	}
	if repository := s.selectedRepository(); repository != nil {
		if err := s.syncCompatibilityState(); err != nil {
			return EmployerConversation{}, err
		}
		return repository.Get(context.Background(), id)
	}
	for _, value := range s.conversations {
		if value.ID == id {
			return cloneKnowledge(value)
		}
	}
	return EmployerConversation{}, ErrConversationNotFound
}

func (s *ConversationStore) GetByHHConversationID(id string) (EmployerConversation, error) {
	if s == nil || id == "" {
		return EmployerConversation{}, ErrConversationNotFound
	}
	if repository := s.selectedRepository(); repository != nil {
		if err := s.syncCompatibilityState(); err != nil {
			return EmployerConversation{}, err
		}
		return repository.GetByHHConversationID(context.Background(), id)
	}
	for _, value := range s.conversations {
		if value.HHConversationID == id {
			return cloneKnowledge(value)
		}
	}
	return EmployerConversation{}, ErrConversationNotFound
}

func (s *ConversationStore) GetByVacancyID(vacancyID int) ([]EmployerConversation, error) {
	if s == nil {
		return []EmployerConversation{}, nil
	}
	if repository := s.selectedRepository(); repository != nil {
		if err := s.syncCompatibilityState(); err != nil {
			return nil, err
		}
		return repository.GetByVacancyID(context.Background(), vacancyID)
	}
	result := []EmployerConversation{}
	for _, value := range s.conversations {
		if value.VacancyID == vacancyID {
			result = append(result, value)
		}
	}
	return cloneKnowledge(result)
}

func (s *ConversationStore) ListConversations() ([]EmployerConversation, error) {
	if s == nil {
		return []EmployerConversation{}, nil
	}
	if repository := s.selectedRepository(); repository != nil {
		if err := s.syncCompatibilityState(); err != nil {
			return nil, err
		}
		return repository.List(context.Background())
	}
	return cloneKnowledge(s.conversations)
}

func (s *ConversationStore) ListConversationsByStatus(status ConversationStatus) ([]EmployerConversation, error) {
	values, err := s.ListConversations()
	if err != nil {
		return nil, err
	}
	result := make([]EmployerConversation, 0)
	for _, value := range values {
		if value.Status == status {
			result = append(result, value)
		}
	}
	return result, nil
}

func (s *ConversationStore) UpsertConversation(value EmployerConversation) (EmployerConversation, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return EmployerConversation{}, errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return EmployerConversation{}, err
	}
	result, err := repository.Upsert(context.Background(), value)
	if err != nil {
		return EmployerConversation{}, err
	}
	if err := s.refreshCompatibilityMirror(); err != nil {
		return EmployerConversation{}, err
	}
	return result, nil
}

func (s *ConversationStore) AppendMessage(id string, value ConversationMessage) (ConversationMessage, error) {
	repository := s.selectedRepository()
	if repository == nil {
		return ConversationMessage{}, errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return ConversationMessage{}, err
	}
	result, err := repository.AppendMessage(context.Background(), id, value)
	if err != nil {
		return ConversationMessage{}, err
	}
	if err := s.refreshCompatibilityMirror(); err != nil {
		return ConversationMessage{}, err
	}
	return result, nil
}

func (s *ConversationStore) UpdateConversationState(id string, state ConversationState) error {
	repository := s.selectedRepository()
	if repository == nil {
		return errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := repository.UpdateState(context.Background(), id, state); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ConversationStore) UpdateSummary(id string, summary ConversationSummary) error {
	repository := s.selectedRepository()
	if repository == nil {
		return errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := repository.UpdateSummary(context.Background(), id, summary); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ConversationStore) RecordCandidateClaim(id string, claim CandidateConversationClaim) error {
	repository := s.selectedRepository()
	if repository == nil {
		return errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := repository.AppendCandidateClaim(context.Background(), id, claim); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ConversationStore) GetConversationTimeline(id string) ([]ConversationMessage, error) {
	if s == nil {
		return nil, ErrConversationNotFound
	}
	if repository := s.selectedRepository(); repository != nil {
		if err := s.syncCompatibilityState(); err != nil {
			return nil, err
		}
		return repository.Timeline(context.Background(), id)
	}
	c, err := s.GetConversation(id)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(c.Messages, func(i, j int) bool { return c.Messages[i].Timestamp.Before(c.Messages[j].Timestamp) })
	return c.Messages, nil
}

func (s *ConversationStore) cloneForSync() (*ConversationStore, error) {
	if s != nil && s.careerRepository != nil {
		return nil, errors.New("conversation sync clone is not supported by the career repository")
	}
	if s == nil || s.repository == nil {
		return nil, errors.New("conversation store is not configured")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return nil, err
	}
	repository, err := s.repository.Clone()
	if err != nil {
		return nil, err
	}
	clone := &ConversationStore{path: s.path, repository: repository, conversations: []EmployerConversation{}, mirrorConversations: []EmployerConversation{}}
	if err := clone.refreshCompatibilityMirror(); err != nil {
		return nil, err
	}
	return clone, nil
}

func (s *ConversationStore) adoptFromSync(source *ConversationStore) error {
	if s == nil || source == nil || source.repository == nil {
		return errors.New("conversation store is not configured")
	}
	if err := source.syncCompatibilityState(); err != nil {
		return err
	}
	s.repository = source.repository
	return s.refreshCompatibilityMirror()
}

// validateConversations and the envelope alias preserve root migration and
// diagnostic call sites while delegating validation/schema ownership.
func validateConversations(values []EmployerConversation) error {
	return jsonstorage.ValidateConversations(values)
}

type ConversationStats struct {
	Total                   int `json:"total"`
	WaitingEmployer         int `json:"waiting_employer"`
	CandidateActionRequired int `json:"candidate_action_required"`
	Interview               int `json:"interview"`
	Offer                   int `json:"offer"`
	Rejected                int `json:"rejected"`
}

func (s *ConversationStore) GetConversationStats() ConversationStats {
	var result ConversationStats
	if s == nil {
		return result
	}
	if s.selectedRepository() != nil {
		_ = s.syncCompatibilityState()
	}
	result.Total = len(s.conversations)
	for _, value := range s.conversations {
		switch value.Status {
		case ConversationWaitingEmployer:
			result.WaitingEmployer++
		case ConversationCandidateActionRequired:
			result.CandidateActionRequired++
		case ConversationInterview:
			result.Interview++
		case ConversationOffer:
			result.Offer++
		case ConversationRejected:
			result.Rejected++
		}
	}
	return result
}
