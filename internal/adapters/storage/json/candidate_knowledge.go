// Package jsonstorage owns the private JSON persistence mechanics for the
// legacy candidate knowledge collections. It deliberately does not assemble
// CandidateProfile or apply candidate mutation policy.
package jsonstorage

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"hh-ai-responder/internal/candidate"
)

const candidateKnowledgeVersion = 1

// KnowledgeCollections is the storage-compatible name for the typed domain
// snapshot. CandidateProfile and candidate stories intentionally do not
// belong here.
type KnowledgeCollections = candidate.KnowledgeSnapshot

// CandidateKnowledgeStore persists the six knowledge collection files beside
// the configured candidate profile path. Save preserves the historical
// validate-all, stage-all, rename-all behavior; it is not a multi-file ACID
// transaction.
type CandidateKnowledgeStore struct {
	profilePath string
}

func NewCandidateKnowledgeStore(profilePath string) *CandidateKnowledgeStore {
	return &CandidateKnowledgeStore{profilePath: profilePath}
}

func (s *CandidateKnowledgeStore) directory() (string, error) {
	if s == nil || s.profilePath == "" {
		return "", errors.New("knowledge base requires a profile path")
	}
	return filepath.Dir(s.profilePath), nil
}

// Load is read-only and returns empty slices for missing collection files.
// Existing malformed files fail closed.
func (s *CandidateKnowledgeStore) Load() (KnowledgeCollections, error) {
	dir, err := s.directory()
	if err != nil {
		return KnowledgeCollections{}, err
	}
	var result KnowledgeCollections
	if result.Skills, err = loadKnowledgeCollection[candidate.CandidateSkillDetailed](dir, "skills"); err != nil {
		return KnowledgeCollections{}, err
	}
	if result.Projects, err = loadKnowledgeCollection[candidate.CandidateProject](dir, "projects"); err != nil {
		return KnowledgeCollections{}, err
	}
	if result.Achievements, err = loadKnowledgeCollection[candidate.CandidateAchievement](dir, "achievements"); err != nil {
		return KnowledgeCollections{}, err
	}
	if result.Unknowns, err = loadKnowledgeCollection[candidate.CandidateUnknown](dir, "unknowns"); err != nil {
		return KnowledgeCollections{}, err
	}
	if result.Proposals, err = loadKnowledgeCollection[candidate.KnowledgeProposal](dir, "proposals"); err != nil {
		return KnowledgeCollections{}, err
	}
	if result.Events, err = loadKnowledgeCollection[candidate.CandidateKnowledgeEvent](dir, "events"); err != nil {
		return KnowledgeCollections{}, err
	}
	return result, nil
}

// KnowledgeEntity is the common validation/key contract for persisted
// collections. It is intentionally based on domain values, not storage DTOs.
type KnowledgeEntity interface {
	Validate() error
	KnowledgeID() string
}

// ValidateKnowledgeCollection validates entries and rejects duplicate IDs.
func ValidateKnowledgeCollection[T KnowledgeEntity](values []T) error {
	return candidate.ValidateKnowledgeCollection(values)
}

func containsForbiddenSecret(raw []byte) bool {
	text := string(bytes.ToLower(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if bytes.Contains([]byte(text), []byte(marker)) {
			return true
		}
	}
	return false
}

func loadKnowledgeCollection[T KnowledgeEntity](dir, kind string) ([]T, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "candidate_"+kind+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read knowledge %s: %w", kind, err)
	}
	if containsForbiddenSecret(raw) {
		return nil, fmt.Errorf("knowledge %s contains a forbidden secret marker", kind)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode knowledge %s: %w", kind, err)
	}
	var version int
	if len(root) != 2 || json.Unmarshal(root["version"], &version) != nil || version != candidateKnowledgeVersion {
		return nil, fmt.Errorf("knowledge %s requires version 1 and its collection array", kind)
	}
	var values []T
	decoder := json.NewDecoder(bytes.NewReader(root[kind]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("decode knowledge %s entries: %w", kind, err)
	}
	if values == nil {
		return nil, fmt.Errorf("knowledge %s must be an array, not null", kind)
	}
	if err := ValidateKnowledgeCollection(values); err != nil {
		return nil, fmt.Errorf("validate knowledge %s: %w", kind, err)
	}
	return values, nil
}

type knowledgeFile struct {
	kind string
	raw  []byte
}

func encodeKnowledgeCollection[T KnowledgeEntity](kind string, values []T) (knowledgeFile, error) {
	if err := ValidateKnowledgeCollection(values); err != nil {
		return knowledgeFile{}, fmt.Errorf("validate knowledge %s: %w", kind, err)
	}
	if values == nil {
		values = []T{}
	}
	raw, err := json.MarshalIndent(map[string]any{"version": candidateKnowledgeVersion, kind: values}, "", "  ")
	if err != nil {
		return knowledgeFile{}, err
	}
	return knowledgeFile{kind: kind, raw: append(raw, '\n')}, nil
}

// Save validates and stages every collection before replacing any destination.
// The separate renames intentionally preserve the historical partial-save
// behavior if an OS failure occurs during replacement.
func (s *CandidateKnowledgeStore) Save(collections KnowledgeCollections) error {
	dir, err := s.directory()
	if err != nil {
		return err
	}
	files := make([]knowledgeFile, 0, 6)
	appendFile := func(file knowledgeFile, fileErr error) error {
		if fileErr == nil {
			files = append(files, file)
		}
		return fileErr
	}
	if err := appendFile(encodeKnowledgeCollection("skills", collections.Skills)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("projects", collections.Projects)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("achievements", collections.Achievements)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("unknowns", collections.Unknowns)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("proposals", collections.Proposals)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("events", collections.Events)); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create knowledge directory: %w", err)
	}
	var staged []string
	defer func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}()
	for _, file := range files {
		tmp, createErr := os.CreateTemp(dir, ".candidate_"+file.kind+"-*.tmp")
		if createErr != nil {
			return fmt.Errorf("stage knowledge %s: %w", file.kind, createErr)
		}
		staged = append(staged, tmp.Name())
		if _, err = io.Copy(tmp, bytes.NewReader(file.raw)); err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err != nil {
			return fmt.Errorf("write knowledge %s: %w", file.kind, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close knowledge %s: %w", file.kind, closeErr)
		}
	}
	for i, file := range files {
		if err := os.Rename(staged[i], filepath.Join(dir, "candidate_"+file.kind+".json")); err != nil {
			return fmt.Errorf("replace knowledge %s (save may be partial): %w", file.kind, err)
		}
	}
	return nil
}

// NewID preserves the historical opaque identifier format used by local JSON
// stores without exposing any workflow semantics.
func NewID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", kind, err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

// Clone performs the storage boundary's JSON round-trip snapshot operation.
func Clone[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}
