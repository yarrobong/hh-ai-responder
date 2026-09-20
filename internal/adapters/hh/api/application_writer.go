package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	hhwrite "hh-ai-responder/internal/ports/hhwrite"
)

const applicationEndpoint = "/negotiations"

// APIApplicationWriter is the sole API mutation adapter for vacancy
// applications. It intentionally exposes no generic POST capability.
type APIApplicationWriter struct {
	client *APIHHClient
}

var _ hhwrite.VacancyResponseWriter = (*APIApplicationWriter)(nil)

func NewAPIApplicationWriter(client *APIHHClient) *APIApplicationWriter {
	return &APIApplicationWriter{client: client}
}

func (w *APIApplicationWriter) SubmitVacancyResponse(ctx context.Context, request hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	if ctx == nil {
		return applicationNotSent(hhwrite.ErrorRequestValidation, errors.New("HH API application context is nil"))
	}
	if err := ctx.Err(); err != nil {
		return applicationNotSent(hhwrite.ErrorRequestValidation, err)
	}
	if w == nil || w.client == nil || w.client.httpClient == nil || !validAPIBaseURL(w.client.baseURL) {
		return applicationNotSent(hhwrite.ErrorRequestValidation, errors.New("HH API application writer is not configured"))
	}
	if request.VacancyID <= 0 || strings.TrimSpace(request.ProviderResumeID) == "" || strings.ContainsAny(request.ProviderResumeID, "\r\n") {
		return applicationNotSent(hhwrite.ErrorRequestValidation, errors.New("HH API application request is invalid"))
	}
	return w.client.postNegotiation(ctx, request)
}

// postNegotiation is deliberately private and only serves the typed
// application writer. It performs at most one application POST.
func (c *APIHHClient) postNegotiation(ctx context.Context, request hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	tokens, err := c.tokenStore.Load(ctx)
	if err != nil {
		return applicationNotSent(hhwrite.ErrorAuthentication, errors.New("HH API token store is unavailable"))
	}
	if tokens.IsExpiredAt(c.now(), c.tokenExpirySkew) {
		if tokens.RefreshToken == "" {
			return applicationAuthRequired(0, errors.New("HH API access token is expired"))
		}
		tokens, err = c.refreshAndSave(ctx, applicationEndpoint, tokens)
		if err != nil {
			return applicationResultForAPIError(err)
		}
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return applicationAuthRequired(0, errors.New("HH API access token is unavailable"))
	}

	form := url.Values{
		"vacancy_id": {strconv.Itoa(request.VacancyID)},
		"resume_id":  {strings.TrimSpace(request.ProviderResumeID)},
	}
	if strings.TrimSpace(request.Letter) != "" {
		form.Set("message", request.Letter)
	}
	requestURL, err := c.resolve(applicationEndpoint, nil)
	if err != nil {
		return applicationNotSent(hhwrite.ErrorRequestValidation, errors.New("HH API application URL is invalid"))
	}
	requestContext := ctx
	cancel := func() {}
	if c.timeout > 0 {
		requestContext, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, requestURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return applicationNotSent(hhwrite.ErrorRequestValidation, errors.New("HH API application request could not be created"))
	}
	httpRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	httpRequest.Header.Set("User-Agent", c.userAgent)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return applicationUnknownSendResult(0, nil, err)
	}
	defer response.Body.Close()
	body, bodyTooLarge, readErr := readBounded(response.Body, maxAPIErrorBody)
	metadata := applicationMetadata(response.Header)
	if response.StatusCode >= 500 {
		return applicationUnknownSendResult(response.StatusCode, metadata, errors.New("HH API application server response is uncertain"))
	}
	if response.StatusCode == http.StatusUnauthorized {
		return applicationAuthRequired(response.StatusCode, errors.New("HH API application authentication is required"))
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return applicationRateLimited(response.StatusCode, errors.New("HH API application rate limit was reached"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if readErr != nil || bodyTooLarge {
			return applicationBusinessRejected(response.StatusCode, errors.New("HH API application response could not be classified"))
		}
		return classifyStructuredApplicationError(response.StatusCode, body)
	}
	if readErr != nil {
		return applicationUnknownSendResult(response.StatusCode, metadata, errors.New("HH API application response could not be read"))
	}
	return hhwrite.WriteResult{
		Outcome:        hhwrite.OutcomeAccepted,
		Class:          hhwrite.ApplicationResultSuccess,
		ProviderStatus: response.StatusCode,
		ProviderID:     applicationProviderID(body),
		Timestamp:      time.Now().UTC(),
		Metadata:       metadata,
	}, nil
}

func applicationResultForAPIError(err error) (hhwrite.WriteResult, error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case APIErrorAuthRequired, APIErrorTokenExpired, APIErrorTokenRevoked:
			return applicationAuthRequired(apiErr.Status, errors.New("HH API application authentication is required"))
		case APIErrorRateLimited:
			return applicationRateLimited(apiErr.Status, errors.New("HH API application rate limit was reached"))
		}
	}
	return applicationNotSent(hhwrite.ErrorAuthentication, errors.New("HH API application authentication setup failed"))
}

func applicationProviderID(body []byte) string {
	var value map[string]json.RawMessage
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	for _, key := range []string{"id", "negotiation_id", "negotiationId"} {
		var result string
		if json.Unmarshal(value[key], &result) == nil {
			return boundedField(result)
		}
	}
	return ""
}

type structuredApplicationError struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type structuredApplicationErrorResponse struct {
	Errors []structuredApplicationError `json:"errors"`
}

var structuredApplicationBusinessValues = map[string]struct{}{
	"invalid_vacancy":            {},
	"resume_not_found":           {},
	"test_required":              {},
	"resume_visibility_conflict": {},
	"application_denied":         {},
	"wrong_state":                {},
	"empty_message":              {},
	"too_long_message":           {},
}

var structuredApplicationOAuthValues = map[string]struct{}{
	"bad_authorization":     {},
	"token_expired":         {},
	"token_revoked":         {},
	"application_not_found": {},
	"user_auth_expected":    {},
}

func classifyStructuredApplicationError(status int, body []byte) (hhwrite.WriteResult, error) {
	var response structuredApplicationErrorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return applicationBusinessRejected(status, errors.New("HH API application was rejected"))
	}
	for _, providerError := range response.Errors {
		providerType := boundedField(providerError.Type)
		providerValue := boundedField(providerError.Value)
		fields := map[string]string{}
		if providerType != "" {
			fields["type"] = providerType
		}
		if providerValue != "" {
			fields["value"] = providerValue
		}
		switch {
		case providerType == "negotiations" && providerValue == "already_applied":
			return applicationAlreadyAppliedWithFields(status, fields)
		case providerValue == "captcha_required":
			return applicationManualChallengeWithFields(status, fields)
		case providerType == "oauth":
			if _, ok := structuredApplicationOAuthValues[providerValue]; ok {
				return applicationAuthRequiredWithFields(status, fields)
			}
		case providerType == "negotiations":
			if _, ok := structuredApplicationBusinessValues[providerValue]; ok {
				return applicationBusinessRejectedWithFields(status, fields)
			}
		}
	}
	return applicationBusinessRejected(status, errors.New("HH API application was rejected"))
}

func applicationMetadata(header http.Header) map[string]string {
	requestID := responseRequestID(header)
	if requestID == "" {
		return nil
	}
	return map[string]string{"request_id": requestID}
}

func applicationNotSent(category hhwrite.ErrorCategory, err error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, &hhwrite.TransportError{Category: category, Outcome: hhwrite.OutcomeNotSent, Err: err}
}

func applicationAuthRequired(status int, err error) (hhwrite.WriteResult, error) {
	return applicationAuthRequiredWithFields(status, nil, err)
}

func applicationAuthRequiredWithFields(status int, fields map[string]string, causes ...error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent, Class: hhwrite.ApplicationResultAuthRequired, ProviderStatus: status}, &hhwrite.TransportError{Category: hhwrite.ErrorAuthentication, Outcome: hhwrite.OutcomeNotSent, Status: status, ErrorFields: fields, Err: firstApplicationError(causes, errors.New("HH API application authentication is required"))}
}

func applicationRateLimited(status int, err error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultRateLimited, ProviderStatus: status}, &hhwrite.TransportError{Category: hhwrite.ErrorRateLimited, Outcome: hhwrite.OutcomeRejected, Status: status, Err: err}
}

func applicationBusinessRejected(status int, err error) (hhwrite.WriteResult, error) {
	return applicationBusinessRejectedWithFields(status, nil, err)
}

func applicationBusinessRejectedWithFields(status int, fields map[string]string, causes ...error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultBusinessRejected, ProviderStatus: status}, &hhwrite.TransportError{Category: hhwrite.ErrorProvider, Outcome: hhwrite.OutcomeRejected, Status: status, ErrorFields: fields, Err: firstApplicationError(causes, errors.New("HH API application was rejected"))}
}

func applicationAlreadyApplied(status int, err error) (hhwrite.WriteResult, error) {
	return applicationAlreadyAppliedWithFields(status, nil, err)
}

func applicationAlreadyAppliedWithFields(status int, fields map[string]string, causes ...error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultAlreadyApplied, ProviderStatus: status}, &hhwrite.TransportError{Category: hhwrite.ErrorProvider, Outcome: hhwrite.OutcomeRejected, Status: status, ErrorFields: fields, Err: firstApplicationError(causes, errors.New("HH API application already exists"))}
}

func applicationManualChallengeWithFields(status int, fields map[string]string) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: hhwrite.ApplicationResultManualChallenge, ProviderStatus: status}, &hhwrite.TransportError{Category: hhwrite.ErrorPermission, Outcome: hhwrite.OutcomeRejected, Status: status, ErrorFields: fields, Err: errors.New("HH API application requires manual challenge")}
}

func firstApplicationError(causes []error, fallback error) error {
	if len(causes) > 0 && causes[0] != nil {
		return causes[0]
	}
	return fallback
}

func applicationUnknownSendResult(status int, metadata map[string]string, err error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: status, Metadata: metadata}, &hhwrite.TransportError{Category: hhwrite.ErrorNetworkAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: status, CorrelationIDs: metadata, Err: err}
}
