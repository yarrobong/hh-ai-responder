package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// These wire types intentionally stay private to the API adapter. HH has
// changed optional field shapes over time, so nullable values are represented
// without exposing provider JSON to the normalized read model.
type wireMe struct {
	ID          any    `json:"id"`
	AuthType    string `json:"auth_type"`
	Type        string `json:"type"`
	IsApplicant *bool  `json:"is_applicant"`
}

type wirePage struct {
	Items []wireVacancy `json:"items"`
	Page  int           `json:"page"`
	Pages int           `json:"pages"`
}

type wireResumePage struct {
	Items []wireResume `json:"items"`
}

type wireResume struct {
	ID              any        `json:"id"`
	Hash            string     `json:"hash"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	SkillsText      string     `json:"skills"`
	SkillSet        wireNames  `json:"skill_set"`
	Area            wireNamed  `json:"area"`
	Salary          wireSalary `json:"salary"`
	Experience      any        `json:"experience"`
	TotalExperience any        `json:"total_experience"`
	Employment      wireNamed  `json:"employment"`
	Schedule        wireNamed  `json:"schedule"`
	WorkFormat      wireNames  `json:"work_format"`
	URL             string     `json:"alternate_url"`
	Links           wireLinks  `json:"links"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

type wireVacancy struct {
	ID                     any           `json:"id"`
	Name                   string        `json:"name"`
	Title                  string        `json:"title"`
	Description            string        `json:"description"`
	Employer               wireNamed     `json:"employer"`
	Company                wireNamed     `json:"company"`
	Area                   wireNamed     `json:"area"`
	Address                wireAddress   `json:"address"`
	Salary                 wireSalary    `json:"salary"`
	SalaryRange            wireSalary    `json:"salary_range"`
	Requirements           wireNames     `json:"requirements"`
	Skills                 wireNames     `json:"skills"`
	KeySkills              wireNames     `json:"key_skills"`
	ProfessionalRoles      wireNames     `json:"professional_roles"`
	Experience             wireNamed     `json:"experience"`
	Employment             wireNamed     `json:"employment"`
	Schedule               wireNamed     `json:"schedule"`
	WorkFormat             wireNames     `json:"work_format"`
	Workplace              wireNames     `json:"workplace"`
	WorkFormats            wireNames     `json:"work_formats"`
	URL                    string        `json:"alternate_url"`
	Links                  wireLinks     `json:"links"`
	PublishedAt            string        `json:"published_at"`
	UpdatedAt              string        `json:"updated_at"`
	TotalResponsesCount    *int          `json:"responses_count"`
	Archived               *bool         `json:"archived"`
	ResponseLetterRequired *bool         `json:"response_letter_required"`
	UserTestPresent        *bool         `json:"has_test"`
	TestPresent            *bool         `json:"test_present"`
	ResponseURL            string        `json:"response_url"`
	AlreadyResponded       *bool         `json:"already_responded"`
	Responded              *bool         `json:"responded"`
	Relation               *wireRelation `json:"relation"`
	Relations              *wireRelation `json:"relations"`
}

type wireRelation struct {
	AlreadyResponded *bool  `json:"already_responded"`
	Responded        *bool  `json:"responded"`
	State            string `json:"state"`
}

type wireNamed struct {
	ID    any    `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

func (v *wireNamed) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		*v = wireNamed{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*v = wireNamed{Name: strings.TrimSpace(text)}
		return nil
	}
	type named wireNamed
	var value named
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*v = wireNamed(value)
	return nil
}

type wireNames []string

func (v *wireNames) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		*v = nil
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		var text string
		if textErr := json.Unmarshal(data, &text); textErr != nil {
			return err
		}
		*v = wireNames{strings.TrimSpace(text)}
		return nil
	}
	result := make(wireNames, 0, len(values))
	for _, raw := range values {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if value := strings.TrimSpace(text); value != "" {
				result = append(result, value)
			}
			continue
		}
		var named wireNamed
		if json.Unmarshal(raw, &named) == nil {
			value := named.Name
			if value == "" {
				value = named.Title
			}
			if value == "" {
				value = scalarString(named.ID)
			}
			if value != "" {
				result = append(result, strings.TrimSpace(value))
			}
		}
	}
	*v = result
	return nil
}

type wireAddress struct {
	Raw         string `json:"raw"`
	Text        string `json:"text"`
	Name        string `json:"name"`
	City        string `json:"city"`
	Street      string `json:"street"`
	Building    string `json:"building"`
	Description string `json:"description"`
}

func (v *wireAddress) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		*v = wireAddress{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*v = wireAddress{Raw: strings.TrimSpace(text)}
		return nil
	}
	type address wireAddress
	var value address
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*v = wireAddress(value)
	return nil
}

type wireSalary struct {
	From     any    `json:"from"`
	To       any    `json:"to"`
	Amount   any    `json:"amount"`
	Currency string `json:"currency"`
}

type wireLinks struct {
	Desktop   string `json:"desktop"`
	Alternate string `json:"alternate"`
}

func decodeWire(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode HH API response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode HH API response: multiple JSON values")
		}
		return fmt.Errorf("decode HH API response: %w", err)
	}
	return nil
}

func decodeResumeCollection(data []byte) ([]wireResume, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var values []wireResume
		return values, decodeWire(data, &values)
	}
	var page wireResumePage
	if err := decodeWire(data, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

func scalarString(value any) string {
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
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
