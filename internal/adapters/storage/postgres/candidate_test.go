package postgresstorage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"hh-ai-responder/internal/candidate"
)

type candidateRepositoryTestDB struct{}

func (candidateRepositoryTestDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (candidateRepositoryTestDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (candidateRepositoryTestDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	panic("unexpected QueryRow")
}

func TestCandidateRepositoryPortAssertions(t *testing.T) {
	var repository CandidateRepository
	var _ interface {
		CurrentCandidate(context.Context) (candidate.Candidate, error)
	} = &repository
}

func TestCandidateRepositoryVersionPreconditionMapsToConflict(t *testing.T) {
	repository := &CandidateRepository{db: candidateRepositoryTestDB{}}
	err := repository.PersistCandidateIfVersion(context.Background(), Candidate{ID: "candidate-1", Version: 3}, 1)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestCandidateRepositoryCanonicalOrderingIsDeterministic(t *testing.T) {
	value := Candidate{
		ID:      "candidate-1",
		Version: 1,
		Skills: []CanonicalCandidateSkill{
			{ID: "skill-b", Name: "B"},
			{ID: "skill-a", Name: "A"},
		},
	}
	sortCandidate(&value)
	if value.Skills[0].ID != "skill-a" || value.Skills[1].ID != "skill-b" {
		t.Fatalf("skills are not ordered by stable id: %+v", value.Skills)
	}
}

func TestCandidateRepositoryNullabilityAndFingerprint(t *testing.T) {
	if candidateNullableJSON(nil) != nil || candidateNullableString("") != nil || candidateNullableTime(time.Time{}) != nil || nullableNanos(time.Time{}) != nil {
		t.Fatal("zero candidate values must map to SQL NULL")
	}
	value := Candidate{ID: "candidate-1", Version: 1}
	first, err := CandidateFingerprint(value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CandidateFingerprint(value)
	if err != nil || first == "" || first != second {
		t.Fatalf("candidate fingerprint is not deterministic: %q %q %v", first, second, err)
	}
}

func TestCandidateRepositoryPostgresErrorTranslation(t *testing.T) {
	err := mapCandidatePostgresError("insert candidate", &pgconn.PgError{Code: "23505", ConstraintName: "candidates_pkey"})
	if !errors.Is(err, ErrRepositoryConflict) {
		t.Fatalf("expected repository conflict, got %v", err)
	}
}
