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
