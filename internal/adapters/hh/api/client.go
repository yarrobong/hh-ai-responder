package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPITimeout      = 30 * time.Second
	defaultMaxRetryAfter   = time.Minute
	maxAPIResponseBody     = 8 << 20
	maxAPIErrorBody        = 64 << 10
	maxProviderFieldLength = 256
)

// APIClientOptions is the complete composition boundary for the read-only
// API transport. It does not read process configuration or environment state.
type APIClientOptions struct {
	BaseURL         *url.URL
	HTTPClient      *http.Client
	TokenStore      TokenStore
	OAuthConfig     OAuthConfig
	UserAgent       string
	Now             func() time.Time
	Timeout         time.Duration
	MaxRetryAfter   time.Duration
	TokenExpirySkew time.Duration
}

// Options is retained as a concise constructor spelling for this adapter.
type Options = APIClientOptions

// APIHHClient performs authenticated, read-only HH API requests. Endpoint
// wire models and normalization are intentionally added by later tasks.
type APIHHClient struct {
	baseURL         *url.URL
	httpClient      *http.Client
	tokenStore      TokenStore
	oauthConfig     OAuthConfig
	userAgent       string
	now             func() time.Time
	timeout         time.Duration
	maxRetryAfter   time.Duration
	tokenExpirySkew time.Duration
}

// NewAPIHHClient constructs the typed read-only API transport.
func NewAPIHHClient(options APIClientOptions) (*APIHHClient, error) {
	if !validAPIBaseURL(options.BaseURL) {
		return nil, errors.New("HH API base URL is invalid")
	}
	if options.TokenStore == nil {
		return nil, errors.New("HH API token store is required")
	}
	userAgent := strings.TrimSpace(options.UserAgent)
	if userAgent == "" || strings.ContainsAny(userAgent, "\r\n") {
		return nil, errors.New("HH API User-Agent is required")
	}
	if options.Timeout < 0 {
		return nil, errors.New("HH API timeout is invalid")
	}
	if options.MaxRetryAfter < 0 {
		return nil, errors.New("HH API retry bound is invalid")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultAPITimeout
	}
	maxRetryAfter := options.MaxRetryAfter
	if maxRetryAfter == 0 {
		maxRetryAfter = defaultMaxRetryAfter
	}
	skew := options.TokenExpirySkew
	if skew == 0 {
		skew = DefaultTokenExpirySkew
	}
	baseURL := *options.BaseURL
	return &APIHHClient{
		baseURL: &baseURL, httpClient: httpClient, tokenStore: options.TokenStore,
		oauthConfig: options.OAuthConfig, userAgent: userAgent, now: now,
		timeout: timeout, maxRetryAfter: maxRetryAfter, tokenExpirySkew: skew,
	}, nil
}

// NewClient is the package-compatible constructor spelling used by the other
// HH adapters.
func NewClient(options APIClientOptions) (*APIHHClient, error) {
	return NewAPIHHClient(options)
}

// Get performs one authenticated GET request for a safe relative API path.
// Query-bearing endpoint helpers are intentionally kept inside this package
// until later tasks add typed wire models.
func (c *APIHHClient) Get(ctx context.Context, path string) ([]byte, error) {
	return c.get(ctx, path, nil)
}

// get is the internal query-capable seam for later read-only endpoint methods.
func (c *APIHHClient) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if c == nil || c.httpClient == nil || c.tokenStore == nil || !validAPIBaseURL(c.baseURL) {
		return nil, newAPIError(APIErrorRemote, 0, path, "", 0, errAPINetworkRequest)
	}
	if ctx == nil {
		return nil, contextAPIError(path, errAPIRequestContext)
	}
	if err := ctx.Err(); err != nil {
		return nil, contextAPIError(path, err)
	}
	if err := validateAPIPath(path); err != nil {
		return nil, newAPIError(APIErrorRemote, 0, path, "", 0, errAPINetworkRequest)
	}

	tokens, err := c.tokenStore.Load(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, contextAPIError(path, ctxErr)
		}
		return nil, newAPIError(APIErrorAuthRequired, 0, path, "", 0, errAPITokenStore)
	}
	refreshed := false
	if tokens.IsExpiredAt(c.now(), c.tokenExpirySkew) {
		if tokens.RefreshToken == "" {
			return nil, newAPIError(APIErrorTokenExpired, 0, path, "", 0, errAPITokenExpired)
		}
		tokens, err = c.refreshAndSave(ctx, path, tokens)
		if err != nil {
			return nil, err
		}
		refreshed = true
	}

	body, apiErr := c.doGET(ctx, path, query, tokens)
	if apiErr == nil {
		return body, nil
	}
	if apiErr.Code == APIErrorTokenExpired && !refreshed && tokens.RefreshToken != "" {
		tokens, err = c.refreshAndSave(ctx, path, tokens)
		if err != nil {
			return nil, err
		}
		body, retryErr := c.doGET(ctx, path, query, tokens)
		if retryErr != nil {
			return nil, retryErr
		}
		return body, nil
	}
	return nil, apiErr
}

func (c *APIHHClient) refreshAndSave(ctx context.Context, path string, previous OAuthTokens) (OAuthTokens, error) {
	config := c.oauthConfig
	if config.HTTPClient == nil {
		config.HTTPClient = c.httpClient
	}
	if config.UserAgent == "" {
		config.UserAgent = c.userAgent
	}
	if config.Now == nil {
		config.Now = c.now
	}
	refreshed, err := RefreshTokens(ctx, config, previous)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return OAuthTokens{}, contextAPIError(path, ctxErr)
		}
		return OAuthTokens{}, newAPIError(APIErrorTokenRevoked, 0, path, "", 0, errAPITokenRefresh)
	}
	if err := c.tokenStore.Save(ctx, refreshed); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return OAuthTokens{}, contextAPIError(path, ctxErr)
		}
		return OAuthTokens{}, newAPIError(APIErrorRemote, 0, path, "", 0, errAPITokenStore)
	}
	return refreshed, nil
}

func (c *APIHHClient) doGET(ctx context.Context, path string, query url.Values, tokens OAuthTokens) ([]byte, *APIError) {
	if tokens.AccessToken == "" {
		return nil, newAPIError(APIErrorAuthRequired, 0, path, "", 0, errAPITokenStore)
	}
	requestURL, err := c.resolve(path, query)
	if err != nil {
		return nil, newAPIError(APIErrorRemote, 0, path, "", 0, errAPINetworkRequest)
	}
	requestContext := ctx
	cancel := func() {}
	if c.timeout > 0 {
		requestContext, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, newAPIError(APIErrorRemote, 0, path, "", 0, errAPINetworkRequest)
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if requestErr := requestContext.Err(); requestErr != nil {
			return nil, contextAPIError(path, requestErr)
		}
		return nil, newAPIError(APIErrorRemote, 0, path, "", 0, errAPINetworkRequest)
	}
	defer resp.Body.Close()
	body, tooLarge, readErr := readBounded(resp.Body, maxAPIResponseBody)
	if readErr != nil {
		if requestErr := requestContext.Err(); requestErr != nil {
			return nil, contextAPIError(path, requestErr)
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return nil, newAPIError(APIErrorRemote, resp.StatusCode, path, responseRequestID(resp.Header), 0, errAPIResponseRead)
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, mapHTTPError(path, resp.StatusCode, resp.Header, body, c.now, c.maxRetryAfter)
	}
	if tooLarge {
		return nil, newAPIError(APIErrorRemote, resp.StatusCode, path, responseRequestID(resp.Header), 0, errAPIResponseRead)
	}
	return body, nil
}

func (c *APIHHClient) resolve(path string, query url.Values) (*url.URL, error) {
	ref := &url.URL{Path: path}
	if query != nil {
		ref.RawQuery = query.Encode()
	}
	resolved := c.baseURL.ResolveReference(ref)
	if !validAPIBaseURL(resolved) || resolved.Host != c.baseURL.Host {
		return nil, errors.New("invalid API request URL")
	}
	return resolved, nil
}

type providerError struct {
	OAuthError  string `json:"oauth_error"`
	Error       string `json:"error"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description"`
	RequestID   string `json:"request_id"`
}

func mapHTTPError(path string, status int, headers http.Header, body []byte, now func() time.Time, maxRetryAfter time.Duration) *APIError {
	evidence := decodeProviderError(body)
	requestID := responseRequestID(headers)
	if requestID == "" {
		requestID = evidence.RequestID
	}
	code := APIErrorRemote
	switch status {
	case http.StatusUnauthorized:
		switch {
		case providerEvidenceContains(evidence, "revoked", "revocation"):
			code = APIErrorTokenRevoked
		case providerEvidenceContains(evidence, "expired", "token_expired"):
			code = APIErrorTokenExpired
		default:
			code = APIErrorAuthRequired
		}
	case http.StatusForbidden:
		if providerEvidenceContains(evidence, "application_not_found", "not_found_application") {
			code = APIErrorApplicationNotFound
		} else {
			code = APIErrorForbidden
		}
	case http.StatusNotFound:
		code = APIErrorApplicationNotFound
	case http.StatusTooManyRequests:
		code = APIErrorRateLimited
	}
	retryAfter := time.Duration(0)
	if code == APIErrorRateLimited {
		retryAfter = boundedRetryAfter(headers.Get("Retry-After"), now, maxRetryAfter)
	}
	return newAPIError(code, status, path, requestID, retryAfter, errAPIProvider)
}

func decodeProviderError(body []byte) providerError {
	if len(body) == 0 {
		return providerError{}
	}
	if len(body) > maxAPIErrorBody {
		body = body[:maxAPIErrorBody]
	}
	var value providerError
	if err := json.Unmarshal(body, &value); err != nil {
		return providerError{}
	}
	value.OAuthError = boundedField(value.OAuthError)
	value.Error = boundedField(value.Error)
	value.Type = boundedField(value.Type)
	value.Value = boundedField(value.Value)
	value.Description = boundedField(value.Description)
	value.RequestID = boundedField(value.RequestID)
	return value
}

func providerEvidenceContains(value providerError, markers ...string) bool {
	evidence := strings.ToLower(strings.Join([]string{value.OAuthError, value.Error, value.Type, value.Value, value.Description}, " "))
	for _, marker := range markers {
		if strings.Contains(evidence, marker) {
			return true
		}
	}
	return false
}

func boundedField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxProviderFieldLength {
		return value[:maxProviderFieldLength]
	}
	return value
}

func boundedRetryAfter(raw string, now func() time.Time, max time.Duration) time.Duration {
	if max <= 0 {
		max = defaultMaxRetryAfter
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseUint(raw, 10, 64); err == nil {
		if seconds == 0 {
			return 0
		}
		if seconds > uint64(max/time.Second) || seconds > uint64(^uint64(0)/uint64(time.Second)) {
			return max
		}
		candidate := time.Duration(seconds) * time.Second
		if candidate > max {
			return max
		}
		return candidate
	} else if strings.Trim(raw, "0123456789") == "" {
		return max
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	if now == nil {
		now = time.Now
	}
	delay := when.Sub(now())
	if delay <= 0 {
		return 0
	}
	if delay > max {
		return max
	}
	return delay
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return body, false, err
	}
	return body, int64(len(body)) > limit, nil
}

func responseRequestID(header http.Header) string {
	for _, name := range []string{"X-Request-ID", "X-Request-Id", "X-Correlation-ID", "Trace-ID", "Traceparent"} {
		if value := safeRequestID(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func validAPIBaseURL(value *url.URL) bool {
	return value != nil && (value.Scheme == "http" || value.Scheme == "https") && value.Host != ""
}

func validateAPIPath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\r\n") {
		return errors.New("invalid API request path")
	}
	return nil
}
