package vacancy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"
)

// FingerprintPair contains deterministic hashes of the provider-owned
// vacancy state. SourceFingerprint includes provider fields that may change
// without changing review relevance; MaterialFingerprint contains fields
// that can justify reconsidering a previous user decision.
type FingerprintPair struct {
	SourceFingerprint   string
	MaterialFingerprint string
}

// Fingerprints computes both hashes from the current normalized domain
// vacancy. Local match/recommendation/reconciliation values and local
// timestamps are never included.
func Fingerprints(value Vacancy) FingerprintPair {
	pair, _ := FingerprintsForVersion(value, FingerprintVersion)
	return pair
}

// FingerprintsForVersion computes a supported historical or current pair.
// The legacy contract is intentionally explicit: v1 did not serialize
// professional_roles at all. This lets the upgrade compare an old v1
// baseline with the same provider data before establishing the v2 baseline.
func FingerprintsForVersion(value Vacancy, version int) (FingerprintPair, bool) {
	source, material := fingerprintProjections(value, version)
	if source == nil || material == nil {
		return FingerprintPair{}, false
	}
	return FingerprintPair{SourceFingerprint: hashJSON(source), MaterialFingerprint: hashJSON(material)}, true
}

func fingerprintProjections(value Vacancy, version int) (interface{}, interface{}) {
	if version == LegacyFingerprintVersion {
		return legacySourceProjectionV1From(value), legacyMaterialProjectionV1From(value)
	}
	if version != FingerprintVersion {
		return nil, nil
	}
	source := sourceProjection{
		ExternalID: normalizedText(value.ExternalID), Name: normalizedText(value.Name), Title: normalizedText(value.Title),
		Description: normalizedText(value.Description), Requirements: normalizedStrings(value.Requirements),
		Skills: normalizedStrings(value.Skills), ProfessionalRoles: normalizedStrings(value.ProfessionalRoles), Salary: normalizedText(value.Salary),
		SalaryCurrency: normalizedText(value.SalaryCurrency), Location: normalizedText(value.Location),
		WorkFormat: normalizedText(value.WorkFormat), EmploymentType: normalizedText(value.EmploymentType),
		Source: normalizedText(value.Source), PublishedAt: normalizedTime(value.PublishedAt),
		HHUpdatedAt: normalizedTime(value.HHUpdatedAt), HHMetadata: normalizedMap(value.HHMetadata),
		WorkSchedule: normalizedText(value.WorkSchedule), WorkExperience: normalizedText(value.WorkExperience),
		Links: normalizedURLMap(value.Links), TotalResponsesCount: value.TotalResponsesCount,
		TotalResponsesCountKnown: value.TotalResponsesCountKnown, AreaName: normalizedText(value.Area.Name),
		CompanyID: value.Company.ID, CompanyName: normalizedText(value.Company.Name),
		CompanySiteURL: normalizeURL(value.Company.CompanySiteURL), Compensation: compensationProjection{
			From: intPointerValue(value.Compensation.From), To: intPointerValue(value.Compensation.To),
			Currency: normalizedText(value.Compensation.Currency), Gross: boolPointerValue(value.Compensation.Gross),
		}, CreationTime: normalizedText(value.CreationTime), LastChangeTime: normalizedText(value.LastChangeTime.Value),
		UserLabels: normalizedStrings(value.UserLabels), ResponseLetterRequired: value.ResponseLetterRequired,
		UserTestPresent: value.UserTestPresent, Archived: value.Archived, ResponseURL: normalizeURL(value.ResponseURL),
	}
	material := materialProjection{
		ExternalID: source.ExternalID, Name: source.Name, Title: source.Title,
		Description: source.Description, Requirements: source.Requirements, Skills: source.Skills,
		Salary: source.Salary, SalaryCurrency: source.SalaryCurrency, Location: source.Location,
		WorkFormat: source.WorkFormat, EmploymentType: source.EmploymentType,
		WorkSchedule: source.WorkSchedule, WorkExperience: source.WorkExperience,
		AreaName: source.AreaName, CompanyID: source.CompanyID, CompanyName: source.CompanyName,
		CompanySiteURL: source.CompanySiteURL, Archived: source.Archived,
	}
	return source, material
}

type sourceProjection struct {
	ExternalID               string                 `json:"external_id"`
	Name                     string                 `json:"name"`
	Title                    string                 `json:"title"`
	Description              string                 `json:"description"`
	Requirements             []string               `json:"requirements"`
	Skills                   []string               `json:"skills"`
	ProfessionalRoles        []string               `json:"professional_roles"`
	Salary                   string                 `json:"salary"`
	SalaryCurrency           string                 `json:"salary_currency"`
	Location                 string                 `json:"location"`
	WorkFormat               string                 `json:"work_format"`
	EmploymentType           string                 `json:"employment_type"`
	Source                   string                 `json:"source"`
	PublishedAt              *string                `json:"published_at"`
	HHUpdatedAt              *string                `json:"hh_updated_at"`
	HHMetadata               map[string]string      `json:"hh_metadata"`
	WorkSchedule             string                 `json:"work_schedule"`
	WorkExperience           string                 `json:"work_experience"`
	Links                    map[string]string      `json:"links"`
	TotalResponsesCount      int                    `json:"total_responses_count"`
	TotalResponsesCountKnown bool                   `json:"total_responses_count_known"`
	AreaName                 string                 `json:"area_name"`
	CompanyID                int                    `json:"company_id"`
	CompanyName              string                 `json:"company_name"`
	CompanySiteURL           string                 `json:"company_site_url"`
	Compensation             compensationProjection `json:"compensation"`
	CreationTime             string                 `json:"creation_time"`
	LastChangeTime           string                 `json:"last_change_time"`
	UserLabels               []string               `json:"user_labels"`
	ResponseLetterRequired   bool                   `json:"response_letter_required"`
	UserTestPresent          bool                   `json:"user_test_present"`
	Archived                 bool                   `json:"archived"`
	ResponseURL              string                 `json:"response_url"`
}

type materialProjection struct {
	ExternalID        string   `json:"external_id"`
	Name              string   `json:"name"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Requirements      []string `json:"requirements"`
	Skills            []string `json:"skills"`
	ProfessionalRoles []string `json:"professional_roles"`
	Salary            string   `json:"salary"`
	SalaryCurrency    string   `json:"salary_currency"`
	Location          string   `json:"location"`
	WorkFormat        string   `json:"work_format"`
	EmploymentType    string   `json:"employment_type"`
	WorkSchedule      string   `json:"work_schedule"`
	WorkExperience    string   `json:"work_experience"`
	AreaName          string   `json:"area_name"`
	CompanyID         int      `json:"company_id"`
	CompanyName       string   `json:"company_name"`
	CompanySiteURL    string   `json:"company_site_url"`
	Archived          bool     `json:"archived"`
}

// These projections preserve the exact v1 JSON shape. In particular, a nil
// professional_roles field would not be compatible with v1: the field did
// not exist in the v1 projection and therefore must be omitted entirely.
type legacySourceProjectionV1 struct {
	ExternalID               string                 `json:"external_id"`
	Name                     string                 `json:"name"`
	Title                    string                 `json:"title"`
	Description              string                 `json:"description"`
	Requirements             []string               `json:"requirements"`
	Skills                   []string               `json:"skills"`
	Salary                   string                 `json:"salary"`
	SalaryCurrency           string                 `json:"salary_currency"`
	Location                 string                 `json:"location"`
	WorkFormat               string                 `json:"work_format"`
	EmploymentType           string                 `json:"employment_type"`
	Source                   string                 `json:"source"`
	PublishedAt              *string                `json:"published_at"`
	HHUpdatedAt              *string                `json:"hh_updated_at"`
	HHMetadata               map[string]string      `json:"hh_metadata"`
	WorkSchedule             string                 `json:"work_schedule"`
	WorkExperience           string                 `json:"work_experience"`
	Links                    map[string]string      `json:"links"`
	TotalResponsesCount      int                    `json:"total_responses_count"`
	TotalResponsesCountKnown bool                   `json:"total_responses_count_known"`
	AreaName                 string                 `json:"area_name"`
	CompanyID                int                    `json:"company_id"`
	CompanyName              string                 `json:"company_name"`
	CompanySiteURL           string                 `json:"company_site_url"`
	Compensation             compensationProjection `json:"compensation"`
	CreationTime             string                 `json:"creation_time"`
	LastChangeTime           string                 `json:"last_change_time"`
	UserLabels               []string               `json:"user_labels"`
	ResponseLetterRequired   bool                   `json:"response_letter_required"`
	UserTestPresent          bool                   `json:"user_test_present"`
	Archived                 bool                   `json:"archived"`
	ResponseURL              string                 `json:"response_url"`
}

type legacyMaterialProjectionV1 struct {
	ExternalID     string   `json:"external_id"`
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Requirements   []string `json:"requirements"`
	Skills         []string `json:"skills"`
	Salary         string   `json:"salary"`
	SalaryCurrency string   `json:"salary_currency"`
	Location       string   `json:"location"`
	WorkFormat     string   `json:"work_format"`
	EmploymentType string   `json:"employment_type"`
	WorkSchedule   string   `json:"work_schedule"`
	WorkExperience string   `json:"work_experience"`
	AreaName       string   `json:"area_name"`
	CompanyID      int      `json:"company_id"`
	CompanyName    string   `json:"company_name"`
	CompanySiteURL string   `json:"company_site_url"`
	Archived       bool     `json:"archived"`
}

func legacySourceProjectionV1From(value Vacancy) legacySourceProjectionV1 {
	return legacySourceProjectionV1{
		ExternalID: normalizedText(value.ExternalID), Name: normalizedText(value.Name), Title: normalizedText(value.Title),
		Description: normalizedText(value.Description), Requirements: normalizedStrings(value.Requirements), Skills: normalizedStrings(value.Skills),
		Salary: normalizedText(value.Salary), SalaryCurrency: normalizedText(value.SalaryCurrency), Location: normalizedText(value.Location),
		WorkFormat: normalizedText(value.WorkFormat), EmploymentType: normalizedText(value.EmploymentType), Source: normalizedText(value.Source),
		PublishedAt: normalizedTime(value.PublishedAt), HHUpdatedAt: normalizedTime(value.HHUpdatedAt), HHMetadata: normalizedMap(value.HHMetadata),
		WorkSchedule: normalizedText(value.WorkSchedule), WorkExperience: normalizedText(value.WorkExperience), Links: normalizedURLMap(value.Links),
		TotalResponsesCount: value.TotalResponsesCount, TotalResponsesCountKnown: value.TotalResponsesCountKnown, AreaName: normalizedText(value.Area.Name),
		CompanyID: value.Company.ID, CompanyName: normalizedText(value.Company.Name), CompanySiteURL: normalizeURL(value.Company.CompanySiteURL),
		Compensation: compensationProjection{From: intPointerValue(value.Compensation.From), To: intPointerValue(value.Compensation.To), Currency: normalizedText(value.Compensation.Currency), Gross: boolPointerValue(value.Compensation.Gross)},
		CreationTime: normalizedText(value.CreationTime), LastChangeTime: normalizedText(value.LastChangeTime.Value), UserLabels: normalizedStrings(value.UserLabels),
		ResponseLetterRequired: value.ResponseLetterRequired, UserTestPresent: value.UserTestPresent, Archived: value.Archived, ResponseURL: normalizeURL(value.ResponseURL),
	}
}

func legacyMaterialProjectionV1From(value Vacancy) legacyMaterialProjectionV1 {
	source := legacySourceProjectionV1From(value)
	return legacyMaterialProjectionV1{
		ExternalID: source.ExternalID, Name: source.Name, Title: source.Title, Description: source.Description,
		Requirements: source.Requirements, Skills: source.Skills, Salary: source.Salary, SalaryCurrency: source.SalaryCurrency,
		Location: source.Location, WorkFormat: source.WorkFormat, EmploymentType: source.EmploymentType, WorkSchedule: source.WorkSchedule,
		WorkExperience: source.WorkExperience, AreaName: source.AreaName, CompanyID: source.CompanyID, CompanyName: source.CompanyName,
		CompanySiteURL: source.CompanySiteURL, Archived: source.Archived,
	}
}

type compensationProjection struct {
	From     *int   `json:"from"`
	To       *int   `json:"to"`
	Currency string `json:"currency"`
	Gross    *bool  `json:"gross"`
}

func hashJSON(value interface{}) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizedText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, normalizedText(value))
	}
	sort.Strings(result)
	return result
}

func normalizedMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[normalizedText(key)] = normalizedText(value)
	}
	return result
}

func normalizedURLMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[normalizedText(key)] = normalizeURL(value)
	}
	return result
}

func normalizeURL(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func normalizedTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	result := value.UTC().Format(time.RFC3339Nano)
	return &result
}

func intPointerValue(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func boolPointerValue(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
