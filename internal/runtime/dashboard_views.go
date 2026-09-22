package runtime

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DashboardApplication struct {
	JobApplication
	AppliedAt      *time.Time `json:"applied_at"`
	LastContact    *time.Time `json:"last_contact"`
	LastActivity   time.Time  `json:"last_activity"`
	DisplayStatus  string     `json:"display_status"`
	Recommendation string     `json:"recommendation"`
}

type DashboardAction struct {
	ApprovedHHAction
	LifecycleStatus              HHWriteActionStatus             `json:"lifecycle_status,omitempty"`
	LifecycleReasons             []string                        `json:"lifecycle_reasons,omitempty"`
	CurrentRelevantKnowledgeHash string                          `json:"current_relevant_knowledge_hash,omitempty"`
	RelevantKnowledgeDiff        []string                        `json:"relevant_knowledge_diff,omitempty"`
	Safety                       string                          `json:"safety,omitempty"`
	RequestValidation            string                          `json:"request_validation,omitempty"`
	WriteCapability              string                          `json:"write_capability,omitempty"`
	RequestPreview               *SanitizedHHWriteRequestPreview `json:"request_preview,omitempty"`
	TransportResponse            *HHWriteEvent                   `json:"transport_response,omitempty"`
}

// dashboardActionPriority is intentionally lifecycle-first. An action's
// UpdatedAt is only a tie-breaker within the same lifecycle class; a late
// failure must never replace a newer approved action in the main Send card.
func dashboardActionPriority(action DashboardAction) int {
	status := action.LifecycleStatus
	if status == "" {
		status = action.Status
	}
	switch status {
	case HHWriteApproved:
		if action.Safety == "READY_TO_SEND" && action.RequestValidation == "VALID" && strings.TrimSpace(action.SendNonce) != "" && action.NonceUsedAt == nil {
			return 500
		}
		return 450
	case HHWritePending:
		return 400
	case HHWriteStale, HHWriteManualReview:
		return 350
	case HHWriteSending, HHWriteSentUnconfirmed, HHWriteDeliveryUncertain:
		return 300
	case HHWriteSent, HHWriteDeliveryConfirmed:
		return 250
	case HHWriteFailed, HHWriteCancelled:
		return 100
	default:
		return 0
	}
}

func dashboardActionIsHistorical(action DashboardAction) bool {
	status := action.LifecycleStatus
	if status == "" {
		status = action.Status
	}
	return status == HHWriteFailed || status == HHWriteCancelled
}

// selectDashboardActions separates the one current lifecycle action from
// historical attempts. It must not select an action by timestamp alone.
func selectDashboardActions(actions []DashboardAction) (*DashboardAction, []DashboardAction, string) {
	ordered := append([]DashboardAction{}, actions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := dashboardActionPriority(ordered[i]), dashboardActionPriority(ordered[j])
		if pi != pj {
			return pi > pj
		}
		if !ordered[i].UpdatedAt.Equal(ordered[j].UpdatedAt) {
			return ordered[i].UpdatedAt.After(ordered[j].UpdatedAt)
		}
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})

	if len(ordered) == 0 {
		return nil, []DashboardAction{}, "No HH write actions exist for this conversation."
	}
	if dashboardActionIsHistorical(ordered[0]) {
		return nil, ordered, "Only failed/cancelled actions exist; they remain history and cannot populate the Send card."
	}
	current := ordered[0]
	history := append([]DashboardAction{}, ordered[1:]...)
	return &current, history, fmt.Sprintf("Selected %s by lifecycle priority (%d); timestamps were used only as tie-breakers.", current.ID, dashboardActionPriority(current))
}

func conversationActivity(c EmployerConversation) time.Time {
	if c.LastActivityAt != nil {
		return *c.LastActivityAt
	}
	return c.UpdatedAt
}
func outstandingEmployerMessage(c EmployerConversation) bool {
	latest := latestDeliveredMessage(c)
	return latest != nil && latest.Sender == ConversationSenderEmployer && conversationReplyRequirement(c, latest, classifyEmployerMessage(latest.Text)) == ReplyRequired && c.LastEmployerMessageAt != nil && (c.LastCandidateMessageAt == nil || c.LastEmployerMessageAt.After(*c.LastCandidateMessageAt))
}
func matchRecommendation(m *MatchResult) string {
	if m != nil && m.Recommendation != nil {
		return string(m.Recommendation.Decision)
	}
	return ""
}
func matchScore(m *MatchResult) int {
	if m == nil {
		return -1
	}
	return m.Score
}
func dashboardApplicationRow(a JobApplication, vacancy *Vacancy, events []ApplicationEvent, conversation *EmployerConversation, now time.Time) DashboardApplication {
	row := DashboardApplication{JobApplication: a, LastActivity: a.UpdatedAt, DisplayStatus: string(a.Status), Recommendation: matchRecommendation(a.MatchResult)}
	if row.MatchResult == nil && vacancy != nil {
		row.MatchResult = vacancy.MatchResult
		row.Recommendation = matchRecommendation(vacancy.MatchResult)
		if vacancy.ApplicationRecommendation != nil {
			row.Recommendation = string(vacancy.ApplicationRecommendation.Decision)
		}
	}
	for _, e := range events {
		if e.Type == ApplicationEventApplied && (row.AppliedAt == nil || e.Timestamp.Before(*row.AppliedAt)) {
			t := e.Timestamp
			row.AppliedAt = &t
		}
		if e.Timestamp.After(row.LastActivity) {
			row.LastActivity = e.Timestamp
		}
	}
	if raw := a.HHMetadata["applied_at"]; row.AppliedAt == nil && raw != "" {
		if t, e := time.Parse(time.RFC3339, raw); e == nil {
			row.AppliedAt = &t
		}
	}
	if a.ConversationID != "" && conversation != nil {
		row.LastContact = conversation.LastActivityAt
		if t := conversationActivity(*conversation); t.After(row.LastActivity) {
			row.LastActivity = t
		}
		state := (ConversationStateResolver{}).Resolve(a, *conversation, knownApplicationTime(a, events), false, nil, now)
		row.DisplayStatus = string(state.Status)
		if conversation.NextAction != "" {
			row.NextAction = conversation.NextAction
		}
	}
	if a.ConversationID != "" && conversation == nil {
		row.DisplayStatus = "manual_review"
	}
	return row
}

func (s *DashboardServer) applicationRow(a JobApplication) (DashboardApplication, error) {
	var vacancy *Vacancy
	if v, err := s.Vacancies.Get(a.VacancyID); err == nil {
		vacancy = &v
	}
	events, err := s.Applications.GetApplicationTimeline(a.ID)
	if err != nil {
		return dashboardApplicationRow(a, vacancy, nil, nil, time.Now()), err
	}
	var conversation *EmployerConversation
	if a.ConversationID != "" {
		c, getErr := s.Conversations.GetConversation(a.ConversationID)
		if errors.Is(getErr, ErrConversationNotFound) {
			return dashboardApplicationRow(a, vacancy, events, nil, time.Now()), nil
		}
		if getErr != nil {
			return DashboardApplication{}, getErr
		}
		conversation = &c
	}
	return dashboardApplicationRow(a, vacancy, events, conversation, time.Now()), nil
}
func (s *DashboardServer) applicationList(q url.Values) ([]DashboardApplication, error) {
	status, recommendation, sortBy := q.Get("status"), q.Get("recommendation"), q.Get("sort")
	if status != "" && !applicationStatusValid(ApplicationStatus(status)) && status != "candidate_action_required" && status != "waiting_employer" {
		return nil, errors.New("invalid status")
	}
	if !validDashboardRecommendation(recommendation) || sortBy != "" && sortBy != "newest" && sortBy != "match_score" && sortBy != "last_activity" {
		return nil, errors.New("invalid filter")
	}
	for _, key := range []string{"waiting_employer", "need_candidate_response"} {
		if v := q.Get(key); v != "" && v != "true" && v != "false" {
			return nil, errors.New("invalid boolean")
		}
	}
	all, err := s.Applications.ListApplications()
	if err != nil {
		return nil, err
	}
	rows := []DashboardApplication{}
	for _, a := range all {
		row, e := s.applicationRow(a)
		if e != nil {
			return nil, e
		}
		if status != "" && row.DisplayStatus != status {
			continue
		}
		if recommendation != "" && row.Recommendation != recommendation {
			continue
		}
		if !strings.Contains(strings.ToLower(a.CompanyName), strings.ToLower(q.Get("company"))) || !strings.Contains(strings.ToLower(a.CompanyName+" "+a.VacancyTitle), strings.ToLower(q.Get("search"))) {
			continue
		}
		if q.Get("waiting_employer") == "true" && row.DisplayStatus != "waiting_employer" && a.NextAction != string(NextActionWaitingEmployerReply) {
			continue
		}
		if q.Get("need_candidate_response") == "true" && row.DisplayStatus != "candidate_action_required" && a.NextAction != string(NextActionWaitingCandidateReply) {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		switch sortBy {
		case "match_score":
			if matchScore(rows[i].MatchResult) != matchScore(rows[j].MatchResult) {
				return matchScore(rows[i].MatchResult) > matchScore(rows[j].MatchResult)
			}
		case "last_activity":
			if !rows[i].LastActivity.Equal(rows[j].LastActivity) {
				return rows[i].LastActivity.After(rows[j].LastActivity)
			}
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}
func validDashboardRecommendation(v string) bool {
	return v == "" || v == "apply" || v == "maybe" || v == "skip"
}
func (s *DashboardServer) vacancyList(q url.Values) ([]Vacancy, error) {
	if !validDashboardRecommendation(q.Get("recommendation")) {
		return nil, errors.New("invalid recommendation")
	}
	min, max := 0, 100
	for key, target := range map[string]*int{"min_score": &min, "max_score": &max} {
		if raw := q.Get(key); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 0 || v > 100 {
				return nil, errors.New("invalid score")
			}
			*target = v
		}
	}
	if min > max {
		return nil, errors.New("invalid score range")
	}
	values, err := s.Vacancies.List()
	if err != nil {
		return nil, err
	}
	result := []Vacancy{}
	for _, v := range values {
		decision := matchRecommendation(v.MatchResult)
		if v.ApplicationRecommendation != nil {
			decision = string(v.ApplicationRecommendation.Decision)
		}
		if q.Get("recommendation") != "" && q.Get("recommendation") != decision {
			continue
		}
		if q.Get("min_score") != "" || q.Get("max_score") != "" {
			score := matchScore(v.MatchResult)
			if score < min || score > max {
				continue
			}
		}
		if !strings.Contains(strings.ToLower(v.Title+" "+v.Name+" "+v.Company.Name), strings.ToLower(q.Get("search"))) {
			continue
		}
		result = append(result, v)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].PublishedAt.Equal(result[j].PublishedAt) {
			return result[i].PublishedAt.After(result[j].PublishedAt)
		}
		return result[i].ID > result[j].ID
	})
	return result, nil
}
func (s *DashboardServer) linkedAI(applicationID, conversationID string) (map[string]any, error) {
	drafts, err := s.Drafts.List()
	if err != nil {
		return nil, err
	}
	cs, err := s.Clarifications.List()
	if err != nil {
		return nil, err
	}
	selected := []AIDraft{}
	clarifications := []CandidateClarificationRequest{}
	for _, d := range drafts {
		if applicationID != "" && d.ApplicationID == applicationID || conversationID != "" && d.ConversationID == conversationID {
			selected = append(selected, d)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].CreatedAt.After(selected[j].CreatedAt) })
	for _, c := range cs {
		if applicationID != "" && c.ApplicationID == applicationID || conversationID != "" && c.ConversationID == conversationID {
			clarifications = append(clarifications, c)
		}
	}
	var latest *AIDraft
	if len(selected) > 0 {
		latest = &selected[0]
	}
	actions := []DashboardAction{}
	if s.WriteGateway != nil {
		// The Dashboard process may outlive a CLI invocation or another
		// process that appended a durable action or transport event.
		if err := s.WriteGateway.Actions.Reload(); err != nil {
			return nil, err
		}
		if err := s.WriteGateway.Audit.Reload(); err != nil {
			return nil, err
		}
		auditEvents := s.WriteGateway.Audit.List()
		for _, action := range s.WriteGateway.Actions.List() {
			if (conversationID != "" && action.ConversationID == conversationID) || (applicationID != "" && action.ApplicationID == applicationID) {
				row := DashboardAction{ApprovedHHAction: action, LifecycleStatus: action.Status}
				for i := len(auditEvents) - 1; i >= 0; i-- {
					if auditEvents[i].ActionID == action.ID && auditEvents[i].Type == "transport_response" {
						transportResponse := auditEvents[i]
						row.TransportResponse = &transportResponse
						break
					}
				}
				if preview, previewErr := s.WriteGateway.BuildRequestPreview(action.ID); previewErr == nil {
					row.RequestValidation = "VALID"
					sanitized := sanitizeHHWriteRequestPreview(preview)
					row.RequestPreview = &sanitized
				} else {
					row.RequestValidation = "INVALID"
				}
				row.Safety = "BLOCKED"
				if s.WriteGateway.PreflightIsFresh(action) {
					row.Safety = "READY_TO_SEND"
				}
				if s.WriteGateway.WriteEnabled() {
					row.WriteCapability = "ENABLED"
				} else if s.WriteGateway.DryRun {
					row.WriteCapability = "BLOCKED_BY_DRY_RUN"
				} else {
					row.WriteCapability = "BLOCKED_BY_WRITE_DISABLED"
				}
				if action.RelevantKnowledgeHash != "" {
					if _, conversationErr := s.Conversations.GetConversation(action.ConversationID); conversationErr == nil {
						if draft, draftErr := s.Drafts.Get(action.DraftID); draftErr == nil {
							if conversationContext, contextErr := NewConversationContextBuilder(s.Conversations, s.Resolver).BuildForReply(action.ConversationID); contextErr == nil {
								if current, snapshotErr := relevantKnowledgeSnapshotForResolverWithSemantic(s.Resolver, conversationContext.CandidateContext, action.ApprovedText, draft.UsedFacts, conversationContext.RelevantExamples); snapshotErr == nil {
									row.CurrentRelevantKnowledgeHash = RelevantKnowledgeHash(current)
									row.RelevantKnowledgeDiff = relevantKnowledgeDiff(action.RelevantKnowledgeSnapshot, current)
									if row.CurrentRelevantKnowledgeHash != action.RelevantKnowledgeHash && action.Status == HHWriteApproved {
										row.LifecycleStatus = HHWriteStale
										row.LifecycleReasons = []string{"Candidate knowledge relevant to this draft changed"}
									}
								}
							}
						}
					}
				}
				actions = append(actions, row)
			}
		}
	}
	currentAction, previousAttempts, selectionReason := selectDashboardActions(actions)
	return map[string]any{
		"latest_draft":            latest,
		"drafts":                  selected,
		"clarifications":          clarifications,
		"actions":                 actions, // compatibility/debug view; do not use for the main Send card.
		"current_action":          currentAction,
		"previous_attempts":       previousAttempts,
		"action_selection_reason": selectionReason,
		"decision":                s.decisions[conversationID],
	}, nil
}
func (s *DashboardServer) conversationDetail(id string) (any, error) {
	c, err := s.Conversations.GetConversation(id)
	if err != nil {
		return nil, err
	}
	timeline, err := s.Conversations.GetConversationTimeline(id)
	if err != nil {
		return nil, err
	}
	c.Messages = timeline
	state := s.careerSnapshotForConversation(c).ResolveConversation(c, time.Now())
	c.Status = state.Status
	c.WaitingSince = state.WaitingSince
	ai, err := s.linkedAI("", id)
	if err != nil {
		return nil, err
	}
	ctx, contextErr := NewConversationContextBuilder(s.Conversations, s.Resolver).BuildForReply(id)
	warning := ""
	if contextErr != nil {
		warning = "Context withheld; manual review required"
	}
	var pilot any
	if contextErr == nil {
		set, pilotErr := BuildPilotCandidateReports(&ConversationStore{conversations: []EmployerConversation{c}}, s.Applications, s.Clarifications, s.Resolver, s.Drafts, time.Now().UTC())
		if pilotErr == nil {
			for _, item := range append(append(append([]PilotCandidateReport{}, set.Recommended...), set.Possible...), set.NotRecommended...) {
				if item.ConversationID == id {
					pilot = item
					break
				}
			}
		}
	}
	if latest, draftErr := latestEmployerReplyDraft(s.Drafts, id); draftErr == nil {
		decision := AIResponseDecision{Action: AIActionDraftReply, Draft: latest.Text, Reason: latest.DecisionReason, UsedFacts: latest.UsedFacts, ForbiddenClaimsChecked: true}
		ai["draft_quality"] = pilotDraftQuality(decision, &latest, ctx)
	}
	application := JobApplication{}
	applications, _ := s.Applications.ListApplications()
	for _, candidate := range applications {
		if candidate.ConversationID == c.ID {
			application = candidate
			break
		}
	}
	clarifications, _ := s.Clarifications.List()
	followUp := FollowUpCandidate{ConversationID: c.ID, Status: FollowUpNotEligible}
	for _, candidate := range s.careerSnapshotLocal().FollowUps(s.FollowUpPolicy, time.Now().UTC()) {
		if candidate.ConversationID == c.ID {
			followUp = candidate
			break
		}
	}
	workflow := classifyCareerWorkflow(application, c, clarifications, time.Now().UTC(), &followUp, s.Resolver)
	return map[string]any{"state_resolution": state, "workflow": workflow, "conversation": c, "context": ctx, "context_warning": warning, "pilot": pilot, "ai": ai, "refreshing": s.targetedRefreshRunning(c.HHConversationID), "freshness": map[string]any{"local_updated_at": c.UpdatedAt, "hh_last_checked_at": conversationHHCheckedAt(c), "display_ttl_seconds": int(s.displayTTL.Seconds())}}, nil
}
func (s *DashboardServer) applicationDetail(id string) (any, error) {
	ctx, err := NewApplicationContextBuilder(s.Applications, s.Conversations, s.Resolver).Build(id)
	if err != nil {
		return nil, err
	}
	row, err := s.applicationRow(ctx.Application)
	if err != nil {
		return nil, err
	}
	var vacancy *Vacancy
	if v, e := s.Vacancies.Get(ctx.Application.VacancyID); e == nil {
		vacancy = &v
		candidate, e := s.Resolver.ResolveForVacancy(v)
		if e != nil {
			return nil, e
		}
		ctx.CandidateContext = candidate
	} else if !errors.Is(e, ErrVacancyNotFound) {
		return nil, e
	}
	ai, err := s.linkedAI(id, ctx.Application.ConversationID)
	if err != nil {
		return nil, err
	}
	var conversation any
	if ctx.Application.ConversationID != "" {
		conversation, err = s.conversationDetail(ctx.Application.ConversationID)
		if err != nil && !errors.Is(err, ErrConversationNotFound) {
			return nil, err
		}
	}
	f, _ := s.followUpCandidate(id, time.Now())
	var followDraft *AIDraft
	for _, draft := range s.Drafts.drafts {
		if draft.Type == AIDraftFollowUp && draft.ApplicationID == id && draft.Status == AIDraftGenerated && (followDraft == nil || draft.CreatedAt.After(followDraft.CreatedAt)) {
			v := draft
			followDraft = &v
		}
	}
	return map[string]any{"follow_up": f, "follow_up_draft": followDraft, "application": row, "vacancy": vacancy, "match_result": row.MatchResult, "timeline": ctx.Timeline, "candidate_context": ctx.CandidateContext, "conversation_detail": conversation, "ai": ai}, nil
}

type DashboardMetrics struct {
	FollowUpEligible       int                  `json:"follow_up_eligible"`
	FollowUpDrafted        int                  `json:"follow_up_drafted"`
	TotalVacancies         int                  `json:"total_vacancies"`
	AnalyzedVacancies      int                  `json:"analyzed_vacancies"`
	Applications           int                  `json:"applications"`
	EmployerReplies        int                  `json:"employer_replies"`
	NeedMyResponse         int                  `json:"need_my_response"`
	WaitingEmployer        int                  `json:"waiting_employer"`
	Interviews             int                  `json:"interviews"`
	Offers                 int                  `json:"offers"`
	Rejected               int                  `json:"rejected"`
	ResponseRate           float64              `json:"response_rate"`
	InterviewConversion    float64              `json:"interview_conversion"`
	NewMessages            int                  `json:"new_messages"`
	AIDrafts               int                  `json:"ai_drafts"`
	PendingClarifications  int                  `json:"pending_clarifications"`
	KnowledgeQuestions     int                  `json:"knowledge_questions"`
	PendingProposals       int                  `json:"pending_proposals"`
	WorkflowNeedsReply     int                  `json:"workflow_needs_reply"`
	WorkflowNeedsAction    int                  `json:"workflow_needs_action"`
	WorkflowInterviews     int                  `json:"workflow_interviews"`
	WorkflowWaiting        int                  `json:"workflow_waiting"`
	WorkflowClarifications int                  `json:"workflow_clarifications"`
	WorkflowImportant      int                  `json:"workflow_important"`
	CareerNew              int                  `json:"career_new"`
	CareerAnalyzing        int                  `json:"career_analyzing"`
	CareerMatched          int                  `json:"career_matched"`
	CareerReviewRequired   int                  `json:"career_review_required"`
	CareerReady            int                  `json:"career_ready"`
	CareerApplied          int                  `json:"career_applied"`
	CareerInterview        int                  `json:"career_interview"`
	CareerRunStatus        string               `json:"career_run_status,omitempty"`
	HHWrite                *HHWriteMetrics      `json:"hh_write_metrics,omitempty"`
	FirstPilot             *HHFirstPilotSummary `json:"first_pilot,omitempty"`
}
type DashboardDay struct {
	Date         string `json:"date"`
	Applications int    `json:"applications"`
	Replies      int    `json:"replies"`
}

// Analytics is a projection of saved events and conversations, with no database
// or inferred state transitions. Conversion rates use the selected application
// cohort, while charts count dated events that actually occurred in the window.
func (s *DashboardServer) analytics(period string, now time.Time) (any, error) {
	start := time.Time{}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case "today":
		start = midnight
	case "7d":
		start = midnight.AddDate(0, 0, -6)
	case "30d":
		start = midnight.AddDate(0, 0, -29)
	}
	inWindow := func(t time.Time) bool { return !t.After(now) && (start.IsZero() || !t.IsZero() && !t.Before(start)) }
	metrics := DashboardMetrics{}
	snapshot, err := s.loadDashboardSnapshot()
	if err != nil {
		return nil, err
	}
	applyCareerAgentMetrics(&metrics, snapshot.careerQueue, snapshot.careerRuns)
	for _, v := range snapshot.vacancies {
		if inWindow(v.CreatedAt) {
			metrics.TotalVacancies++
			if v.MatchResult != nil {
				metrics.AnalyzedVacancies++
			}
		}
	}
	conversations := snapshot.conversations
	originalConversationsByID := snapshot.conversationsByID()
	byConversation := make(map[string]EmployerConversation, len(originalConversationsByID))
	for id, conversation := range originalConversationsByID {
		byConversation[id] = conversation
	}
	days := map[string]*DashboardDay{}
	dayFor := func(t time.Time) *DashboardDay {
		key := t.In(now.Location()).Format("2006-01-02")
		if days[key] == nil {
			days[key] = &DashboardDay{Date: key}
		}
		return days[key]
	}
	career := snapshot.career()
	for _, c := range conversations {
		state := career.ResolveConversation(c, now)
		c.Status = state.Status
		c.WaitingSince = state.WaitingSince
		byConversation[c.ID] = c
		if inWindow(conversationActivity(c)) {
			if c.Status == ConversationCandidateActionRequired {
				metrics.NeedMyResponse++
			}
			if c.Status == ConversationWaitingEmployer {
				metrics.WaitingEmployer++
			}
			if outstandingEmployerMessage(c) {
				for _, m := range c.Messages {
					if !m.HHSystemEvent && m.Sender == ConversationSenderEmployer && m.Source != ConversationSourceAIDraft && (c.LastCandidateMessageAt == nil || m.Timestamp.After(*c.LastCandidateMessageAt)) {
						metrics.NewMessages++
					}
				}
			}
		}
	}
	eventsByApplication := snapshot.eventsByApplicationID()
	vacanciesByID := snapshot.vacanciesByID()
	for _, a := range snapshot.applications {
		vacancy, vacancyExists := vacanciesByID[a.VacancyID]
		var vacancyPtr *Vacancy
		if vacancyExists {
			vacancyPtr = &vacancy
		}
		conversation, conversationExists := byConversation[a.ConversationID]
		var conversationPtr *EmployerConversation
		if conversationExists {
			// Use the original conversation for application-row semantics. The
			// resolved copy above is only the aggregate input for metrics.
			original, originalExists := originalConversationsByID[a.ConversationID]
			if originalExists {
				conversation = original
			}
			conversationPtr = &conversation
		}
		events := eventsByApplication[a.ID]
		row := dashboardApplicationRow(a, vacancyPtr, events, conversationPtr, time.Now())
		applied := row.AppliedAt != nil || a.Status == ApplicationApplied || a.Status == ApplicationEmployerReplied || a.Status == ApplicationInterview || a.Status == ApplicationOffer || a.Status == ApplicationRejected
		// Structured imported states prove application existence, but undated
		// applications only participate in all-time metrics, never daily charts.
		date := time.Time{}
		if row.AppliedAt != nil {
			date = *row.AppliedAt
		}
		replied, interview, offer, rejected := a.Status == ApplicationEmployerReplied, a.Status == ApplicationInterview, a.Status == ApplicationOffer, a.Status == ApplicationRejected
		var firstReply time.Time
		for _, e := range events {
			switch e.Type {
			case ApplicationEventMessageReceived:
				replied = true
				if firstReply.IsZero() || e.Timestamp.Before(firstReply) {
					firstReply = e.Timestamp
				}
			case ApplicationEventInterviewScheduled:
				interview = true
			case ApplicationEventOfferReceived:
				offer = true
			case ApplicationEventRejected:
				rejected = true
			}
		}
		if c, ok := byConversation[a.ConversationID]; ok {
			for _, m := range c.Messages {
				if !m.HHSystemEvent && m.Sender == ConversationSenderEmployer && m.Source != ConversationSourceAIDraft {
					replied = true
					if firstReply.IsZero() || m.Timestamp.Before(firstReply) {
						firstReply = m.Timestamp
					}
				}
			}
			interview = interview || c.Status == ConversationInterview
			offer = offer || c.Status == ConversationOffer
			rejected = rejected || c.Status == ConversationRejected
		}
		if applied && inWindow(date) {
			metrics.Applications++
			if replied {
				metrics.EmployerReplies++
			}
			if interview {
				metrics.Interviews++
			}
			if offer {
				metrics.Offers++
			}
			if rejected {
				metrics.Rejected++
			}
		}
		if applied && !date.IsZero() && inWindow(date) {
			dayFor(date).Applications++
		}
		if !firstReply.IsZero() && inWindow(firstReply) {
			dayFor(firstReply).Replies++
		}
	}
	if metrics.Applications > 0 {
		metrics.ResponseRate = float64(metrics.EmployerReplies) * 100 / float64(metrics.Applications)
		metrics.InterviewConversion = float64(metrics.Interviews) * 100 / float64(metrics.Applications)
	}
	for _, d := range snapshot.drafts {
		if d.Status == AIDraftGenerated {
			metrics.AIDrafts++
		}
	}
	clarifications := snapshot.clarifications
	for _, c := range clarifications {
		if c.Status == ClarificationPending {
			metrics.PendingClarifications++
		}
	}
	workflowApplications := map[string]JobApplication{}
	for _, application := range snapshot.applications {
		if application.ConversationID != "" {
			workflowApplications[application.ConversationID] = application
		}
	}
	workflowFollowUps := map[string]FollowUpCandidate{}
	for _, followUp := range career.FollowUps(s.FollowUpPolicy, now) {
		workflowFollowUps[followUp.ConversationID] = followUp
	}
	for _, conversation := range conversations {
		followUp, exists := workflowFollowUps[conversation.ID]
		if !exists {
			followUp = FollowUpCandidate{ConversationID: conversation.ID, Status: FollowUpNotEligible}
		}
		projection := classifyCareerWorkflow(workflowApplications[conversation.ID], conversation, clarifications, now, &followUp, s.Resolver)
		switch projection.State {
		case WorkflowNeedsReply:
			metrics.WorkflowNeedsReply++
		case WorkflowNeedsUserAction:
			metrics.WorkflowNeedsAction++
		case WorkflowNeedsClarification:
			metrics.WorkflowClarifications++
		case WorkflowInterview, WorkflowExternalAction:
			metrics.WorkflowInterviews++
		case WorkflowWaitingForEmployer:
			metrics.WorkflowWaiting++
		}
		if projection.State != WorkflowNoReplyNeeded && projection.State != WorkflowTerminal {
			metrics.WorkflowImportant++
		}
	}
	for _, u := range s.Knowledge.Unknowns {
		if u.Status == CandidateUnknownNeedsConfirmation {
			metrics.KnowledgeQuestions++
		}
	}
	for _, p := range s.Knowledge.Proposals {
		if p.Status == KnowledgeProposalPending {
			metrics.PendingProposals++
		}
	}
	if !start.IsZero() {
		for d := start; !d.After(now); d = d.AddDate(0, 0, 1) {
			dayFor(d)
		}
	}
	series := []DashboardDay{}
	for _, d := range days {
		series = append(series, *d)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Date < series[j].Date })
	fu := career.FollowUpAnalytics(s.FollowUpPolicy, now)
	metrics.FollowUpEligible = fu.FollowUpEligible
	metrics.FollowUpDrafted = fu.FollowUpDrafted
	if s.WriteGateway != nil {
		events := s.WriteGateway.Audit.List()
		if len(events) > 0 {
			writeMetrics := BuildHHWriteMetrics(events)
			metrics.HHWrite = &writeMetrics
			metrics.FirstPilot = BuildHHFirstPilotSummary(events)
		}
	}
	return map[string]any{"follow_up_analytics": fu, "period": period, "metrics": metrics, "daily": series, "sync": snapshot.sync, "generated_at": now, "notes": []string{"Response rate and interview conversion use applications with a known application date in the selected period; all-time also includes undated imported applications.", "Charts show dated applications and first employer replies. Missing dates are not guessed.", "New messages means employer messages since the latest candidate response, not HH unread receipts. Knowledge and draft counts show the current queue."}}, nil
}
