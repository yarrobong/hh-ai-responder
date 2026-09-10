// Package hhwrite is the concrete, one-attempt HH mutation transport.
// Authorization, approval, preflight, dry-run, and reconciliation belong to
// callers; this package only builds and dispatches provider requests.
package hhwrite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"hh-ai-responder/internal/ports/hhwrite"
)

const (
	defaultBaseURL       = "https://hh.ru"
	defaultChatURL       = "https://chatik.hh.ru"
	acceptHeader         = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	acceptLanguageHeader = "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7"
	secCHUAHeader        = `"Chromium";v="151", "Google Chrome";v="151", "Not-A.Brand";v="99"`
	userAgent            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
	maxResponseBody      = 64 << 10
)

var (
	sensitiveField = regexp.MustCompile(`(?i)(authorization|cookie|x-xsrftoken|xsrf|access[_-]?token|refresh[_-]?token|session[_-]?id|password|client[_-]?secret)`)
	htmlTag        = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<[^>]+>`)
	errorField     = regexp.MustCompile(`(?i)(error|message|code|reason|field|type|status)`)
)

type Options struct {
	BaseURL          *url.URL
	ChatURL          *url.URL
	ResumeProfileURL *url.URL
	HTTPClient       *http.Client
	XSRFToken        string
	UserAgent        string
	AcceptLanguage   string
	Accept           string
}

type Client struct {
	baseURL          *url.URL
	chatURL          *url.URL
	resumeProfileURL *url.URL
	httpClient       *http.Client
	xsrfToken        string
	userAgent        string
	acceptLanguage   string
	accept           string
}

func NewClient(options Options) (*Client, error) {
	baseURL := cloneURL(options.BaseURL)
	if baseURL == nil {
		baseURL, _ = url.Parse(defaultBaseURL)
	}
	if !validBaseURL(baseURL) {
		return nil, errors.New("HH write base URL is invalid")
	}
	chatURL := cloneURL(options.ChatURL)
	if chatURL == nil {
		chatURL, _ = url.Parse(defaultChatURL)
	}
	if !validBaseURL(chatURL) {
		return nil, errors.New("HH write chat URL is invalid")
	}
	if options.ResumeProfileURL != nil && !validBaseURL(options.ResumeProfileURL) {
		return nil, errors.New("HH write resume profile URL is invalid")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	ua := options.UserAgent
	if ua == "" {
		ua = userAgent
	}
	language := options.AcceptLanguage
	if language == "" {
		language = acceptLanguageHeader
	}
	accept := options.Accept
	if accept == "" {
		accept = acceptHeader
	}
	return &Client{
		baseURL: baseURL, chatURL: chatURL, resumeProfileURL: cloneURL(options.ResumeProfileURL),
		httpClient: client, xsrfToken: strings.TrimSpace(options.XSRFToken),
		userAgent: ua, acceptLanguage: language, accept: accept,
	}, nil
}

var _ hhwrite.VacancyResponseWriter = (*Client)(nil)
var _ hhwrite.ChatMessageWriter = (*Client)(nil)
var _ hhwrite.ChatLeaveWriter = (*Client)(nil)
var _ hhwrite.ResumeWriter = (*Client)(nil)
var _ hhwrite.JobSearchStatusWriter = (*Client)(nil)

func (c *Client) SubmitVacancyResponse(ctx context.Context, request hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	if err := validateVacancyResponse(request); err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if err := c.requireToken(); err != nil {
		return notSent(hhwrite.ErrorAuthentication, err)
	}
	form := url.Values{
		"_xsrf": {c.xsrfToken}, "vacancy_id": {strconv.Itoa(request.VacancyID)},
		"resume_hash": {request.ResumeHash}, "letter": {request.Letter},
	}
	if request.IgnorePostponed != "" {
		form.Set("ignore_postponed", request.IgnorePostponed)
	}
	if request.Test != nil {
		form.Set("uidPk", request.Test.UIDPK)
		form.Set("guid", request.Test.GUID)
		form.Set("startTime", request.Test.StartTime)
		form.Set("testRequired", request.Test.Required)
		form.Set("incomplete", request.Test.Incomplete)
		form.Set("lux", request.Test.Lux)
		form.Set("withoutTest", request.Test.WithoutTest)
		form.Set("mark_applicant_visible_in_vacancy_country", request.Test.VisibleInVacancyCountry)
		form.Set("country_ids", request.Test.CountryIDs)
		for _, answer := range request.Test.Answers {
			field := "task_" + strconv.Itoa(answer.TaskID)
			if answer.ChoiceID != "" {
				form.Set(field, answer.ChoiceID)
			} else {
				form.Set(field+"_text", answer.Text)
			}
		}
	}
	result, err := c.do(ctx, http.MethodPost, c.baseURL, "/applicant/vacancy_response/popup", strings.NewReader(form.Encode()), request.RefererURL, "application/x-www-form-urlencoded", true)
	return result, err
}

func (c *Client) SendChatMessage(ctx context.Context, request hhwrite.ChatMessageRequest) (hhwrite.WriteResult, error) {
	chatID, err := parsePositiveID(request.ConversationID, "HH conversation external id")
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if strings.TrimSpace(request.Text) == "" || !utf8.ValidString(request.Text) || strings.TrimSpace(request.IdempotencyKey) == "" {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("HH chat message request is invalid"))
	}
	if err := c.requireToken(); err != nil {
		return notSent(hhwrite.ErrorAuthentication, err)
	}
	body, err := json.Marshal(chatMessageWire{ChatID: chatID, Text: request.Text, IdempotencyKey: request.IdempotencyKey})
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, fmt.Errorf("marshal HH chat message: %w", err))
	}
	result, err := c.do(ctx, http.MethodPost, c.chatURL, "/chatik/api/send", bytes.NewReader(body), c.chatURL.String()+"/?platform=xhh&dest=iframe", "application/json", true)
	if err != nil {
		return result, err
	}
	if result.ProviderID == "" {
		return ambiguous(result, hhwrite.ErrorResponseAmbiguous, errors.New("HH chat response has no message id"))
	}
	return result, nil
}

func (c *Client) LeaveChat(ctx context.Context, request hhwrite.ChatLeaveRequest) (hhwrite.WriteResult, error) {
	chatID, err := parsePositiveID(request.ConversationID, "HH conversation external id")
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if err := c.requireToken(); err != nil {
		return notSent(hhwrite.ErrorAuthentication, err)
	}
	body, err := json.Marshal(chatLeaveWire{ChatID: chatID})
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, fmt.Errorf("marshal HH chat leave: %w", err))
	}
	referer := strings.TrimRight(c.chatURL.String(), "/") + "/chat/" + strconv.FormatInt(chatID, 10)
	return c.do(ctx, http.MethodPost, c.chatURL, "/chatik/api/leave", bytes.NewReader(body), referer, "application/json", true)
}

func (c *Client) TouchResume(ctx context.Context, request hhwrite.ResumeTouchRequest) (hhwrite.WriteResult, error) {
	if strings.TrimSpace(request.ResumeHash) == "" {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("resume hash is empty"))
	}
	if err := c.requireToken(); err != nil {
		return notSent(hhwrite.ErrorAuthentication, err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("resume", request.ResumeHash); err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if err := writer.WriteField("undirectable", "true"); err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if err := writer.Close(); err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	return c.do(ctx, http.MethodPost, c.baseURL, "/applicant/resumes/touch", &body, c.baseURL.String()+"/applicant/my_resumes", writer.FormDataContentType(), false)
}

func (c *Client) SetJobSearchStatus(ctx context.Context, request hhwrite.JobSearchStatusRequest) (hhwrite.WriteResult, error) {
	if strings.TrimSpace(request.Status) == "" {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("job-search status is empty"))
	}
	if err := c.requireToken(); err != nil {
		return notSent(hhwrite.ErrorAuthentication, err)
	}
	if c.resumeProfileURL == nil {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("HH resume profile URL is not configured"))
	}
	endpoint := "/profile/shards/user_statuses/job_search_status?status=" + url.QueryEscape(request.Status)
	return c.do(ctx, http.MethodPost, c.resumeProfileURL, endpoint, nil, "", "", false)
}

type chatMessageWire struct {
	ChatID         int64  `json:"chatId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Text           string `json:"text"`
}

type chatLeaveWire struct {
	ChatID int64 `json:"chatId"`
}

func (c *Client) do(ctx context.Context, method string, base *url.URL, endpoint string, body io.Reader, referer, contentType string, decodeJSON bool) (hhwrite.WriteResult, error) {
	if ctx == nil {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("HH write context is nil"))
	}
	if err := ctx.Err(); err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	if c == nil || c.httpClient == nil || !validBaseURL(base) {
		return notSent(hhwrite.ErrorRequestValidation, errors.New("HH write client is not configured"))
	}
	u, err := resolveURL(base, endpoint)
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return notSent(hhwrite.ErrorRequestValidation, fmt.Errorf("create HH write request: %w", err))
	}
	setStandardHeaders(req, c.userAgent, c.acceptLanguage, c.accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Xsrftoken", c.xsrfToken)
	setOperationHeaders(req, endpoint)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ambiguous(hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous}, hhwrite.ErrorNetworkAmbiguous, err)
	}
	defer resp.Body.Close()
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	metadata := correlationIDs(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if readErr != nil {
			bodyBytes = nil
		}
		if resp.StatusCode == http.StatusConflict {
			return ambiguousHTTPStatus(resp.StatusCode, hhwrite.ErrorResponseAmbiguous, resp.Header.Get("Content-Type"), bodyBytes, metadata, readErr)
		}
		if resp.StatusCode >= 500 {
			return ambiguousHTTPStatus(resp.StatusCode, hhwrite.ErrorServer, resp.Header.Get("Content-Type"), bodyBytes, metadata, readErr)
		}
		return rejected(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes, metadata, readErr)
	}
	result := hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderStatus: resp.StatusCode, Timestamp: time.Now().UTC(), Metadata: metadata}
	if readErr != nil || len(bodyBytes) > maxResponseBody {
		return ambiguous(result, hhwrite.ErrorResponseAmbiguous, errors.New("HH write response could not be read"))
	}
	if !decodeJSON {
		return result, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		return ambiguous(result, hhwrite.ErrorResponseAmbiguous, errors.New("invalid HH write response"))
	}
	if _, hasError := decoded["error"]; hasError {
		return rejected(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes, metadata, errors.New("HH write rejected"))
	}
	result.ProviderID = firstString(decoded, "messageId", "message_id", "id")
	return result, nil
}

func (c *Client) requireToken() error {
	if c == nil || strings.TrimSpace(c.xsrfToken) == "" {
		return errors.New("xsrf token not found")
	}
	return nil
}

func validateVacancyResponse(request hhwrite.VacancyResponseRequest) error {
	if request.VacancyID <= 0 || strings.TrimSpace(request.ResumeHash) == "" {
		return errors.New("HH vacancy response request is invalid")
	}
	if request.Test != nil {
		seen := map[int]bool{}
		for _, answer := range request.Test.Answers {
			if answer.TaskID <= 0 || seen[answer.TaskID] || (answer.ChoiceID == "" && answer.Text == "") {
				return errors.New("HH vacancy test answer is invalid")
			}
			seen[answer.TaskID] = true
		}
	}
	return nil
}

func notSent(category hhwrite.ErrorCategory, err error) (hhwrite.WriteResult, error) {
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, &hhwrite.TransportError{Category: category, Outcome: hhwrite.OutcomeNotSent, Err: err}
}

func ambiguous(result hhwrite.WriteResult, category hhwrite.ErrorCategory, err error) (hhwrite.WriteResult, error) {
	result.Outcome = hhwrite.OutcomeAmbiguous
	return result, &hhwrite.TransportError{Category: category, Outcome: hhwrite.OutcomeAmbiguous, Status: result.ProviderStatus, Err: err}
}

func ambiguousHTTPStatus(status int, category hhwrite.ErrorCategory, contentType string, body []byte, metadata map[string]string, readErr error) (hhwrite.WriteResult, error) {
	err := fmt.Errorf("HH returned status %d; delivery is uncertain", status)
	if readErr != nil {
		err = fmt.Errorf("%w: response body unavailable", err)
	}
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAmbiguous, ProviderStatus: status, Metadata: metadata}, &hhwrite.TransportError{
		Category: category, Outcome: hhwrite.OutcomeAmbiguous, Status: status,
		ResponseContentType: contentType, ResponseBody: sanitizeBody(body, contentType), ErrorFields: errorFields(body, contentType), CorrelationIDs: cloneStrings(metadata), Err: err,
	}
}

func rejected(status int, contentType string, body []byte, metadata map[string]string, readErr error) (hhwrite.WriteResult, error) {
	category := hhwrite.ErrorProvider
	if status == http.StatusUnauthorized {
		category = hhwrite.ErrorAuthentication
	} else if status == http.StatusForbidden {
		category = hhwrite.ErrorPermission
	} else if status == http.StatusTooManyRequests {
		category = hhwrite.ErrorRateLimited
	} else if status >= 500 {
		category = hhwrite.ErrorServer
	}
	err := fmt.Errorf("HH returned status %d", status)
	if readErr != nil {
		err = fmt.Errorf("%w: response body unavailable", err)
	}
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeRejected, ProviderStatus: status, Metadata: metadata}, &hhwrite.TransportError{
		Category: category, Outcome: hhwrite.OutcomeRejected, Status: status,
		ResponseContentType: contentType, ResponseBody: sanitizeBody(body, contentType), ErrorFields: errorFields(body, contentType), CorrelationIDs: cloneStrings(metadata), Err: err,
	}
}

func setStandardHeaders(req *http.Request, ua, language, accept string) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", language)
	req.Header.Set("Accept", accept)
	req.Header.Set("Sec-CH-UA", secCHUAHeader)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
}

func setOperationHeaders(req *http.Request, endpoint string) {
	switch endpoint {
	case "/applicant/vacancy_response/popup":
		req.Header.Set("X-Hhtmfrom", "vacancy")
		req.Header.Set("X-Hhtmsource", "vacancy_response")
	case "/chatik/api/leave":
		req.Header.Set("X-hhtmFrom", "resume")
		req.Header.Set("X-hhtmFromLabel", "resume")
		req.Header.Set("X-hhtmSource", "app")
		req.Header.Set("X-hhtmSourceLabel", "resume")
	case "/applicant/resumes/touch":
		req.Header.Set("X-Hhtmfrom", "negotiation_list")
		req.Header.Set("X-Hhtmsource", "resume_list")
	case "/profile/shards/user_statuses/job_search_status?status=looking_for_offers":
		req.Header.Set("X-hhtmSource", "resume_list")
		req.Header.Set("X-hhtmFrom", "")
		req.Header.Set("X-hhtmSourceLabel", "")
		req.Header.Set("X-hhtmFromLabel", "")
	}
}

func parsePositiveID(value, label string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s is not numeric", label)
	}
	return id, nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
		if value, ok := values[key].(float64); ok {
			return strconv.FormatInt(int64(value), 10)
		}
	}
	return ""
}

func validBaseURL(value *url.URL) bool {
	return value != nil && value.Scheme != "" && value.Host != "" && value.User == nil
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func resolveURL(base *url.URL, endpoint string) (*url.URL, error) {
	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse HH write endpoint: %w", err)
	}
	return base.ResolveReference(ref), nil
}

func correlationIDs(header http.Header) map[string]string {
	result := map[string]string{}
	for _, name := range []string{"X-Request-ID", "X-Request-Id", "X-Correlation-ID", "Traceparent"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			result[name] = value[:min(len(value), 256)]
		}
	}
	return result
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func sanitizeBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		var value any
		if json.Unmarshal(body, &value) == nil {
			body, _ = json.Marshal(sanitizeValue(value))
		}
	} else {
		text := html.UnescapeString(htmlTag.ReplaceAllString(string(body), " "))
		body = []byte(strings.Join(strings.Fields(text), " "))
	}
	if len(body) > 4096 {
		body = body[:4096]
	}
	if sensitiveField.Match(body) {
		return "[HH response omitted: sensitive field detected]"
	}
	return strings.TrimSpace(string(body))
}

func sanitizeValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range item {
			if !sensitiveField.MatchString(key) {
				result[key] = sanitizeValue(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = sanitizeValue(child)
		}
		return result
	default:
		return value
	}
}

func errorFields(body []byte, contentType string) map[string]string {
	result := map[string]string{}
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return result
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return result
	}
	for key, value := range raw {
		if sensitiveField.MatchString(key) || !errorField.MatchString(key) {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" || sensitiveField.MatchString(text) {
			continue
		}
		if len(text) > 512 {
			text = text[:512]
		}
		result[key] = text
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
