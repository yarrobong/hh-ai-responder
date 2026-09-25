package hhwebsession

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseNetscapeCookiesPreservesSafeCookieSemantics(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	data := strings.Join([]string{
		"# Netscape HTTP Cookie File",
		"hh.ru\tFALSE\t/\tTRUE\t0\t_hh\t",
		".hh.ru\tTRUE\t/app\tFALSE\t1700000100\tst_medium\tsecret",
		"#HttpOnly_.hh.ru\tTRUE\t/\tTRUE\t1700000200\thhtoken\tprivate",
		".hh.ru\tTRUE\t/\tFALSE\t1699999999\told\tignored",
	}, "\n")

	cookies, err := parseNetscapeCookies([]byte(data), now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cookies) != 3 {
		t.Fatalf("cookies=%d, want 3", len(cookies))
	}
	if !cookies[0].hostOnly || cookies[0].Value != "" {
		t.Fatalf("host-only empty cookie=%+v", cookies[0])
	}
	if cookies[1].hostOnly || cookies[1].Path != "/app" || cookies[1].Expires.IsZero() {
		t.Fatalf("domain cookie=%+v", cookies[1])
	}
	if !cookies[2].HttpOnly || cookies[2].Name != "hhtoken" {
		t.Fatalf("httponly cookie=%+v", cookies[2])
	}
}

func TestParseNetscapeCookiesRejectsMalformedOrForeignRows(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []string{
		"hh.ru\tFALSE\t/\tTRUE\tbad\ta\tb",
		"hh.ru\tFALSE\t/\tTRUE\t0\t\tb",
		"hh.ru\tFALSE\t\tTRUE\t0\ta\tb",
		"evil.example\tTRUE\t/\tTRUE\t0\ta\tb",
		".not-hh.ru\tTRUE\t/\tTRUE\t0\ta\tb",
		"hh.ru\tMAYBE\t/\tTRUE\t0\ta\tb",
	}
	for _, row := range cases {
		if _, err := parseNetscapeCookies([]byte(row+"\n"), now); err == nil {
			t.Errorf("row %q parsed successfully", row)
		}
	}
}

func TestSerializeNetscapeCookiesPreservesHostOnlyAndHttpOnly(t *testing.T) {
	cookies := []storedCookie{
		{Cookie: http.Cookie{Name: "host", Value: "", Domain: "hh.ru", Path: "/", Secure: true}, hostOnly: true},
		{Cookie: http.Cookie{Name: "token", Value: "v", Domain: ".hh.ru", Path: "/", HttpOnly: true}, hostOnly: false},
	}
	data, err := serializeNetscapeCookies(cookies)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "hh.ru\tFALSE") {
		t.Fatalf("host-only row lost: %q", text)
	}
	if !strings.Contains(text, "#HttpOnly_.hh.ru\tTRUE") {
		t.Fatalf("httponly row lost: %q", text)
	}
}
