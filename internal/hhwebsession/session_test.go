package hhwebsession

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionReadClientFollowsOnlyHHRedirectsAndPersistsSetCookie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Set-Cookie", "_xsrf=token; Path=/")
			http.Redirect(w, r, "/done", http.StatusFound)
		case "/done":
			if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Cookie"), "_xsrf=token") {
				http.Error(w, "missing cookie", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	session, err := New(path, Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	resp, err := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/start"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if token, err := session.XSRFToken(base); err != nil || token != "token" {
		t.Fatalf("xsrf=%q err=%v", token, err)
	}
	if mode := mustFileMode(t, path); mode != 0600 {
		t.Fatalf("mode=%#o", mode)
	}
}

func TestSessionRejectsExternalRedirectAndReadWriteMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/", http.StatusFound)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, err := New(filepath.Join(t.TempDir(), "cookies.txt"), Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadClient().Do(mustRequest(t, http.MethodPost, server.URL)); !errors.Is(err, ErrMethodNotAllowed) {
		t.Fatalf("POST read error=%v", err)
	}
	if _, err := session.WriteClient().Do(mustRequest(t, http.MethodGet, server.URL)); !errors.Is(err, ErrMethodNotAllowed) {
		t.Fatalf("GET write error=%v", err)
	}
	if _, err := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL)); !errors.Is(err, ErrExternalRedirect) {
		t.Fatalf("external redirect error=%v", err)
	}
	resp, err := session.WriteClient().Do(mustRequest(t, http.MethodPost, server.URL))
	if err != nil {
		t.Fatalf("write redirect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("write redirect status=%d", resp.StatusCode)
	}
}

func TestSessionSafeMetadataNeverContainsCookieValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	session, err := New(path, Options{AllowedHosts: []string{"hh.ru"}})
	if err != nil {
		t.Fatal(err)
	}
	session.jar.SetCookies(mustURL(t, "https://hh.ru/"), []*http.Cookie{{Name: "hhtoken", Value: "private", Path: "/"}})
	metadata := session.SafeMetadata(time.Now())
	text := fmt.Sprintf("%+v", metadata)
	if strings.Contains(text, "private") {
		t.Fatalf("metadata leaked value: %s", text)
	}
}

func mustRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func mustFileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
