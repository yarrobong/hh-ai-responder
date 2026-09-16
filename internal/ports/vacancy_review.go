package ports

import (
	"context"
	"time"

	"hh-ai-responder/internal/vacancy"
)

// VacancyObserver is an optional synchronization capability implemented by
// the canonical PostgreSQL vacancy repository. It atomically persists the
// normalized vacancy and its provider observation metadata.
type VacancyObserver interface {
	ObserveVacancy(context.Context, vacancy.Vacancy, time.Time) (vacancy.ObservationResult, error)
}

// VacancyFreshnessReader is intentionally separate from VacancyReader so
// legacy JSON compatibility does not become a second review-state source.
type VacancyFreshnessReader interface {
	GetFreshness(context.Context, int) (vacancy.Freshness, error)
	ListFreshness(context.Context, []int) (map[int]vacancy.Freshness, error)
}
