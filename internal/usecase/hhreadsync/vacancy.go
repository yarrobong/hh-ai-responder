package hhreadsync

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/vacancy"
)

func (s *Service) importVacancy(ctx context.Context, record hhread.VacancyRecord) (outcome, error) {
	value, err := MapVacancy(record)
	if err != nil {
		return skipped, err
	}
	old, getErr := s.deps.Vacancies.GetByExternalID(ctx, value.ExternalID)
	if getErr == nil {
		value.ID, value.CreatedAt = old.ID, old.CreatedAt
		if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = old.UpdatedAt
		}
		// Provider refreshes replace provider-owned fields only. Local matching,
		// recommendation, and reconciliation evidence are retained.
		value.MatchResult = old.MatchResult
		value.ApplicationRecommendation = old.ApplicationRecommendation
		value.ReconciliationEvidence = old.ReconciliationEvidence
		if VacancySourceEquivalent(old, value) {
			return unchanged, nil
		}
		return updated, s.deps.Vacancies.Update(ctx, value)
	}
	if !errors.Is(getErr, vacancy.ErrVacancyNotFound) {
		return skipped, getErr
	}
	_, err = s.deps.Vacancies.Create(ctx, value)
	return created, err
}

// MapVacancy converts a normalized provider record into the persisted domain
// aggregate. Raw HH decoding remains in the read transport adapter.
func MapVacancy(record hhread.VacancyRecord) (vacancy.Vacancy, error) {
	externalID := strings.TrimSpace(record.ExternalID)
	if externalID == "" && record.ID > 0 {
		externalID = strconv.Itoa(record.ID)
	}
	if externalID == "" {
		return vacancy.Vacancy{}, errors.New("HH vacancy has no external id")
	}
	title := firstNonEmpty(record.Title, externalID)
	created, updated := record.PublishedAt, record.UpdatedAt
	if created.IsZero() {
		created = updated
	}
	if updated.IsZero() {
		updated = created
	}
	if created.IsZero() {
		created = time.Now().UTC()
		updated = created
	}
	value := vacancy.Vacancy{
		ID: record.ID, ExternalID: externalID, Name: title, Title: title,
		Description: record.Description, Requirements: append([]string{}, record.Requirements...), Skills: append([]string{}, record.KeySkills...),
		Salary: record.Salary, SalaryCurrency: record.Currency, Location: record.Location,
		WorkFormat: record.WorkFormat, EmploymentType: record.EmploymentType, WorkExperience: record.Experience,
		WorkSchedule: record.Schedule, Source: "hh", Links: map[string]string{"desktop": record.URL},
		CreatedAt: created, UpdatedAt: updated, PublishedAt: record.PublishedAt, HHUpdatedAt: record.UpdatedAt,
		TotalResponsesCount: record.TotalResponsesCount, TotalResponsesCountKnown: record.TotalResponsesCountKnown,
		Archived: record.Archived, ResponseLetterRequired: record.ResponseLetterRequired, UserTestPresent: record.UserTestPresent,
		ResponseURL: record.ResponseURL, HHMetadata: copyStringMap(record.Metadata),
	}
	value.DataCompleteness = inferCompleteness(value)
	return value, nil
}

func inferCompleteness(value vacancy.Vacancy) vacancy.DataCompleteness {
	if strings.TrimSpace(value.Description) != "" && (len(value.Requirements) > 0 || len(value.Skills) > 0) && strings.TrimSpace(value.Location) != "" {
		return vacancy.DataCompletenessFull
	}
	if strings.TrimSpace(value.Description) != "" || len(value.Requirements) > 0 || len(value.Skills) > 0 || strings.TrimSpace(value.Location) != "" {
		return vacancy.DataCompletenessPartial
	}
	return vacancy.DataCompletenessMinimal
}

// VacancySourceEquivalent compares only provider-owned fields. Local IDs,
// timestamps, match output, and reconciliation evidence are excluded.
func VacancySourceEquivalent(a, b vacancy.Vacancy) bool {
	type projection struct {
		ExternalID, Name, Title, Description, Salary, SalaryCurrency, Location, WorkFormat, EmploymentType string
		Source, WorkSchedule, WorkExperience, URL                                                          string
		Requirements, Skills                                                                               []string
		PublishedAt, HHUpdatedAt                                                                           time.Time
	}
	project := func(value vacancy.Vacancy) projection {
		return projection{ExternalID: value.ExternalID, Name: value.Name, Title: value.Title, Description: value.Description,
			Salary: value.Salary, SalaryCurrency: value.SalaryCurrency, Location: value.Location, WorkFormat: value.WorkFormat,
			EmploymentType: value.EmploymentType, Source: value.Source, WorkSchedule: value.WorkSchedule, WorkExperience: value.WorkExperience,
			URL: value.Links["desktop"], Requirements: value.Requirements, Skills: value.Skills, PublishedAt: value.PublishedAt, HHUpdatedAt: value.HHUpdatedAt}
	}
	return reflect.DeepEqual(project(a), project(b)) && stringMapsEqual(a.HHMetadata, b.HHMetadata) &&
		a.TotalResponsesCount == b.TotalResponsesCount && a.TotalResponsesCountKnown == b.TotalResponsesCountKnown &&
		a.Archived == b.Archived && a.ResponseLetterRequired == b.ResponseLetterRequired && a.UserTestPresent == b.UserTestPresent && a.ResponseURL == b.ResponseURL
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func copyStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
