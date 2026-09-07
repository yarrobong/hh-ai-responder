package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const EmployerConversationsFilename = "employer_conversations.json"

var ErrConversationNotFound = errors.New("conversation not found")

// ConversationStore owns its snapshots. Mutations change memory only; Save is
// explicit. The caller serializes all access, including between processes,
// just as for CandidateKnowledgeBase. No method calls HH or an AI service.
type ConversationStore struct {
	byID          map[string]int
	byChatID      map[string]int
	path          string
	conversations []EmployerConversation
}

type conversationStoreFile struct {
	Version       int                    `json:"version"`
	Conversations []EmployerConversation `json:"conversations"`
}

func NewConversationStore(path string) *ConversationStore {
	return &ConversationStore{path: path, conversations: []EmployerConversation{}}
}

func (s *ConversationStore) Load() error {
	start := time.Now()
	defer perfRecord("store.ConversationStore.Load", start, 1)
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("conversation store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.conversations = []EmployerConversation{}
		s.reindex()
		return nil
	}
	if err != nil {
		return errors.New("cannot read conversation store")
	}
	var file conversationStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Conversations == nil {
		return errors.New("invalid conversation store: expected version 1 and conversations array")
	}
	if err := validateConversations(file.Conversations); err != nil {
		return err
	}
	s.conversations = file.Conversations
	s.reindex()
	return nil
}

// Same-directory private temp file + sync + close + rename. Original message
// bodies stay private on disk; never include them in errors or normal logs.
func (s *ConversationStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("conversation store requires a path")
	}
	if err := validateConversations(s.conversations); err != nil {
		return err
	}
	return withStoreLock(s.path, func() error { return s.saveUnlocked() })
}

func (s *ConversationStore) saveUnlocked() error {
	start := time.Now()
	defer perfRecord("store.ConversationStore.saveUnlocked", start, 1)
	values := s.conversations
	if values == nil {
		values = []EmployerConversation{}
	}
	raw, err := json.MarshalIndent(conversationStoreFile{Version: 1, Conversations: values}, "", "  ")
	if err != nil {
		return errors.New("cannot encode conversation store")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("cannot create conversation directory")
	}
	tmp, err := os.CreateTemp(dir, ".employer_conversations-*.tmp")
	if err != nil {
		return errors.New("cannot stage conversation store")
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(raw, '\n')); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write conversation store")
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return errors.New("cannot replace conversation store")
	}
	return nil
}

func validateConversations(values []EmployerConversation) error {
	ids, hhIDs := map[string]bool{}, map[string]bool{}
	for _, value := range values {
		if err := value.validate(); err != nil {
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

// Index publication follows the same caller serialization as snapshot Load.
func (s *ConversationStore) reindex() {
	ids, chats := map[string]int{}, map[string]int{}
	for i, c := range s.conversations {
		ids[c.ID] = i
		if c.HHConversationID != "" {
			chats[c.HHConversationID] = i
		}
	}
	s.byID, s.byChatID = ids, chats
}
func (s *ConversationStore) GetConversation(id string) (EmployerConversation, error) {
	if s != nil {
		if s.byID == nil || len(s.byID) != len(s.conversations) {
			s.reindex()
		}
		if i, ok := s.byID[id]; ok && i < len(s.conversations) && s.conversations[i].ID == id {
			return cloneKnowledge(s.conversations[i])
		}
	}
	return EmployerConversation{}, ErrConversationNotFound
}
func (s *ConversationStore) GetByHHConversationID(id string) (EmployerConversation, error) {
	if s != nil && id != "" {
		if s.byID == nil || len(s.byID) != len(s.conversations) {
			s.reindex()
		}
		if i, ok := s.byChatID[id]; ok && i < len(s.conversations) && s.conversations[i].HHConversationID == id {
			return cloneKnowledge(s.conversations[i])
		}
	}
	return EmployerConversation{}, ErrConversationNotFound
}

// Several conversations may refer to one vacancy (e.g. separate applications).
func (s *ConversationStore) GetByVacancyID(vacancyID int) ([]EmployerConversation, error) {
	return s.list(func(c EmployerConversation) bool { return c.VacancyID == vacancyID })
}

func (s *ConversationStore) ListConversations() ([]EmployerConversation, error) {
	return s.list(func(EmployerConversation) bool { return true })
}

func (s *ConversationStore) ListConversationsByStatus(status ConversationStatus) ([]EmployerConversation, error) {
	return s.list(func(c EmployerConversation) bool { return c.Status == status })
}

func (s *ConversationStore) list(include func(EmployerConversation) bool) ([]EmployerConversation, error) {
	result := []EmployerConversation{}
	if s != nil {
		for _, c := range s.conversations {
			if include(c) {
				result = append(result, c)
			}
		}
	}
	return cloneKnowledge(result)
}

// Upsert accepts a complete snapshot. Existing original messages must remain
// an unchanged prefix; use AppendMessage/UpdateSummary for incremental work.
func (s *ConversationStore) UpsertConversation(value EmployerConversation) (EmployerConversation, error) {
	if s == nil {
		return EmployerConversation{}, errors.New("conversation store is nil")
	}
	c, err := cloneKnowledge(value)
	if err != nil {
		return EmployerConversation{}, errors.New("invalid conversation snapshot")
	}
	if c.ID == "" && c.HHConversationID != "" {
		if old, err := s.GetByHHConversationID(c.HHConversationID); err == nil {
			c.ID = old.ID
		}
	}
	if c.ID == "" {
		c.ID, err = newKnowledgeID("conversation")
		if err != nil {
			return EmployerConversation{}, err
		}
	}
	index := -1
	for i, old := range s.conversations {
		if old.ID != c.ID {
			continue
		}
		index = i
		if c.CreatedAt.IsZero() {
			c.CreatedAt = old.CreatedAt
		}
		if !old.CreatedAt.Equal(c.CreatedAt) || (old.VacancyID != 0 && old.VacancyID != c.VacancyID) ||
			(old.HHConversationID != "" && old.HHConversationID != c.HHConversationID) {
			return EmployerConversation{}, errors.New("cannot reassign an existing conversation")
		}
		if len(c.Messages) < len(old.Messages) {
			return EmployerConversation{}, errors.New("cannot remove original messages")
		}
		for j, message := range old.Messages {
			if message.ID != c.Messages[j].ID || !sameConversationMessage(message, c.Messages[j]) {
				return EmployerConversation{}, errors.New("cannot rewrite original messages")
			}
		}
		// A new summary cannot erase the ledger of already-recorded claims.
		for _, claim := range old.Summary.CandidateClaims {
			found := false
			for _, incoming := range c.Summary.CandidateClaims {
				found = found || reflect.DeepEqual(claim, incoming)
			}
			if !found {
				return EmployerConversation{}, errors.New("cannot remove or rewrite recorded claims")
			}
		}
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.Status == "" {
		c.Status = ConversationApplied
	}
	if c.FollowUpState == "" {
		c.FollowUpState = ConversationFollowUpNone
	}
	if c.Messages == nil {
		c.Messages = []ConversationMessage{}
	}
	c.UpdatedAt = now
	if c.UpdatedAt.Before(c.CreatedAt) {
		c.UpdatedAt = c.CreatedAt
	}
	if index >= 0 && c.UpdatedAt.Before(s.conversations[index].UpdatedAt) {
		c.UpdatedAt = s.conversations[index].UpdatedAt
	}
	c.refreshActivity()
	if c.LastActivityAt != nil && c.UpdatedAt.Before(*c.LastActivityAt) {
		c.UpdatedAt = *c.LastActivityAt
	}
	next := append([]EmployerConversation{}, s.conversations...)
	if index < 0 {
		next = append(next, c)
	} else {
		next[index] = c
	}
	if err := validateConversations(next); err != nil {
		return EmployerConversation{}, err
	}
	result, err := cloneKnowledge(c)
	if err != nil {
		return EmployerConversation{}, err
	}
	s.conversations = next
	s.reindex()
	return result, nil
}

func sameConversationMessage(a, b ConversationMessage) bool {
	return a.ContentUnavailable == b.ContentUnavailable && a.ExternalID == b.ExternalID && a.Timestamp.Equal(b.Timestamp) && a.Sender == b.Sender &&
		a.Text == b.Text && a.Source == b.Source && a.Direction == b.Direction && reflect.DeepEqual(a.Metadata, b.Metadata)
}

func (s *ConversationStore) AppendMessage(id string, value ConversationMessage) (ConversationMessage, error) {
	c, err := s.GetConversation(id)
	if err != nil {
		return ConversationMessage{}, err
	}
	for _, existing := range c.Messages {
		if (value.ExternalID != "" && existing.ExternalID == value.ExternalID && existing.Source == value.Source) ||
			(value.ID != "" && existing.ID == value.ID) {
			if !sameConversationMessage(existing, value) {
				return ConversationMessage{}, errors.New("message identity conflicts with original content")
			}
			return existing, nil
		}
	}
	if value.ID == "" {
		value.ID, err = newKnowledgeID("message")
		if err != nil {
			return ConversationMessage{}, err
		}
	}
	// Timestamp is never guessed: an importer must supply the original time.
	if err := value.validate(); err != nil {
		return ConversationMessage{}, err
	}
	c.Messages = append(c.Messages, value)
	if _, err := s.UpsertConversation(c); err != nil {
		return ConversationMessage{}, err
	}
	return value, nil
}

func (s *ConversationStore) UpdateConversationState(id string, state ConversationState) error {
	c, err := s.GetConversation(id)
	if err != nil {
		return err
	}
	c.Status, c.NextAction, c.WaitingSince, c.FollowUpState = state.Status, state.NextAction, state.WaitingSince, state.FollowUpState
	if c.Status == "" {
		return errors.New("state update requires an explicit status")
	}
	_, err = s.UpsertConversation(c)
	return err
}

// Future summarizers call this explicitly, retaining already-recorded claims.
// This does not run a model and does not modify any original message.
func (s *ConversationStore) UpdateSummary(id string, summary ConversationSummary) error {
	c, err := s.GetConversation(id)
	if err != nil {
		return err
	}
	c.Summary = summary
	_, err = s.UpsertConversation(c)
	return err
}

func (s *ConversationStore) RecordCandidateClaim(id string, claim CandidateConversationClaim) error {
	c, err := s.GetConversation(id)
	if err != nil {
		return err
	}
	for _, old := range c.Summary.CandidateClaims {
		if reflect.DeepEqual(old, claim) {
			return nil
		}
	}
	c.Summary.CandidateClaims = append(c.Summary.CandidateClaims, claim)
	_, err = s.UpsertConversation(c)
	return err
}

// Storage preserves append order. Timeline is a separate stable chronological
// view; equal timestamps retain import order. The returned messages are copies.
func (s *ConversationStore) GetConversationTimeline(id string) ([]ConversationMessage, error) {
	c, err := s.GetConversation(id)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(c.Messages, func(i, j int) bool { return c.Messages[i].Timestamp.Before(c.Messages[j].Timestamp) })
	return c.Messages, nil
}

type ConversationStats struct {
	Total                   int `json:"total"`
	WaitingEmployer         int `json:"waiting_employer"`
	CandidateActionRequired int `json:"candidate_action_required"`
	Interview               int `json:"interview"`
	Offer                   int `json:"offer"`
	Rejected                int `json:"rejected"`
}

func (s *ConversationStore) GetConversationStats() ConversationStats {
	var result ConversationStats
	if s == nil {
		return result
	}
	result.Total = len(s.conversations)
	for _, c := range s.conversations {
		switch c.Status {
		case ConversationWaitingEmployer:
			result.WaitingEmployer++
		case ConversationCandidateActionRequired:
			result.CandidateActionRequired++
		case ConversationInterview:
			result.Interview++
		case ConversationOffer:
			result.Offer++
		case ConversationRejected:
			result.Rejected++
		}
	}
	return result
}
