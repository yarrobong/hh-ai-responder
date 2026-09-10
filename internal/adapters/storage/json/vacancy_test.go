package jsonstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

func adapterVacancy(id int, externalID string, at time.Time) vacancy.Vacancy {
	return vacancy.Vacancy{
		ID: id, ExternalID: externalID, Name: "Python integration specialist",
		Area:         vacancy.NamedObject{Name: "Екатеринбург"},
		Company:      vacancy.Company{ID: 7, Name: "Example", CompanySiteURL: "https://example.test"},
		Requirements: []string{"Python"}, Skills: []string{"Django"},
		CreatedAt: at, UpdatedAt: at, TotalResponsesCount: 0, TotalResponsesCountKnown: true,
		MatchResult:            &vacancy.MatchResult{Score: 82, Recommendation: &vacancy.ApplicationRecommendation{Decision: vacancy.RecommendationMaybe, Reason: "review"}},
		DataCompleteness:       vacancy.DataCompletenessPartial,
		ReconciliationEvidence: []vacancy.ReconciliationEvidence{{Method: "fixture", Source: "hh", Confidence: 0.8, ReconciledAt: at}},
	}
}

func TestVacancyRepositoryRoundTripRestartAndExplicitSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", VacanciesFilename)
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	repo := NewVacancyRepository(path)
	want := adapterVacancy(42, "hh-42", at)
	got, err := repo.Create(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create changed vacancy: got=%+v want=%+v", got, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("create persisted before explicit Save: %v", err)
	}
	if err := repo.Save(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted := NewVacancyRepository(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	byID, err := restarted.Get(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	byExternal, err := restarted.GetByExternalID(context.Background(), want.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byID, want) || !reflect.DeepEqual(byExternal, want) {
		t.Fatalf("restart changed vacancy: byID=%+v byExternal=%+v want=%+v", byID, byExternal, want)
	}
	if byID.MatchResult == nil || byID.MatchResult.Recommendation == nil || byID.DataCompleteness != vacancy.DataCompletenessPartial || len(byID.ReconciliationEvidence) != 1 {
		t.Fatalf("restart dropped persisted metadata: %+v", byID)
	}
}

func TestVacancyRepositoryMissingCorruptAndResponseCountSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, VacanciesFilename)
	repo := NewVacancyRepository(path)
	if err := repo.Load(); err != nil {
		t.Fatal(err)
	}
	values, err := repo.List(context.Background(), ports.VacancyQuery{})
	if err != nil || len(values) != 0 {
		t.Fatalf("missing file did not load as empty: values=%+v err=%v", values, err)
	}

	knownPath := filepath.Join(dir, "known.json")
	unknownPath := filepath.Join(dir, "unknown.json")
	writeVacancyFixture := func(path, count string) {
		t.Helper()
		raw := `{"version":1,"vacancies":[{"vacancyId":7,"external_id":"hh-7"` + count + `}]}`
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeVacancyFixture(knownPath, `,"totalResponsesCount":0`)
	known := NewVacancyRepository(knownPath)
	if err := known.Load(); err != nil {
		t.Fatal(err)
	}
	knownValue, err := known.Get(context.Background(), 7)
	if err != nil || !knownValue.TotalResponsesCountKnown {
		t.Fatalf("explicit zero response count was lost: value=%+v err=%v", knownValue, err)
	}
	writeVacancyFixture(unknownPath, "")
	unknown := NewVacancyRepository(unknownPath)
	if err := unknown.Load(); err != nil {
		t.Fatal(err)
	}
	unknownValue, err := unknown.Get(context.Background(), 7)
	if err != nil || unknownValue.TotalResponsesCountKnown {
		t.Fatalf("absent response count became known: value=%+v err=%v", unknownValue, err)
	}

	keep := adapterVacancy(9, "keep", time.Now().UTC())
	if _, err := repo.Create(context.Background(), keep); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.Load(); err == nil || !strings.Contains(err.Error(), "invalid vacancy store") {
		t.Fatalf("corrupt JSON was accepted: %v", err)
	}
	got, err := repo.Get(context.Background(), keep.ID)
	if err != nil || got.ExternalID != keep.ExternalID {
		t.Fatalf("corrupt load did not preserve memory: got=%+v err=%v", got, err)
	}
}

func TestVacancyRepositoryDuplicateUpdateQueryOrderingAndDetachment(t *testing.T) {
	repo := NewVacancyRepository(filepath.Join(t.TempDir(), VacanciesFilename))
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	first, err := repo.Create(context.Background(), adapterVacancy(0, "hh-1", at))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(context.Background(), adapterVacancy(0, "hh-2", at.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("local ID sequence changed: first=%d second=%d", first.ID, second.ID)
	}
	if _, err := repo.Create(context.Background(), adapterVacancy(99, "hh-1", at)); !errors.Is(err, ErrDuplicateVacancyExternal) {
		t.Fatalf("duplicate external ID accepted: %v", err)
	}
	if err := repo.Update(context.Background(), vacancy.Vacancy{ID: first.ID, ExternalID: second.ExternalID}); !errors.Is(err, ErrDuplicateVacancyExternal) {
		t.Fatalf("duplicate external ID on update accepted: %v", err)
	}
	if err := repo.Update(context.Background(), vacancy.Vacancy{ID: 404}); !errors.Is(err, ErrVacancyNotFound) {
		t.Fatalf("missing update error changed: %v", err)
	}

	values, err := repo.List(context.Background(), ports.VacancyQuery{})
	if err != nil || len(values) != 2 || values[0].ID != first.ID || values[1].ID != second.ID {
		t.Fatalf("ordering changed: values=%+v err=%v", values, err)
	}
	values[0].Skills[0] = "mutated"
	values[0].HHMetadata = map[string]string{"changed": "yes"}
	unchanged, err := repo.Get(context.Background(), first.ID)
	if err != nil || unchanged.Skills[0] == "mutated" || unchanged.HHMetadata != nil {
		t.Fatalf("query result was attached to repository state: %+v err=%v", unchanged, err)
	}

	id := second.ID
	filtered, err := repo.List(context.Background(), ports.VacancyQuery{ID: &id})
	if err != nil || len(filtered) != 1 || filtered[0].ID != second.ID {
		t.Fatalf("ID query changed: values=%+v err=%v", filtered, err)
	}
}

func TestVacancyRepositoryPrivateFileContractAndContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, VacanciesFilename)
	repo := NewVacancyRepository(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Create(ctx, vacancy.Vacancy{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled create error changed: %v", err)
	}
	if err := repo.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, dir); mode.Perm() != 0o700 {
		t.Fatalf("directory mode = %o, want 700", mode.Perm())
	}
	if mode := fileMode(t, path); mode.Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", mode.Perm())
	}
}
