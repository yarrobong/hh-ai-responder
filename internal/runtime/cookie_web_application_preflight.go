package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	hhwebadapter "hh-ai-responder/internal/adapters/hh/web"
	"hh-ai-responder/internal/hhwebsession"
)

type BrowserResume = hhwebadapter.BrowserResume

type ResumeMappingReader interface {
	ReadBrowserResumes(context.Context) ([]BrowserResume, error)
}

var (
	errCookieWebPreflightUnknown = errors.New("cookie web application preflight is unknown")
	errCookieWebPreflightBlocked = errors.New("cookie web application preflight is blocked")
)

type CookieWebApplicationPreflight struct {
	VacancyID                 int
	ResponseURL               string
	Resume                    BrowserResume
	ApprovedProviderResumeID  string
	ApprovedResumeHash        string
	VacancyPreflight          VacancyPreflight
	Authenticated             bool
	StandardResponsePathKnown bool
	PersistenceHealthy        bool
}

func (p CookieWebApplicationPreflight) ValidateForSend(letter string) error {
	if p.VacancyID <= 0 || !p.Authenticated {
		return errCookieWebPreflightUnknown
	}
	if !p.PersistenceHealthy {
		return errCookieWebPreflightBlocked
	}
	if !p.StandardResponsePathKnown || strings.TrimSpace(p.ResponseURL) == "" {
		return errCookieWebPreflightUnknown
	}
	if p.VacancyPreflight.ArchivedKnown && p.VacancyPreflight.Archived {
		return errCookieWebPreflightBlocked
	}
	if !p.VacancyPreflight.ArchivedKnown {
		return errCookieWebPreflightUnknown
	}
	if p.VacancyPreflight.ActiveState == VacancyActiveStateInactive {
		return errCookieWebPreflightBlocked
	}
	if p.VacancyPreflight.ActiveState != VacancyActiveStateActive {
		return errCookieWebPreflightUnknown
	}
	evidence := p.VacancyPreflight.alreadyRespondedEvidence()
	if evidence.Value == AlreadyRespondedYes {
		return errCookieWebPreflightBlocked
	}
	if evidence.Value != AlreadyRespondedNo || !p.VacancyPreflight.AlreadyRespondedKnown {
		return errCookieWebPreflightUnknown
	}
	if !p.VacancyPreflight.CanApplyKnown {
		return errCookieWebPreflightUnknown
	}
	if !p.VacancyPreflight.CanApply {
		return errCookieWebPreflightBlocked
	}
	if !p.VacancyPreflight.TestPresentKnown {
		return errCookieWebPreflightUnknown
	}
	if p.VacancyPreflight.TestPresent {
		return errCookieWebPreflightBlocked
	}
	if !p.VacancyPreflight.LetterRequiredKnown {
		return errCookieWebPreflightUnknown
	}
	if p.VacancyPreflight.LetterRequired && strings.TrimSpace(letter) == "" {
		return errCookieWebPreflightBlocked
	}
	approvedHash := strings.TrimSpace(p.ApprovedResumeHash)
	currentHash := strings.TrimSpace(p.Resume.BrowserHash)
	if approvedHash == "" || currentHash == "" {
		return errApprovedResumeNotAvailableInBrowserSession
	}
	if approvedHash != currentHash {
		return errBrowserResumeBindingStale
	}
	approvedProviderID, approvedProviderOK := normalizeProviderResumeID(p.ApprovedProviderResumeID)
	currentProviderID, currentProviderOK := normalizeProviderResumeID(p.Resume.ProviderID)
	if !approvedProviderOK || !currentProviderOK {
		return errApprovedResumeNotAvailableInBrowserSession
	}
	if approvedProviderID != currentProviderID {
		return errBrowserResumeBindingStale
	}
	return nil
}

type CookieWebApplicationPreflightService struct {
	client           hhwebsession.RequestDoer
	baseURL          *url.URL
	resumeMapping    ResumeMappingReader
	persistenceError func() error
}

func NewCookieWebApplicationPreflightService(client hhwebsession.RequestDoer, baseURL *url.URL, mapping ResumeMappingReader, persistenceError func() error) (*CookieWebApplicationPreflightService, error) {
	if client == nil || baseURL == nil || baseURL.Host == "" || mapping == nil {
		return nil, errors.New("cookie web preflight requires GET client, base URL, and resume mapping")
	}
	return &CookieWebApplicationPreflightService{client: client, baseURL: baseURL, resumeMapping: mapping, persistenceError: persistenceError}, nil
}

func (s *CookieWebApplicationPreflightService) Preflight(ctx context.Context, vacancyID int, approvedProviderResumeID, approvedResumeHash string) (CookieWebApplicationPreflight, error) {
	if s == nil || vacancyID <= 0 {
		return CookieWebApplicationPreflight{}, errors.New("cookie web preflight vacancy is invalid")
	}
	vacancyURL := s.baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("/vacancy/%d", vacancyID)})
	responseURL := s.baseURL.ResolveReference(&url.URL{Path: "/applicant/vacancy_response", RawQuery: url.Values{"vacancyId": {fmt.Sprint(vacancyID)}, "startedWithQuestion": {"false"}, "hhtmFrom": {"vacancy"}}.Encode()})
	vacancyPage, err := s.get(ctx, vacancyURL)
	if err != nil {
		return CookieWebApplicationPreflight{}, err
	}
	responsePage, err := s.get(ctx, responseURL)
	if err != nil {
		return CookieWebApplicationPreflight{}, err
	}
	authenticated := !webAuthFailure(vacancyPage) && !webAuthFailure(responsePage)
	preflight := VacancyPreflight{VacancyID: vacancyID, ResponseURL: responseURL.String()}
	if authenticated {
		parsed, parseErr := parseVacancyPreflight(responsePage.Body, Vacancy{ID: vacancyID}, responseURL.String())
		if parseErr != nil {
			return CookieWebApplicationPreflight{}, parseErr
		}
		// The response page is the authoritative vacancy-wide application
		// surface for cookie-web transport. An explicit responded/not-responded
		// marker is therefore a complete duplicate proof; no resume-specific
		// absence is inferred from the page.
		parsed.NegotiationScanComplete = parsed.alreadyRespondedEvidence().Value != AlreadyRespondedUnknown
		// This fixed endpoint is the standard applicant response path. The
		// generic parser marks any non-empty response URL as an identifier for
		// legacy API compatibility, so normalize that projection here rather
		// than treating the route itself as a direct/external response.
		parsed.ResponseIdentifierPresent = false
		parsed.VacancyTypeID, parsed.VacancyTypeKnown = "open", true
		preflight = parsed
		activeState, evidence := parseVacancyActiveState(vacancyPage.Body, vacancyID, webTraceVacancy)
		mergeVacancyActiveEvidence(&preflight, activeState, evidence)
	}
	resumes, err := s.resumeMapping.ReadBrowserResumes(ctx)
	if err != nil {
		return CookieWebApplicationPreflight{}, err
	}
	var selected BrowserResume
	for _, resume := range resumes {
		if strings.TrimSpace(resume.BrowserHash) == strings.TrimSpace(approvedResumeHash) {
			selected = resume
			break
		}
	}
	selectedHash := strings.TrimSpace(selected.BrowserHash)
	approvedHash := strings.TrimSpace(approvedResumeHash)
	preflight.SuitableResumesScanComplete = true
	preflight.SelectedResumeSuitableKnown = approvedHash != "" && selectedHash != ""
	preflight.SelectedResumeSuitable = preflight.SelectedResumeSuitableKnown && approvedHash == selectedHash
	persistenceHealthy := s.persistenceError == nil || s.persistenceError() == nil
	result := CookieWebApplicationPreflight{VacancyID: vacancyID, ResponseURL: responseURL.String(), Resume: selected, ApprovedProviderResumeID: approvedProviderResumeID, ApprovedResumeHash: approvedResumeHash, VacancyPreflight: preflight, Authenticated: authenticated, StandardResponsePathKnown: true, PersistenceHealthy: persistenceHealthy}
	return result, nil
}

type cookieWebPage struct {
	Status   int
	FinalURL *url.URL
	Body     []byte
}

func (s *CookieWebApplicationPreflightService) get(ctx context.Context, target *url.URL) (cookieWebPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return cookieWebPage{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return cookieWebPage{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return cookieWebPage{}, err
	}
	finalURL := target
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	return cookieWebPage{Status: response.StatusCode, FinalURL: finalURL, Body: body}, nil
}

func webAuthFailure(page cookieWebPage) bool {
	if page.Status == http.StatusUnauthorized || page.Status == http.StatusForbidden {
		return true
	}
	if page.FinalURL != nil {
		path := strings.ToLower(page.FinalURL.Path)
		if strings.Contains(path, "login") || strings.Contains(path, "captcha") || strings.Contains(path, "challenge") {
			return true
		}
	}
	lower := strings.ToLower(string(page.Body))
	return strings.Contains(lower, "captcha") || strings.Contains(lower, "challenge") || strings.Contains(lower, "/account/login")
}
