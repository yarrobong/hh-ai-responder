package jsonstorage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

const ApplicationsFilename = "job_applications.json"

var (
	ErrApplicationNotFound    = application.ErrApplicationNotFound
	ErrDuplicateExternalID    = application.ErrDuplicateExternalID
	ErrDuplicateApplicationID = application.ErrDuplicateApplicationID
)

type applicationStoreFile struct {
	Version      int                          `json:"version"`
	Applications []application.JobApplication `json:"applications"`
	Events       []application.Event          `json:"events"`
}

// ApplicationRepository is the authoritative JSON persistence implementation
// for applications. It stores only application values and their event ledger;
// conversation and candidate context are resolved by higher-level callers.
type ApplicationRepository struct {
	mu           sync.RWMutex
	path         string
	applications []application.JobApplication
	events       []application.Event
}

func NewApplicationRepository(path string) *ApplicationRepository {
	if strings.TrimSpace(path) == "" {
		path = ApplicationsFilename
	}
	return &ApplicationRepository{path: path, applications: []application.JobApplication{}, events: []application.Event{}}
}

func (r *ApplicationRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application store requires a path")
	}
	raw, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.mu.Lock()
		r.applications = []application.JobApplication{}
		r.events = []application.Event{}
		r.mu.Unlock()
		return nil
	}
	if err != nil {
		return errors.New("cannot read application store")
	}
	if containsApplicationSecret(raw) {
		return errors.New("application store contains a forbidden secret marker")
	}

	var file applicationStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(struct{})) != io.EOF || file.Version != 1 || file.Applications == nil {
		return errors.New("invalid application store: expected version 1 and applications array")
	}
	if file.Events == nil {
		file.Events = []application.Event{}
	}
	if err := ValidateApplicationCollections(file.Applications, file.Events); err != nil {
		return err
	}
	applications, err := cloneApplications(file.Applications)
	if err != nil {
		return err
	}
	events, err := cloneEvents(file.Events)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.applications = applications
	r.events = events
	r.mu.Unlock()
	return nil
}

func (r *ApplicationRepository) Save(ctx context.Context) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application store requires a path")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return fmt.Errorf("create application directory: %w", err)
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		return r.SaveUnlocked(ctx)
	})
}

// SaveUnlocked preserves the existing batch-sync lock choreography. Callers
// must hold the process lock when using it directly.
func (r *ApplicationRepository) SaveUnlocked(ctx context.Context) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("application store requires a path")
	}
	r.mu.RLock()
	applications, err := cloneApplications(r.applications)
	if err == nil {
		var events []application.Event
		events, err = cloneEvents(r.events)
		if err == nil {
			err = ValidateApplicationCollections(applications, events)
			if err == nil {
				raw, marshalErr := json.MarshalIndent(applicationStoreFile{Version: 1, Applications: applications, Events: events}, "", "  ")
				if marshalErr != nil {
					err = errors.New("cannot encode application store")
				} else if containsApplicationSecret(raw) {
					err = errors.New("application store would contain a forbidden secret marker")
				} else {
					err = platform.WritePrivateFileAtomic(r.path, append(raw, '\n'), ".job_applications-*.tmp")
				}
			}
		}
	}
	r.mu.RUnlock()
	return err
}

func (r *ApplicationRepository) Get(ctx context.Context, id string) (application.JobApplication, error) {
	if err := applicationContextError(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if r == nil {
		return application.JobApplication{}, ErrApplicationNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, value := range r.applications {
		if value.ID == id {
			return cloneApplication(value)
		}
	}
	return application.JobApplication{}, ErrApplicationNotFound
}

func (r *ApplicationRepository) GetByExternalID(ctx context.Context, externalID string) (application.JobApplication, error) {
	if err := applicationContextError(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if r == nil {
		return application.JobApplication{}, ErrApplicationNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if strings.TrimSpace(externalID) == "" {
		return application.JobApplication{}, ErrApplicationNotFound
	}
	for _, value := range r.applications {
		if value.ExternalID == externalID {
			return cloneApplication(value)
		}
	}
	return application.JobApplication{}, ErrApplicationNotFound
}

func (r *ApplicationRepository) List(ctx context.Context) ([]application.JobApplication, error) {
	if err := applicationContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return []application.JobApplication{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneApplications(r.applications)
}

func (r *ApplicationRepository) Create(ctx context.Context, value application.JobApplication) (application.JobApplication, error) {
	if err := applicationContextError(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if r == nil {
		return application.JobApplication{}, errors.New("application repository is not configured")
	}
	if value.ID == "" {
		var err error
		value.ID, err = newApplicationID("application")
		if err != nil {
			return application.JobApplication{}, err
		}
	}
	if value.Source == "" {
		value.Source = application.SourceManual
	}
	if value.Status == "" {
		value.Status = application.StatusDiscovered
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	value, err := cloneApplication(value)
	if err != nil {
		return application.JobApplication{}, errors.New("cannot copy application")
	}
	if value.MatchResult != nil {
		value.MatchResult.Normalize()
	}
	if err := value.Validate(); err != nil {
		return application.JobApplication{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.applications {
		if existing.ID == value.ID {
			return application.JobApplication{}, ErrDuplicateApplicationID
		}
		if value.ExternalID != "" && existing.ExternalID == value.ExternalID {
			return application.JobApplication{}, ErrDuplicateExternalID
		}
	}
	event, err := r.newEventLocked(value.ID, value.CreatedAt, application.EventCreated, "application created")
	if err != nil {
		return application.JobApplication{}, err
	}
	nextApplications, err := cloneApplications(append(append([]application.JobApplication{}, r.applications...), value))
	if err != nil {
		return application.JobApplication{}, err
	}
	nextEvents, err := cloneEvents(append(append([]application.Event{}, r.events...), event))
	if err != nil {
		return application.JobApplication{}, err
	}
	if err := ValidateApplicationCollections(nextApplications, nextEvents); err != nil {
		return application.JobApplication{}, err
	}
	r.applications, r.events = nextApplications, nextEvents
	return cloneApplication(value)
}

func (r *ApplicationRepository) Update(ctx context.Context, value application.JobApplication) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	if strings.TrimSpace(value.ID) == "" {
		return errors.New("application update requires an id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
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
		for j, other := range r.applications {
			if i != j && value.ExternalID != "" && other.ExternalID == value.ExternalID {
				return ErrDuplicateExternalID
			}
		}
		copy, err := cloneApplication(value)
		if err != nil {
			return err
		}
		if err := copy.Validate(); err != nil {
			return err
		}
		next, err := cloneApplications(r.applications)
		if err != nil {
			return err
		}
		next[i] = copy
		if err := ValidateApplicationCollections(next, r.events); err != nil {
			return err
		}
		r.applications = next
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) UpsertImported(ctx context.Context, value application.JobApplication) (application.JobApplication, bool, error) {
	if err := applicationContextError(ctx); err != nil {
		return application.JobApplication{}, false, err
	}
	if r == nil {
		return application.JobApplication{}, false, errors.New("application repository is not configured")
	}
	if strings.TrimSpace(value.ExternalID) == "" {
		return application.JobApplication{}, false, errors.New("imported application requires an external id")
	}
	incomingUpdatedAt := value.UpdatedAt
	if value.Source == "" {
		value.Source = application.SourceHH
	}
	if value.Status == "" {
		value.Status = application.StatusUnknown
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
		value.ID, err = newApplicationID("application")
		if err != nil {
			return application.JobApplication{}, false, err
		}
	}
	value, err := cloneApplication(value)
	if err != nil {
		return application.JobApplication{}, false, err
	}
	if value.MatchResult != nil {
		value.MatchResult.Normalize()
	}
	if err := value.Validate(); err != nil {
		return application.JobApplication{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
		if old.ExternalID != value.ExternalID {
			continue
		}
		value.ID = old.ID
		value.CreatedAt = old.CreatedAt
		if incomingUpdatedAt.IsZero() || value.UpdatedAt.Before(old.UpdatedAt) {
			value.UpdatedAt = old.UpdatedAt
		}
		if applicationEquivalent(old, value) {
			copy, err := cloneApplication(old)
			return copy, false, err
		}
		next, err := cloneApplications(r.applications)
		if err != nil {
			return application.JobApplication{}, false, err
		}
		next[i] = value
		if err := ValidateApplicationCollections(next, r.events); err != nil {
			return application.JobApplication{}, false, err
		}
		r.applications = next
		copy, err := cloneApplication(value)
		return copy, false, err
	}
	// Inline Create so the lock is not recursively acquired.
	event, err := r.newEventLocked(value.ID, value.CreatedAt, application.EventCreated, "application created")
	if err != nil {
		return application.JobApplication{}, false, err
	}
	for _, existing := range r.applications {
		if existing.ID == value.ID {
			return application.JobApplication{}, false, ErrDuplicateApplicationID
		}
	}
	nextApplications, err := cloneApplications(append(append([]application.JobApplication{}, r.applications...), value))
	if err != nil {
		return application.JobApplication{}, false, err
	}
	nextEvents, err := cloneEvents(append(append([]application.Event{}, r.events...), event))
	if err != nil {
		return application.JobApplication{}, false, err
	}
	if err := ValidateApplicationCollections(nextApplications, nextEvents); err != nil {
		return application.JobApplication{}, false, err
	}
	r.applications, r.events = nextApplications, nextEvents
	copy, err := cloneApplication(value)
	return copy, true, err
}

func (r *ApplicationRepository) AttachImportedConversation(ctx context.Context, externalID, conversationID string) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("conversation id is required")
	}
	value, err := r.GetByExternalID(ctx, externalID)
	if err != nil {
		return err
	}
	return r.attachConversationValue(ctx, value.ID, conversationID)
}

func (r *ApplicationRepository) AttachConversation(ctx context.Context, applicationID, conversationID string) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	return r.attachConversationValue(ctx, applicationID, conversationID)
}

func (r *ApplicationRepository) attachConversationValue(ctx context.Context, applicationID, conversationID string) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("conversation id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
		if old.ID != applicationID {
			continue
		}
		if old.ConversationID == conversationID {
			return nil
		}
		updated := old
		updated.ConversationID = conversationID
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		copy, err := cloneApplication(updated)
		if err != nil {
			return err
		}
		next, err := cloneApplications(r.applications)
		if err != nil {
			return err
		}
		next[i] = copy
		if err := ValidateApplicationCollections(next, r.events); err != nil {
			return err
		}
		r.applications = next
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) UpdateStatus(ctx context.Context, id string, status application.Status) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if !application.IsValidStatus(status) {
		return errors.New("invalid application status")
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
		if old.ID != id {
			continue
		}
		if old.Status == status {
			return nil
		}
		updated := old
		updated.Status = status
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		event, err := r.newEventLocked(id, updated.UpdatedAt, application.EventTypeForStatus(status), fmt.Sprintf("status changed from %q to %q", old.Status, status))
		if err != nil {
			return err
		}
		nextApplications, err := cloneApplications(r.applications)
		if err != nil {
			return err
		}
		nextApplications[i] = updated
		nextEvents, err := cloneEvents(append(append([]application.Event{}, r.events...), event))
		if err != nil {
			return err
		}
		if err := ValidateApplicationCollections(nextApplications, nextEvents); err != nil {
			return err
		}
		r.applications, r.events = nextApplications, nextEvents
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) SetFollowUpState(ctx context.Context, id string, state conversation.FollowUpState) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	if state == "" {
		return errors.New("follow-up state is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
		if old.ID != id {
			continue
		}
		updated := old
		updated.FollowUpState = state
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		next, err := cloneApplications(r.applications)
		if err != nil {
			return err
		}
		next[i] = updated
		if err := ValidateApplicationCollections(next, r.events); err != nil {
			return err
		}
		r.applications = next
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) SaveMatchResult(ctx context.Context, id string, result vacancy.MatchResult) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	result.Normalize()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, old := range r.applications {
		if old.ID != id {
			continue
		}
		if old.MatchResult != nil && reflect.DeepEqual(*old.MatchResult, result) {
			return nil
		}
		updated := old
		copyResult := result
		updated.MatchResult = &copyResult
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		event, err := r.newEventLocked(id, updated.UpdatedAt, application.EventMatched, "vacancy match result saved")
		if err != nil {
			return err
		}
		nextApplications, err := cloneApplications(r.applications)
		if err != nil {
			return err
		}
		nextApplications[i] = updated
		nextEvents, err := cloneEvents(append(append([]application.Event{}, r.events...), event))
		if err != nil {
			return err
		}
		if err := ValidateApplicationCollections(nextApplications, nextEvents); err != nil {
			return err
		}
		r.applications, r.events = nextApplications, nextEvents
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) AppendEvent(ctx context.Context, id string, at time.Time, eventType application.EventType, description string) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.applications {
		if value.ID != id {
			continue
		}
		event, err := r.newEventLocked(id, at, eventType, description)
		if err != nil {
			return err
		}
		next, err := cloneEvents(append(append([]application.Event{}, r.events...), event))
		if err != nil {
			return err
		}
		if err := ValidateApplicationCollections(r.applications, next); err != nil {
			return err
		}
		r.events = next
		return nil
	}
	return ErrApplicationNotFound
}

func (r *ApplicationRepository) Timeline(ctx context.Context, id string) ([]application.Event, error) {
	if err := applicationContextError(ctx); err != nil {
		return nil, err
	}
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrApplicationNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []application.Event{}
	for _, event := range r.events {
		if event.ApplicationID == id {
			result = append(result, event)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })
	return cloneEvents(result)
}

// ListEvents is a compatibility read used by dashboard/migration projections;
// it is intentionally not part of ports.ApplicationReader.
func (r *ApplicationRepository) ListEvents(ctx context.Context) ([]application.Event, error) {
	if err := applicationContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return []application.Event{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneEvents(r.events)
}

// Snapshot returns detached values for compatibility batch planning. It does
// not expose the repository's in-memory slices.
func (r *ApplicationRepository) Snapshot(ctx context.Context) ([]application.JobApplication, []application.Event, error) {
	applications, err := r.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	events, err := r.ListEvents(ctx)
	if err != nil {
		return nil, nil, err
	}
	return applications, events, nil
}

// ReplaceSnapshot is a compatibility bridge for legacy root tests/workflows
// that construct detached application fixtures directly. It remains fully
// validated and cloned before replacing adapter state.
func (r *ApplicationRepository) ReplaceSnapshot(ctx context.Context, applications []application.JobApplication, events []application.Event) error {
	if err := applicationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("application repository is not configured")
	}
	if err := ValidateApplicationCollections(applications, events); err != nil {
		return err
	}
	applicationsCopy, err := cloneApplications(applications)
	if err != nil {
		return err
	}
	eventsCopy, err := cloneEvents(events)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.applications, r.events = applicationsCopy, eventsCopy
	r.mu.Unlock()
	return nil
}

func (r *ApplicationRepository) Clone() (*ApplicationRepository, error) {
	if r == nil {
		return nil, errors.New("application repository is not configured")
	}
	applications, events, err := r.Snapshot(context.Background())
	if err != nil {
		return nil, err
	}
	return &ApplicationRepository{path: r.path, applications: applications, events: events}, nil
}

func (r *ApplicationRepository) newEventLocked(applicationID string, timestamp time.Time, eventType application.EventType, description string) (application.Event, error) {
	id, err := newApplicationID("application-event")
	if err != nil {
		return application.Event{}, err
	}
	event := application.Event{ID: id, ApplicationID: applicationID, Timestamp: timestamp, Type: eventType, Description: description}
	if err := event.Validate(); err != nil {
		return application.Event{}, err
	}
	return event, nil
}

func ValidateApplicationCollections(applications []application.JobApplication, events []application.Event) error {
	ids := make(map[string]bool, len(applications))
	externalIDs := make(map[string]bool, len(applications))
	for i, value := range applications {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("application %d: %w", i+1, err)
		}
		if ids[value.ID] {
			return errors.New("duplicate application id")
		}
		ids[value.ID] = true
		if value.ExternalID != "" {
			if externalIDs[value.ExternalID] {
				return ErrDuplicateExternalID
			}
			externalIDs[value.ExternalID] = true
		}
	}
	eventIDs := make(map[string]bool, len(events))
	for i, event := range events {
		if err := event.Validate(); err != nil {
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

func applicationEquivalent(a, b application.JobApplication) bool {
	return a.FollowUpState == b.FollowUpState && a.ID == b.ID && a.VacancyID == b.VacancyID && a.ExternalID == b.ExternalID &&
		a.CompanyName == b.CompanyName && a.VacancyTitle == b.VacancyTitle && a.VacancyURL == b.VacancyURL &&
		a.Source == b.Source && a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && a.Status == b.Status &&
		reflect.DeepEqual(a.MatchResult, b.MatchResult) && a.ConversationID == b.ConversationID &&
		a.Notes == b.Notes && a.NextAction == b.NextAction && a.RawStatus == b.RawStatus &&
		reflect.DeepEqual(a.HHMetadata, b.HHMetadata) && a.Partial == b.Partial && a.DataCompleteness == b.DataCompleteness &&
		reflect.DeepEqual(a.ReconciliationEvidence, b.ReconciliationEvidence)
}

func newApplicationID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate application id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func cloneApplication(value application.JobApplication) (application.JobApplication, error) {
	var copy application.JobApplication
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}

func cloneApplications(values []application.JobApplication) ([]application.JobApplication, error) {
	if values == nil {
		return []application.JobApplication{}, nil
	}
	result := make([]application.JobApplication, len(values))
	for i, value := range values {
		copy, err := cloneApplication(value)
		if err != nil {
			return nil, err
		}
		result[i] = copy
	}
	return result, nil
}

func cloneEvents(values []application.Event) ([]application.Event, error) {
	if values == nil {
		return []application.Event{}, nil
	}
	result := make([]application.Event, len(values))
	copyBytes, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(copyBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func containsApplicationSecret(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func applicationContextError(ctx context.Context) error {
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

var _ ports.ApplicationReader = (*ApplicationRepository)(nil)
var _ ports.ApplicationWriter = (*ApplicationRepository)(nil)
var _ ports.ApplicationStore = (*ApplicationRepository)(nil)
