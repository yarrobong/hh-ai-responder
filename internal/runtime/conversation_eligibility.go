package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EligibilityClassification is intentionally separate from ConversationStatus.
// A local state warning must not silently become a write permission.
type EligibilityClassification string

const (
	EligibilitySafe         EligibilityClassification = "SAFE_FOR_MANUAL_REPLY"
	EligibilityBlocked      EligibilityClassification = "BLOCKED"
	EligibilityManualReview EligibilityClassification = "MANUAL_REVIEW"
)

type EligibilitySeverity string

const (
	EligibilitySeverityBlock  EligibilitySeverity = "BLOCK"
	EligibilitySeverityReview EligibilitySeverity = "REVIEW"
	EligibilitySeverityWarn   EligibilitySeverity = "WARNING"
	EligibilitySeverityInfo   EligibilitySeverity = "INFO"
)

// EligibilityFinding is safe for aggregate output: it contains no message
// bodies or candidate facts. The code is stable; the message is explanatory.
type EligibilityFinding struct {
	Code             string              `json:"code"`
	Severity         EligibilitySeverity `json:"severity"`
	Recoverable      bool                `json:"recoverable"`
	RecoveryStrategy string              `json:"recovery_strategy"`
	Message          string              `json:"message,omitempty"`
}

type EligibilityBlockerDefinition struct {
	Code             string              `json:"code"`
	Severity         EligibilitySeverity `json:"severity"`
	Recoverable      bool                `json:"recoverable"`
	RecoveryStrategy string              `json:"recovery_strategy"`
}

var eligibilityBlockerDefinitions = map[string]EligibilityBlockerDefinition{
	"NO_HH_DESTINATION":              {"NO_HH_DESTINATION", EligibilitySeverityBlock, true, "sync a conversation with a real HH chat identifier"},
	"AMBIGUOUS_DESTINATION":          {"AMBIGUOUS_DESTINATION", EligibilitySeverityReview, true, "reconcile the HH chat/topic identifiers"},
	"NO_HUMAN_EMPLOYER_MESSAGE":      {"NO_HUMAN_EMPLOYER_MESSAGE", EligibilitySeverityBlock, false, "wait for a human employer message"},
	"INCOMPLETE_HISTORY":             {"INCOMPLETE_HISTORY", EligibilitySeverityReview, true, "reload the complete HH history"},
	"HAS_MORE_UNRESOLVED":            {"HAS_MORE_UNRESOLVED", EligibilitySeverityReview, true, "resolve pending clarification or conversation questions"},
	"UNKNOWN_SENDER":                 {"UNKNOWN_SENDER", EligibilitySeverityReview, true, "re-sync and verify the message participant"},
	"UNKNOWN_DIRECTION":              {"UNKNOWN_DIRECTION", EligibilitySeverityReview, true, "re-sync and verify message direction"},
	"STATE_CONFLICT":                 {"STATE_CONFLICT", EligibilitySeverityReview, true, "reconcile stored and HH conversation state"},
	"CRITICAL_CLARIFICATION":         {"CRITICAL_CLARIFICATION", EligibilitySeverityReview, true, "answer or remove the candidate clarification"},
	"KNOWLEDGE_CONFLICT":             {"KNOWLEDGE_CONFLICT", EligibilitySeverityReview, true, "resolve conflicting candidate knowledge"},
	"UNKNOWN_HH_STATUS":              {"UNKNOWN_HH_STATUS", EligibilitySeverityReview, true, "sync a known HH status before acting"},
	"MANUAL_REVIEW_FLAG":             {"MANUAL_REVIEW_FLAG", EligibilitySeverityReview, true, "review and explicitly clear the local flag"},
	"UNSUPPORTED_EMPLOYER_INTENT":    {"UNSUPPORTED_EMPLOYER_INTENT", EligibilitySeverityReview, true, "manually interpret the employer message before drafting"},
	"CLOSED_CONVERSATION":            {"CLOSED_CONVERSATION", EligibilitySeverityBlock, false, "do not reply to a closed conversation"},
	"REJECTED":                       {"REJECTED", EligibilitySeverityBlock, false, "do not reply after rejection"},
	"OFFER":                          {"OFFER", EligibilitySeverityBlock, false, "route offer handling to the candidate"},
	"STALE_LOCAL_DATA":               {"STALE_LOCAL_DATA", EligibilitySeverityReview, true, "sync HH again before reviewing the conversation"},
	"BROKEN_RELATION":                {"BROKEN_RELATION", EligibilitySeverityReview, true, "reconcile local application and conversation links"},
	"WAITING_FOR_EMPLOYER":           {"WAITING_FOR_EMPLOYER", EligibilitySeverityBlock, false, "wait for a human employer message"},
	"CONTEXT_UNAVAILABLE":            {"CONTEXT_UNAVAILABLE", EligibilitySeverityReview, true, "repair candidate context data and retry"},
	"INTERVIEW_REQUIRES_REVIEW":      {"INTERVIEW_REQUIRES_REVIEW", EligibilitySeverityReview, false, "handle interview coordination manually"},
	"EXTERNAL_ACTION_REQUIRED":       {"EXTERNAL_ACTION_REQUIRED", EligibilitySeverityReview, false, "open the external invitation and handle it manually"},
	"USER_CONFIRMATION_REQUIRED":     {"USER_CONFIRMATION_REQUIRED", EligibilitySeverityReview, true, "confirm the employer instruction with the candidate"},
	"FOLLOW_UP_REQUIRES_APPLICATION": {"FOLLOW_UP_REQUIRES_APPLICATION", EligibilitySeverityReview, true, "confirm the application and vacancy relation"},
	"CANDIDATE_ACTION_REQUIRED":      {"CANDIDATE_ACTION_REQUIRED", EligibilitySeverityBlock, false, "reply to the employer before a follow-up"},
	"NO_REPLY_NEEDED":                {"NO_REPLY_NEEDED", EligibilitySeverityBlock, false, "do not reply to a terminal or informational message"},
	"REPLY_OPTIONAL":                 {"REPLY_OPTIONAL", EligibilitySeverityBlock, false, "do not treat a courtesy reply as required"},
}

// EligibilityBlockerDefinitions returns a copy so callers cannot alter the
// policy used by the evaluator.
func EligibilityBlockerDefinitions() map[string]EligibilityBlockerDefinition {
	result := make(map[string]EligibilityBlockerDefinition, len(eligibilityBlockerDefinitions))
	for code, definition := range eligibilityBlockerDefinitions {
		result[code] = definition
	}
	return result
}

type ConversationEligibilityReport struct {
	ConversationID         string                     `json:"conversation_id"`
	HHTopicID              string                     `json:"hh_topic_id,omitempty"`
	HHConversationID       string                     `json:"hh_conversation_id,omitempty"`
	VacancyID              int                        `json:"vacancy_id"`
	ApplicationID          string                     `json:"application_id,omitempty"`
	Classification         EligibilityClassification  `json:"classification"`
	Blockers               []EligibilityFinding       `json:"blockers,omitempty"`
	Warnings               []EligibilityFinding       `json:"warnings,omitempty"`
	Informational          []EligibilityFinding       `json:"informational,omitempty"`
	DestinationStatus      string                     `json:"destination_status"`
	HistoryStatus          string                     `json:"history_status"`
	StateStatus            string                     `json:"state_status"`
	CandidateContextStatus string                     `json:"candidate_context_status"`
	ExternalAction         *ExternalActionRequirement `json:"external_action,omitempty"`
	ClarificationStatus    string                     `json:"clarification_status"`
	AuditStatus            string                     `json:"audit_status"`
	WritePreflightStatus   string                     `json:"write_preflight_status"`
}

type EligibilityEvaluationInput struct {
	Conversation         EmployerConversation
	Application          JobApplication
	Applications         []JobApplication
	PendingClarification bool
	Context              *ConversationContext
	ContextError         error
	AppliedAt            *time.Time
	Now                  time.Time
}

type EligibilityDecision struct {
	Eligible       bool
	Classification EligibilityClassification
	Blockers       []EligibilityFinding
	Warnings       []EligibilityFinding
	Informational  []EligibilityFinding
	StateStatus    string
}

func eligibilityFinding(code, message string) EligibilityFinding {
	definition, ok := eligibilityBlockerDefinitions[code]
	if !ok {
		definition = EligibilityBlockerDefinition{Code: code, Severity: EligibilitySeverityWarn, Recoverable: true, RecoveryStrategy: "inspect the local diagnostic and re-sync if needed"}
	}
	return EligibilityFinding{Code: code, Severity: definition.Severity, Recoverable: definition.Recoverable, RecoveryStrategy: definition.RecoveryStrategy, Message: message}
}

func appendEligibilityFinding(items []EligibilityFinding, value EligibilityFinding) []EligibilityFinding {
	for _, item := range items {
		if item.Code == value.Code {
			return items
		}
	}
	return append(items, value)
}

func EvaluateReplyEligibility(input EligibilityEvaluationInput) EligibilityDecision {
	return evaluateConversationEligibility(input, false)
}

// EvaluateFollowUpEligibility deliberately adds requirements that a normal
// reply does not have: application/vacancy linkage and a confirmed waiting
// state. A direct HH chat destination is sufficient for a normal reply.
func EvaluateFollowUpEligibility(input EligibilityEvaluationInput) EligibilityDecision {
	decision := evaluateConversationEligibility(input, true)
	c := input.Conversation
	if decision.StateStatus == string(ConversationCandidateActionRequired) {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("CANDIDATE_ACTION_REQUIRED", "the employer is waiting for a reply"))
	} else if decision.StateStatus != string(ConversationWaitingEmployer) {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("STATE_CONFLICT", "follow-up requires a human candidate message followed by waiting for the employer"))
	}
	if len(input.Applications) != 1 || input.Application.ID == "" || input.Application.VacancyID <= 0 || c.VacancyID <= 0 || input.Application.VacancyID != c.VacancyID {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("FOLLOW_UP_REQUIRES_APPLICATION", "follow-up requires one confirmed application and vacancy relation"))
	}
	if input.Application.Source != ApplicationSourceHH && input.Application.ExternalID == "" {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("FOLLOW_UP_REQUIRES_APPLICATION", "follow-up requires a confirmed HH application"))
	}
	if c.Status != ConversationWaitingEmployer {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("STATE_CONFLICT", "follow-up requires a waiting-for-employer state"))
	}
	if input.AppliedAt == nil || input.AppliedAt.IsZero() {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding("FOLLOW_UP_REQUIRES_APPLICATION", "application delivery time is not confirmed"))
	}
	if len(decision.Blockers) > 0 {
		decision.Eligible = false
		decision.Classification = eligibilityClassForFindings(decision.Blockers)
	} else {
		decision.Eligible = true
		decision.Classification = EligibilitySafe
	}
	return decision
}

func evaluateConversationEligibility(input EligibilityEvaluationInput, followUp bool) EligibilityDecision {
	c := input.Conversation
	decision := EligibilityDecision{Classification: EligibilitySafe, StateStatus: "unknown"}
	addBlocker := func(code, message string) {
		decision.Blockers = appendEligibilityFinding(decision.Blockers, eligibilityFinding(code, message))
	}
	addWarning := func(code, message string) {
		decision.Warnings = appendEligibilityFinding(decision.Warnings, eligibilityFinding(code, message))
	}
	addInfo := func(code, message string) {
		finding := eligibilityFinding(code, message)
		finding.Severity = EligibilitySeverityInfo
		decision.Informational = appendEligibilityFinding(decision.Informational, finding)
	}

	if strings.TrimSpace(c.HHConversationID) == "" {
		addBlocker("NO_HH_DESTINATION", "a real HH conversation identifier is absent")
	} else if parseHHChatID(c.HHConversationID) <= 0 {
		addBlocker("AMBIGUOUS_DESTINATION", "the stored HH conversation identifier is not a valid chat destination")
	}
	if id := firstMetadata(c.HHMetadata, "topic_id", "hh_topic_id", "negotiation_topic_id", "negotiation_id", "hh_id"); id != "" {
		if other := firstMetadata(c.HHMetadata, "chat_id", "conversation_id"); other != "" && other != c.HHConversationID {
			addBlocker("AMBIGUOUS_DESTINATION", "HH chat identifiers disagree")
		}
	}

	human := deliveredMessages(c.Messages)
	latest := latestMessage(human)
	unknownSender, unknownDirection := false, false
	for _, message := range c.Messages {
		if message.HHSystemEvent || message.Sender == ConversationSenderSystem || message.Source == ConversationSourceAIDraft {
			continue
		}
		if message.Sender == ConversationSenderUnknown {
			unknownSender = true
		}
		if message.Direction == ConversationDirectionUnknown {
			unknownDirection = true
		}
		if message.ContentUnavailable {
			addBlocker("INCOMPLETE_HISTORY", "a human message has unavailable content")
		}
	}
	if metadataTrue(c.HHMetadata, "history_incomplete") || metadataTrue(c.HHMetadata, "warning_skipped_messages") || metadataTrue(c.HHMetadata, "warning_missing_latest_message") {
		addBlocker("INCOMPLETE_HISTORY", "HH did not provide a complete conversation history")
	}
	if unknownSender {
		addBlocker("UNKNOWN_SENDER", "a human message has no trusted participant")
	}
	if unknownDirection {
		addBlocker("UNKNOWN_DIRECTION", "a human message has no trusted direction")
	}
	if latest == nil {
		addBlocker("NO_HUMAN_EMPLOYER_MESSAGE", "there is no recorded human employer message")
		decision.StateStatus = "no_human_message"
	} else if latest.Sender == ConversationSenderEmployer {
		if terminal, ok := terminalConversationState(c); ok {
			decision.StateStatus = string(terminal)
			addBlocker("NO_REPLY_NEEDED", "the employer message is terminal")
		} else if external := externalInterviewAction(latest.Text, latest.Timestamp, time.Now().UTC()); external != nil {
			decision.StateStatus = string(ConversationEmployerReplied)
			addBlocker("EXTERNAL_ACTION_REQUIRED", fmt.Sprintf("%s / %s", external.Classification, external.Type))
		} else {
			switch conversationReplyRequirement(c, latest, classifyEmployerMessage(latest.Text)) {
			case ReplyRequired:
				decision.StateStatus = string(ConversationCandidateActionRequired)
			case ReplyOptional:
				decision.StateStatus = string(ConversationEmployerReplied)
				addBlocker("REPLY_OPTIONAL", "the employer message does not require a candidate reply")
			default:
				decision.StateStatus = string(ConversationEmployerReplied)
				addBlocker("NO_REPLY_NEEDED", "the employer message is terminal or informational")
			}
		}
	} else {
		decision.StateStatus = string(ConversationWaitingEmployer)
		if !followUp {
			addBlocker("WAITING_FOR_EMPLOYER", "the latest human message is from the candidate")
		}
	}

	if metadataTrue(c.HHMetadata, "warning_message_content_conflict") || metadataTrue(c.HHMetadata, "warning_conflicting_topics") {
		addBlocker("STATE_CONFLICT", "HH supplied conflicting conversation evidence")
	}
	if c.HHUpdatedAt.After(c.UpdatedAt) && !c.HHUpdatedAt.IsZero() {
		addBlocker("STALE_LOCAL_DATA", "HH data is newer than the local conversation snapshot")
	}
	if metadataTrue(c.HHMetadata, "manual_review") || c.NextAction == "manual_review" {
		addBlocker("MANUAL_REVIEW_FLAG", "an explicit local manual-review flag is set")
	}

	if strings.TrimSpace(c.RawStatus) == "" && strings.TrimSpace(c.HHConversationID) != "" {
		addBlocker("UNKNOWN_HH_STATUS", "the HH conversation status is absent")
	} else if rawStatus, known := MapHHApplicationStatus(c.RawStatus); !known && strings.TrimSpace(c.RawStatus) != "" {
		addBlocker("UNKNOWN_HH_STATUS", "the HH conversation status is not mapped")
	} else {
		switch rawStatus {
		case ApplicationRejected:
			addBlocker("REJECTED", "HH reports a rejected conversation")
		case ApplicationOffer:
			addBlocker("OFFER", "HH reports an offer")
		case ApplicationArchived:
			addBlocker("CLOSED_CONVERSATION", "HH reports an archived conversation")
		case ApplicationInterview:
			addBlocker("INTERVIEW_REQUIRES_REVIEW", "HH reports an interview state")
		}
	}
	if terminal, terminalState := terminalConversationState(c); terminalState {
		switch terminal {
		case ConversationRejected:
			addBlocker("REJECTED", "the conversation is terminal and the candidate was rejected")
		case ConversationClosed:
			addBlocker("CLOSED_CONVERSATION", "the vacancy or conversation is closed")
		}
	} else if c.Status == ConversationRejected {
		addBlocker("REJECTED", "local state is rejected")
	} else if c.Status == ConversationOffer {
		addBlocker("OFFER", "local state is offer")
	} else if c.Status == ConversationClosed {
		addBlocker("CLOSED_CONVERSATION", "local state is closed")
	} else if latest != nil && ((latest.Sender == ConversationSenderEmployer && c.Status == ConversationWaitingEmployer) || (latest.Sender == ConversationSenderCandidate && c.Status == ConversationCandidateActionRequired)) {
		addBlocker("STATE_CONFLICT", "stored state disagrees with the latest human message")
	}
	if latest != nil && latest.Sender == ConversationSenderEmployer && c.NextAction == string(NextActionWaitingEmployerReply) {
		addBlocker("STATE_CONFLICT", "next action says to wait for an employer after an employer message")
	}

	if input.PendingClarification || len(c.Summary.PendingQuestions) > 0 {
		addBlocker("HAS_MORE_UNRESOLVED", "candidate clarification or pending conversation question exists")
	}
	if input.ContextError != nil {
		addBlocker("CONTEXT_UNAVAILABLE", "candidate context could not be built")
	} else if input.Context == nil {
		addBlocker("CONTEXT_UNAVAILABLE", "candidate context was not supplied")
	} else {
		// Keep the eligibility rule aligned with the reply orchestrator: its
		// generic "clarify the meaning" hint is guidance when confirmed facts
		// already cover the topic, not a critical candidate clarification.
		if input.Context.CandidateContext.UserConfirmationRequired {
			addBlocker("USER_CONFIRMATION_REQUIRED", "the employer instruction requires explicit candidate confirmation")
		} else if len(input.Context.UnresolvedQuestions) > 0 && conversationNeedsCandidateInput(*input.Context) {
			addBlocker("CRITICAL_CLARIFICATION", "the reply would require an unknown candidate fact")
		}
		if input.Context.CandidateContext.UnsupportedIntent {
			addBlocker("UNSUPPORTED_EMPLOYER_INTENT", "the employer question is unsupported and requires manual interpretation")
		}
		if len(input.Context.ConsistencyWarnings) > 0 {
			addBlocker("KNOWLEDGE_CONFLICT", "conversation claims conflict with confirmed candidate knowledge")
		}
	}
	for key, value := range c.HHMetadata {
		if !metadataTrue(map[string]string{key: value}, key) || !strings.HasPrefix(key, "warning") {
			continue
		}
		switch key {
		case "warning_message_content_conflict", "warning_conflicting_topics", "warning_skipped_messages", "warning_missing_latest_message", "warning_message_content_unavailable":
			// Covered by the precise deterministic codes above.
		default:
			addWarning("AUDIT_WARNING", "an HH import warning remains attached to the conversation")
		}
	}
	if len(input.Applications) == 0 {
		addInfo("APPLICATION_NOT_REQUIRED_FOR_REPLY", "a direct HH conversation destination is sufficient for a normal reply")
	} else if len(input.Applications) > 1 {
		addWarning("BROKEN_RELATION", "more than one local application points to this conversation")
	}
	if followUp {
		// Strict follow-up checks are applied by EvaluateFollowUpEligibility.
		return finalizeEligibility(decision)
	}
	return finalizeEligibility(decision)
}

func finalizeEligibility(decision EligibilityDecision) EligibilityDecision {
	if len(decision.Blockers) == 0 && decision.StateStatus == string(ConversationCandidateActionRequired) {
		decision.Eligible = true
		decision.Classification = EligibilitySafe
		return decision
	}
	decision.Eligible = false
	decision.Classification = eligibilityClassForFindings(decision.Blockers)
	return decision
}

func eligibilityClassForFindings(findings []EligibilityFinding) EligibilityClassification {
	if len(findings) == 0 {
		return EligibilityManualReview
	}
	// A known terminal/non-actionable state wins over unrelated incomplete
	// metadata: there is no safe reply to make, even if another warning also
	// needs cleanup.
	for _, finding := range findings {
		if finding.Severity == EligibilitySeverityBlock {
			return EligibilityBlocked
		}
	}
	for _, finding := range findings {
		if finding.Severity == EligibilitySeverityReview || finding.Severity == EligibilitySeverityWarn {
			return EligibilityManualReview
		}
	}
	return EligibilityBlocked
}

func latestMessage(messages []ConversationMessage) *ConversationMessage {
	if len(messages) == 0 {
		return nil
	}
	latest := messages[0]
	for _, message := range messages[1:] {
		if message.Timestamp.After(latest.Timestamp) {
			latest = message
		}
	}
	return &latest
}

func firstMetadata(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func metadataTrue(values map[string]string, key string) bool {
	switch strings.ToLower(strings.TrimSpace(values[key])) {
	case "1", "true", "yes", "warning", "incomplete":
		return true
	default:
		return false
	}
}

type EligibilitySummary struct {
	Total        int                    `json:"total"`
	Safe         int                    `json:"safe_for_manual_reply"`
	Blocked      int                    `json:"blocked"`
	ManualReview int                    `json:"manual_review"`
	TopBlockers  []EligibilityCodeCount `json:"top_blockers"`
}

type EligibilityCodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

func SummarizeEligibility(reports []ConversationEligibilityReport) EligibilitySummary {
	result := EligibilitySummary{Total: len(reports)}
	counts := map[string]int{}
	for _, report := range reports {
		switch report.Classification {
		case EligibilitySafe:
			result.Safe++
		case EligibilityBlocked:
			result.Blocked++
		case EligibilityManualReview:
			result.ManualReview++
		}
		for _, blocker := range report.Blockers {
			counts[blocker.Code]++
		}
	}
	for code, count := range counts {
		result.TopBlockers = append(result.TopBlockers, EligibilityCodeCount{Code: code, Count: count})
	}
	sort.Slice(result.TopBlockers, func(i, j int) bool {
		if result.TopBlockers[i].Count != result.TopBlockers[j].Count {
			return result.TopBlockers[i].Count > result.TopBlockers[j].Count
		}
		return result.TopBlockers[i].Code < result.TopBlockers[j].Code
	})
	return result
}

func BuildConversationEligibilityReports(conversations *ConversationStore, applications *ApplicationStore, clarifications *CandidateClarificationStore, resolver *CandidateContextResolver) ([]ConversationEligibilityReport, error) {
	if conversations == nil || resolver == nil {
		return nil, errors.New("eligibility report dependencies are unavailable")
	}
	values, err := conversations.ListConversations()
	if err != nil {
		return nil, err
	}
	allApplications := []JobApplication{}
	if applications != nil {
		allApplications, err = applications.ListApplications()
		if err != nil {
			return nil, err
		}
	}
	byConversation := map[string][]JobApplication{}
	for _, application := range allApplications {
		if application.ConversationID != "" {
			byConversation[application.ConversationID] = append(byConversation[application.ConversationID], application)
		}
	}
	pending := map[string]bool{}
	if clarifications != nil {
		items, listErr := clarifications.List()
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range items {
			if item.Status == ClarificationPending {
				pending[item.ConversationID] = true
			}
		}
	}
	builder := NewConversationContextBuilder(conversations, resolver)
	result := make([]ConversationEligibilityReport, 0, len(values))
	for _, conversation := range values {
		linked := append([]JobApplication{}, byConversation[conversation.ID]...)
		for _, application := range allApplications {
			if conversation.HHConversationID != "" && application.HHMetadata["conversation_external_id"] == conversation.HHConversationID {
				found := false
				for _, old := range linked {
					found = found || old.ID == application.ID
				}
				if !found {
					linked = append(linked, application)
				}
			}
		}
		application := JobApplication{}
		if len(linked) == 1 {
			application = linked[0]
		}
		context, contextErr := builder.BuildForReply(conversation.ID)
		pendingClarification := pending[conversation.ID]
		// A stale clarification must not remain an eligibility blocker after
		// the resolver can answer the current atomic fact from the KB.
		if contextErr == nil && !context.CandidateContext.RequiresCandidateInput() {
			pendingClarification = false
		}
		input := EligibilityEvaluationInput{Conversation: conversation, Application: application, Applications: linked, PendingClarification: pendingClarification, Now: time.Now().UTC()}
		if contextErr != nil {
			input.ContextError = contextErr
		} else {
			input.Context = &context
		}
		decision := EvaluateReplyEligibility(input)
		report := ConversationEligibilityReport{ConversationID: conversation.ID, HHConversationID: conversation.HHConversationID, VacancyID: conversation.VacancyID, Classification: decision.Classification, Blockers: decision.Blockers, Warnings: decision.Warnings, Informational: decision.Informational, StateStatus: decision.StateStatus, DestinationStatus: destinationStatus(conversation), HistoryStatus: historyStatus(conversation), CandidateContextStatus: candidateContextStatus(input), ClarificationStatus: clarificationStatus(input), AuditStatus: auditStatus(conversation, decision), WritePreflightStatus: "REQUIRED_BEFORE_SEND"}
		if latest := latestDeliveredMessage(conversation); latest != nil && latest.Sender == ConversationSenderEmployer {
			report.ExternalAction = externalInterviewAction(latest.Text, latest.Timestamp, time.Now().UTC())
		}
		if topic := firstMetadata(conversation.HHMetadata, "topic_id", "hh_topic_id", "negotiation_topic_id", "negotiation_id", "hh_id"); topic != "" {
			report.HHTopicID = topic
		}
		if len(linked) == 1 {
			report.ApplicationID = application.ID
			if report.HHTopicID == "" {
				report.HHTopicID = firstMetadata(application.HHMetadata, "topic_id", "hh_topic_id", "negotiation_topic_id", "negotiation_id", "hh_id")
			}
		}
		if report.Classification == EligibilitySafe {
			report.WritePreflightStatus = "REQUIRED_BEFORE_WRITE"
		}
		result = append(result, report)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Classification != result[j].Classification {
			return result[i].Classification < result[j].Classification
		}
		return result[i].ConversationID < result[j].ConversationID
	})
	return result, nil
}

func destinationStatus(c EmployerConversation) string {
	if strings.TrimSpace(c.HHConversationID) == "" {
		return "MISSING"
	}
	if parseHHChatID(c.HHConversationID) <= 0 {
		return "AMBIGUOUS"
	}
	return "STRONG"
}

func historyStatus(c EmployerConversation) string {
	if metadataTrue(c.HHMetadata, "history_incomplete") || metadataTrue(c.HHMetadata, "warning_skipped_messages") {
		return "INCOMPLETE"
	}
	for _, message := range c.Messages {
		if message.HHSystemEvent || message.Source == ConversationSourceAIDraft || message.Sender == ConversationSenderSystem {
			continue
		}
		if message.Sender == ConversationSenderUnknown || message.Direction == ConversationDirectionUnknown || message.ContentUnavailable {
			return "UNTRUSTED"
		}
	}
	if latestMessage(deliveredMessages(c.Messages)) == nil {
		return "NO_HUMAN_MESSAGES"
	}
	return "VALID"
}

func candidateContextStatus(input EligibilityEvaluationInput) string {
	if input.ContextError != nil || input.Context == nil {
		return "UNAVAILABLE"
	}
	if len(input.Context.ConsistencyWarnings) > 0 {
		return "CONFLICT"
	}
	if input.Context.CandidateContext.UserConfirmationRequired {
		return CandidateContextStatusUserConfirmationRequired
	}
	if len(input.Context.UnresolvedQuestions) > 0 && conversationNeedsCandidateInput(*input.Context) {
		return "UNKNOWN"
	}
	return "READY"
}

func clarificationStatus(input EligibilityEvaluationInput) string {
	if input.PendingClarification || len(input.Conversation.Summary.PendingQuestions) > 0 {
		return "PENDING"
	}
	return "NONE"
}

func auditStatus(c EmployerConversation, decision EligibilityDecision) string {
	if len(decision.Blockers) > 0 {
		return "BLOCKED"
	}
	for key, value := range c.HHMetadata {
		if strings.HasPrefix(key, "warning") && strings.TrimSpace(value) != "" && value != "false" {
			return "WARNINGS"
		}
	}
	return "CLEAN"
}
