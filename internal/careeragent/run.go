package careeragent

import (
	"errors"
	"strings"
	"time"
)

type AgentRunStage string

const (
	AgentRunStageCareerAgent   AgentRunStage = "career_agent"
	AgentRunStageDiscovery     AgentRunStage = "discovery"
	AgentRunStageAnalysis      AgentRunStage = "analysis"
	AgentRunStageRouting       AgentRunStage = "routing"
	AgentRunStageReview        AgentRunStage = "review"
	AgentRunStagePreparation   AgentRunStage = "preparation"
	AgentRunStageCommunication AgentRunStage = "communication"
)

func (s AgentRunStage) valid() bool {
	switch s {
	case AgentRunStageCareerAgent, AgentRunStageDiscovery, AgentRunStageAnalysis,
		AgentRunStageRouting, AgentRunStageReview, AgentRunStagePreparation, AgentRunStageCommunication:
		return true
	default:
		return false
	}
}

type AgentRunStatus string

const (
	AgentRunStatusRunning   AgentRunStatus = "running"
	AgentRunStatusCompleted AgentRunStatus = "completed"
	AgentRunStatusPartial   AgentRunStatus = "partial"
	AgentRunStatusFailed    AgentRunStatus = "failed"
)

func (s AgentRunStatus) valid() bool {
	switch s {
	case AgentRunStatusRunning, AgentRunStatusCompleted, AgentRunStatusPartial, AgentRunStatusFailed:
		return true
	default:
		return false
	}
}

func (s AgentRunStatus) resultCode() AgentRunResultCode {
	switch s {
	case AgentRunStatusCompleted:
		return AgentRunResultCompleted
	case AgentRunStatusPartial:
		return AgentRunResultPartial
	case AgentRunStatusFailed:
		return AgentRunResultFailed
	default:
		return ""
	}
}

// AgentRun is redacted observability metadata. It does not authorize an
// application, message, test, resume touch or any other HH mutation.
type AgentRun struct {
	ID           string             `json:"id"`
	RunType      string             `json:"run_type,omitempty"`
	Stage        AgentRunStage      `json:"stage"`
	Status       AgentRunStatus     `json:"status"`
	StartedAt    time.Time          `json:"started_at"`
	FinishedAt   *time.Time         `json:"finished_at,omitempty"`
	Result       string             `json:"result,omitempty"`
	ResultCode   AgentRunResultCode `json:"result_code,omitempty"`
	Summary      string             `json:"summary,omitempty"`
	ErrorCode    string             `json:"error_code,omitempty"`
	ErrorSummary string             `json:"error_summary,omitempty"`
	CreatedAt    time.Time          `json:"created_at,omitempty"`
	Errors       []string           `json:"errors,omitempty"`
	Confidence   *float64           `json:"confidence,omitempty"`
}

func NewAgentRun(id string, stage AgentRunStage, startedAt time.Time) AgentRun {
	startedAt = startedAt.UTC()
	return AgentRun{ID: strings.TrimSpace(id), Stage: stage, Status: AgentRunStatusRunning, StartedAt: startedAt, CreatedAt: startedAt, Errors: []string{}}
}

func (r AgentRun) Validate() error {
	if strings.TrimSpace(r.ID) == "" || !r.Stage.valid() || !r.Status.valid() || r.StartedAt.IsZero() {
		return errors.New("invalid agent run identity or status")
	}
	if !r.CreatedAt.IsZero() && r.CreatedAt.After(r.StartedAt) {
		return errors.New("agent run created_at follows started_at")
	}
	if r.FinishedAt == nil {
		if r.Status != AgentRunStatusRunning {
			return errors.New("completed agent run requires finished_at")
		}
	} else if r.FinishedAt.Before(r.StartedAt) {
		return errors.New("agent run finished_at precedes started_at")
	}
	if r.Status == AgentRunStatusFailed && len(r.Errors) == 0 {
		return errors.New("failed agent run requires an error summary")
	}
	if r.ResultCode != "" && r.ResultCode != r.Status.resultCode() && r.ResultCode != AgentRunResultInterrupted {
		return errors.New("agent run result code does not match status")
	}
	if r.Confidence != nil && (*r.Confidence < 0 || *r.Confidence > 1) {
		return errors.New("agent run confidence must be between 0 and 1")
	}
	return nil
}

func (r *AgentRun) Finish(status AgentRunStatus, result string, finishedAt time.Time, runErr error) error {
	if r == nil {
		return errors.New("agent run is nil")
	}
	if !status.valid() || status == AgentRunStatusRunning {
		return errors.New("agent run finish requires a terminal status")
	}
	if r.FinishedAt != nil {
		return errors.New("agent run is already finished")
	}
	if finishedAt.IsZero() {
		return errors.New("agent run finished_at is required")
	}
	r.Status = status
	r.Result = strings.TrimSpace(result)
	r.ResultCode = status.resultCode()
	r.Summary = r.Result
	finishedAt = finishedAt.UTC()
	r.FinishedAt = &finishedAt
	if runErr != nil {
		redacted := RedactAgentError(runErr)
		r.Errors = appendUniqueStrings(r.Errors, redacted)
		r.ErrorSummary = redacted
		r.ErrorCode = "RUN_ERROR"
	}
	return r.Validate()
}

func (r *AgentRun) RecoverInterrupted(finishedAt time.Time) error {
	if r == nil {
		return errors.New("agent run is nil")
	}
	if r.Status != AgentRunStatusRunning || r.FinishedAt != nil {
		return errors.New("only a running agent run can be recovered")
	}
	if finishedAt.IsZero() || finishedAt.Before(r.StartedAt) {
		return errors.New("agent run recovery timestamp is invalid")
	}
	r.Status = AgentRunStatusFailed
	r.Result = "interrupted run"
	r.ResultCode = AgentRunResultInterrupted
	r.Summary = r.Result
	r.ErrorCode = "INTERRUPTED"
	r.ErrorSummary = "run interrupted before completion"
	r.Errors = appendUniqueStrings(r.Errors, r.ErrorSummary)
	finishedAt = finishedAt.UTC()
	r.FinishedAt = &finishedAt
	return r.Validate()
}

// RedactAgentError retains a safe error class while avoiding credentials,
// cookies, authorization headers and potentially private provider bodies.
func RedactAgentError(runErr error) string {
	if runErr == nil {
		return ""
	}
	value := strings.TrimSpace(runErr.Error())
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "bearer ", "cookie", "set-cookie", "token=", "api_key", "apikey", "password=", "secret", ".env"} {
		if strings.Contains(lower, marker) {
			return "error redacted: sensitive details omitted"
		}
	}
	if len(value) > 500 {
		return value[:500] + "…"
	}
	return value
}
