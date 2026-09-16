package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	hhreadadapter "hh-ai-responder/internal/adapters/hh/read"
	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/platform"
	hhreadports "hh-ai-responder/internal/ports/hhread"
	"hh-ai-responder/internal/usecase/hhreadsync"
)

const HHSyncStateFilename = "hh_sync_state.json"

// These aliases keep the root synchronization API source-compatible while
// making the neutral values physically independent of the root package.
type HHVacancyRecord = hhread.VacancyRecord
type HHApplicationRecord = hhread.ApplicationRecord
type HHConversationRecord = hhread.ConversationRecord
type HHMessageRecord = hhread.MessageRecord
type HHVacancyPage = hhread.VacancyPage
type HHApplicationPage = hhread.ApplicationPage
type HHConversationPage = hhread.ConversationPage

// HHReadClient has no write-shaped method. Implementations may use the
// existing responder's GET helpers or a test fake, but cannot submit an HH
// action through this synchronization layer.
type HHReadClient = hhreadports.HHReadSource

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
	Performance             OperationPerformance `json:"performance"`
	Fetched                 int                  `json:"fetched"`
	Created                 int                  `json:"created"`
	Updated                 int                  `json:"updated"`
	Unchanged               int                  `json:"unchanged"`
	Skipped                 int                  `json:"skipped"`
	MetadataChecked         int                  `json:"metadata_checked,omitempty"`
	HistoryReused           int                  `json:"history_reused,omitempty"`
	DetailedChatsFetched    int                  `json:"detailed_chats_fetched,omitempty"`
	DetailRequested         int                  `json:"detail_requested,omitempty"`
	DetailSucceeded         int                  `json:"detail_succeeded,omitempty"`
	DetailSkipped           int                  `json:"detail_skipped,omitempty"`
	DetailFailed            int                  `json:"detail_failed,omitempty"`
	DetailFieldsEnriched    int                  `json:"detail_fields_enriched,omitempty"`
	SelectedConversationIDs []string             `json:"selected_conversation_ids,omitempty"`
	ChangedConversationIDs  []string             `json:"changed_conversation_ids,omitempty"`
	Errors                  []string             `json:"errors,omitempty"`
	Warnings                []string             `json:"warnings,omitempty"`
	StartedAt               time.Time            `json:"started_at"`
	FinishedAt              time.Time            `json:"finished_at"`
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
	externalCommitMu sync.Locker
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
	syncPolicy     *hhreadsync.Service
}

// syncUsecase returns the typed policy constructed by the root compatibility
// layer. It owns no import decisions; those live in hhreadsync.
func (s *HHReadSyncService) syncUsecase() *hhreadsync.Service {
	// Rebuild the compatibility translation for each operation because legacy
	// tests and dashboard composition may replace the source or stores after
	// constructing the root façade.
	s.syncPolicy = s.buildSyncUsecase()
	return s.syncPolicy
}

// buildSyncUsecase is the sole root compatibility translation from legacy
// stores and policy collaborators to the typed synchronization use case.
func (s *HHReadSyncService) buildSyncUsecase() *hhreadsync.Service {
	deps := hhreadsync.Dependencies{Source: s.client, MapStatus: s.statusMapper.Map, WarnStatus: s.statusMapper.Warning}
	if s.career != nil {
		deps.Vacancies = s.career.Vacancies
		deps.Applications = s.career.Applications
		deps.Conversations = s.career.Conversations
	} else if s.vacancies != nil {
		deps.Vacancies = NewJSONVacancyRepository(s.vacancies)
	}
	if s.career == nil && s.applications != nil {
		deps.Applications = NewJSONApplicationRepository(s.applications)
	}
	if s.career == nil && s.conversations != nil {
		deps.Conversations = NewJSONConversationRepository(s.conversations)
	}
	deps.ResolveState = func(a JobApplication, c EmployerConversation, appliedAt *time.Time, pending bool, warnings []string, now time.Time) hhreadsync.ConversationResolution {
		resolved := (ConversationStateResolver{}).Resolve(a, c, appliedAt, pending, warnings, now)
		return hhreadsync.ConversationResolution{Status: resolved.Status, WaitingSince: resolved.WaitingSince}
	}
	if s.clarifications != nil {
		deps.PendingState = func(conversationID, applicationID string) (bool, error) {
			values, err := s.clarifications.List()
			if err != nil {
				return false, err
			}
			for _, value := range values {
				if value.Status == ClarificationPending && ((conversationID != "" && value.ConversationID == conversationID) || (applicationID != "" && value.ApplicationID == applicationID)) {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return hhreadsync.NewService(deps)
}

func syncOutcomeFromResult(result hhreadsync.Result) syncOutcome {
	switch {
	case result.Created > 0:
		return syncCreated
	case result.Updated > 0:
		return syncUpdated
	case result.Unchanged > 0:
		return syncUnchanged
	default:
		return syncSkipped
	}
}

// HHReadSyncServiceOptions is the explicit composition boundary for the root
// read-sync façade. It keeps storage, read-model and policy collaborators
// visible without moving the legacy JSON/Postgres compatibility layer.
type HHReadSyncServiceOptions struct {
	Vacancies     *VacancyStore
	Applications  *ApplicationStore
	Conversations *ConversationStore
	Analyzer      interface {
		Analyze(Vacancy, any) MatchResult
	}
	Candidate      any
	Clarifications *CandidateClarificationStore
	Drafts         *AIDraftStore
	StatusMapper   HHStatusMapper
	StatePath      string
}

// NewHHReadSyncServiceWithOptions is the typed construction path used by
// production composition.
func NewHHReadSyncServiceWithOptions(client HHReadClient, options HHReadSyncServiceOptions) *HHReadSyncService {
	statePath := options.StatePath
	if strings.TrimSpace(statePath) == "" {
		statePath = HHSyncStateFilename
	}
	statusMapper := options.StatusMapper
	if statusMapper.statuses == nil {
		statusMapper = NewHHStatusMapper()
	}
	s := &HHReadSyncService{
		client: client, vacancies: options.Vacancies, applications: options.Applications,
		conversations: options.Conversations, analyzer: options.Analyzer,
		candidate: options.Candidate, clarifications: options.Clarifications,
		drafts: options.Drafts, statusMapper: statusMapper, statePath: statePath,
	}
	s.syncPolicy = s.buildSyncUsecase()
	return s
}

// NewHHReadSyncServiceWithRepositories is the PostgreSQL career-data path.
// It keeps the read-only HH client and local draft/clarification stores
// separate while ensuring vacancy, application and conversation imports use
// one repository bundle and one transaction per fetched batch.
func NewHHReadSyncServiceWithRepositories(client HHReadClient, career CareerRepositories, options HHReadSyncServiceOptions) *HHReadSyncService {
	s := NewHHReadSyncServiceWithOptions(client, options)
	s.career = &career
	s.vacancies = newVacancyStoreFromRepository(career.Vacancies)
	s.applications = newApplicationStoreFromRepository(career.Applications)
	s.conversations = newConversationStoreFromRepository(career.Conversations)
	s.applications.SetConversationStore(s.conversations)
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
	return platform.WritePrivateFileAtomic(s.statePath, append(raw, '\n'), ".hh-sync-state-*.tmp")
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

// RefreshInboxBounded performs the same read-only inbox refresh as
// RefreshInbox, but limits conversation detail expansion before the provider
// requests are started. It is intended only for an explicit operator one-run.
func (s *HHReadSyncService) RefreshInboxBounded(ctx context.Context, maxConversations int) (SyncResult, error) {
	return s.syncReadWithOptions(withHHReadPriority(ctx, hhReadForegroundInbox), "inbox", hhreadsync.ReadOptions{MaxConversations: maxConversations})
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
	prepared, prepareErr := s.prepareVacancyAnalysis([]HHVacancyRecord{record})
	if prepareErr != nil {
		return syncSkipped, prepareErr
	}
	result, err := s.syncUsecase().ImportBatch(context.Background(), hhreadsync.Batch{Vacancies: []hhread.VacancyRecord{record}}, hhreadsync.ImportOptions{})
	if err == nil {
		err = s.applyPreparedVacancyAnalysis(context.Background(), s.vacancyRepository(), []HHVacancyRecord{record}, prepared)
	}
	return syncOutcomeFromResult(result), err
}

func (s *HHReadSyncService) vacancyRepository() VacancyRepository {
	if s != nil && s.career != nil {
		return s.career.Vacancies
	}
	if s == nil || s.vacancies == nil {
		return nil
	}
	return NewJSONVacancyRepository(s.vacancies)
}

func mapHHVacancy(record HHVacancyRecord) (Vacancy, error) {
	return hhreadsync.MapVacancy(record)
}

func vacancySourceEquivalent(a, b Vacancy) bool {
	return hhreadsync.VacancySourceEquivalent(a, b)
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
	result, err := s.syncUsecase().ImportBatch(context.Background(), hhreadsync.Batch{Applications: []hhread.ApplicationRecord{record}}, hhreadsync.ImportOptions{})
	return syncOutcomeFromResult(result), err
}

func (s *HHReadSyncService) importConversation(record HHConversationRecord) (syncOutcome, error) {
	result, err := s.syncUsecase().ImportBatch(context.Background(), hhreadsync.Batch{Conversations: []hhread.ConversationRecord{record}}, hhreadsync.ImportOptions{})
	return syncOutcomeFromResult(result), err
}

func mapHHMessages(records []HHMessageRecord) ([]ConversationMessage, []string) {
	return hhreadsync.MapMessages(records)
}

func mapHHMessageSender(record HHMessageRecord) (ConversationSender, ConversationDirection) {
	return hhreadsync.MapMessageSender(record)
}

func conversationStatusForImport(raw string, messages []ConversationMessage) ConversationStatus {
	return hhreadsync.ImportedConversationStatus(raw, messages)
}

func conversationEquivalent(a, b EmployerConversation) bool {
	return hhreadsync.ConversationEquivalent(a, b)
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
type HHAIResponderReadClient struct {
	responder *HHAIResponder
	adapter   *hhreadadapter.Client
}

func NewHHAIResponderReadClient(responder *HHAIResponder) *HHAIResponderReadClient {
	client := &HHAIResponderReadClient{responder: responder}
	if responder != nil {
		responder.readClient = client
	}
	return client
}

func (r *HHAIResponder) hhReadClient() *HHAIResponderReadClient {
	if r == nil {
		return nil
	}
	if r.readClient != nil {
		return r.readClient
	}
	return NewHHAIResponderReadClient(r)
}

func (r *HHAIResponder) getChatsThroughReadAdapter(page int) (*ChatsResponse, error) {
	return r.getChatsThroughReadAdapterContext(ctxOrBackground(r.ctx), page)
}

func (r *HHAIResponder) getChatsThroughReadAdapterContext(ctx context.Context, page int) (*ChatsResponse, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return nil, errors.New("HH responder is not configured")
	}
	adapter, err := reader.readAdapter()
	if err != nil {
		return nil, err
	}
	// HH's chat list uses an opaque `from` cursor. The first request must omit
	// it; `from=0` is rejected with HTTP 400 by the current provider contract.
	cursor := ""
	if page > 0 {
		cursor = strconv.Itoa(page)
	}
	pageValue, err := adapter.ReadChatList(ctxOrBackground(ctx), cursor)
	if err != nil {
		return nil, err
	}
	result := &ChatsResponse{Chats: ChatsList{NextFrom: pageValue.NextFrom, Page: pageValue.Page, PerPage: pageValue.PerPage, Pages: pageValue.Pages, Items: []ChatListItem{}}, Resources: ChatsResources{Vacancies: map[string]ChatVacancyResource{}, Resumes: map[string]ChatResumeResource{}}}
	for id := range pageValue.ResumeIDs {
		result.Resources.Resumes[id] = ChatResumeResource{Id: parseHHChatID(id)}
	}
	for id, value := range pageValue.Vacancies {
		compensation := &Compensation{From: value.SalaryFrom, To: value.SalaryTo, Currency: value.SalaryCurrency}
		result.Resources.Vacancies[id] = ChatVacancyResource{VacancyID: value.ID, Name: value.Name, Company: struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			SiteURL string `json:"companySiteUrl,omitempty"`
			Trusted bool   `json:"trusted,omitempty"`
		}{Name: value.Company}, Links: VacancyLinks{Desktop: value.URL}, Compensation: compensation}
	}
	for _, value := range pageValue.Items {
		item := ChatListItem{Id: value.ID, Resources: ChatItemResources{Vacancy: append([]string(nil), value.VacancyIDs...), Resume: append([]string(nil), value.ResumeIDs...)}, LastActivityTime: value.LastActivityTime}
		if value.LastMessage != nil {
			item.LastMessage = legacyChatMessage(*value.LastMessage)
		}
		result.Chats.Items = append(result.Chats.Items, item)
	}
	return result, nil
}

func legacyChatsResponse(pageValue hhread.ChatListPage) *ChatsResponse {
	result := &ChatsResponse{Chats: ChatsList{NextFrom: pageValue.NextFrom, Page: pageValue.Page, PerPage: pageValue.PerPage, Pages: pageValue.Pages, Items: []ChatListItem{}}, Resources: ChatsResources{Vacancies: map[string]ChatVacancyResource{}, Resumes: map[string]ChatResumeResource{}}}
	for id := range pageValue.ResumeIDs {
		result.Resources.Resumes[id] = ChatResumeResource{Id: parseHHChatID(id)}
	}
	for id, value := range pageValue.Vacancies {
		compensation := &Compensation{From: value.SalaryFrom, To: value.SalaryTo, Currency: value.SalaryCurrency}
		result.Resources.Vacancies[id] = ChatVacancyResource{VacancyID: value.ID, Name: value.Name, Company: struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			SiteURL string `json:"companySiteUrl,omitempty"`
			Trusted bool   `json:"trusted,omitempty"`
		}{Name: value.Company}, Links: VacancyLinks{Desktop: value.URL}, Compensation: compensation}
	}
	for _, value := range pageValue.Items {
		item := ChatListItem{Id: value.ID, Resources: ChatItemResources{Vacancy: append([]string(nil), value.VacancyIDs...), Resume: append([]string(nil), value.ResumeIDs...)}, LastActivityTime: value.LastActivityTime}
		if value.LastMessage != nil {
			item.LastMessage = legacyChatMessage(*value.LastMessage)
		}
		result.Chats.Items = append(result.Chats.Items, item)
	}
	return result
}

func (r *HHAIResponder) getChatDataThroughReadAdapter(chatID, applicantID int64) (*ChatDataResponse, error) {
	return r.getChatDataThroughReadAdapterContext(ctxOrBackground(r.ctx), chatID, applicantID)
}

func (r *HHAIResponder) getChatDataThroughReadAdapterContext(ctx context.Context, chatID, applicantID int64) (*ChatDataResponse, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return nil, errors.New("HH responder is not configured")
	}
	adapter, err := reader.readAdapter()
	if err != nil {
		return nil, err
	}
	history, err := adapter.ReadChatHistory(ctxOrBackground(ctx), chatID, applicantID)
	if err != nil {
		return nil, err
	}
	result := &ChatDataResponse{Chat: ChatDetail{ID: history.ID, Messages: ChatMessages{Items: []ChatMessage{}}}, ChatStates: ChatStates{WriteMessageState: StateAllowed{Allowed: history.WriteAllowed}}}
	for _, value := range history.Messages {
		result.Chat.Messages.Items = append(result.Chat.Messages.Items, *legacyChatMessage(value))
	}
	return result, nil
}

func legacyChatMessage(value hhread.MessageRecord) *ChatMessage {
	result := &ChatMessage{ID: parseHHChatID(value.ExternalID), CreationTime: value.Timestamp, Text: value.Text, ParticipantID: value.ParticipantID, ParticipantDisplay: ParticipantDisplay{Name: value.ParticipantName}, HasContent: strings.TrimSpace(value.Text) != ""}
	if value.WorkflowApplicantState != "" {
		result.WorkflowTransition = &WorkflowTransition{ApplicantState: value.WorkflowApplicantState}
	}
	if len(value.Actions) > 0 {
		result.Actions = &MessageActions{}
		for _, action := range value.Actions {
			result.Actions.TextButtons = append(result.Actions.TextButtons, TextButton{Text: action.Label, Size: action.Metadata["size"]})
		}
	}
	return result
}

func messageExternalID(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func (r *HHAIResponder) ignoreChatTrigger(chatID int64, triggerID string) {
	if r == nil || strings.TrimSpace(triggerID) == "" {
		return
	}
	r.ignoredChatsMu.Lock()
	defer r.ignoredChatsMu.Unlock()
	if r.ignoredChatTriggers == nil {
		r.ignoredChatTriggers = make(map[string]struct{})
	}
	r.ignoredChatTriggers[fmt.Sprintf("%d\x00%s", chatID, triggerID)] = struct{}{}
}

func (r *HHAIResponder) isIgnoredChatTrigger(chatID int64, triggerID string) bool {
	if r == nil {
		return false
	}
	r.ignoredChatsMu.Lock()
	defer r.ignoredChatsMu.Unlock()
	if strings.TrimSpace(triggerID) != "" {
		if _, exists := r.ignoredChatTriggers[fmt.Sprintf("%d\x00%s", chatID, triggerID)]; exists {
			return true
		}
	}
	// Chat-only suppression remains a compatibility optimization for responders
	// without the durable trigger store. It is not authoritative in R14.4.
	if r.autoChatAttempts == nil {
		return slices.Contains(r.ignoredChats, chatID)
	}
	return false
}

func (c *HHAIResponderReadClient) readAdapter() (*hhreadadapter.Client, error) {
	if c == nil || c.responder == nil {
		return nil, errors.New("HH responder is not configured")
	}
	if c.adapter != nil {
		return c.adapter, nil
	}
	r := c.responder
	chatURL := r.chatURL
	var parsedChatURL *url.URL
	if strings.TrimSpace(chatURL) != "" {
		parsedChatURL, _ = url.Parse(chatURL)
	}
	client := r.client
	if client == nil && r.requester != nil {
		client = r.requester.client
	}
	interval := time.Duration(0)
	concurrency := 0
	if r.requester != nil {
		interval = r.requester.interval
		concurrency = r.requester.readConcurrency
	}
	token := ""
	if r.jar != nil && r.baseURL != nil {
		token = r.XSRFToken()
	}
	adapter, err := hhreadadapter.NewClient(hhreadadapter.Options{
		BaseURL: r.baseURL, ChatURL: parsedChatURL, SearchParams: r.searchParams,
		HTTPClient: client, XSRFToken: token, UserID: r.userId,
		RequestInterval: interval, ReadConcurrency: concurrency,
	})
	if err != nil {
		return nil, err
	}
	c.adapter = adapter
	return adapter, nil
}

func (c *HHAIResponderReadClient) ReadVacancies(ctx context.Context, cursor string) (HHVacancyPage, error) {
	adapter, err := c.readAdapter()
	if err != nil {
		return HHVacancyPage{}, err
	}
	return adapter.ReadVacancies(ctx, cursor)
}

func (c *HHAIResponderReadClient) ReadVacancyDetail(ctx context.Context, id int) (HHVacancyRecord, error) {
	adapter, err := c.readAdapter()
	if err != nil {
		return HHVacancyRecord{}, err
	}
	return adapter.ReadVacancyDetail(ctx, id)
}

func (c *HHAIResponderReadClient) ReadApplications(ctx context.Context, cursor string) (HHApplicationPage, error) {
	adapter, err := c.readAdapter()
	if err != nil {
		return HHApplicationPage{}, err
	}
	return adapter.ReadApplications(ctx, cursor)
}

func (c *HHAIResponderReadClient) ReadConversations(ctx context.Context, cursor string) (HHConversationPage, error) {
	return c.readConversationPage(ctx, cursor)
}

func (c *HHAIResponderReadClient) ReadConversation(ctx context.Context, externalID string) (HHConversationRecord, error) {
	adapter, err := c.readAdapter()
	if err != nil {
		return HHConversationRecord{}, err
	}
	return adapter.ReadConversation(ctx, externalID)
}

// ReadVacancyPreflight is a narrow read-only capability used by explicit
// application reconciliation. It is not part of the HH write gateway.
func (c *HHAIResponderReadClient) ReadVacancyPreflight(ctx context.Context, vacancyID int) (VacancyPreflight, error) {
	if c == nil || c.responder == nil {
		return VacancyPreflight{}, errors.New("HH responder is not configured")
	}
	if vacancyID <= 0 {
		return VacancyPreflight{}, errors.New("HH vacancy ID is required")
	}
	return c.responder.getVacancyPreflightContext(ctx, Vacancy{ID: vacancyID})
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
