package candidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type KnowledgeSource string

const (
	KnowledgeSourceUserConfirmed        KnowledgeSource = "user_confirmed"
	KnowledgeSourceHHResume             KnowledgeSource = "hh_resume"
	KnowledgeSourceGithubVerified       KnowledgeSource = "github_verified"
	KnowledgeSourceCandidateInterview   KnowledgeSource = "candidate_interview"
	KnowledgeSourceProjectAnalysis      KnowledgeSource = "project_analysis"
	KnowledgeSourceEmployerConversation KnowledgeSource = "employer_conversation"
	KnowledgeSourceDerived              KnowledgeSource = "derived"
	KnowledgeSourceUnknown              KnowledgeSource = "unknown"
)

// TruthStatus is deliberately not a boolean. Confidence and provenance never
// promote a hypothesis or unknown value into a confirmed/verified fact.
type TruthStatus string

const (
	TruthStatusConfirmed  TruthStatus = "confirmed"
	TruthStatusVerified   TruthStatus = "verified"
	TruthStatusHypothesis TruthStatus = "hypothesis"
	TruthStatusUnknown    TruthStatus = "unknown"
)

type KnowledgeSourceRecord struct {
	Type       KnowledgeSource `json:"type"`
	Evidence   []string        `json:"evidence,omitempty"`
	Reference  string          `json:"reference,omitempty"`
	ObservedAt *time.Time      `json:"observed_at,omitempty"`
}

type KnowledgeMetadata struct {
	Confidence  *float64                `json:"confidence"`
	TruthStatus TruthStatus             `json:"truth_status"`
	Sources     []KnowledgeSourceRecord `json:"sources"`
	Evidence    []string                `json:"evidence,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
	ConfirmedAt *time.Time              `json:"confirmed_at,omitempty"`
}

type CandidateSkillDetailed struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Category    string              `json:"category,omitempty"`
	Level       SkillLevel          `json:"level"`
	Projects    []string            `json:"projects,omitempty"`
	Uses        []CanonicalSkillUse `json:"uses,omitempty"`
	CanDo       []string            `json:"can_do,omitempty"`
	CannotClaim []string            `json:"cannot_claim,omitempty"`
	LastUsed    string              `json:"last_used,omitempty"`
	Negative    bool                `json:"negative,omitempty"`
	KnowledgeMetadata
}

type CandidateProjectType string

const (
	CandidateProjectCommercial CandidateProjectType = "commercial"
	CandidateProjectPersonal   CandidateProjectType = "personal"
	CandidateProjectEducation  CandidateProjectType = "education"
)

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
	CandidateUnknownDismissed         CandidateUnknownStatus = "dismissed"
	CandidateUnknownSuperseded        CandidateUnknownStatus = "superseded"
)

type CandidateUnknown struct {
	ID                string                 `json:"id"`
	Question          string                 `json:"question"`
	RelatedEntity     string                 `json:"related_entity,omitempty"`
	Hypothesis        string                 `json:"hypothesis,omitempty"`
	Status            CandidateUnknownStatus `json:"status"`
	GapKey            string                 `json:"gap_key,omitempty"`
	Source            KnowledgeSource        `json:"source,omitempty"`
	ConversationID    string                 `json:"conversation_id,omitempty"`
	ApplicationID     string                 `json:"application_id,omitempty"`
	VacancyID         string                 `json:"vacancy_id,omitempty"`
	EmployerMessageID string                 `json:"employer_message_id,omitempty"`
	KnowledgeMetadata
}
type CandidateKnowledgeEvent struct {
	ID                string          `json:"id"`
	Timestamp         time.Time       `json:"timestamp"`
	Action            string          `json:"action"`
	EntityType        string          `json:"entity_type"`
	EntityID          string          `json:"entity_id"`
	OldValue          json.RawMessage `json:"old_value"`
	NewValue          json.RawMessage `json:"new_value"`
	Source            KnowledgeSource `json:"source"`
	Actor             string          `json:"actor"`
	UnknownID         string          `json:"unknown_id,omitempty"`
	ClarificationID   string          `json:"clarification_id,omitempty"`
	ProposalID        string          `json:"proposal_id,omitempty"`
	ConversationID    string          `json:"conversation_id,omitempty"`
	EmployerMessageID string          `json:"employer_message_id,omitempty"`
}

func (m KnowledgeMetadata) Metadata() KnowledgeMetadata { return m }
func (s CandidateSkillDetailed) KnowledgeMetadataValue() KnowledgeMetadata {
	return s.KnowledgeMetadata
}
func (p CandidateProject) KnowledgeMetadataValue() KnowledgeMetadata     { return p.KnowledgeMetadata }
func (a CandidateAchievement) KnowledgeMetadataValue() KnowledgeMetadata { return a.KnowledgeMetadata }
func (u CandidateUnknown) KnowledgeMetadataValue() KnowledgeMetadata     { return u.KnowledgeMetadata }
func (m KnowledgeMetadata) IsEmpty() bool {
	return m.Confidence == nil && m.TruthStatus == "" && len(m.Sources) == 0 && len(m.Evidence) == 0 && m.CreatedAt.IsZero() && m.UpdatedAt.IsZero() && m.ConfirmedAt == nil
}
func (s CandidateSkillDetailed) KnowledgeID() string  { return s.ID }
func (p CandidateProject) KnowledgeID() string        { return p.ID }
func (a CandidateAchievement) KnowledgeID() string    { return a.ID }
func (u CandidateUnknown) KnowledgeID() string        { return u.ID }
func (e CandidateKnowledgeEvent) KnowledgeID() string { return e.ID }

func validKnowledgeSource(source KnowledgeSource) bool {
	switch source {
	case KnowledgeSourceUserConfirmed, KnowledgeSourceHHResume, KnowledgeSourceGithubVerified, KnowledgeSourceCandidateInterview, KnowledgeSourceProjectAnalysis, KnowledgeSourceEmployerConversation, KnowledgeSourceDerived, KnowledgeSourceUnknown:
		return true
	default:
		return false
	}
}
func ValidKnowledgeSource(source KnowledgeSource) bool { return validKnowledgeSource(source) }
func HasKnowledgeEvidence(values []string) bool        { return hasKnowledgeEvidence(values) }

func ValidTruthStatus(status TruthStatus) bool {
	switch status {
	case TruthStatusConfirmed, TruthStatusVerified, TruthStatusHypothesis, TruthStatusUnknown:
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

func (m KnowledgeMetadata) Validate() error {
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

func (s CandidateSkillDetailed) Validate() error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Name) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	if !ValidSkillLevel(s.Level) {
		return errors.New("invalid detailed skill level")
	}
	return s.KnowledgeMetadata.Validate()
}
func (p CandidateProject) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" && strings.TrimSpace(p.Description) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	switch p.Type {
	case "", CandidateProjectCommercial, CandidateProjectPersonal, CandidateProjectEducation:
	default:
		return errors.New("invalid project type")
	}
	return p.KnowledgeMetadata.Validate()
}
func (a CandidateAchievement) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Title) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	return a.KnowledgeMetadata.Validate()
}
func (u CandidateUnknown) Validate() error {
	if strings.TrimSpace(u.ID) == "" || strings.TrimSpace(u.Question) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	if u.Source != "" && !validKnowledgeSource(u.Source) {
		return errors.New("invalid unknown provenance source")
	}
	if err := u.KnowledgeMetadata.Validate(); err != nil {
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
	case CandidateUnknownRejected, CandidateUnknownDismissed, CandidateUnknownSuperseded:
		if u.TruthStatus != TruthStatusHypothesis && u.TruthStatus != TruthStatusUnknown {
			return errors.New("unresolved unknown cannot be a confirmed fact")
		}
		for _, source := range u.Sources {
			if source.Type == KnowledgeSourceUserConfirmed && hasKnowledgeEvidence(source.Evidence) {
				return nil
			}
		}
		return errors.New("unknown resolution requires explicit user evidence")
	default:
		return errors.New("invalid unknown status")
	}
	return nil
}
func (e CandidateKnowledgeEvent) Validate() error {
	if strings.TrimSpace(e.ID) == "" || e.Timestamp.IsZero() || strings.TrimSpace(e.Action) == "" || strings.TrimSpace(e.EntityType) == "" || strings.TrimSpace(e.EntityID) == "" || strings.TrimSpace(e.Actor) == "" {
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

// CompareKnowledgeSourcePriority preserves the established trusted-source
// ordering. Sources outside that ordering are intentionally not promoted.
func CompareKnowledgeSourcePriority(a, b KnowledgeSource) int {
	return knowledgeSourcePriority(a) - knowledgeSourcePriority(b)
}

func knowledgeSourcePriority(source KnowledgeSource) int {
	switch source {
	case KnowledgeSourceUserConfirmed:
		return 4
	case KnowledgeSourceHHResume:
		return 3
	case KnowledgeSourceGithubVerified:
		return 2
	case KnowledgeSourceDerived:
		return 1
	default:
		return 0
	}
}

// ValidateKnowledgeValue is the storage-independent validation entry point.
func ValidateKnowledgeValue(value interface{ Validate() error }) error {
	if err := value.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode knowledge entity: %w", err)
	}
	if bytesContainSecret(raw) {
		return errors.New("knowledge contains a forbidden secret marker")
	}
	return nil
}

func ValidateKnowledgeCollection[T interface {
	Validate() error
	KnowledgeID() string
}](values []T) error {
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		if err := ValidateKnowledgeValue(value); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
		if seen[value.KnowledgeID()] {
			return errors.New("duplicate knowledge entity id")
		}
		seen[value.KnowledgeID()] = true
	}
	return nil
}

func ContainsForbiddenSecret(raw []byte) bool {
	return bytesContainSecret(raw)
}
func bytesContainSecret(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "authorization", "cookie", "access_token", "refresh_token"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
