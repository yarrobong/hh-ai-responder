package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestVacancyStorageContract_RestartPreservesIDsExternalIDsTimestampsAndEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), VacanciesFilename)
	createdAt := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	vacancy := Vacancy{
		ID: 42, ExternalID: "hh-vacancy-42", Name: "Python integration specialist", Title: "Python/Django",
		Description: "Integrate APIs", Requirements: []string{"Python"}, Skills: []string{"Python", "Django"},
		Salary: "150000", SalaryCurrency: "RUB", Location: "Екатеринбург", WorkFormat: "remote",
		Source: "hh", PublishedAt: createdAt, HHUpdatedAt: updatedAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
		HHMetadata:          map[string]string{"area_id": "1002", "response_state": "unknown"},
		Links:               map[string]string{"alternate_url": "https://hh.example/vacancy/42"},
		TotalResponsesCount: 7, WorkSchedule: "flexible", WorkExperience: "between1And3",
		ResponseLetterRequired: true, UserTestPresent: true, Archived: false, ResponseURL: "https://hh.example/apply/42",
		DataCompleteness:       DataCompletenessPartial,
		ReconciliationEvidence: []ReconciliationEvidence{{Method: "fixture", Source: "hh", Confidence: 0.8, ReconciledAt: updatedAt}},
	}
	store := NewVacancyStore(path)
	created, err := store.Create(vacancy)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != vacancy.ID || created.ExternalID != vacancy.ExternalID || !created.CreatedAt.Equal(vacancy.CreatedAt) ||
		!created.UpdatedAt.Equal(vacancy.UpdatedAt) || !created.TotalResponsesCountKnown {
		t.Fatalf("explicit vacancy identity/timestamps changed on create: got=%+v want=%+v", created, vacancy)
	}
	want := created
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	restarted := NewVacancyStore(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	byID, err := restarted.Get(42)
	if err != nil {
		t.Fatal(err)
	}
	byExternal, err := restarted.GetByExternalID("hh-vacancy-42")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byID, want) || !reflect.DeepEqual(byExternal, want) {
		t.Fatalf("vacancy restart changed state: byID=%+v byExternal=%+v", byID, byExternal)
	}
	if byID.CreatedAt != createdAt || byID.UpdatedAt != updatedAt || byID.ExternalID != "hh-vacancy-42" ||
		byID.DataCompleteness != DataCompletenessPartial || len(byID.ReconciliationEvidence) != 1 {
		t.Fatalf("vacancy identity/timestamp/evidence contract changed: %+v", byID)
	}
	if _, err := restarted.Create(Vacancy{ID: 99, ExternalID: "hh-vacancy-42", Name: "duplicate"}); !errors.Is(err, ErrDuplicateVacancyExternal) {
		t.Fatalf("duplicate vacancy external id was accepted: %v", err)
	}
}

func TestVacancyStorageContract_MissingCorruptAndUnknownVersionPreserveMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, VacanciesFilename)
	missing := NewVacancyStore(path)
	if err := missing.Load(); err != nil {
		t.Fatal(err)
	}
	values, err := missing.List()
	if err != nil || len(values) != 0 {
		t.Fatalf("missing vacancy file did not load as empty: values=%+v err=%v", values, err)
	}
	if NewVacancyStore().path != VacanciesFilename {
		t.Fatalf("default vacancy path changed: %q", NewVacancyStore().path)
	}

	store := NewVacancyStore(path)
	keep, err := store.Create(Vacancy{ID: 7, ExternalID: "keep", Name: "Keep", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"corrupt":  "{",
		"version":  `{"version":2,"vacancies":[]}`,
		"null":     `{"version":1,"vacancies":null}`,
		"extra":    `{"version":1,"vacancies":[],"extra":true}`,
		"trailing": `{"version":1,"vacancies":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Load(); err == nil {
				t.Fatal("invalid vacancy store was accepted")
			}
			got, err := store.Get(keep.ID)
			if err != nil || got.ExternalID != keep.ExternalID {
				t.Fatalf("failed load replaced memory: got=%+v err=%v", got, err)
			}
		})
	}
}
