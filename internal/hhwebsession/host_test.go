package hhwebsession

import (
	"net/url"
	"testing"
)

func TestValidateHHWebBaseURLAcceptsOnlyHTTPSHHHosts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "root", raw: "https://hh.ru/search/vacancy", want: true},
		{name: "subdomain", raw: "https://spb.hh.ru/search/vacancy", want: true},
		{name: "http", raw: "http://hh.ru/search/vacancy"},
		{name: "foreign", raw: "https://example.com/search/vacancy"},
		{name: "lookalike suffix", raw: "https://hh.ru.example.com/search/vacancy"},
		{name: "lookalike name", raw: "https://evilhh.ru/search/vacancy"},
		{name: "userinfo", raw: "https://user@hh.ru/search/vacancy"},
		{name: "port", raw: "https://hh.ru:8443/search/vacancy"},
		{name: "malformed subdomain", raw: "https://foo..hh.ru/search/vacancy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateHHWebBaseURL(raw)
			if (err == nil) != tt.want {
				t.Fatalf("ValidateHHWebBaseURL(%q) error=%v, want allowed=%t", tt.raw, err, tt.want)
			}
		})
	}
}
