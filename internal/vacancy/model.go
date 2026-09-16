package vacancy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Vacancy is the current career-domain vacancy model. Its legacy HH-shaped
// fields and JSON tags are retained until the HH adapter boundary is moved.
type Vacancy struct {
	ID                        int                        `json:"id"`
	ExternalID                string                     `json:"external_id,omitempty"`
	Name                      string                     `json:"name"`
	Title                     string                     `json:"title,omitempty"`
	Description               string                     `json:"description,omitempty"`
	Requirements              []string                   `json:"requirements,omitempty"`
	Skills                    []string                   `json:"skills,omitempty"`
	ProfessionalRoles         []string                   `json:"professional_roles,omitempty"`
	Salary                    string                     `json:"salary,omitempty"`
	SalaryCurrency            string                     `json:"salary_currency,omitempty"`
	Location                  string                     `json:"location,omitempty"`
	WorkFormat                string                     `json:"work_format,omitempty"`
	EmploymentType            string                     `json:"employment_type,omitempty"`
	Source                    string                     `json:"source,omitempty"`
	PublishedAt               time.Time                  `json:"published_at,omitempty"`
	HHUpdatedAt               time.Time                  `json:"hh_updated_at,omitempty"`
	HHMetadata                map[string]string          `json:"hh_metadata,omitempty"`
	CreatedAt                 time.Time                  `json:"created_at,omitempty"`
	UpdatedAt                 time.Time                  `json:"updated_at,omitempty"`
	WorkSchedule              string                     `json:"@workSchedule"`
	WorkExperience            string                     `json:"workExperience"`
	Links                     map[string]string          `json:"links"`
	TotalResponsesCount       int                        `json:"totalResponsesCount"`
	Area                      NamedObject                `json:"area"`
	Company                   Company                    `json:"company"`
	Compensation              Compensation               `json:"compensation"`
	CreationTime              string                     `json:"creationTime"`
	LastChangeTime            ChangeTime                 `json:"lastChangeTime"`
	UserLabels                []string                   `json:"userLabels"`
	ResponseLetterRequired    bool                       `json:"@responseLetterRequired"`
	UserTestPresent           bool                       `json:"userTestPresent"`
	Archived                  bool                       `json:"archived"`
	ResponseURL               string                     `json:"response_url"`
	TotalResponsesCountKnown  bool                       `json:"-"`
	MatchResult               *MatchResult               `json:"match_result,omitempty"`
	ApplicationRecommendation *ApplicationRecommendation `json:"application_recommendation,omitempty"`
	DataCompleteness          DataCompleteness           `json:"data_completeness,omitempty"`
	ReconciliationEvidence    []ReconciliationEvidence   `json:"reconciliation_evidence,omitempty"`
}

// MarshalJSON preserves both the domain-shaped id and HH's legacy vacancyId.
func (v Vacancy) MarshalJSON() ([]byte, error) {
	type vacancyAlias Vacancy
	raw, err := json.Marshal(vacancyAlias(v))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	id, err := json.Marshal(v.ID)
	if err != nil {
		return nil, err
	}
	fields["id"] = id
	fields["vacancyId"] = id
	return json.Marshal(fields)
}

// UnmarshalJSON tracks whether HH actually supplied the response count. A
// zero count is valid data; an absent count must not be presented as zero.
func (v *Vacancy) UnmarshalJSON(data []byte) error {
	type vacancyAlias Vacancy
	var decoded vacancyAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*v = Vacancy(decoded)
	if decoded.ID == 0 {
		if rawID, ok := fields["vacancyId"]; ok && !bytes.Equal(bytes.TrimSpace(rawID), []byte("null")) {
			if err := json.Unmarshal(rawID, &v.ID); err != nil {
				return err
			}
		}
	}
	responseCount, ok := fields["totalResponsesCount"]
	v.TotalResponsesCountKnown = ok && !bytes.Equal(bytes.TrimSpace(responseCount), []byte("null"))
	if len(v.Skills) == 0 {
		v.Skills = jsonStringValues(fields["keySkills"], fields["key_skills"])
	}
	if len(v.ProfessionalRoles) == 0 {
		v.ProfessionalRoles = jsonStringValues(fields["professionalRoles"], fields["professional_roles"], fields["professionalRoleIds"])
	}
	if v.WorkFormat == "" {
		v.WorkFormat = canonicalWorkFormat(jsonStringValues(fields["workFormat"], fields["work_format"], fields["workFormats"]))
	}
	if v.PublishedAt.IsZero() {
		v.PublishedAt = jsonProviderTime(fields["publicationTime"], fields["publishedAt"], fields["published_at"])
	}
	if v.HHUpdatedAt.IsZero() {
		// updated_at is the local persistence timestamp and must never be
		// promoted to a provider update timestamp.
		v.HHUpdatedAt = jsonProviderTime(fields["lastChangeTime"], fields["updatedAt"])
	}
	return nil
}

func jsonStringValues(rawValues ...json.RawMessage) []string {
	result := []string{}
	for _, raw := range rawValues {
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var single string
		if json.Unmarshal(raw, &single) == nil && strings.TrimSpace(single) != "" {
			result = append(result, strings.TrimSpace(single))
			continue
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil && strings.TrimSpace(number.String()) != "" {
			result = append(result, number.String())
			continue
		}
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			continue
		}
		for _, item := range values {
			if json.Unmarshal(item, &single) == nil && strings.TrimSpace(single) != "" {
				result = append(result, strings.TrimSpace(single))
				continue
			}
			var itemNumber json.Number
			if json.Unmarshal(item, &itemNumber) == nil && strings.TrimSpace(itemNumber.String()) != "" {
				result = append(result, itemNumber.String())
				continue
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(item, &object) != nil {
				continue
			}
			matched := false
			for _, key := range []string{"name", "title", "text", "value", "id"} {
				if json.Unmarshal(object[key], &single) == nil && strings.TrimSpace(single) != "" {
					result = append(result, strings.TrimSpace(single))
					matched = true
					break
				}
			}
			if !matched {
				for _, key := range []string{"keySkills", "key_skills", "professionalRoleId", "professional_role_id", "workFormatsElement", "work_formats_element"} {
					result = append(result, jsonStringValues(object[key])...)
				}
			}
		}
	}
	result = uniqueJSONStrings(result)
	if len(result) == 0 {
		return nil
	}
	return result
}

func canonicalWorkFormat(values []string) string {
	remote, office := false, false
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch {
		case strings.Contains(value, "remote"), strings.Contains(value, "удален"), strings.Contains(value, "дистанцион"):
			remote = true
		case strings.Contains(value, "office"), strings.Contains(value, "офис"), strings.Contains(value, "on_site"), strings.Contains(value, "onsite"), strings.Contains(value, "на месте"):
			office = true
		case value == "hybrid", strings.Contains(value, "гибрид"):
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

func jsonProviderTime(rawValues ...json.RawMessage) time.Time {
	for _, raw := range rawValues {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) == nil {
				for _, key := range []string{"$", "value", "date"} {
					if json.Unmarshal(object[key], &value) == nil {
						break
					}
				}
			}
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700"} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func uniqueJSONStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

type NamedObject struct {
	Name string `json:"name"`
}

type Company struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	CompanySiteURL string `json:"companySiteUrl"`
}

// Compensation preserves the current HH response shape, including the
// unexported-in-JSON marker for noCompensation payloads.
type Compensation struct {
	From     *int        `json:"from,omitempty"`
	To       *int        `json:"to,omitempty"`
	Currency string      `json:"currencyCode,omitempty"`
	Gross    *bool       `json:"gross,omitempty"`
	Raw      interface{} `json:"-"`
}

type ChangeTime struct {
	Value string `json:"$"`
}

// FormatCompensation formats a vacancy compensation for existing UI and
// prompt consumers. It contains no transport or persistence behavior.
func FormatCompensation(c *Compensation) string {
	if c == nil {
		return ""
	}
	if c.Raw != nil && c.From == nil && c.To == nil {
		return ""
	}

	var fromStr, toStr string
	if c.From != nil {
		fromStr = fmt.Sprintf("%d", *c.From)
	}
	if c.To != nil {
		toStr = fmt.Sprintf("%d", *c.To)
	}

	cur := strings.TrimSpace(c.Currency)
	switch {
	case c.From != nil && c.To != nil:
		return fmt.Sprintf("%s-%s %s", fromStr, toStr, cur)
	case c.From != nil && c.To == nil:
		return fmt.Sprintf("%s+ %s", fromStr, cur)
	case c.From == nil && c.To != nil:
		return fmt.Sprintf("0-%s %s", toStr, cur)
	default:
		return ""
	}
}

// MatchResult is deterministic matching output attached to a vacancy. It is
// temporarily retained here as a compatibility value type for application
// persistence until the application boundary is extracted.
type MatchResult struct {
	Score           int                        `json:"score"`
	Confidence      float64                    `json:"confidence"`
	MatchedSkills   []string                   `json:"matched_skills"`
	UnknownSkills   []string                   `json:"unknown_skills"`
	MatchedRoles    []string                   `json:"matched_roles"`
	MatchedProjects []string                   `json:"matched_projects"`
	MissingSkills   []string                   `json:"missing_skills"`
	Risks           []string                   `json:"risks"`
	RiskDetails     []MatchRisk                `json:"risk_details,omitempty"`
	ExperienceNote  string                     `json:"experience_note,omitempty"`
	Explanation     string                     `json:"explanation"`
	Recommendations []string                   `json:"recommendations"`
	Recommendation  *ApplicationRecommendation `json:"recommendation,omitempty"`
}

// Validate checks the persisted matching value. It is exported temporarily
// because the application package still persists this vacancy-owned value.
func (m MatchResult) Validate() error {
	if m.Score < 0 || m.Score > 100 {
		return errors.New("match result score must be between 0 and 100")
	}
	if m.Confidence < 0 || m.Confidence > 1 {
		return errors.New("match result confidence must be between 0 and 1")
	}
	if m.Recommendation != nil {
		switch m.Recommendation.Decision {
		case RecommendationApply, RecommendationMaybe, RecommendationSkip:
		default:
			return errors.New("invalid application recommendation")
		}
		if strings.TrimSpace(m.Recommendation.Reason) == "" {
			return errors.New("application recommendation reason is required")
		}
	}
	return nil
}

// Normalize fills collection fields used by existing persistence consumers.
func (m *MatchResult) Normalize() {
	if m == nil {
		return
	}
	if m.MatchedSkills == nil {
		m.MatchedSkills = []string{}
	}
	if m.UnknownSkills == nil {
		m.UnknownSkills = []string{}
	}
	if m.MatchedRoles == nil {
		m.MatchedRoles = []string{}
	}
	if m.MatchedProjects == nil {
		m.MatchedProjects = []string{}
	}
	if m.MissingSkills == nil {
		m.MissingSkills = []string{}
	}
	if m.Risks == nil {
		m.Risks = []string{}
	}
	if m.Recommendations == nil {
		m.Recommendations = []string{}
	}
}

type MatchRisk struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type RecommendationDecision string

const (
	RecommendationApply RecommendationDecision = "apply"
	RecommendationMaybe RecommendationDecision = "maybe"
	RecommendationSkip  RecommendationDecision = "skip"
)

type ApplicationRecommendation struct {
	Decision RecommendationDecision `json:"decision"`
	Reason   string                 `json:"reason"`
}

// DataCompleteness describes how much trusted vacancy data is available.
type DataCompleteness string

const (
	DataCompletenessFull    DataCompleteness = "full"
	DataCompletenessPartial DataCompleteness = "partial"
	DataCompletenessMinimal DataCompleteness = "minimal"
)

type ReconciliationEvidence struct {
	Method       string    `json:"method"`
	Source       string    `json:"source"`
	Confidence   float64   `json:"confidence"`
	ReconciledAt time.Time `json:"reconciled_at"`
}
