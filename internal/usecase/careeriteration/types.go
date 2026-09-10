package careeriteration

import (
	"context"
	"encoding/json"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/usecase/conversationpolicy"
	"hh-ai-responder/internal/vacancy"
)

// SyncResult is the read-only inbox refresh evidence needed by one iteration.
// It mirrors the existing root compatibility value without exposing the HH
// responder or any write capability.
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
	SelectedConversationIDs []string             `json:"selected_conversation_ids,omitempty"`
	ChangedConversationIDs  []string             `json:"changed_conversation_ids,omitempty"`
	Errors                  []string             `json:"errors,omitempty"`
	Warnings                []string             `json:"warnings,omitempty"`
	StartedAt               time.Time            `json:"started_at"`
	FinishedAt              time.Time            `json:"finished_at"`
}

type OperationPerformance struct {
	Duration        time.Duration `json:"duration_ns"`
	NetworkDuration time.Duration `json:"network_duration_ns"`
	RateWait        time.Duration `json:"rate_wait_ns"`
	DiskDuration    time.Duration `json:"disk_duration_ns"`
	ComputeDuration time.Duration `json:"compute_duration_ns"`
	Requests        int           `json:"requests"`
	CacheHits       int           `json:"cache_hits"`
	CacheMisses     int           `json:"cache_misses"`
}

// SyncAllResult preserves the monitor state's established shape. R12.1 uses
// only the conversations member because RefreshInbox is intentionally the
// current read boundary.
type SyncAllResult struct {
	Vacancies     SyncResult `json:"vacancies"`
	Applications  SyncResult `json:"applications"`
	Conversations SyncResult `json:"conversations"`
}

type ReconcileIssue struct {
	Relation string `json:"relation"`
	RecordID string `json:"record_id"`
	Reason   string `json:"reason"`
}

type ReconciliationResult struct {
	DryRun                bool             `json:"dry_run"`
	LinksFound            int              `json:"links_found"`
	ApplicationsCreated   int              `json:"applications_created"`
	VacanciesEnriched     int              `json:"vacancies_enriched"`
	MatchesBackfilled     int              `json:"matches_backfilled"`
	EventsCreated         int              `json:"events_created"`
	UnresolvedRelations   []ReconcileIssue `json:"unresolved_relations,omitempty"`
	InsufficientMatchData []string         `json:"insufficient_match_data,omitempty"`
}

type NotificationResult struct {
	Created      int
	Resolved     int
	Deduplicated int
}

// Snapshot is the established CareerSnapshot data set used by the monitor.
// It deliberately does not redesign or add a second dashboard read model.
type Snapshot struct {
	Vacancies      []vacancy.Vacancy
	Applications   []application.JobApplication
	Conversations  []conversation.EmployerConversation
	Events         []application.Event
	Clarifications []candidateacquisition.CandidateClarificationRequest
	Consistency    map[string][]string
}

// State retains the monitor's existing JSON fields. LastAuditResult remains a
// raw JSON value because the root audit report is a compatibility value owned
// by the root package, not by this orchestration use case.
type State struct {
	LastRunAt           time.Time             `json:"last_run_at,omitempty"`
	LastSuccessAt       time.Time             `json:"last_success_at,omitempty"`
	LastSyncResult      *SyncAllResult        `json:"last_sync_result,omitempty"`
	LastReconcileResult *ReconciliationResult `json:"last_reconcile_result,omitempty"`
	LastAuditResult     json.RawMessage       `json:"last_audit_result,omitempty"`
	ConsecutiveFailures int                   `json:"consecutive_failures"`
	NextRunAt           time.Time             `json:"next_run_at,omitempty"`
}

// InboxRefresher exposes only the current read/sync operation.
type InboxRefresher interface {
	RefreshInbox(context.Context) (SyncResult, error)
}

// BoundedInboxRefresher is the optional operator one-run capability. The
// implementation must bound provider detail expansion, not merely truncate
// the resulting Career snapshot.
type BoundedInboxRefresher interface {
	RefreshInboxBounded(context.Context, int) (SyncResult, error)
}

type Reconciler interface {
	Reconcile(context.Context, bool) (ReconciliationResult, error)
}

type ConversationStore interface {
	List(context.Context) ([]conversation.EmployerConversation, error)
	UpdateState(context.Context, string, conversation.State) error
	Save(context.Context) error
}

type VacancyReader interface {
	List(context.Context, ports.VacancyQuery) ([]vacancy.Vacancy, error)
}

type ApplicationReader interface {
	List(context.Context) ([]application.JobApplication, error)
	Timeline(context.Context, string) ([]application.Event, error)
}

type ClarificationReader interface {
	List() ([]candidateacquisition.CandidateClarificationRequest, error)
}

type StateResolver interface {
	Resolve(application.JobApplication, conversation.EmployerConversation, *time.Time, bool, []string, time.Time) conversationpolicy.Resolution
}

type NotificationProcessor interface {
	Calculate(Snapshot, time.Time) (NotificationResult, error)
	RecordSyncProblem(time.Time)
	Save() error
}

type Auditor interface {
	Audit(Snapshot, time.Time) json.RawMessage
}

type StateStore interface {
	Save(State) error
}

type Dependencies struct {
	Inbox           InboxRefresher
	Reconciler      Reconciler
	Vacancies       VacancyReader
	Applications    ApplicationReader
	Conversations   ConversationStore
	Clarifications  ClarificationReader
	Resolver        StateResolver
	ApplicationTime func(application.JobApplication, []application.Event) *time.Time
	Notifications   NotificationProcessor
	Auditor         Auditor
	StateStore      StateStore
	State           State
}

type Options struct {
	Interval         time.Duration
	QuietHours       string
	Now              func() time.Time
	MaxConversations int
}
