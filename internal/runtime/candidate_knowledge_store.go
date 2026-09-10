package runtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
)

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

// Compatibility aliases keep mutation and mapping code in the root package;
// validation itself is implemented by the JSON storage adapter.
type knowledgeEntity = jsonstorage.KnowledgeEntity

func validateKnowledgeCollection[T knowledgeEntity](values []T) error {
	for _, value := range values {
		if proposal, ok := any(value).(KnowledgeProposal); ok {
			if err := validateKnowledgeProposal(proposal); err != nil {
				return err
			}
		}
	}
	return jsonstorage.ValidateKnowledgeCollection(values)
}

func NewCandidateKnowledgeBase(profilePath string) *CandidateKnowledgeBase {
	return &CandidateKnowledgeBase{ProfilePath: profilePath}
}

// Load is read-only and replaces in-memory state only after every file passes
// validation. Missing collections are empty; malformed existing files fail closed.
func (kb *CandidateKnowledgeBase) Load() error {
	start := time.Now()
	defer perfRecord("store.CandidateKnowledgeBase.Load", start, 1)
	next := NewCandidateKnowledgeBase(kb.ProfilePath)
	var err error
	if next.Profile, err = LoadCandidateProfile(kb.ProfilePath); err != nil {
		return err
	}
	collections, err := jsonstorage.NewCandidateKnowledgeStore(kb.ProfilePath).Load()
	if err != nil {
		return err
	}
	// Proposal payload validation includes mutation-owned identity/provenance
	// rules and therefore remains in the root compatibility workflow.
	if err := validateKnowledgeCollection(collections.Proposals); err != nil {
		return fmt.Errorf("validate knowledge proposals: %w", err)
	}
	next.Skills, next.Projects, next.Achievements = collections.Skills, collections.Projects, collections.Achievements
	next.Unknowns, next.Proposals, next.Events = collections.Unknowns, collections.Proposals, collections.Events
	*kb = *next
	return nil
}

// Save validates and stages all collections before replacing any destination.
// Each file is atomic (private temp file, sync, close, rename). This is not a
// multi-file transaction: an OS failure during renames can leave a partial save.
// Callers must handle the error; this foundation performs no HH actions.
func (kb *CandidateKnowledgeBase) Save() error {
	if err := validateKnowledgeCollection(kb.Proposals); err != nil {
		return fmt.Errorf("validate knowledge proposals: %w", err)
	}
	return jsonstorage.NewCandidateKnowledgeStore(kb.ProfilePath).Save(jsonstorage.KnowledgeCollections{
		Skills: kb.Skills, Projects: kb.Projects, Achievements: kb.Achievements,
		Unknowns: kb.Unknowns, Proposals: kb.Proposals, Events: kb.Events,
	})
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
		if value.KnowledgeID() == id {
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
	if containsKnowledgeID(*values, value.KnowledgeID()) {
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
		Action: action, EntityType: kind, EntityID: value.KnowledgeID(), NewValue: raw,
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
