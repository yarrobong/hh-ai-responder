package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/browsersession"
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
	cookies, err := browsersession.LoadNetscapeCookies(cookiePath, time.Now())
	if err != nil {
		return fmt.Errorf("AUTH_REQUIRED: replace cookies.txt with a fresh authenticated export and run again: %w", err)
	}
	cookies = hhCookiesOnly(cookies)
	if len(cookies) == 0 {
		return errors.New("AUTH_REQUIRED: cookies.txt contains no active hh.ru cookies; replace cookies.txt and run again")
	}
	base := browserDoctorBaseURL(cfg.SearchURL)
	adapter, err := browsersession.NewPlaywrightAdapter(context.Background(), browsersession.PlaywrightOptions{CookiePath: cookiePath, Headless: headless, HHURL: base.String()})
	if err != nil {
		return err
	}
	defer adapter.Close()

	fmt.Fprintln(stdout, "HH Browser Doctor")
	fmt.Fprintf(stdout, "Browser transport: PLAYWRIGHT\nMode: %s\nCookie count: %d\nCookie domains: %s\nCookie names: %s\nCookie values logged: NO\n", browserModeName(headless), len(cookies), browserCookieDomains(cookies), browserCookieNames(cookies))

	homeState, err := adapter.GetPage(context.Background(), base.String())
	if err != nil {
		return err
	}
	resumeState, err := adapter.GetPage(context.Background(), joinDoctorURL(base, "/applicant/my_resumes"))
	if err != nil {
		return err
	}
	if status := browserDoctorPageStatus(homeState, cookies); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}
	if status := browserDoctorPageStatus(resumeState, cookies); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}

	if vacancyURL == "" {
		searchURL := base.ResolveReference(&url.URL{Path: "/search/vacancy", RawQuery: "items_on_page=1&search_period=1&order_by=publication_time"}).String()
		searchState, searchErr := adapter.GetPage(context.Background(), searchURL)
		if searchErr != nil {
			return searchErr
		}
		if status := browserDoctorPageStatus(searchState, cookies); status != browsersession.DoctorAuthOK {
			return reportBrowserDoctorFailure(stdout, status)
		}
		vacancyURL, err = adapter.Evaluate(context.Background(), `(function(){for (const a of document.querySelectorAll('a[href]')) { const raw=a.getAttribute('href')||""; try { const u=new URL(raw, location.href); if (u.protocol === "https:" && /(^|\.)hh\.ru$/i.test(u.hostname) && /^\/vacancy\/[^/?#]+/.test(u.pathname)) return u.href; } catch (_) {} } return ""})()`)
		if err != nil || strings.TrimSpace(vacancyURL) == "" {
			fmt.Fprintln(stdout, "Vacancy page: UNKNOWN")
			fmt.Fprintln(stdout, "Overall: UNKNOWN")
			return errors.New("UNKNOWN: no bounded vacancy link found on search page")
		}
	}
	validatedVacancyURL, err := normalizeDoctorVacancyURL(vacancyURL, base)
	if err != nil {
		return err
	}
	vacancyState, err := adapter.GetPage(context.Background(), validatedVacancyURL)
	if err != nil {
		return err
	}
	if status := browserDoctorPageStatus(vacancyState, cookies); status != browsersession.DoctorAuthOK {
		return reportBrowserDoctorFailure(stdout, status)
	}
	fmt.Fprintf(stdout, "Home: AUTH_OK\nMy resumes: AUTH_OK\nVacancy: AUTH_OK\nOverall: %s\n", browsersession.DoctorAuthOK)
	return nil
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
	if err == nil && isHHHost(parsed.Hostname()) {
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
