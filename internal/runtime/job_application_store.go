package runtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/ports"
)

var (
	ErrApplicationNotFound = jsonstorage.ErrApplicationNotFound
	ErrDuplicateExternalID = jsonstorage.ErrDuplicateExternalID
)

// ApplicationStore is the legacy root façade. JSON persistence, locking,
// validation, IDs and event storage belong to repository; the root keeps only
// compatibility mirrors plus the higher-level context/read-model dependencies.
type ApplicationStore struct {
	path             string
	repository       *jsonstorage.ApplicationRepository
	careerRepository ports.ApplicationStore

	// Deprecated compatibility mirrors. They are synchronized with repository
	// for old root-package workflows that still inspect these fields directly.
	applications []JobApplication
	events       []ApplicationEvent
	mirrorApps   []JobApplication
	mirrorEvents []ApplicationEvent

	compatibilityMu   sync.Mutex
	conversationStore *ConversationStore
	resolver          *CandidateContextResolver
}

// NewApplicationStore constructs a JSON application store without hidden
// dependency discovery. Context collaborators are attached explicitly by
// NewApplicationStoreWithDependencies when a legacy read-model needs them.
func NewApplicationStore(path string) *ApplicationStore {
	repository := jsonstorage.NewApplicationRepository(path)
	s := &ApplicationStore{path: path, repository: repository, applications: []JobApplication{}, events: []ApplicationEvent{}, mirrorApps: []JobApplication{}, mirrorEvents: []ApplicationEvent{}}
	return s
}

func newApplicationStoreFromRepository(repository ports.ApplicationStore) *ApplicationStore {
	return &ApplicationStore{careerRepository: repository, applications: []JobApplication{}, events: []ApplicationEvent{}, mirrorApps: []JobApplication{}, mirrorEvents: []ApplicationEvent{}}
}

func (s *ApplicationStore) selectedRepository() ports.ApplicationStore {
	if s == nil {
		return nil
	}
	if s.careerRepository != nil {
		return s.careerRepository
	}
	return s.repository
}

func NewApplicationStoreWithDependencies(path string, conversations *ConversationStore, resolver *CandidateContextResolver) *ApplicationStore {
	s := NewApplicationStore(path)
	s.conversationStore = conversations
	s.resolver = resolver
	return s
}

func (s *ApplicationStore) SetConversationStore(store *ConversationStore) {
	if s != nil {
		s.conversationStore = store
	}
}

func (s *ApplicationStore) SetCandidateContextResolver(resolver *CandidateContextResolver) {
	if s != nil {
		s.resolver = resolver
	}
}

func (s *ApplicationStore) syncCompatibilityState() error {
	if s != nil && s.careerRepository != nil {
		return s.refreshCompatibilityMirror()
	}
	if s == nil || s.repository == nil {
		return errors.New("application store is not configured")
	}
	s.compatibilityMu.Lock()
	defer s.compatibilityMu.Unlock()
	applications, events, err := s.repository.Snapshot(nil)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(s.applications, s.mirrorApps) || !reflect.DeepEqual(s.events, s.mirrorEvents) {
		if err := s.repository.ReplaceSnapshot(nil, s.applications, s.events); err != nil {
			return err
		}
		applications, events, err = s.repository.Snapshot(nil)
		if err != nil {
			return err
		}
	}
	s.applications, s.events = applications, events
	s.mirrorApps, s.mirrorEvents = cloneApplicationMirror(applications), append([]ApplicationEvent{}, events...)
	return nil
}

func (s *ApplicationStore) refreshCompatibilityMirror() error {
	if s != nil && s.careerRepository != nil {
		applications, err := s.careerRepository.List(nil)
		if err != nil {
			return err
		}
		events := []ApplicationEvent{}
		if eventReader, ok := s.careerRepository.(interface {
			ListEvents(context.Context) ([]ApplicationEvent, error)
		}); ok {
			events, err = eventReader.ListEvents(nil)
			if err != nil {
				return err
			}
		}
		s.compatibilityMu.Lock()
		s.applications, s.events = applications, events
		s.mirrorApps, s.mirrorEvents = cloneApplicationMirror(applications), append([]ApplicationEvent{}, events...)
		s.compatibilityMu.Unlock()
		return nil
	}
	if s == nil || s.repository == nil {
		return errors.New("application store is not configured")
	}
	applications, events, err := s.repository.Snapshot(nil)
	if err != nil {
		return err
	}
	s.compatibilityMu.Lock()
	s.applications, s.events = applications, events
	s.mirrorApps, s.mirrorEvents = cloneApplicationMirror(applications), append([]ApplicationEvent{}, events...)
	s.compatibilityMu.Unlock()
	return nil
}

func (s *ApplicationStore) Load() error {
	if s != nil && s.careerRepository != nil {
		return s.refreshCompatibilityMirror()
	}
	if s == nil || s.repository == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("application store requires a path")
	}
	if err := s.repository.Load(); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) Save() error {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Save(nil)
	}
	if s == nil || s.repository == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("application store requires a path")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	return s.repository.Save(nil)
}

func (s *ApplicationStore) saveUnlocked() error {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Save(nil)
	}
	if s == nil || s.repository == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("application store requires a path")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	return s.repository.SaveUnlocked(nil)
}

func (s *ApplicationStore) CreateApplication(value JobApplication) (JobApplication, error) {
	if s != nil && s.careerRepository != nil {
		created, err := s.careerRepository.Create(nil, value)
		if err == nil {
			err = s.refreshCompatibilityMirror()
		}
		return created, err
	}
	if err := s.syncCompatibilityState(); err != nil {
		return JobApplication{}, err
	}
	created, err := s.repository.Create(nil, value)
	if err != nil {
		return JobApplication{}, err
	}
	if err := s.refreshCompatibilityMirror(); err != nil {
		return JobApplication{}, err
	}
	return created, nil
}

func (s *ApplicationStore) GetApplication(id string) (JobApplication, error) {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Get(nil, id)
	}
	if err := s.syncCompatibilityState(); err != nil {
		return JobApplication{}, ErrApplicationNotFound
	}
	return s.repository.Get(nil, id)
}

func (s *ApplicationStore) GetByExternalID(externalID string) (JobApplication, error) {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.GetByExternalID(nil, externalID)
	}
	if err := s.syncCompatibilityState(); err != nil {
		return JobApplication{}, ErrApplicationNotFound
	}
	return s.repository.GetByExternalID(nil, externalID)
}

func (s *ApplicationStore) UpdateApplication(value JobApplication) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.Update(nil, value); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.Update(nil, value); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) UpsertImportedApplication(value JobApplication) (JobApplication, bool, error) {
	if s != nil && s.careerRepository != nil {
		result, created, err := s.careerRepository.UpsertImported(nil, value)
		if err == nil {
			err = s.refreshCompatibilityMirror()
		}
		return result, created, err
	}
	if err := s.syncCompatibilityState(); err != nil {
		return JobApplication{}, false, err
	}
	result, created, err := s.repository.UpsertImported(nil, value)
	if err != nil {
		return JobApplication{}, false, err
	}
	if err := s.refreshCompatibilityMirror(); err != nil {
		return JobApplication{}, false, err
	}
	return result, created, nil
}

func (s *ApplicationStore) AttachImportedConversation(externalID, conversationID string) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.AttachImportedConversation(nil, externalID, conversationID); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.AttachImportedConversation(nil, externalID, conversationID); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) ListApplications() ([]JobApplication, error) {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.List(nil)
	}
	if err := s.syncCompatibilityState(); err != nil {
		return []JobApplication{}, err
	}
	return s.repository.List(nil)
}

// ListApplicationsForDashboard is a read-only compatibility projection. It
// deliberately avoids syncCompatibilityState because that method may persist
// legacy mirror edits. Dashboard reads must not turn a GET into a write.
func (s *ApplicationStore) ListApplicationsForDashboard() ([]JobApplication, error) {
	if s == nil {
		return []JobApplication{}, errors.New("application store is not configured")
	}
	if s.careerRepository != nil {
		return s.careerRepository.List(nil)
	}
	if s.repository == nil {
		return []JobApplication{}, errors.New("application store is not configured")
	}
	s.compatibilityMu.Lock()
	defer s.compatibilityMu.Unlock()
	if !reflect.DeepEqual(s.applications, s.mirrorApps) {
		return cloneApplicationMirror(s.applications), nil
	}
	return s.repository.List(nil)
}

func (s *ApplicationStore) UpdateStatus(id string, status ApplicationStatus) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.UpdateStatus(nil, id, status); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.UpdateStatus(nil, id, status); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) RecordEvent(applicationID string, timestamp time.Time, eventType ApplicationEventType, description string) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.AppendEvent(nil, applicationID, timestamp, eventType, description); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.AppendEvent(nil, applicationID, timestamp, eventType, description); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) SetFollowUpState(applicationID string, state ConversationFollowUpState) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.SetFollowUpState(nil, applicationID, state); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.SetFollowUpState(nil, applicationID, state); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) AttachConversation(applicationID, conversationID string) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.AttachConversation(nil, applicationID, conversationID); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	application, err := s.repository.Get(nil, applicationID)
	if err != nil {
		return err
	}
	if s.conversationStore != nil {
		conversation, getErr := s.conversationStore.GetConversation(conversationID)
		if getErr != nil {
			return getErr
		}
		if conversation.VacancyID != application.VacancyID {
			return errors.New("conversation vacancy does not match application vacancy")
		}
	}
	if err := s.repository.AttachConversation(nil, applicationID, conversationID); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) SaveMatchResult(applicationID string, result MatchResult) error {
	if s != nil && s.careerRepository != nil {
		if err := s.careerRepository.SaveMatchResult(nil, applicationID, result); err != nil {
			return err
		}
		return s.refreshCompatibilityMirror()
	}
	if err := s.syncCompatibilityState(); err != nil {
		return err
	}
	if err := s.repository.SaveMatchResult(nil, applicationID, result); err != nil {
		return err
	}
	return s.refreshCompatibilityMirror()
}

func (s *ApplicationStore) GetApplicationTimeline(applicationID string) ([]ApplicationEvent, error) {
	if s != nil && s.careerRepository != nil {
		return s.careerRepository.Timeline(nil, applicationID)
	}
	if err := s.syncCompatibilityState(); err != nil {
		return nil, err
	}
	return s.repository.Timeline(nil, applicationID)
}

func (s *ApplicationStore) ListApplicationEvents(applicationID string) ([]ApplicationEvent, error) {
	return s.GetApplicationTimeline(applicationID)
}

func (s *ApplicationStore) ListEvents() ([]ApplicationEvent, error) {
	if s != nil && s.careerRepository != nil {
		if eventReader, ok := s.careerRepository.(interface {
			ListEvents(context.Context) ([]ApplicationEvent, error)
		}); ok {
			return eventReader.ListEvents(nil)
		}
		return []ApplicationEvent{}, nil
	}
	if err := s.syncCompatibilityState(); err != nil {
		return []ApplicationEvent{}, err
	}
	return s.repository.ListEvents(nil)
}

// ListEventsForDashboard is the non-persisting counterpart of ListEvents for
// request-scoped dashboard snapshots.
func (s *ApplicationStore) ListEventsForDashboard() ([]ApplicationEvent, error) {
	if s == nil {
		return []ApplicationEvent{}, errors.New("application store is not configured")
	}
	if s.careerRepository != nil {
		if eventReader, ok := s.careerRepository.(interface {
			ListEvents(context.Context) ([]ApplicationEvent, error)
		}); ok {
			return eventReader.ListEvents(nil)
		}
		return []ApplicationEvent{}, nil
	}
	if s.repository == nil {
		return []ApplicationEvent{}, errors.New("application store is not configured")
	}
	s.compatibilityMu.Lock()
	defer s.compatibilityMu.Unlock()
	if !reflect.DeepEqual(s.events, s.mirrorEvents) {
		return append([]ApplicationEvent{}, s.events...), nil
	}
	return s.repository.ListEvents(nil)
}

func (s *ApplicationStore) GetApplicationStats() ApplicationStats {
	if s != nil && s.careerRepository != nil {
		if err := s.refreshCompatibilityMirror(); err != nil {
			return ApplicationStats{}
		}
	}
	if s == nil || s.syncCompatibilityState() != nil {
		return ApplicationStats{}
	}
	s.compatibilityMu.Lock()
	defer s.compatibilityMu.Unlock()
	var result ApplicationStats
	result.Total = len(s.applications)
	responses := 0
	for _, application := range s.applications {
		switch application.Status {
		case ApplicationDiscovered:
			result.Discovered++
		case ApplicationApplied:
			result.Applied++
			result.WaitingReply++
		case ApplicationEmployerReplied:
			result.Applied++
			responses++
		case ApplicationInterview:
			result.Applied++
			result.Interviews++
			responses++
		case ApplicationOffer:
			result.Applied++
			result.Offers++
			responses++
		case ApplicationRejected:
			result.Applied++
			result.Rejected++
			responses++
		case ApplicationArchived:
			if applicationHasEvent(s.events, application.ID, ApplicationEventApplied) {
				result.Applied++
			}
		}
	}
	if result.Applied > 0 {
		result.ResponseRate = float64(responses) * 100 / float64(result.Applied)
		result.ConversionRate = float64(result.Interviews) * 100 / float64(result.Applied)
	}
	return result
}

func applicationHasEvent(events []ApplicationEvent, applicationID string, eventType ApplicationEventType) bool {
	for _, event := range events {
		if event.ApplicationID == applicationID && event.Type == eventType {
			return true
		}
	}
	return false
}

// Postgres still uses this root compatibility helper until its boundary stage.
func newApplicationEvent(applicationID string, timestamp time.Time, eventType ApplicationEventType, description string) (ApplicationEvent, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ApplicationEvent{}, fmt.Errorf("generate application event id: %w", err)
	}
	return ApplicationEvent{ID: fmt.Sprintf("application-event-%x", value), ApplicationID: applicationID, Timestamp: timestamp, Type: eventType, Description: description}, nil
}

func applicationEquivalent(a, b JobApplication) bool {
	return a.FollowUpState == b.FollowUpState && a.ID == b.ID && a.VacancyID == b.VacancyID && a.ExternalID == b.ExternalID &&
		a.CompanyName == b.CompanyName && a.VacancyTitle == b.VacancyTitle && a.VacancyURL == b.VacancyURL &&
		a.Source == b.Source && a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && a.Status == b.Status &&
		reflect.DeepEqual(a.MatchResult, b.MatchResult) && a.ConversationID == b.ConversationID &&
		a.Notes == b.Notes && a.NextAction == b.NextAction && a.RawStatus == b.RawStatus &&
		reflect.DeepEqual(a.HHMetadata, b.HHMetadata) && a.Partial == b.Partial && a.DataCompleteness == b.DataCompleteness &&
		reflect.DeepEqual(a.ReconciliationEvidence, b.ReconciliationEvidence)
}

func cloneApplicationMirror(values []JobApplication) []JobApplication {
	if values == nil {
		return []JobApplication{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return append([]JobApplication{}, values...)
	}
	var result []JobApplication
	if err := json.Unmarshal(raw, &result); err != nil {
		return append([]JobApplication{}, values...)
	}
	return result
}

func (s *ApplicationStore) cloneForSync() (*ApplicationStore, error) {
	if s != nil && s.careerRepository != nil {
		return nil, errors.New("application sync clone is not supported by the career repository")
	}
	if err := s.syncCompatibilityState(); err != nil {
		return nil, err
	}
	repository, err := s.repository.Clone()
	if err != nil {
		return nil, err
	}
	clone := &ApplicationStore{path: s.path, repository: repository, conversationStore: s.conversationStore, resolver: s.resolver, applications: []JobApplication{}, events: []ApplicationEvent{}, mirrorApps: []JobApplication{}, mirrorEvents: []ApplicationEvent{}}
	if err := clone.refreshCompatibilityMirror(); err != nil {
		return nil, err
	}
	return clone, nil
}

func (s *ApplicationStore) GetApplicationContext(applicationID string) (ApplicationContext, error) {
	if s == nil {
		return ApplicationContext{}, errors.New("application context requires an application store")
	}
	return buildApplicationContext(s, s.conversationStore, s.resolver, applicationID)
}

type ApplicationContextBuilder struct {
	applications      *ApplicationStore
	conversationStore *ConversationStore
	resolver          *CandidateContextResolver
	semanticRetriever CandidateSemanticRetriever
}

func NewApplicationContextBuilder(applications *ApplicationStore, conversations *ConversationStore, resolver *CandidateContextResolver, retrievers ...CandidateSemanticRetriever) *ApplicationContextBuilder {
	builder := &ApplicationContextBuilder{applications: applications, conversationStore: conversations, resolver: resolver}
	if len(retrievers) > 0 {
		builder.semanticRetriever = retrievers[0]
	}
	return builder
}

func (b *ApplicationContextBuilder) Build(applicationID string) (ApplicationContext, error) {
	if b == nil {
		return ApplicationContext{}, errors.New("application context builder is nil")
	}
	return buildApplicationContext(b.applications, b.conversationStore, b.resolver, applicationID)
}

func buildApplicationContext(applications *ApplicationStore, conversations *ConversationStore, resolver *CandidateContextResolver, applicationID string) (ApplicationContext, error) {
	if applications == nil {
		return ApplicationContext{}, errors.New("application context requires an application store")
	}
	application, err := applications.GetApplication(applicationID)
	if err != nil {
		return ApplicationContext{}, err
	}
	result := ApplicationContext{Application: application, CandidateContext: emptyCandidateContext()}
	result.Timeline, err = applications.GetApplicationTimeline(applicationID)
	if err != nil {
		return ApplicationContext{}, err
	}
	if application.MatchResult != nil {
		result.MatchResult, err = cloneKnowledge(*application.MatchResult)
		if err != nil {
			return ApplicationContext{}, errors.New("cannot copy match result")
		}
	}
	if application.ConversationID != "" {
		if conversations == nil {
			return ApplicationContext{}, errors.New("application conversation store is not configured")
		}
		result.Conversation, err = conversations.GetConversation(application.ConversationID)
		if err != nil {
			return ApplicationContext{}, err
		}
		if result.Conversation.VacancyID != application.VacancyID {
			return ApplicationContext{}, errors.New("conversation vacancy does not match application vacancy")
		}
	}
	if resolver != nil {
		result.CandidateContext, err = resolver.ResolveForVacancy(Vacancy{ID: application.VacancyID, Name: application.VacancyTitle, Company: Company{Name: application.CompanyName}, Links: map[string]string{"desktop": application.VacancyURL}})
		if err != nil {
			return ApplicationContext{}, err
		}
	}
	return result, nil
}
