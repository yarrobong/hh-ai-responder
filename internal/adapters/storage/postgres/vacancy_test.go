package postgresstorage

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type vacancyScannerFixture struct {
	values []interface{}
}

func TestMapPostgresVacancyErrorUsesDomainSentinels(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "local id", constraint: "vacancies_pkey", want: ErrDuplicateVacancyID},
		{name: "external id", constraint: "vacancies_external_id_unique", want: ErrDuplicateVacancyExternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := mapPostgresVacancyError("create vacancy", &pgconn.PgError{Code: "23505", ConstraintName: test.constraint})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want errors.Is(..., %v)", err, test.want)
			}
		})
	}
}

func (f vacancyScannerFixture) Scan(dest ...interface{}) error {
	for index, target := range dest {
		value := reflect.ValueOf(target).Elem()
		if f.values[index] == nil {
			value.Set(reflect.Zero(value.Type()))
			continue
		}
		source := reflect.ValueOf(f.values[index])
		if source.Type().AssignableTo(value.Type()) {
			value.Set(source)
			continue
		}
		value.Set(source.Convert(value.Type()))
	}
	return nil
}

func TestScanPreservesResponseCountKnownAndJSONBValues(t *testing.T) {
	createdAt := time.Date(2026, 9, 8, 12, 0, 0, 123456789, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	base := []interface{}{
		42, "hh-42", "Example", "Python", "Integrate APIs", []byte(`["Python"]`), []byte(`["Django"]`),
		"150000", "RUB", "Екатеринбург", "remote", "full", "hh",
		pgtype.Timestamptz{}, pgtype.Timestamptz{}, []byte(`{"area_id":"1002"}`),
		pgtype.Timestamptz{Time: createdAt, Valid: true}, pgtype.Timestamptz{Time: updatedAt, Valid: true},
		pgtype.Int8{}, pgtype.Int8{}, pgtype.Int8{Int64: createdAt.UnixNano(), Valid: true}, pgtype.Int8{Int64: updatedAt.UnixNano(), Valid: true},
		"flexible", "between1And3", []byte(`{"desktop":"https://hh.example/42"}`), 0,
		"Екатеринбург", 8, "Example", "https://example.test", []byte(`{"from":100000}`), "2026-09-08", nil, []byte(`["label"]`),
		true, true, false, "https://hh.example/apply/42", true,
		[]byte(`{"score":82}`), nil, "partial", []byte(`[{"method":"fixture","source":"hh","confidence":0.8}]`),
	}

	known, err := scanPostgresVacancy(vacancyScannerFixture{values: base})
	if err != nil {
		t.Fatal(err)
	}
	if !known.TotalResponsesCountKnown || known.TotalResponsesCount != 0 {
		t.Fatalf("explicit zero response count changed: %+v", known)
	}
	if known.MatchResult == nil || known.MatchResult.Score != 82 || len(known.Requirements) != 1 || known.HHMetadata["area_id"] != "1002" {
		t.Fatalf("JSONB vacancy values changed: %+v", known)
	}
	if !known.CreatedAt.Equal(createdAt) || !known.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("nanosecond timestamps changed: created=%s updated=%s", known.CreatedAt, known.UpdatedAt)
	}

	unknownValues := append([]interface{}{}, base...)
	unknownValues[38] = false
	unknown, err := scanPostgresVacancy(vacancyScannerFixture{values: unknownValues})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.TotalResponsesCountKnown || unknown.TotalResponsesCount != 0 {
		t.Fatalf("unknown response count collapsed into known zero: %+v", unknown)
	}
}
