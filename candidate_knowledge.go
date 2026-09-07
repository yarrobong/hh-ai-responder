package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// KnowledgeSource describes provenance, not a confidence score or permission
// for an AI caller to assert facts. CandidateSource remains the legacy format.
type KnowledgeSource string

const (
	KnowledgeSourceUserConfirmed      KnowledgeSource = "user_confirmed"
	KnowledgeSourceHHResume           KnowledgeSource = "hh_resume"
	KnowledgeSourceGithubVerified     KnowledgeSource = "github_verified"
	KnowledgeSourceCandidateInterview KnowledgeSource = "candidate_interview"
	KnowledgeSourceProjectAnalysis    KnowledgeSource = "project_analysis"
	KnowledgeSourceDerived            KnowledgeSource = "derived"
	KnowledgeSourceUnknown            KnowledgeSource = "unknown"
)

type TruthStatus string

const (
	TruthStatusConfirmed  TruthStatus = "confirmed"
	TruthStatusVerified   TruthStatus = "verified"
	TruthStatusHypothesis TruthStatus = "hypothesis"
	TruthStatusUnknown    TruthStatus = "unknown"
)

// Evidence belongs to an individual source so that a hypothesis cannot borrow
// confirmation merely from the presence of an unrelated trusted source label.
type KnowledgeSourceRecord struct {
	Type       KnowledgeSource `json:"type"`
	Evidence   []string        `json:"evidence,omitempty"`
	Reference  string          `json:"reference,omitempty"`
	ObservedAt *time.Time      `json:"observed_at,omitempty"`
}

type KnowledgeMetadata struct {
	// nil means unmeasured, including all migrated legacy confidence values.
	Confidence  *float64                `json:"confidence"`
	TruthStatus TruthStatus             `json:"truth_status"`
	Sources     []KnowledgeSourceRecord `json:"sources"`
	Evidence    []string                `json:"evidence,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
	ConfirmedAt *time.Time              `json:"confirmed_at,omitempty"`
}

// CandidateSkillDetailed deliberately does not change CandidateSkill or the
// legacy profile's string-valued level field.
type CandidateSkillDetailed struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Category    string     `json:"category,omitempty"`
	Level       SkillLevel `json:"level"`
	Projects    []string   `json:"projects,omitempty"`
	CanDo       []string   `json:"can_do,omitempty"`
	CannotClaim []string   `json:"cannot_claim,omitempty"`
	LastUsed    string     `json:"last_used,omitempty"`
	// Preserve explicit negative legacy facts; unknown level alone is not denial.
	Negative bool `json:"negative,omitempty"`
	KnowledgeMetadata
}

type CandidateProjectType string

const (
	CandidateProjectCommercial CandidateProjectType = "commercial"
	CandidateProjectPersonal   CandidateProjectType = "personal"
	CandidateProjectEducation  CandidateProjectType = "education"
)

// Empty boundaries mean unknown. An empty end date does not imply ongoing work.
type CandidateProjectPeriod struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type CandidateProject struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Type          CandidateProjectType   `json:"type,omitempty"`
	Role          string                 `json:"role,omitempty"`
	Period        CandidateProjectPeriod `json:"period"`
	Description   string                 `json:"description,omitempty"`
	Technologies  []string               `json:"technologies,omitempty"`
	Tasks         []string               `json:"tasks,omitempty"`
	Results       []string               `json:"results,omitempty"`
	RelatedSkills []string               `json:"related_skills,omitempty"`
	KnowledgeMetadata
}

type CandidateAchievement struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Problem      string   `json:"problem,omitempty"`
	Solution     []string `json:"solution,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Result       []string `json:"result,omitempty"`
	Technologies []string `json:"technologies,omitempty"`
	ProjectID    string   `json:"project_id,omitempty"`
	KnowledgeMetadata
}

type CandidateUnknownStatus string

const (
	CandidateUnknownNeedsConfirmation CandidateUnknownStatus = "needs_confirmation"
	CandidateUnknownConfirmed         CandidateUnknownStatus = "confirmed"
	CandidateUnknownRejected          CandidateUnknownStatus = "rejected"
)

type CandidateUnknown struct {
	ID            string                 `json:"id"`
	Question      string                 `json:"question"`
	RelatedEntity string                 `json:"related_entity,omitempty"`
	Hypothesis    string                 `json:"hypothesis,omitempty"`
	Status        CandidateUnknownStatus `json:"status"`
	KnowledgeMetadata
}

// Events are local audit data, never employer-facing context. Actor identifies
// the writer; it is not proof of user confirmation. Raw values are JSON snapshots.
type CandidateKnowledgeEvent struct {
	ID         string          `json:"id"`
	Timestamp  time.Time       `json:"timestamp"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	OldValue   json.RawMessage `json:"old_value"`
	NewValue   json.RawMessage `json:"new_value"`
	Source     KnowledgeSource `json:"source"`
	Actor      string          `json:"actor"`
}

func validKnowledgeSource(source KnowledgeSource) bool {
	switch source {
	case KnowledgeSourceUserConfirmed, KnowledgeSourceHHResume, KnowledgeSourceGithubVerified,
		KnowledgeSourceCandidateInterview, KnowledgeSourceProjectAnalysis, KnowledgeSourceDerived, KnowledgeSourceUnknown:
		return true
	default:
		return false
	}
}

func hasKnowledgeEvidence(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (m KnowledgeMetadata) validate() error {
	if m.Confidence != nil && (math.IsNaN(*m.Confidence) || math.IsInf(*m.Confidence, 0) || *m.Confidence < 0 || *m.Confidence > 1) {
		return errors.New("confidence must be a finite number between 0 and 1, or null")
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() || m.UpdatedAt.Before(m.CreatedAt) {
		return errors.New("knowledge timestamps are missing or out of order")
	}
	if len(m.Sources) == 0 {
		return errors.New("knowledge requires an explicit source (use unknown when unavailable)")
	}
	userEvidence, verifiedEvidence := false, false
	for _, source := range m.Sources {
		if !validKnowledgeSource(source.Type) {
			return errors.New("invalid knowledge source")
		}
		if source.ObservedAt != nil && source.ObservedAt.IsZero() {
			return errors.New("source observed_at must not be zero")
		}
		if hasKnowledgeEvidence(source.Evidence) {
			userEvidence = userEvidence || source.Type == KnowledgeSourceUserConfirmed
			verifiedEvidence = verifiedEvidence || source.Type == KnowledgeSourceHHResume || source.Type == KnowledgeSourceGithubVerified
		}
	}
	switch m.TruthStatus {
	case TruthStatusConfirmed:
		if !userEvidence || m.ConfirmedAt == nil || m.ConfirmedAt.IsZero() {
			return errors.New("confirmed knowledge requires user_confirmed evidence and confirmed_at")
		}
	case TruthStatusVerified:
		if !verifiedEvidence {
			return errors.New("verified knowledge requires HH resume or verified GitHub evidence")
		}
	case TruthStatusHypothesis, TruthStatusUnknown:
		if m.ConfirmedAt != nil {
			return errors.New("unconfirmed knowledge cannot have confirmed_at")
		}
	default:
		return errors.New("invalid truth_status")
	}
	if m.ConfirmedAt != nil && m.ConfirmedAt.IsZero() {
		return errors.New("confirmed_at must not be zero")
	}
	return nil
}

func validateKnowledgeIdentity(id, label string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(label) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	return nil
}

func (s CandidateSkillDetailed) validate() error {
	if err := validateKnowledgeIdentity(s.ID, s.Name); err != nil {
		return err
	}
	if !validSkillLevel(s.Level) {
		return errors.New("invalid detailed skill level")
	}
	return s.KnowledgeMetadata.validate()
}

func (p CandidateProject) validate() error {
	// Legacy ProjectFact permits unnamed entries. Preserve these on migration.
	if err := validateKnowledgeIdentity(p.ID, firstNonEmpty(p.Name, p.Description)); err != nil {
		return err
	}
	switch p.Type {
	case "", CandidateProjectCommercial, CandidateProjectPersonal, CandidateProjectEducation:
	default:
		return errors.New("invalid project type")
	}
	return p.KnowledgeMetadata.validate()
}

func (a CandidateAchievement) validate() error {
	if err := validateKnowledgeIdentity(a.ID, a.Title); err != nil {
		return err
	}
	return a.KnowledgeMetadata.validate()
}

func (u CandidateUnknown) validate() error {
	if err := validateKnowledgeIdentity(u.ID, u.Question); err != nil {
		return err
	}
	if err := u.KnowledgeMetadata.validate(); err != nil {
		return err
	}
	switch u.Status {
	case CandidateUnknownNeedsConfirmation:
		if u.TruthStatus != TruthStatusHypothesis && u.TruthStatus != TruthStatusUnknown {
			return errors.New("pending question must remain hypothesis or unknown")
		}
	case CandidateUnknownConfirmed:
		if u.TruthStatus != TruthStatusConfirmed {
			return errors.New("confirmed question requires explicit user confirmation")
		}
	case CandidateUnknownRejected:
		if u.TruthStatus != TruthStatusHypothesis && u.TruthStatus != TruthStatusUnknown {
			return errors.New("rejected hypothesis cannot be a confirmed or verified fact")
		}
		for _, source := range u.Sources {
			if source.Type == KnowledgeSourceUserConfirmed && hasKnowledgeEvidence(source.Evidence) {
				return nil
			}
		}
		return errors.New("rejection requires explicit user evidence")
	default:
		return errors.New("invalid unknown status")
	}
	return nil
}

func (e CandidateKnowledgeEvent) validate() error {
	if strings.TrimSpace(e.ID) == "" || e.Timestamp.IsZero() || strings.TrimSpace(e.Action) == "" ||
		strings.TrimSpace(e.EntityType) == "" || strings.TrimSpace(e.EntityID) == "" || strings.TrimSpace(e.Actor) == "" {
		return errors.New("incomplete knowledge event")
	}
	if !validKnowledgeSource(e.Source) {
		return errors.New("invalid event source")
	}
	for _, raw := range []json.RawMessage{e.OldValue, e.NewValue} {
		if len(raw) > 0 && !json.Valid(raw) {
			return errors.New("event snapshot must be valid JSON")
		}
	}
	return nil
}

// Validation is repeated at Add, Load and Save boundaries. This layer accepts
// trusted local callers; source authentication belongs to future input adapters.
func validateKnowledgeValue(value interface{ validate() error }) error {
	if err := value.validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode knowledge entity: %w", err)
	}
	if profileContainsSecret(raw) {
		return errors.New("knowledge contains a forbidden secret marker")
	}
	return nil
}
