package hhreadsync

import (
	"context"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/ports"
	hhreadport "hh-ai-responder/internal/ports/hhread"
)

// Target identifies one synchronization stream. The values are intentionally
// strings because they are also the stable CLI/dashboard target names.
type Target string

const (
	TargetVacancies     Target = "vacancies"
	TargetApplications  Target = "applications"
	TargetConversations Target = "conversations"
	TargetInbox         Target = "inbox"
)

// Batch is the normalized, already-read input for one persistence batch.
// Keeping it separate from the source page types also lets the root retain
// its JSON clone/lock and PostgreSQL transaction choreography.
type Batch struct {
	Vacancies     []hhread.VacancyRecord
	Applications  []hhread.ApplicationRecord
	Conversations []hhread.ConversationRecord
}

// Result contains use-case counters and warnings. Transport timing and
// presentation metrics remain outside this package.
type Result struct {
	Fetched                 int       `json:"fetched"`
	Created                 int       `json:"created"`
	Updated                 int       `json:"updated"`
	Unchanged               int       `json:"unchanged"`
	Skipped                 int       `json:"skipped"`
	MetadataChecked         int       `json:"metadata_checked,omitempty"`
	HistoryReused           int       `json:"history_reused,omitempty"`
	DetailedChatsFetched    int       `json:"detailed_chats_fetched,omitempty"`
	SelectedConversationIDs []string  `json:"selected_conversation_ids,omitempty"`
	ChangedConversationIDs  []string  `json:"changed_conversation_ids,omitempty"`
	Errors                  []string  `json:"errors,omitempty"`
	Warnings                []string  `json:"warnings,omitempty"`
	StartedAt               time.Time `json:"started_at"`
	FinishedAt              time.Time `json:"finished_at"`
}

// ImportOptions controls only failure policy. JSON uses the historical
// accumulating behavior; a PostgreSQL transaction uses fail-fast behavior so
// its caller can roll the whole career batch back.
type ImportOptions struct {
	FailFast bool
}

// ReadOptions contains presentation-free hooks for one-run page reads. The
// callback is invoked after each successfully read page, before the next
// cursor is requested; it is useful for caller-owned progress reporting.
type ReadOptions struct {
	OnPage           func(Result)
	MaxConversations int
}

// ConversationResolution is a deliberately small seam for the established
// conversation-state policy. hhreadsync stores the result but does not import
// conversationpolicy or implement reply/terminal decisions.
type ConversationResolution struct {
	Status       conversation.Status
	WaitingSince *time.Time
}

// Dependencies are the only collaborators used by the synchronization
// policy. All are consumer-owned ports or pure callbacks; no concrete HH or
// storage adapter can enter this package.
type Dependencies struct {
	Source        hhreadport.HHReadSource
	Vacancies     ports.VacancyStore
	Applications  ports.ApplicationStore
	Conversations ports.ConversationStore
	MapStatus     func(string) (application.Status, bool)
	WarnStatus    func(string) string
	ResolveState  func(application.JobApplication, conversation.EmployerConversation, *time.Time, bool, []string, time.Time) ConversationResolution
	PendingState  func(string, string) (bool, error)
}

// TargetedConversationSource is the optional narrow read capability needed
// by a targeted conversation refresh. It is intentionally not added to the
// regular HHReadSource port.
type TargetedConversationSource interface {
	ReadConversation(context.Context, string) (hhread.ConversationRecord, error)
}
