package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestStorageBackendSelectionDefaultsToJSON(t *testing.T) {
	backend, err := normalizeStorageBackend("")
	if err != nil || backend != storageBackendJSON {
		t.Fatalf("default backend=%q err=%v", backend, err)
	}
	store := NewVacancyStore(t.TempDir() + "/vacancies.json")
	repository, closeRepository, err := BuildVacancyRepository(context.Background(), Config{}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRepository()
	if _, ok := repository.(*JSONVacancyRepository); !ok {
		t.Fatalf("default repository type=%T, want JSONVacancyRepository", repository)
	}
}

func TestStorageBackendValidation(t *testing.T) {
	if _, err := normalizeStorageBackend("sqlite"); err == nil {
		t.Fatal("unsupported storage backend was accepted")
	}
	if _, err := OpenPostgres(context.Background(), PostgresConfig{}); err == nil {
		t.Fatal("postgres without URL was accepted")
	}
	if _, err := OpenPostgres(context.Background(), PostgresConfig{DatabaseURL: "postgres://localhost/test", MinConns: 2, MaxConns: 1}); err == nil {
		t.Fatal("invalid pool limits were accepted")
	}
}

// Set POSTGRES_TEST_DATABASE_URL to run the real PostgreSQL contract. The
// test is intentionally opt-in so ordinary unit tests never need a database.
func TestPostgresVacancyRepositoryContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresVacancyRepository(pool)

	id := int(time.Now().UnixNano() % 1_000_000_000)
	if id <= 0 {
		id = 1
	}
	externalID := "contract-" + time.Now().UTC().Format("20060102150405.000000000")
	createdAt := time.Date(2026, 9, 1, 10, 11, 12, 123456789, time.UTC)
	updatedAt := createdAt.Add(2*time.Hour + 7*time.Nanosecond)
	value := Vacancy{
		ID: id, ExternalID: externalID, Name: "Python integration specialist", Title: "Python/Django",
		Description: "Integrate APIs", Requirements: []string{"Python"}, Skills: []string{"Python", "Django"},
		Salary: "150000", SalaryCurrency: "RUB", Location: "Екатеринбург", WorkFormat: "remote",
		EmploymentType: "full", Source: "hh", PublishedAt: createdAt, HHUpdatedAt: updatedAt,
		HHMetadata: map[string]string{"area_id": "1002"}, Links: map[string]string{"desktop": "https://hh.example/42"},
		TotalResponsesCount: 7, TotalResponsesCountKnown: true, Area: NamedObject{Name: "Екатеринбург"},
		Company: Company{ID: 8, Name: "Example", CompanySiteURL: "https://example.test"}, CreationTime: "2026-09-01",
		WorkSchedule: "flexible", WorkExperience: "between1And3", ResponseLetterRequired: true,
		UserTestPresent: true, ResponseURL: "https://hh.example/apply/42", CreatedAt: createdAt,
		UpdatedAt: updatedAt, DataCompleteness: DataCompletenessPartial,
		ReconciliationEvidence: []ReconciliationEvidence{{Method: "fixture", Source: "hh", Confidence: .8, ReconciledAt: updatedAt}},
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM vacancies WHERE id = $1", id) }()

	created, err := repository.Create(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != id || !created.CreatedAt.Equal(createdAt) || !created.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("identity/timestamps changed on create: got=%+v", created)
	}
	byID, err := repository.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	byExternal, err := repository.GetByExternalID(ctx, externalID)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]Vacancy{"by id": byID, "by external id": byExternal} {
		if got.ID != value.ID || got.ExternalID != value.ExternalID || got.Company != value.Company ||
			!got.CreatedAt.Equal(createdAt) || !got.UpdatedAt.Equal(updatedAt) ||
			len(got.ReconciliationEvidence) != 1 || got.ReconciliationEvidence[0].Confidence != .8 {
			t.Errorf("%s changed persisted vacancy: got=%+v", name, got)
		}
	}
	if _, err := repository.Create(ctx, Vacancy{ID: id + 1, ExternalID: externalID, Name: "duplicate"}); !errors.Is(err, ErrDuplicateVacancyExternal) {
		t.Fatalf("duplicate external id error=%v", err)
	}

	updated := value
	updated.Title = "Updated title"
	updated.CreatedAt = time.Time{}
	updated.UpdatedAt = updatedAt.Add(time.Hour)
	if err := repository.Update(ctx, updated); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, id)
	if err != nil || got.Title != "Updated title" || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("update did not preserve created_at: got=%+v err=%v", got, err)
	}
	if _, err := repository.Get(ctx, id+987654321); !errors.Is(err, ErrVacancyNotFound) {
		t.Fatalf("missing vacancy error=%v", err)
	}
	if err := repository.Save(nil); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresVacancyRepositoryRequiresPool(t *testing.T) {
	repository := NewPostgresVacancyRepository(nil)
	if _, err := repository.Get(context.Background(), 1); err == nil {
		t.Fatal("repository without pool did not fail closed")
	}
}
