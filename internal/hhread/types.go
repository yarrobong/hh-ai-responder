// Package hhread contains provider-neutral values returned by the HH read
// adapter. It deliberately has no HTTP, persistence, candidate, or AI
// dependencies.
package hhread

import (
	"time"

	"hh-ai-responder/internal/vacancy"
)

// VacancyRecord is a normalized HH vacancy snapshot. The response-count
// known bit is intentionally separate from the integer value: HH can omit or
// return null for the field, and those cases are not explicit zeroes.
type VacancyRecord struct {
	ExternalID                  string
	ID                          int
	Title                       string
	Company                     string
	Description                 string
	Requirements                []string
	KeySkills                   []string
	Salary                      string
	Currency                    string
	Location                    string
	AreaName                    string
	Address                     string
	WorkFormat                  string
	Experience                  string
	EmploymentType              string
	Schedule                    string
	URL                         string
	PublishedAt                 time.Time
	UpdatedAt                   time.Time
	TotalResponsesCount         int
	TotalResponsesCountKnown    bool
	Archived                    bool
	ArchivedKnown               bool
	ResponseLetterRequired      bool
	ResponseLetterRequiredKnown bool
	UserTestPresent             bool
	UserTestPresentKnown        bool
	ResponseURL                 string
	NegotiationsURL             string
	SuitableResumesURL          string
	Relations                   []string
	ClosedForApplicants         bool
	ClosedForApplicantsKnown    bool
	QuickResponsesAllowed       bool
	QuickResponsesAllowedKnown  bool
	AlreadyResponded            *bool
	AlreadyRespondedEvidence    string
	ProfessionalRoles           []string
	Metadata                    map[string]string
}

type VacancyPage struct {
	Items      []VacancyRecord
	NextCursor string
	Found      int
	FoundKnown bool
}

type ApplicationRecord struct {
	Vacancy              *vacancy.Vacancy
	ExternalID           string
	VacancyExternalID    string
	VacancyID            int
	Company              string
	VacancyTitle         string
	VacancyURL           string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ConversationExternal string
	ResumeID             string
	URL                  string
	Metadata             map[string]string
}

type ApplicationPage struct {
	Items      []ApplicationRecord
	NextCursor string
}

// NegotiationCollection is a provider-declared applicant negotiation bucket.
// SubCollections are retained because HH does not guarantee a fixed set of
// collections over time.
type NegotiationCollection struct {
	ID             string
	URL            string
	Total          int
	TotalKnown     bool
	SubCollections []NegotiationCollection
}

// NegotiationCollectionIndex is the read-only response to the vacancy-scoped
// negotiations endpoint. GeneratedCollections are included when HH returns
// them for the request.
type NegotiationCollectionIndex struct {
	Collections          []NegotiationCollection
	GeneratedCollections []NegotiationCollection
	DirectPage           *NegotiationPage
}

// NegotiationPage is one page from a provider negotiation collection.
// Complete is true only when the adapter has established that this is the
// final page according to provider pagination metadata.
type NegotiationPage struct {
	Items        []ApplicationRecord
	Page         int
	Pages        int
	PagesKnown   bool
	Found        int
	FoundKnown   bool
	HasNext      bool
	HasNextKnown bool
	NextURL      string
	Complete     bool
}

// ResumeRecord is a provider-neutral, read-only projection of an own resume.
// Optional fields remain empty when the provider omits or nulls them; an empty
// value is not evidence that the candidate lacks the corresponding fact.
type ResumeRecord struct {
	ID                         string
	Hash                       string
	Title                      string
	Description                string
	Skills                     []string
	Area                       string
	Salary                     string
	Currency                   string
	Experience                 string
	TotalExperienceMonths      int
	TotalExperienceMonthsKnown bool
	EmploymentType             string
	Schedule                   string
	WorkFormat                 string
	URL                        string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type Action struct {
	Kind     string
	ID       string
	Label    string
	Value    string
	Metadata map[string]string
}

// ChatListPage and ChatHistory are small neutral projections used by the
// legacy chat responder compatibility façade. They contain no provider JSON
// and expose no mutation affordance.
type ChatSummary struct {
	ID               int64
	VacancyIDs       []string
	ResumeIDs        []string
	LastMessage      *MessageRecord
	LastActivityTime time.Time
}

type ChatVacancySummary struct {
	ID             int64
	Name           string
	Company        string
	URL            string
	SalaryFrom     *int
	SalaryTo       *int
	SalaryCurrency string
}

type ChatListPage struct {
	NextFrom  string
	Page      int
	PerPage   int
	Pages     int
	Items     []ChatSummary
	Vacancies map[string]ChatVacancySummary
	ResumeIDs map[string]struct{}
}

type ChatHistory struct {
	ID           int64
	Messages     []MessageRecord
	WriteAllowed bool
}

type MessageRecord struct {
	SystemEvent            bool
	ContentUnavailable     bool
	ExternalID             string
	Sender                 string
	Direction              string
	Text                   string
	Timestamp              time.Time
	Metadata               map[string]string
	Actions                []Action
	ParticipantID          string
	ParticipantName        string
	WorkflowApplicantState string
}

type ConversationRecord struct {
	MetadataUnchanged  bool
	ExternalID         string
	VacancyExternalID  string
	VacancyID          int
	Company            string
	VacancyTitle       string
	VacancyDescription string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Metadata           map[string]string
	Messages           []MessageRecord
}

type ConversationPage struct {
	Items                []ConversationRecord
	NextCursor           string
	MetadataChecked      int
	HistoryReused        int
	DetailedChatsFetched int
}
