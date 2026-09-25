package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

type controlledTransportFixture struct {
	prepared     controlledApplicationContext
	prepareCalls int
}

func (f *controlledTransportFixture) Prepare(context.Context, APIApplicationApproval) (controlledApplicationContext, error) {
	f.prepareCalls++
	return f.prepared, nil
}
func (f *controlledTransportFixture) Writer() hhwrite.VacancyResponseWriter { return nil }
func (f *controlledTransportFixture) EvidenceReader() applicationreconciliation.EvidenceReader {
	return nil
}
func (f *controlledTransportFixture) PersistenceHealth() error { return nil }

func TestControlledApplicationTransportOwnsOnlyProviderPreparation(t *testing.T) {
	fixture := &controlledTransportFixture{prepared: controlledApplicationContext{ResumeID: "resume-1", ResponseURL: "https://hh.ru/applicant/vacancy_response?vacancyId=1"}}
	var transport controlledApplicationTransport = fixture
	value, err := transport.Prepare(context.Background(), APIApplicationApproval{VacancyID: 1})
	if err != nil || value.ResumeID != "resume-1" || fixture.prepareCalls != 1 {
		t.Fatalf("prepared=%+v calls=%d err=%v", value, fixture.prepareCalls, err)
	}
	if err := transport.PersistenceHealth(); err != nil {
		t.Fatal(err)
	}
	_ = errors.Is
	_ = time.Time{}
}

func TestBrowserControlledApplicationServiceDoesNotRequireOAuth(t *testing.T) {
	service, err := newControlledApplicationService(context.Background(), Config{HHTransport: "browser", DryRun: true, CookiesPath: filepath.Join(t.TempDir(), "cookies.txt"), HHOAuthTokenURL: "::invalid", HHAPITokenFile: filepath.Join(t.TempDir(), "absent-token")}, HHAPICommandDeps{}, 1)
	if err != nil {
		t.Fatalf("browser service construction failed without OAuth: %v", err)
	}
	if service == nil || service.client != nil || service.transport == nil {
		t.Fatalf("browser service composition=%+v", service)
	}
}

func TestBrowserControlledApplicationRejectsNonHHConfiguredBase(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "root", raw: "https://hh.ru/search/vacancy", want: true},
		{name: "subdomain", raw: "https://spb.hh.ru/search/vacancy", want: true},
		{name: "http", raw: "http://hh.ru/search/vacancy"},
		{name: "foreign", raw: "https://example.com/search/vacancy"},
		{name: "suffix trick", raw: "https://hh.ru.example.com/search/vacancy"},
		{name: "lookalike", raw: "https://evilhh.ru/search/vacancy"},
		{name: "userinfo", raw: "https://user@hh.ru/search/vacancy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := newControlledApplicationService(context.Background(), Config{
				HHTransport: "browser", SearchURL: tt.raw, DryRun: true,
				CookiesPath: filepath.Join(t.TempDir(), "cookies.txt"),
			}, HHAPICommandDeps{}, 1)
			if (err == nil) != tt.want {
				t.Fatalf("service=%+v err=%v, want allowed=%t", service, err, tt.want)
			}
		})
	}
}
