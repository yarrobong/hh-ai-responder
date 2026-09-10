package writeapproval

import (
	"errors"
	"time"
)

type DraftType string

const (
	DraftEmployerReply     DraftType = "employer_reply"
	DraftFollowUp          DraftType = "follow_up"
	DraftCoverLetter       DraftType = "cover_letter"
	DraftApplicationAnswer DraftType = "application_answer"
)

type DraftStatus string

const (
	DraftGenerated  DraftStatus = "generated"
	DraftApproved   DraftStatus = "approved"
	DraftRejected   DraftStatus = "rejected"
	DraftSuperseded DraftStatus = "superseded"
	DraftSent       DraftStatus = "sent"
)

type ActionType string

const (
	ActionConversationReply ActionType = "conversation_reply"
	ActionFollowUp          ActionType = "follow_up"
)

type ActionStatus string

const (
	ActionPending           ActionStatus = "pending"
	ActionApproved          ActionStatus = "approved"
	ActionSending           ActionStatus = "sending"
	ActionSent              ActionStatus = "sent"
	ActionFailed            ActionStatus = "failed"
	ActionCancelled         ActionStatus = "cancelled"
	ActionStale             ActionStatus = "stale"
	ActionSentUnconfirmed   ActionStatus = "sent_unconfirmed"
	ActionDeliveryUncertain ActionStatus = "delivery_uncertain"
	ActionManualReview      ActionStatus = "manual_review"
	ActionDeliveryConfirmed ActionStatus = "delivery_confirmed"
)

// DraftSnapshot is the exact local artifact relevant to approval. Workflow
// metadata not present here is intentionally not made stale by this package.
type DraftSnapshot struct {
	ID                    string
	Type                  DraftType
	ApplicationID         string
	ConversationID        string
	InputFingerprint      string
	EmployerMessageHash   string
	RelevantKnowledgeHash string
	Text                  string
	Status                DraftStatus
}

// MessageSnapshot contains the persisted message fields used by the
// conversation-version contract. Keeping the full shape preserves the
// existing hash for already persisted approvals.
type MessageSnapshot struct {
	HHSystemEvent      bool              `json:"hh_system_event,omitempty"`
	ContentUnavailable bool              `json:"content_unavailable,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	ID                 string            `json:"id"`
	ExternalID         string            `json:"external_id,omitempty"`
	Timestamp          time.Time         `json:"timestamp"`
	Sender             string            `json:"sender"`
	Text               string            `json:"text"`
	Source             string            `json:"source"`
	Direction          string            `json:"direction"`
}

type ConversationSnapshot struct {
	ID              string
	ExternalID      string
	Status          string
	Messages        []MessageSnapshot
	LatestMessageID string
}

// Approval is the local authorization evidence persisted by the root
// composition layer as an ApprovedHHAction. It intentionally does not contain
// remote preflight or transport evidence.
type Approval struct {
	ID                        string
	ActionType                ActionType
	ConversationID            string
	ApplicationID             string
	DraftID                   string
	ReplyPurpose              string
	ApprovedText              string
	ApprovedBy                string
	ApprovedAt                time.Time
	SourceMessageID           string
	ConversationVersion       string
	CandidateKnowledgeVersion string
	RelevantKnowledgeHash     string
	LastMessageID             string
	ContentHash               string
	SendNonce                 string
	Status                    ActionStatus
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

type CreateInput struct {
	Draft                            DraftSnapshot
	ActionType                       ActionType
	Conversation                     ConversationSnapshot
	SourceMessageID                  string
	LastMessageID                    string
	ReplyPurpose                     string
	ApprovedBy                       string
	CurrentRelevantKnowledgeHash     string
	CurrentCandidateKnowledgeVersion string
	ExistingApprovals                []Approval
}

type ApprovalCreated struct {
	Approval    Approval
	Invalidated []Invalidation
}

type Invalidation struct {
	ApprovalID string
	Reason     string
}

type AuthorizationEvidence struct {
	ApprovalID            string
	DraftID               string
	ConversationID        string
	ActionType            ActionType
	ApprovedText          string
	ContentHash           string
	RelevantKnowledgeHash string
	ConversationVersion   string
	LastMessageID         string
	SendNonce             string
}

type ValidationStatus string

const (
	ValidationValid           ValidationStatus = "valid"
	ValidationStale           ValidationStatus = "stale"
	ValidationInvalid         ValidationStatus = "invalid"
	ValidationNeedsReapproval ValidationStatus = "needs_reapproval"
)

type ValidationResult struct {
	Status   ValidationStatus
	Reasons  []string
	Evidence AuthorizationEvidence
}

type ValidateInput struct {
	Approval                         Approval
	Draft                            DraftSnapshot
	Conversation                     ConversationSnapshot
	LatestDeliveredMessageID         string
	CurrentRelevantKnowledgeHash     string
	CurrentCandidateKnowledgeVersion string
	PresentedNonce                   string
}

var (
	ErrApprovedByRequired = errors.New("approved_by is required")
	ErrInvalidDraftState  = errors.New("only a current generated draft can be approved")
	ErrUnsupportedDraft   = errors.New("only conversation reply and follow-up drafts may be sent")
	ErrActiveApproval     = errors.New("conversation already has an active approval for the latest employer message")
	ErrApprovalExists     = errors.New("draft already has an active approval")
	ErrApprovalInvalid    = errors.New("approval evidence is invalid")
	ErrApprovalStale      = errors.New("approval evidence is stale")
	ErrContentMismatch    = errors.New("approved text hash does not match current draft")
	ErrKnowledgeMismatch  = errors.New("candidate knowledge relevant to this draft changed")
	ErrNonceMismatch      = errors.New("approval nonce does not match")
)

func cloneMessages(values []MessageSnapshot) []MessageSnapshot {
	if values == nil {
		return nil
	}
	result := make([]MessageSnapshot, len(values))
	for i, value := range values {
		result[i] = value
		if value.Metadata != nil {
			result[i].Metadata = map[string]string{}
			for key, item := range value.Metadata {
				result[i].Metadata[key] = item
			}
		}
	}
	return result
}
