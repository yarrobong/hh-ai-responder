package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// APIErrorCode is the stable, provider-neutral classification of an API
// request failure.
type APIErrorCode string

const (
	APIErrorAuthRequired        APIErrorCode = "AUTH_REQUIRED"
	APIErrorTokenExpired        APIErrorCode = "TOKEN_EXPIRED"
	APIErrorTokenRevoked        APIErrorCode = "TOKEN_REVOKED"
	APIErrorApplicationNotFound APIErrorCode = "APPLICATION_NOT_FOUND"
	APIErrorRateLimited         APIErrorCode = "RATE_LIMITED"
	APIErrorForbidden           APIErrorCode = "FORBIDDEN"
	APIErrorRemote              APIErrorCode = "REMOTE_ERROR"

	// Short aliases make the taxonomy convenient for callers without
	// duplicating string literals at transport boundaries.
	AUTH_REQUIRED         = APIErrorAuthRequired
	TOKEN_EXPIRED         = APIErrorTokenExpired
	TOKEN_REVOKED         = APIErrorTokenRevoked
	APPLICATION_NOT_FOUND = APIErrorApplicationNotFound
	RATE_LIMITED          = APIErrorRateLimited
	FORBIDDEN             = APIErrorForbidden
	REMOTE_ERROR          = APIErrorRemote
)

var (
	errAPIRequestContext = errors.New("request context was invalid")
	errAPINetworkRequest = errors.New("network request failed")
	errAPIResponseRead   = errors.New("response could not be read")
	errAPIProvider       = errors.New("provider rejected request")
	errAPITokenStore     = errors.New("token store unavailable")
	errAPITokenRefresh   = errors.New("token refresh was rejected")
	errAPITokenExpired   = errors.New("access token is expired")
)

// APIError contains only safe request diagnostics. It deliberately has no
// response body, header map, URL, authorization value, or provider form data.
type APIError struct {
	Code       APIErrorCode
	Status     int
	Path       string
	RequestID  string
	RetryAfter time.Duration

	cause error
}

func (e *APIError) Error() string {
	if e == nil {
		return "HH API error"
	}
	parts := []string{fmt.Sprintf("classification=%s", e.Code)}
	if e.Status > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.Status))
	}
	if e.Path != "" {
		parts = append(parts, "path="+safeRequestPath(e.Path))
	}
	if e.RequestID != "" {
		parts = append(parts, "request_id="+safeRequestID(e.RequestID))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, "retry_after="+e.RetryAfter.String())
	}
	if cause := safeCauseText(e.cause); cause != "" {
		parts = append(parts, "cause="+cause)
	}
	return "HH API error: " + strings.Join(parts, " ")
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newAPIError(code APIErrorCode, status int, path, requestID string, retryAfter time.Duration, cause error) *APIError {
	return &APIError{
		Code: APIErrorCode(code), Status: status, Path: safeRequestPath(path),
		RequestID: safeRequestID(requestID), RetryAfter: retryAfter, cause: safeCause(cause),
	}
}

func safeCause(cause error) error {
	if cause == nil {
		return nil
	}
	switch {
	case errors.Is(cause, context.Canceled):
		return context.Canceled
	case errors.Is(cause, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(cause, errAPIRequestContext):
		return errAPIRequestContext
	case errors.Is(cause, errAPINetworkRequest):
		return errAPINetworkRequest
	case errors.Is(cause, errAPIResponseRead):
		return errAPIResponseRead
	case errors.Is(cause, errAPIProvider):
		return errAPIProvider
	case errors.Is(cause, errAPITokenStore):
		return errAPITokenStore
	case errors.Is(cause, errAPITokenRefresh):
		return errAPITokenRefresh
	case errors.Is(cause, errAPITokenExpired):
		return errAPITokenExpired
	default:
		return errAPINetworkRequest
	}
}

func safeCauseText(cause error) string {
	switch {
	case cause == nil:
		return ""
	case errors.Is(cause, context.Canceled):
		return "context canceled"
	case errors.Is(cause, context.DeadlineExceeded):
		return "context deadline exceeded"
	case errors.Is(cause, errAPIRequestContext):
		return "invalid context"
	case errors.Is(cause, errAPINetworkRequest):
		return "network request failed"
	case errors.Is(cause, errAPIResponseRead):
		return "response could not be read"
	case errors.Is(cause, errAPIProvider):
		return "provider rejected request"
	case errors.Is(cause, errAPITokenStore):
		return "token store unavailable"
	case errors.Is(cause, errAPITokenRefresh):
		return "token refresh was rejected"
	case errors.Is(cause, errAPITokenExpired):
		return "access token is expired"
	default:
		return "request failed"
	}
}

func safeRequestPath(path string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\r\n") {
		return "/"
	}
	return path
}

func safeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		value = value[:256]
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

func contextAPIError(path string, err error) *APIError {
	if err == nil {
		err = errAPIRequestContext
	}
	return newAPIError(APIErrorRemote, 0, path, "", 0, err)
}
