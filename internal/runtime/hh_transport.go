package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	hhreadadapter "hh-ai-responder/internal/adapters/hh/read"
	"hh-ai-responder/internal/browsersession"
	"hh-ai-responder/internal/candidate"
	appconfig "hh-ai-responder/internal/config"
	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
)

const (
	transportBrowser = "BROWSER"
	transportAPI     = "API"
	transportAuto    = "auto"

	transportAuthRequired       = "AUTH_REQUIRED"
	transportBrowserUnavailable = "BROWSER_UNAVAILABLE"
	transportWriteDisabled      = "WRITE_DISABLED"
	transportNotImplemented     = "NOT_IMPLEMENTED"
	transportResumeNotFound     = "RESUME_NOT_FOUND"

	fallbackAPIProbeFailed  = "API_PROBE_FAILED"
	fallbackAPIAuthRequired = "API_AUTH_REQUIRED"
	fallbackAPITokenExpired = "API_TOKEN_EXPIRED"
	fallbackAPITokenRevoked = "API_TOKEN_REVOKED"
	fallbackAPIForbidden    = "API_FORBIDDEN"
	browserAuthOK           = "AUTH_OK"
	browserAuthRequired     = "AUTH_REQUIRED"
)

type TransportMetadata struct {
	Selected          string
	FallbackReason    string
	APIAuthStatus     string
	BrowserAuthStatus string
	APIUser           hhapi.UserMetadata
}

type TransportError struct {
	Code   string
	Reason string
	Cause  error
}

func (e *TransportError) Error() string {
	if e == nil {
		return "HH transport error"
	}
	message := e.Code
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	return "HH transport error: " + message
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type apiTransportSource interface {
	hhreadport.HHReadSource
	CurrentUser(context.Context) (hhapi.UserMetadata, error)
}

type apiResumeTransportSource interface {
	apiTransportSource
	hhreadport.ResumeReadSource
}

type TransportOptions struct {
	Mode          string
	API           apiTransportSource
	APISetupError error
	Browser       hhreadport.HHReadSource
	BrowserDoctor func(context.Context) (string, error)
}

func selectHHReadSource(ctx context.Context, options TransportOptions) (hhreadport.HHReadSource, TransportMetadata, error) {
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = "browser"
	}
	switch mode {
	case "browser":
		if options.Browser == nil {
			return nil, TransportMetadata{}, &TransportError{Code: transportBrowserUnavailable, Reason: "browser read source is unavailable"}
		}
		return options.Browser, TransportMetadata{Selected: transportBrowser}, nil
	case "api":
		if options.API == nil {
			return nil, TransportMetadata{}, &TransportError{Code: transportAuthRequired, Reason: "API authentication is unavailable", Cause: options.APISetupError}
		}
		user, err := options.API.CurrentUser(ctx)
		if err != nil {
			return nil, TransportMetadata{}, apiProbeTransportError(err)
		}
		return options.API, TransportMetadata{Selected: transportAPI, APIAuthStatus: browserAuthOK, APIUser: user}, nil
	case "auto":
		meta := TransportMetadata{}
		if options.API != nil {
			user, err := options.API.CurrentUser(ctx)
			if err == nil {
				return options.API, TransportMetadata{Selected: transportAPI, APIAuthStatus: browserAuthOK, APIUser: user}, nil
			}
			meta.APIAuthStatus = apiFailureStatus(err)
			meta.FallbackReason = apiFailureReason(err)
		} else {
			meta.APIAuthStatus = transportAuthRequired
			meta.FallbackReason = apiFailureReason(options.APISetupError)
		}
		if options.Browser == nil || options.BrowserDoctor == nil {
			return nil, meta, &TransportError{Code: transportBrowserUnavailable, Reason: "browser fallback is unavailable", Cause: options.APISetupError}
		}
		status, err := options.BrowserDoctor(ctx)
		meta.BrowserAuthStatus = strings.TrimSpace(status)
		if err != nil || status != browserAuthOK {
			return nil, meta, &TransportError{Code: transportBrowserUnavailable, Reason: "browser doctor did not report AUTH_OK", Cause: err}
		}
		meta.Selected = transportBrowser
		return options.Browser, meta, nil
	default:
		return nil, TransportMetadata{}, &TransportError{Code: transportNotImplemented, Reason: "unsupported HH transport mode"}
	}
}

func apiProbeTransportError(err error) error {
	code := transportAuthRequired
	if err != nil {
		var apiErr *hhapi.APIError
		if errors.As(err, &apiErr) && apiErr != nil && apiErr.Code != hhapi.APIErrorAuthRequired {
			code = string(apiErr.Code)
		}
	}
	return &TransportError{Code: code, Reason: "API /me probe failed", Cause: err}
}

func apiFailureStatus(err error) string {
	var apiErr *hhapi.APIError
	if errors.As(err, &apiErr) && apiErr != nil && apiErr.Code != "" {
		return string(apiErr.Code)
	}
	return fallbackAPIProbeFailed
}

func apiFailureReason(err error) string {
	var apiErr *hhapi.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		switch apiErr.Code {
		case hhapi.APIErrorAuthRequired:
			return fallbackAPIAuthRequired
		case hhapi.APIErrorTokenExpired:
			return fallbackAPITokenExpired
		case hhapi.APIErrorTokenRevoked:
			return fallbackAPITokenRevoked
		case hhapi.APIErrorForbidden:
			return fallbackAPIForbidden
		}
	}
	return fallbackAPIProbeFailed
}

func transportErrorCode(err error) string {
	var transportErr *TransportError
	if errors.As(err, &transportErr) && transportErr != nil {
		return transportErr.Code
	}
	return ""
}

func validateAPITransportWrites(dryRun, writeEnabled bool) error {
	if !dryRun || writeEnabled {
		return &TransportError{Code: transportWriteDisabled, Reason: "API transport is read-only"}
	}
	return nil
}

func newRuntimeAPIClient(cfg Config, httpClient *http.Client, searchParams url.Values) (*hhapi.APIHHClient, error) {
	baseURLText := strings.TrimSpace(cfg.HHAPIBaseURL)
	if baseURLText == "" {
		baseURLText = appconfig.DefaultHHAPIBaseURL
	}
	baseURL, err := url.Parse(baseURLText)
	if err != nil {
		return nil, fmt.Errorf("HH API base URL is invalid: %w", err)
	}
	if strings.TrimSpace(cfg.HHAPITokenFile) == "" {
		return nil, errors.New("HH API token file is required")
	}
	return hhapi.NewAPIHHClient(hhapi.APIClientOptions{
		BaseURL:    baseURL,
		HTTPClient: httpClient,
		TokenStore: hhapi.NewFileTokenStore(cfg.HHAPITokenFile),
		OAuthConfig: hhapi.OAuthConfig{
			TokenURL:     strings.TrimSpace(cfg.HHOAuthTokenURL),
			ClientID:     cfg.HHOAuthClientID,
			ClientSecret: cfg.HHOAuthClientSecret,
			RedirectURI:  cfg.HHOAuthRedirectURI,
			UserAgent:    cfg.HHOAuthUserAgent,
			HTTPClient:   httpClient,
		},
		UserAgent:    cfg.HHOAuthUserAgent,
		SearchParams: searchParams,
	})
}

func browserDoctorForSource(source browsersession.BrowserPageSource, baseURL *url.URL) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if source == nil || baseURL == nil {
			return browserAuthRequired, errors.New("browser session is unavailable")
		}
		state, err := source.GetPage(ctx, baseURL.ResolveReference(&url.URL{Path: "/applicant/my_resumes"}).String())
		if err != nil {
			return browserAuthRequired, err
		}
		finalURL := strings.ToLower(state.FinalURL)
		if state.Challenge || strings.Contains(finalURL, "/account/captcha") || strings.Contains(finalURL, "/account/login") || !state.Authenticated {
			return browserAuthRequired, errors.New("browser session is not authenticated")
		}
		return browserAuthOK, nil
	}
}

func bootstrapAPIResumeData(ctx context.Context, responder *HHAIResponder, source apiResumeTransportSource, selectedResumeID string) error {
	user, err := source.CurrentUser(ctx)
	if err != nil {
		return err
	}
	return bootstrapAPIResumeDataForUser(ctx, responder, source, selectedResumeID, user)
}

func bootstrapAPIResumeDataForUser(ctx context.Context, responder *HHAIResponder, source hhreadport.ResumeReadSource, selectedResumeID string, user hhapi.UserMetadata) error {
	if responder == nil || source == nil {
		return errors.New("API resume source is unavailable")
	}
	resumes, err := source.ReadResumes(ctx)
	if err != nil {
		return err
	}
	if len(resumes) == 0 {
		return errors.New("API resume list is empty")
	}
	selected := -1
	selectedResumeID = strings.TrimSpace(selectedResumeID)
	for i, resume := range resumes {
		if selectedResumeID != "" && (resume.ID == selectedResumeID || resume.Hash == selectedResumeID) {
			selected = i
			break
		}
	}
	if selected < 0 {
		if selectedResumeID != "" {
			return &TransportError{Code: transportResumeNotFound, Reason: "configured API resume is unavailable"}
		}
		selected = 0
	}
	detail, err := source.ReadResume(ctx, resumes[selected].ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(detail.Hash) == "" {
		return errors.New("API selected resume has no hash")
	}
	resumes[selected] = detail
	responder.resumes = make([]ResumeItem, 0, len(resumes))
	for _, resume := range resumes {
		item, err := apiResumeItem(resume)
		if err != nil {
			return err
		}
		responder.resumes = append(responder.resumes, item)
	}
	responder.userId, _ = strconv.ParseInt(strings.TrimSpace(user.ID), 10, 64)
	responder.resumeHash = detail.Hash
	responder.latestResumeHash = detail.Hash
	responder.resumeFacts = ResumeFacts{ExperienceText: detail.Experience, TotalExperienceMonths: detail.TotalExperienceMonths, TotalExperienceMonthsKnown: detail.TotalExperienceMonthsKnown}
	responder.resumeExperience = responder.resumeFacts.ExperienceText
	responder.resumeFactsByHash = map[string]ResumeFacts{detail.Hash: responder.resumeFacts}
	return nil
}

func apiResumeItem(value hhread.ResumeRecord) (candidate.ResumeItem, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value.ID), 10, 64)
	if err != nil || id <= 0 {
		return candidate.ResumeItem{}, errors.New("API resume has an invalid id")
	}
	salary := strings.TrimSpace(value.Salary)
	if salary != "" && strings.TrimSpace(value.Currency) != "" {
		salary += " " + strings.TrimSpace(value.Currency)
	}
	return candidate.ResumeItem{Id: id, Hash: strings.TrimSpace(value.Hash), Title: strings.TrimSpace(value.Title), Skills: strings.Join(value.Skills, ", "), Area: strings.TrimSpace(value.Area), Salary: salary}, nil
}

func newLegacyBrowserReadSource(responder *HHAIResponder) (hhreadport.HHReadSource, error) {
	if responder == nil {
		return nil, errors.New("HH responder is not configured")
	}
	return hhreadadapter.NewClient(hhreadadapter.Options{
		BaseURL: responder.baseURL, SearchParams: responder.searchParams, HTTPClient: responder.client,
		XSRFToken: responder.XSRFToken(), UserID: responder.userId,
	})
}
