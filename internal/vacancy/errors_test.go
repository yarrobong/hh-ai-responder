package vacancy_test

import (
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/vacancy"
)

func TestValidateKeepsNormalizedVacancyInvariantsInDomain(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		value   vacancy.Vacancy
		wantErr string
	}{
		{name: "negative id", value: vacancy.Vacancy{ID: -1}, wantErr: "negative"},
		{name: "one timestamp", value: vacancy.Vacancy{CreatedAt: at}, wantErr: "supplied together"},
		{name: "reversed timestamps", value: vacancy.Vacancy{CreatedAt: at, UpdatedAt: at.Add(-time.Second)}, wantErr: "out of order"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := vacancy.Validate(test.value); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error=%v, want substring %q", err, test.wantErr)
			}
		})
	}
	if err := vacancy.Validate(vacancy.Vacancy{ID: 0, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("valid normalized vacancy rejected: %v", err)
	}
}
