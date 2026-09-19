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

// VacancyDetailSource is an optional, read-only capability. Search results
// are intentionally allowed to be partial; callers decide which records need
// a bounded detail GET before importing them.
type VacancyDetailSource interface {
	ReadVacancyDetail(context.Context, int) (hhread.VacancyRecord, error)
}

// VacancyDuplicateStateSource is an optional read-only capability for callers
// that require authoritative applicant duplicate-state evidence. Implementors
// must return a typed capability error when the provider relation is absent or
// ambiguous rather than turning unknown state into false.
type VacancyDuplicateStateSource interface {
	ReadVacancyDetailRequiringRelation(context.Context, int) (hhread.VacancyRecord, error)
}

// ResumeReadSource is an optional read-only capability. It is deliberately
// separate from HHReadSource because the browser reader's existing profile
// parser remains authoritative for browser mode.
type ResumeReadSource interface {
	ReadResumes(context.Context) ([]hhread.ResumeRecord, error)
	ReadResume(context.Context, string) (hhread.ResumeRecord, error)
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
