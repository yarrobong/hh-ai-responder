package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/browsersession"
	"hh-ai-responder/internal/hhwebsession"
)

func runBrowserDoctorCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("career-agent browser-doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	headless, headed := cfg.BrowserHeadless, false
	vacancyURL := strings.TrimSpace(cfg.BrowserTraceVacancyURL)
	fs.BoolVar(&headless, "headless", headless, "run the validation in headless mode")
	fs.BoolVar(&headed, "headed", false, "run the validation in headed mode")
	fs.StringVar(&vacancyURL, "vacancy-url", vacancyURL, "one explicit HTTPS hh.ru vacancy URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: career-agent browser-doctor [--headless|--headed] [--vacancy-url URL]")
	}
	headlessFlag, headedFlag := false, false
	fs.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "headless":
			headlessFlag = true
		case "headed":
			headedFlag = true
		}
	})
	if headlessFlag && headedFlag {
		return errors.New("usage: career-agent browser-doctor [--headless|--headed] [--vacancy-url URL]")
	}
	if headedFlag {
		headless = false
	}
	cookiePath := strings.TrimSpace(cfg.CookiesPath)
	if cookiePath == "" {
		cookiePath = "cookies.txt"
	}
	base := browserDoctorBaseURL(cfg.SearchURL)
	session, err := hhwebsession.New(cookiePath, hhwebsession.Options{BaseURL: base, AllowedHosts: []string{"hh.ru", base.Hostname()}, UserAgent: userAgent})
	if err != nil {
		return fmt.Errorf("AUTH_REQUIRED: replace cookies.txt with a fresh authenticated export and run again: %w", err)
	}
	metadata := session.SafeMetadata(time.Now())
	fmt.Fprintln(stdout, "HH Browser Doctor")
	fmt.Fprintf(stdout, "Browser transport: COOKIE_WEB\nMode: GET-only\nCookie count: %d\nCookie values logged: NO\n", len(metadata.Cookies))
	home, err := cookieDoctorGet(context.Background(), session, base)
	if err != nil {
		return reportCookieDoctorFailure(stdout, err)
	}
	resume, err := cookieDoctorGet(context.Background(), session, base.ResolveReference(&url.URL{Path: "/applicant/my_resumes"}))
	if err != nil {
		return reportCookieDoctorFailure(stdout, err)
	}
	if status := classifyCookieDoctorResponse(home); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}
	if status := classifyCookieDoctorResponse(resume); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}
	if cfg.HHWriteEnabled {
		if _, err := session.XSRFToken(base); err != nil {
			fmt.Fprintln(stdout, "XSRF: MISSING")
			return errors.New("AUTH_REQUIRED: authenticated cookie session has no XSRF cookie")
		}
		fmt.Fprintln(stdout, "XSRF: PRESENT")
	}

	if vacancyURL == "" {
		searchURL := base.ResolveReference(&url.URL{Path: "/search/vacancy", RawQuery: "items_on_page=1&search_period=1&order_by=publication_time"})
		search, searchErr := cookieDoctorGet(context.Background(), session, searchURL)
		if searchErr != nil {
			return reportCookieDoctorFailure(stdout, searchErr)
		}
		if classifyCookieDoctorResponse(search) != browsersession.DoctorAuthOK {
			return reportBrowserDoctorFailure(stdout, classifyCookieDoctorResponse(search))
		}
		fmt.Fprintln(stdout, "Vacancy page: NOT_CHECKED (provide --vacancy-url for a bounded vacancy probe)")
		fmt.Fprintln(stdout, "Overall: AUTH_OK")
		return nil
	}
	validatedVacancyURL, err := normalizeDoctorVacancyURL(vacancyURL, base)
	if err != nil {
		return err
	}
	vacancyState, err := cookieDoctorGet(context.Background(), session, mustParseDoctorURL(validatedVacancyURL))
	if err != nil {
		return reportCookieDoctorFailure(stdout, err)
	}
	if status := classifyCookieDoctorResponse(vacancyState); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}
	fmt.Fprintf(stdout, "Home: AUTH_OK\nMy resumes: AUTH_OK\nVacancy: AUTH_OK\nOverall: %s\n", browsersession.DoctorAuthOK)
	return nil
}

type cookieDoctorResponse struct {
	Status   int
	FinalURL string
	Body     []byte
}

func cookieDoctorGet(ctx context.Context, session *hhwebsession.Session, target *url.URL) (cookieDoctorResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return cookieDoctorResponse{}, err
	}
	response, err := session.ReadClient().Do(request)
	if err != nil {
		return cookieDoctorResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 512*1024))
	if err != nil {
		return cookieDoctorResponse{}, err
	}
	finalURL := target.String()
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	return cookieDoctorResponse{Status: response.StatusCode, FinalURL: finalURL, Body: body}, nil
}

func classifyCookieDoctorResponse(response cookieDoctorResponse) browsersession.DoctorStatus {
	finalURL := strings.ToLower(response.FinalURL)
	body := strings.ToLower(string(response.Body))
	if strings.Contains(finalURL, "/account/captcha") || strings.Contains(body, "captcha") || strings.Contains(body, "challenge") {
		return browsersession.DoctorChallenge
	}
	if strings.Contains(finalURL, "/account/login") || response.Status == http.StatusUnauthorized || response.Status == http.StatusForbidden && strings.Contains(body, "login") {
		return browsersession.DoctorSessionExpired
	}
	if response.Status < 200 || response.Status >= 400 {
		return browsersession.DoctorUnknown
	}
	return browsersession.DoctorAuthOK
}

func reportCookieDoctorFailure(stdout io.Writer, err error) error {
	if errors.Is(err, hhwebsession.ErrExternalRedirect) {
		fmt.Fprintln(stdout, "Overall: UNKNOWN")
		return errors.New("UNKNOWN: external redirect rejected")
	}
	return err
}

func mustParseDoctorURL(raw string) *url.URL {
	parsed, _ := url.Parse(raw)
	return parsed
}

func normalizeDoctorVacancyURL(raw string, base *url.URL) (string, error) {
	if base == nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("browser-doctor base URL is invalid")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("vacancy-url must be an HTTPS hh.ru vacancy URL")
	}
	if !parsed.IsAbs() {
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "https" || !isHHHost(parsed.Hostname()) || !strings.HasPrefix(parsed.Path, "/vacancy/") || strings.TrimPrefix(parsed.Path, "/vacancy/") == "" {
		return "", errors.New("vacancy-url must be an HTTPS hh.ru vacancy URL")
	}
	return parsed.String(), nil
}

func browserDoctorPageStatus(state browsersession.PageState, cookies []browsersession.Cookie) browsersession.DoctorStatus {
	if state.Challenge || strings.Contains(strings.ToLower(state.FinalURL), "/account/captcha") || strings.EqualFold(state.PageClass, "challenge") {
		return browsersession.DoctorChallenge
	}
	if strings.Contains(strings.ToLower(state.FinalURL), "/account/login") || strings.EqualFold(state.PageClass, "login") {
		if hasActiveAuthCookie(cookies) {
			return browsersession.DoctorSessionExpired
		}
		return browsersession.DoctorAuthRequired
	}
	if !state.Authenticated {
		return browsersession.DoctorUnknown
	}
	return browsersession.DoctorAuthOK
}

func reportBrowserDoctorFailure(stdout io.Writer, status browsersession.DoctorStatus) error {
	fmt.Fprintf(stdout, "Overall: %s\n", status)
	if status == browsersession.DoctorChallenge || status == browsersession.DoctorSessionExpired {
		fmt.Fprintln(stdout, "HH session requires refresh.")
		fmt.Fprintln(stdout, "Replace cookies.txt with a fresh authenticated export and run again.")
		return errors.New(string(browsersession.DoctorAuthRefreshRequired))
	}
	return errors.New(string(status))
}

func hasActiveAuthCookie(cookies []browsersession.Cookie) bool {
	for _, cookie := range cookies {
		switch strings.ToLower(cookie.Name) {
		case "hhtoken", "crypted_hhuid", "crypted_id", "hhuid", "hhrole", "hhul", "_xsrf":
			return true
		}
	}
	return false
}

func browserCookieNames(cookies []browsersession.Cookie) string {
	values := map[string]struct{}{}
	for _, cookie := range cookies {
		values[cookie.Name] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return strings.Join(result, ",")
}

func hhCookiesOnly(cookies []browsersession.Cookie) []browsersession.Cookie {
	result := make([]browsersession.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if browsersession.IsHHCookieDomain(cookie.Domain) {
			result = append(result, cookie)
		}
	}
	return result
}

func browserCookieDomains(cookies []browsersession.Cookie) string {
	values := map[string]struct{}{}
	for _, cookie := range cookies {
		values[cookie.Domain] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return strings.Join(result, ",")
}

func browserModeName(headless bool) string {
	if headless {
		return "headless"
	}
	return "headed"
}

func browserDoctorBaseURL(raw string) *url.URL {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && hhwebsession.ValidateHHWebBaseURL(parsed) == nil {
		return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	}
	return &url.URL{Scheme: "https", Host: "hh.ru"}
}

func browserDoctorRequestedFor(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && isHHHost(parsed.Hostname())
}

func joinDoctorURL(base *url.URL, path string) string {
	return base.ResolveReference(&url.URL{Path: path}).String()
}
