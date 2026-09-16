package hhread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hh-ai-responder/internal/hhread"
	hhreadports "hh-ai-responder/internal/ports/hhread"
)

const (
	defaultBaseURL       = "https://hh.ru"
	defaultChatURL       = "https://chatik.hh.ru"
	acceptHeader         = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	acceptLanguageHeader = "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7"
	userAgent            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
	secCHUAHeader        = `"Chromium";v="151", "Google Chrome";v="151", "Not-A.Brand";v="99"`
)

// Options are the complete typed composition boundary for read transport.
// Authentication is supplied by the caller; this package does not read
// environment or config files and never persists or logs the token.
type Options struct {
	BaseURL         *url.URL
	ChatURL         *url.URL
	SearchParams    url.Values
	HTTPClient      *http.Client
	XSRFToken       string
	UserID          int64
	RequestInterval time.Duration
	ReadConcurrency int
}

type Client struct {
	baseURL      *url.URL
	chatURL      *url.URL
	searchParams url.Values
	userID       int64
	token        string
	transport    *readTransport
}

func NewClient(options Options) (*Client, error) {
	baseURL := cloneURL(options.BaseURL)
	if baseURL == nil {
		baseURL, _ = url.Parse(defaultBaseURL)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("HH read base URL is invalid")
	}
	chatURL := cloneURL(options.ChatURL)
	if chatURL == nil {
		chatURL, _ = url.Parse(defaultChatURL)
	}
	if chatURL.Scheme == "" || chatURL.Host == "" {
		return nil, errors.New("HH read chat URL is invalid")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &Client{
		baseURL: baseURL, chatURL: chatURL, searchParams: cloneValues(options.SearchParams),
		userID: options.UserID, token: strings.TrimSpace(options.XSRFToken),
		transport: newReadTransport(client, options.RequestInterval, options.ReadConcurrency),
	}, nil
}

var _ hhreadports.HHReadSource = (*Client)(nil)
var _ hhreadports.HHConversationReadSource = (*Client)(nil)
var _ hhreadports.VacancyDetailSource = (*Client)(nil)

func (c *Client) ReadVacancies(ctx context.Context, cursor string) (hhread.VacancyPage, error) {
	if err := c.validate(); err != nil {
		return hhread.VacancyPage{}, err
	}
	page, err := parseCursor(cursor)
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	return c.readVacancyPage(ctx, c.searchParams, page, false)
}

// ReadVacanciesWithSearch is a typed compatibility operation for the legacy
// multi-profile search flow. It still performs only the same GET search read;
// pagination remains sequential in the caller.
func (c *Client) ReadVacanciesWithSearch(ctx context.Context, searchParams url.Values, page int) (hhread.VacancyPage, error) {
	if err := c.validate(); err != nil {
		return hhread.VacancyPage{}, err
	}
	if page < 0 {
		return hhread.VacancyPage{}, ErrInvalidCursor
	}
	return c.readVacancyPage(ctx, searchParams, page, false)
}

func (c *Client) readVacancyPage(ctx context.Context, searchParams url.Values, page int, enrichDescriptions bool) (hhread.VacancyPage, error) {
	params := cloneValues(searchParams)
	params.Set("page", strconv.Itoa(page))
	body, err := c.get(ctx, c.baseURL, "/search/vacancy", params, nil)
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	values, err := parseVacancies(body, c.baseURL)
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	items := make([]hhread.VacancyRecord, 0, len(values))
	for _, value := range values {
		description := value.Description
		if enrichDescriptions && strings.TrimSpace(description) == "" && value.ID > 0 {
			description, err = c.readVacancyDescription(ctx, value.ID)
			if err != nil {
				return hhread.VacancyPage{}, err
			}
		}
		items = append(items, vacancyRecord(value, description))
	}
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return hhread.VacancyPage{Items: items, NextCursor: next}, nil
}

// ReadVacancyDescription is the typed readback used by the legacy
// application workflow when a search card omits its description.
func (c *Client) ReadVacancyDescription(ctx context.Context, id int) (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	return c.readVacancyDescription(ctx, id)
}

// ReadVacancyDetail performs one GET-only read of the vacancy page and maps
// the provider's richer vacancyView/redirectConfig projection. It is kept
// separate from search pagination so the sync use case can apply a bounded,
// evidence-driven enrichment policy.
func (c *Client) ReadVacancyDetail(ctx context.Context, id int) (hhread.VacancyRecord, error) {
	if err := c.validate(); err != nil {
		return hhread.VacancyRecord{}, err
	}
	body, err := c.readVacancyDetailBody(ctx, id)
	if err != nil {
		return hhread.VacancyRecord{}, err
	}
	return parseVacancyDetail(body, c.baseURL, id)
}

func (c *Client) ReadApplications(ctx context.Context, cursor string) (hhread.ApplicationPage, error) {
	if err := c.validate(); err != nil {
		return hhread.ApplicationPage{}, err
	}
	page, err := parseCursor(cursor)
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	body, err := c.get(ctx, c.baseURL, "/applicant/negotiations", url.Values{"page": {strconv.Itoa(page)}}, nil)
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	items, next, recognized, err := parseNegotiations(body)
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	if !recognized {
		items, err = parseNormalizedApplications(body)
		if err != nil {
			return hhread.ApplicationPage{}, err
		}
		if len(items) > 0 {
			next = strconv.Itoa(page + 1)
		}
	}
	return hhread.ApplicationPage{Items: items, NextCursor: next}, nil
}

func (c *Client) ReadConversations(ctx context.Context, cursor string) (hhread.ConversationPage, error) {
	return c.readConversations(ctx, cursor, 0)
}

// ReadConversationsBounded preserves provider order while expanding details
// for at most limit conversations. The list page is read once, but no detail
// request is started for items outside the selected prefix.
func (c *Client) ReadConversationsBounded(ctx context.Context, cursor string, limit int) (hhread.ConversationPage, error) {
	if limit < 0 {
		return hhread.ConversationPage{}, errors.New("HH conversation limit must be non-negative")
	}
	return c.readConversations(ctx, cursor, limit)
}

func (c *Client) readConversations(ctx context.Context, cursor string, limit int) (hhread.ConversationPage, error) {
	if err := c.validate(); err != nil {
		return hhread.ConversationPage{}, err
	}
	response, err := c.readChatList(ctx, cursor)
	if err != nil {
		return hhread.ConversationPage{}, err
	}
	n := len(response.Chats.Items)
	if limit > 0 && n > limit {
		n = limit
	}
	items := make([]hhread.ConversationRecord, n)
	errs := make([]error, n)
	var reused, fetched, completed atomic.Int64
	workers := c.transport.readConcurrency
	if workers < 1 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}
	if workers > n {
		workers = n
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					errs[i] = err
					continue
				}
				data, err := c.readChatData(ctx, response.Chats.Items[i].ID)
				if err != nil {
					errs[i] = err
					continue
				}
				items[i] = mapConversation(response.Chats.Items[i], data, response)
				fetched.Add(1)
				completed.Add(1)
			}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return hhread.ConversationPage{}, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return hhread.ConversationPage{}, err
		}
	}
	return hhread.ConversationPage{Items: items, NextCursor: response.Chats.NextFrom, MetadataChecked: n, HistoryReused: int(reused.Load()), DetailedChatsFetched: int(fetched.Load())}, nil
}

func (c *Client) ReadConversation(ctx context.Context, externalID string) (hhread.ConversationRecord, error) {
	if err := c.validate(); err != nil {
		return hhread.ConversationRecord{}, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(externalID), 10, 64)
	if err != nil || id <= 0 {
		return hhread.ConversationRecord{}, ErrInvalidConversation
	}
	data, err := c.readChatData(ctx, id)
	if err != nil {
		return hhread.ConversationRecord{}, err
	}
	return mapConversation(rawChat{ID: id, Resources: data.Chat.Resources, CurrentParticipantID: data.Chat.CurrentParticipantID}, data, rawChatListResponse{}), nil
}

func (c *Client) validate() error {
	if c == nil || c.transport == nil {
		return ErrNotConfigured
	}
	return nil
}

func (c *Client) readVacancyDescription(ctx context.Context, id int) (string, error) {
	body, err := c.readVacancyDetailBody(ctx, id)
	if err != nil {
		return "", err
	}
	text := html.UnescapeString(string(body))
	idx := strings.Index(text, `{"redirectConfig":`)
	if idx < 0 {
		return "", errors.New("redirect config not found on page")
	}
	var page struct {
		VacancyView struct {
			Description string `json:"description"`
		} `json:"vacancyView"`
	}
	if err := json.NewDecoder(strings.NewReader(text[idx:])).Decode(&page); err != nil {
		return "", fmt.Errorf("failed to parse vacancy: %w", err)
	}
	return html.UnescapeString(page.VacancyView.Description), nil
}

func (c *Client) readVacancyDetailBody(ctx context.Context, id int) ([]byte, error) {
	if id <= 0 {
		return nil, errors.New("HH vacancy ID is required")
	}
	return c.get(ctx, c.baseURL, fmt.Sprintf("/vacancy/%d", id), url.Values{"hhtmFrom": {"negotiation_list"}}, nil)
}

func (c *Client) get(ctx context.Context, origin *url.URL, path string, query url.Values, extra map[string]string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ref := &url.URL{Path: path, RawQuery: query.Encode()}
	u := origin.ResolveReference(ref)
	u.RawQuery = ref.RawQuery
	// query is encoded before resolution so search parameters retain HH's
	// established escaping semantics.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create HH read request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept-Language", acceptLanguageHeader)
	request.Header.Set("Accept", acceptHeader)
	request.Header.Set("Sec-CH-UA", secCHUAHeader)
	request.Header.Set("Sec-CH-UA-Mobile", "?0")
	request.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Sec-Fetch-Mode", "cors")
	request.Header.Set("Sec-Fetch-Dest", "empty")
	if c.token != "" {
		request.Header.Set("X-Xsrftoken", c.token)
	}
	if strings.HasPrefix(path, "/chatik/") {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	for key, value := range extra {
		if value != "" {
			request.Header.Set(key, value)
		}
	}
	response, err := c.transport.do(request)
	if err != nil {
		return nil, err
	}
	if response.Status != http.StatusOK {
		return nil, unexpectedStatus(response.Status)
	}
	return response.Body, nil
}

type readResponse struct {
	Status     int
	RetryAfter string
	Body       []byte
}

type readTransport struct {
	client          *http.Client
	interval        time.Duration
	readConcurrency int
	mu              sync.Mutex
	lastStart       time.Time
	readNotBefore   time.Time
	queue           []*readWaiter
	wake            chan struct{}
	scheduler       bool
	active          int
}
type readWaiter struct {
	granted chan struct{}
	ctx     context.Context
}

func newReadTransport(client *http.Client, interval time.Duration, concurrency int) *readTransport {
	return &readTransport{client: client, interval: interval, readConcurrency: concurrency}
}

func (r *readTransport) do(req *http.Request) (*readResponse, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		result, err := r.doOnce(req)
		if err != nil || result.Status != http.StatusTooManyRequests || req.Method != http.MethodGet {
			return result, err
		}
		if attempt >= 2 {
			return result, nil
		}
		delay := time.Second * time.Duration(1<<attempt)
		if seconds, parseErr := strconv.Atoi(result.RetryAfter); parseErr == nil && seconds > 0 {
			if candidate := time.Duration(seconds) * time.Second; candidate > delay {
				delay = candidate
			}
		} else if until, parseErr := http.ParseTime(result.RetryAfter); parseErr == nil {
			if candidate := time.Until(until); candidate > delay {
				delay = candidate
			}
		}
		if err := waitContext(req.Context(), delay); err != nil {
			return nil, err
		}
	}
}

func (r *readTransport) doOnce(req *http.Request) (*readResponse, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if err := r.acquire(req.Context()); err != nil {
		return nil, err
	}
	defer r.release()
	response, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return &readResponse{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After"), Body: body}, nil
}

func (r *readTransport) acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waiter := &readWaiter{granted: make(chan struct{}), ctx: ctx}
	r.mu.Lock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	r.queue = append(r.queue, waiter)
	if !r.scheduler {
		r.scheduler = true
		go r.runScheduler()
	}
	r.signalLocked()
	r.mu.Unlock()
	select {
	case <-waiter.granted:
		return nil
	case <-ctx.Done():
		r.signal()
		return ctx.Err()
	}
}
func (r *readTransport) release() {
	r.mu.Lock()
	if r.active > 0 {
		r.active--
	}
	r.signalLocked()
	r.mu.Unlock()
}
func (r *readTransport) signal() { r.mu.Lock(); r.signalLocked(); r.mu.Unlock() }
func (r *readTransport) signalLocked() {
	if r.wake != nil {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}
func (r *readTransport) runScheduler() {
	for {
		r.mu.Lock()
		for i := len(r.queue) - 1; i >= 0; i-- {
			select {
			case <-r.queue[i].ctx.Done():
				r.queue = append(r.queue[:i], r.queue[i+1:]...)
			default:
			}
		}
		limit := r.readConcurrency
		if limit < 1 {
			limit = 4
		}
		if r.active < limit && len(r.queue) > 0 {
			wait := time.Until(r.lastStart.Add(r.interval))
			if wait <= 0 && time.Now().After(r.readNotBefore) {
				waiter := r.queue[0]
				r.queue = r.queue[1:]
				r.active++
				r.lastStart = time.Now()
				close(waiter.granted)
				r.mu.Unlock()
				continue
			}
			wake := r.wake
			r.mu.Unlock()
			timer := time.NewTimer(maxDuration(wait, time.Until(r.readNotBefore)))
			select {
			case <-timer.C:
			case <-wake:
			}
			timer.Stop()
			continue
		}
		if len(r.queue) == 0 {
			r.scheduler = false
			r.mu.Unlock()
			return
		}
		wake := r.wake
		r.mu.Unlock()
		<-wake
	}
}
func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneValues(value url.Values) url.Values {
	result := make(url.Values, len(value))
	for key, values := range value {
		result[key] = append([]string(nil), values...)
	}
	return result
}
func parseCursor(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	page, err := strconv.Atoi(value)
	if err != nil || page < 0 {
		return 0, ErrInvalidCursor
	}
	return page, nil
}
func decodeEmbedded(data []byte, marker string, target any) error {
	_, after, ok := bytes.Cut(data, []byte(marker))
	if !ok {
		return errors.New("HH response marker not found")
	}
	decoder := json.NewDecoder(bytes.NewReader(after))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
