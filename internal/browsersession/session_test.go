package browsersession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAdapter struct {
	states      []PageState
	inspects    int
	navigations []string
	cookies     []Cookie
}

func (f *fakeAdapter) Navigate(_ context.Context, rawURL string) error {
	f.navigations = append(f.navigations, rawURL)
	return nil
}
func (f *fakeAdapter) Inspect(context.Context) (PageState, error) {
	index := f.inspects
	if index >= len(f.states) {
		index = len(f.states) - 1
	}
	f.inspects++
	return f.states[index], nil
}
func (f *fakeAdapter) Evaluate(context.Context, string) (string, error) { return "", nil }
func (f *fakeAdapter) Cookies(context.Context) ([]Cookie, error)        { return f.cookies, nil }

func TestLoginRequiredStopsForUserAndNeverExports(t *testing.T) {
	fake := &fakeAdapter{states: []PageState{{FinalURL: "https://hh.ru/account/login", PageClass: "login"}}}
	report, err := New(fake, Options{CookiePath: filepath.Join(t.TempDir(), "cookies.txt")}).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUserActionRequired || report.DetectedStatus != StatusLoginRequired || !report.ManualAction {
		t.Fatalf("report=%+v", report)
	}
	if len(fake.navigations) != 1 || len(fake.cookies) != 0 {
		t.Fatalf("navigation/cookies=%v/%v", fake.navigations, fake.cookies)
	}
}

func TestCaptchaIsNeverAutomaticallySolved(t *testing.T) {
	fake := &fakeAdapter{states: []PageState{{FinalURL: "https://hh.ru/account/captcha", PageClass: "challenge", Challenge: true}}}
	report, err := New(fake, Options{CookiePath: filepath.Join(t.TempDir(), "cookies.txt")}).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUserActionRequired || report.DetectedStatus != StatusCaptchaRequired || !report.ManualAction {
		t.Fatalf("report=%+v", report)
	}
	if len(fake.navigations) != 1 {
		t.Fatalf("captcha flow navigated %v times", len(fake.navigations))
	}
}

func TestReadyExportsCookiesAndDoesNotLogValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	fake := &fakeAdapter{
		states:  []PageState{{FinalURL: "https://hh.ru/", PageClass: "normal_hh_page", Authenticated: true}, {FinalURL: "https://hh.ru/applicant/my_resumes", PageClass: "normal_hh_page", Authenticated: true}},
		cookies: []Cookie{{Name: "hhtoken", Value: "private-secret", Domain: ".hh.ru", Path: "/", Secure: true}, {Name: "_xsrf", Value: "private-xsrf", Domain: ".hh.ru", Path: "/"}},
	}
	report, err := New(fake, Options{CookiePath: path, Now: func() time.Time { return time.Unix(1000, 0) }}).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusSessionReady || report.CookiesExported != 2 || !report.CookiesValidated || !report.RequiredAuthPresent || !report.NoLocalExpiry || report.CookieValuesLogged {
		t.Fatalf("report=%+v", report)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hhtoken") || !strings.Contains(string(raw), "private-secret") {
		t.Fatal("cookie file was not exported")
	}
	if strings.Contains(strings.Join(report.CookieNames, "\n"), "private") {
		t.Fatal("cookie value leaked to report")
	}
}

func TestStatusOnlyDoesNotNavigateOrExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	fake := &fakeAdapter{states: []PageState{{FinalURL: "https://hh.ru/", Authenticated: true, PageClass: "normal_hh_page"}}}
	report, err := New(fake, Options{CookiePath: path, StatusOnly: true}).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusLoggedIn || len(fake.navigations) != 0 || report.CookiesValidated {
		t.Fatalf("report=%+v navigations=%v", report, fake.navigations)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("status-only created cookie file: err=%v", err)
	}
}

func TestRefreshWaitsForManualReady(t *testing.T) {
	fake := &fakeAdapter{states: []PageState{{FinalURL: "https://hh.ru/account/captcha", Challenge: true}, {FinalURL: "https://hh.ru/account/captcha", Challenge: true}, {FinalURL: "https://hh.ru/applicant/my_resumes", Authenticated: true, PageClass: "normal_hh_page"}}, cookies: []Cookie{{Name: "hhtoken", Value: "v", Domain: ".hh.ru"}}}
	report, err := New(fake, Options{CookiePath: filepath.Join(t.TempDir(), "cookies.txt"), WaitForReady: true, PollInterval: time.Millisecond}).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusSessionReady || fake.inspects < 3 {
		t.Fatalf("report=%+v inspects=%d", report, fake.inspects)
	}
}

func TestExpiredSessionDoesNotOverwriteValidCookies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	original := []byte("# Netscape HTTP Cookie File\n.hh.ru\tTRUE\t/\tTRUE\t0\thhtoken\told-valid\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := ExportCookiesAtomic(path, []Cookie{{Name: "hhtoken", Value: "expired", Domain: ".hh.ru", Expires: time.Unix(99, 0)}}, time.Unix(100, 0))
	if err == nil {
		t.Fatal("expired browser session was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("valid cookie file was overwritten")
	}
}

func TestLoadNetscapeCookiesFiltersExpiredRowsAndKeepsValuesOutOfMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	contents := "# Netscape HTTP Cookie File\n.hh.ru\tTRUE\t/\tTRUE\t0\thhtoken\tsecret-value\n.hh.ru\tTRUE\t/\tTRUE\t99\told\ttoo-old\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadNetscapeCookies(path, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 || cookies[0].Name != "hhtoken" || cookies[0].Value != "secret-value" {
		t.Fatalf("cookies=%+v", cookies)
	}
	names, auth := safeCookieMetadata(cookies, time.Unix(100, 0))
	if strings.Join(names, ",") != "hhtoken" || strings.Join(auth, ",") != "hhtoken" {
		t.Fatalf("metadata names=%v auth=%v", names, auth)
	}
}

func TestLoadNetscapeCookiesAcceptsEmptyCookieValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	contents := ".hh.ru\tTRUE\t/\tTRUE\t0\tst_medium\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadNetscapeCookies(path, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 || cookies[0].Name != "st_medium" || cookies[0].Value != "" {
		t.Fatalf("cookies=%+v", cookies)
	}
}

func TestBoundedPageHTMLKeepsLargeResumeBootstrap(t *testing.T) {
	input := strings.Repeat("x", 200001) + `{"redirectConfig":{}}`
	got := boundedPageHTML(input)
	if !strings.Contains(got, `{"redirectConfig":{}}`) {
		t.Fatal("large page bootstrap was truncated")
	}
}

func TestLoadNetscapeCookiesFailsClosedOnMalformedRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(".hh.ru\tTRUE\t/\tTRUE\tnot-a-number\thhtoken\tvalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNetscapeCookies(path, time.Unix(100, 0)); err == nil {
		t.Fatal("malformed cookie expiry was accepted")
	}
}
