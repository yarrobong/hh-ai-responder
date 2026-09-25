package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyHH403LoginWithAuthCookieAsSessionExpired(t *testing.T) {
	response := HHResponse{
		Status:   http.StatusForbidden,
		URL:      mustURL(t, "https://hh.ru/applicant/my_resumes"),
		FinalURL: "https://perm.hh.ru/applicant/my_resumes",
		Headers:  map[string]string{"Content-Type": "text/html; charset=utf-8", "Server": "ddos-guard"},
		Body:     []byte(`<title>HeadHunter</title><div class="ForbiddenPage"><a href="/account/login?backurl=/applicant/my_resumes">Войти</a></div>`),
	}
	classification, signals := classifyHHReadResponse(response, []string{"hhtoken"})
	if classification != HHAuthSessionExpired || !containsDoctorString(signals, "ForbiddenPage") || !containsDoctorString(signals, "account_login_link") {
		t.Fatalf("classification=%s signals=%v", classification, signals)
	}
	if err := hhAccessError("authenticated profile read", response); strings.Contains(err.Error(), "ForbiddenPage") == false {
		t.Fatalf("diagnostic did not retain safe marker: %v", err)
	}
}

func TestClassifyLoginRedirectAndChallenge(t *testing.T) {
	login := HHResponse{
		Status:    http.StatusOK,
		URL:       mustURL(t, "https://hh.ru/applicant/my_resumes"),
		FinalURL:  "https://hh.ru/account/login?backurl=%2Fapplicant%2Fmy_resumes",
		Redirects: []HHRedirectHop{{Status: http.StatusFound, URL: "https://hh.ru/account/login?backurl=%2Fapplicant%2Fmy_resumes"}},
		Body:      []byte(`<form action="/account/login"><input name="login"></form>`),
	}
	classification, _ := classifyHHReadResponse(login, []string{"hhtoken"})
	if classification != HHAuthSessionExpired {
		t.Fatalf("login redirect classification=%s", classification)
	}

	challenge := HHResponse{Status: http.StatusForbidden, Headers: map[string]string{"Server": "provider"}, Body: []byte(`<div class="captcha-container">challenge</div>`)}
	classification, signals := classifyHHReadResponse(challenge, nil)
	if classification != HHAntiBotOrChallenge || !containsDoctorString(signals, "explicit_challenge_marker") {
		t.Fatalf("challenge classification=%s signals=%v", classification, signals)
	}
}

func TestExpiredCookieIsReportedAndNotAttached(t *testing.T) {
	base := mustURL(t, "https://hh.ru/applicant/my_resumes")
	jar := &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	jar.SetCookies(base, []*http.Cookie{{Name: "expired-auth", Value: "fixture", Domain: ".hh.ru", Path: "/", Expires: time.Now().Add(-time.Hour)}})
	summary := jar.safeSummary(time.Now(), base)
	if summary.Parsed != 1 || summary.Expired != 1 || len(summary.AppliedNames) != 0 {
		t.Fatalf("expired cookie summary=%+v", summary)
	}
}

func TestCookieFileParserJarAndRequestAttachCookie(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n%s\tTRUE\t/\tFALSE\t0\thhtoken\tfixture-auth-value\n", parsed.Hostname())
	if err := os.WriteFile(cookiePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	jar, err := NewMemoryPersistentJar(cookiePath)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Transport: &hhDiagnosticTransport{base: server.Client().Transport}}
	response, err := doctorProbe(context.Background(), client, parsed, "authenticated profile read", "/applicant/my_resumes")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusOK || !strings.Contains(received, "hhtoken=fixture-auth-value") || !containsDoctorString(response.RequestCookieNames, "hhtoken") {
		t.Fatalf("cookie was not attached: status=%d header_names=%v", response.Status, response.RequestCookieNames)
	}
}

func TestValidAuthenticatedGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/applicant/my_resumes" || r.Header.Get("Cookie") != "hhtoken=valid" {
			t.Fatalf("unexpected request method=%s path=%s cookie_present=%t", r.Method, r.URL.Path, r.Header.Get("Cookie") != "")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `{"redirectConfig":{}}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	jar := &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	jar.SetCookies(base, []*http.Cookie{{Name: "hhtoken", Value: "valid", Path: "/"}})
	client := &http.Client{Jar: jar, Transport: &hhDiagnosticTransport{base: server.Client().Transport}}
	response, err := doctorProbe(context.Background(), client, base, "authenticated profile read", "/applicant/my_resumes")
	if err != nil || response.Status != http.StatusOK {
		t.Fatalf("authenticated GET failed: response=%+v err=%v", response, err)
	}
	classification, _ := classifyHHReadResponse(response, response.RequestCookieNames)
	if classification != "OK" {
		t.Fatalf("valid GET classification=%s", classification)
	}
}

func TestHHAccessErrorDoesNotLeakResponseBody(t *testing.T) {
	response := HHResponse{Status: http.StatusForbidden, URL: mustURL(t, "https://hh.ru/applicant/my_resumes"), FinalURL: "https://hh.ru/applicant/my_resumes", Body: []byte("authorization=fixture-secret cookie=private-value")}
	err := hhAccessError("authenticated profile read", response)
	if strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "authorization") {
		t.Fatalf("secret/body data leaked: %v", err)
	}
}

func TestDoctorNeverWritesAndUsesGETOnly(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `{"redirectConfig":{}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	base, _ := url.Parse(server.URL)
	original := []byte(fmt.Sprintf("%s\tTRUE\t/\tFALSE\t0\thhtoken\tfixture\n", base.Hostname()))
	if err := os.WriteFile(cookiePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runHHDoctor(Config{CookiesPath: cookiePath, SearchURL: server.URL}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(cookiePath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("doctor changed cookie file: err=%v", err)
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("doctor issued non-GET method %s", method)
		}
	}
	if !strings.Contains(output.String(), "HH writes attempted: 0") || strings.Contains(output.String(), "fixture") {
		t.Fatalf("unsafe doctor output: %s", output.String())
	}
}

func TestCareerAgentRejectsNonHHProductionSearchURLBeforeRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("non-HH production URL was contacted: %s %s", r.Method, r.URL)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := runCareerAgentCommand([]string{"--shadow"}, Config{StorageBackend: "json", SearchURL: server.URL, CookiesPath: filepath.Join(t.TempDir(), "missing-cookies.txt"), RequestInterval: time.Millisecond, HHReadConcurrency: 1}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "allowed HH base URL") {
		t.Fatalf("career-agent did not reject non-HH production URL: err=%v", err)
	}
	if strings.Contains(stdout.String(), "career_agent") {
		t.Fatalf("career-agent emitted output after unsafe URL rejection: stdout=%q", stdout.String())
	}
}

func containsDoctorString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
