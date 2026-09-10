package autochatorchestration

import (
	"context"
	"time"

	autochatattemptport "hh-ai-responder/internal/ports/autochatattempt"
	"hh-ai-responder/internal/usecase/autochatreply"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

// Chat is the detached, already-normalized snapshot selected by the legacy
// HH read path. It contains no provider DTO or mutation capability.
type Chat struct {
	ID                  int64
	TriggerMessageID    string
	ContactName         string
	EmployerMessage     string
	VacancyName         string
	VacancyURL          string
	CompanyName         string
	VacancyCompensation string
	ReplyOptions        []autochatreply.Button
	ResumeHash          string
	ResumeTitle         string
	ApplicantID         int64
	Candidate           autochatreply.Candidate
	IsDiscard           bool
}

// History is the bounded normalized chat history used for the safety decision
// and for one proposal attempt.
type History struct {
	Messages         []autochatreply.HistoryMessage
	WriteAllowed     bool
	TriggerMessageID string
}

// ChatSource is the narrow read capability required by this iteration. The
// source owns provider-specific discovery/resource normalization; the service
// owns the per-chat sequence and policy gates.
type ChatSource interface {
	AwaitingChats(context.Context, int) ([]Chat, error)
	ReadHistory(context.Context, int64, int64) (History, error)
	IgnoreChat(int64)
}

// ReplyPreparer is the semantic R10 proposal boundary. It must not send or
// leave a chat and must not expose a generic completion provider here.
type ReplyPreparer interface {
	Prepare(context.Context, autochatreply.Input) (autochatreply.ProposedResult, error)
}

type ActionOutcome string

const (
	ActionAccepted          ActionOutcome = "accepted"
	ActionRejected          ActionOutcome = "rejected"
	ActionNotSent           ActionOutcome = "not_sent"
	ActionDeliveryUncertain ActionOutcome = "delivery_uncertain"
	ActionBlocked           ActionOutcome = "blocked"
	ActionNoOp              ActionOutcome = "no_op"
)

// ActionResult contains only provider-neutral delivery evidence. An
// ambiguous result is terminal for this invocation and must never be retried.
type ActionResult struct {
	Outcome        ActionOutcome
	ProviderID     string
	ProviderStatus int
	Metadata       map[string]string
	Error          string
}

// ChatActionExecutor is the only external-action capability visible to this
// package. Its implementation is responsible for the existing HH write gate
// and R11 gateway path.
type ChatActionExecutor interface {
	SendChatMessage(context.Context, int64, string) (ActionResult, error)
	LeaveChat(context.Context, int64) (ActionResult, error)
}

// ReservedChatActionExecutor is the write capability used when durable replay
// protection is enabled. The attempt/request key arrives from the persisted
// reservation; implementations must not create a replacement key.
type ReservedChatActionExecutor interface {
	SendChatMessageForAttempt(context.Context, ActionRequest, string) (ActionResult, error)
	LeaveChatForAttempt(context.Context, ActionRequest) (ActionResult, error)
}

type ActionRequest struct {
	AttemptID      string
	ConversationID string
	RequestKey     string
}

type AuditEvent struct {
	Type             string
	ChatID           int64
	AttemptID        string
	ConversationID   string
	TriggerMessageID string
	State            string
	Resume           string
	ResumeTitle      string
	EmployerMsg      string
	Reply            string
	ReviewReason     string
	Error            string
	At               time.Time
}

type AuditSink interface {
	Append(context.Context, AuditEvent) error
}

type Logger interface {
	Debug(string, ...any)
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}

type Dependencies struct {
	Chats         ChatSource
	ReplyPreparer ReplyPreparer
	Actions       ChatActionExecutor
	Audit         AuditSink
	Logger        Logger
	Attempts      autochatattemptport.Store
	Notifications reliabilitynotifications.Sink
}

type Options struct {
	Mode                 string
	DryRun               bool
	MaxPages             int
	MaxHistoryMessages   int
	CommunicationProfile string
	GitHubURL            string
	Contacts             string
	ExtraPrompt          string
	DurableAttempts      bool
	AttemptStoreInitErr  error
	WriteEnabled         bool
	Now                  func() time.Time
}

type Input struct{}

type ItemOutcome string

const (
	ItemReplySent         ItemOutcome = "reply_sent"
	ItemLeaveExecuted     ItemOutcome = "leave_executed"
	ItemManualReview      ItemOutcome = "manual_review"
	ItemNoReply           ItemOutcome = "no_reply"
	ItemSkipped           ItemOutcome = "skipped"
	ItemFailed            ItemOutcome = "failed"
	ItemDeliveryUncertain ItemOutcome = "delivery_uncertain"
)

type ItemResult struct {
	ChatID  int64
	Outcome ItemOutcome
	Error   string
}

type Result struct {
	Seen         int
	Eligible     int
	Generated    int
	Replied      int
	Left         int
	ManualReview int
	Skipped      int
	Errors       int
	Items        []ItemResult
}
