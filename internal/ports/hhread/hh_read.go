// Package hhreadport defines the narrow read-only source used by HH
// synchronization. It intentionally does not import the aggregate ports
// package, whose other interfaces bring unrelated domain dependencies.
package hhreadport

import (
	"context"

	"hh-ai-responder/internal/hhread"
)

// HHReadSource exposes only normalized, read-only HH snapshots.
type HHReadSource interface {
	ReadVacancies(context.Context, string) (hhread.VacancyPage, error)
	ReadApplications(context.Context, string) (hhread.ApplicationPage, error)
	ReadConversations(context.Context, string) (hhread.ConversationPage, error)
}

// BoundedConversationReadSource is the optional read capability used by an
// operator-bounded Career run. Implementations must apply the limit before
// expanding conversation details; zero means the regular unbounded read.
type BoundedConversationReadSource interface {
	ReadConversationsBounded(context.Context, string, int) (hhread.ConversationPage, error)
}

// HHConversationReadSource adds the targeted read used by reconciliation and
// write preflight without exposing any mutation capability.
type HHConversationReadSource interface {
	HHReadSource
	ReadConversation(context.Context, string) (hhread.ConversationRecord, error)
}
