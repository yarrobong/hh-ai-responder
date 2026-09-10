package inboxrefresh

import (
	"context"
	"time"
)

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

// SyncResult is the read-only evidence returned by the HH inbox read.
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

// Input contains only parameters for this iteration. The caller owns cadence
// and concurrency; the service does not schedule another run.
type Input struct {
	Now time.Time
}

// Projection is a small diagnostic summary of the local post-sync projection.
// The projection itself remains owned by the root compatibility adapter and is
// intentionally not replaced with a second inbox domain model here.
type Projection struct {
	ConversationCount      int
	ApplicationCount       int
	WorkflowItems          int
	FollowUpsEligible      int
	ChangedConversationIDs []string
}

type NotificationResult struct {
	Created      int
	Resolved     int
	Deduplicated int
}

type DailyStateResult struct {
	Persisted bool
}

type Result struct {
	Sync          SyncResult
	Projection    Projection
	Notifications NotificationResult
	DailyState    DailyStateResult
}

// InboxSyncer is the R9 read boundary consumed by this usecase.
type InboxSyncer interface {
	RefreshInbox(context.Context) (SyncResult, error)
}

// LocalProjection owns reload and projection assembly. It may use local
// stores, but it must not use HH transport or mutation capabilities.
type LocalProjection interface {
	Reload(context.Context) error
	Build(context.Context, time.Time) (Projection, error)
}

type NotificationRefresher interface {
	Refresh(context.Context, time.Time) (NotificationResult, error)
}

type DailyStateStore interface {
	Save(context.Context, DailyStateInput) (DailyStateResult, error)
}

type DailyStateInput struct {
	Now        time.Time
	Sync       SyncResult
	Projection Projection
}

type Dependencies struct {
	Inbox         InboxSyncer
	Projection    LocalProjection
	Notifications NotificationRefresher
	DailyState    DailyStateStore
}

type Options struct {
	Now func() time.Time
}
