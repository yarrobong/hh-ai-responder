package vacancy

import "errors"

// Repository errors shared by persistence adapters. They describe normalized
// Vacancy identity semantics and intentionally do not mention a backend.
var (
	ErrVacancyNotFound          = errors.New("vacancy not found")
	ErrDuplicateVacancyID       = errors.New("duplicate vacancy id")
	ErrDuplicateVacancyExternal = errors.New("duplicate vacancy external id")
)

// Validate checks invariants intrinsic to a normalized Vacancy. Collection
// uniqueness and file/database constraints remain owned by storage adapters.
func Validate(value Vacancy) error {
	if value.ID < 0 {
		return errors.New("vacancy id must not be negative")
	}
	if value.CreatedAt.IsZero() != value.UpdatedAt.IsZero() {
		return errors.New("vacancy timestamps must be supplied together")
	}
	if !value.CreatedAt.IsZero() && value.UpdatedAt.Before(value.CreatedAt) {
		return errors.New("vacancy timestamps are out of order")
	}
	return nil
}
