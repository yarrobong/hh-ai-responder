package runtime

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"hh-ai-responder/internal/careeragent"
)

// dashboardSnapshot is the request-scoped input set for the Overview
// projection. Each collection is loaded once and all relations below are
// resolved in memory. It deliberately remains local to the dashboard path;
// the canonical store APIs keep their existing contracts for other callers.
type dashboardSnapshot struct {
	vacancies      []Vacancy
	applications   []JobApplication
	events         []ApplicationEvent
	conversations  []EmployerConversation
	drafts         []AIDraft
	clarifications []CandidateClarificationRequest
	careerRuns     []careeragent.AgentRun
	preparations   []careeragent.ApplicationPreparation
	careerQueue    []careeragent.ReviewQueueItem
	attention      []careeragent.AttentionItem
	careerError    string
	sync           HHSyncState
}

func (s *DashboardServer) loadDashboardSnapshot() (dashboardSnapshot, error) {
	result := dashboardSnapshot{}
	var err error

	start := time.Now()
	result.vacancies, err = s.Vacancies.List()
	perfRecord("dashboard.read.vacancies", start, len(result.vacancies))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	start = time.Now()
	result.applications, err = s.Applications.ListApplicationsForDashboard()
	perfRecord("dashboard.read.applications", start, len(result.applications))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	start = time.Now()
	result.events, err = s.Applications.ListEventsForDashboard()
	perfRecord("dashboard.read.application_events", start, len(result.events))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	start = time.Now()
	result.conversations, err = s.Conversations.ListConversationsForDashboard()
	perfRecord("dashboard.read.conversations", start, len(result.conversations))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	start = time.Now()
	result.drafts, err = s.Drafts.List()
	perfRecord("dashboard.read.drafts", start, len(result.drafts))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	start = time.Now()
	result.clarifications, err = s.Clarifications.List()
	perfRecord("dashboard.read.clarifications", start, len(result.clarifications))
	if err != nil {
		return dashboardSnapshot{}, err
	}

	result.sync = s.Sync.SyncState()
	if s.CareerWorkflow == nil {
		result.careerError = "Career Agent workflow store is unavailable"
	} else {
		result.careerRuns, err = s.CareerWorkflow.ListRuns(context.Background(), careeragent.RunQuery{Limit: 100})
		if err != nil {
			result.careerError = careerReviewUnavailableMessage("Career Agent workflow runs unavailable", err)
			result.careerRuns = nil
		} else {
			result.preparations, err = s.CareerWorkflow.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 1000})
			if err != nil {
				result.careerError = careerReviewUnavailableMessage("Career Agent preparations unavailable", err)
				result.preparations = nil
			}
		}
	}
	if result.careerError == "" {
		result.careerQueue, err = buildCareerReviewQueue(result)
		if err != nil {
			result.careerError = careerReviewUnavailableMessage("Career Agent review workspace unavailable", err)
			result.careerQueue = nil
		}
	}
	if result.careerError == "" {
		result.attention, err = s.buildAttentionQueue(result)
		if err != nil {
			result.careerError = careerReviewUnavailableMessage("Career Agent attention queue unavailable", err)
			result.attention = nil
		}
	}
	return result, nil
}

func (d dashboardSnapshot) career() CareerSnapshot {
	return CareerSnapshot{
		Vacancies: d.vacancies, Applications: d.applications, Events: d.events,
		Conversations: d.conversations, Drafts: d.drafts, Clarifications: d.clarifications,
		Sync: d.sync, Consistency: map[string][]string{}, CareerReviewQueue: d.careerQueue,
		CareerWorkflowUnavailable: d.careerError,
	}
}

func (d dashboardSnapshot) vacanciesByID() map[int]Vacancy {
	result := make(map[int]Vacancy, len(d.vacancies))
	for _, vacancy := range d.vacancies {
		result[vacancy.ID] = vacancy
	}
	return result
}

func buildCareerReviewQueue(snapshot dashboardSnapshot) ([]careeragent.ReviewQueueItem, error) {
	preparations := make(map[int]careeragent.ApplicationPreparation, len(snapshot.preparations))
	for _, preparation := range snapshot.preparations {
		if existing, ok := preparations[preparation.VacancyID]; !ok || preparation.UpdatedAt.After(existing.UpdatedAt) {
			preparations[preparation.VacancyID] = preparation
		}
	}
	applications := make(map[int]JobApplication)
	for _, application := range snapshot.applications {
		if existing, ok := applications[application.VacancyID]; !ok || application.UpdatedAt.After(existing.UpdatedAt) {
			applications[application.VacancyID] = application
		}
	}
	conversations := make(map[int]EmployerConversation)
	for _, conversation := range snapshot.conversations {
		if existing, ok := conversations[conversation.VacancyID]; !ok || conversation.UpdatedAt.After(existing.UpdatedAt) {
			conversations[conversation.VacancyID] = conversation
		}
	}

	inputs := make([]careeragent.VacancyReviewSnapshot, 0, len(snapshot.vacancies))
	for _, vacancy := range snapshot.vacancies {
		input := careeragent.VacancyReviewInput{
			Vacancy:   careeragent.ReviewVacancyReference{ID: vacancy.ID, ExternalID: vacancy.ExternalID, Title: firstNonEmpty(vacancy.Title, vacancy.Name), Company: vacancy.Company.Name, PublishedAt: vacancy.PublishedAt, UpdatedAt: vacancy.UpdatedAt},
			Freshness: careeragent.ReviewFreshness{Archived: vacancy.Archived, ArchivedKnown: vacancy.Archived || vacancy.DataCompleteness == DataCompletenessFull, Stale: vacancy.DataCompleteness != DataCompletenessFull},
			Route:     careeragent.ResumeRoute{VacancyID: vacancy.ID, Status: careeragent.ResumeRouteReviewRequired, Evidence: []string{"route evidence is unavailable in canonical vacancy snapshot"}},
		}
		if match := vacancy.MatchResult; match != nil {
			input.DeterministicMatch = careeragent.ReviewMatchEvidence{Score: match.Score, Known: true}
			if match.Recommendation != nil {
				input.DeterministicMatch.Recommendation = string(match.Recommendation.Decision)
			}
		}
		if application, ok := applications[vacancy.ID]; ok {
			input.Application = careeragent.ReviewApplicationState{ID: application.ID, Status: string(application.Status), NextAction: application.NextAction, UpdatedAt: application.UpdatedAt}
			if input.DeterministicMatch.Known == false && application.MatchResult != nil {
				input.DeterministicMatch = careeragent.ReviewMatchEvidence{Score: application.MatchResult.Score, Known: true}
				if application.MatchResult.Recommendation != nil {
					input.DeterministicMatch.Recommendation = string(application.MatchResult.Recommendation.Decision)
				}
			}
		}
		if conversation, ok := conversations[vacancy.ID]; ok {
			input.Conversation = careeragent.ReviewConversationState{ID: conversation.ID, Status: string(conversation.Status), NextAction: conversation.NextAction, UpdatedAt: conversation.UpdatedAt}
		}
		for _, clarification := range snapshot.clarifications {
			if clarification.Status != ClarificationPending || clarification.VacancyID != strconv.Itoa(vacancy.ID) {
				continue
			}
			input.KnowledgeRequests = append(input.KnowledgeRequests, careeragent.KnowledgeRequest{Topic: string(clarification.Category), Question: clarification.Question, Source: clarification.ID, Status: string(clarification.Status)})
		}
		if preparation, ok := preparations[vacancy.ID]; ok {
			preparationCopy := preparation
			input.Preparation = &preparationCopy
			input.Route = careeragent.ResumeRoute{VacancyID: vacancy.ID, ResumeID: preparation.ResumeID, Confidence: preparation.RouteConfidence, Status: preparation.RouteStatus, Evidence: append([]string(nil), boundedReviewEvidence(preparation.Evidence)...)}
		}
		projected, err := careeragent.BuildVacancyReviewSnapshot(input)
		if err != nil {
			return nil, fmt.Errorf("vacancy %d: %w", vacancy.ID, err)
		}
		inputs = append(inputs, projected)
	}
	return careeragent.BuildReviewQueue(inputs), nil
}

func boundedReviewEvidence(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	// Evidence is validated by ApplicationPreparation.Validate before this
	// projection. Keep the dashboard projection bounded and do not expose raw
	// JSON/private candidate context here.
	return []string{"stored evidence present"}
}

func (d dashboardSnapshot) eventsByApplicationID() map[string][]ApplicationEvent {
	result := make(map[string][]ApplicationEvent)
	for _, event := range d.events {
		result[event.ApplicationID] = append(result[event.ApplicationID], event)
	}
	return result
}

func (d dashboardSnapshot) conversationsByID() map[string]EmployerConversation {
	result := make(map[string]EmployerConversation, len(d.conversations))
	for _, conversation := range d.conversations {
		result[conversation.ID] = conversation
	}
	return result
}
