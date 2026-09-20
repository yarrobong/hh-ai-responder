// Package hhwrite defines narrow capabilities for state-changing HH actions.
// It contains no provider-specific HTTP request or response types.
package hhwrite

import (
	"context"
	"errors"
	"time"
)

// Outcome describes what the transport can prove about a mutation attempt.
type Outcome string

const (
	OutcomeAccepted  Outcome = "accepted"
	OutcomeRejected  Outcome = "rejected"
	OutcomeNotSent   Outcome = "not_sent"
	OutcomeAmbiguous Outcome = "ambiguous"
)

// ApplicationResultClass is the stable classification of the one supported
// vacancy-application mutation. A successful, duplicate, or ambiguous result
// must be reconciled by the caller before it is treated as final.
type ApplicationResultClass string

const (
	ApplicationResultSuccess           ApplicationResultClass = "SUCCESS"
	ApplicationResultAlreadyApplied    ApplicationResultClass = "ALREADY_APPLIED"
	ApplicationResultBusinessRejected  ApplicationResultClass = "BUSINESS_REJECTED"
	ApplicationResultAuthRequired      ApplicationResultClass = "AUTH_REQUIRED"
	ApplicationResultRateLimited       ApplicationResultClass = "RATE_LIMITED"
	ApplicationResultUnknownSendResult ApplicationResultClass = "UNKNOWN_SEND_RESULT"
)

// ErrorCategory is intentionally small: callers need to distinguish local
// failures, concrete HH responses, and uncertain delivery.
type ErrorCategory string

const (
	ErrorRequestValidation ErrorCategory = "request_validation_failed"
	ErrorAuthentication    ErrorCategory = "authentication_failed"
	ErrorPermission        ErrorCategory = "permission_denied"
	ErrorRateLimited       ErrorCategory = "rate_limited"
	ErrorProvider          ErrorCategory = "provider_rejected"
	ErrorServer            ErrorCategory = "server_error"
	ErrorNetworkAmbiguous  ErrorCategory = "network_ambiguous"
	ErrorResponseAmbiguous ErrorCategory = "response_ambiguous"
)

// TransportError is safe to inspect and wrap. ResponseBody is bounded and
// sanitized by the adapter; it is diagnostic evidence, not a raw provider API.
type TransportError struct {
	Category            ErrorCategory
	Outcome             Outcome
	Status              int
	ResponseContentType string
	ResponseBody        string
	ErrorFields         map[string]string
	CorrelationIDs      map[string]string
	Err                 error
}

func (e *TransportError) Error() string {
	if e == nil {
		return "HH write failed"
	}
	message := "HH write failed"
	if e.Err != nil {
		message = e.Err.Error()
	}
	if e.Category != "" {
		message = string(e.Category) + ": " + message
	}
	return message
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsAmbiguous reports whether HH may have applied the mutation.
func IsAmbiguous(err error) bool {
	var transportErr *TransportError
	return errors.As(err, &transportErr) && transportErr != nil && transportErr.Outcome == OutcomeAmbiguous
}

// WriteResult contains only provider-neutral evidence useful to a higher
// gateway. It never exposes *http.Response or raw response JSON.
type WriteResult struct {
	Outcome        Outcome
	Class          ApplicationResultClass
	ProviderStatus int
	ProviderID     string
	Timestamp      time.Time
	Metadata       map[string]string
}

type ChatMessageRequest struct {
	ConversationID string
	Text           string
	IdempotencyKey string
}

type ChatLeaveRequest struct {
	ConversationID string
}

// VacancyResponseRequest represents the one HH application mutation. Test
// answers are part of the same request when present; there is no separate
// test-submission capability because HH does not use a separate write here.
type VacancyResponseRequest struct {
	VacancyID        int
	ProviderResumeID string
	ResumeHash       string
	Letter           string
	RefererURL       string
	IgnorePostponed  string
	Test             *VacancyTestSubmission
}

type VacancyTestSubmission struct {
	UIDPK                   string
	GUID                    string
	StartTime               string
	Required                string
	Incomplete              string
	Lux                     string
	WithoutTest             string
	CountryIDs              string
	VisibleInVacancyCountry string
	Answers                 []VacancyTestAnswer
}

type VacancyTestAnswer struct {
	TaskID   int
	ChoiceID string
	Text     string
}

type ResumeTouchRequest struct {
	ResumeHash string
}

type JobSearchStatusRequest struct {
	Status string
}

type VacancyResponseWriter interface {
	SubmitVacancyResponse(context.Context, VacancyResponseRequest) (WriteResult, error)
}

type ChatMessageWriter interface {
	SendChatMessage(context.Context, ChatMessageRequest) (WriteResult, error)
}

type ChatLeaveWriter interface {
	LeaveChat(context.Context, ChatLeaveRequest) (WriteResult, error)
}

type ResumeWriter interface {
	TouchResume(context.Context, ResumeTouchRequest) (WriteResult, error)
}

type JobSearchStatusWriter interface {
	SetJobSearchStatus(context.Context, JobSearchStatusRequest) (WriteResult, error)
}
