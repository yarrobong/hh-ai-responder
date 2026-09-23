package runtime

import "time"

// overviewInboxCardLimit is the number of Inbox cards rendered by the
// initial Overview. Keep this aligned with overview()'s slice limit in the
// served frontend; the full Inbox route has no such limit.
const overviewInboxCardLimit = 5

type overviewInboxConversation struct {
	ID           string    `json:"id"`
	CompanyName  string    `json:"company_name"`
	VacancyTitle string    `json:"vacancy_title"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type overviewInboxMessage struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type overviewInboxDraft struct {
	Status string `json:"status"`
	Text   string `json:"text"`
}

type overviewInboxWorkflow struct {
	State    CareerWorkflowState `json:"state"`
	WhatToDo string              `json:"what_to_do"`
	Bucket   string              `json:"bucket,omitempty"`
}

type overviewInboxItem struct {
	Conversation  overviewInboxConversation `json:"conversation"`
	LatestMessage *overviewInboxMessage     `json:"latest_message,omitempty"`
	AIDrafts      []overviewInboxDraft      `json:"ai_drafts,omitempty"`
	Workflow      overviewInboxWorkflow     `json:"workflow"`
}

type overviewInbox struct {
	Items []overviewInboxItem `json:"items"`
}

// overviewConversationValues narrows legacy in-memory values to the same
// latest meaningful message shape returned by the relational read model.
// JSON stores already hold the aggregate in memory, but no complete history
// is allowed to leak into the Overview response or its classifier input.
func overviewConversationValues(values []EmployerConversation) []EmployerConversation {
	for i := range values {
		latest := latestDeliveredMessage(values[i])
		values[i].Messages = nil
		if latest != nil {
			values[i].Messages = []ConversationMessage{*latest}
		}
	}
	return values
}

func (s *DashboardServer) inboxOverview() (overviewInbox, error) {
	readStart := time.Now()
	conversations, err := s.Conversations.ListConversationsForOverview()
	perfRecord("dashboard.inbox_overview.read.conversations", readStart, len(conversations))
	if err != nil {
		return overviewInbox{}, err
	}
	readStart = time.Now()
	applications, err := s.Applications.ListApplicationsForDashboard()
	perfRecord("dashboard.inbox_overview.read.applications", readStart, len(applications))
	if err != nil {
		return overviewInbox{}, err
	}
	readStart = time.Now()
	events, err := s.Applications.ListEventsForDashboard()
	perfRecord("dashboard.inbox_overview.read.application_events", readStart, len(events))
	if err != nil {
		return overviewInbox{}, err
	}
	readStart = time.Now()
	drafts, err := s.Drafts.List()
	perfRecord("dashboard.inbox_overview.read.drafts", readStart, len(drafts))
	if err != nil {
		return overviewInbox{}, err
	}
	readStart = time.Now()
	clarifications, err := s.Clarifications.List()
	perfRecord("dashboard.inbox_overview.read.clarifications", readStart, len(clarifications))
	if err != nil {
		return overviewInbox{}, err
	}

	now := time.Now().UTC()
	snapshot := CareerSnapshot{
		Applications: applications, Conversations: conversations, Events: events,
		Drafts: drafts, Clarifications: clarifications, Sync: s.Sync.SyncState(),
		Consistency: map[string][]string{},
	}
	applicationsByConversation := make(map[string]JobApplication)
	for _, application := range applications {
		if application.ConversationID != "" {
			applicationsByConversation[application.ConversationID] = application
		}
	}
	followUpsByConversation := make(map[string]FollowUpCandidate)
	for _, followUp := range snapshot.FollowUps(s.FollowUpPolicy, now) {
		followUpsByConversation[followUp.ConversationID] = followUp
	}
	pendingByConversation := make(map[string][]CandidateClarificationRequest)
	for _, clarification := range clarifications {
		if clarification.Status == ClarificationPending && clarification.ConversationID != "" {
			pendingByConversation[clarification.ConversationID] = append(pendingByConversation[clarification.ConversationID], clarification)
		}
	}
	draftsByConversation := make(map[string][]AIDraft)
	for _, draft := range drafts {
		if draft.Status == AIDraftGenerated && draft.ConversationID != "" {
			draftsByConversation[draft.ConversationID] = append(draftsByConversation[draft.ConversationID], draft)
		}
	}

	items := make([]CandidateInboxItem, 0, len(conversations))
	for _, conversation := range conversations {
		item := CandidateInboxItem{Conversation: conversation, Warnings: []string{}}
		baseState := (ConversationStateResolver{}).Resolve(JobApplication{}, conversation, nil, false, nil, now)
		item.Warnings = append(item.Warnings, baseState.Warnings...)
		if conversation.Status == ConversationManualReview {
			item.Warnings = append(item.Warnings, "manual_review")
		}
		if latest := latestDeliveredMessage(conversation); latest != nil {
			item.LatestMessage = latest
			if latest.Sender == ConversationSenderEmployer {
				if reason := classifyHighRiskChatMessage(latest.Text); reason != "" {
					item.Warnings = append(item.Warnings, "high-risk employer message: "+reason)
				}
			}
		}
		item.PendingClarifications = append(item.PendingClarifications, pendingByConversation[conversation.ID]...)
		item.AIDrafts = append(item.AIDrafts, draftsByConversation[conversation.ID]...)

		// Match the initial CandidateInbox filter before applying the complete
		// application-aware workflow projection. This preserves draft and
		// clarification attachment semantics without loading full histories.
		seenByInitialInbox := baseState.Status != ConversationClosed && baseState.Status != ConversationRejected &&
			(baseState.Status == ConversationCandidateActionRequired || len(item.PendingClarifications) > 0 || len(item.AIDrafts) > 0 || len(item.Warnings) > 0)
		if !seenByInitialInbox {
			item.PendingClarifications = nil
			item.AIDrafts = nil
			item.Warnings = nil
		} else {
			item.Conversation.Status = baseState.Status
		}

		state := snapshot.ResolveConversation(item.Conversation, now)
		item.Conversation.Status = state.Status
		item.Conversation.WaitingSince = state.WaitingSince
		item.Warnings = uniqueStrings(append(item.Warnings, state.Warnings...))
		followUp := followUpsByConversation[item.Conversation.ID]
		if followUp.ConversationID == "" {
			followUp = FollowUpCandidate{ConversationID: item.Conversation.ID, Status: FollowUpNotEligible}
		}
		item.Workflow = classifyCareerWorkflow(applicationsByConversation[item.Conversation.ID], item.Conversation, item.PendingClarifications, now, &followUp, s.Resolver)
		items = append(items, item)
	}

	// Preserve the existing side effects of the full read path. Draft workers
	// re-read complete histories only for eligible background preparation; the
	// Overview projection itself remains bounded and history-free.
	s.sortAndScheduleOverviewItems(items, now)
	result := overviewInbox{Items: make([]overviewInboxItem, 0, minInt(overviewInboxCardLimit, len(items)))}
	for _, item := range items[:minInt(overviewInboxCardLimit, len(items))] {
		result.Items = append(result.Items, overviewInboxItemFrom(item))
	}
	return result, nil
}

func (s *DashboardServer) sortAndScheduleOverviewItems(items []CandidateInboxItem, now time.Time) {
	sortWorkflowInbox(items)
	s.scheduleInboxDraftsDeferred(items)
	s.recordQualityObservations(CandidateInbox{Items: items}, now)
}

func overviewInboxItemFrom(item CandidateInboxItem) overviewInboxItem {
	result := overviewInboxItem{
		Conversation: overviewInboxConversation{
			ID: item.Conversation.ID, CompanyName: item.Conversation.CompanyName,
			VacancyTitle: item.Conversation.VacancyTitle, UpdatedAt: item.Conversation.UpdatedAt,
		},
		Workflow: overviewInboxWorkflow{State: item.Workflow.State, WhatToDo: item.Workflow.WhatToDo, Bucket: item.Bucket},
	}
	if item.LatestMessage != nil {
		result.LatestMessage = &overviewInboxMessage{Text: item.LatestMessage.Text, Timestamp: item.LatestMessage.Timestamp}
	}
	for _, draft := range item.AIDrafts {
		if draft.Status == AIDraftGenerated {
			result.AIDrafts = append(result.AIDrafts, overviewInboxDraft{Status: string(draft.Status), Text: draft.Text})
		}
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
