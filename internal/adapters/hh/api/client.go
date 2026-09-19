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

	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
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
	SearchParams    url.Values
	Now             func() time.Time
	Timeout         time.Duration
	MaxRetryAfter   time.Duration
	TokenExpirySkew time.Duration
}

// Options is retained as a concise constructor spelling for this adapter.
type Options = APIClientOptions

// APIHHClient performs authenticated, read-only HH API requests and normalizes
// the supported user, resume, and vacancy endpoints.
type APIHHClient struct {
	baseURL         *url.URL
	httpClient      *http.Client
	tokenStore      TokenStore
	oauthConfig     OAuthConfig
	userAgent       string
	searchParams    url.Values
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
		oauthConfig: options.OAuthConfig, userAgent: userAgent, searchParams: cloneAPIValues(options.SearchParams), now: now,
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

var _ hhreadport.HHReadSource = (*APIHHClient)(nil)
var _ hhreadport.VacancyDetailSource = (*APIHHClient)(nil)
var _ hhreadport.VacancyDuplicateStateSource = (*APIHHClient)(nil)
var _ hhreadport.ResumeReadSource = (*APIHHClient)(nil)

// CurrentUser reads only the safe identity metadata needed by API transport
// selection and diagnostics.
func (c *APIHHClient) CurrentUser(ctx context.Context) (UserMetadata, error) {
	body, err := c.get(ctx, "/me", nil)
	if err != nil {
		return UserMetadata{}, err
	}
	var value wireMe
	if err := decodeWire(body, &value); err != nil {
		return UserMetadata{}, err
	}
	return mapUser(value), nil
}

// ReadResumes reads the operator's own resume summaries. It is a read-only
// optional capability and does not imply suitability or application parity.
func (c *APIHHClient) ReadResumes(ctx context.Context) ([]hhread.ResumeRecord, error) {
	body, err := c.get(ctx, "/resumes/mine", nil)
	if err != nil {
		return nil, err
	}
	values, err := decodeResumeCollection(body)
	if err != nil {
		return nil, err
	}
	result := make([]hhread.ResumeRecord, 0, len(values))
	for _, value := range values {
		mapped, mapErr := mapResumeWire(value)
		if mapErr != nil {
			return nil, mapErr
		}
		result = append(result, mapped)
	}
	return result, nil
}

func (c *APIHHClient) ReadResume(ctx context.Context, id string) (hhread.ResumeRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\?#\r\n") {
		return hhread.ResumeRecord{}, errors.New("HH API resume id is invalid")
	}
	body, err := c.get(ctx, "/resumes/"+url.PathEscape(id), nil)
	if err != nil {
		return hhread.ResumeRecord{}, err
	}
	var value wireResume
	if err := decodeWire(body, &value); err != nil {
		return hhread.ResumeRecord{}, err
	}
	return mapResumeWire(value)
}

func (c *APIHHClient) ReadVacancies(ctx context.Context, cursor string) (hhread.VacancyPage, error) {
	page, err := parseAPICursor(cursor)
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	body, err := c.get(ctx, "/vacancies", apiVacancyQuery(c.searchParams, page))
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	var value wirePage
	if err := decodeWire(body, &value); err != nil {
		return hhread.VacancyPage{}, err
	}
	items := make([]hhread.VacancyRecord, 0, len(value.Items))
	for _, item := range value.Items {
		mapped, mapErr := mapVacancyWire(item)
		if mapErr != nil {
			return hhread.VacancyPage{}, mapErr
		}
		items = append(items, mapped)
	}
	result := hhread.VacancyPage{Items: items, NextCursor: nextAPICursor(page, value.Pages, len(items))}
	if value.Found != nil {
		result.Found, result.FoundKnown = *value.Found, true
	}
	return result, nil
}

func (c *APIHHClient) ReadVacancyDetail(ctx context.Context, id int) (hhread.VacancyRecord, error) {
	value, err := c.readVacancyDetailWire(ctx, id)
	if err != nil {
		return hhread.VacancyRecord{}, err
	}
	return mapVacancyWire(value)
}

// ReadSuitableResumeIDs follows the provider-supplied vacancy URL and returns
// only the opaque provider resume IDs exposed by that read-only resource.
func (c *APIHHClient) ReadSuitableResumeIDs(ctx context.Context, endpoint string) ([]string, error) {
	body, _, err := c.getProviderEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var value wireSuitableResumePage
	if err := decodeWire(body, &value); err != nil {
		return nil, err
	}
	return mapSuitableResumePage(value)
}

// ReadSuitableResumeScan exhausts the provider-supplied suitable-resume
// resource when pagination metadata proves how to reach its final page.
// Missing pagination evidence is returned as an incomplete scan, not as a
// false negative.
func (c *APIHHClient) ReadSuitableResumeScan(ctx context.Context, endpoint string) (hhread.SuitableResumeScan, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return hhread.SuitableResumeScan{}, errors.New("HH API suitable resumes endpoint is empty")
	}
	result := hhread.SuitableResumeScan{}
	seenEndpoints := map[string]struct{}{}
	seenIDs := map[string]struct{}{}
	current := endpoint
	found := -1
	rawItems := 0
	for result.PagesChecked < 1000 {
		if _, seen := seenEndpoints[current]; seen {
			return hhread.SuitableResumeScan{}, errors.New("HH API suitable resumes pagination repeated an endpoint")
		}
		seenEndpoints[current] = struct{}{}
		body, _, err := c.getProviderEndpoint(ctx, current)
		if err != nil {
			return hhread.SuitableResumeScan{}, err
		}
		var value wireSuitableResumePage
		if err := decodeWire(body, &value); err != nil {
			return hhread.SuitableResumeScan{}, err
		}
		ids, err := mapSuitableResumePage(value)
		if err != nil {
			return hhread.SuitableResumeScan{}, err
		}
		result.PagesChecked++
		rawItems += len(ids)
		for _, id := range ids {
			if _, ok := seenIDs[id]; ok {
				continue
			}
			seenIDs[id] = struct{}{}
			result.IDs = append(result.IDs, id)
		}
		if value.Found != nil {
			if *value.Found < 0 || (found >= 0 && found != *value.Found) {
				return hhread.SuitableResumeScan{}, errors.New("HH API suitable resumes found count is inconsistent")
			}
			found = *value.Found
		}
		next, complete, err := nextSuitableResumeEndpoint(current, value)
		if err != nil {
			return hhread.SuitableResumeScan{}, err
		}
		if complete {
			if found >= 0 && rawItems != found {
				return hhread.SuitableResumeScan{}, errors.New("HH API suitable resumes scan count is incomplete")
			}
			result.Complete = true
			return result, nil
		}
		if next == "" {
			return result, nil
		}
		current = next
	}
	return hhread.SuitableResumeScan{}, errors.New("HH API suitable resumes pagination exceeded safety bound")
}

func nextSuitableResumeEndpoint(current string, value wireSuitableResumePage) (string, bool, error) {
	if value.Paging != nil && value.Paging.Next != nil && strings.TrimSpace(value.Paging.Next.URL) != "" {
		return strings.TrimSpace(value.Paging.Next.URL), false, nil
	}
	if value.HasNext != nil {
		if !*value.HasNext {
			return "", true, nil
		}
		return nextProviderPageURLChecked(current, value.Page)
	}
	if value.Pages != nil {
		if *value.Pages < 1 || value.Page == nil || *value.Page < 0 || *value.Page >= *value.Pages {
			return "", false, errors.New("HH API suitable resumes pagination is invalid")
		}
		if *value.Page+1 >= *value.Pages {
			return "", true, nil
		}
		return nextProviderPageURLChecked(current, value.Page)
	}
	return "", false, nil
}

func nextProviderPageURLChecked(current string, page *int) (string, bool, error) {
	if page == nil || *page < 0 {
		return "", false, errors.New("HH API suitable resumes page is invalid")
	}
	next := nextProviderPageURL(current, *page+1)
	if next == "" {
		return "", false, errors.New("HH API suitable resumes next page URL is invalid")
	}
	return next, false, nil
}

// ReadNegotiationCollections follows the vacancy-scoped provider URL and
// decodes the collection index. It never treats the index as a negotiation
// item list.
func (c *APIHHClient) ReadNegotiationCollections(ctx context.Context, endpoint string) (hhread.NegotiationCollectionIndex, error) {
	body, endpointVacancyID, err := c.getProviderEndpoint(ctx, endpoint)
	if err != nil {
		return hhread.NegotiationCollectionIndex{}, err
	}
	if endpointVacancyID <= 0 {
		return hhread.NegotiationCollectionIndex{}, errors.New("HH API negotiations vacancy id is invalid")
	}
	var value wireNegotiationCollections
	if err := decodeWire(body, &value); err != nil {
		return hhread.NegotiationCollectionIndex{}, err
	}
	if value.Collections == nil && value.Items != nil {
		var page wireNegotiationPage
		if err := decodeWire(body, &page); err != nil {
			return hhread.NegotiationCollectionIndex{}, err
		}
		mapped, mapErr := mapNegotiationPage(page, endpointVacancyID, endpoint)
		if mapErr != nil {
			return hhread.NegotiationCollectionIndex{}, mapErr
		}
		return hhread.NegotiationCollectionIndex{DirectPage: &mapped}, nil
	}
	return mapNegotiationCollections(value)
}

// ReadNegotiationCollection follows one provider-declared collection URL.
// Pagination metadata is retained so callers can exhaust the collection
// before deriving a negative duplicate result.
func (c *APIHHClient) ReadNegotiationCollection(ctx context.Context, endpoint string) (hhread.NegotiationPage, error) {
	body, endpointVacancyID, err := c.getProviderEndpoint(ctx, endpoint)
	if err != nil {
		return hhread.NegotiationPage{}, err
	}
	var value wireNegotiationPage
	if err := decodeWire(body, &value); err != nil {
		return hhread.NegotiationPage{}, err
	}
	return mapNegotiationPage(value, endpointVacancyID, endpoint)
}

// ReadNegotiations preserves the older one-page normalized read API for
// callers outside application preflight. New duplicate-state code uses the
// collection-aware methods above.
func (c *APIHHClient) ReadNegotiations(ctx context.Context, endpoint string) (hhread.ApplicationPage, error) {
	page, err := c.ReadNegotiationCollection(ctx, endpoint)
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	return hhread.ApplicationPage{Items: page.Items, NextCursor: page.NextURL}, nil
}

// ReadVacancyDetailRequiringRelation is the explicit duplicate-state probe.
// It fails closed when the API does not provide one unambiguous applicant
// relation instead of treating unknown relation data as an unresponded state.
func (c *APIHHClient) ReadVacancyDetailRequiringRelation(ctx context.Context, id int) (hhread.VacancyRecord, error) {
	value, err := c.readVacancyDetailWire(ctx, id)
	if err != nil {
		return hhread.VacancyRecord{}, err
	}
	return mapVacancyWireRequiringRelation(value)
}

func (c *APIHHClient) readVacancyDetailWire(ctx context.Context, id int) (wireVacancy, error) {
	if id <= 0 {
		return wireVacancy{}, errors.New("HH API vacancy id is invalid")
	}
	body, err := c.get(ctx, "/vacancies/"+strconv.Itoa(id), nil)
	if err != nil {
		return wireVacancy{}, err
	}
	var value wireVacancy
	if err := decodeWire(body, &value); err != nil {
		return wireVacancy{}, err
	}
	return value, nil
}

// Applicant negotiation semantics are not proven for the API transport. A
// capability error is safer than an empty page, an inferred negative, or a
// browser fallback.
func (c *APIHHClient) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, unsupportedCapability("applications", "applicant negotiation semantics are unproven")
}

func (c *APIHHClient) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, unsupportedCapability("conversations", "applicant conversation semantics are unproven")
}

func parseAPICursor(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid HH API pagination cursor")
	}
	return parsed, nil
}

func nextAPICursor(page, pages, itemCount int) string {
	if pages > 0 {
		if page+1 >= pages {
			return ""
		}
		return strconv.Itoa(page + 1)
	}
	if itemCount > 0 {
		return strconv.Itoa(page + 1)
	}
	return ""
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

func (c *APIHHClient) getProviderEndpoint(ctx context.Context, endpoint string) ([]byte, int, error) {
	if c == nil || c.baseURL == nil {
		return nil, 0, newAPIError(APIErrorRemote, 0, "/", "", 0, errAPINetworkRequest)
	}
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Fragment != "" || parsed.Path == "" {
		return nil, 0, newAPIError(APIErrorRemote, 0, "/", "", 0, errAPINetworkRequest)
	}
	if parsed.User != nil || parsed.Host != "" && !parsed.IsAbs() {
		return nil, 0, newAPIError(APIErrorRemote, 0, "/", "", 0, errAPINetworkRequest)
	}
	if parsed.IsAbs() && (parsed.Scheme != c.baseURL.Scheme || parsed.Host != c.baseURL.Host) {
		return nil, 0, newAPIError(APIErrorRemote, 0, "/", "", 0, errAPINetworkRequest)
	}
	body, err := c.get(ctx, parsed.Path, parsed.Query())
	if err != nil {
		return nil, 0, err
	}
	return body, int(parseProviderID(parsed.Query().Get("vacancy_id"))), nil
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
		return OAuthTokens{}, newAPIError(APIErrorRemote, 0, path, "", 0, errAPITokenRefresh)
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
