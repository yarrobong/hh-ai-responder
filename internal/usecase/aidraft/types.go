package aidraft

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Type string

const (
	TypeFollowUp          Type = "follow_up"
	TypeEmployerReply     Type = "employer_reply"
	TypeCoverLetter       Type = "cover_letter"
	TypeApplicationAnswer Type = "application_answer"
)

type Status string

const (
	StatusGenerated  Status = "generated"
	StatusApproved   Status = "approved"
	StatusRejected   Status = "rejected"
	StatusSuperseded Status = "superseded"
	StatusSent       Status = "sent"
)

type Source string

const (
	SourceAI         Source = "ai"
	SourceUserEdited Source = "user_edited"
)

// Draft is the backend-neutral durable representation shared by JSON and
// PostgreSQL. It intentionally contains no HH transport or approval methods.
type Draft struct {
	InputFingerprint      string    `json:"input_fingerprint,omitempty"`
	PromptVersion         string    `json:"prompt_version,omitempty"`
	EmployerMessageHash   string    `json:"employer_message_hash,omitempty"`
	RelevantKnowledgeHash string    `json:"relevant_knowledge_hash,omitempty"`
	ID                    string    `json:"id"`
	Type                  Type      `json:"type"`
	ApplicationID         string    `json:"application_id,omitempty"`
	ConversationID        string    `json:"conversation_id,omitempty"`
	InputMessageID        string    `json:"input_message_id,omitempty"`
	Text                  string    `json:"text"`
	OriginalText          string    `json:"original_text,omitempty"`
	EditedText            string    `json:"edited_text,omitempty"`
	Source                Source    `json:"source,omitempty"`
	Status                Status    `json:"status"`
	Model                 string    `json:"model,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	DecisionReason        string    `json:"decision_reason"`
	UsedFacts             []string  `json:"used_facts"`
}

func (d Draft) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Text) == "" || d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return errors.New("AI draft requires identity, text and ordered timestamps")
	}
	switch d.Type {
	case TypeFollowUp, TypeEmployerReply, TypeCoverLetter, TypeApplicationAnswer:
	default:
		return errors.New("invalid AI draft type")
	}
	switch d.Status {
	case StatusGenerated, StatusApproved, StatusRejected, StatusSuperseded, StatusSent:
	default:
		return errors.New("invalid AI draft status")
	}
	if strings.TrimSpace(d.DecisionReason) == "" {
		return errors.New("AI draft requires a decision reason")
	}
	if d.Source != "" && d.Source != SourceAI && d.Source != SourceUserEdited {
		return errors.New("invalid AI draft source")
	}
	return nil
}

// Backend is the persistence capability used by the existing runtime store.
// It is context-aware so SQL implementations can honor request cancellation
// while the legacy JSON façade remains source-compatible.
type Backend interface {
	Load(context.Context) ([]Draft, error)
	Save(context.Context, []Draft) error
	Create(context.Context, Draft) (Draft, error)
	UpsertByInputFingerprint(context.Context, Draft) (Draft, bool, error)
	Get(context.Context, string) (Draft, error)
	List(context.Context) ([]Draft, error)
	SetStatus(context.Context, string, Status) error
	UpdateText(context.Context, string, string, Source) error
}
