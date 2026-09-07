package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

var (
	ErrApplicationNotFound = errors.New("application not found")
	ErrDuplicateExternalID = errors.New("duplicate application external id")
)

// ApplicationStore owns detached snapshots. Mutations change memory only;
// Save is explicit. This store has no HH/API or AI side effects.
type ApplicationStore struct {
	path              string
	applications      []JobApplication
	events            []ApplicationEvent
	conversationStore *ConversationStore
	resolver          *CandidateContextResolver
}

type applicationStoreFile struct {
	Version      int                `json:"version"`
	Applications []JobApplication   `json:"applications"`
	Events       []ApplicationEvent `json:"events"`
}

// Optional dependencies make the basic store usable with the same one-path
// constructor as the other stores, while allowing a context aggregate to be
// built directly when they are supplied.
func NewApplicationStore(path string, dependencies ...any) *ApplicationStore {
	s := &ApplicationStore{path: path, applications: []JobApplication{}, events: []ApplicationEvent{}}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case *ConversationStore:
			s.conversationStore = value
		case *CandidateContextResolver:
			s.resolver = value
		}
	}
	return s
}

func NewApplicationStoreWithDependencies(path string, conversations *ConversationStore, resolver *CandidateContextResolver) *ApplicationStore {
	return NewApplicationStore(path, conversations, resolver)
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

func (s *ApplicationStore) Load() error {
	start := time.Now()
	defer perfRecord("store.ApplicationStore.Load", start, 1)
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("application store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.applications = []JobApplication{}
		s.events = []ApplicationEvent{}
		return nil
	}
	if err != nil {
		return errors.New("cannot read application store")
	}
	if profileContainsSecret(raw) {
		return errors.New("application store contains a forbidden secret marker")
	}
	var file applicationStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Applications == nil {
		return errors.New("invalid application store: expected version 1 and applications array")
	}
	// Events were introduced together with the pipeline, but treating an
	// omitted events field as an empty timeline keeps hand-authored stores
	// readable. An explicit null applications field remains invalid above.
	if file.Events == nil {
		file.Events = []ApplicationEvent{}
	}
	if err := validateApplicationCollections(file.Applications, file.Events); err != nil {
		return err
	}
	s.applications = file.Applications
	s.events = file.Events
	return nil
}

func (s *ApplicationStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("application store requires a path")
	}
	if err := validateApplicationCollections(s.applications, s.events); err != nil {
		return err
	}
	return withStoreLock(s.path, func() error { return s.saveUnlocked() })
}

func (s *ApplicationStore) saveUnlocked() error {
	start := time.Now()
	defer perfRecord("store.ApplicationStore.saveUnlocked", start, 1)
	applications := s.applications
	events := s.events
	if applications == nil {
		applications = []JobApplication{}
	}
	if events == nil {
		events = []ApplicationEvent{}
	}
	raw, err := json.MarshalIndent(applicationStoreFile{Version: 1, Applications: applications, Events: events}, "", "  ")
	if err != nil {
		return errors.New("cannot encode application store")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("cannot create application directory")
	}
	tmp, err := os.CreateTemp(dir, ".job_applications-*.tmp")
	if err != nil {
		return errors.New("cannot stage application store")
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(raw, '\n')); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write application store")
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return errors.New("cannot replace application store")
	}
	return nil
}

func validateApplicationCollections(applications []JobApplication, events []ApplicationEvent) error {
	ids := make(map[string]bool, len(applications))
	externalIDs := make(map[string]bool, len(applications))
	for i, application := range applications {
		if err := application.validate(); err != nil {
			return fmt.Errorf("application %d: %w", i+1, err)
		}
		if ids[application.ID] {
			return errors.New("duplicate application id")
		}
		ids[application.ID] = true
		if application.ExternalID != "" {
			if externalIDs[application.ExternalID] {
				return ErrDuplicateExternalID
			}
			externalIDs[application.ExternalID] = true
		}
	}
	eventIDs := make(map[string]bool, len(events))
	for i, event := range events {
		if err := event.validate(); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
		if !ids[event.ApplicationID] {
			return errors.New("application event references an unknown application")
		}
		if eventIDs[event.ID] {
			return errors.New("duplicate application event id")
		}
		eventIDs[event.ID] = true
	}
	return nil
}

func (s *ApplicationStore) CreateApplication(value JobApplication) (JobApplication, error) {
	if s == nil {
		return JobApplication{}, errors.New("application store is nil")
	}
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("application")
		if err != nil {
			return JobApplication{}, err
		}
	}
	if value.Source == "" {
		value.Source = ApplicationSourceManual
	}
	if value.Status == "" {
		value.Status = ApplicationDiscovered
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if value.MatchResult != nil {
		copyResult, err := cloneKnowledge(*value.MatchResult)
		if err != nil {
			return JobApplication{}, errors.New("cannot copy match result")
		}
		copyResult.normalize()
		value.MatchResult = &copyResult
	}
	if err := value.validate(); err != nil {
		return JobApplication{}, err
	}
	for _, existing := range s.applications {
		if existing.ID == value.ID {
			return JobApplication{}, errors.New("application id already exists")
		}
		if value.ExternalID != "" && existing.ExternalID == value.ExternalID {
			return JobApplication{}, ErrDuplicateExternalID
		}
	}
	event, err := newApplicationEvent(value.ID, value.CreatedAt, ApplicationEventCreated, "application created")
	if err != nil {
		return JobApplication{}, err
	}
	nextApplications := append(append([]JobApplication{}, s.applications...), value)
	nextEvents := append(append([]ApplicationEvent{}, s.events...), event)
	if err := validateApplicationCollections(nextApplications, nextEvents); err != nil {
		return JobApplication{}, err
	}
	s.applications, s.events = nextApplications, nextEvents
	return cloneKnowledge(value)
}

func (s *ApplicationStore) GetApplication(id string) (JobApplication, error) {
	if s != nil {
		for _, application := range s.applications {
			if application.ID == id {
				return cloneKnowledge(application)
			}
		}
	}
	return JobApplication{}, ErrApplicationNotFound
}

// GetByExternalID returns an imported HH application without exposing store
// internals. An external ID is the stable identity for read-only imports.
func (s *ApplicationStore) GetByExternalID(externalID string) (JobApplication, error) {
	if s != nil && strings.TrimSpace(externalID) != "" {
		for _, application := range s.applications {
			if application.ExternalID == externalID {
				return cloneKnowledge(application)
			}
		}
	}
	return JobApplication{}, ErrApplicationNotFound
}

// UpdateApplication replaces one detached application snapshot without
// creating an event. Callers that represent a business transition must append
// an explicit event separately; this keeps repository updates from silently
// changing the append-only event history contract.
func (s *ApplicationStore) UpdateApplication(value JobApplication) error {
	if s == nil {
		return errors.New("application store is nil")
	}
	if strings.TrimSpace(value.ID) == "" {
		return errors.New("application update requires an id")
	}
	for i, old := range s.applications {
		if old.ID != value.ID {
			continue
		}
		if value.CreatedAt.IsZero() {
			value.CreatedAt = old.CreatedAt
		}
		if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = old.UpdatedAt
			if value.UpdatedAt.Before(value.CreatedAt) {
				value.UpdatedAt = value.CreatedAt
			}
		}
		for j, other := range s.applications {
			if i != j && value.ExternalID != "" && other.ExternalID == value.ExternalID {
				return ErrDuplicateExternalID
			}
		}
		if err := value.validate(); err != nil {
			return err
		}
		next := append([]JobApplication{}, s.applications...)
		next[i] = value
		if err := validateApplicationCollections(next, s.events); err != nil {
			return err
		}
		s.applications = next
		return nil
	}
	return ErrApplicationNotFound
}

// UpsertImportedApplication imports an already-existing HH negotiation. It
// never calls HH and does not turn an import into a new application action.
func (s *ApplicationStore) UpsertImportedApplication(value JobApplication) (JobApplication, bool, error) {
	if s == nil {
		return JobApplication{}, false, errors.New("application store is nil")
	}
	if strings.TrimSpace(value.ExternalID) == "" {
		return JobApplication{}, false, errors.New("imported application requires an external id")
	}
	incomingUpdatedAt := value.UpdatedAt
	if value.Source == "" {
		value.Source = ApplicationSourceHH
	}
	if value.Status == "" {
		value.Status = ApplicationUnknown
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		value.UpdatedAt = value.CreatedAt
	}
	if value.HHMetadata == nil {
		value.HHMetadata = map[string]string{}
	}
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("application")
		if err != nil {
			return JobApplication{}, false, err
		}
	}
	if value.MatchResult != nil {
		value.MatchResult.normalize()
	}
	if err := value.validate(); err != nil {
		return JobApplication{}, false, err
	}
	for i, old := range s.applications {
		if old.ExternalID != value.ExternalID {
			continue
		}
		value.ID = old.ID
		value.CreatedAt = old.CreatedAt
		if incomingUpdatedAt.IsZero() || value.UpdatedAt.Before(old.UpdatedAt) {
			value.UpdatedAt = old.UpdatedAt
		}
		if applicationEquivalent(old, value) {
			copy, copyErr := cloneKnowledge(old)
			return copy, false, copyErr
		}
		next := append([]JobApplication{}, s.applications...)
		next[i] = value
		if err := validateApplicationCollections(next, s.events); err != nil {
			return JobApplication{}, false, err
		}
		s.applications = next
		copy, copyErr := cloneKnowledge(value)
		return copy, false, copyErr
	}
	created, err := s.CreateApplication(value)
	if err != nil {
		return JobApplication{}, false, err
	}
	return created, true, nil
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

// AttachImportedConversation links an HH conversation after both read-only
// collections have been imported. It has no HH side effects.
func (s *ApplicationStore) AttachImportedConversation(externalID, conversationID string) error {
	application, err := s.GetByExternalID(externalID)
	if err != nil {
		return err
	}
	if application.ConversationID == conversationID {
		return nil
	}
	application.ConversationID = conversationID
	application.UpdatedAt = time.Now().UTC()
	for i, old := range s.applications {
		if old.ID == application.ID {
			next := append([]JobApplication{}, s.applications...)
			next[i] = application
			if err := validateApplicationCollections(next, s.events); err != nil {
				return err
			}
			s.applications = next
			return nil
		}
	}
	return ErrApplicationNotFound
}

func (s *ApplicationStore) ListApplications() ([]JobApplication, error) {
	result := []JobApplication{}
	if s != nil {
		result = append(result, s.applications...)
	}
	return cloneKnowledge(result)
}

func (s *ApplicationStore) UpdateStatus(id string, status ApplicationStatus) error {
	if !applicationStatuses[status] {
		return errors.New("invalid application status")
	}
	if s == nil {
		return errors.New("application store is nil")
	}
	index := -1
	for i := range s.applications {
		if s.applications[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrApplicationNotFound
	}
	if s.applications[index].Status == status {
		return nil
	}
	updated := s.applications[index]
	updated.Status = status
	updated.UpdatedAt = time.Now().UTC()
	if updated.UpdatedAt.Before(updated.CreatedAt) {
		updated.UpdatedAt = updated.CreatedAt
	}
	eventType := applicationEventTypeForStatus(status)
	event, err := newApplicationEvent(id, updated.UpdatedAt, eventType, fmt.Sprintf("status changed from %q to %q", s.applications[index].Status, status))
	if err != nil {
		return err
	}
	nextApplications := append([]JobApplication{}, s.applications...)
	nextApplications[index] = updated
	nextEvents := append(append([]ApplicationEvent{}, s.events...), event)
	if err := validateApplicationCollections(nextApplications, nextEvents); err != nil {
		return err
	}
	s.applications, s.events = nextApplications, nextEvents
	return nil
}

func applicationEventTypeForStatus(status ApplicationStatus) ApplicationEventType {
	switch status {
	case ApplicationApplied:
		return ApplicationEventApplied
	case ApplicationInterview:
		return ApplicationEventInterviewScheduled
	case ApplicationOffer:
		return ApplicationEventOfferReceived
	case ApplicationRejected:
		return ApplicationEventRejected
	default:
		return ApplicationEventStatusChanged
	}
}

func newApplicationEvent(applicationID string, timestamp time.Time, eventType ApplicationEventType, description string) (ApplicationEvent, error) {
	id, err := newKnowledgeID("application-event")
	if err != nil {
		return ApplicationEvent{}, err
	}
	return ApplicationEvent{ID: id, ApplicationID: applicationID, Timestamp: timestamp, Type: eventType, Description: description}, nil
}

// RecordEvent is used by controlled write flows after HH confirms delivery.
// It never performs an HH request and keeps the application timeline explicit.
func (s *ApplicationStore) RecordEvent(applicationID string, timestamp time.Time, eventType ApplicationEventType, description string) error {
	if s == nil {
		return errors.New("application store is nil")
	}
	if _, err := s.GetApplication(applicationID); err != nil {
		return err
	}
	event, err := newApplicationEvent(applicationID, timestamp, eventType, description)
	if err != nil {
		return err
	}
	next := append(append([]ApplicationEvent{}, s.events...), event)
	if err := validateApplicationCollections(s.applications, next); err != nil {
		return err
	}
	s.events = next
	return nil
}

func (s *ApplicationStore) SetFollowUpState(applicationID string, state ConversationFollowUpState) error {
	if s == nil {
		return errors.New("application store is nil")
	}
	if state == "" {
		return errors.New("follow-up state is required")
	}
	for i := range s.applications {
		if s.applications[i].ID != applicationID {
			continue
		}
		updated := s.applications[i]
		updated.FollowUpState = state
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		next := append([]JobApplication{}, s.applications...)
		next[i] = updated
		if err := validateApplicationCollections(next, s.events); err != nil {
			return err
		}
		s.applications = next
		return nil
	}
	return ErrApplicationNotFound
}

func (s *ApplicationStore) AttachConversation(applicationID, conversationID string) error {
	if s == nil {
		return errors.New("application store is nil")
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("conversation id is required")
	}
	application, err := s.GetApplication(applicationID)
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
	if application.ConversationID == conversationID {
		return nil
	}
	application.ConversationID = conversationID
	application.UpdatedAt = time.Now().UTC()
	if application.UpdatedAt.Before(application.CreatedAt) {
		application.UpdatedAt = application.CreatedAt
	}
	for i := range s.applications {
		if s.applications[i].ID == applicationID {
			next := append([]JobApplication{}, s.applications...)
			next[i] = application
			if err := validateApplicationCollections(next, s.events); err != nil {
				return err
			}
			s.applications = next
			return nil
		}
	}
	return ErrApplicationNotFound
}

func (s *ApplicationStore) SaveMatchResult(applicationID string, result MatchResult) error {
	if err := result.validate(); err != nil {
		return err
	}
	if s == nil {
		return errors.New("application store is nil")
	}
	for i, old := range s.applications {
		if old.ID != applicationID {
			continue
		}
		result.normalize()
		if old.MatchResult != nil && reflect.DeepEqual(*old.MatchResult, result) {
			return nil
		}
		copyResult, err := cloneKnowledge(result)
		if err != nil {
			return err
		}
		copyResult.normalize()
		updated := old
		updated.MatchResult = &copyResult
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		event, err := newApplicationEvent(applicationID, updated.UpdatedAt, ApplicationEventMatched, "vacancy match result saved")
		if err != nil {
			return err
		}
		nextApplications := append([]JobApplication{}, s.applications...)
		nextApplications[i] = updated
		nextEvents := append(append([]ApplicationEvent{}, s.events...), event)
		if err := validateApplicationCollections(nextApplications, nextEvents); err != nil {
			return err
		}
		s.applications, s.events = nextApplications, nextEvents
		return nil
	}
	return ErrApplicationNotFound
}

func (s *ApplicationStore) GetApplicationTimeline(applicationID string) ([]ApplicationEvent, error) {
	if _, err := s.GetApplication(applicationID); err != nil {
		return nil, err
	}
	result := []ApplicationEvent{}
	if s != nil {
		for _, event := range s.events {
			if event.ApplicationID == applicationID {
				result = append(result, event)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })
	return cloneKnowledge(result)
}

func (s *ApplicationStore) ListApplicationEvents(applicationID string) ([]ApplicationEvent, error) {
	return s.GetApplicationTimeline(applicationID)
}

// ListEvents returns a detached copy of the complete event ledger for
// read-only projections. Business code must use RecordEvent for mutations.
func (s *ApplicationStore) ListEvents() ([]ApplicationEvent, error) {
	if s == nil {
		return []ApplicationEvent{}, nil
	}
	return cloneKnowledge(append([]ApplicationEvent{}, s.events...))
}

func (s *ApplicationStore) GetApplicationStats() ApplicationStats {
	var result ApplicationStats
	if s == nil {
		return result
	}
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
		result.CandidateContext, err = resolver.ResolveForVacancy(Vacancy{
			ID:      application.VacancyID,
			Name:    application.VacancyTitle,
			Company: Company{Name: application.CompanyName},
			Links:   map[string]string{"desktop": application.VacancyURL},
		})
		if err != nil {
			return ApplicationContext{}, err
		}
	}
	return result, nil
}
