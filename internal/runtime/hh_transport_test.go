package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	"hh-ai-responder/internal/browsersession"
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

func TestLegacyBrowserProductionBaseURLUsesStrictHHBoundary(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "root", raw: "https://hh.ru", want: true},
		{name: "subdomain", raw: "https://ekaterinburg.hh.ru", want: true},
		{name: "http", raw: "http://hh.ru"},
		{name: "foreign", raw: "https://evil.example"},
		{name: "suffix trick", raw: "https://hh.ru.evil.example"},
		{name: "lookalike", raw: "https://evilhh.ru"},
		{name: "userinfo", raw: "https://user@hh.ru"},
		{name: "malformed", raw: "://hh.ru"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := url.Parse(tt.raw)
			if err != nil {
				if tt.want {
					t.Fatal(err)
				}
				return
			}
			err = validateLegacyBrowserProductionBaseURL(parsed)
			if (err == nil) != tt.want {
				t.Fatalf("validation error=%v, want allowed=%t", err, tt.want)
			}
		})
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
	if responder.userId != 77 || responder.resumeHash != "" || responder.resumeIdentifier != "42" || len(responder.resumes) != 1 {
		t.Fatalf("responder identity/resumes not populated: user=%d hash=%q identifier=%q resumes=%+v", responder.userId, responder.resumeHash, responder.resumeIdentifier, responder.resumes)
	}
	if got := responder.resumes[0]; got.Id != 42 || got.ProviderID != "42" || got.Hash != "resume-hash" || got.Title != "Python backend" || got.Skills != "Python, Django" || got.Area != "Екатеринбург" || got.Salary != "100000 RUR" {
		t.Fatalf("unexpected resume projection: %+v", got)
	}
	if responder.firstName != "" || responder.lastName != "" {
		t.Fatalf("API bootstrap invented profile identity: %q %q", responder.firstName, responder.lastName)
	}
}

func TestBootstrapAPIResumeUsesProviderIDWithoutBrowserHash(t *testing.T) {
	responder := &HHAIResponder{}
	source := &transportFakeResumeSource{
		resumes: []hhread.ResumeRecord{{ID: "api-resume-id-123", Title: "Backend developer", Experience: "36"}},
	}
	if err := bootstrapAPIResumeDataForUser(context.Background(), responder, source, "", hhapi.UserMetadata{ID: "77", AuthType: "applicant"}); err != nil {
		t.Fatal(err)
	}
	if len(source.readResumeIDs) != 1 || source.readResumeIDs[0] != "api-resume-id-123" {
		t.Fatalf("ReadResume IDs=%v, want [api-resume-id-123]", source.readResumeIDs)
	}
	if len(responder.resumes) != 1 || responder.resumes[0].Hash != "" {
		t.Fatalf("API bootstrap fabricated a browser hash: %+v", responder.resumes)
	}
}

func TestBootstrapAPIResumeRejectsMissingProviderID(t *testing.T) {
	responder := &HHAIResponder{}
	source := &transportFakeResumeSource{resumes: []hhread.ResumeRecord{{Title: "Backend developer"}}}
	err := bootstrapAPIResumeDataForUser(context.Background(), responder, source, "", hhapi.UserMetadata{AuthType: "applicant"})
	if err == nil || !strings.Contains(err.Error(), "API selected resume has no id") {
		t.Fatalf("error=%v, want precise missing API resume id error", err)
	}
}

func TestBootstrapAPIResumeSelectsConfiguredProviderIDAmongMultipleResumes(t *testing.T) {
	responder := &HHAIResponder{}
	source := &transportFakeResumeSource{resumes: []hhread.ResumeRecord{
		{ID: "api-resume-id-123", Title: "Support specialist"},
		{ID: "api-resume-id-456", Title: "Backend developer", Experience: "36"},
	}}
	if err := bootstrapAPIResumeDataForUser(context.Background(), responder, source, "api-resume-id-456", hhapi.UserMetadata{AuthType: "applicant"}); err != nil {
		t.Fatal(err)
	}
	if len(source.readResumeIDs) != 1 || source.readResumeIDs[0] != "api-resume-id-456" {
		t.Fatalf("ReadResume IDs=%v, want configured provider ID", source.readResumeIDs)
	}
}

func TestAPIResumeItemAcceptsOpaqueProviderIDWithoutHash(t *testing.T) {
	item, err := apiResumeItem(hhread.ResumeRecord{ID: "api-resume-id-123", Title: "Backend developer"})
	if err != nil {
		t.Fatal(err)
	}
	if item.ProviderID != "api-resume-id-123" || item.Hash != "" || item.Title != "Backend developer" {
		t.Fatalf("item=%+v, want empty browser hash and preserved title", item)
	}
}

func TestExplicitAPIReadPathsNeverCallBrowserOrLegacyRequester(t *testing.T) {
	baseURL, err := url.Parse("https://hh.ru")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func(*HHAIResponder) error
	}{
		{name: "vacancy preflight", call: func(r *HHAIResponder) error {
			_, err := r.getVacancyPreflightContext(context.Background(), Vacancy{ID: 42})
			return err
		}},
		{name: "vacancy tests", call: func(r *HHAIResponder) error {
			_, err := r.getVacancyTestsContext(context.Background(), "https://hh.ru/applicant/vacancy_response?vacancyId=42")
			return err
		}},
		{name: "resume facts", call: func(r *HHAIResponder) error {
			_, err := r.GetResumeFacts()
			return err
		}},
		{name: "web trace", call: func(r *HHAIResponder) error {
			_, err := r.traceVacancyWeb(context.Background(), 42)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := &transportBrowserSentinel{}
			var requesterCalls int
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requesterCalls++
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
			})}
			responder := &HHAIResponder{
				ctx:           context.Background(),
				baseURL:       baseURL,
				transport:     transportAPI,
				browserSource: browser,
				requester:     NewHHRequester(context.Background(), client, 0),
			}
			err := test.call(responder)
			if transportErrorCode(err) != transportNotImplemented {
				t.Fatalf("error code=%q err=%v", transportErrorCode(err), err)
			}
			if browser.calls != 0 || requesterCalls != 0 {
				t.Fatalf("API read used browser/requester: browser=%d requester=%d", browser.calls, requesterCalls)
			}
		})
	}
}

func TestBootstrapAPIResumeRejectsMissingConfiguredResume(t *testing.T) {
	source := &transportFakeResumeSource{resumes: []hhread.ResumeRecord{{ID: "42", Hash: "available-hash", Title: "Python backend"}}}
	responder := &HHAIResponder{}
	err := bootstrapAPIResumeData(context.Background(), responder, source, "missing-hash")
	if transportErrorCode(err) != "RESUME_NOT_FOUND" {
		t.Fatalf("error code=%q err=%v", transportErrorCode(err), err)
	}
	if len(responder.resumes) != 0 {
		t.Fatalf("missing configured resume silently selected a fallback: %+v", responder.resumes)
	}
}

type transportBrowserSentinel struct{ calls int }

func (s *transportBrowserSentinel) GetPage(context.Context, string) (browsersession.PageState, error) {
	s.calls++
	return browsersession.PageState{Authenticated: true, FinalURL: "https://hh.ru"}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type transportFakeResumeSource struct {
	currentUser   hhapi.UserMetadata
	resumes       []hhread.ResumeRecord
	readResumeIDs []string
}

func (f *transportFakeResumeSource) CurrentUser(context.Context) (hhapi.UserMetadata, error) {
	return f.currentUser, nil
}

func (f *transportFakeResumeSource) ReadResumes(context.Context) ([]hhread.ResumeRecord, error) {
	return f.resumes, nil
}

func (f *transportFakeResumeSource) ReadResume(_ context.Context, id string) (hhread.ResumeRecord, error) {
	f.readResumeIDs = append(f.readResumeIDs, id)
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
