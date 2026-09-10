package jsonstorage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"hh-ai-responder/internal/candidate"
)

type CandidateStories struct {
	Version int                        `json:"version,omitempty"`
	Stories []candidate.CandidateStory `json:"stories"`
}

type CandidateStoryStore struct {
	path string
}

func NewCandidateStoryStore(path string) *CandidateStoryStore {
	return &CandidateStoryStore{path: path}
}

func (s *CandidateStoryStore) Load() (CandidateStories, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return CandidateStories{Version: 1, Stories: []candidate.CandidateStory{}}, nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return CandidateStories{Version: 1, Stories: []candidate.CandidateStory{}}, nil
	}
	if err != nil {
		return CandidateStories{}, fmt.Errorf("read candidate stories: %w", err)
	}
	if containsForbiddenSecret(raw) {
		return CandidateStories{}, errors.New("candidate stories contain a forbidden secret field")
	}
	stories, err := decodeCandidateStories(raw)
	if err != nil {
		return CandidateStories{}, fmt.Errorf("decode candidate stories: %w", err)
	}
	if stories.Version == 0 {
		stories.Version = 1
	}
	for i := range stories.Stories {
		if err := stories.Stories[i].Validate(); err != nil {
			return CandidateStories{}, fmt.Errorf("candidate story %d: %w", i+1, err)
		}
	}
	return stories, nil
}

func decodeCandidateStories(raw []byte) (CandidateStories, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return CandidateStories{}, errors.New("file is empty")
	}
	if trimmed[0] == '[' {
		var stories []candidate.CandidateStory
		if err := json.Unmarshal(trimmed, &stories); err != nil {
			return CandidateStories{}, err
		}
		return CandidateStories{Version: 1, Stories: stories}, nil
	}
	var file CandidateStories
	if err := json.Unmarshal(trimmed, &file); err != nil {
		return CandidateStories{}, err
	}
	if file.Stories == nil {
		return CandidateStories{}, errors.New("stories must be an array")
	}
	if file.Version != 0 && file.Version != 1 {
		return CandidateStories{}, fmt.Errorf("unsupported stories schema version %d", file.Version)
	}
	return file, nil
}

func FormatCandidateStories(stories CandidateStories) (string, error) {
	if stories.Version == 0 {
		stories.Version = 1
	}
	if stories.Stories == nil {
		stories.Stories = []candidate.CandidateStory{}
	}
	encoded, err := json.MarshalIndent(stories, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
