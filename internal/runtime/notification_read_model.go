package runtime

import "time"

const notificationOverviewLimit = 8

// loadNotificationSnapshot contains exactly the collections consumed by
// CandidateNotificationEngine.Calculate. Vacancies, drafts, and sync state
// are part of the broader career snapshot but are not notification inputs.
// The conversation store's dashboard projection is a bulk-read path; its
// compatibility implementation retains the established in-memory behavior.
func (s *DashboardServer) loadNotificationSnapshot() CareerSnapshot {
	result := CareerSnapshot{Consistency: map[string][]string{}}

	start := time.Now()
	result.Applications, _ = s.Applications.ListApplicationsForDashboard()
	perfRecord("dashboard.notifications.read.applications", start, len(result.Applications))

	start = time.Now()
	result.Events, _ = s.Applications.ListEventsForDashboard()
	perfRecord("dashboard.notifications.read.application_events", start, len(result.Events))

	start = time.Now()
	var err error
	result.Conversations, err = s.Conversations.ListConversationsForDashboard()
	if err != nil {
		// Preserve the legacy read-path fallback when a specialized projection
		// is unavailable or cannot be read.
		result.Conversations, _ = s.Conversations.ListConversations()
	}
	perfRecord("dashboard.notifications.read.conversations", start, len(result.Conversations))

	start = time.Now()
	result.Clarifications, _ = s.Clarifications.List()
	perfRecord("dashboard.notifications.read.clarifications", start, len(result.Clarifications))

	return result
}
