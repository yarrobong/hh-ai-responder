package ports

import (
	"context"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/vacancy"
)

// ApplicationReader contains the current read capabilities used by career
// synchronization and application context assembly.
type ApplicationReader interface {
	Get(context.Context, string) (application.JobApplication, error)
	GetByExternalID(context.Context, string) (application.JobApplication, error)
	List(context.Context) ([]application.JobApplication, error)
	Timeline(context.Context, string) ([]application.Event, error)
}

// ApplicationWriter contains normalized import, relation, lifecycle, match,
// and append-only event capabilities currently exercised by orchestration.
type ApplicationWriter interface {
	Create(context.Context, application.JobApplication) (application.JobApplication, error)
	Update(context.Context, application.JobApplication) error
	UpsertImported(context.Context, application.JobApplication) (application.JobApplication, bool, error)
	AttachImportedConversation(context.Context, string, string) error
	AttachConversation(context.Context, string, string) error
	UpdateStatus(context.Context, string, application.Status) error
	SetFollowUpState(context.Context, string, conversation.FollowUpState) error
	SaveMatchResult(context.Context, string, vacancy.MatchResult) error
	AppendEvent(context.Context, string, time.Time, application.EventType, string) error
	Save(context.Context) error
}

// ApplicationStore is the read/write capability required by the current
// career synchronization transaction. Dashboard projections and storage-only
// event enumeration remain outside this contract.
type ApplicationStore interface {
	ApplicationReader
	ApplicationWriter
}
