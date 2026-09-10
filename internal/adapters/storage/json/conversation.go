package jsonstorage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
)

const EmployerConversationsFilename = "employer_conversations.json"

var ErrConversationNotFound = conversation.ErrConversationNotFound

// ConversationStoreFile is exported only so the root compatibility/read-model
// code can decode the established envelope without owning its schema.
type ConversationStoreFile struct {
	Version       int                                 `json:"version"`
	Conversations []conversation.EmployerConversation `json:"conversations"`
}

// ConversationRepository is the authoritative JSON-backed conversation store.
// Mutations affect memory only; Save is explicit. The repository contains no
// HH, candidate, AI, dashboard, or policy behavior.
type ConversationRepository struct {
	mu            sync.RWMutex
	path          string
	byID          map[string]int
	byHHID        map[string]int
	conversations []conversation.EmployerConversation
}

func NewConversationRepository(path string) *ConversationRepository {
	if strings.TrimSpace(path) == "" {
		path = EmployerConversationsFilename
	}
	return &ConversationRepository{
		path:          path,
		byID:          map[string]int{},
		byHHID:        map[string]int{},
		conversations: []conversation.EmployerConversation{},
	}
}

// SetPath is a narrow compatibility hook for the legacy root façade, whose
// unexported path field is still inspected and changed by old workflows/tests.
func (r *ConversationRepository) SetPath(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.path = path
	r.mu.Unlock()
}

func (r *ConversationRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("conversation store requires a path")
	}
	raw, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.mu.Lock()
		r.conversations = []conversation.EmployerConversation{}
		r.reindexLocked()
		r.mu.Unlock()
		return nil
	}
	if err != nil {
		return errors.New("cannot read conversation store")
	}

	var file ConversationStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Conversations == nil {
		return errors.New("invalid conversation store: expected version 1 and conversations array")
	}
	if err := ValidateConversations(file.Conversations); err != nil {
		return err
	}
	values, err := cloneConversations(file.Conversations)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.conversations = values
	r.reindexLocked()
	r.mu.Unlock()
	return nil
}

func (r *ConversationRepository) Save(ctx context.Context) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("conversation store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		return r.SaveUnlocked(ctx)
	})
}

// SaveUnlocked preserves the existing batch-sync lock choreography. Callers
// must hold the process lock when using it directly.
func (r *ConversationRepository) SaveUnlocked(ctx context.Context) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("conversation store requires a path")
	}
	r.mu.RLock()
	values, err := cloneConversations(r.conversations)
	r.mu.RUnlock()
	if err != nil {
		return err
	}
	if values == nil {
		values = []conversation.EmployerConversation{}
	}
	if err := ValidateConversations(values); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(ConversationStoreFile{Version: 1, Conversations: values}, "", "  ")
	if err != nil {
		return errors.New("cannot encode conversation store")
	}
	return platform.WritePrivateFileAtomic(r.path, append(raw, '\n'), ".employer_conversations-*.tmp")
}

func (r *ConversationRepository) Get(ctx context.Context, id string) (conversation.EmployerConversation, error) {
	if err := conversationContextError(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if r == nil {
		return conversation.EmployerConversation{}, ErrConversationNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if index, ok := r.byID[id]; ok && index < len(r.conversations) {
		return cloneConversation(r.conversations[index])
	}
	return conversation.EmployerConversation{}, ErrConversationNotFound
}

func (r *ConversationRepository) GetByHHConversationID(ctx context.Context, id string) (conversation.EmployerConversation, error) {
	if err := conversationContextError(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if r == nil || id == "" {
		return conversation.EmployerConversation{}, ErrConversationNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if index, ok := r.byHHID[id]; ok && index < len(r.conversations) {
		return cloneConversation(r.conversations[index])
	}
	return conversation.EmployerConversation{}, ErrConversationNotFound
}

func (r *ConversationRepository) GetByVacancyID(ctx context.Context, id int) ([]conversation.EmployerConversation, error) {
	if err := conversationContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return []conversation.EmployerConversation{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]conversation.EmployerConversation, 0)
	for _, value := range r.conversations {
		if value.VacancyID == id {
			values = append(values, value)
		}
	}
	return cloneConversations(values)
}

func (r *ConversationRepository) List(ctx context.Context) ([]conversation.EmployerConversation, error) {
	if err := conversationContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return []conversation.EmployerConversation{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneConversations(r.conversations)
}

func (r *ConversationRepository) Upsert(ctx context.Context, value conversation.EmployerConversation) (conversation.EmployerConversation, error) {
	if err := conversationContextError(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if r == nil {
		return conversation.EmployerConversation{}, errors.New("conversation repository is not configured")
	}
	c, err := cloneConversation(value)
	if err != nil {
		return conversation.EmployerConversation{}, errors.New("invalid conversation snapshot")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if c.ID == "" && c.HHConversationID != "" {
		if index, ok := r.byHHID[c.HHConversationID]; ok && index < len(r.conversations) {
			c.ID = r.conversations[index].ID
		}
	}
	if c.ID == "" {
		c.ID, err = newConversationID()
		if err != nil {
			return conversation.EmployerConversation{}, err
		}
	}

	index := -1
	for i, old := range r.conversations {
		if old.ID != c.ID {
			continue
		}
		index = i
		if c.CreatedAt.IsZero() {
			c.CreatedAt = old.CreatedAt
		}
		if !old.CreatedAt.Equal(c.CreatedAt) || (old.VacancyID != 0 && old.VacancyID != c.VacancyID) ||
			(old.HHConversationID != "" && old.HHConversationID != c.HHConversationID) {
			return conversation.EmployerConversation{}, errors.New("cannot reassign an existing conversation")
		}
		if len(c.Messages) < len(old.Messages) {
			return conversation.EmployerConversation{}, errors.New("cannot remove original messages")
		}
		for j, message := range old.Messages {
			if message.ID != c.Messages[j].ID || !conversation.SameMessage(message, c.Messages[j]) {
				return conversation.EmployerConversation{}, errors.New("cannot rewrite original messages")
			}
		}
		for _, claim := range old.Summary.CandidateClaims {
			found := false
			for _, incoming := range c.Summary.CandidateClaims {
				found = found || reflect.DeepEqual(claim, incoming)
			}
			if !found {
				return conversation.EmployerConversation{}, errors.New("cannot remove or rewrite recorded claims")
			}
		}
	}

	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.Status == "" {
		c.Status = conversation.StatusApplied
	}
	if c.FollowUpState == "" {
		c.FollowUpState = conversation.FollowUpNone
	}
	if c.Messages == nil {
		c.Messages = []conversation.Message{}
	}
	c.UpdatedAt = now
	if c.UpdatedAt.Before(c.CreatedAt) {
		c.UpdatedAt = c.CreatedAt
	}
	if index >= 0 && c.UpdatedAt.Before(r.conversations[index].UpdatedAt) {
		c.UpdatedAt = r.conversations[index].UpdatedAt
	}
	c.RefreshActivity()
	if c.LastActivityAt != nil && c.UpdatedAt.Before(*c.LastActivityAt) {
		c.UpdatedAt = *c.LastActivityAt
	}

	next := append([]conversation.EmployerConversation{}, r.conversations...)
	if index < 0 {
		next = append(next, c)
	} else {
		next[index] = c
	}
	if err := ValidateConversations(next); err != nil {
		return conversation.EmployerConversation{}, err
	}
	r.conversations = next
	r.reindexLocked()
	return cloneConversation(c)
}

func (r *ConversationRepository) AppendMessage(ctx context.Context, id string, value conversation.Message) (conversation.Message, error) {
	if err := conversationContextError(ctx); err != nil {
		return conversation.Message{}, err
	}
	if r == nil {
		return conversation.Message{}, errors.New("conversation repository is not configured")
	}
	c, err := r.Get(ctx, id)
	if err != nil {
		return conversation.Message{}, err
	}
	for _, existing := range c.Messages {
		if (value.ExternalID != "" && existing.ExternalID == value.ExternalID && existing.Source == value.Source) ||
			(value.ID != "" && existing.ID == value.ID) {
			if !conversation.SameMessage(existing, value) {
				return conversation.Message{}, errors.New("message identity conflicts with original content")
			}
			return existing, nil
		}
	}
	if value.ID == "" {
		value.ID, err = newMessageID()
		if err != nil {
			return conversation.Message{}, err
		}
	}
	if err := value.Validate(); err != nil {
		return conversation.Message{}, err
	}
	c.Messages = append(c.Messages, value)
	if _, err := r.Upsert(ctx, c); err != nil {
		return conversation.Message{}, err
	}
	return value, nil
}

func (r *ConversationRepository) UpdateState(ctx context.Context, id string, state conversation.State) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	c, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	c.Status, c.NextAction, c.WaitingSince, c.FollowUpState = state.Status, state.NextAction, state.WaitingSince, state.FollowUpState
	if c.Status == "" {
		return errors.New("state update requires an explicit status")
	}
	_, err = r.Upsert(ctx, c)
	return err
}

func (r *ConversationRepository) UpdateSummary(ctx context.Context, id string, summary conversation.Summary) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	c, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	c.Summary = summary
	_, err = r.Upsert(ctx, c)
	return err
}

func (r *ConversationRepository) AppendCandidateClaim(ctx context.Context, id string, claim conversation.Claim) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	c, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	for _, old := range c.Summary.CandidateClaims {
		if reflect.DeepEqual(old, claim) {
			return nil
		}
	}
	c.Summary.CandidateClaims = append(c.Summary.CandidateClaims, claim)
	_, err = r.Upsert(ctx, c)
	return err
}

// Timeline returns the persisted message history in stable chronological
// order. Equal timestamps retain append order; returned values are detached.
func (r *ConversationRepository) Timeline(ctx context.Context, id string) ([]conversation.Message, error) {
	if err := conversationContextError(ctx); err != nil {
		return nil, err
	}
	c, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(c.Messages, func(i, j int) bool { return c.Messages[i].Timestamp.Before(c.Messages[j].Timestamp) })
	return c.Messages, nil
}

// Snapshot and ReplaceSnapshot are narrow compatibility bridges for the root
// façade and the existing staged HH synchronization transaction. They do not
// broaden the ports or expose repository-owned slices.
func (r *ConversationRepository) Snapshot(ctx context.Context) ([]conversation.EmployerConversation, error) {
	return r.List(ctx)
}

func (r *ConversationRepository) ReplaceSnapshot(ctx context.Context, values []conversation.EmployerConversation) error {
	if err := conversationContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("conversation repository is not configured")
	}
	if err := ValidateConversations(values); err != nil {
		return err
	}
	copyValues, err := cloneConversations(values)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.conversations = copyValues
	r.reindexLocked()
	r.mu.Unlock()
	return nil
}

func (r *ConversationRepository) Clone() (*ConversationRepository, error) {
	if r == nil {
		return nil, errors.New("conversation repository is not configured")
	}
	values, err := r.Snapshot(context.Background())
	if err != nil {
		return nil, err
	}
	clone := NewConversationRepository(r.path)
	clone.conversations = values
	clone.reindexLocked()
	return clone, nil
}

func (r *ConversationRepository) reindexLocked() {
	r.byID = make(map[string]int, len(r.conversations))
	r.byHHID = make(map[string]int, len(r.conversations))
	for i, value := range r.conversations {
		r.byID[value.ID] = i
		if value.HHConversationID != "" {
			r.byHHID[value.HHConversationID] = i
		}
	}
}

func ValidateConversations(values []conversation.EmployerConversation) error {
	ids, hhIDs := map[string]bool{}, map[string]bool{}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		if ids[value.ID] {
			return errors.New("duplicate conversation id")
		}
		ids[value.ID] = true
		if value.HHConversationID != "" {
			if hhIDs[value.HHConversationID] {
				return errors.New("duplicate HH conversation id")
			}
			hhIDs[value.HHConversationID] = true
		}
	}
	return nil
}

func cloneConversation(value conversation.EmployerConversation) (conversation.EmployerConversation, error) {
	var copy conversation.EmployerConversation
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}

func cloneConversations(values []conversation.EmployerConversation) ([]conversation.EmployerConversation, error) {
	if values == nil {
		return []conversation.EmployerConversation{}, nil
	}
	result := make([]conversation.EmployerConversation, len(values))
	for i, value := range values {
		copy, err := cloneConversation(value)
		if err != nil {
			return nil, err
		}
		result[i] = copy
	}
	return result, nil
}

func newConversationID() (string, error) {
	return newConversationScopedID("conversation")
}

func newMessageID() (string, error) {
	return newConversationScopedID("message")
}

func newConversationScopedID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate knowledge id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func conversationContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

var _ ports.ConversationReader = (*ConversationRepository)(nil)
var _ ports.ConversationWriter = (*ConversationRepository)(nil)
var _ ports.ConversationStore = (*ConversationRepository)(nil)
