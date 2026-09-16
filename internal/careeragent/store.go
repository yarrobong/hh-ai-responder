package careeragent

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"hh-ai-responder/internal/platform"
)

type RegistryStore struct {
	Path string
}

func (s RegistryStore) Load() (RegistryOverrides, error) {
	if strings.TrimSpace(s.Path) == "" {
		return RegistryOverrides{Enabled: map[string]bool{}}, nil
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return RegistryOverrides{Enabled: map[string]bool{}}, nil
	}
	if err != nil {
		return RegistryOverrides{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return RegistryOverrides{Enabled: map[string]bool{}}, nil
	}
	var value RegistryOverrides
	if err := json.Unmarshal(raw, &value); err != nil {
		return RegistryOverrides{}, err
	}
	if value.Enabled == nil {
		value.Enabled = map[string]bool{}
	}
	return value, nil
}

func (s RegistryStore) Save(value RegistryOverrides) error {
	if strings.TrimSpace(s.Path) == "" {
		return nil
	}
	if value.Enabled == nil {
		value.Enabled = map[string]bool{}
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return platform.WritePrivateFileAtomic(s.Path, append(raw, '\n'), ".career-agent-registry-*.tmp")
}

type FeedbackStore struct {
	Path string
	mu   sync.Mutex
}

type feedbackFile struct {
	Version int        `json:"version"`
	Items   []Feedback `json:"items"`
}

func (s *FeedbackStore) List() ([]Feedback, error) {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return []Feedback{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listUnlocked()
}

func (s *FeedbackStore) listUnlocked() ([]Feedback, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Feedback{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return []Feedback{}, nil
	}
	var value feedbackFile
	if err := json.Unmarshal(raw, &value); err != nil || value.Version != 1 {
		return nil, errors.New("invalid career-agent feedback store")
	}
	return append([]Feedback(nil), value.Items...), nil
}

func (s *FeedbackStore) Add(value Feedback) error {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return errors.New("career-agent feedback store is not configured")
	}
	if value.VacancyID <= 0 || !ValidFeedbackType(value.Type) {
		return errors.New("feedback requires a vacancy id and a supported type")
	}
	value.ID = FeedbackID(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listUnlocked()
	if err != nil {
		return err
	}
	for _, existing := range items {
		if existing.ID == value.ID {
			return nil
		}
	}
	items = append(items, value)
	raw, err := json.MarshalIndent(feedbackFile{Version: 1, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return platform.WritePrivateFileAtomic(s.Path, append(raw, '\n'), ".career-agent-feedback-*.tmp")
}
