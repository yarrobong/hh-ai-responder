package runtime

import "testing"

func TestVacancyBoundaryRootFlowUsesExtractedModel(t *testing.T) {
	store := NewVacancyStore(t.TempDir() + "/vacancies.json")
	value := Vacancy{Name: "Python backend", ExternalID: "hh-boundary", Skills: []string{"Python"}}
	created, err := store.Create(value)
	if err != nil {
		t.Fatal(err)
	}
	if reason := deterministicVacancyRejectReason(created, "Python", 0, []string{"PHP"}); reason != "" {
		t.Fatalf("compatible vacancy was rejected: %s", reason)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := NewVacancyStore(store.path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := loaded.GetByExternalID("hh-boundary")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != value.Name || got.ExternalID != value.ExternalID {
		t.Fatalf("root flow changed extracted vacancy: %+v", got)
	}
}
