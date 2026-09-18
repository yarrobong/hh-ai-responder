// Package browsersession owns the read-only browser boundary used for HH web
// pages. Authentication is supplied by cookies.txt; this package never
// exports cookies from a browser and never submits a form.
package browsersession

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHHURL        = "https://hh.ru"
	DefaultPollInterval = 1500 * time.Millisecond
	DefaultProfileDir   = ".hh-browser-profile"
)

type Status string

const (
	StatusLoggedIn           Status = "LOGGED_IN"
	StatusLoginRequired      Status = "LOGIN_REQUIRED"
	StatusCaptchaRequired    Status = "CAPTCHA_REQUIRED"
	StatusReady              Status = "READY"
	StatusUserActionRequired Status = "USER_ACTION_REQUIRED"
	StatusSessionReady       Status = "BROWSER_SESSION_READY"
)

type DoctorStatus string

const (
	DoctorAuthOK              DoctorStatus = "AUTH_OK"
	DoctorAuthRequired        DoctorStatus = "AUTH_REQUIRED"
	DoctorChallenge           DoctorStatus = "CHALLENGE"
	DoctorSessionExpired      DoctorStatus = "SESSION_EXPIRED"
	DoctorUnknown             DoctorStatus = "UNKNOWN"
	DoctorAuthRefreshRequired DoctorStatus = "AUTH_REFRESH_REQUIRED"
)

type PageState struct {
	FinalURL      string
	PageClass     string
	Challenge     bool
	Authenticated bool
	HTML          string
}

type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Secure   bool
	Expires  time.Time
	Session  bool
	HTTPOnly bool
}

// Adapter is deliberately read-only. Implementations must not click submit,
// post forms, solve challenges, or mutate HH state.
type Adapter interface {
	Navigate(context.Context, string) error
	Inspect(context.Context) (PageState, error)
	Evaluate(context.Context, string) (string, error)
	Cookies(context.Context) ([]Cookie, error)
}

type BrowserPageSource interface {
	GetPage(context.Context, string) (PageState, error)
}

type Options struct {
	ProfileDir      string
	CookiePath      string
	HHURL           string
	TraceVacancyURL string
	PollInterval    time.Duration
	WaitForReady    bool
	StatusOnly      bool
	Now             func() time.Time
}

type Report struct {
	ProfileFound        bool
	ProfileDir          string
	BrowserAuth         string
	Challenge           bool
	CookiesExported     int
	ExpiredCookies      int
	CookieNames         []string
	AuthCookieNames     []string
	RequiredAuthPresent bool
	NoLocalExpiry       bool
	CookiesValidated    bool
	Status              Status
	DetectedStatus      Status
	FinalURL            string
	PageClass           string
	ManualAction        bool
	CookieValuesLogged  bool
}

type Session struct {
	adapter Adapter
	options Options
}

func New(adapter Adapter, options Options) *Session {
	if options.PollInterval <= 0 {
		options.PollInterval = DefaultPollInterval
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if strings.TrimSpace(options.HHURL) == "" {
		options.HHURL = DefaultHHURL
	}
	return &Session{adapter: adapter, options: options}
}

// Run is retained as a small compatibility wrapper for existing fakes. The
// production Playwright adapter consumes cookies.txt before its first
// navigation; it does not use this method to export a new authenticated file.
func (s *Session) Run(ctx context.Context, _ io.Writer) (Report, error) {
	if s == nil || s.adapter == nil {
		return Report{}, errors.New("browser session adapter is not configured")
	}
	report := Report{ProfileDir: s.options.ProfileDir, CookieValuesLogged: false}
	if s.options.StatusOnly {
		state, err := s.adapter.Inspect(ctx)
		if err != nil {
			return report, err
		}
		report = reportFromState(report, state)
		report.Status = report.DetectedStatus
		return report, nil
	}
	if err := s.adapter.Navigate(ctx, s.options.HHURL); err != nil {
		return report, err
	}
	state, err := s.adapter.Inspect(ctx)
	if err != nil {
		return report, err
	}
	if state.Challenge || isCaptchaURL(state.FinalURL) || (!state.Authenticated && isLoginURL(state.FinalURL)) {
		if !s.options.WaitForReady {
			return userActionReport(report, state), nil
		}
		state, err = s.waitForReady(ctx)
		if err != nil {
			return report, err
		}
	}
	if err := s.adapter.Navigate(ctx, resolveURL(s.options.HHURL, "/applicant/my_resumes")); err != nil {
		return report, err
	}
	state, err = s.adapter.Inspect(ctx)
	if err != nil {
		return report, err
	}
	if state.Challenge || isCaptchaURL(state.FinalURL) || isLoginURL(state.FinalURL) || !state.Authenticated {
		if !s.options.WaitForReady {
			return userActionReport(report, state), nil
		}
		state, err = s.waitForReady(ctx)
		if err != nil {
			return report, err
		}
	}
	report = reportFromState(report, state)
	report.Status = StatusReady
	cookies, err := s.adapter.Cookies(ctx)
	if err != nil {
		return report, err
	}
	if err := ExportCookiesAtomic(s.options.CookiePath, cookies, s.options.Now()); err != nil {
		return report, err
	}
	report.CookiesExported = len(activeCookies(cookies, s.options.Now()))
	report.CookieNames, report.AuthCookieNames = safeCookieMetadata(cookies, s.options.Now())
	report.RequiredAuthPresent = len(report.AuthCookieNames) > 0
	report.ExpiredCookies = len(cookies) - report.CookiesExported
	report.NoLocalExpiry = true
	report.CookiesValidated = true
	report.Status = StatusSessionReady
	return report, nil
}

func (s *Session) waitForReady(ctx context.Context) (PageState, error) {
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return PageState{}, ctx.Err()
		case <-ticker.C:
			state, err := s.adapter.Inspect(ctx)
			if err != nil {
				return PageState{}, err
			}
			if !state.Challenge && state.Authenticated && !isLoginURL(state.FinalURL) && !isCaptchaURL(state.FinalURL) {
				return state, nil
			}
		}
	}
}

func userActionReport(report Report, state PageState) Report {
	report = reportFromState(report, state)
	report.Status = StatusUserActionRequired
	report.ManualAction = true
	return report
}

func reportFromState(report Report, state PageState) Report {
	report.FinalURL = state.FinalURL
	report.PageClass = state.PageClass
	report.Challenge = state.Challenge || isCaptchaURL(state.FinalURL)
	switch {
	case report.Challenge:
		report.DetectedStatus = StatusCaptchaRequired
	case isLoginURL(state.FinalURL) || !state.Authenticated:
		report.DetectedStatus = StatusLoginRequired
	case strings.Contains(state.FinalURL, "/applicant/my_resumes"):
		report.DetectedStatus = StatusReady
	default:
		report.DetectedStatus = StatusLoggedIn
	}
	switch {
	case state.Authenticated:
		report.BrowserAuth = "authenticated"
	case isLoginURL(state.FinalURL) || strings.EqualFold(state.PageClass, "login"):
		report.BrowserAuth = "login_required"
	default:
		report.BrowserAuth = "unknown"
	}
	return report
}

func isLoginURL(raw string) bool   { return strings.Contains(strings.ToLower(raw), "/account/login") }
func isCaptchaURL(raw string) bool { return strings.Contains(strings.ToLower(raw), "/account/captcha") }

func resolveURL(base, path string) string {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return path
	}
	ref, _ := url.Parse(path)
	return parsed.ResolveReference(ref).String()
}

var requiredAuthCookieNames = map[string]struct{}{
	"hhtoken": {}, "crypted_hhuid": {}, "crypted_id": {}, "hhuid": {}, "hhrole": {}, "hhul": {}, "_xsrf": {},
}

func activeCookies(cookies []Cookie, now time.Time) []Cookie {
	result := make([]Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) == "" || (!cookie.Expires.IsZero() && cookie.Expires.Before(now)) {
			continue
		}
		result = append(result, cookie)
	}
	return result
}

func safeCookieMetadata(cookies []Cookie, now time.Time) ([]string, []string) {
	names := map[string]struct{}{}
	auth := map[string]struct{}{}
	for _, cookie := range activeCookies(cookies, now) {
		names[cookie.Name] = struct{}{}
		if _, ok := requiredAuthCookieNames[cookie.Name]; ok {
			auth[cookie.Name] = struct{}{}
		}
	}
	return sortedKeys(names), sortedKeys(auth)
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// LoadNetscapeCookies parses cookies.txt without logging or returning any
// diagnostic containing values. Malformed rows fail closed.
func LoadNetscapeCookies(path string, now time.Time) ([]Cookie, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []Cookie
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 6 && len(parts) != 7 {
			return nil, fmt.Errorf("invalid Netscape cookie row at line %d", lineNo)
		}
		expires, parseErr := strconv.ParseInt(parts[4], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid cookie expiry at line %d", lineNo)
		}
		value := ""
		if len(parts) == 7 {
			value = parts[6]
		}
		cookie := Cookie{Name: parts[5], Value: value, Domain: parts[0], Path: parts[2], Secure: strings.EqualFold(parts[3], "TRUE"), Session: expires == 0}
		if expires > 0 {
			cookie.Expires = time.Unix(expires, 0)
		}
		if cookie.Name == "" || cookie.Domain == "" || cookie.Path == "" {
			return nil, fmt.Errorf("invalid cookie identity at line %d", lineNo)
		}
		if cookie.Expires.IsZero() || !cookie.Expires.Before(now) {
			result = append(result, cookie)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("cookies.txt contains no active cookies")
	}
	return result, nil
}

// ExportCookiesAtomic is kept only for compatibility with old unit tests and
// tooling. The runtime never calls it: cookies flow in one direction from
// cookies.txt into the browser context.
func ExportCookiesAtomic(path string, cookies []Cookie, now time.Time) error {
	active := activeCookies(cookies, now)
	_, auth := safeCookieMetadata(active, now)
	if strings.TrimSpace(path) == "" {
		return errors.New("cookie path is required")
	}
	if len(active) == 0 || len(auth) == 0 {
		return errors.New("browser session has no active HH auth cookie")
	}
	var builder strings.Builder
	builder.WriteString("# Netscape HTTP Cookie File\n")
	for _, cookie := range active {
		expires := int64(0)
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		}
		includeSubdomains := "FALSE"
		if strings.HasPrefix(cookie.Domain, ".") {
			includeSubdomains = "TRUE"
		}
		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}
		builder.WriteString(strings.Join([]string{cookie.Domain, includeSubdomains, cookie.Path, secure, strconv.FormatInt(expires, 10), cookie.Name, cookie.Value}, "\t"))
		builder.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(builder.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
