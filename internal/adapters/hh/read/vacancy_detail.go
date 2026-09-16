package hhread

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhread"
)

// parseVacancyDetail accepts the HTML bootstrap JSON used by hh.ru as well as
// the smaller JSON fixtures used by the read adapter tests. HH has changed
// names around this projection over time, so aliases are explicit and are
// resolved to one normalized record here.
func parseVacancyDetail(data []byte, baseURL *url.URL, requestedID int) (hhread.VacancyRecord, error) {
	text := html.UnescapeString(string(data))
	root, err := firstJSONObject(text)
	if err != nil {
		return hhread.VacancyRecord{}, fmt.Errorf("parse HH vacancy detail: %w", err)
	}
	view := objectField(root, "vacancyView", "vacancy", "vacancy_view")
	redirect := objectField(root, "redirectConfig", "redirect_config")
	if view == nil && redirect == nil {
		// The public API returns the vacancy object directly, while the web
		// page embeds it under vacancyView/redirectConfig. Accept both shapes.
		view = root
	}
	if view == nil && len(redirect) == 0 {
		return hhread.VacancyRecord{}, errors.New("HH vacancy detail contains no vacancy fields")
	}
	sources := []map[string]json.RawMessage{view, redirect}
	value := hhread.VacancyRecord{ID: requestedID, ExternalID: strconv.Itoa(requestedID), Metadata: map[string]string{"hh_detail_source": "detail_response"}}
	value.Title = firstRawString(sources, "name", "title")
	value.Company = firstNestedString(sources, []string{"company", "employer"}, "name", "visibleName")
	value.Description = firstRawString(sources, "description", "snippet")
	value.AreaName = firstNestedString(sources, []string{"area", "region"}, "name", "title")
	value.Address = addressString(firstRaw(sources, "address"))
	value.Location = firstRawString(sources, "location")
	value.Salary, value.Currency = salaryFields(sources)
	value.KeySkills = namedValues(firstRaw(sources, "keySkills", "key_skills"))
	value.ProfessionalRoles = namedValues(firstRaw(sources, "professionalRoles", "professional_roles", "professionalRoleIds"))
	value.Experience = firstRawString(sources, "workExperience", "experience")
	value.EmploymentType = firstNestedString(sources, []string{"employment", "employmentForm"}, "name", "id", "type")
	value.Schedule = firstRawString(sources, "@workSchedule", "workSchedule", "schedule")
	value.WorkFormat = canonicalWorkFormat(namedValues(firstRaw(sources, "workFormats", "work_format", "workFormat")))
	value.PublishedAt = firstRawTime(sources, "publicationTime", "publishedAt", "published_at")
	value.UpdatedAt = firstRawTime(sources, "lastChangeTime", "updatedAt", "updated_at")
	value.URL = firstNestedString(sources, []string{"links"}, "desktop", "alternate", "alternate_url")
	if value.URL == "" {
		value.URL = firstRawString(sources, "alternateUrl", "alternate_url", "url")
	}
	if value.URL == "" && baseURL != nil {
		value.URL = baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("/vacancy/%d", requestedID)}).String()
	}
	value.ResponseURL = firstRawString(sources, "responseUrl", "response_url")
	value.Archived = firstRawBool(sources, "archived", "isArchived")
	value.ResponseLetterRequired = firstRawBool(sources, "@responseLetterRequired", "responseLetterRequired", "response_letter_required")
	value.UserTestPresent = firstRawObjectPresent(sources, "test", "userTestPresent")
	value.TotalResponsesCount, value.TotalResponsesCountKnown = firstRawInt(sources, "totalResponsesCount", "total_responses_count")
	if value.ID == 0 {
		value.ID = firstRawIntValue(sources, "vacancyId", "id")
		value.ExternalID = strconv.Itoa(value.ID)
	}
	if value.ID <= 0 || value.ID != requestedID {
		return hhread.VacancyRecord{}, fmt.Errorf("HH vacancy detail identity mismatch: got %d, want %d", value.ID, requestedID)
	}
	return value, nil
}

func firstJSONObject(text string) (map[string]json.RawMessage, error) {
	markers := []string{`{"redirectConfig":`, `{"vacancyView":`, `{"vacancy":`}
	for _, marker := range markers {
		if idx := strings.Index(text, marker); idx >= 0 {
			var root map[string]json.RawMessage
			if err := json.Unmarshal([]byte(text[idx:]), &root); err == nil {
				return root, nil
			}
			decoder := json.NewDecoder(strings.NewReader(text[idx:]))
			if err := decoder.Decode(&root); err == nil {
				return root, nil
			}
		}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace([]byte(text)), &root); err != nil {
		return nil, err
	}
	return root, nil
}

func objectField(root map[string]json.RawMessage, names ...string) map[string]json.RawMessage {
	for _, name := range names {
		var value map[string]json.RawMessage
		if raw, ok := root[name]; ok && json.Unmarshal(raw, &value) == nil && value != nil {
			return value
		}
	}
	return nil
}

func firstRaw(sources []map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, source := range sources {
		for _, name := range names {
			if raw, ok := source[name]; ok {
				return raw
			}
		}
	}
	return nil
}

func firstRawString(sources []map[string]json.RawMessage, names ...string) string {
	raw := firstRaw(sources, names...)
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return nestedString(raw, "value", "name", "title", "text", "id")
}

func firstNestedString(sources []map[string]json.RawMessage, objects []string, names ...string) string {
	for _, object := range objects {
		if value := nestedString(firstRaw(sources, object), names...); value != "" {
			return value
		}
	}
	return ""
}

func nestedString(raw json.RawMessage, names ...string) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, name := range names {
		if result := firstRawString([]map[string]json.RawMessage{object}, name); result != "" {
			return result
		}
	}
	return ""
}

func addressString(raw json.RawMessage) string {
	if result := nestedString(raw, "text", "value", "name"); result != "" {
		return result
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	parts := []string{}
	for _, name := range []string{"city", "street", "building", "description"} {
		if value := nestedString(object[name], "name", "value", "text"); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func salaryFields(sources []map[string]json.RawMessage) (string, string) {
	raw := firstRaw(sources, "salary_range", "salary", "compensation")
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "", ""
	}
	from := nestedString(object["from"], "value")
	to := nestedString(object["to"], "value")
	currency := firstRawString([]map[string]json.RawMessage{object}, "currency", "currencyCode")
	salary := ""
	if from != "" && to != "" {
		salary = from + "–" + to
	} else if from != "" {
		salary = from
	} else {
		salary = to
	}
	return salary, currency
}

func namedValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		if value := nestedString(raw, "name", "title", "id"); value != "" {
			return []string{value}
		}
		return nil
	}
	result := []string{}
	for _, item := range values {
		var number json.Number
		if json.Unmarshal(item, &number) == nil && strings.TrimSpace(number.String()) != "" {
			result = append(result, number.String())
			continue
		}
		if value := nestedString(item, "name", "title", "text", "value", "id"); value != "" {
			result = append(result, value)
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(item, &object) == nil {
			for _, key := range []string{"keySkills", "professionalRoleId", "professional_role_id", "workFormatsElement"} {
				result = append(result, namedValues(object[key])...)
			}
		}
	}
	return uniqueStrings(result)
}

func canonicalWorkFormat(values []string) string {
	remote, office := false, false
	for _, value := range values {
		text := strings.ToLower(strings.TrimSpace(value))
		switch {
		case strings.Contains(text, "remote"), strings.Contains(text, "удален"), strings.Contains(text, "дистанцион"):
			remote = true
		case strings.Contains(text, "office"), strings.Contains(text, "офис"), strings.Contains(text, "on_site"), strings.Contains(text, "onsite"), strings.Contains(text, "на месте"):
			office = true
		case text == "hybrid", strings.Contains(text, "гибрид"):
			remote, office = true, true
		}
	}
	if remote && office {
		return "hybrid"
	}
	if remote {
		return "remote"
	}
	if office {
		return "office"
	}
	return ""
}

func firstRawTime(sources []map[string]json.RawMessage, names ...string) time.Time {
	raw := firstRaw(sources, names...)
	if value := nestedString(raw, "$", "value", "date"); value != "" {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05 -0700"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func firstRawBool(sources []map[string]json.RawMessage, names ...string) bool {
	var value bool
	return json.Unmarshal(firstRaw(sources, names...), &value) == nil && value
}

func firstRawObjectPresent(sources []map[string]json.RawMessage, names ...string) bool {
	raw := firstRaw(sources, names...)
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return true
}

func firstRawInt(sources []map[string]json.RawMessage, names ...string) (int, bool) {
	raw := firstRaw(sources, names...)
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value int
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	return 0, false
}

func firstRawIntValue(sources []map[string]json.RawMessage, names ...string) int {
	value, _ := firstRawInt(sources, names...)
	return value
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}
