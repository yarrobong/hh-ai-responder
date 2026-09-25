package runtime

import (
	"errors"
	"testing"
)

func TestCookieWebApplicationPreflightFailsClosed(t *testing.T) {
	base := CookieWebApplicationPreflight{
		VacancyID:                42,
		ResponseURL:              "https://hh.ru/applicant/vacancy_response?vacancyId=42",
		Resume:                   BrowserResume{BrowserHash: "browser-hash-42", ProviderID: "42", HHID: 42},
		ApprovedResumeHash:       "browser-hash-42",
		ApprovedProviderResumeID: "42",
		VacancyPreflight: VacancyPreflight{
			VacancyID: 42, ArchivedKnown: true, ActiveState: VacancyActiveStateActive, CanApplyKnown: true, CanApply: true,
			AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded},
			TestPresentKnown: true, LetterRequiredKnown: true,
		},
		Authenticated: true, StandardResponsePathKnown: true, PersistenceHealthy: true,
	}
	if err := base.ValidateForSend("letter"); err != nil {
		t.Fatalf("known-safe preflight rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*CookieWebApplicationPreflight)
		want   error
	}{
		{"unknown active", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.ArchivedKnown = false }, errCookieWebPreflightUnknown},
		{"duplicate", func(p *CookieWebApplicationPreflight) {
			p.VacancyPreflight.AlreadyRespondedEvidence.Value = AlreadyRespondedYes
		}, errCookieWebPreflightBlocked},
		{"test", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.TestPresent = true }, errCookieWebPreflightBlocked},
		{"letter unknown", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.LetterRequiredKnown = false }, errCookieWebPreflightUnknown},
		{"empty required letter", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.LetterRequired = true }, errCookieWebPreflightBlocked},
		{"resume mismatch", func(p *CookieWebApplicationPreflight) { p.Resume.BrowserHash = "other" }, errBrowserResumeBindingStale},
		{"browser hash missing", func(p *CookieWebApplicationPreflight) { p.Resume.BrowserHash = "" }, errApprovedResumeNotAvailableInBrowserSession},
		{"approved provider missing", func(p *CookieWebApplicationPreflight) { p.ApprovedProviderResumeID = "" }, errApprovedResumeNotAvailableInBrowserSession},
		{"provider mismatch", func(p *CookieWebApplicationPreflight) { p.Resume.ProviderID = "99" }, errBrowserResumeBindingStale},
		{"provider missing", func(p *CookieWebApplicationPreflight) { p.Resume.ProviderID = "" }, errApprovedResumeNotAvailableInBrowserSession},
		{"active unknown", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.ActiveState = VacancyActiveStateUnknown }, errCookieWebPreflightUnknown},
		{"inactive", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.ActiveState = VacancyActiveStateInactive }, errCookieWebPreflightBlocked},
		{"archived", func(p *CookieWebApplicationPreflight) { p.VacancyPreflight.Archived = true }, errCookieWebPreflightBlocked},
		{"auth", func(p *CookieWebApplicationPreflight) { p.Authenticated = false }, errCookieWebPreflightUnknown},
		{"path", func(p *CookieWebApplicationPreflight) { p.StandardResponsePathKnown = false }, errCookieWebPreflightUnknown},
		{"persistence", func(p *CookieWebApplicationPreflight) { p.PersistenceHealthy = false }, errCookieWebPreflightBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.mutate(&value)
			letter := "letter"
			if tc.name == "empty required letter" {
				letter = ""
			}
			if err := value.ValidateForSend(letter); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
}
