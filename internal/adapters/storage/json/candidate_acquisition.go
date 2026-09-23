package jsonstorage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

type clarificationStoreFile struct {
	Version        int                                                  `json:"version"`
	Clarifications []candidateacquisition.CandidateClarificationRequest `json:"clarifications"`
}

// CandidateClarificationStore owns only the persisted clarification values.
// Deduplication and resolution policy remain in candidateacquisition and the
// candidate mutation use cases.
type CandidateClarificationStore struct {
	path           string
	clarifications []candidateacquisition.CandidateClarificationRequest
}

// Path returns the configured file path for diagnostics and reload planners.
func (s *CandidateClarificationStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func NewCandidateClarificationStore(path string) *CandidateClarificationStore {
	return &CandidateClarificationStore{path: path, clarifications: []candidateacquisition.CandidateClarificationRequest{}}
}

func NewClarificationStore(path string) *CandidateClarificationStore {
	return NewCandidateClarificationStore(path)
}

func validateClarification(value candidateacquisition.CandidateClarificationRequest) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Question) == "" || strings.TrimSpace(value.Topic) == "" || strings.TrimSpace(value.Reason) == "" || value.CreatedAt.IsZero() {
		return errors.New("invalid clarification request")
	}
	switch value.Status {
	case candidateacquisition.ClarificationPending, candidateacquisition.ClarificationAnswered, candidateacquisition.ClarificationDismissed, candidateacquisition.ClarificationResolvedExistingKnowledge:
	default:
		return errors.New("invalid clarification status")
	}
	if value.ResolvedAt != nil && value.ResolvedAt.IsZero() {
		return errors.New("invalid clarification resolved_at")
	}
	if value.Answer != nil && strings.TrimSpace(value.Answer.Raw) == "" {
		return errors.New("clarification answer raw value is required")
	}
	return nil
}

func containsClarificationSecret(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (s *CandidateClarificationStore) Load() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("clarification store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.clarifications = []candidateacquisition.CandidateClarificationRequest{}
		return nil
	}
	if err != nil || containsClarificationSecret(raw) {
		return errors.New("cannot read clarification store")
	}
	var file clarificationStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Clarifications == nil {
		return errors.New("invalid clarification store")
	}
	if err := validateClarifications(file.Clarifications); err != nil {
		return err
	}
	s.clarifications = file.Clarifications
	return nil
}

func validateClarifications(values []candidateacquisition.CandidateClarificationRequest) error {
	ids := map[string]bool{}
	for _, value := range values {
		if err := validateClarification(value); err != nil {
			return err
		}
		if ids[value.ID] {
			return errors.New("duplicate clarification id")
		}
		ids[value.ID] = true
	}
	return nil
}

func (s *CandidateClarificationStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("clarification store requires a path")
	}
	if err := validateClarifications(s.clarifications); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(clarificationStoreFile{Version: 1, Clarifications: s.clarifications}, "", "  ")
	if err != nil {
		return errors.New("cannot encode clarification store")
	}
	return platform.WithPrivateFileLock(s.path, 30*time.Minute, func() error {
		return platform.WritePrivateFileAtomic(s.path, append(raw, '\n'), ".clarifications-*.tmp")
	})
}

func (s *CandidateClarificationStore) Create(value candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error) {
	if s == nil {
		return candidateacquisition.CandidateClarificationRequest{}, errors.New("clarification store is nil")
	}
	if value.ID == "" {
		var err error
		value.ID, err = NewID("clarification")
		if err != nil {
			return candidateacquisition.CandidateClarificationRequest{}, err
		}
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.Status == "" {
		value.Status = candidateacquisition.ClarificationPending
	}
	if err := validateClarification(value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	for _, old := range s.clarifications {
		if old.ID == value.ID {
			return candidateacquisition.CandidateClarificationRequest{}, errors.New("clarification id already exists")
		}
	}
	s.clarifications = append(s.clarifications, value)
	return Clone(value)
}

func (s *CandidateClarificationStore) Get(id string) (candidateacquisition.CandidateClarificationRequest, error) {
	if s != nil {
		for _, value := range s.clarifications {
			if value.ID == id {
				return Clone(value)
			}
		}
	}
	return candidateacquisition.CandidateClarificationRequest{}, errors.New("clarification not found")
}

func (s *CandidateClarificationStore) List() ([]candidateacquisition.CandidateClarificationRequest, error) {
	if s == nil {
		return nil, errors.New("clarification store is nil")
	}
	return Clone(s.clarifications)
}

func (s *CandidateClarificationStore) SetStatus(id string, status candidateacquisition.CandidateClarificationStatus) error {
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	if status != candidateacquisition.ClarificationAnswered && status != candidateacquisition.ClarificationDismissed && status != candidateacquisition.ClarificationPending && status != candidateacquisition.ClarificationResolvedExistingKnowledge {
		return errors.New("invalid clarification status")
	}
	value.Status = status
	if status == candidateacquisition.ClarificationPending {
		value.ResolvedAt = nil
	} else {
		now := time.Now().UTC()
		value.ResolvedAt = &now
	}
	return s.replace(value)
}

func (s *CandidateClarificationStore) RecordAnswer(id string, answer candidateacquisition.CandidateAnswer) error {
	if strings.TrimSpace(answer.Raw) == "" {
		return errors.New("candidate answer is empty")
	}
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	if value.Status != candidateacquisition.ClarificationPending && value.Status != candidateacquisition.ClarificationAnswered {
		return errors.New("clarification is not accepting an answer")
	}
	value.Answer = &answer
	value.Status = candidateacquisition.ClarificationAnswered
	value.ReadyForRegeneration = false
	now := time.Now().UTC()
	value.ResolvedAt = &now
	return s.replace(value)
}

// RecordAnswerEvidence preserves original text without resolving the request.
func (s *CandidateClarificationStore) RecordAnswerEvidence(id string, answer candidateacquisition.CandidateAnswer) error {
	if strings.TrimSpace(answer.Raw) == "" {
		return errors.New("candidate answer is empty")
	}
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	value.Answer = &answer
	return s.replace(value)
}

func (s *CandidateClarificationStore) SetProposalIDs(id string, ids []string) error {
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	value.ProposalIDs = append([]string{}, ids...)
	if len(ids) > 0 {
		value.ProposalID = ids[0]
	}
	return s.replace(value)
}

func (s *CandidateClarificationStore) MarkResolved(id string, status candidateacquisition.CandidateClarificationStatus, reason string) error {
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	if status != candidateacquisition.ClarificationResolvedExistingKnowledge && status != candidateacquisition.ClarificationDismissed {
		return errors.New("invalid resolved clarification status")
	}
	now := time.Now().UTC()
	value.Status, value.ResolvedAt, value.ResolutionReason, value.ReadyForRegeneration = status, &now, reason, status == candidateacquisition.ClarificationResolvedExistingKnowledge
	return s.replace(value)
}

// Reopen returns a rejected interpretation to the candidate-input stage while
// retaining the prior answer and proposal IDs as audit history.
func (s *CandidateClarificationStore) Reopen(id, reason string) error {
	value, err := s.Get(id)
	if err != nil {
		return err
	}
	value.Status = candidateacquisition.ClarificationPending
	value.ResolvedAt = nil
	value.ResolutionReason = strings.TrimSpace(reason)
	value.ReadyForRegeneration = true
	return s.replace(value)
}

// Replace updates one already-present persisted value. Policy callers decide
// whether the new lifecycle value is allowed; this method only stores it.
func (s *CandidateClarificationStore) Replace(value candidateacquisition.CandidateClarificationRequest) error {
	return s.replace(value)
}

func (s *CandidateClarificationStore) replace(value candidateacquisition.CandidateClarificationRequest) error {
	for i := range s.clarifications {
		if s.clarifications[i].ID == value.ID {
			s.clarifications[i] = value
			return nil
		}
	}
	return errors.New("clarification not found")
}

var _ ports.CandidateAcquisitionStore = (*CandidateClarificationStore)(nil)
