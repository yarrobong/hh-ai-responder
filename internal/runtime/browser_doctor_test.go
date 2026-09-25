package runtime

import (
	"net/http"
	"net/url"
	"testing"

	"hh-ai-responder/internal/browsersession"
)

func TestNormalizeDoctorVacancyURLResolvesOnlyHHVacancies(t *testing.T) {
	base, err := url.Parse("https://ekaterinburg.hh.ru")
	if err != nil {
		t.Fatal(err)
	}

	got, err := normalizeDoctorVacancyURL("/vacancy/123?from=search", base)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://ekaterinburg.hh.ru/vacancy/123?from=search" {
		t.Fatalf("normalized URL = %q", got)
	}

	if _, err := normalizeDoctorVacancyURL("/search/vacancy?query=/vacancy/123", base); err == nil {
		t.Fatal("search URL was accepted as vacancy")
	}
}

func TestClassifyCookieDoctorResponseStates(t *testing.T) {
	cases := []struct {
		name   string
		input  cookieDoctorResponse
		status browsersession.DoctorStatus
	}{
		{name: "ok", input: cookieDoctorResponse{Status: http.StatusOK, FinalURL: "https://hh.ru/applicant/my_resumes"}, status: browsersession.DoctorAuthOK},
		{name: "login", input: cookieDoctorResponse{Status: http.StatusOK, FinalURL: "https://hh.ru/account/login"}, status: browsersession.DoctorSessionExpired},
		{name: "captcha", input: cookieDoctorResponse{Status: http.StatusForbidden, FinalURL: "https://hh.ru/applicant/my_resumes", Body: []byte("captcha")}, status: browsersession.DoctorChallenge},
		{name: "unknown", input: cookieDoctorResponse{Status: http.StatusInternalServerError, FinalURL: "https://hh.ru/applicant/my_resumes"}, status: browsersession.DoctorUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCookieDoctorResponse(tc.input); got != tc.status {
				t.Fatalf("status=%s, want %s", got, tc.status)
			}
		})
	}
}
