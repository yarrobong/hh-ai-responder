package hhwebsession

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPersistentJarMatchesHostOnlyDomainPathAndSecureCookies(t *testing.T) {
	jar := newPersistentJar(filepath.Join(t.TempDir(), "cookies.txt"), time.Unix(1_700_000_000, 0))
	base := mustURL(t, "https://hh.ru/")
	jar.SetCookies(base, []*http.Cookie{
		{Name: "host", Value: "1", Path: "/", Domain: ""},
		{Name: "sub", Value: "2", Path: "/", Domain: ".hh.ru"},
		{Name: "path", Value: "3", Path: "/app", Domain: ".hh.ru"},
		{Name: "secure", Value: "4", Path: "/", Domain: ".hh.ru", Secure: true},
	})

	if got := cookieNames(jar.Cookies(mustURL(t, "https://hh.ru/app/form"))); !strings.Contains(got, "host,sub,path,secure") {
		t.Fatalf("https://hh.ru/app/form cookies=%s", got)
	}
	if got := cookieNames(jar.Cookies(mustURL(t, "https://api.hh.ru/apply"))); got != "sub,secure" {
		t.Fatalf("https://api.hh.ru/apply cookies=%s", got)
	}
	if got := cookieNames(jar.Cookies(mustURL(t, "http://hh.ru/apply"))); got != "host,sub" {
		t.Fatalf("http://hh.ru/apply cookies=%s", got)
	}
	if got := cookieNames(jar.Cookies(mustURL(t, "https://hh.ru/other"))); got != "host,sub,secure" {
		t.Fatalf("https://hh.ru/other cookies=%s", got)
	}
}

func TestPersistentJarRejectsForeignSetCookieAndPersistsUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	jar := newPersistentJar(path, time.Unix(1_700_000_000, 0))
	base := mustURL(t, "https://hh.ru/")
	jar.SetCookies(base, []*http.Cookie{{Name: "ok", Value: "1", Path: "/"}})
	if err := jar.PersistenceError(); err != nil {
		t.Fatalf("initial persist: %v", err)
	}
	jar.SetCookies(base, []*http.Cookie{{Name: "ok", Value: "2", Path: "/"}, {Name: "deleted", Value: "x", Path: "/"}})
	jar.SetCookies(base, []*http.Cookie{{Name: "deleted", MaxAge: -1, Path: "/"}})
	jar.SetCookies(mustURL(t, "https://evil.example/"), []*http.Cookie{{Name: "evil", Value: "x", Path: "/"}})
	if got := cookieNames(jar.Cookies(base)); got != "ok" {
		t.Fatalf("cookies=%s", got)
	}
	if strings.Contains(string(mustRead(t, path)), "evil") {
		t.Fatal("foreign cookie persisted")
	}
}

func TestPersistentJarConcurrentAccessIsRaceSafe(t *testing.T) {
	jar := newPersistentJar("", time.Unix(1_700_000_000, 0))
	u := mustURL(t, "https://hh.ru/")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				jar.SetCookies(u, []*http.Cookie{{Name: "n", Value: string(rune('a' + i)), Path: "/"}})
				_ = jar.Cookies(u)
			}
		}(i)
	}
	wg.Wait()
}

func TestPersistentJarExpiresWithoutRewritingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	jar := newPersistentJar(path, time.Unix(1_700_000_000, 0))
	base := mustURL(t, "https://hh.ru/")
	jar.SetCookies(base, []*http.Cookie{{Name: "expired", Value: "x", Path: "/", Expires: time.Unix(1_699_999_999, 0)}})
	if got := len(jar.Cookies(base)); got != 0 {
		t.Fatalf("expired cookies=%d", got)
	}
}

func TestPersistentJarRejectsSetCookieDomainWideningAndPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	jar := newPersistentJar(path, time.Unix(1_700_000_000, 0))
	origin := mustURL(t, "https://api.hh.ru/applicant")
	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "old", Domain: "api.hh.ru", Path: "/"}})
	if err := jar.PersistenceError(); err != nil {
		t.Fatalf("initial persist: %v", err)
	}
	before := string(mustRead(t, path))

	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "parent", Domain: "hh.ru", Path: "/"}})
	if got := cookieValue(jar.Cookies(origin), "auth"); got != "old" {
		t.Fatalf("origin cookie=%q, want old", got)
	}
	if got := cookieValue(jar.Cookies(mustURL(t, "https://hh.ru/")), "auth"); got != "" {
		t.Fatalf("parent received widened cookie=%q", got)
	}
	if after := string(mustRead(t, path)); after != before {
		t.Fatalf("persisted cookie file changed after rejected widening")
	}
	if err := jar.PersistenceError(); err == nil {
		t.Fatal("unsafe Set-Cookie was not observable")
	}
}

func TestPersistentJarRejectsHostOnlyToDomainConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	jar := newPersistentJar(path, time.Unix(1_700_000_000, 0))
	origin := mustURL(t, "https://api.hh.ru/applicant")
	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "host-only", Path: "/"}})
	before := string(mustRead(t, path))

	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "wider", Domain: ".api.hh.ru", Path: "/"}})
	if cookies := jar.Cookies(mustURL(t, "https://api.hh.ru/")); len(cookies) != 1 || cookies[0].Value != "host-only" {
		t.Fatalf("host-only cookie changed: %+v", cookies)
	}
	if after := string(mustRead(t, path)); after != before {
		t.Fatalf("persisted cookie file changed after host-only widening")
	}
}

func TestPersistentJarAllowsSameScopeReplacementAndExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	jar := newPersistentJar(path, time.Unix(1_700_000_000, 0))
	origin := mustURL(t, "https://api.hh.ru/applicant")
	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "old", Domain: ".api.hh.ru", Path: "/"}})
	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "new", Domain: "api.hh.ru", Path: "/"}})
	if got := cookieValue(jar.Cookies(origin), "auth"); got != "new" {
		t.Fatalf("replacement value=%q, want new", got)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "auth", Value: "new", Domain: "api.hh.ru", Path: "/", Expires: time.Unix(1_700_000_100, 0)}})
	if cookies := jar.Cookies(origin); len(cookies) != 1 || !cookies[0].Expires.Equal(time.Unix(1_700_000_100, 0)) {
		t.Fatalf("same-scope expiry update=%+v", cookies)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func cookieNames(cookies []*http.Cookie) string {
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		names = append(names, cookie.Name)
	}
	return strings.Join(names, ",")
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
