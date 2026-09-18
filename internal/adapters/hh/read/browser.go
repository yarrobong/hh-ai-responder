package hhread

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"hh-ai-responder/internal/browsersession"
	"hh-ai-responder/internal/hhread"
	hhreadports "hh-ai-responder/internal/ports/hhread"
)

// ErrBrowserChallenge is returned without retry when the headed browser is on
// /account/captcha or another known challenge page. The caller may ask the
// user to complete it and explicitly retry the current read once.
var ErrBrowserChallenge = errors.New("HH browser challenge requires user")

// BrowserPageSource is the read-only browser boundary. It deliberately has no
// click, form-submit, JavaScript mutation, or POST-shaped method.
type BrowserPageSource interface {
	GetPage(context.Context, string) (browsersession.PageState, error)
}

// BrowserHHReader implements the same narrow read interface as the HTTP HH
// adapter for web pages. It is selected only after an explicit HTTP challenge
// diagnosis; Career Agent business logic remains unchanged.
type BrowserHHReader struct {
	source       BrowserPageSource
	baseURL      *url.URL
	searchParams url.Values
}

// BrowserHHClient is the transport-facing name used by the architecture
// reset. BrowserHHReader is retained as the descriptive implementation name.
type BrowserHHClient = BrowserHHReader

func NewBrowserHHClient(source BrowserPageSource, baseURL *url.URL, searchParams url.Values) (*BrowserHHClient, error) {
	return NewBrowserHHReader(source, baseURL, searchParams)
}

func NewBrowserHHReader(source BrowserPageSource, baseURL *url.URL, searchParams url.Values) (*BrowserHHReader, error) {
	if source == nil {
		return nil, errors.New("browser page source is required")
	}
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("browser HH base URL is required")
	}
	return &BrowserHHReader{source: source, baseURL: cloneURL(baseURL), searchParams: cloneValues(searchParams)}, nil
}

var _ hhreadports.HHReadSource = (*BrowserHHReader)(nil)
var _ hhreadports.VacancyDetailSource = (*BrowserHHReader)(nil)

func (r *BrowserHHReader) GetVacancyPage(ctx context.Context, id int) (browsersession.PageState, error) {
	if id <= 0 {
		return browsersession.PageState{}, errors.New("HH vacancy ID is required")
	}
	return r.get(ctx, fmt.Sprintf("/vacancy/%d", id))
}

func (r *BrowserHHReader) GetVacancy(ctx context.Context, id int) (browsersession.PageState, error) {
	return r.GetVacancyPage(ctx, id)
}

func (r *BrowserHHReader) GetResponsePage(ctx context.Context, id string) (browsersession.PageState, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return browsersession.PageState{}, errors.New("HH response ID is required")
	}
	return r.get(ctx, "/applicant/negotiation/"+url.PathEscape(id))
}

func (r *BrowserHHReader) GetVacancyResponsePage(ctx context.Context, id string) (browsersession.PageState, error) {
	return r.GetResponsePage(ctx, id)
}

func (r *BrowserHHReader) GetNegotiationsPage(ctx context.Context) (browsersession.PageState, error) {
	return r.get(ctx, "/applicant/negotiations")
}

func (r *BrowserHHReader) GetNegotiations(ctx context.Context) (browsersession.PageState, error) {
	return r.GetNegotiationsPage(ctx)
}

func (r *BrowserHHReader) GetMyResumesPage(ctx context.Context) (browsersession.PageState, error) {
	return r.get(ctx, "/applicant/my_resumes")
}

func (r *BrowserHHReader) GetMyResumes(ctx context.Context) (browsersession.PageState, error) {
	return r.GetMyResumesPage(ctx)
}

func (r *BrowserHHReader) get(ctx context.Context, path string) (browsersession.PageState, error) {
	if r == nil || r.source == nil || r.baseURL == nil {
		return browsersession.PageState{}, errors.New("browser HH reader is not configured")
	}
	ref, err := url.Parse(path)
	if err != nil {
		return browsersession.PageState{}, err
	}
	state, err := r.source.GetPage(ctx, r.baseURL.ResolveReference(ref).String())
	if err != nil {
		return browsersession.PageState{}, err
	}
	if state.Challenge || strings.Contains(strings.ToLower(state.FinalURL), "/account/captcha") {
		return browsersession.PageState{}, fmt.Errorf("%w: current operation stopped", ErrBrowserChallenge)
	}
	return state, nil
}

func (r *BrowserHHReader) ReadVacancies(ctx context.Context, cursor string) (hhread.VacancyPage, error) {
	page := 0
	if strings.TrimSpace(cursor) != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return hhread.VacancyPage{}, ErrInvalidCursor
		}
		page = parsed
	}
	params := cloneValues(r.searchParams)
	params.Set("page", strconv.Itoa(page))
	state, err := r.get(ctx, "/search/vacancy?"+params.Encode())
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	values, err := parseVacancySearchHTML([]byte(state.HTML), r.baseURL)
	if err != nil {
		return hhread.VacancyPage{}, err
	}
	items := make([]hhread.VacancyRecord, 0, len(values))
	for _, value := range values {
		items = append(items, vacancyRecord(value, value.Description))
	}
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return hhread.VacancyPage{Items: items, NextCursor: next}, nil
}

func (r *BrowserHHReader) SearchVacancies(ctx context.Context, cursor string) (hhread.VacancyPage, error) {
	return r.ReadVacancies(ctx, cursor)
}

func (r *BrowserHHReader) ReadVacancyDetail(ctx context.Context, id int) (hhread.VacancyRecord, error) {
	state, err := r.GetVacancyPage(ctx, id)
	if err != nil {
		return hhread.VacancyRecord{}, err
	}
	return parseVacancyDetail([]byte(state.HTML), r.baseURL, id)
}

func (r *BrowserHHReader) ReadApplications(ctx context.Context, cursor string) (hhread.ApplicationPage, error) {
	state, err := r.GetNegotiationsPage(ctx)
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	items, next, _, err := parseNegotiations([]byte(state.HTML))
	if err != nil {
		return hhread.ApplicationPage{}, err
	}
	if strings.TrimSpace(cursor) != "" && next == cursor {
		next = ""
	}
	return hhread.ApplicationPage{Items: items, NextCursor: next}, nil
}

// Conversations currently require the Chatik JSON read model rather than a
// vacancy web page. Returning an explicit read-only error is safer than
// inventing a parser or silently switching to a write-capable path.
func (r *BrowserHHReader) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, errors.New("browser-backed HH conversations are not supported by this web-page reader")
}
