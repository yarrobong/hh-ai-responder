package runtime

import "time"

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
	return result, nil
}

func (d dashboardSnapshot) career() CareerSnapshot {
	return CareerSnapshot{
		Vacancies: d.vacancies, Applications: d.applications, Events: d.events,
		Conversations: d.conversations, Drafts: d.drafts, Clarifications: d.clarifications,
		Sync: d.sync, Consistency: map[string][]string{},
	}
}

func (d dashboardSnapshot) vacanciesByID() map[int]Vacancy {
	result := make(map[int]Vacancy, len(d.vacancies))
	for _, vacancy := range d.vacancies {
		result[vacancy.ID] = vacancy
	}
	return result
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
