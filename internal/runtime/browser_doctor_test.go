package runtime

import (
	"net/url"
	"testing"
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
