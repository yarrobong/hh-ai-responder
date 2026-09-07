package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const HHSyncStateFilename = "hh_sync_state.json"

// HHVacancyRecord, HHApplicationRecord and HHConversationRecord are the
// small read-only boundary models. They intentionally contain only useful HH
// fields, while HHMetadata preserves identifiers that may be needed later.
type HHVacancyRecord struct {
	ExternalID     string
	ID             int
	Title          string
	Company        string
	Description    string
	Requirements   []string
	KeySkills      []string
	Salary         string
	Currency       string
	Location       string
	WorkFormat     string
	Experience     string
	EmploymentType string
	Schedule       string
	URL            string
	PublishedAt    time.Time
	UpdatedAt      time.Time
	Metadata       map[string]string
}

type HHApplicationRecord struct {
	Vacancy              *Vacancy
	ExternalID           string
	VacancyExternalID    string
	VacancyID            int
	Company              string
	VacancyTitle         string
	VacancyURL           string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ConversationExternal string
	Metadata             map[string]string
}

type HHConversationRecord struct {
	MetadataUnchanged  bool // Only produced by explicit lightweight Inbox refresh.
	ExternalID         string
	VacancyExternalID  string
	VacancyID          int
	Company            string
	VacancyTitle       string
	VacancyDescription string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Metadata           map[string]string
	Messages           []HHMessageRecord
}

type HHMessageRecord struct {
	SystemEvent        bool
	ContentUnavailable bool
	ExternalID         string
	Sender             string
	Direction          string
	Text               string
	Timestamp          time.Time
}

type HHVacancyPage struct {
	Items      []HHVacancyRecord
	NextCursor string
}

type HHApplicationPage struct {
	Items      []HHApplicationRecord
	NextCursor string
}

type HHConversationPage struct {
	Items                []HHConversationRecord
	NextCursor           string
	MetadataChecked      int
	HistoryReused        int
	DetailedChatsFetched int
}

// HHReadClient has no write-shaped method. Implementations may use the
// existing responder's GET helpers or a test fake, but cannot submit an HH
// action through this synchronization layer.
type HHReadClient interface {
	ReadVacancies(ctx context.Context, cursor string) (HHVacancyPage, error)
	ReadApplications(ctx context.Context, cursor string) (HHApplicationPage, error)
	ReadConversations(ctx context.Context, cursor string) (HHConversationPage, error)
}

// HHConversationPreflightReader is an optional narrow read capability used by
// HHWriteGateway immediately before a manually approved send. It remains a
// read-only interface and is not part of HHWriteClient.
type HHConversationPreflightReader interface {
	ReadConversationState(context.Context, string) (HHConversationReadState, error)
}

// HHConversationRecordReader is the narrow read-only capability used by the
// manual transport probe to refresh one known HH chat without importing every
// chat first.
type HHConversationRecordReader interface {
	ReadConversation(context.Context, string) (HHConversationRecord, error)
}

type SyncResult struct {
	Performance            OperationPerformance `json:"performance"`
	Fetched                int                  `json:"fetched"`
	Created                int                  `json:"created"`
	Updated                int                  `json:"updated"`
	Unchanged              int                  `json:"unchanged"`
	Skipped                int                  `json:"skipped"`
	MetadataChecked        int                  `json:"metadata_checked,omitempty"`
	HistoryReused          int                  `json:"history_reused,omitempty"`
	DetailedChatsFetched   int                  `json:"detailed_chats_fetched,omitempty"`
	ChangedConversationIDs []string             `json:"changed_conversation_ids,omitempty"`
	Errors                 []string             `json:"errors,omitempty"`
	Warnings               []string             `json:"warnings,omitempty"`
	StartedAt              time.Time            `json:"started_at"`
	FinishedAt             time.Time            `json:"finished_at"`
}

type HHSyncAllResult struct {
	Vacancies     SyncResult `json:"vacancies"`
	Applications  SyncResult `json:"applications"`
	Conversations SyncResult `json:"conversations"`
}

type HHSyncState struct {
	LastVacancySyncAt      time.Time `json:"last_vacancy_sync_at,omitempty"`
	LastApplicationSyncAt  time.Time `json:"last_application_sync_at,omitempty"`
	LastConversationSyncAt time.Time `json:"last_conversation_sync_at,omitempty"`
	LastInboxRefreshAt     time.Time `json:"last_inbox_refresh_at,omitempty"`
	VacancyCursor          string    `json:"vacancy_cursor,omitempty"`
	ApplicationCursor      string    `json:"application_cursor,omitempty"`
	ConversationCursor     string    `json:"conversation_cursor,omitempty"`
	LastSuccess            time.Time `json:"last_success,omitempty"`
	LastError              string    `json:"last_error,omitempty"`
}

type HHReadSyncService struct {
	callsMu          sync.Mutex
	allCall          *hhAllSyncCall
	calls            map[string]*hhSyncCall
	lastProgress     HHSyncProgress
	commitMu         sync.Mutex
	externalCommitMu *sync.Mutex
	stateMu          sync.Mutex
	client           HHReadClient
	vacancies        *VacancyStore
	applications     *ApplicationStore
	conversations    *ConversationStore
	analyzer         interface {
		Analyze(Vacancy, any) MatchResult
	}
	candidate      any
	statusMapper   HHStatusMapper
	statePath      string
	state          HHSyncState
	clarifications *CandidateClarificationStore
	drafts         *AIDraftStore
	career         *CareerRepositories
}

// NewHHReadSyncServiceWithRepositories is the PostgreSQL career-data path.
// It keeps the read-only HH client and local draft/clarification stores
// separate while ensuring vacancy, application and conversation imports use
// one repository bundle and one transaction per fetched batch.
func NewHHReadSyncServiceWithRepositories(client HHReadClient, career CareerRepositories, dependencies ...any) *HHReadSyncService {
	s := NewHHReadSyncService(client, dependencies...)
	s.career = &career
	return s
}

// Dependencies are discovered by type to keep construction convenient in
// tests and in the CLI while retaining one service implementation.
func NewHHReadSyncService(client HHReadClient, dependencies ...any) *HHReadSyncService {
	s := &HHReadSyncService{client: client, statusMapper: NewHHStatusMapper(), statePath: HHSyncStateFilename}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case *VacancyStore:
			s.vacancies = value
		case *ApplicationStore:
			s.applications = value
		case *ConversationStore:
			s.conversations = value
		case interface {
			Analyze(Vacancy, any) MatchResult
		}:
			s.analyzer = value
		case *CandidateClarificationStore:
			s.clarifications = value
		case *AIDraftStore:
			s.drafts = value
		case HHStatusMapper:
			s.statusMapper = value
		case string:
			if strings.TrimSpace(value) != "" {
				s.statePath = value
			}
		default:
			if s.candidate == nil && value != nil {
				s.candidate = value
			}
		}
	}
	return s
}

func (s *HHReadSyncService) LoadState() error {
	if s != nil {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
	}
	if s == nil || strings.TrimSpace(s.statePath) == "" {
		return errors.New("HH sync state path is required")
	}
	raw, err := readPrivateJSON(s.statePath, &s.state)
	if errors.Is(err, errPrivateFileNotFound) {
		s.state = HHSyncState{}
		return nil
	}
	if err != nil {
		s.state = HHSyncState{}
		return fmt.Errorf("load HH sync state: %w", err)
	}
	if profileContainsSecret(raw) {
		s.state = HHSyncState{}
		return errors.New("HH sync state contains a forbidden secret marker")
	}
	return nil
}

func (s *HHReadSyncService) saveState() error {
	return withStoreLock(s.statePath, s.saveStateUnlocked)
}
func (s *HHReadSyncService) saveStateUnlocked() error {
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return errors.New("encode HH sync state")
	}
	return atomicPrivateStoreWriteUnlocked(s.statePath, raw, ".hh-sync-state-*.tmp")
}

var errPrivateFileNotFound = errors.New("private file not found")

func readPrivateJSON(path string, target any) ([]byte, error) {
	raw, err := osReadFile(path)
	if errors.Is(err, osErrNotExist) {
		return nil, errPrivateFileNotFound
	}
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return raw, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return raw, errors.New("private JSON contains trailing data")
	}
	return raw, nil
}

// Small indirections keep the sync file easy to test without introducing a
// second filesystem abstraction into the rest of the project.
var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }
var osErrNotExist = os.ErrNotExist

func (s *HHReadSyncService) SyncVacancies(contexts ...context.Context) (SyncResult, error) {
	return s.syncRead(syncContext(contexts), "vacancies")
}
func (s *HHReadSyncService) SyncApplications(contexts ...context.Context) (SyncResult, error) {
	return s.syncRead(syncContext(contexts), "applications")
}
func (s *HHReadSyncService) SyncEmployerConversations(contexts ...context.Context) (SyncResult, error) {
	return s.syncRead(syncContext(contexts), "conversations")
}

// RefreshInbox performs the lightweight conversation-list read. The HH read
// adapter compares list fingerprints with the local snapshot and fetches chat
// history only for changed conversations; this method never writes to HH.
func (s *HHReadSyncService) RefreshInbox(contexts ...context.Context) (SyncResult, error) {
	return s.syncRead(withHHReadPriority(syncContext(contexts), hhReadForegroundInbox), "inbox")
}
func (s *HHReadSyncService) SyncConversation(id string, contexts ...context.Context) (SyncResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SyncResult{Errors: []string{"HH conversation id is required"}}, nil
	}
	return s.syncRead(withHHReadPriority(syncContext(contexts), hhReadTargeted), "conversation:"+id)
}

type hhAllSyncCall struct {
	done   chan struct{}
	result HHSyncAllResult
	err    error
}

func (s *HHReadSyncService) SyncAll(contexts ...context.Context) (HHSyncAllResult, error) {
	ctx := syncContext(contexts)
	s.callsMu.Lock()
	if call := s.allCall; call != nil {
		s.callsMu.Unlock()
		select {
		case <-call.done:
			return call.result, call.err
		case <-ctx.Done():
			return HHSyncAllResult{}, ctx.Err()
		}
	}
	call := &hhAllSyncCall{done: make(chan struct{})}
	s.allCall = call
	s.callsMu.Unlock()
	result, err := s.syncAll(ctx)
	s.callsMu.Lock()
	call.result, call.err = result, err
	s.allCall = nil
	close(call.done)
	s.callsMu.Unlock()
	return result, err
}
func (s *HHReadSyncService) syncAll(ctx context.Context) (all HHSyncAllResult, err error) {
	previous := s.SyncState().LastSuccess
	defer func() {
		failures := append(append(append([]string{}, all.Vacancies.Errors...), all.Applications.Errors...), all.Conversations.Errors...)
		if err != nil {
			failures = append(failures, safeSyncError(err))
		}
		if len(failures) == 0 {
			return
		}
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
		saveErr := withStoreLock(s.statePath, func() error {
			if _, loadErr := readPrivateJSON(s.statePath, &s.state); loadErr != nil && !errors.Is(loadErr, errPrivateFileNotFound) {
				return loadErr
			}
			s.state.LastSuccess = previous
			s.state.LastError = firstSyncError(failures)
			return s.saveStateUnlocked()
		})
		if saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}()
	all.Vacancies, err = s.SyncVacancies(ctx)
	if err != nil {
		return all, err
	}
	all.Applications, err = s.SyncApplications(ctx)
	if err != nil {
		return all, err
	}
	all.Conversations, err = s.SyncEmployerConversations(ctx)
	return all, err
}

// SyncConversations is a compatibility alias for callers that use the short
// domain name. It remains the same read-only operation.
func (s *HHReadSyncService) SyncConversations(contexts ...context.Context) (SyncResult, error) {
	return s.SyncEmployerConversations(contexts...)
}

func syncContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}

func (s *HHReadSyncService) SyncState() HHSyncState {
	if s == nil {
		return HHSyncState{}
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.state
}

type syncOutcome uint8

const (
	syncSkipped syncOutcome = iota
	syncCreated
	syncUpdated
	syncUnchanged
)

func (s *HHReadSyncService) importVacancy(record HHVacancyRecord) (syncOutcome, error) {
	vacancy, err := mapHHVacancy(record)
	if err != nil {
		return syncSkipped, err
	}
	old, getErr := s.vacancies.GetByExternalID(vacancy.ExternalID)
	if getErr == nil {
		vacancy.ID, vacancy.CreatedAt = old.ID, old.CreatedAt
		if vacancy.UpdatedAt.IsZero() || vacancy.UpdatedAt.Before(vacancy.CreatedAt) {
			vacancy.UpdatedAt = old.UpdatedAt
		}
		if vacancySourceEquivalent(old, vacancy) {
			return syncUnchanged, nil
		}
		if s.analyzer != nil {
			result := s.analyzer.Analyze(vacancy, s.candidate)
			vacancy.MatchResult = &result
			vacancy.ApplicationRecommendation = result.Recommendation
		}
		return syncUpdated, s.vacancies.Update(vacancy)
	}
	if !errors.Is(getErr, ErrVacancyNotFound) {
		return syncSkipped, getErr
	}
	if s.analyzer != nil {
		result := s.analyzer.Analyze(vacancy, s.candidate)
		vacancy.MatchResult = &result
		vacancy.ApplicationRecommendation = result.Recommendation
	}
	_, err = s.vacancies.Create(vacancy)
	return syncCreated, err
}

func mapHHVacancy(record HHVacancyRecord) (Vacancy, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" && record.ID > 0 {
		externalID = strconv.Itoa(record.ID)
	}
	if externalID == "" {
		return Vacancy{}, errors.New("HH vacancy has no external id")
	}
	title := firstNonEmpty(strings.TrimSpace(record.Title), externalID)
	created := record.PublishedAt
	updated := record.UpdatedAt
	if created.IsZero() {
		created = updated
	}
	if updated.IsZero() {
		updated = created
	}
	if created.IsZero() {
		created = time.Now().UTC()
		updated = created
	}
	return Vacancy{
		ID: record.ID, ExternalID: externalID, Name: title, Title: title,
		Description: record.Description, Requirements: append([]string{}, record.Requirements...), Skills: append([]string{}, record.KeySkills...),
		Salary: record.Salary, SalaryCurrency: record.Currency, Location: record.Location,
		WorkFormat: record.WorkFormat, EmploymentType: record.EmploymentType, WorkExperience: record.Experience,
		WorkSchedule: record.Schedule, Source: "hh", Links: map[string]string{"desktop": record.URL},
		CreatedAt: created, UpdatedAt: updated, PublishedAt: record.PublishedAt, HHUpdatedAt: record.UpdatedAt,
		HHMetadata: copyStringMap(record.Metadata), DataCompleteness: inferVacancyCompleteness(Vacancy{Description: record.Description, Requirements: record.Requirements, Skills: record.KeySkills, Location: record.Location}),
	}, nil
}

func vacancySourceEquivalent(a, b Vacancy) bool {
	type sourceProjection struct {
		ExternalID, Name, Title, Description, Salary, SalaryCurrency, Location, WorkFormat, EmploymentType string
		Source, WorkSchedule, WorkExperience, URL                                                          string
		Requirements, Skills                                                                               []string
		PublishedAt, HHUpdatedAt                                                                           time.Time
	}
	projection := func(value Vacancy) sourceProjection {
		return sourceProjection{ExternalID: value.ExternalID, Name: value.Name, Title: value.Title, Description: value.Description,
			Salary: value.Salary, SalaryCurrency: value.SalaryCurrency, Location: value.Location, WorkFormat: value.WorkFormat,
			EmploymentType: value.EmploymentType, Source: value.Source, WorkSchedule: value.WorkSchedule, WorkExperience: value.WorkExperience,
			URL: value.Links["desktop"], Requirements: value.Requirements, Skills: value.Skills, PublishedAt: value.PublishedAt,
			HHUpdatedAt: value.HHUpdatedAt}
	}
	return reflect.DeepEqual(projection(a), projection(b)) && stringMapsEqual(a.HHMetadata, b.HHMetadata)
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func (s *HHReadSyncService) importApplication(record HHApplicationRecord) (syncOutcome, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" {
		return syncSkipped, errors.New("HH application has no external id")
	}
	if record.Vacancy != nil && s.vacancies != nil {
		if _, err := s.vacancies.Get(record.Vacancy.ID); errors.Is(err, ErrVacancyNotFound) {
			v := *record.Vacancy
			v.Source = "hh"
			v.ExternalID = strconv.Itoa(v.ID)
			v.HHMetadata = map[string]string{"detail_partial": "true"}
			v.DataCompleteness = inferVacancyCompleteness(v)
			if _, err := s.vacancies.Create(v); err != nil {
				return syncSkipped, err
			}
		}
	}
	status, _ := s.statusMapper.Map(record.Status)
	vacancyID := record.VacancyID
	if vacancyID == 0 && strings.TrimSpace(record.VacancyExternalID) != "" && s.vacancies != nil {
		if vacancy, err := s.vacancies.GetByExternalID(record.VacancyExternalID); err == nil {
			vacancyID = vacancy.ID
		}
	}
	value := JobApplication{ExternalID: externalID, VacancyID: vacancyID, CompanyName: record.Company, VacancyTitle: record.VacancyTitle,
		VacancyURL: record.VacancyURL, Source: ApplicationSourceHH, Status: status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		RawStatus: record.Status, HHMetadata: copyStringMap(record.Metadata), Partial: record.Vacancy == nil, DataCompleteness: DataCompletenessPartial}
	if record.Vacancy != nil {
		value.DataCompleteness = record.Vacancy.DataCompleteness
		if value.DataCompleteness == "" {
			value.DataCompleteness = inferVacancyCompleteness(*record.Vacancy)
		}
		value.Partial = value.DataCompleteness != DataCompletenessFull
	}
	if !record.CreatedAt.IsZero() && record.Metadata["delivery_confirmed"] == "true" {
		value.HHMetadata["applied_at"] = record.CreatedAt.Format(time.RFC3339Nano)
	}
	if record.ConversationExternal != "" {
		value.HHMetadata["conversation_external_id"] = record.ConversationExternal
	}
	if record.ConversationExternal != "" && s.conversations != nil {
		if conversation, err := s.conversations.GetByHHConversationID(record.ConversationExternal); err == nil {
			value.ConversationID = conversation.ID
		}
	}
	old, oldErr := s.applications.GetByExternalID(externalID)
	if oldErr == nil {
		if value.Status == ApplicationApplied && old.Status == ApplicationEmployerReplied {
			value.Status = old.Status
		}
		value.MatchResult = old.MatchResult
		value.Notes = old.Notes
		value.NextAction = old.NextAction
		value.FollowUpState = old.FollowUpState
		if value.ConversationID == "" {
			value.ConversationID = old.ConversationID
		}
	}
	_, _, err := s.applications.UpsertImportedApplication(value)
	if err != nil {
		return syncSkipped, err
	}
	if oldErr == nil {
		imported, getErr := s.applications.GetByExternalID(externalID)
		if getErr == nil && imported.ConversationID == "" && value.ConversationID != "" {
			_ = s.applications.AttachImportedConversation(externalID, value.ConversationID)
			imported, _ = s.applications.GetByExternalID(externalID)
		}
		if applicationEquivalent(old, imported) {
			return syncUnchanged, nil
		}
		return syncUpdated, nil
	}
	return syncCreated, nil
}

func (s *HHReadSyncService) importConversation(record HHConversationRecord) (syncOutcome, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" {
		return syncSkipped, errors.New("HH conversation has no external id")
	}
	vacancyID := record.VacancyID
	if vacancyID == 0 && s.vacancies != nil && record.VacancyExternalID != "" {
		if vacancy, err := s.vacancies.GetByExternalID(record.VacancyExternalID); err == nil {
			vacancyID = vacancy.ID
		}
	}
	messages, warnings := mapHHMessages(record.Messages)

	old, oldErr := s.conversations.GetByHHConversationID(externalID)
	value := EmployerConversation{ID: "", VacancyID: vacancyID, HHConversationID: externalID, CompanyName: record.Company,
		VacancyTitle: record.VacancyTitle, VacancyDescription: record.VacancyDescription, Status: conversationStatusForImport(record.Status, messages),
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, HHUpdatedAt: record.UpdatedAt, Messages: messages, RawStatus: record.Status, HHMetadata: copyStringMap(record.Metadata)}
	if len(warnings) > 0 {
		value.HHMetadata["warning_skipped_messages"] = "true"
	}
	if oldErr == nil {
		mergeHHConversation(old, &value)
	}
	application := JobApplication{}
	localApplications := []JobApplication{}
	if s.applications != nil {
		localApplications, _ = s.applications.ListApplications()
		for _, a := range localApplications {
			if a.HHMetadata["conversation_external_id"] == externalID {
				application = a
				break
			}
		}
	}
	var appliedAt *time.Time
	if s.applications != nil && application.ID != "" {
		if events, eventsErr := s.applications.GetApplicationTimeline(application.ID); eventsErr == nil {
			appliedAt = knownApplicationTime(application, events)
		}
	}
	pending := false
	if s.clarifications != nil {
		for _, q := range s.clarifications.clarifications {
			if q.Status == ClarificationPending && (value.ID != "" && q.ConversationID == value.ID || application.ID != "" && q.ApplicationID == application.ID) {
				pending = true
			}
		}
	}
	resolved := (ConversationStateResolver{}).Resolve(application, value, appliedAt, pending, nil, time.Now())
	value.Status = resolved.Status
	value.WaitingSince = resolved.WaitingSince
	value.refreshActivity()

	if oldErr != nil || !conversationEquivalent(old, value) {
		if _, err := s.conversations.UpsertConversation(value); err != nil {
			return syncSkipped, err
		}
	}
	saved, err := s.conversations.GetByHHConversationID(externalID)
	if err != nil {
		return syncSkipped, err
	}
	if s.applications != nil {
		applications := localApplications
		if applications == nil {
			applications = mustListApplications(s.applications)
		}
		exactMatch := false
		for _, application := range applications {
			if application.HHMetadata["conversation_external_id"] == saved.HHConversationID {
				exactMatch = true
				break
			}
		}
		vacancyMatches := 0
		for _, application := range applications {
			if application.VacancyID == saved.VacancyID {
				vacancyMatches++
			}
		}
		for _, application := range applications {
			if exactMatch && application.HHMetadata["conversation_external_id"] != saved.HHConversationID {
				continue
			}
			if !exactMatch && (saved.VacancyID <= 0 || vacancyMatches != 1 || application.VacancyID != saved.VacancyID) {
				continue
			}
			if saved.Status == ConversationCandidateActionRequired && (application.Status == ApplicationApplied) {
				_ = s.applications.UpdateStatus(application.ID, ApplicationEmployerReplied)
			}
			if application.ConversationID == "" {
				_ = s.applications.AttachConversation(application.ID, saved.ID)
			}
		}
	}
	if oldErr != nil {
		return syncCreated, nil
	}
	if conversationEquivalent(old, saved) {
		return syncUnchanged, nil
	}
	return syncUpdated, nil
}

func mustListApplications(s *ApplicationStore) []JobApplication {
	if s == nil {
		return nil
	}
	values, _ := s.ListApplications()
	return values
}

func mapHHMessages(records []HHMessageRecord) ([]ConversationMessage, []string) {
	indexed := append([]HHMessageRecord{}, records...)
	sort.SliceStable(indexed, func(i, j int) bool { return indexed[i].Timestamp.Before(indexed[j].Timestamp) })
	result, warnings := []ConversationMessage{}, []string{}
	for _, record := range indexed {
		sender, direction := mapHHMessageSender(record)
		if sender == "" || record.Timestamp.IsZero() || (strings.TrimSpace(record.Text) == "" && sender != ConversationSenderSystem && sender != ConversationSenderUnknown && !record.ContentUnavailable) {
			warnings = append(warnings, "malformed HH message skipped")
			continue
		}
		externalID := strings.TrimSpace(record.ExternalID)
		if externalID == "" {
			externalID = fmt.Sprintf("%d:%x", record.Timestamp.UnixNano(), hashText(record.Text))
		}
		result = append(result, ConversationMessage{HHSystemEvent: record.SystemEvent, ContentUnavailable: record.ContentUnavailable, ID: "hh-message-" + externalID, ExternalID: externalID, Timestamp: record.Timestamp,
			Sender: sender, Text: record.Text, Source: ConversationSourceHH, Direction: direction})
	}
	return result, warnings
}

func mapHHMessageSender(record HHMessageRecord) (ConversationSender, ConversationDirection) {
	sender := strings.ToLower(strings.TrimSpace(record.Sender))
	if sender == "unknown" {
		return ConversationSenderUnknown, ConversationDirectionUnknown
	}
	if sender == "system" {
		return ConversationSenderSystem, ConversationIncoming
	}
	direction := strings.ToLower(strings.TrimSpace(record.Direction))
	if sender == "candidate" || sender == "applicant" || sender == "me" || sender == "current_user" {
		return ConversationSenderCandidate, ConversationOutgoing
	}
	if sender == "employer" || sender == "manager" || sender == "recruiter" || sender == "opponent" {
		return ConversationSenderEmployer, ConversationIncoming
	}
	if direction == "incoming" {
		return ConversationSenderEmployer, ConversationIncoming
	}
	if direction == "outgoing" {
		return ConversationSenderCandidate, ConversationOutgoing
	}
	return "", ""
}

func conversationStatusForImport(raw string, messages []ConversationMessage) ConversationStatus {
	return (ConversationStateResolver{}).Resolve(JobApplication{}, EmployerConversation{RawStatus: raw, Messages: messages}, nil, false, nil, time.Now()).Status
}

func conversationEquivalent(a, b EmployerConversation) bool {
	left, right := a, b
	if !stringMapsEqual(left.HHMetadata, right.HHMetadata) {
		return false
	}
	left.HHMetadata, right.HHMetadata = nil, nil
	left.ID, right.ID = "", ""
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func copyStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return mapsClone(value)
}

var mapsClone = func(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func safeSyncError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
}

func firstSyncError(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hashText(value string) uint64 {
	var result uint64 = 14695981039346656037
	for _, char := range []byte(value) {
		result ^= uint64(char)
		result *= 1099511628211
	}
	return result
}

// Candidate inbox is a read-only aggregate used by CLI and future dashboard.
type CandidateInboxItem struct {
	Conversation          EmployerConversation            `json:"conversation"`
	LatestMessage         *ConversationMessage            `json:"latest_message,omitempty"`
	PendingClarifications []CandidateClarificationRequest `json:"pending_clarifications,omitempty"`
	AIDrafts              []AIDraft                       `json:"ai_drafts,omitempty"`
	Warnings              []string                        `json:"warnings,omitempty"`
	Workflow              CareerWorkflowProjection        `json:"workflow"`
}

type CandidateInbox struct {
	FollowUps      []FollowUpCandidate   `json:"follow_ups"`
	Items          []CandidateInboxItem  `json:"items"`
	Sections       []InboxSectionSummary `json:"sections,omitempty"`
	ImportantCount int                   `json:"important_count"`
}

func (s *HHReadSyncService) GetCandidateInbox() (CandidateInbox, error) {
	if s == nil || s.conversations == nil {
		return CandidateInbox{}, errors.New("candidate inbox requires conversation store")
	}
	conversations, err := s.conversations.ListConversations()
	if err != nil {
		return CandidateInbox{}, err
	}
	clarifications := []CandidateClarificationRequest{}
	if s.clarifications != nil {
		clarifications, err = s.clarifications.List()
		if err != nil {
			return CandidateInbox{}, err
		}
	}
	drafts := []AIDraft{}
	if s.drafts != nil {
		drafts, err = s.drafts.List()
		if err != nil {
			return CandidateInbox{}, err
		}
	}
	result := CandidateInbox{Items: []CandidateInboxItem{}}
	for _, conversation := range conversations {
		item := CandidateInboxItem{Conversation: conversation, Warnings: []string{}}
		resolvedState := (ConversationStateResolver{}).Resolve(JobApplication{}, conversation, nil, false, nil, time.Now().UTC())
		item.Conversation.Status = resolvedState.Status
		item.Warnings = append(item.Warnings, resolvedState.Warnings...)
		if conversation.Status == ConversationManualReview {
			item.Warnings = append(item.Warnings, "manual_review")
		}
		if timeline := deliveredMessages(conversation.Messages); len(timeline) > 0 {
			latest := timeline[len(timeline)-1]
			item.LatestMessage = &latest
			if latest.Sender == ConversationSenderEmployer {
				if reason := classifyHighRiskChatMessage(latest.Text); reason != "" {
					item.Warnings = append(item.Warnings, "high-risk employer message: "+reason)
				}
			}
		}
		for _, clarification := range clarifications {
			if clarification.Status == ClarificationPending && clarification.ConversationID == conversation.ID {
				item.PendingClarifications = append(item.PendingClarifications, clarification)
			}
		}
		for _, draft := range drafts {
			if draft.Status == AIDraftGenerated && draft.ConversationID == conversation.ID {
				item.AIDrafts = append(item.AIDrafts, draft)
			}
		}
		item.Workflow = classifyCareerWorkflow(JobApplication{}, item.Conversation, item.PendingClarifications, time.Now().UTC(), nil, nil)
		if resolvedState.Status != ConversationClosed && resolvedState.Status != ConversationRejected &&
			(resolvedState.Status == ConversationCandidateActionRequired || len(item.PendingClarifications) > 0 || len(item.AIDrafts) > 0 || len(item.Warnings) > 0) {
			result.Items = append(result.Items, item)
		}
	}
	return result, nil
}

// HHAIResponderReadClient adapts the existing responder's GET-only helpers.
// No method below calls an HH write method.
type HHAIResponderReadClient struct{ responder *HHAIResponder }

func NewHHAIResponderReadClient(responder *HHAIResponder) *HHAIResponderReadClient {
	return &HHAIResponderReadClient{responder: responder}
}

func (c *HHAIResponderReadClient) ReadVacancies(ctx context.Context, cursor string) (HHVacancyPage, error) {
	if c == nil || c.responder == nil {
		return HHVacancyPage{}, errors.New("HH responder is not configured")
	}
	page, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil && strings.TrimSpace(cursor) != "" {
		return HHVacancyPage{}, errors.New("invalid HH vacancy cursor")
	}
	vacancies, err := c.responder.fetchVacancyPageContext(ctx, c.responder.searchParams, c.responder.baseURL, page)
	if err != nil {
		return HHVacancyPage{}, err
	}
	items := make([]HHVacancyRecord, 0, len(vacancies))
	for _, vacancy := range vacancies {
		description := vacancy.Description
		if strings.TrimSpace(description) == "" && vacancy.ID > 0 {
			var descriptionErr error
			description, descriptionErr = c.responder.getVacancyDescriptionContext(ctx, vacancy.ID)
			if descriptionErr != nil {
				return HHVacancyPage{}, descriptionErr
			}
		}
		items = append(items, HHVacancyRecord{ID: vacancy.ID, ExternalID: vacancy.ExternalID, Title: firstNonEmpty(vacancy.Title, vacancy.Name), Company: vacancy.Company.Name,
			Description: description, Requirements: vacancy.Requirements, KeySkills: vacancy.Skills, Salary: vacancy.Salary, Currency: firstNonEmpty(vacancy.SalaryCurrency, vacancy.Compensation.Currency),
			Location: firstNonEmpty(vacancy.Location, vacancy.Area.Name), WorkFormat: vacancy.WorkFormat, Experience: vacancy.WorkExperience, Schedule: vacancy.WorkSchedule,
			URL: vacancy.Links["desktop"], PublishedAt: vacancy.PublishedAt, UpdatedAt: vacancy.HHUpdatedAt, Metadata: vacancy.HHMetadata})
	}
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return HHVacancyPage{Items: items, NextCursor: next}, nil
}

func (c *HHAIResponderReadClient) ReadApplications(ctx context.Context, cursor string) (HHApplicationPage, error) {
	if c == nil || c.responder == nil {
		return HHApplicationPage{}, errors.New("HH responder is not configured")
	}
	page := 0
	var err error
	if strings.TrimSpace(cursor) != "" {
		page, err = strconv.Atoi(cursor)
		if err != nil {
			return HHApplicationPage{}, errors.New("invalid HH application cursor")
		}
	}
	endpoint := "/applicant/negotiations?page=" + strconv.Itoa(page)
	request, err := c.responder.buildRequest(http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return HHApplicationPage{}, err
	}
	response, err := c.responder.requester.Do(request.WithContext(ctx))
	if err != nil {
		return HHApplicationPage{}, err
	}
	if response.Status != http.StatusOK {
		return HHApplicationPage{}, unexpectedHTTPStatus(response.Status)
	}
	if items, next, recognized, err := parseHHNegotiations(response.Body); recognized {
		return HHApplicationPage{Items: items, NextCursor: next}, err
	}
	items, err := parseHHApplicationPage(response.Body)
	if err != nil {
		return HHApplicationPage{}, err
	}
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return HHApplicationPage{Items: items, NextCursor: next}, nil
}

func (c *HHAIResponderReadClient) ReadConversations(ctx context.Context, cursor string) (HHConversationPage, error) {
	if c == nil || c.responder == nil {
		return HHConversationPage{}, errors.New("HH responder is not configured")
	}
	return c.readConversationPage(ctx, cursor)
}

func (c *HHAIResponderReadClient) ReadConversation(ctx context.Context, externalID string) (HHConversationRecord, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return HHConversationRecord{}, errors.New("HH conversation external id is required")
	}
	if c == nil || c.responder == nil {
		return HHConversationRecord{}, errors.New("HH responder is not configured")
	}
	if hhReadPriorityFromContext(ctx) == 0 {
		ctx = withHHReadPriority(ctx, hhReadTargeted)
	}
	id := parseHHChatID(externalID)
	if id <= 0 {
		return HHConversationRecord{}, errors.New("invalid HH chat id")
	}
	data, err := c.readChatData(ctx, id)
	if err != nil {
		return HHConversationRecord{}, err
	}
	chat := ChatListItem{Id: id, Resources: data.Chat.Resources, CurrentParticipantID: data.Chat.CurrentParticipantID}
	return hhChatRecord(chat, data, &ChatsResponse{}), nil
}

func (c *HHAIResponderReadClient) ReadConversationState(ctx context.Context, externalID string) (HHConversationReadState, error) {
	if c == nil || c.responder == nil {
		return HHConversationReadState{}, errors.New("HH responder is not configured")
	}
	ctx = withHHReadPriority(ctx, hhReadSafety)
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return HHConversationReadState{}, errors.New("HH conversation external id is required")
	}
	record, err := c.ReadConversation(ctx, externalID)
	if err != nil {
		return HHConversationReadState{}, err
	}
	{
		messageIDs := []string{}
		for _, message := range record.Messages {
			if strings.TrimSpace(message.ExternalID) != "" {
				messageIDs = append(messageIDs, message.ExternalID)
			}
		}
		messages, _ := mapHHMessages(record.Messages)
		// LastMessageID is compared with the local conversation ID, so it must
		// use the same prefixed ID and ignore HH-only system events. Keep the
		// raw IDs separately above for delivery reconciliation.
		lastID := latestHHConversationMessageID(messages)
		warnings := []string{}
		for key, value := range record.Metadata {
			if strings.HasPrefix(key, "warning") && value != "" && value != "false" {
				warnings = append(warnings, key)
			}
		}
		local := EmployerConversation{HHConversationID: record.ExternalID, RawStatus: record.Status, Messages: messages}
		latest := latestDeliveredMessage(local)
		requirement := NoReplyNeeded
		if latest != nil {
			requirement = conversationReplyRequirement(local, latest, classifyEmployerMessage(latest.Text))
		}
		return HHConversationReadState{ExternalID: record.ExternalID, LastMessageID: lastID, State: string(conversationStatusForImport(record.Status, messages)), ReplyRequirement: requirement, MessageCount: len(record.Messages), Warnings: warnings, MessageIDs: messageIDs}, nil
	}
}

func latestHHConversationMessageID(messages []ConversationMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.HHSystemEvent || message.Sender == ConversationSenderSystem {
			continue
		}
		return message.ID
	}
	return ""
}

func parseHHApplicationPage(data []byte) ([]HHApplicationRecord, error) {
	if items, _, recognized, err := parseHHNegotiations(data); recognized {
		return items, err
	}
	// Compatibility for an explicit normalized JSON list, never a recursive page walk.
	var root struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(data, &root) != nil || root.Items == nil {
		return nil, errors.New("unrecognized HH negotiation response; no records imported")
	}
	result := []HHApplicationRecord{}
	seen := map[string]bool{}
	for _, v := range root.Items {
		id := stringValue(v, "id", "negotiationId", "responseId", "externalId", "external_id")
		vid := intValue(v, "vacancyId", "vacancy_id")
		if id == "" || vid <= 0 || seen[id] {
			return nil, errors.New("invalid or duplicate HH negotiation identity")
		}
		seen[id] = true
		result = append(result, HHApplicationRecord{ExternalID: id, VacancyID: vid, Company: stringValue(v, "companyName", "employerName"), VacancyTitle: stringValue(v, "vacancyTitle", "title"), Status: stringValue(v, "status", "state", "applicantState"), CreatedAt: timeValue(v, "createdAt", "creationTime", "created_at"), UpdatedAt: timeValue(v, "updatedAt", "lastChangeTime", "updated_at"), ConversationExternal: stringValue(v, "chatId", "conversationId")})
	}
	return result, nil
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if item, ok := value[key]; ok {
			if result, ok := item.(string); ok {
				return strings.TrimSpace(result)
			}
			if result, ok := item.(json.Number); ok {
				return result.String()
			}
			if result, ok := item.(float64); ok {
				return strconv.FormatInt(int64(result), 10)
			}
		}
	}
	return ""
}

func intValue(value map[string]any, keys ...string) int {
	return int(parseInt64(stringValue(value, keys...)))
}

func parseInt64(value string) int64 {
	result, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return result
}

func timeValue(value map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if text, ok := raw.(string); ok {
				if result, err := time.Parse(time.RFC3339, text); err == nil {
					return result
				}
			}
		}
	}
	return time.Time{}
}
