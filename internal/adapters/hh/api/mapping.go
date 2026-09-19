package api

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
)

// UserMetadata contains only safe current-user metadata needed by transport
// selection and doctor-like callers.
type UserMetadata struct {
	ID       string
	AuthType string
}

// CapabilityError is returned when the API adapter cannot prove that an API
// operation has the applicant semantics required by the read model.
type CapabilityError struct {
	Capability string
	Reason     string
}

func (e *CapabilityError) Error() string {
	if e == nil {
		return "HH API capability is unavailable"
	}
	if e.Reason == "" {
		return "HH API capability unavailable: " + e.Capability
	}
	return "HH API capability unavailable: " + e.Capability + " (" + e.Reason + ")"
}

func unsupportedCapability(name, reason string) error {
	return &CapabilityError{Capability: name, Reason: reason}
}

func mapUser(value wireMe) UserMetadata {
	authType := strings.TrimSpace(value.AuthType)
	if authType == "" {
		authType = strings.TrimSpace(value.Type)
	}
	if authType == "" && value.IsApplicant != nil && *value.IsApplicant {
		authType = "applicant"
	}
	return UserMetadata{ID: scalarString(value.ID), AuthType: authType}
}

func mapResumeWire(value wireResume) (hhread.ResumeRecord, error) {
	result := hhread.ResumeRecord{
		ID: scalarString(value.ID), Hash: strings.TrimSpace(value.Hash), Title: strings.TrimSpace(value.Title),
		Description: strings.TrimSpace(value.Description), Skills: append([]string(nil), value.SkillSet...), Area: namedValue(value.Area),
		Salary: salaryValue(value.Salary), Currency: strings.TrimSpace(value.Salary.Currency), Experience: resumeExperienceValue(value.Experience),
		EmploymentType: namedValue(value.Employment), Schedule: namedValue(value.Schedule), WorkFormat: canonicalWorkFormat(value.WorkFormat),
		URL: firstNonEmpty(value.URL, value.Links.Alternate, value.Links.Desktop), CreatedAt: parseAPITime(value.CreatedAt), UpdatedAt: parseAPITime(value.UpdatedAt),
	}
	result.Skills = append([]string(nil), value.SkillSet...)
	result.TotalExperienceMonths, result.TotalExperienceMonthsKnown = totalExperienceMonths(value.TotalExperience)
	if result.Experience == "" && result.TotalExperienceMonthsKnown {
		result.Experience = strconv.Itoa(result.TotalExperienceMonths)
	}
	if result.ID == "" {
		return hhread.ResumeRecord{}, errors.New("HH API resume has no id")
	}
	return result, nil
}

func mapVacancyWire(value wireVacancy) (hhread.VacancyRecord, error) {
	id, err := strconv.Atoi(scalarString(value.ID))
	if err != nil || id <= 0 {
		return hhread.VacancyRecord{}, errors.New("HH API vacancy has an invalid id")
	}
	company := value.Employer
	if namedValue(company) == "" {
		company = value.Company
	}
	roles := append([]string(nil), value.ProfessionalRoles...)
	skills := append([]string(nil), value.KeySkills...)
	if len(skills) == 0 {
		skills = append([]string(nil), value.Skills...)
	} else {
		skills = appendUnique(skills, value.Skills...)
	}
	salary := value.Salary
	if salaryValue(salary) == "" {
		salary = value.SalaryRange
	}
	result := hhread.VacancyRecord{
		ExternalID: scalarString(value.ID), ID: id, Title: firstNonEmpty(value.Name, value.Title), Description: strings.TrimSpace(value.Description),
		Company: namedValue(company), Requirements: append([]string(nil), value.Requirements...), KeySkills: skills,
		Salary: salaryValue(salary), Currency: strings.TrimSpace(salary.Currency), Location: namedValue(value.Area), AreaName: namedValue(value.Area),
		Address: addressValue(value.Address), WorkFormat: canonicalWorkFormat(append(append(append(wireWorkFormats{}, value.WorkFormat...), value.Workplace...), value.WorkFormats...)), Experience: namedIDOrValue(value.Experience),
		EmploymentType: namedValue(value.Employment), Schedule: namedValue(value.Schedule), URL: firstNonEmpty(value.URL, value.Links.Desktop, value.Links.Alternate),
		PublishedAt: parseAPITime(value.PublishedAt), UpdatedAt: parseAPITime(value.UpdatedAt), ProfessionalRoles: roles,
		Metadata: map[string]string{"hh_read_source": "api"},
	}
	if value.TotalResponsesCount != nil {
		result.TotalResponsesCount = *value.TotalResponsesCount
		result.TotalResponsesCountKnown = true
	}
	if value.Archived != nil {
		result.Archived, result.ArchivedKnown = *value.Archived, true
	}
	if value.ResponseLetterRequired != nil {
		result.ResponseLetterRequired, result.ResponseLetterRequiredKnown = *value.ResponseLetterRequired, true
	}
	testPresent := value.UserTestPresent
	if testPresent == nil {
		testPresent = value.TestPresent
	}
	if testPresent != nil {
		result.UserTestPresent, result.UserTestPresentKnown = *testPresent, true
	}
	result.ResponseURL = strings.TrimSpace(value.ResponseURL)
	if responded, evidence, relationErr := relationValue(value); relationErr == nil && responded != nil {
		copy := *responded
		result.AlreadyResponded = &copy
		result.AlreadyRespondedEvidence = evidence
	}
	return result, nil
}

func mapVacancyWireRequiringRelation(value wireVacancy) (hhread.VacancyRecord, error) {
	result, err := mapVacancyWire(value)
	if err != nil {
		return hhread.VacancyRecord{}, err
	}
	responded, evidence, relationErr := relationValue(value)
	if relationErr != nil {
		return hhread.VacancyRecord{}, relationErr
	}
	if responded == nil {
		return hhread.VacancyRecord{}, unsupportedCapability("duplicate-state", "applicant relation evidence is missing")
	}
	result.AlreadyResponded = responded
	result.AlreadyRespondedEvidence = evidence
	return result, nil
}

func relationValue(value wireVacancy) (*bool, string, error) {
	type candidate struct {
		value    bool
		evidence string
	}
	candidates := make([]candidate, 0, 6)
	if value.AlreadyResponded != nil {
		candidates = append(candidates, candidate{*value.AlreadyResponded, "vacancy.already_responded"})
	}
	if value.Responded != nil {
		candidates = append(candidates, candidate{*value.Responded, "vacancy.responded"})
	}
	for _, relation := range []*wireRelation{value.Relation, value.Relations} {
		if relation == nil {
			continue
		}
		if relation.AlreadyResponded != nil {
			candidates = append(candidates, candidate{*relation.AlreadyResponded, "vacancy.relation.already_responded"})
		}
		if relation.Responded != nil {
			candidates = append(candidates, candidate{*relation.Responded, "vacancy.relation.responded"})
		}
	}
	if len(candidates) == 0 {
		return nil, "", nil
	}
	for _, value := range candidates[1:] {
		if value.value != candidates[0].value {
			return nil, "", unsupportedCapability("duplicate-state", "applicant relation evidence is conflicting")
		}
	}
	result := candidates[0].value
	return &result, candidates[0].evidence, nil
}

func salaryValue(value wireSalary) string {
	from, to, amount := scalarString(value.From), scalarString(value.To), scalarString(value.Amount)
	switch {
	case from != "" && to != "":
		return from + "–" + to
	case from != "":
		return from
	case amount != "":
		return amount
	default:
		return to
	}
}

func resumeExperienceValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case wireNamed:
		return namedIDOrValue(typed)
	case map[string]any:
		return firstNonEmpty(scalarString(typed["id"]), scalarString(typed["name"]), scalarString(typed["title"]))
	default:
		return ""
	}
}

func totalExperienceMonths(value any) (int, bool) {
	if object, ok := value.(map[string]any); ok {
		value = object["months"]
	}
	text := scalarString(value)
	months, err := strconv.Atoi(text)
	if err != nil || months < 0 {
		return 0, false
	}
	return months, true
}

func namedValue(value wireNamed) string {
	return firstNonEmpty(value.Name, value.Title, scalarString(value.ID))
}

func namedIDOrValue(value wireNamed) string {
	return firstNonEmpty(scalarString(value.ID), value.Name, value.Title)
}

func addressValue(value wireAddress) string {
	if result := firstNonEmpty(value.Raw, value.Text, value.Name); result != "" {
		return result
	}
	parts := []string{}
	for _, part := range []string{value.City, value.Street, value.Building, value.Description} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return strings.Join(parts, ", ")
}

func canonicalWorkFormat(values wireWorkFormats) string {
	hasRemote, hasOffice := false, false
	for _, format := range values {
		for _, raw := range []string{format.Code, format.Slug, scalarString(format.ID), format.Value, format.Name, format.Title} {
			value := strings.ToLower(strings.TrimSpace(raw))
			switch {
			case value == "":
				continue
			case strings.Contains(value, "hybrid"), strings.Contains(value, "гибрид"):
				return "hybrid"
			case strings.Contains(value, "remote"), strings.Contains(value, "дистан"), strings.Contains(value, "удален"), strings.Contains(value, "удалён"), strings.Contains(value, "из дома"), strings.Contains(value, "на дому"), value == "home", value == "at_home", value == "from_home":
				hasRemote = true
			case strings.Contains(value, "office"), strings.Contains(value, "onsite"), strings.Contains(value, "on_site"), strings.Contains(value, "on-site"), strings.Contains(value, "workplace"), strings.Contains(value, "employer"), strings.Contains(value, "на месте работодателя"), strings.Contains(value, "на территории работодателя"), strings.Contains(value, "в офисе"), strings.Contains(value, "офис"):
				hasOffice = true
			}
		}
	}
	if hasRemote && hasOffice {
		return "hybrid"
	}
	if hasRemote {
		return "remote"
	}
	if hasOffice {
		return "office"
	}
	return ""
}

func parseAPITime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05 -0700"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value = strings.TrimSpace(value); value != "" {
			if _, ok := seen[value]; !ok {
				values = append(values, value)
				seen[value] = struct{}{}
			}
		}
	}
	return values
}

func apiVacancyQuery(searchParams url.Values, page int) url.Values {
	result := url.Values{}
	for _, key := range []string{"text", "area", "salary", "currency", "date_from", "date_to", "employment", "schedule", "experience", "professional_role", "work_format", "search_field", "order_by", "clusters", "only_with_salary", "part_time", "accept_temporary", "label", "locale", "host", "premium", "no_magic", "responses_count_enabled"} {
		for _, value := range searchParams[key] {
			result.Add(key, value)
		}
	}
	copyQueryAlias(result, searchParams, "period", "search_period")
	copyQueryAlias(result, searchParams, "per_page", "items_on_page")
	result.Set("page", strconv.Itoa(page))
	return result
}

func copyQueryAlias(target, source url.Values, targetKey, sourceKey string) {
	if len(target[targetKey]) > 0 {
		return
	}
	for _, value := range source[sourceKey] {
		target.Add(targetKey, value)
	}
}

func cloneAPIValues(value url.Values) url.Values {
	if value == nil {
		return nil
	}
	result := make(url.Values, len(value))
	for key, values := range value {
		result[key] = append([]string(nil), values...)
	}
	return result
}
