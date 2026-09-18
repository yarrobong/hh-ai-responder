package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hh-ai-responder/internal/browsersession"
)

type browserTraceResult struct {
	BrowserFinalURL  string
	BrowserPageClass string
	BrowserChallenge bool
	BrowserAuth      bool
	HTTPFinalURL     string
	HTTPPageClass    string
	HTTPChallenge    bool
	HTTPAuth         bool
	HTTPTransport    string
	Conclusion       string
}

func runBrowserSessionCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("career-agent browser-session", flag.ContinueOnError)
	fs.SetOutput(stderr)
	refresh := false
	statusOnly := false
	fs.BoolVar(&refresh, "refresh", false, "wait for the user to finish login/CAPTCHA, then export the session")
	fs.BoolVar(&statusOnly, "status", false, "inspect the existing browser/profile without navigation or export")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || (refresh && statusOnly) {
		return errors.New("usage: career-agent browser-session [--refresh|--status]")
	}
	profile := strings.TrimSpace(cfg.BrowserProfilePath)
	if profile == "" {
		profile = filepath.Join(currentWorkingDir(), browsersession.DefaultProfileDir)
	}
	cookiePath := cfg.CookiesPath
	if cookiePath == "" {
		cookiePath = filepath.Join(currentWorkingDir(), "cookies.txt")
	}
	profileFound := pathExists(profile)
	if statusOnly && !profileFound {
		fmt.Fprintf(stdout, "Browser profile: not found\nBrowser auth: unknown\nChallenge: unknown\nCookies exported: 0\nCookie values logged: NO\nSTATUS: %s\n", browsersession.StatusLoginRequired)
		return nil
	}
	adapter, err := browsersession.NewChromeAdapter(context.Background(), browsersession.ChromeOptions{ProfileDir: profile, StartURL: firstNonEmpty(cfg.SearchURL, browsersession.DefaultHHURL), Launch: !statusOnly, CookiePath: cookiePath, Headless: cfg.BrowserHeadless})
	if err != nil {
		if statusOnly {
			fmt.Fprintf(stdout, "Browser profile: found\nBrowser auth: unknown\nChallenge: unknown\nCookies exported: 0\nCookie values logged: NO\nSTATUS: UNKNOWN\nBrowser status: not running or unavailable\n")
			return nil
		}
		return err
	}
	defer adapter.Close()
	session := browsersession.New(adapter, browsersession.Options{ProfileDir: profile, CookiePath: cookiePath, HHURL: firstNonEmpty(cfg.SearchURL, browsersession.DefaultHHURL), PollInterval: browsersession.DefaultPollInterval, WaitForReady: refresh, StatusOnly: statusOnly})
	report, err := session.Run(context.Background(), stdout)
	if err != nil {
		return err
	}
	report.ProfileDir = profile
	report.ProfileFound = profileFound || pathExists(profile)
	fmt.Fprintf(stdout, "Browser profile: %s\nBrowser auth: %s\nDetected state: %s\nChallenge: %s\nCookies exported: %d\nCookie validation: required_auth=%s no_local_expiry=%s\nCookie names: %s\nAuth cookie names: %s\nCookie values logged: NO\nSTATUS: %s\n", foundLabel(report.ProfileFound), firstNonEmpty(report.BrowserAuth, "unknown"), report.DetectedStatus, yesNo(report.Challenge), report.CookiesExported, yesNo(report.RequiredAuthPresent), yesNo(report.NoLocalExpiry), strings.Join(report.CookieNames, ","), strings.Join(report.AuthCookieNames, ","), report.Status)
	if report.Status != browsersession.StatusSessionReady || statusOnly {
		return nil
	}

	// Keep the existing HTTP read path and doctor diagnostic. The command has
	// already completed its only browser write-like concern (cookie refresh);
	// all following operations are bounded GETs.
	doctorOut := stdout
	if err := runHHDoctor(cfg, doctorOut, stderr); err != nil {
		fmt.Fprintf(stdout, "Doctor: FAIL (%s)\n", sanitizeBrowserError(err))
	} else {
		fmt.Fprintln(stdout, "Doctor: PASS")
	}
	trace, err := runOneBrowserHTTPTrace(context.Background(), cfg, adapter)
	if err != nil {
		fmt.Fprintf(stdout, "Vacancy browser read: NOT_RUN (%s)\nVacancy HTTP read: NOT_RUN\nTransport conclusion: BROWSER_READ_REQUIRED\n", sanitizeBrowserError(err))
		return nil
	}
	fmt.Fprintf(stdout, "Vacancy browser read: %s\nVacancy HTTP read: %s\nTransport conclusion: %s\n", passFail(!trace.BrowserChallenge && trace.BrowserPageClass == "normal_hh_page"), passFail(!trace.HTTPChallenge && trace.HTTPPageClass == "OK"), trace.Conclusion)
	fmt.Fprintf(stdout, "HTTP transport: %s\n", trace.HTTPTransport)
	fmt.Fprintln(stdout, "\nWeb trace comparison:")
	fmt.Fprintln(stdout, "                 Browser       Go HTTP")
	fmt.Fprintf(stdout, "Authenticated    %-13s %s\n", yesNo(trace.BrowserAuth), yesNo(trace.HTTPAuth))
	fmt.Fprintf(stdout, "Vacancy opens    %-13s %s\n", yesNo(!trace.BrowserChallenge && trace.BrowserPageClass == "normal_hh_page"), yesNo(!trace.HTTPChallenge && trace.HTTPPageClass == "OK"))
	fmt.Fprintf(stdout, "Captcha          %-13s %s\n", yesNo(trace.BrowserChallenge), yesNo(trace.HTTPChallenge))
	fmt.Fprintf(stdout, "Final path       %-13s %s\n", safeURLForOutput(trace.BrowserFinalURL), safeURLForOutput(trace.HTTPFinalURL))
	fmt.Fprintf(stdout, "Web trace: Browser final=%s page_class=%s challenge=%s; Go HTTP final=%s page_class=%s challenge=%s\n", safeURLForOutput(trace.BrowserFinalURL), trace.BrowserPageClass, yesNo(trace.BrowserChallenge), safeURLForOutput(trace.HTTPFinalURL), trace.HTTPPageClass, yesNo(trace.HTTPChallenge))
	return nil
}

func runOneBrowserHTTPTrace(ctx context.Context, cfg Config, adapter *browsersession.ChromeAdapter) (browserTraceResult, error) {
	traceURL := strings.TrimSpace(cfg.BrowserTraceVacancyURL)
	if traceURL == "" {
		if strings.TrimSpace(cfg.SearchURL) == "" {
			return browserTraceResult{}, errors.New("set HH_BROWSER_TRACE_VACANCY or HH_SEARCH_URL for the one-vacancy trace")
		}
		if err := adapter.Navigate(ctx, cfg.SearchURL); err != nil {
			return browserTraceResult{}, err
		}
		candidate, err := adapter.Evaluate(ctx, `(function(){var a=document.querySelector('a[href*="/vacancy/"]');return a?a.href:""})()`)
		if err != nil || strings.TrimSpace(candidate) == "" {
			return browserTraceResult{}, errors.New("no vacancy link found for the one-vacancy trace")
		}
		traceURL = candidate
	}
	parsed, err := url.Parse(traceURL)
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), "hh.ru") || !strings.HasPrefix(parsed.Path, "/vacancy/") {
		return browserTraceResult{}, errors.New("browser trace vacancy must be an HTTPS hh.ru vacancy URL")
	}
	if err := adapter.Navigate(ctx, parsed.String()); err != nil {
		return browserTraceResult{}, err
	}
	state, err := adapter.Inspect(ctx)
	if err != nil {
		return browserTraceResult{}, err
	}
	jar, err := NewMemoryPersistentJar(cfg.CookiesPath)
	if err != nil {
		return browserTraceResult{}, err
	}
	jar.persistPath = ""
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: &hhDiagnosticTransport{base: http.DefaultTransport}}
	response, err := doctorProbe(ctx, client, parsed, "one vacancy web trace", parsed.EscapedPath())
	if err != nil {
		return browserTraceResult{}, err
	}
	class, signals := classifyHHReadResponse(response, response.RequestCookieNames)
	challenge := class == HHAntiBotOrChallenge || strings.Contains(strings.ToLower(response.FinalURL), "/account/captcha") || hasSignal(signals, "explicit_challenge_marker")
	result := browserTraceResult{BrowserFinalURL: state.FinalURL, BrowserPageClass: state.PageClass, BrowserChallenge: state.Challenge || strings.Contains(strings.ToLower(state.FinalURL), "/account/captcha"), BrowserAuth: state.Authenticated, HTTPFinalURL: response.FinalURL, HTTPPageClass: class, HTTPChallenge: challenge, HTTPAuth: class == "OK", HTTPTransport: "HTTP_READ_FAILED"}
	if challenge {
		result.HTTPTransport = "HTTP_CLIENT_CHALLENGED"
	} else if class == "OK" {
		result.HTTPTransport = "HTTP_OK"
	}
	switch {
	case !result.BrowserChallenge && result.BrowserPageClass == "normal_hh_page" && !result.HTTPChallenge && class == "OK":
		result.Conclusion = "HTTP_OK"
	case !result.BrowserChallenge && result.HTTPChallenge:
		result.Conclusion = "BROWSER_READ_REQUIRED"
	default:
		result.Conclusion = "BROWSER_READ_REQUIRED"
	}
	return result, nil
}

func pathExists(path string) bool { _, err := os.Stat(path); return err == nil }
func currentWorkingDir() string {
	value, err := os.Getwd()
	if err != nil || value == "" {
		return "."
	}
	return value
}
func foundLabel(found bool) string {
	if found {
		return "found"
	}
	return "not found"
}
func sanitizeBrowserError(err error) string {
	if err == nil {
		return "unknown"
	}
	return strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\n", " "), "\r", " ")
}

func hasSignal(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
