package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	"hh-ai-responder/internal/hhread"
)

type transportFakeReadSource struct {
	currentUser hhapi.UserMetadata
	probeErr    error
	probes      int
}

func (f *transportFakeReadSource) CurrentUser(context.Context) (hhapi.UserMetadata, error) {
	f.probes++
	if f.probeErr != nil {
		return hhapi.UserMetadata{}, f.probeErr
	}
	return f.currentUser, nil
}

func (*transportFakeReadSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	return hhread.VacancyPage{}, nil
}

func (*transportFakeReadSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, nil
}

func (*transportFakeReadSource) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, nil
}

type transportFakeBrowserSource struct{}

func (*transportFakeBrowserSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	return hhread.VacancyPage{}, nil
}

func (*transportFakeBrowserSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, nil
}

func (*transportFakeBrowserSource) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, nil
}

func TestSelectHHTransportBrowserIsTheDefault(t *testing.T) {
	browser := &transportFakeBrowserSource{}
	selected, meta, err := selectHHReadSource(context.Background(), TransportOptions{Browser: browser})
	if err != nil {
		t.Fatal(err)
	}
	if selected != browser || meta.Selected != transportBrowser {
		t.Fatalf("selected=%T meta=%+v", selected, meta)
	}
}

func TestSelectHHTransportExplicitAPIMissingTokenDoesNotFallback(t *testing.T) {
	browser := &transportFakeBrowserSource{}
	doctorCalls := 0
	_, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAPI,
		Browser: browser,
		BrowserDoctor: func(context.Context) (string, error) {
			doctorCalls++
			return browserAuthOK, nil
		},
	})
	if err == nil {
		t.Fatal("explicit API mode unexpectedly succeeded without an API source")
	}
	if transportErrorCode(err) != transportAuthRequired {
		t.Fatalf("error code=%q err=%v", transportErrorCode(err), err)
	}
	if doctorCalls != 0 || meta.Selected != "" {
		t.Fatalf("explicit API used browser fallback: doctor=%d meta=%+v", doctorCalls, meta)
	}
}

func TestSelectHHTransportExplicitAPIProbeFailureDoesNotFallback(t *testing.T) {
	api := &transportFakeReadSource{probeErr: errors.New("probe failed")}
	browser := &transportFakeBrowserSource{}
	doctorCalls := 0
	_, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAPI,
		API:     api,
		Browser: browser,
		BrowserDoctor: func(context.Context) (string, error) {
			doctorCalls++
			return browserAuthOK, nil
		},
	})
	if err == nil {
		t.Fatal("explicit API mode unexpectedly ignored probe failure")
	}
	if doctorCalls != 0 || meta.Selected != "" {
		t.Fatalf("explicit API used browser fallback: doctor=%d meta=%+v", doctorCalls, meta)
	}
}

func TestSelectHHTransportAutoPrefersAPI(t *testing.T) {
	api := &transportFakeReadSource{currentUser: hhapi.UserMetadata{ID: "1", AuthType: "applicant"}}
	browser := &transportFakeBrowserSource{}
	selected, meta, err := selectHHReadSource(context.Background(), TransportOptions{Mode: transportAuto, API: api, Browser: browser})
	if err != nil {
		t.Fatal(err)
	}
	if selected != api || meta.Selected != transportAPI || api.probes != 1 {
		t.Fatalf("selected=%T probes=%d meta=%+v", selected, api.probes, meta)
	}
}

func TestSelectHHTransportAutoFallsBackAfterBrowserAuthOK(t *testing.T) {
	api := &transportFakeReadSource{probeErr: errors.New("token unavailable")}
	browser := &transportFakeBrowserSource{}
	selected, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAuto,
		API:     api,
		Browser: browser,
		BrowserDoctor: func(context.Context) (string, error) {
			return browserAuthOK, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != browser || meta.Selected != transportBrowser || meta.FallbackReason != fallbackAPIProbeFailed || meta.BrowserAuthStatus != browserAuthOK {
		t.Fatalf("selected=%T meta=%+v", selected, meta)
	}
}

func TestSelectHHTransportAutoFailsWhenBrowserUnavailable(t *testing.T) {
	api := &transportFakeReadSource{probeErr: errors.New("token unavailable")}
	browser := &transportFakeBrowserSource{}
	_, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAuto,
		API:     api,
		Browser: browser,
		BrowserDoctor: func(context.Context) (string, error) {
			return browserAuthRequired, errors.New("session unavailable")
		},
	})
	if err == nil {
		t.Fatal("auto mode unexpectedly used an unavailable browser")
	}
	if transportErrorCode(err) != transportBrowserUnavailable || meta.FallbackReason != fallbackAPIProbeFailed {
		t.Fatalf("error code=%q meta=%+v err=%v", transportErrorCode(err), meta, err)
	}
}

func TestSelectHHTransportFallbackMetadataIsSafe(t *testing.T) {
	api := &transportFakeReadSource{probeErr: errors.New("secret-token-value")}
	_, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAuto,
		API:     api,
		Browser: &transportFakeBrowserSource{},
		BrowserDoctor: func(context.Context) (string, error) {
			return browserAuthOK, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(meta.FallbackReason, "secret-token-value") || meta.FallbackReason != fallbackAPIProbeFailed {
		t.Fatalf("unsafe fallback metadata: %+v", meta)
	}
}

func TestAPITransportWriteGuardFailsClosed(t *testing.T) {
	if err := validateAPITransportWrites(false, true); err == nil || transportErrorCode(err) != transportWriteDisabled {
		t.Fatalf("write-enabled API mode was not blocked: %v", err)
	}
	if err := validateAPITransportWrites(false, false); err == nil || transportErrorCode(err) != transportWriteDisabled {
		t.Fatalf("live API mode was not blocked: %v", err)
	}
	if err := validateAPITransportWrites(true, false); err != nil {
		t.Fatalf("dry-run API mode was blocked: %v", err)
	}
}

func TestNewHHAIResponderAPIWriteGuardRunsBeforeComposition(t *testing.T) {
	for _, cfg := range []Config{
		{HHTransport: transportAPI, DryRun: false},
		{HHTransport: transportAPI, DryRun: true, HHWriteEnabled: true},
	} {
		if _, err := NewHHAIResponder(context.Background(), cfg); err == nil || transportErrorCode(err) != transportWriteDisabled {
			t.Fatalf("API write guard did not fail closed for cfg=%+v: %v", cfg, err)
		}
	}
}

func TestBootstrapAPIResumePopulatesExistingResponderInputs(t *testing.T) {
	responder := &HHAIResponder{}
	source := &transportFakeResumeSource{
		currentUser: hhapi.UserMetadata{ID: "77", AuthType: "applicant"},
		resumes:     []hhread.ResumeRecord{{ID: "42", Hash: "resume-hash", Title: "Python backend", Skills: []string{"Python", "Django"}, Area: "Екатеринбург", Salary: "100000", Currency: "RUR", Experience: "36"}},
	}
	if err := bootstrapAPIResumeData(context.Background(), responder, source, ""); err != nil {
		t.Fatal(err)
	}
	if responder.userId != 77 || responder.resumeHash != "resume-hash" || len(responder.resumes) != 1 {
		t.Fatalf("responder identity/resumes not populated: user=%d hash=%q resumes=%+v", responder.userId, responder.resumeHash, responder.resumes)
	}
	if got := responder.resumes[0]; got.Id != 42 || got.Title != "Python backend" || got.Skills != "Python, Django" || got.Area != "Екатеринбург" || got.Salary != "100000 RUR" {
		t.Fatalf("unexpected resume projection: %+v", got)
	}
	if responder.firstName != "" || responder.lastName != "" {
		t.Fatalf("API bootstrap invented profile identity: %q %q", responder.firstName, responder.lastName)
	}
}

type transportFakeResumeSource struct {
	currentUser hhapi.UserMetadata
	resumes     []hhread.ResumeRecord
}

func (f *transportFakeResumeSource) CurrentUser(context.Context) (hhapi.UserMetadata, error) {
	return f.currentUser, nil
}

func (f *transportFakeResumeSource) ReadResumes(context.Context) ([]hhread.ResumeRecord, error) {
	return f.resumes, nil
}

func (f *transportFakeResumeSource) ReadResume(_ context.Context, id string) (hhread.ResumeRecord, error) {
	for _, resume := range f.resumes {
		if resume.ID == id {
			return resume, nil
		}
	}
	return hhread.ResumeRecord{}, errors.New("resume not found")
}

func (*transportFakeResumeSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	return hhread.VacancyPage{}, nil
}

func (*transportFakeResumeSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, nil
}

func (*transportFakeResumeSource) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, nil
}
