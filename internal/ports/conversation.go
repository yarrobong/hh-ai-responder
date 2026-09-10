package ports

import (
	"context"

	"hh-ai-responder/internal/conversation"
)

// ConversationReader contains stable persistence lookups and immutable
// message-history reads used by synchronization and orchestration.
type ConversationReader interface {
	Get(context.Context, string) (conversation.EmployerConversation, error)
	GetByHHConversationID(context.Context, string) (conversation.EmployerConversation, error)
	GetByVacancyID(context.Context, int) ([]conversation.EmployerConversation, error)
	List(context.Context) ([]conversation.EmployerConversation, error)
	Timeline(context.Context, string) ([]conversation.Message, error)
}

// ConversationWriter contains normalized synchronization and state/history
// mutation capabilities. Query projections and context helpers are deferred.
type ConversationWriter interface {
	Upsert(context.Context, conversation.EmployerConversation) (conversation.EmployerConversation, error)
	AppendMessage(context.Context, string, conversation.Message) (conversation.Message, error)
	UpdateState(context.Context, string, conversation.State) error
	UpdateSummary(context.Context, string, conversation.Summary) error
	AppendCandidateClaim(context.Context, string, conversation.Claim) error
}

// ConversationStore is the read/write capability required by the current
// career synchronization transaction.
type ConversationStore interface {
	ConversationReader
	ConversationWriter
}
