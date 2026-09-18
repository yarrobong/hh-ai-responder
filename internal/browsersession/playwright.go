package browsersession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

// PlaywrightOptions controls a temporary browser context. Headless is
// explicit so validation can compare headed and headless behavior without
// adding fingerprint or challenge-bypass behavior.
type PlaywrightOptions struct {
	CookiePath string
	Headless   bool
	BrowserBin string
	HHURL      string
}

type PlaywrightAdapter struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page
}

const maxPageHTMLBytes = 4 * 1024 * 1024

// ChromeAdapter and ChromeOptions are compatibility names for the previous
// browser command. They now use Playwright internally.
type ChromeAdapter = PlaywrightAdapter

type ChromeOptions struct {
	ProfileDir string
	StartURL   string
	Launch     bool
	CookiePath string
	Headless   bool
}

func NewChromeAdapter(ctx context.Context, options ChromeOptions) (*ChromeAdapter, error) {
	return NewPlaywrightAdapter(ctx, PlaywrightOptions{CookiePath: options.CookiePath, Headless: options.Headless, HHURL: options.StartURL})
}

var _ Adapter = (*PlaywrightAdapter)(nil)
var _ BrowserPageSource = (*PlaywrightAdapter)(nil)

func NewPlaywrightAdapter(_ context.Context, options PlaywrightOptions) (*PlaywrightAdapter, error) {
	if strings.TrimSpace(options.CookiePath) == "" {
		return nil, errors.New("cookies.txt path is required")
	}
	now := time.Now()
	cookies, err := LoadNetscapeCookies(options.CookiePath, now)
	if err != nil {
		return nil, fmt.Errorf("load cookies.txt: %w", err)
	}
	if strings.TrimSpace(options.HHURL) == "" {
		options.HHURL = DefaultHHURL
	}
	runOptions := &playwright.RunOptions{SkipInstallBrowsers: true, Stdout: io.Discard, Stderr: io.Discard}
	pw, err := playwright.Run(runOptions)
	if err != nil {
		// The driver is a local Playwright runtime dependency, not a browser
		// bypass. Install only that driver; the browser executable remains the
		// installed Chrome/Chromium selected below.
		if installErr := playwright.Install(runOptions); installErr == nil {
			pw, err = playwright.Run(runOptions)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("start Playwright driver: %w", err)
	}
	launch := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(options.Headless)}
	if binary := firstNonEmpty(options.BrowserBin, os.Getenv("HH_BROWSER_BIN"), findInstalledChrome()); binary != "" {
		launch.ExecutablePath = playwright.String(binary)
	} else {
		launch.Channel = playwright.String("chrome")
	}
	browser, err := pw.Chromium.Launch(launch)
	if err != nil {
		_ = pw.Stop()
		return nil, fmt.Errorf("launch Chromium/Chrome through Playwright: %w", err)
	}
	browserContext, err := browser.NewContext()
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	if err := browserContext.AddCookies(toPlaywrightCookies(cookies)); err != nil {
		_ = browserContext.Close()
		_ = browser.Close()
		_ = pw.Stop()
		return nil, fmt.Errorf("apply cookies.txt to browser context: %w", err)
	}
	page, err := browserContext.NewPage()
	if err != nil {
		_ = browserContext.Close()
		_ = browser.Close()
		_ = pw.Stop()
		return nil, fmt.Errorf("create browser page: %w", err)
	}
	return &PlaywrightAdapter{pw: pw, browser: browser, context: browserContext, page: page}, nil
}

func (a *PlaywrightAdapter) Navigate(_ context.Context, rawURL string) error {
	if a == nil || a.page == nil {
		return errors.New("Playwright page is not configured")
	}
	if strings.TrimSpace(rawURL) == "" {
		return errors.New("browser navigation URL is empty")
	}
	_, err := a.page.Goto(rawURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(30000)})
	if err != nil {
		return err
	}
	return nil
}

func (a *PlaywrightAdapter) Inspect(_ context.Context) (PageState, error) {
	if a == nil || a.page == nil {
		return PageState{}, errors.New("Playwright page is not configured")
	}
	body, err := a.page.TextContent("body")
	if err != nil {
		body = ""
	}
	html, err := a.page.Content()
	if err != nil {
		html = ""
	}
	html = boundedPageHTML(html)
	title, _ := a.page.Title()
	pageURL := a.page.URL()
	combined := strings.ToLower(pageURL + "\n" + title + "\n" + body + "\n" + html)
	challenge := isCaptchaURL(pageURL) || strings.Contains(combined, "captcha-container") || strings.Contains(combined, "cf-chl-") || strings.Contains(combined, "cloudflare challenge") || strings.Contains(combined, "ddos-guard challenge")
	login := isLoginURL(pageURL) || strings.Contains(combined, "supernova-login-wrapper") || strings.Contains(combined, "forbiddenpage")
	pageClass := "normal_hh_page"
	if challenge {
		pageClass = "challenge"
	} else if login {
		pageClass = "login"
	} else if !strings.Contains(strings.ToLower(pageURL), "hh.ru") {
		pageClass = "external_or_unknown"
	}
	return PageState{FinalURL: pageURL, PageClass: pageClass, Challenge: challenge, Authenticated: !login && !challenge && strings.Contains(strings.ToLower(pageURL), "hh.ru"), HTML: html}, nil
}

func boundedPageHTML(html string) string {
	if len(html) > maxPageHTMLBytes {
		return html[:maxPageHTMLBytes]
	}
	return html
}

func (a *PlaywrightAdapter) Evaluate(_ context.Context, expression string) (string, error) {
	if a == nil || a.page == nil {
		return "", errors.New("Playwright page is not configured")
	}
	value, err := a.page.Evaluate(expression, nil)
	if err != nil {
		return "", err
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *PlaywrightAdapter) Cookies(_ context.Context) ([]Cookie, error) {
	if a == nil || a.context == nil {
		return nil, errors.New("Playwright browser context is not configured")
	}
	values, err := a.context.Cookies()
	if err != nil {
		return nil, err
	}
	result := make([]Cookie, 0, len(values))
	for _, value := range values {
		cookie := Cookie{Name: value.Name, Value: value.Value, Domain: value.Domain, Path: value.Path, Secure: value.Secure, HTTPOnly: value.HttpOnly, Session: value.Expires == 0}
		if value.Expires > 0 {
			cookie.Expires = time.Unix(int64(value.Expires), 0)
		}
		result = append(result, cookie)
	}
	return result, nil
}

func (a *PlaywrightAdapter) GetPage(ctx context.Context, rawURL string) (PageState, error) {
	if err := a.Navigate(ctx, rawURL); err != nil {
		return PageState{}, err
	}
	return a.Inspect(ctx)
}

func (a *PlaywrightAdapter) Close() error {
	if a == nil {
		return nil
	}
	var first error
	if a.context != nil {
		if err := a.context.Close(); err != nil && first == nil {
			first = err
		}
	}
	if a.browser != nil {
		if err := a.browser.Close(); err != nil && first == nil {
			first = err
		}
	}
	if a.pw != nil {
		if err := a.pw.Stop(); err != nil && first == nil {
			first = err
		}
	}
	a.context, a.browser, a.pw = nil, nil, nil
	return first
}

func toPlaywrightCookies(values []Cookie) []playwright.OptionalCookie {
	result := make([]playwright.OptionalCookie, 0, len(values))
	for _, value := range values {
		if !IsHHCookieDomain(value.Domain) {
			continue
		}
		cookie := playwright.OptionalCookie{Name: value.Name, Value: value.Value, Domain: playwright.String(value.Domain), Path: playwright.String(value.Path), Secure: playwright.Bool(value.Secure), HttpOnly: playwright.Bool(value.HTTPOnly)}
		if !value.Expires.IsZero() {
			expires := float64(value.Expires.Unix())
			cookie.Expires = &expires
		}
		result = append(result, cookie)
	}
	return result
}

func IsHHCookieDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return domain == "hh.ru" || strings.HasSuffix(domain, ".hh.ru")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func findInstalledChrome() string {
	candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}
	if runtime.GOOS == "darwin" {
		candidates = append([]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"}, candidates...)
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			continue
		}
		if value, err := exec.LookPath(candidate); err == nil {
			return value
		}
	}
	return ""
}
