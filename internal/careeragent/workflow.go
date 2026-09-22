package careeragent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type AgentRunResultCode string

const (
	AgentRunResultCompleted   AgentRunResultCode = "completed"
	AgentRunResultPartial     AgentRunResultCode = "partial"
	AgentRunResultFailed      AgentRunResultCode = "failed"
	AgentRunResultInterrupted AgentRunResultCode = "interrupted"
)

type AgentRunItemStatus string

const (
	AgentRunItemStatusRunning        AgentRunItemStatus = "running"
	AgentRunItemStatusCompleted      AgentRunItemStatus = "completed"
	AgentRunItemStatusFailed         AgentRunItemStatus = "failed"
	AgentRunItemStatusReviewRequired AgentRunItemStatus = "review_required"
	AgentRunItemStatusMatched        AgentRunItemStatus = "matched"
	AgentRunItemStatusRejected       AgentRunItemStatus = "rejected"
	AgentRunItemStatusPrepared       AgentRunItemStatus = "prepared"
)

func (s AgentRunItemStatus) valid() bool {
	switch s {
	case AgentRunItemStatusRunning, AgentRunItemStatusCompleted, AgentRunItemStatusFailed,
		AgentRunItemStatusReviewRequired, AgentRunItemStatusMatched, AgentRunItemStatusRejected,
		AgentRunItemStatusPrepared:
		return true
	default:
		return false
	}
}

type AgentRunItem struct {
	ID           string             `json:"id"`
	RunID        string             `json:"run_id"`
	VacancyID    int                `json:"vacancy_id"`
	Stage        AgentRunStage      `json:"stage"`
	Status       AgentRunItemStatus `json:"status"`
	DecisionCode string             `json:"decision_code,omitempty"`
	Confidence   *float64           `json:"confidence,omitempty"`
	Evidence     json.RawMessage    `json:"evidence_json,omitempty"`
	ErrorCode    string             `json:"error_code,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
}

type RunQuery struct {
	Limit  int
	Status *AgentRunStatus
}

type PreparationQuery struct {
	Limit     int
	VacancyID *int
	Statuses  []PreparationStatus
}

func (i AgentRunItem) Validate() error {
	if strings.TrimSpace(i.ID) == "" || strings.TrimSpace(i.RunID) == "" || i.VacancyID <= 0 || !i.Stage.valid() || !i.Status.valid() || i.CreatedAt.IsZero() {
		return errors.New("invalid agent run item identity or status")
	}
	if i.Confidence != nil && (*i.Confidence < 0 || *i.Confidence > 1) {
		return errors.New("agent run item confidence must be between 0 and 1")
	}
	if err := validateWorkflowJSON(i.Evidence, true); err != nil {
		return fmt.Errorf("validate agent run item evidence: %w", err)
	}
	return nil
}

type PreparationStatus string

const (
	PreparationStatusPreparing      PreparationStatus = "preparing"
	PreparationStatusReviewRequired PreparationStatus = "review_required"
	PreparationStatusReady          PreparationStatus = "ready"
	PreparationStatusStale          PreparationStatus = "stale"
	PreparationStatusInvalid        PreparationStatus = "invalid"
)

func (s PreparationStatus) valid() bool {
	switch s {
	case PreparationStatusPreparing, PreparationStatusReviewRequired, PreparationStatusReady,
		PreparationStatusStale, PreparationStatusInvalid:
		return true
	default:
		return false
	}
}

type KnowledgeRequest struct {
	Topic    string `json:"topic"`
	Question string `json:"question"`
	Source   string `json:"source,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ApplicationPreparation struct {
	ID                    string             `json:"id"`
	VacancyID             int                `json:"vacancy_id"`
	ResumeID              string             `json:"resume_id,omitempty"`
	ResumeProviderID      string             `json:"resume_provider_id,omitempty"`
	CandidateID           string             `json:"candidate_id"`
	CandidateVersion      int                `json:"candidate_version"`
	CandidateSnapshotHash string             `json:"candidate_snapshot_hash"`
	RouteStatus           ResumeRouteStatus  `json:"route_status"`
	RouteConfidence       string             `json:"route_confidence,omitempty"`
	Evidence              json.RawMessage    `json:"evidence_json,omitempty"`
	StoryIDs              []string           `json:"story_ids,omitempty"`
	CoverLetter           string             `json:"cover_letter,omitempty"`
	CoverLetterHash       string             `json:"cover_letter_hash,omitempty"`
	TestAnswerDrafts      json.RawMessage    `json:"test_answer_drafts_json,omitempty"`
	KnowledgeRequests     []KnowledgeRequest `json:"knowledge_requests,omitempty"`
	InputFingerprint      string             `json:"input_fingerprint"`
	Status                PreparationStatus  `json:"status"`
	StaleReason           string             `json:"stale_reason,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

var ErrPreparationContentHash = errors.New("application preparation content hash does not match cover letter")

var (
	ErrAgentRunNotFound    = errors.New("agent run not found")
	ErrPreparationNotFound = errors.New("application preparation not found")
	ErrWorkflowConflict    = errors.New("career workflow record conflicts with existing record")
)

func (p ApplicationPreparation) Validate() error {
	if strings.TrimSpace(p.ID) == "" || p.VacancyID <= 0 || strings.TrimSpace(p.CandidateID) == "" || p.CandidateVersion <= 0 || strings.TrimSpace(p.CandidateSnapshotHash) == "" || !p.RouteStatus.valid() || !p.Status.valid() || p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return errors.New("invalid application preparation identity or status")
	}
	if len(p.CoverLetter) == 0 {
		if strings.TrimSpace(p.CoverLetterHash) != "" {
			return ErrPreparationContentHash
		}
	} else if contentHash(p.CoverLetter) != strings.TrimSpace(p.CoverLetterHash) {
		return ErrPreparationContentHash
	}
	if fingerprint := strings.TrimSpace(p.InputFingerprint); len(fingerprint) != sha256.Size*2 {
		return errors.New("application preparation input fingerprint is invalid")
	} else if _, err := hex.DecodeString(fingerprint); err != nil {
		return errors.New("application preparation input fingerprint is invalid")
	}
	if err := validateWorkflowJSON(p.Evidence, true); err != nil {
		return fmt.Errorf("validate application preparation evidence: %w", err)
	}
	if err := validateWorkflowJSON(p.TestAnswerDrafts, false); err != nil {
		return fmt.Errorf("validate application preparation test drafts: %w", err)
	}
	for _, request := range p.KnowledgeRequests {
		if strings.TrimSpace(request.Topic) == "" || strings.TrimSpace(request.Question) == "" {
			return errors.New("application preparation knowledge request is incomplete")
		}
	}
	return nil
}

func (p ApplicationPreparation) IsStale() bool {
	return p.Status == PreparationStatusStale
}

func (p *ApplicationPreparation) MarkStale(reason string, updatedAt time.Time) error {
	if p == nil {
		return errors.New("application preparation is nil")
	}
	if updatedAt.IsZero() {
		return errors.New("application preparation stale timestamp is required")
	}
	p.Status = PreparationStatusStale
	p.StaleReason = strings.TrimSpace(reason)
	p.UpdatedAt = updatedAt.UTC()
	return p.Validate()
}

func PreparationInputFingerprint(p ApplicationPreparation) string {
	value := struct {
		VacancyID             int
		ResumeID              string
		ResumeProviderID      string
		CandidateID           string
		CandidateVersion      int
		CandidateSnapshotHash string
		RouteStatus           ResumeRouteStatus
		RouteConfidence       string
		Evidence              json.RawMessage
		StoryIDs              []string
		CoverLetterHash       string
		TestAnswerDrafts      json.RawMessage
		KnowledgeRequests     []KnowledgeRequest
	}{
		VacancyID: p.VacancyID, ResumeID: p.ResumeID, ResumeProviderID: p.ResumeProviderID,
		CandidateID: p.CandidateID, CandidateVersion: p.CandidateVersion, CandidateSnapshotHash: p.CandidateSnapshotHash,
		RouteStatus: p.RouteStatus, RouteConfidence: p.RouteConfidence, Evidence: p.Evidence,
		StoryIDs: append([]string(nil), p.StoryIDs...), CoverLetterHash: p.CoverLetterHash,
		TestAnswerDrafts: p.TestAnswerDrafts, KnowledgeRequests: append([]KnowledgeRequest(nil), p.KnowledgeRequests...),
	}
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (p ApplicationPreparation) ContentHash() string {
	return contentHash(p.CoverLetter)
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validateWorkflowJSON(raw json.RawMessage, objectOnly bool) error {
	if len(raw) == 0 || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if containsWorkflowSecretMarker(trimmed) {
		return errors.New("sensitive workflow evidence is not allowed")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("workflow evidence is malformed JSON")
	}
	if objectOnly {
		if _, ok := value.(map[string]any); !ok {
			return errors.New("workflow evidence must be a JSON object")
		}
	}
	return nil
}

func containsWorkflowSecretMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "bearer ", "cookie", "set-cookie", "token=", "api_key", "apikey", "password=", "secret"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
