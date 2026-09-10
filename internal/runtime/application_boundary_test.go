package runtime

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestApplicationBoundary_VacancyValueStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), JobApplicationsFilename)
	vacancy := Vacancy{ID: 42, ExternalID: "hh-vacancy-42", Name: "Python/Django", Company: Company{Name: "Fixture Company"}}
	created := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	application := JobApplication{
		ID: "application-fixed", VacancyID: vacancy.ID, ExternalID: "hh-negotiation-42",
		CompanyName: vacancy.Company.Name, VacancyTitle: vacancy.Name, Source: ApplicationSourceHH,
		CreatedAt: created, UpdatedAt: created, Status: ApplicationApplied,
		MatchResult: &MatchResult{Score: 82, Confidence: 0.75, MatchedSkills: []string{"Python"}},
	}
	store := NewApplicationStore(path)
	createdApplication, err := store.CreateApplication(application)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewApplicationStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.GetApplication(createdApplication.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, createdApplication) || got.VacancyID != vacancy.ID || got.MatchResult == nil {
		t.Fatalf("vacancy/application domain representation changed after reload: got=%+v want=%+v", got, createdApplication)
	}
}
