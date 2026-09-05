package main

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	educationLevelHigher                = "higher"
	educationLevelIncompleteHigher      = "incomplete_higher"
	educationLevelSecondaryProfessional = "secondary_professional"
	educationLevelSecondary             = "secondary"
	educationLevelUnknown               = "unknown"
)

// ResumeFacts contains only facts derived from structured HH resume fields.
// The Known flags are intentionally independent from the values so zero does
// not mean that the candidate is known to have no education or experience.
type ResumeFacts struct {
	ExperienceText             string
	EducationKnown             bool
	EducationLevel             string
	EducationDetails           string
	TotalExperienceMonthsKnown bool
	TotalExperienceMonths      int
}

type resumeExperienceEntry struct {
	StartDate   string          `json:"startDate"`
	EndDate     json.RawMessage `json:"endDate"`
	CompanyName string          `json:"companyName"`
	Position    string          `json:"position"`
	Description string          `json:"description"`
}

type experiencePeriod struct {
	start time.Time
	end   time.Time
}

func parseResumeFacts(data []byte, now time.Time) (ResumeFacts, error) {
	root, err := resumeJSONRoot(data)
	if err != nil {
		return ResumeFacts{}, err
	}
	applicantResume, ok := rawObjectField(root, "applicantResume")
	if !ok {
		return ResumeFacts{}, fmt.Errorf("applicantResume not found in resume")
	}

	facts := ResumeFacts{}
	experienceRaw, experiencePresent := applicantResume["experience"]
	if experiencePresent {
		if strings.TrimSpace(string(experienceRaw)) != "null" {
			var entries []resumeExperienceEntry
			if err := json.Unmarshal(experienceRaw, &entries); err != nil {
				return ResumeFacts{}, fmt.Errorf("failed to parse resume experience: %w", err)
			}
			facts.ExperienceText = formatResumeExperience(entries)
		}
	}

	levels, details := extractEducationSignals(applicantResume)
	facts.EducationDetails = strings.Join(uniqueNonEmpty(details), "; ")
	if len(levels) == 1 {
		facts.EducationKnown = true
		facts.EducationLevel = levels[0]
	} else {
		facts.EducationLevel = educationLevelUnknown
	}

	if months, ok := extractTotalExperienceMonths(applicantResume); ok {
		facts.TotalExperienceMonthsKnown = true
		facts.TotalExperienceMonths = months
	} else if experiencePresent {
		if strings.TrimSpace(string(experienceRaw)) == "null" {
			return facts, nil
		}
		var entries []resumeExperienceEntry
		if err := json.Unmarshal(experienceRaw, &entries); err != nil {
			return ResumeFacts{}, fmt.Errorf("failed to parse resume experience: %w", err)
		}
		facts.TotalExperienceMonths, facts.TotalExperienceMonthsKnown = totalExperienceMonths(entries, now)
	}

	return facts, nil
}

func resumeJSONRoot(data []byte) (map[string]json.RawMessage, error) {
	bodyText := string(data)
	if strings.Contains(bodyText, "{&#34;") {
		bodyText = html.UnescapeString(bodyText)
	}
	idx := strings.Index(bodyText, `{"redirectConfig":`)
	if idx < 0 {
		trimmed := strings.TrimSpace(bodyText)
		if !strings.HasPrefix(trimmed, "{") {
			return nil, fmt.Errorf("redirect config not found on page")
		}
		idx = strings.Index(bodyText, trimmed)
	}
	var root map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(bodyText[idx:]))
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("failed to parse resume: %w", err)
	}
	return root, nil
}

func rawObjectField(object map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool) {
	raw, ok := object[key]
	if !ok {
		return nil, false
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return nil, false
	}
	return result, true
}

func formatResumeExperience(entries []resumeExperienceEntry) string {
	var sb strings.Builder
	for i, exp := range entries {
		if i >= 3 {
			break
		}
		if i > 0 {
			sb.WriteString("\n\n")
		}
		end := "по настоящее время"
		if len(exp.EndDate) > 0 && string(exp.EndDate) != "null" {
			var value string
			if json.Unmarshal(exp.EndDate, &value) == nil {
				end = value
			}
		}
		sb.WriteString(html.UnescapeString(exp.Position))
		sb.WriteString("\n")
		sb.WriteString(html.UnescapeString(exp.CompanyName))
		sb.WriteString("\n")
		sb.WriteString(exp.StartDate)
		sb.WriteString(" - ")
		sb.WriteString(end)
		sb.WriteString("\n\n")
		sb.WriteString(html.UnescapeString(exp.Description))
	}
	return sb.String()
}

func extractTotalExperienceMonths(resume map[string]json.RawMessage) (int, bool) {
	for _, key := range []string{"totalExperienceMonths", "total_experience_months", "experienceMonths"} {
		if raw, ok := resume[key]; ok {
			if months, valid := integerJSONValue(raw); valid && months >= 0 {
				return months, true
			}
		}
	}
	for _, key := range []string{"totalExperience", "total_experience", "experienceTotal"} {
		if raw, ok := resume[key]; ok {
			if months, valid := monthsFromStructuredValue(raw); valid && months >= 0 {
				return months, true
			}
		}
	}
	return 0, false
}

func monthsFromStructuredValue(raw json.RawMessage) (int, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return 0, false
	}
	if years, ok := object["years"]; ok {
		yearsValue, yearsValid := integerJSONValue(years)
		monthsValue := 0
		monthsValid := true
		if months, present := object["months"]; present {
			monthsValue, monthsValid = integerJSONValue(months)
		}
		if yearsValid && monthsValid && yearsValue >= 0 && monthsValue >= 0 {
			return yearsValue*12 + monthsValue, true
		}
	}
	for _, key := range []string{"months", "monthCount", "totalMonths"} {
		if value, ok := object[key]; ok {
			if months, valid := integerJSONValue(value); valid {
				return months, true
			}
		}
	}
	return 0, false
}

func integerJSONValue(raw json.RawMessage) (int, bool) {
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, false
	}
	value, err := strconv.Atoi(string(number))
	return value, err == nil
}

func totalExperienceMonths(entries []resumeExperienceEntry, now time.Time) (int, bool) {
	if len(entries) == 0 {
		return 0, true
	}
	currentDate := dateOnly(now)
	periods := make([]experiencePeriod, 0, len(entries))
	for _, entry := range entries {
		start, ok := parseHHDate(entry.StartDate)
		if !ok {
			return 0, false
		}
		end := currentDate
		if len(entry.EndDate) > 0 && string(entry.EndDate) != "null" {
			var endText string
			if json.Unmarshal(entry.EndDate, &endText) != nil {
				return 0, false
			}
			end, ok = parseHHDate(endText)
			if !ok {
				return 0, false
			}
		}
		if start.After(end) || end.After(currentDate) {
			return 0, false
		}
		periods = append(periods, experiencePeriod{start: start, end: end})
	}

	sort.Slice(periods, func(i, j int) bool { return periods[i].start.Before(periods[j].start) })
	merged := periods[:1]
	for _, period := range periods[1:] {
		last := &merged[len(merged)-1]
		if !period.start.After(last.end) {
			if period.end.After(last.end) {
				last.end = period.end
			}
			continue
		}
		merged = append(merged, period)
	}

	total := 0
	for _, period := range merged {
		total += fullMonthsBetween(period.start, period.end)
	}
	return total, true
}

func parseHHDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func fullMonthsBetween(start, end time.Time) int {
	months := (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
	if end.Day() < start.Day() {
		months--
	}
	if months < 0 {
		return 0
	}
	return months
}

func extractEducationSignals(resume map[string]json.RawMessage) ([]string, []string) {
	var levels []string
	var details []string
	for _, key := range []string{"educationLevel", "education_level"} {
		if raw, ok := resume[key]; ok {
			levels = append(levels, educationLevelSignals(raw, true)...)
			details = append(details, structuredEducationDetails(raw)...)
		}
	}
	for _, key := range []string{"education", "educations", "educationEntries", "education_entries"} {
		if raw, ok := resume[key]; ok {
			entryLevels, entryDetails := educationEntriesSignals(raw)
			levels = append(levels, entryLevels...)
			details = append(details, entryDetails...)
		}
	}
	levels = uniqueNonEmpty(levels)
	return levels, uniqueNonEmpty(details)
}

func educationEntriesSignals(raw json.RawMessage) ([]string, []string) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		entries = []json.RawMessage{raw}
	}
	var levels []string
	var details []string
	for _, entry := range entries {
		var object map[string]json.RawMessage
		if json.Unmarshal(entry, &object) != nil {
			continue
		}
		for _, key := range []string{"level", "educationLevel", "education_level", "educationType", "education_type"} {
			if value, ok := object[key]; ok {
				levels = append(levels, educationLevelSignals(value, true)...)
			}
		}
		for _, key := range []string{"name", "title", "specialty", "speciality", "qualification", "organization", "university", "year"} {
			if value, ok := object[key]; ok {
				details = append(details, structuredScalarDetails(value)...)
			}
		}
	}
	return levels, details
}

func educationLevelSignals(raw json.RawMessage, allowScalar bool) []string {
	if allowScalar {
		if value, ok := rawString(raw); ok {
			if level := normalizeEducationLevel(value); level != educationLevelUnknown {
				return []string{level}
			}
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	var result []string
	for _, key := range []string{"id", "code", "name", "title", "value"} {
		if value, ok := object[key]; ok {
			if text, ok := rawString(value); ok {
				if level := normalizeEducationLevel(text); level != educationLevelUnknown {
					result = append(result, level)
				}
			}
		}
	}
	return uniqueNonEmpty(result)
}

func normalizeEducationLevel(value string) string {
	text := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	text = strings.ReplaceAll(text, "ё", "е")
	compact := strings.ReplaceAll(strings.ReplaceAll(text, "_", ""), "-", "")
	if text == "higher" || strings.Contains(text, "высш") || strings.Contains(text, "бакалав") || strings.Contains(text, "магистр") || strings.Contains(text, "специалитет") {
		if strings.Contains(text, "незакончен") || strings.Contains(text, "неполное") || strings.Contains(text, "incomplete") {
			return educationLevelIncompleteHigher
		}
		return educationLevelHigher
	}
	if compact == "incompletehigher" || strings.Contains(text, "incomplete higher") {
		return educationLevelIncompleteHigher
	}
	if compact == "secondaryprofessional" || strings.Contains(text, "secondary professional") || strings.Contains(text, "средн") && (strings.Contains(text, "профессион") || strings.Contains(text, "специальн")) {
		return educationLevelSecondaryProfessional
	}
	if text == "secondary" || strings.Contains(text, "среднее общее") || text == "среднее" {
		return educationLevelSecondary
	}
	return educationLevelUnknown
}

func structuredEducationDetails(raw json.RawMessage) []string {
	return structuredScalarDetails(raw)
}

func structuredScalarDetails(raw json.RawMessage) []string {
	if text, ok := rawString(raw); ok && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	var result []string
	for _, key := range []string{"id", "code", "name", "title", "value", "specialty", "speciality", "qualification", "organization", "university", "year"} {
		if value, ok := object[key]; ok {
			result = append(result, structuredScalarDetails(value)...)
		}
	}
	return result
}

func rawString(raw json.RawMessage) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	return "", false
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func candidateEducationSummary(candidate CandidateContext) string {
	if !candidate.EducationKnown || strings.TrimSpace(candidate.EducationLevel) == "" {
		return "не передано в CandidateContext; уровень неизвестен"
	}
	if details := strings.TrimSpace(candidate.EducationDetails); details != "" {
		return fmt.Sprintf("%s (%s)", candidate.EducationLevel, details)
	}
	return candidate.EducationLevel
}

func candidateExperienceSummary(candidate CandidateContext) string {
	if !candidate.TotalExperienceMonthsKnown {
		return "не передана; не вычисляй её по датам"
	}
	return fmt.Sprintf("%d months", candidate.TotalExperienceMonths)
}
