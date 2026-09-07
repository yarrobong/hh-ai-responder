package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const candidateKnowledgeVersion = 1

// CandidateKnowledgeBase stores knowledge collections beside ProfilePath.
// Profile is a read-only legacy input here: its existing import/save functions
// remain authoritative. Save never rewrites candidate_profile.json or stories.
// Add methods mutate memory and append audit events; Save explicitly persists.
// A caller must serialize access, including access from other processes.
type CandidateKnowledgeBase struct {
	ProfilePath  string
	Profile      CandidateProfile
	Skills       []CandidateSkillDetailed
	Projects     []CandidateProject
	Achievements []CandidateAchievement
	Unknowns     []CandidateUnknown
	Proposals    []KnowledgeProposal
	Events       []CandidateKnowledgeEvent
}

func NewCandidateKnowledgeBase(profilePath string) *CandidateKnowledgeBase {
	return &CandidateKnowledgeBase{ProfilePath: profilePath}
}

func (kb *CandidateKnowledgeBase) directory() (string, error) {
	if kb.ProfilePath == "" {
		return "", errors.New("knowledge base requires a profile path")
	}
	return filepath.Dir(kb.ProfilePath), nil
}

// Load is read-only and replaces in-memory state only after every file passes
// validation. Missing collections are empty; malformed existing files fail closed.
func (kb *CandidateKnowledgeBase) Load() error {
	start := time.Now()
	defer perfRecord("store.CandidateKnowledgeBase.Load", start, 1)
	dir, err := kb.directory()
	if err != nil {
		return err
	}
	next := NewCandidateKnowledgeBase(kb.ProfilePath)
	if next.Profile, err = LoadCandidateProfile(kb.ProfilePath); err != nil {
		return err
	}
	if next.Skills, err = loadKnowledgeCollection[CandidateSkillDetailed](dir, "skills"); err != nil {
		return err
	}
	if next.Projects, err = loadKnowledgeCollection[CandidateProject](dir, "projects"); err != nil {
		return err
	}
	if next.Achievements, err = loadKnowledgeCollection[CandidateAchievement](dir, "achievements"); err != nil {
		return err
	}
	if next.Unknowns, err = loadKnowledgeCollection[CandidateUnknown](dir, "unknowns"); err != nil {
		return err
	}
	if next.Proposals, err = loadKnowledgeCollection[KnowledgeProposal](dir, "proposals"); err != nil {
		return err
	}
	if next.Events, err = loadKnowledgeCollection[CandidateKnowledgeEvent](dir, "events"); err != nil {
		return err
	}
	*kb = *next
	return nil
}

type knowledgeEntity interface {
	validate() error
	knowledgeID() string
}

func (s CandidateSkillDetailed) knowledgeID() string  { return s.ID }
func (p CandidateProject) knowledgeID() string        { return p.ID }
func (a CandidateAchievement) knowledgeID() string    { return a.ID }
func (u CandidateUnknown) knowledgeID() string        { return u.ID }
func (e CandidateKnowledgeEvent) knowledgeID() string { return e.ID }

func validateKnowledgeCollection[T knowledgeEntity](values []T) error {
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		if err := validateKnowledgeValue(value); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
		if seen[value.knowledgeID()] {
			return errors.New("duplicate knowledge entity id")
		}
		seen[value.knowledgeID()] = true
	}
	return nil
}

func loadKnowledgeCollection[T knowledgeEntity](dir, kind string) ([]T, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "candidate_"+kind+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read knowledge %s: %w", kind, err)
	}
	if profileContainsSecret(raw) {
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
	if err := validateKnowledgeCollection(values); err != nil {
		return nil, fmt.Errorf("validate knowledge %s: %w", kind, err)
	}
	return values, nil
}

type knowledgeFile struct {
	kind string
	raw  []byte
}

func encodeKnowledgeCollection[T knowledgeEntity](kind string, values []T) (knowledgeFile, error) {
	if err := validateKnowledgeCollection(values); err != nil {
		return knowledgeFile{}, fmt.Errorf("validate knowledge %s: %w", kind, err)
	}
	if values == nil {
		values = []T{}
	}
	raw, err := json.MarshalIndent(map[string]any{"version": candidateKnowledgeVersion, kind: values}, "", "  ")
	return knowledgeFile{kind: kind, raw: append(raw, '\n')}, err
}

// Save validates and stages all collections before replacing any destination.
// Each file is atomic (private temp file, sync, close, rename). This is not a
// multi-file transaction: an OS failure during renames can leave a partial save.
// Callers must handle the error; this foundation performs no HH actions.
func (kb *CandidateKnowledgeBase) Save() error {
	dir, err := kb.directory()
	if err != nil {
		return err
	}
	var files []knowledgeFile
	appendFile := func(file knowledgeFile, err error) error {
		if err == nil {
			files = append(files, file)
		}
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("skills", kb.Skills)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("projects", kb.Projects)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("achievements", kb.Achievements)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("unknowns", kb.Unknowns)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("proposals", kb.Proposals)); err != nil {
		return err
	}
	if err := appendFile(encodeKnowledgeCollection("events", kb.Events)); err != nil {
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
		tmp, err := os.CreateTemp(dir, ".candidate_"+file.kind+"-*.tmp")
		if err != nil {
			return fmt.Errorf("stage knowledge %s: %w", file.kind, err)
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

func newKnowledgeID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate knowledge id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func prepareKnowledgeEntity(id *string, meta *KnowledgeMetadata, kind string) error {
	if *id == "" {
		generated, err := newKnowledgeID(kind)
		if err != nil {
			return err
		}
		*id = generated
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	return nil
}

func containsKnowledgeID[T knowledgeEntity](values []T, id string) bool {
	for _, value := range values {
		if value.knowledgeID() == id {
			return true
		}
	}
	return false
}

// Clone on entry so modifying a caller's slices/pointers cannot alter stored
// facts or silently change their event snapshots.
func cloneKnowledge[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	err = json.Unmarshal(raw, &copy)
	return copy, err
}

func addKnowledgeEntity[T knowledgeEntity](kb *CandidateKnowledgeBase, values *[]T, value T, kind string, meta KnowledgeMetadata, actor string) error {
	if err := validateKnowledgeValue(value); err != nil {
		return err
	}
	if containsKnowledgeID(*values, value.knowledgeID()) {
		return errors.New("knowledge entity id already exists; Add does not replace facts")
	}
	copy, err := cloneKnowledge(value)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	source := meta.Sources[0].Type
	for _, record := range meta.Sources {
		if meta.TruthStatus == TruthStatusConfirmed && record.Type == KnowledgeSourceUserConfirmed && hasKnowledgeEvidence(record.Evidence) {
			source = record.Type
			break
		}
		if meta.TruthStatus == TruthStatusVerified && (record.Type == KnowledgeSourceHHResume || record.Type == KnowledgeSourceGithubVerified) && hasKnowledgeEvidence(record.Evidence) {
			source = record.Type
			break
		}
	}
	action := "add"
	if actor == "legacy_migration" {
		action = "migrate"
	}
	if err := kb.AddEvent(CandidateKnowledgeEvent{
		Action: action, EntityType: kind, EntityID: value.knowledgeID(), NewValue: raw,
		Source: source, Actor: actor,
	}); err != nil {
		return err
	}
	*values = append(*values, copy)
	return nil
}

func (kb *CandidateKnowledgeBase) AddSkill(skill CandidateSkillDetailed) error {
	if skill.Level == "" {
		skill.Level = SkillLevelUnknown
	}
	if err := prepareKnowledgeEntity(&skill.ID, &skill.KnowledgeMetadata, "skill"); err != nil {
		return err
	}
	return addKnowledgeEntity(kb, &kb.Skills, skill, "skill", skill.KnowledgeMetadata, "local_storage")
}

func (kb *CandidateKnowledgeBase) AddProject(project CandidateProject) error {
	if err := prepareKnowledgeEntity(&project.ID, &project.KnowledgeMetadata, "project"); err != nil {
		return err
	}
	return addKnowledgeEntity(kb, &kb.Projects, project, "project", project.KnowledgeMetadata, "local_storage")
}

func (kb *CandidateKnowledgeBase) AddAchievement(achievement CandidateAchievement) error {
	if err := prepareKnowledgeEntity(&achievement.ID, &achievement.KnowledgeMetadata, "achievement"); err != nil {
		return err
	}
	return addKnowledgeEntity(kb, &kb.Achievements, achievement, "achievement", achievement.KnowledgeMetadata, "local_storage")
}

func (kb *CandidateKnowledgeBase) AddUnknown(unknown CandidateUnknown) error {
	if err := prepareKnowledgeEntity(&unknown.ID, &unknown.KnowledgeMetadata, "unknown"); err != nil {
		return err
	}
	return addKnowledgeEntity(kb, &kb.Unknowns, unknown, "unknown", unknown.KnowledgeMetadata, "local_storage")
}

func (kb *CandidateKnowledgeBase) AddEvent(event CandidateKnowledgeEvent) error {
	if event.ID == "" {
		id, err := newKnowledgeID("event")
		if err != nil {
			return err
		}
		event.ID = id
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := validateKnowledgeValue(event); err != nil {
		return err
	}
	if containsKnowledgeID(kb.Events, event.ID) {
		return errors.New("knowledge event id already exists")
	}
	copy, err := cloneKnowledge(event)
	if err != nil {
		return err
	}
	kb.Events = append(kb.Events, copy)
	return nil
}

func legacyKnowledgeMetadata(fact ProfileFact, now time.Time) KnowledgeMetadata {
	source := KnowledgeSource(fact.Source)
	if source == "" {
		source = KnowledgeSourceUnknown
	}
	meta := KnowledgeMetadata{
		TruthStatus: TruthStatusUnknown, Sources: []KnowledgeSourceRecord{{Type: source, Evidence: append([]string(nil), fact.Evidence...)}},
		Evidence: append([]string(nil), fact.Evidence...), CreatedAt: now, UpdatedAt: now,
	}
	if !fact.ConfirmedAt.IsZero() {
		observed := fact.ConfirmedAt
		meta.Sources[0].ObservedAt = &observed
	}
	if source == KnowledgeSourceDerived {
		meta.TruthStatus = TruthStatusHypothesis
	}
	if fact.Confirmed && hasKnowledgeEvidence(fact.Evidence) {
		switch source {
		case KnowledgeSourceUserConfirmed:
			meta.TruthStatus = TruthStatusConfirmed
		case KnowledgeSourceHHResume, KnowledgeSourceGithubVerified:
			meta.TruthStatus = TruthStatusVerified
		}
		confirmed := fact.ConfirmedAt
		meta.ConfirmedAt = &confirmed
	}
	return meta
}

func legacyKnowledgeID(kind string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("legacy-%s-%x", kind, sum[:16]), nil
}

// MigrateLegacyProfile copies legacy facts already loaded in Profile. It does
// not infer project types/periods, confidence, abilities, or skills from stories.
// IDs fingerprint each legacy assertion, preserving conflicting assertions
// without overwriting enriched records. Re-running adds neither facts nor events.
// Load, MigrateLegacyProfile, Save is the explicit migration sequence.
func (kb *CandidateKnowledgeBase) MigrateLegacyProfile() error {
	if err := validateCandidateProfile(kb.Profile); err != nil {
		return fmt.Errorf("validate migration profile: %w", err)
	}
	next, err := cloneKnowledge(*kb)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, fact := range kb.Profile.Skills {
		key := fact
		key.Name = canonicalSkillName(key.Name)
		id, err := legacyKnowledgeID("skill", key)
		if err != nil {
			return err
		}
		if containsKnowledgeID(next.Skills, id) {
			continue
		}
		skill := CandidateSkillDetailed{
			ID: id, Name: fact.Name, Level: fact.Level, Negative: fact.Negative,
			KnowledgeMetadata: legacyKnowledgeMetadata(fact.ProfileFact, now),
		}
		if err := addKnowledgeEntity(&next, &next.Skills, skill, "skill", skill.KnowledgeMetadata, "legacy_migration"); err != nil {
			return fmt.Errorf("migrate legacy skill: %w", err)
		}
	}
	for _, fact := range kb.Profile.Projects {
		key := fact
		key.Name = normalizeProfileName(key.Name)
		id, err := legacyKnowledgeID("project", key)
		if err != nil {
			return err
		}
		if containsKnowledgeID(next.Projects, id) {
			continue
		}
		project := CandidateProject{
			ID: id, Name: fact.Name, Role: fact.Role, Description: fact.Description,
			Technologies:      append([]string(nil), fact.Technologies...),
			KnowledgeMetadata: legacyKnowledgeMetadata(fact.ProfileFact, now),
		}
		if fact.BusinessImpact != "" {
			project.Results = []string{fact.BusinessImpact}
		}
		if err := addKnowledgeEntity(&next, &next.Projects, project, "project", project.KnowledgeMetadata, "legacy_migration"); err != nil {
			return fmt.Errorf("migrate legacy project: %w", err)
		}
	}
	*kb = next
	return nil
}
