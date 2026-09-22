package careeragent

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// ReviewPipelineState is a read-only projection state. It is deliberately
// separate from canonical application and conversation statuses.
type ReviewPipelineState string

const (
	ReviewStateClarificationNeeded ReviewPipelineState = "clarification_needed"
	ReviewStateReady               ReviewPipelineState = "ready_review"
	ReviewStateMatched             ReviewPipelineState = "matched"
	ReviewStateAIReview            ReviewPipelineState = "ai_review"
	ReviewStateNew                 ReviewPipelineState = "new"
	ReviewStateApplied             ReviewPipelineState = "applied"
	ReviewStateInterview           ReviewPipelineState = "interview"
	ReviewStateDismissed           ReviewPipelineState = "dismissed"
	ReviewStateRejected            ReviewPipelineState = "rejected"
	ReviewStateClosed              ReviewPipelineState = "closed"
)

type ReviewVacancyReference struct {
	ID          int       `json:"id"`
	ExternalID  string    `json:"external_id,omitempty"`
	Title       string    `json:"title,omitempty"`
	Company     string    `json:"company,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type ReviewApplicationState struct {
	ID         string    `json:"id,omitempty"`
	Status     string    `json:"status,omitempty"`
	NextAction string    `json:"next_action,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type ReviewConversationState struct {
	ID         string    `json:"id,omitempty"`
	Status     string    `json:"status,omitempty"`
	NextAction string    `json:"next_action,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type ReviewFreshness struct {
	ObservedAt    time.Time `json:"observed_at,omitempty"`
	Archived      bool      `json:"archived,omitempty"`
	ArchivedKnown bool      `json:"archived_known,omitempty"`
	Stale         bool      `json:"stale,omitempty"`
}

type ReviewMatchEvidence struct {
	Score          int    `json:"score,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
	Known          bool   `json:"known,omitempty"`
}

type ReviewAdvisoryEvidence struct {
	Evaluated      bool     `json:"evaluated,omitempty"`
	Score          *int     `json:"score,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Reasons        []string `json:"reasons,omitempty"`
	ReviewOnly     bool     `json:"review_only"`
}

type VacancyReviewSnapshot struct {
	Vacancy            ReviewVacancyReference  `json:"vacancy"`
	PipelineState      ReviewPipelineState     `json:"pipeline_state"`
	Route              ResumeRoute             `json:"route"`
	DeterministicMatch ReviewMatchEvidence     `json:"deterministic_match"`
	Advisory           ReviewAdvisoryEvidence  `json:"advisory"`
	Freshness          ReviewFreshness         `json:"freshness"`
	Application        ReviewApplicationState  `json:"application,omitempty"`
	Conversation       ReviewConversationState `json:"conversation,omitempty"`
	Preparation        *ApplicationPreparation `json:"preparation,omitempty"`
	KnowledgeRequests  []KnowledgeRequest      `json:"knowledge_requests,omitempty"`
	NextAction         string                  `json:"next_action"`
}

type VacancyReviewInput struct {
	Vacancy            ReviewVacancyReference
	Route              ResumeRoute
	DeterministicMatch ReviewMatchEvidence
	Advisory           ReviewAdvisoryEvidence
	Freshness          ReviewFreshness
	Application        ReviewApplicationState
	Conversation       ReviewConversationState
	Preparation        *ApplicationPreparation
	KnowledgeRequests  []KnowledgeRequest
}

// BuildVacancyReviewSnapshot projects canonical references and workflow
// artifacts into a review-only workspace record. It does not copy or replace
// canonical vacancy, application or conversation truth.
func BuildVacancyReviewSnapshot(input VacancyReviewInput) (VacancyReviewSnapshot, error) {
	if input.Vacancy.ID <= 0 {
		return VacancyReviewSnapshot{}, errors.New("review vacancy requires a canonical id")
	}
	if input.Preparation != nil {
		if err := input.Preparation.Validate(); err != nil {
			return VacancyReviewSnapshot{}, err
		}
		if input.Preparation.VacancyID != input.Vacancy.ID {
			return VacancyReviewSnapshot{}, errors.New("review preparation vacancy does not match canonical vacancy")
		}
	}
	if input.Route.VacancyID != 0 && input.Route.VacancyID != input.Vacancy.ID {
		return VacancyReviewSnapshot{}, errors.New("review route vacancy does not match canonical vacancy")
	}
	if input.Advisory.Evaluated {
		input.Advisory.ReviewOnly = true
	}
	state := deriveReviewPipelineState(input)
	requests := append([]KnowledgeRequest(nil), input.KnowledgeRequests...)
	if len(requests) == 0 && input.Preparation != nil {
		requests = append(requests, input.Preparation.KnowledgeRequests...)
	}
	snapshot := VacancyReviewSnapshot{
		Vacancy: input.Vacancy, PipelineState: state, Route: input.Route,
		DeterministicMatch: input.DeterministicMatch, Advisory: input.Advisory,
		Freshness: input.Freshness, Application: input.Application, Conversation: input.Conversation,
		KnowledgeRequests: requests, NextAction: strings.TrimSpace(input.Application.NextAction),
	}
	if input.Preparation != nil {
		copyPreparation := *input.Preparation
		copyPreparation.KnowledgeRequests = append([]KnowledgeRequest(nil), input.Preparation.KnowledgeRequests...)
		snapshot.Preparation = &copyPreparation
	}
	if snapshot.NextAction == "" {
		snapshot.NextAction = reviewNextAction(snapshot)
	}
	return snapshot, nil
}

func deriveReviewPipelineState(input VacancyReviewInput) ReviewPipelineState {
	if input.Freshness.ArchivedKnown && input.Freshness.Archived {
		return ReviewStateClosed
	}
	if input.Application.Status == "rejected" {
		return ReviewStateRejected
	}
	if input.Application.Status == "archived" || input.Conversation.Status == "closed" {
		return ReviewStateClosed
	}
	if input.Application.Status == "interview" || input.Conversation.Status == "interview" {
		return ReviewStateInterview
	}
	if input.Application.Status == "applied" || input.Application.Status == "employer_replied" || input.Conversation.Status == "applied" {
		return ReviewStateApplied
	}
	if len(input.KnowledgeRequests) > 0 || input.Preparation != nil && len(input.Preparation.KnowledgeRequests) > 0 {
		return ReviewStateClarificationNeeded
	}
	if input.Preparation != nil && input.Preparation.Status == PreparationStatusReady {
		return ReviewStateReady
	}
	if input.Advisory.Evaluated || input.Route.Status == ResumeRouteReviewRequired {
		return ReviewStateAIReview
	}
	if input.DeterministicMatch.Known && strings.EqualFold(input.DeterministicMatch.Recommendation, "apply") {
		return ReviewStateMatched
	}
	return ReviewStateNew
}

func reviewNextAction(snapshot VacancyReviewSnapshot) string {
	switch snapshot.PipelineState {
	case ReviewStateClarificationNeeded:
		return "Answer candidate knowledge requests before review."
	case ReviewStateReady:
		return "Review the preparation and approve only after fresh preflight."
	case ReviewStateAIReview:
		return "Review the advisory analysis; the route remains review-only."
	case ReviewStateMatched:
		return "Review the match and prepare an explicit approval if appropriate."
	case ReviewStateApplied, ReviewStateInterview:
		return "Review the current application and conversation state."
	case ReviewStateRejected, ReviewStateDismissed, ReviewStateClosed:
		return "No safe action is pending."
	default:
		return "Review vacancy details and route evidence."
	}
}

type ReviewQueueItem struct {
	VacancyID     int                   `json:"vacancy_id"`
	Title         string                `json:"title,omitempty"`
	Company       string                `json:"company,omitempty"`
	PipelineState ReviewPipelineState   `json:"pipeline_state"`
	Priority      int                   `json:"priority"`
	PublishedAt   time.Time             `json:"published_at,omitempty"`
	Snapshot      VacancyReviewSnapshot `json:"snapshot"`
}

func ReviewQueuePriority(state ReviewPipelineState) int {
	switch state {
	case ReviewStateClarificationNeeded:
		return 700
	case ReviewStateReady:
		return 650
	case ReviewStateMatched:
		return 600
	case ReviewStateAIReview:
		return 550
	case ReviewStateNew:
		return 500
	case ReviewStateInterview:
		return 450
	case ReviewStateApplied:
		return 425
	case ReviewStateDismissed, ReviewStateRejected:
		return 200
	case ReviewStateClosed:
		return 100
	default:
		return 0
	}
}

func BuildReviewQueue(snapshots []VacancyReviewSnapshot) []ReviewQueueItem {
	result := make([]ReviewQueueItem, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, ReviewQueueItem{VacancyID: snapshot.Vacancy.ID, Title: snapshot.Vacancy.Title, Company: snapshot.Vacancy.Company, PipelineState: snapshot.PipelineState, Priority: ReviewQueuePriority(snapshot.PipelineState), PublishedAt: snapshot.Vacancy.PublishedAt, Snapshot: snapshot})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		if !result[i].PublishedAt.Equal(result[j].PublishedAt) {
			return result[i].PublishedAt.After(result[j].PublishedAt)
		}
		return result[i].VacancyID < result[j].VacancyID
	})
	return result
}
