package ports

import (
	"context"

	"hh-ai-responder/internal/vacancy"
)

// VacancyQuery contains the currently supported persistence filters. HH
// search and transport concerns do not belong to this port.
type VacancyQuery struct {
	ID         *int
	ExternalID string
}

// VacancyReader is the read capability used by career synchronization and
// vacancy-backed orchestration.
type VacancyReader interface {
	Get(context.Context, int) (vacancy.Vacancy, error)
	GetByExternalID(context.Context, string) (vacancy.Vacancy, error)
	List(context.Context, VacancyQuery) ([]vacancy.Vacancy, error)
}

// VacancyWriter is the write capability used by normalized vacancy imports
// and updates.
type VacancyWriter interface {
	Create(context.Context, vacancy.Vacancy) (vacancy.Vacancy, error)
	Update(context.Context, vacancy.Vacancy) error
	Save(context.Context) error
}

// VacancyStore is the aggregate capability required by the current career
// synchronization transaction. It is a named composition of small ports,
// not a generic CRUD repository.
type VacancyStore interface {
	VacancyReader
	VacancyWriter
}
