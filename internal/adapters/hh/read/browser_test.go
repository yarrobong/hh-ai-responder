package hhread

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"hh-ai-responder/internal/browsersession"
)

type browserSourceFake struct {
	urls  []string
	state browsersession.PageState
}

func (f *browserSourceFake) GetPage(_ context.Context, rawURL string) (browsersession.PageState, error) {
	f.urls = append(f.urls, rawURL)
	return f.state, nil
}

func TestBrowserHHClientUsesBoundedKnownPaths(t *testing.T) {
	base, err := url.Parse("https://hh.ru")
	if err != nil {
		t.Fatal(err)
	}
	source := &browserSourceFake{state: browsersession.PageState{FinalURL: "https://hh.ru/vacancy/42", PageClass: "normal_hh_page", Authenticated: true}}
	client, err := NewBrowserHHClient(source, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVacancy(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVacancyResponsePage(context.Background(), "response-7"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetMyResumes(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"/vacancy/42", "/applicant/negotiation/response-7", "/applicant/my_resumes"}
	if len(source.urls) != len(want) {
		t.Fatalf("urls=%v", source.urls)
	}
	for i, path := range want {
		if !strings.HasSuffix(source.urls[i], path) {
			t.Fatalf("url[%d]=%q, want suffix %q", i, source.urls[i], path)
		}
	}
}

func TestBrowserHHClientStopsOnChallenge(t *testing.T) {
	base, _ := url.Parse("https://hh.ru")
	source := &browserSourceFake{state: browsersession.PageState{FinalURL: "https://hh.ru/account/captcha", PageClass: "challenge", Challenge: true}}
	client, err := NewBrowserHHClient(source, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVacancy(context.Background(), 42); err == nil || !strings.Contains(err.Error(), "challenge") {
		t.Fatalf("err=%v, want challenge stop", err)
	}
}
