package jsonstorage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
)

// CandidateProfileStore owns only the legacy profile file. Import, merge,
// bootstrap, and HH resume workflows remain outside the storage adapter.
type CandidateProfileStore struct {
	path string
}

func NewCandidateProfileStore(path string) *CandidateProfileStore {
	return &CandidateProfileStore{path: path}
}

func (s *CandidateProfileStore) Load() (candidate.CandidateProfile, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return candidate.NewProfile(time.Now()), nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return candidate.NewProfile(time.Now()), nil
	}
	if err != nil {
		return candidate.CandidateProfile{}, fmt.Errorf("read candidate profile: %w", err)
	}
	if containsProfileSecret(raw) {
		return candidate.CandidateProfile{}, errors.New("candidate profile contains a forbidden secret field")
	}
	if err := validateProfileSchema(raw); err != nil {
		return candidate.CandidateProfile{}, err
	}
	var profile candidate.CandidateProfile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return candidate.CandidateProfile{}, fmt.Errorf("decode candidate profile: %w", err)
	}
	if err := candidate.ValidateCandidateProfile(profile); err != nil {
		return candidate.CandidateProfile{}, fmt.Errorf("validate candidate profile: %w", err)
	}
	return profile, nil
}

func validateProfileSchema(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("candidate profile root must be a JSON object: %w", err)
	}
	for _, field := range []string{"version", "updated_at", "identity", "work_preferences", "employer_communication_preferences"} {
		value, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("candidate profile schema is missing %q", field)
		}
	}
	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil || version != 1 {
		return errors.New("candidate profile schema version must be 1")
	}
	return nil
}

func (s *CandidateProfileStore) Save(profile candidate.CandidateProfile) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("candidate profile path is empty")
	}
	if err := candidate.ValidateCandidateProfile(profile); err != nil {
		return err
	}
	profile.Version = 1
	profile.UpdatedAt = time.Now()
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode candidate profile: %w", err)
	}
	if containsProfileSecret(raw) {
		return errors.New("candidate profile would contain a forbidden secret field")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create candidate profile directory: %w", err)
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write candidate profile: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace candidate profile: %w", err)
	}
	return nil
}

func containsProfileSecret(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
