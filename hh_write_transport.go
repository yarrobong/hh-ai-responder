package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var sensitiveResponseField = regexp.MustCompile(`(?i)(authorization|cookie|x-xsrftoken|xsrf|access[_-]?token|refresh[_-]?token|session[_-]?id|password|client[_-]?secret)`)
var sensitiveResponseAssignment = regexp.MustCompile(`(?i)(authorization|cookie|x-xsrftoken|xsrf|access[_-]?token|refresh[_-]?token|session[_-]?id|password|client[_-]?secret)\s*[:=]\s*[^,;\s<]+`)
var responseHTMLTag = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<[^>]+>`)
var responseErrorField = regexp.MustCompile(`(?i)(error|message|code|reason|field|type|status)`)

const (
	defaultHHChatURL = "https://chatik.hh.ru"

	HHWriteErrorRequestValidationFailed = "request_validation_failed"
	HHWriteErrorBadRequest              = "bad_request"
	HHWriteErrorAuthenticationFailed    = "authentication_failed"
	HHWriteErrorPermissionDenied        = "permission_denied"
	HHWriteErrorRateLimited             = "rate_limited"
	HHWriteErrorServerError             = "server_error"
	HHWriteErrorNetworkUncertain        = "network_uncertain"
	HHWriteErrorDeliveryUncertain       = "delivery_uncertain"
)

// HHWriteTransportError is the safe, structured transport error stored by the
// gateway. ResponseBody is sanitized and bounded before it reaches the audit.
type HHWriteTransportError struct {
	Category            string            `json:"category"`
	Status              int               `json:"status,omitempty"`
	ResponseContentType string            `json:"response_content_type,omitempty"`
	ResponseBody        string            `json:"response_body,omitempty"`
	HHErrorFields       map[string]string `json:"hh_error_fields,omitempty"`
	CorrelationIDs      map[string]string `json:"correlation_ids,omitempty"`
	Err                 error             `json:"-"`
	DeliveryUncertain   bool              `json:"delivery_uncertain,omitempty"`
	Retryable           bool              `json:"retryable,omitempty"`
}

func (e *HHWriteTransportError) Error() string {
	if e == nil {
		return "HH write failed"
	}
	message := "HH write failed"
	if e.Err != nil {
		message = e.Err.Error()
	}
	if e.Category != "" {
		message = e.Category + ": " + message
	}
	return message
}

func (e *HHWriteTransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HHWriteError remains as a source-compatible name for integrations/tests
// written before the transport taxonomy was introduced.
type HHWriteError = HHWriteTransportError

type HHWriteHeaderPresence struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

type HHWriteRequestPreview struct {
	Method          string                  `json:"method"`
	Endpoint        string                  `json:"endpoint"`
	DestinationType string                  `json:"destination_type"`
	DestinationID   string                  `json:"destination_id"`
	ContentType     string                  `json:"content_type"`
	Payload         map[string]any          `json:"payload"`
	Headers         []HHWriteHeaderPresence `json:"headers"`
}

// SanitizedHHWriteRequestPreview is the CLI/dashboard representation. The
// idempotency value is intentionally not exposed even though its presence is
// required by the transport contract.
type SanitizedHHWriteRequestPreview struct {
	Method               string                  `json:"method"`
	Endpoint             string                  `json:"endpoint"`
	DestinationType      string                  `json:"destination_type"`
	ChatID               string                  `json:"chat_id"`
	ContentType          string                  `json:"content_type"`
	IdempotencyKey       string                  `json:"idempotency_key"`
	IdempotencyKeySource string                  `json:"idempotency_key_source"`
	IdempotencyKeyLength int                     `json:"idempotency_key_length"`
	Text                 string                  `json:"text"`
	Headers              []HHWriteHeaderPresence `json:"headers"`
}

func sanitizeHHWriteRequestPreview(preview HHWriteRequestPreview) SanitizedHHWriteRequestPreview {
	chatID := preview.DestinationID
	text, _ := preview.Payload["text"].(string)
	key, _ := preview.Payload["idempotencyKey"].(string)
	return SanitizedHHWriteRequestPreview{
		Method: preview.Method, Endpoint: preview.Endpoint, DestinationType: preview.DestinationType,
		ChatID: chatID, ContentType: preview.ContentType, IdempotencyKey: "present",
		IdempotencyKeySource: "SendNonce", IdempotencyKeyLength: len(key), Text: text,
		Headers: append([]HHWriteHeaderPresence{}, preview.Headers...),
	}
}

// buildHHWriteRequest is the single source of truth for both offline request
// previews and the live Chatik transport. The transport must never rebuild a
// payload from action.ID or any other audit identity.
func buildHHWriteRequest(chatURL, destinationID, text, idempotencyKey string) (HHWriteRequestPreview, []byte, error) {
	chatURL = strings.TrimRight(strings.TrimSpace(chatURL), "/")
	if chatURL == "" {
		chatURL = defaultHHChatURL
	}
	preview := HHWriteRequestPreview{
		Method:          "POST",
		Endpoint:        chatURL + "/chatik/api/send",
		DestinationType: "chat_id",
		DestinationID:   strings.TrimSpace(destinationID),
		ContentType:     "application/json",
		Payload: map[string]any{
			"chatId":         0,
			"text":           text,
			"idempotencyKey": idempotencyKey,
		},
	}
	if id, err := strconv.ParseInt(preview.DestinationID, 10, 64); err == nil {
		preview.Payload["chatId"] = id
	}
	preview.Headers = hhWriteHeaderPresence()
	if err := ValidateHHWriteRequest(preview); err != nil {
		return preview, nil, err
	}
	body, err := json.Marshal(preview.Payload)
	if err != nil {
		return preview, nil, fmt.Errorf("marshal HH write payload: %w", err)
	}
	return preview, body, nil
}

// buildHHWriteRequestPreview remains a source-compatible name for callers
// that explicitly ask for an offline preview. It delegates to the canonical
// request builder above.
func buildHHWriteRequestPreview(chatURL, destinationID, text, idempotencyKey string) (HHWriteRequestPreview, []byte, error) {
	return buildHHWriteRequest(chatURL, destinationID, text, idempotencyKey)
}

func hhWriteHeaderPresence() []HHWriteHeaderPresence {
	names := []string{
		"Accept", "Accept-Language", "Content-Type", "Referer", "Sec-CH-UA",
		"Sec-CH-UA-Mobile", "Sec-CH-UA-Platform", "Sec-Fetch-Dest",
		"Sec-Fetch-Mode", "Sec-Fetch-Site", "User-Agent", "X-Requested-With",
		"X-Xsrftoken",
	}
	result := make([]HHWriteHeaderPresence, 0, len(names))
	for _, name := range names {
		result = append(result, HHWriteHeaderPresence{Name: name, Present: true})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// ValidateHHWriteRequest checks the exact chatik request contract before any
// requester call. It does not inspect cookies and never performs I/O.
func ValidateHHWriteRequest(request HHWriteRequestPreview) error {
	if strings.TrimSpace(request.Method) != "POST" {
		return errors.New("HH write request method must be POST")
	}
	parsed, err := url.Parse(strings.TrimSpace(request.Endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "/chatik/api/send" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("HH write request endpoint is invalid")
	}
	if request.DestinationType != "chat_id" {
		return errors.New("HH write destination type is unsupported")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(request.DestinationID), 10, 64)
	if err != nil || id <= 0 {
		return errors.New("HH write destination id must be a positive numeric chat id")
	}
	if request.ContentType != "application/json" {
		return errors.New("HH write content type must be application/json")
	}
	if len(request.Payload) != 3 {
		return errors.New("HH write payload has unsupported fields")
	}
	chatID, ok := request.Payload["chatId"].(int64)
	if !ok || chatID != id {
		return errors.New("HH write payload chatId does not match destination")
	}
	text, ok := request.Payload["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return errors.New("HH write payload text is empty")
	}
	if !utf8.ValidString(text) {
		return errors.New("HH write payload text is not valid UTF-8")
	}
	key, ok := request.Payload["idempotencyKey"].(string)
	if !ok || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return errors.New("HH write idempotencyKey is invalid")
	}
	// Approved nonces are ASCII/hex, so byte length is deterministic and cannot
	// drift between preview and live transport because of Unicode rune semantics.
	keyLength := len(key)
	if keyLength < 30 || keyLength > 40 {
		return errors.New("HH write idempotencyKey must be from 30 to 40 symbols")
	}
	for _, required := range []string{"Accept", "Content-Type", "Referer", "X-Requested-With", "X-Xsrftoken"} {
		if !headerPresenceContains(request.Headers, required) {
			return fmt.Errorf("HH write required header %s is not configured", required)
		}
	}
	return nil
}

func headerPresenceContains(headers []HHWriteHeaderPresence, name string) bool {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) && header.Present {
			return true
		}
	}
	return false
}

func sanitizeHHResponseBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	const maxResponseBody = 4096
	var value any
	if strings.Contains(strings.ToLower(contentType), "json") && json.Unmarshal(body, &value) == nil {
		value = sanitizeResponseValue(value)
		encoded, err := json.Marshal(value)
		if err == nil {
			body = encoded
		}
	} else {
		text := responseHTMLTag.ReplaceAllString(string(body), " ")
		text = html.UnescapeString(text)
		text = sensitiveResponseAssignment.ReplaceAllString(text, "$1=[REDACTED]")
		body = []byte(strings.Join(strings.Fields(text), " "))
	}
	if len(body) > maxResponseBody {
		body = body[:maxResponseBody]
	}
	result := strings.TrimSpace(string(body))
	if sensitiveResponseField.MatchString(result) {
		return "[HH response omitted: sensitive field detected]"
	}
	return result
}

func sanitizeResponseValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if sensitiveResponseField.MatchString(key) {
				continue
			}
			result[key] = sanitizeResponseValue(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = sanitizeResponseValue(item)
		}
		return result
	default:
		return value
	}
}

func sanitizedHHErrorFields(body []byte, contentType string) map[string]string {
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return map[string]string{}
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return map[string]string{}
	}
	result := map[string]string{}
	add := func(key string, value any) {
		if sensitiveResponseField.MatchString(key) || !responseErrorField.MatchString(key) {
			return
		}
		var text string
		switch item := value.(type) {
		case string:
			text = item
		case float64, bool:
			text = fmt.Sprint(item)
		default:
			return
		}
		text = strings.TrimSpace(text)
		if text == "" || sensitiveResponseField.MatchString(text) {
			return
		}
		if len(text) > 512 {
			text = text[:512]
		}
		result[key] = text
	}
	for key, value := range raw {
		add(key, value)
		if items, ok := value.([]any); ok {
			for index, item := range items {
				object, ok := item.(map[string]any)
				if !ok {
					continue
				}
				for nestedKey, nestedValue := range object {
					add(fmt.Sprintf("%s[%d].%s", key, index, nestedKey), nestedValue)
				}
			}
		}
	}
	return result
}

func populateTransportErrorEvent(event *HHWriteEvent, err error, preview HHWriteRequestPreview) {
	if event == nil {
		return
	}
	event.Endpoint = preview.Endpoint
	event.RequestMethod = preview.Method
	event.RequestContentType = preview.ContentType
	event.DestinationType = preview.DestinationType
	event.DestinationID = preview.DestinationID
	var transportErr *HHWriteTransportError
	if !errors.As(err, &transportErr) || transportErr == nil {
		return
	}
	event.Error = transportErr.Error()
	event.HTTPStatus = transportErr.Status
	event.ResponseContentType = transportErr.ResponseContentType
	event.ResponseBody = transportErr.ResponseBody
	event.HHErrorFields = copyStringMap(transportErr.HHErrorFields)
	event.CorrelationIDs = copyStringMap(transportErr.CorrelationIDs)
}

func (r *HHAIResponder) buildHHWriteRequest(chatID int64, text, idempotencyKey string) (*HHWriteRequestPreview, []byte, error) {
	preview, body, err := buildHHWriteRequest(r.chatURL, strconv.FormatInt(chatID, 10), text, idempotencyKey)
	if err != nil {
		return &preview, nil, err
	}
	token := r.XSRFToken()
	if token == "" {
		return &preview, nil, &HHWriteTransportError{Category: HHWriteErrorAuthenticationFailed, Err: errors.New("xsrf token not found")}
	}
	return &preview, body, nil
}

func (r *HHAIResponder) newHHWriteHTTPRequest(preview HHWriteRequestPreview, body []byte) (*http.Request, error) {
	return r.buildRequest("POST", preview.Endpoint, bytes.NewReader(body), map[string]string{
		"Content-Type":     "application/json",
		"Accept":           "application/json",
		"X-Requested-With": "XMLHttpRequest",
		"X-Xsrftoken":      r.XSRFToken(),
		"Referer":          strings.TrimRight(r.chatURL, "/") + "/?platform=xhh&dest=iframe",
	})
}
