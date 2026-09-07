package main

import (
	"errors"
	"strings"
	"time"
)

const JobApplicationsFilename = "job_applications.json"

type ApplicationStatus string

const (
	ApplicationDiscovered      ApplicationStatus = "discovered"
	ApplicationAnalyzed        ApplicationStatus = "analyzed"
	ApplicationShortlisted     ApplicationStatus = "shortlisted"
	ApplicationApplied         ApplicationStatus = "applied"
	ApplicationEmployerReplied ApplicationStatus = "employer_replied"
	ApplicationInterview       ApplicationStatus = "interview"
	ApplicationOffer           ApplicationStatus = "offer"
	ApplicationRejected        ApplicationStatus = "rejected"
	ApplicationArchived        ApplicationStatus = "archived"
	ApplicationUnknown         ApplicationStatus = "unknown"
)

type ApplicationSource string

const (
	ApplicationSourceHH     ApplicationSource = "hh"
	ApplicationSourceManual ApplicationSource = "manual"
)

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

// MatchRisk is structured risk metadata. Risks remains a []string for JSON
// and source compatibility with the existing application pipeline.
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

type JobApplication struct {
	FollowUpState          ConversationFollowUpState `json:"follow_up_state,omitempty"`
	ID                     string                    `json:"id"`
	VacancyID              int                       `json:"vacancy_id"`
	ExternalID             string                    `json:"external_id,omitempty"`
	CompanyName            string                    `json:"company_name"`
	VacancyTitle           string                    `json:"vacancy_title"`
	VacancyURL             string                    `json:"vacancy_url,omitempty"`
	Source                 ApplicationSource         `json:"source"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
	Status                 ApplicationStatus         `json:"status"`
	MatchResult            *MatchResult              `json:"match_result,omitempty"`
	ConversationID         string                    `json:"conversation_id,omitempty"`
	Notes                  string                    `json:"notes,omitempty"`
	NextAction             string                    `json:"next_action,omitempty"`
	RawStatus              string                    `json:"raw_status,omitempty"`
	HHMetadata             map[string]string         `json:"hh_metadata,omitempty"`
	Partial                bool                      `json:"partial,omitempty"`
	DataCompleteness       DataCompleteness          `json:"data_completeness,omitempty"`
	ReconciliationEvidence []ReconciliationEvidence  `json:"reconciliation_evidence,omitempty"`
}

type ApplicationEventType string

const (
	ApplicationEventCreated            ApplicationEventType = "created"
	ApplicationEventMatched            ApplicationEventType = "matched"
	ApplicationEventApplied            ApplicationEventType = "applied"
	ApplicationEventMessageReceived    ApplicationEventType = "message_received"
	ApplicationEventInterviewScheduled ApplicationEventType = "interview_scheduled"
	ApplicationEventStatusChanged      ApplicationEventType = "status_changed"
	ApplicationEventRejected           ApplicationEventType = "rejected"
	ApplicationEventOfferReceived      ApplicationEventType = "offer_received"
	ApplicationEventFollowUpSent       ApplicationEventType = "follow_up_sent"
	ApplicationEventLinkedConversation ApplicationEventType = "application_linked_to_conversation"
	ApplicationEventLinkedVacancy      ApplicationEventType = "application_linked_to_vacancy"
	ApplicationEventPartialCreated     ApplicationEventType = "partial_application_created"
	ApplicationEventVacancyEnriched    ApplicationEventType = "vacancy_enriched"
	ApplicationEventMatchBackfilled    ApplicationEventType = "match_backfilled"
)

type ApplicationEvent struct {
	ID            string               `json:"id"`
	ApplicationID string               `json:"application_id"`
	Timestamp     time.Time            `json:"timestamp"`
	Type          ApplicationEventType `json:"type"`
	Description   string               `json:"description"`
}

type NextAction string

const (
	NextActionWaitingCandidateReply NextAction = "waiting_candidate_reply"
	NextActionWaitingEmployerReply  NextAction = "waiting_employer_reply"
	NextActionPrepareInterview      NextAction = "prepare_interview"
	NextActionFollowUpPossible      NextAction = "follow_up_possible"
)

type ApplicationStats struct {
	Total          int     `json:"total"`
	Discovered     int     `json:"discovered"`
	Applied        int     `json:"applied"`
	WaitingReply   int     `json:"waiting_reply"`
	Interviews     int     `json:"interviews"`
	Offers         int     `json:"offers"`
	Rejected       int     `json:"rejected"`
	ResponseRate   float64 `json:"response_rate"`
	ConversionRate float64 `json:"conversion_rate"`
}

// ApplicationStatistics is kept as a descriptive alias for callers that use
// the full domain name.
type ApplicationStatistics = ApplicationStats

// ApplicationContext is a read-only aggregate. Empty MatchResult and
// Conversation mean that the corresponding optional link has not been saved.
type ApplicationContext struct {
	Application       JobApplication            `json:"application"`
	MatchResult       MatchResult               `json:"match_result"`
	Conversation      EmployerConversation      `json:"conversation"`
	Timeline          []ApplicationEvent        `json:"timeline"`
	CandidateContext  CandidateContext          `json:"candidate_context"`
	RelevantExamples  []SafeSemanticSelection   `json:"relevant_examples,omitempty"`
	RelevantKnowledge RelevantKnowledgeSnapshot `json:"relevant_knowledge_snapshot,omitempty"`
}

var applicationStatuses = map[ApplicationStatus]bool{
	ApplicationDiscovered: true, ApplicationAnalyzed: true, ApplicationShortlisted: true,
	ApplicationApplied: true, ApplicationEmployerReplied: true, ApplicationInterview: true,
	ApplicationOffer: true, ApplicationRejected: true, ApplicationArchived: true,
	ApplicationUnknown: true,
}

var applicationSources = map[ApplicationSource]bool{
	ApplicationSourceHH: true, ApplicationSourceManual: true,
}

var applicationEventTypes = map[ApplicationEventType]bool{
	ApplicationEventCreated: true, ApplicationEventMatched: true, ApplicationEventApplied: true,
	ApplicationEventMessageReceived: true, ApplicationEventInterviewScheduled: true,
	ApplicationEventStatusChanged: true, ApplicationEventRejected: true,
	ApplicationEventOfferReceived: true, ApplicationEventFollowUpSent: true,
	ApplicationEventLinkedConversation: true, ApplicationEventLinkedVacancy: true,
	ApplicationEventPartialCreated: true, ApplicationEventVacancyEnriched: true,
	ApplicationEventMatchBackfilled: true,
}

func (m MatchResult) validate() error {
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

func (m *MatchResult) normalize() {
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

func (a JobApplication) validate() error {
	if strings.TrimSpace(a.ID) == "" || a.VacancyID < 0 || a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return errors.New("invalid application identity or timestamps")
	}
	if !applicationSources[a.Source] || !applicationStatuses[a.Status] {
		return errors.New("invalid application source or status")
	}
	if a.MatchResult != nil {
		if err := a.MatchResult.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (e ApplicationEvent) validate() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ApplicationID) == "" || e.Timestamp.IsZero() ||
		!applicationEventTypes[e.Type] || strings.TrimSpace(e.Description) == "" {
		return errors.New("invalid application event")
	}
	return nil
}
