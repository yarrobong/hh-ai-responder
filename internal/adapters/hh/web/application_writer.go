package hhweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhwebsession"
	hhwrite "hh-ai-responder/internal/ports/hhwrite"
)

const maxCookieWebResponseBody = 64 << 10

type CookieWebVacancyResponseWriter struct {
	baseURL   *url.URL
	session   *hhwebsession.Session
	userAgent string
}

func NewCookieWebVacancyResponseWriter(baseURL *url.URL, session *hhwebsession.Session, userAgent string) (*CookieWebVacancyResponseWriter, error) {
	return newCookieWebVacancyResponseWriter(baseURL, session, userAgent, false)
}

// NewCookieWebVacancyResponseWriterForTest is the explicit httptest seam for
// fixtures that cannot bind to an actual hh.ru hostname. Production code must
// use NewCookieWebVacancyResponseWriter.
func NewCookieWebVacancyResponseWriterForTest(baseURL *url.URL, session *hhwebsession.Session, userAgent string) (*CookieWebVacancyResponseWriter, error) {
	return newCookieWebVacancyResponseWriter(baseURL, session, userAgent, true)
}

func newCookieWebVacancyResponseWriter(baseURL *url.URL, session *hhwebsession.Session, userAgent string, allowNonHHHost bool) (*CookieWebVacancyResponseWriter, error) {
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" || session == nil {
		return nil, errors.New("cookie web writer requires base URL and session")
	}
	if !allowNonHHHost {
		if err := hhwebsession.ValidateHHWebBaseURL(baseURL); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0 (HH cookie web transport)"
	}
	return &CookieWebVacancyResponseWriter{baseURL: cloneURL(baseURL), session: session, userAgent: userAgent}, nil
}

var _ hhwrite.VacancyResponseWriter = (*CookieWebVacancyResponseWriter)(nil)

func (w *CookieWebVacancyResponseWriter) SubmitVacancyResponse(ctx context.Context, request hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	if request.Test != nil {
		return blockedPreSend(errors.New("cookie web writer does not support test submission")), &hhwrite.TransportError{Category: hhwrite.ErrorRequestValidation, Outcome: hhwrite.OutcomeNotSent, Err: errors.New("cookie web test submission is unsupported")}
	}
	if err := w.session.PersistenceError(); err != nil {
		return blockedPreSend(errors.New("cookie web session persistence is unhealthy")), &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeNotSent, Err: errors.New("cookie web session persistence is unhealthy")}
	}
	req, err := w.buildRequest(ctx, request)
	if err != nil {
		return blockedPreSend(err), &hhwrite.TransportError{Category: hhwrite.ErrorRequestValidation, Outcome: hhwrite.OutcomeNotSent, Err: err}
	}
	response, err := w.session.WriteClient().Do(req)
	if err != nil {
		metadata := map[string]string{"transport_attempted": "true", "reconciliation_required": "true"}
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, Timestamp: time.Now().UTC(), Metadata: metadata}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorNetworkAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Err: errors.New("cookie web application transport failed after request start")}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxCookieWebResponseBody+1))
	if persistenceErr := w.session.PersistenceError(); persistenceErr != nil {
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: response.StatusCode, Timestamp: time.Now().UTC(), Metadata: map[string]string{"transport_attempted": "true", "reconciliation_required": "true", "session_persistence": "unhealthy"}}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: response.StatusCode, Err: errors.New("cookie web session persistence failed after request start")}
	}
	if readErr != nil || len(body) > maxCookieWebResponseBody {
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: response.StatusCode, Timestamp: time.Now().UTC(), Metadata: map[string]string{"transport_attempted": "true", "reconciliation_required": "true"}}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: response.StatusCode, Err: errors.New("cookie web response was not safely readable")}
	}
	return classifyCookieWebResponse(response.StatusCode, response.Header.Get("Content-Type"), body)
}

func (w *CookieWebVacancyResponseWriter) buildRequest(ctx context.Context, request hhwrite.VacancyResponseRequest) (*http.Request, error) {
	if ctx == nil {
		return nil, errors.New("cookie web writer context is nil")
	}
	if request.VacancyID <= 0 || strings.TrimSpace(request.ResumeHash) == "" || request.IgnorePostponed != "true" {
		return nil, errors.New("cookie web vacancy response request is not approved")
	}
	referer, err := url.Parse(strings.TrimSpace(request.RefererURL))
	if err != nil || referer.Hostname() != w.baseURL.Hostname() || referer.Path != "/vacancy/"+strconv.Itoa(request.VacancyID) {
		return nil, errors.New("cookie web vacancy response referer is invalid")
	}
	xsrf, err := w.session.XSRFToken(w.baseURL)
	if err != nil {
		return nil, errors.New("cookie web session XSRF token is unavailable")
	}
	form := url.Values{
		"_xsrf": {xsrf}, "vacancy_id": {strconv.Itoa(request.VacancyID)}, "resume_hash": {request.ResumeHash},
		"letter": {request.Letter}, "ignore_postponed": {"true"},
	}
	target := w.baseURL.ResolveReference(&url.URL{Path: "/applicant/vacancy_response/popup"})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("cookie web application request could not be created")
	}
	httpRequest.Header.Set("User-Agent", w.userAgent)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	httpRequest.Header.Set("X-Xsrftoken", xsrf)
	httpRequest.Header.Set("Referer", referer.String())
	return httpRequest, nil
}

func classifyCookieWebResponse(status int, contentType string, body []byte) (hhwrite.WriteResult, error) {
	metadata := map[string]string{"transport_attempted": "true"}
	if status >= 300 && status < 400 {
		metadata["reconciliation_required"] = "true"
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: status, Err: errors.New("cookie web application returned an unexpected redirect")}
	}
	if status == http.StatusUnauthorized {
		return rejectedCookieWeb(status, hhwrite.ApplicationResultAuthRequired, hhwrite.ErrorAuthentication, metadata)
	}
	if status == http.StatusForbidden {
		return rejectedCookieWeb(status, hhwrite.ApplicationResultManualChallenge, hhwrite.ErrorPermission, metadata)
	}
	if status == http.StatusTooManyRequests {
		return rejectedCookieWeb(status, hhwrite.ApplicationResultRateLimited, hhwrite.ErrorRateLimited, metadata)
	}
	if status >= 500 {
		metadata["reconciliation_required"] = "true"
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorServer, Outcome: hhwrite.OutcomeAmbiguous, Status: status, Err: errors.New("HH server response leaves delivery uncertain")}
	}
	if cookieWebDuplicateContract(status, body) {
		return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, Class: hhwrite.ApplicationResultAlreadyApplied, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}, &hhwrite.TransportError{Category: hhwrite.ErrorProvider, Outcome: hhwrite.OutcomeAccepted, Status: status, Err: errors.New("HH reports that the vacancy was already applied")}
	}
	if status < 200 || status >= 300 {
		return rejectedCookieWeb(status, hhwrite.ApplicationResultBusinessRejected, hhwrite.ErrorProvider, metadata)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		metadata["reconciliation_required"] = "true"
		result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}
		return result, &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: status, Err: errors.New("HH response contract is unknown")}
	}
	if accepted, ok := payload["success"].(bool); !ok || !accepted {
		if value, ok := payload["ok"].(bool); !ok || !value {
			metadata["reconciliation_required"] = "true"
			result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, Class: hhwrite.ApplicationResultUnknownSendResult, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}
			return result, &hhwrite.TransportError{Category: hhwrite.ErrorResponseAmbiguous, Outcome: hhwrite.OutcomeAmbiguous, Status: status, Err: errors.New("HH response contract is unknown")}
		}
	}
	providerID := ""
	for _, key := range []string{"responseId", "response_id", "id"} {
		if value, ok := payload[key].(string); ok {
			providerID = value
			break
		}
	}
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, Class: hhwrite.ApplicationResultSuccess, ProviderStatus: status, ProviderID: providerID, Timestamp: time.Now().UTC(), Metadata: metadata}, nil
}

func cookieWebDuplicateContract(status int, body []byte) bool {
	if (status < 200 || status >= 300) && status != http.StatusConflict && status != http.StatusUnprocessableEntity {
		return false
	}
	var payload struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Errors []struct {
			Value string `json:"value"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	for _, value := range append([]string{payload.Error, payload.Code}, func() []string {
		values := make([]string, 0, len(payload.Errors))
		for _, item := range payload.Errors {
			values = append(values, item.Value)
		}
		return values
	}()...) {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "already applied", "already_applied", "already responded", "already_responded":
			return true
		}
	}
	return false
}

func blockedPreSend(err error) hhwrite.WriteResult {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent, Class: hhwrite.ApplicationResultBusinessRejected, Timestamp: time.Now().UTC(), Metadata: map[string]string{"blocked_phase": "pre_send", "transport_attempted": "false"}}
}

func rejectedCookieWeb(status int, class hhwrite.ApplicationResultClass, category hhwrite.ErrorCategory, metadata map[string]string) (hhwrite.WriteResult, error) {
	result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, Class: class, ProviderStatus: status, Timestamp: time.Now().UTC(), Metadata: metadata}
	return result, &hhwrite.TransportError{Category: category, Outcome: hhwrite.OutcomeRejected, Status: status, Err: fmt.Errorf("HH rejected application with status %d", status)}
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
