package hhreadsync

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/ports"
	hhreadport "hh-ai-responder/internal/ports/hhread"
	"hh-ai-responder/internal/vacancy"
)

func (s *Service) importVacancy(ctx context.Context, record hhread.VacancyRecord, observedAt time.Time) (outcome, error) {
	value, err := MapVacancy(record)
	if err != nil {
		return skipped, err
	}
	if observer, ok := s.deps.Vacancies.(ports.VacancyObserver); ok {
		observation, err := observer.ObserveVacancy(ctx, value, observedAt)
		if err != nil {
			return skipped, err
		}
		if observation.Created {
			return created, nil
		}
		if observation.Updated || observation.SourceChanged || observation.MaterialChanged {
			return updated, nil
		}
		return unchanged, nil
	}
	old, getErr := s.deps.Vacancies.GetByExternalID(ctx, value.ExternalID)
	if getErr == nil {
		value = MergeVacancy(old, value)
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
		Description: record.Description, Requirements: append([]string{}, record.Requirements...), Skills: append([]string{}, record.KeySkills...), ProfessionalRoles: append([]string{}, record.ProfessionalRoles...),
		Salary: record.Salary, SalaryCurrency: record.Currency, Location: record.Location,
		WorkFormat: record.WorkFormat, EmploymentType: record.EmploymentType, WorkExperience: record.Experience,
		WorkSchedule: record.Schedule, Source: "hh", Links: map[string]string{"desktop": record.URL},
		CreatedAt: created, UpdatedAt: updated, PublishedAt: record.PublishedAt, HHUpdatedAt: record.UpdatedAt,
		TotalResponsesCount: record.TotalResponsesCount, TotalResponsesCountKnown: record.TotalResponsesCountKnown,
		Archived: record.Archived, ResponseLetterRequired: record.ResponseLetterRequired, UserTestPresent: record.UserTestPresent,
		ResponseURL: record.ResponseURL, HHMetadata: copyStringMap(record.Metadata), Company: vacancy.Company{Name: record.Company}, Area: vacancy.NamedObject{Name: record.AreaName},
	}
	if value.Location == "" {
		value.Location = record.Address
	}
	value.DataCompleteness = inferCompleteness(value)
	return value, nil
}

// MergeVacancy preserves richer provider data when a later search snapshot is
// partial. An empty search value is treated as omitted, not as provider
// deletion; HH detail reads are the only source used to replace a rich field.
// This policy is intentionally conservative because the current normalized
// DTO does not retain JSON omitted-vs-null presence bits.
func MergeVacancy(old, incoming vacancy.Vacancy) vacancy.Vacancy {
	merged := incoming
	merged.Description = firstNonEmpty(incoming.Description, old.Description)
	merged.Salary = firstNonEmpty(incoming.Salary, old.Salary)
	merged.SalaryCurrency = firstNonEmpty(incoming.SalaryCurrency, old.SalaryCurrency)
	merged.Location = firstNonEmpty(incoming.Location, old.Location)
	merged.WorkFormat = firstNonEmpty(incoming.WorkFormat, old.WorkFormat)
	merged.EmploymentType = firstNonEmpty(incoming.EmploymentType, old.EmploymentType)
	merged.WorkSchedule = firstNonEmpty(incoming.WorkSchedule, old.WorkSchedule)
	merged.WorkExperience = firstNonEmpty(incoming.WorkExperience, old.WorkExperience)
	merged.Name = firstNonEmpty(incoming.Name, old.Name)
	merged.Title = firstNonEmpty(incoming.Title, old.Title)
	merged.Source = firstNonEmpty(incoming.Source, old.Source)
	merged.ResponseURL = firstNonEmpty(incoming.ResponseURL, old.ResponseURL)
	merged.CreationTime = firstNonEmpty(incoming.CreationTime, old.CreationTime)
	merged.LastChangeTime.Value = firstNonEmpty(incoming.LastChangeTime.Value, old.LastChangeTime.Value)
	merged.Area.Name = firstNonEmpty(incoming.Area.Name, old.Area.Name)
	merged.Company.ID = firstNonZero(incoming.Company.ID, old.Company.ID)
	merged.Company.Name = firstNonEmpty(incoming.Company.Name, old.Company.Name)
	merged.Company.CompanySiteURL = firstNonEmpty(incoming.Company.CompanySiteURL, old.Company.CompanySiteURL)
	merged.PublishedAt = firstTime(incoming.PublishedAt, old.PublishedAt)
	merged.HHUpdatedAt = firstTime(incoming.HHUpdatedAt, old.HHUpdatedAt)
	if len(incoming.Requirements) == 0 {
		merged.Requirements = append([]string(nil), old.Requirements...)
	}
	if len(incoming.Skills) == 0 {
		merged.Skills = append([]string(nil), old.Skills...)
	}
	if len(incoming.ProfessionalRoles) == 0 {
		merged.ProfessionalRoles = append([]string(nil), old.ProfessionalRoles...)
	}
	if len(incoming.Links) == 0 {
		merged.Links = copyStringMap(old.Links)
	} else {
		merged.Links = copyStringMap(old.Links)
		for key, value := range incoming.Links {
			if strings.TrimSpace(value) != "" {
				merged.Links[key] = value
			}
		}
	}
	if incoming.Compensation.From == nil && incoming.Compensation.To == nil && incoming.Compensation.Currency == "" && incoming.Compensation.Gross == nil {
		merged.Compensation = old.Compensation
	}
	merged.HHMetadata = copyStringMap(old.HHMetadata)
	for key, value := range incoming.HHMetadata {
		if strings.TrimSpace(value) != "" {
			merged.HHMetadata[key] = value
		}
	}
	// Response-count knowledge is an observation-level flag. A later search
	// result that omits the count may legitimately make this field unknown;
	// unlike detail text it is not used as rich vacancy evidence.
	// A false value in the partial search DTO can mean omitted. Preserve a
	// previously asserted positive provider flag until a full detail snapshot
	// supplies a presence-aware replacement.
	merged.ResponseLetterRequired = incoming.ResponseLetterRequired || old.ResponseLetterRequired
	merged.UserTestPresent = incoming.UserTestPresent || old.UserTestPresent
	merged.Archived = incoming.Archived || old.Archived
	merged.DataCompleteness = inferCompleteness(merged)
	return merged
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Service) enrichVacancyDetails(ctx context.Context, records []hhread.VacancyRecord, options ReadOptions, result *Result) []hhread.VacancyRecord {
	source, ok := s.deps.Source.(hhreadport.VacancyDetailSource)
	if !ok || len(records) == 0 {
		return records
	}
	limit := options.MaxVacancyDetails
	if limit == 0 {
		limit = defaultMaxVacancyDetails
	}
	for i := range records {
		if err := ctx.Err(); err != nil {
			return records
		}
		if !vacancyNeedsDetail(s.deps.Vacancies, ctx, records[i]) {
			result.DetailSkipped++
			continue
		}
		if result.DetailRequested >= limit {
			result.DetailSkipped++
			continue
		}
		result.DetailRequested++
		detail, err := source.ReadVacancyDetail(ctx, records[i].ID)
		if err != nil {
			result.DetailFailed++
			result.Warnings = append(result.Warnings, "vacancy detail read failed for "+strconv.Itoa(records[i].ID)+": "+safeError(err))
			continue
		}
		before := records[i]
		records[i] = mergeVacancyRecords(records[i], detail)
		if records[i].Metadata == nil {
			records[i].Metadata = map[string]string{}
		}
		records[i].Metadata["hh_detail_enrichment_version"] = "1"
		records[i].Metadata["hh_detail_enriched_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		result.DetailSucceeded++
		result.DetailFieldsEnriched += countEnrichedVacancyFields(before, records[i])
	}
	return records
}

func vacancyNeedsDetail(store ports.VacancyStore, ctx context.Context, record hhread.VacancyRecord) bool {
	if store == nil || record.ID <= 0 {
		return true
	}
	old, err := store.GetByExternalID(ctx, firstNonEmpty(record.ExternalID, strconv.Itoa(record.ID)))
	if err != nil {
		return true
	}
	if old.HHMetadata["hh_detail_enrichment_version"] != "1" {
		return true
	}
	if !record.UpdatedAt.IsZero() && !old.HHUpdatedAt.IsZero() && !record.UpdatedAt.Equal(old.HHUpdatedAt) {
		return true
	}
	return strings.TrimSpace(old.Description) == "" || len(old.Skills) == 0 || strings.TrimSpace(old.WorkFormat) == "" || strings.TrimSpace(old.Area.Name) == "" || strings.TrimSpace(old.WorkExperience) == ""
}

func mergeVacancyRecords(search, detail hhread.VacancyRecord) hhread.VacancyRecord {
	merged := search
	merged.ExternalID = firstNonEmpty(detail.ExternalID, search.ExternalID)
	if detail.ID > 0 {
		merged.ID = detail.ID
	}
	merged.Title = firstNonEmpty(detail.Title, search.Title)
	merged.Company = firstNonEmpty(detail.Company, search.Company)
	merged.Description = firstNonEmpty(detail.Description, search.Description)
	merged.Salary = firstNonEmpty(detail.Salary, search.Salary)
	merged.Currency = firstNonEmpty(detail.Currency, search.Currency)
	merged.Location = firstNonEmpty(detail.Location, search.Location)
	merged.AreaName = firstNonEmpty(detail.AreaName, search.AreaName)
	merged.Address = firstNonEmpty(detail.Address, search.Address)
	merged.WorkFormat = firstNonEmpty(detail.WorkFormat, search.WorkFormat)
	merged.Experience = firstNonEmpty(detail.Experience, search.Experience)
	merged.EmploymentType = firstNonEmpty(detail.EmploymentType, search.EmploymentType)
	merged.Schedule = firstNonEmpty(detail.Schedule, search.Schedule)
	merged.URL = firstNonEmpty(detail.URL, search.URL)
	merged.PublishedAt = firstTime(detail.PublishedAt, search.PublishedAt)
	merged.UpdatedAt = firstTime(detail.UpdatedAt, search.UpdatedAt)
	if len(detail.Requirements) > 0 {
		merged.Requirements = append([]string(nil), detail.Requirements...)
	}
	if len(detail.KeySkills) > 0 {
		merged.KeySkills = append([]string(nil), detail.KeySkills...)
	}
	if len(detail.ProfessionalRoles) > 0 {
		merged.ProfessionalRoles = append([]string(nil), detail.ProfessionalRoles...)
	}
	if detail.Metadata != nil {
		merged.Metadata = copyStringMap(search.Metadata)
		for key, value := range detail.Metadata {
			if strings.TrimSpace(value) != "" {
				merged.Metadata[key] = value
			}
		}
	}
	if detail.TotalResponsesCountKnown {
		merged.TotalResponsesCount, merged.TotalResponsesCountKnown = detail.TotalResponsesCount, true
	}
	merged.Archived = detail.Archived || search.Archived
	merged.ResponseLetterRequired = detail.ResponseLetterRequired || search.ResponseLetterRequired
	merged.UserTestPresent = detail.UserTestPresent || search.UserTestPresent
	merged.ResponseURL = firstNonEmpty(detail.ResponseURL, search.ResponseURL)
	return merged
}

func countEnrichedVacancyFields(before, after hhread.VacancyRecord) int {
	count := 0
	if strings.TrimSpace(before.Description) == "" && strings.TrimSpace(after.Description) != "" {
		count++
	}
	if len(before.KeySkills) == 0 && len(after.KeySkills) > 0 {
		count++
	}
	if strings.TrimSpace(before.WorkFormat) == "" && strings.TrimSpace(after.WorkFormat) != "" {
		count++
	}
	if strings.TrimSpace(before.AreaName) == "" && strings.TrimSpace(after.AreaName) != "" {
		count++
	}
	if strings.TrimSpace(before.Experience) == "" && strings.TrimSpace(after.Experience) != "" {
		count++
	}
	if before.PublishedAt.IsZero() && !after.PublishedAt.IsZero() {
		count++
	}
	if before.UpdatedAt.IsZero() && !after.UpdatedAt.IsZero() {
		count++
	}
	if len(before.ProfessionalRoles) == 0 && len(after.ProfessionalRoles) > 0 {
		count++
	}
	return count
}

func inferCompleteness(value vacancy.Vacancy) vacancy.DataCompleteness {
	location := firstNonEmpty(value.Location, value.Area.Name)
	if strings.TrimSpace(value.Description) != "" && (len(value.Requirements) > 0 || len(value.Skills) > 0) && location != "" {
		return vacancy.DataCompletenessFull
	}
	if strings.TrimSpace(value.Description) != "" || len(value.Requirements) > 0 || len(value.Skills) > 0 || location != "" {
		return vacancy.DataCompletenessPartial
	}
	return vacancy.DataCompletenessMinimal
}

// VacancySourceEquivalent compares the canonical provider fingerprint. Local
// IDs, timestamps, match output, and reconciliation evidence are excluded by
// vacancy.Fingerprints, so every persisted provider field has one comparison
// policy across JSON and PostgreSQL imports.
func VacancySourceEquivalent(a, b vacancy.Vacancy) bool {
	return vacancy.Fingerprints(a).SourceFingerprint == vacancy.Fingerprints(b).SourceFingerprint
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
