package hhweb

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"hh-ai-responder/internal/hhwebsession"
)

func TestCookieWebReadClientUsesSessionCookiesAndGETOnlyCapability(t *testing.T) {
	var sawCookie bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			w.Header().Set("Set-Cookie", "auth=ok; Path=/")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/search/vacancy" {
			http.NotFound(w, r)
			return
		}
		sawCookie = strings.Contains(r.Header.Get("Cookie"), "auth=ok")
		w.Header().Set("Set-Cookie", "read_state=updated; Path=/")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body></body></html>`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	session, err := hhwebsession.New(filepath.Join(t.TempDir(), "cookies.txt"), hhwebsession.Options{BaseURL: base, AllowedHosts: []string{base.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	seedResponse, err := session.ReadClient().Do(mustRequest(t, http.MethodGet, server.URL+"/seed"))
	if err != nil {
		t.Fatalf("seed cookie: %v", err)
	}
	_ = seedResponse.Body.Close()
	client, err := NewCookieWebReadClient(session, base, nil)
	if err != nil {
		t.Fatalf("new read client: %v", err)
	}
	if _, err := client.ReadVacancies(t.Context(), ""); err != nil {
		t.Fatalf("read vacancies: %v", err)
	}
	if !sawCookie {
		t.Fatal("cookie session was not used")
	}
	if _, err := session.ReadClient().Do(mustRequest(t, http.MethodPost, server.URL)); err == nil {
		t.Fatal("read capability accepted POST")
	}
	if _, err := session.XSRFToken(base); err == nil {
		t.Fatal("missing XSRF unexpectedly succeeded")
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
