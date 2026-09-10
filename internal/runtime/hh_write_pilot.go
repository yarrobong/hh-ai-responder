package runtime

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// hhPreviewWriteClient intentionally has no HH transport. It makes the
// preview gateway capable enough to validate configuration while making any
// accidental send from the preview command fail closed before network I/O.
type hhPreviewWriteClient struct{}

func (*hhPreviewWriteClient) SendConversationMessage(context.Context, string, string, string) (HHWriteTransportResult, error) {
	return HHWriteTransportResult{}, errors.New("production preview cannot send HH messages")
}

// HHWriteMetrics is derived from the append-only write audit. It describes
// attempts and outcomes, not AI drafts or read-only sync activity.
type HHWriteMetrics struct {
	WriteAttemptsTotal int           `json:"write_attempts_total"`
	ManualWritesTotal  int           `json:"manual_writes_total"` // Deprecated alias: attempts, not successful writes.
	SuccessfulWrites   int           `json:"successful_writes"`
	DeliveryConfirmed  int           `json:"delivery_confirmed"`
	DeliveryUncertain  int           `json:"delivery_uncertain"`
	FailedWrites       int           `json:"failed_writes"`
	StaleBeforeSend    int           `json:"stale_before_send"`
	BlockedByPreflight int           `json:"blocked_by_preflight"`
	LastSuccessful     *HHWriteEvent `json:"last_successful_write,omitempty"`
	LastFailed         *HHWriteEvent `json:"last_failed_write,omitempty"`
}

type HHFirstPilotSummary struct {
	ActionID               string `json:"action_id"`
	Delivery               string `json:"delivery"`
	Attempts               int    `json:"attempts"`
	Successful             int    `json:"successful"`
	DeliveryConfirmed      int    `json:"delivery_confirmed"`
	DeliveryUncertain      int    `json:"delivery_uncertain"`
	Failed                 int    `json:"failed"`
	HistoricalFailuresNote string `json:"historical_failures_note"`
}

type HHWritePendingAction struct {
	ActionID       string              `json:"action_id"`
	Conversation   string              `json:"conversation"`
	ConversationID string              `json:"conversation_id"`
	Status         HHWriteActionStatus `json:"status"`
	Sendability    string              `json:"sendability"`
	Reason         string              `json:"reason"`
}

func (m HHWriteMetrics) cloneEvent(event HHWriteEvent) *HHWriteEvent {
	copy := event
	return &copy
}

func BuildHHWriteMetrics(events []HHWriteEvent) HHWriteMetrics {
	result := HHWriteMetrics{}
	failedAttemptIDs := map[string]bool{}
	for _, event := range events {
		switch event.Type {
		case "send_started":
			result.WriteAttemptsTotal++
			result.ManualWritesTotal++
		case "sent":
			result.SuccessfulWrites++
			if result.LastSuccessful == nil || event.CreatedAt.After(result.LastSuccessful.CreatedAt) {
				result.LastSuccessful = result.cloneEvent(event)
			}
		case "delivery_confirmed":
			result.DeliveryConfirmed++
			if result.LastSuccessful == nil || event.CreatedAt.After(result.LastSuccessful.CreatedAt) {
				result.LastSuccessful = result.cloneEvent(event)
			}
		case string(HHWriteDeliveryUncertain):
			result.DeliveryUncertain++
			if result.LastFailed == nil || event.CreatedAt.After(result.LastFailed.CreatedAt) {
				result.LastFailed = result.cloneEvent(event)
			}
		case string(HHWriteFailed):
			if event.ActionID == "" || !failedAttemptIDs[event.ActionID] {
				result.FailedWrites++
				failedAttemptIDs[event.ActionID] = true
			}
			if result.LastFailed == nil || event.CreatedAt.After(result.LastFailed.CreatedAt) {
				result.LastFailed = result.cloneEvent(event)
			}
		case "transport_response":
			// The transport response is the durable source of truth for an
			// attempt. Count a non-2xx response even if the follow-up terminal
			// event was lost because its store write failed.
			if event.HTTPStatus >= 400 && (event.ActionID == "" || !failedAttemptIDs[event.ActionID]) {
				result.FailedWrites++
				failedAttemptIDs[event.ActionID] = true
			}
			if event.HTTPStatus >= 400 && (result.LastFailed == nil || event.CreatedAt.After(result.LastFailed.CreatedAt)) {
				result.LastFailed = result.cloneEvent(event)
			}
		case "preflight_blocked":
			result.StaleBeforeSend++
			result.BlockedByPreflight++
		case string(HHWriteManualReview):
			if result.LastFailed == nil || event.CreatedAt.After(result.LastFailed.CreatedAt) {
				result.LastFailed = result.cloneEvent(event)
			}
		}
	}
	return result
}

func BuildHHFirstPilotSummary(events []HHWriteEvent) *HHFirstPilotSummary {
	var confirmed *HHWriteEvent
	for _, event := range sortHHWriteEvents(events) {
		if event.Type == string(HHWriteDeliveryConfirmed) && strings.TrimSpace(event.ActionID) != "" {
			copy := event
			confirmed = &copy
			break
		}
	}
	if confirmed == nil {
		return nil
	}
	// Freeze the first-pilot counters at the first confirmed delivery. Later
	// pilots must not rewrite the historical result shown in the dashboard.
	scoped := make([]HHWriteEvent, 0, len(events))
	for _, event := range events {
		if !event.CreatedAt.After(confirmed.CreatedAt) {
			scoped = append(scoped, event)
		}
	}
	metrics := BuildHHWriteMetrics(scoped)
	return &HHFirstPilotSummary{
		ActionID:               confirmed.ActionID,
		Delivery:               string(HHWriteDeliveryConfirmed),
		Attempts:               metrics.WriteAttemptsTotal,
		Successful:             metrics.SuccessfulWrites,
		DeliveryConfirmed:      metrics.DeliveryConfirmed,
		DeliveryUncertain:      metrics.DeliveryUncertain,
		Failed:                 metrics.FailedWrites,
		HistoricalFailuresNote: "The failed count contains historical transport-contract failures from this pilot; it does not describe the current transport after the fix.",
	}
}

type HHWriteStatusReport struct {
	HHWriteEnabled      bool                   `json:"hh_write_enabled"`
	DryRun              bool                   `json:"dry_run"`
	Gateway             string                 `json:"gateway"`
	LegacyWrites        string                 `json:"legacy_writes"`
	CareerMonitor       string                 `json:"career_monitor"`
	AIReplyOrchestrator string                 `json:"ai_reply_orchestrator"`
	Frontend            string                 `json:"frontend"`
	PendingApproved     int                    `json:"pending_approved_actions"`
	PendingDetails      []HHWritePendingAction `json:"pending_approved_action_details,omitempty"`
	ExcludedApproved    []HHWritePendingAction `json:"excluded_approved_actions,omitempty"`
	DeliveryUncertain   int                    `json:"delivery_uncertain"`
	LastSuccessfulWrite *HHWriteEvent          `json:"last_successful_write,omitempty"`
	LastFailedWrite     *HHWriteEvent          `json:"last_failed_write,omitempty"`
	Metrics             HHWriteMetrics         `json:"metrics"`
	MaxWritesPerRun     int                    `json:"max_writes_per_run"`
	MaxWritesPerDay     int                    `json:"max_writes_per_day"`
	FirstPilot          *HHFirstPilotSummary   `json:"first_pilot,omitempty"`
}

type HHEligibleConversation struct {
	ConversationID         string             `json:"conversation_id"`
	Company                string             `json:"company"`
	Vacancy                string             `json:"vacancy"`
	CurrentState           ConversationStatus `json:"current_state"`
	LastMessageAt          *time.Time         `json:"last_message_timestamp,omitempty"`
	HasStrongHHDestination bool               `json:"has_strong_hh_destination"`
	CandidateContextStatus string             `json:"candidate_context_status"`
	ClarificationStatus    string             `json:"clarification_status"`
	ConsistencyStatus      string             `json:"consistency_status"`
	Classification         string             `json:"classification"`
	Reasons                []string           `json:"reasons,omitempty"`
}

func BuildHHEligibleConversations(conversations *ConversationStore, applications *ApplicationStore, clarifications *CandidateClarificationStore, resolver *CandidateContextResolver) ([]HHEligibleConversation, error) {
	reports, err := BuildConversationEligibilityReports(conversations, applications, clarifications, resolver)
	if err != nil {
		return nil, err
	}
	values, err := conversations.ListConversations()
	if err != nil {
		return nil, err
	}
	byID := map[string]EmployerConversation{}
	for _, conversation := range values {
		byID[conversation.ID] = conversation
	}
	result := make([]HHEligibleConversation, 0, len(reports))
	for _, report := range reports {
		conversation := byID[report.ConversationID]
		reasons := make([]string, 0, len(report.Blockers)+len(report.Warnings)+len(report.Informational))
		for _, finding := range append(append(append([]EligibilityFinding{}, report.Blockers...), report.Warnings...), report.Informational...) {
			reasons = append(reasons, finding.Code)
		}
		currentState := ConversationStatus(report.StateStatus)
		if currentState == "no_human_message" || currentState == "unknown" {
			currentState = ConversationManualReview
		}
		consistencyStatus := "none"
		if report.CandidateContextStatus == "CONFLICT" || len(report.Warnings) > 0 {
			consistencyStatus = "warnings"
		}
		result = append(result, HHEligibleConversation{ConversationID: report.ConversationID, Company: conversation.CompanyName, Vacancy: conversation.VacancyTitle, CurrentState: currentState, LastMessageAt: conversation.LastActivityAt, HasStrongHHDestination: report.DestinationStatus == "STRONG", CandidateContextStatus: report.CandidateContextStatus, ClarificationStatus: strings.ToLower(report.ClarificationStatus), ConsistencyStatus: consistencyStatus, Classification: string(report.Classification), Reasons: uniqueStrings(reasons)})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Classification != result[j].Classification {
			return result[i].Classification < result[j].Classification
		}
		return result[i].Company < result[j].Company
	})
	return result, nil
}

func pendingApprovedActionDetails(actions *ApprovedHHActionStore, conversations *ConversationStore) ([]HHWritePendingAction, []HHWritePendingAction) {
	pending, excluded := []HHWritePendingAction{}, []HHWritePendingAction{}
	if actions == nil {
		return pending, excluded
	}
	for _, action := range actions.List() {
		if action.Status != HHWriteApproved {
			continue
		}
		detail := HHWritePendingAction{ActionID: action.ID, ConversationID: action.ConversationID, Status: action.Status, Sendability: "AWAITING_FRESH_PREFLIGHT", Reason: "Approved action awaits a fresh read-only preflight; no HH write has been performed."}
		if action.NonceUsedAt != nil {
			detail.Sendability, detail.Reason = "NOT_SENDABLE", "The send nonce is already consumed."
			excluded = append(excluded, detail)
			continue
		}
		if conversations != nil {
			conversation, err := conversations.GetConversation(action.ConversationID)
			if err != nil {
				detail.Sendability, detail.Reason = "REVIEW_REQUIRED", "The linked conversation is unavailable."
				excluded = append(excluded, detail)
				continue
			}
			detail.Conversation = strings.TrimSpace(conversation.CompanyName + " / " + conversation.VacancyTitle)
			if terminal, ok := terminalConversationState(conversation); ok {
				detail.Sendability, detail.Reason = "NOT_SENDABLE", "The conversation is terminal: "+string(terminal)+"."
				excluded = append(excluded, detail)
				continue
			}
			if action.ConversationVersion != "" && action.ConversationVersion != conversationVersion(conversation) || action.LastMessageID != latestDeliveredMessageID(conversation) {
				detail.Sendability, detail.Reason = "STALE", "The conversation changed after approval."
				excluded = append(excluded, detail)
				continue
			}
			if action.ActionType == HHActionConversationReply {
				latest := latestDeliveredMessage(conversation)
				if latest == nil || latest.Sender != ConversationSenderEmployer {
					detail.Sendability, detail.Reason = "STALE", "There is no unanswered employer message for this approved reply."
					excluded = append(excluded, detail)
					continue
				}
			}
		}
		pending = append(pending, detail)
	}
	return pending, excluded
}

func BuildHHWriteStatusReportWithConversations(cfg Config, actions *ApprovedHHActionStore, audit *HHWriteAuditStore, conversations *ConversationStore) HHWriteStatusReport {
	events := []HHWriteEvent{}
	if audit != nil {
		events = audit.List()
	}
	metrics := BuildHHWriteMetrics(events)
	pendingDetails, excludedDetails := pendingApprovedActionDetails(actions, conversations)
	gateway := "DISABLED"
	if cfg.DryRun {
		gateway = "BLOCKED_BY_DRY_RUN"
	} else if cfg.HHWriteEnabled {
		gateway = "MANUAL_APPROVAL"
	}
	return HHWriteStatusReport{
		HHWriteEnabled:      cfg.HHWriteEnabled,
		DryRun:              cfg.DryRun,
		Gateway:             gateway,
		LegacyWrites:        "BLOCKED_BY_GATEWAY",
		CareerMonitor:       "READ_ONLY",
		AIReplyOrchestrator: "NO_WRITE_CAPABILITY",
		Frontend:            "MANUAL_APPROVAL_AND_SEND_ONLY",
		PendingApproved:     len(pendingDetails),
		PendingDetails:      pendingDetails,
		ExcludedApproved:    excludedDetails,
		DeliveryUncertain:   metrics.DeliveryUncertain,
		LastSuccessfulWrite: metrics.LastSuccessful,
		LastFailedWrite:     metrics.LastFailed,
		Metrics:             metrics,
		MaxWritesPerRun:     cfg.HHMaxWritesPerRun,
		MaxWritesPerDay:     cfg.HHMaxWritesPerDay,
		FirstPilot:          BuildHHFirstPilotSummary(events),
	}
}

func BuildHHWriteStatusReport(cfg Config, actions *ApprovedHHActionStore, audit *HHWriteAuditStore) HHWriteStatusReport {
	return BuildHHWriteStatusReportWithConversations(cfg, actions, audit, nil)
}

func (r HHWriteStatusReport) text() string {
	lastSuccess := "none"
	if r.LastSuccessfulWrite != nil {
		lastSuccess = r.LastSuccessfulWrite.CreatedAt.UTC().Format(time.RFC3339)
	}
	lastFailed := "none"
	if r.LastFailedWrite != nil {
		lastFailed = r.LastFailedWrite.CreatedAt.UTC().Format(time.RFC3339)
	}
	lines := []string{
		"HH write enabled: " + boolWord(r.HHWriteEnabled),
		"Dry run: " + boolWord(r.DryRun),
		"Gateway: " + r.Gateway,
		"Legacy writes: " + r.LegacyWrites,
		"CareerMonitor: " + r.CareerMonitor,
		"AIReplyOrchestrator: " + r.AIReplyOrchestrator,
		"Frontend: " + r.Frontend,
		"Pending approved actions: " + itoa(r.PendingApproved),
		"Delivery uncertain: " + itoa(r.DeliveryUncertain),
		"Last successful write: " + lastSuccess,
		"Last failed write: " + lastFailed,
		"HH max writes per run: " + itoa(r.MaxWritesPerRun),
		"HH max writes per day: " + itoa(r.MaxWritesPerDay),
		"write_attempts_total: " + itoa(r.Metrics.WriteAttemptsTotal),
		"manual_writes_total (legacy alias, attempts): " + itoa(r.Metrics.ManualWritesTotal),
		"successful_writes: " + itoa(r.Metrics.SuccessfulWrites),
		"delivery_confirmed: " + itoa(r.Metrics.DeliveryConfirmed),
		"delivery_uncertain: " + itoa(r.Metrics.DeliveryUncertain),
		"failed_writes: " + itoa(r.Metrics.FailedWrites),
		"stale_before_send: " + itoa(r.Metrics.StaleBeforeSend),
		"blocked_by_preflight: " + itoa(r.Metrics.BlockedByPreflight),
	}
	if r.FirstPilot != nil {
		lines = append(lines,
			"First pilot: "+r.FirstPilot.Delivery,
			"First pilot action: "+r.FirstPilot.ActionID,
			"First pilot attempts: "+itoa(r.FirstPilot.Attempts),
			"First pilot successful: "+itoa(r.FirstPilot.Successful),
			"First pilot failed: "+itoa(r.FirstPilot.Failed),
		)
	}
	if len(r.PendingDetails) == 0 {
		lines = append(lines, "Pending approved details: none")
	} else {
		for _, detail := range r.PendingDetails {
			lines = append(lines, "Pending approved detail: "+detail.ActionID+" · "+detail.Conversation+" · "+detail.ConversationID+" · "+string(detail.Status)+" · "+detail.Sendability+" · "+detail.Reason)
		}
	}
	for _, detail := range r.ExcludedApproved {
		lines = append(lines, "Excluded approved action: "+detail.ActionID+" · "+detail.Conversation+" · "+detail.ConversationID+" · "+string(detail.Status)+" · "+detail.Sendability+" · "+detail.Reason)
	}
	return strings.Join(lines, "\n")
}

func boolWord(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func sortHHWriteEvents(events []HHWriteEvent) []HHWriteEvent {
	result := append([]HHWriteEvent{}, events...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}
