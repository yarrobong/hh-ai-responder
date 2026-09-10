package conversationpolicy

import (
	"regexp"
	"strings"
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

type ReplyRequirement string

const (
	ReplyRequired ReplyRequirement = "REPLY_REQUIRED"
	ReplyOptional ReplyRequirement = "REPLY_OPTIONAL"
	NoReplyNeeded ReplyRequirement = "NO_REPLY_NEEDED"
)

type ReplyPolicyInput struct {
	Conversation conversation.EmployerConversation
	Latest       *conversation.Message
	Intent       candidatecontext.EmployerMessageIntent
}

type ReplyPolicyResult struct {
	Requirement ReplyRequirement `json:"requirement"`
	Eligible    bool             `json:"eligible"`
	Reason      string           `json:"reason,omitempty"`
}

type EligibilityResult = ReplyPolicyResult

func EvaluateReplyPolicy(input ReplyPolicyInput) ReplyPolicyResult {
	if _, terminal := TerminalConversationState(input.Conversation); terminal {
		return ReplyPolicyResult{Requirement: NoReplyNeeded, Reason: "terminal_conversation"}
	}
	if input.Latest == nil || input.Latest.Sender != conversation.SenderEmployer {
		return ReplyPolicyResult{Requirement: NoReplyNeeded, Reason: "employer_not_waiting"}
	}
	if input.Intent == candidatecontext.EmployerMessageIntentTerminal || input.Intent == candidatecontext.EmployerMessageIntentRejection {
		return ReplyPolicyResult{Requirement: NoReplyNeeded, Reason: "terminal_employer_message"}
	}
	if input.Intent == candidatecontext.EmployerMessageIntentInterviewInvitation && hasExternalDestination(input.Latest.Text) &&
		!candidatecontext.ContainsAny(strings.ToLower(input.Latest.Text), "подтверд", "ответьте", "напишите", "соглас") {
		return ReplyPolicyResult{Requirement: NoReplyNeeded, Reason: "external_interview_action"}
	}
	if input.Intent == candidatecontext.EmployerMessageIntentAcknowledgement || input.Intent == candidatecontext.EmployerMessageIntentStatusMessage {
		return ReplyPolicyResult{Requirement: ReplyOptional, Reason: "courtesy_or_status"}
	}
	return ReplyPolicyResult{Requirement: ReplyRequired, Eligible: true, Reason: "active_employer_message"}
}

func EvaluateReplyEligibility(input ReplyPolicyInput) EligibilityResult {
	return EvaluateReplyPolicy(input)
}

var externalURLRE = regexp.MustCompile(`https?://[^\s<>]+`)
var interviewDurationRE = regexp.MustCompile(`(?i)около\s+([0-9]+\s*[–-]\s*[0-9]+\s*минут)`)

type ExternalActionRequirement struct {
	Classification string `json:"classification"`
	Type           string `json:"type"`
	Destination    string `json:"destination,omitempty"`
	Duration       string `json:"duration,omitempty"`
	RequiredAction string `json:"required_action,omitempty"`
	ResultTiming   string `json:"result_timing,omitempty"`
	Aging          string `json:"aging,omitempty"`
}

const (
	ExternalActionRequired            = "EXTERNAL_ACTION_REQUIRED"
	ExternalActionInterviewInvitation = "INTERVIEW_INVITATION"
)

func ExternalInterviewAction(text string, at, now time.Time) *ExternalActionRequirement {
	if candidatecontext.ClassifyEmployerMessage(text) != candidatecontext.EmployerMessageIntentInterviewInvitation || !hasExternalDestination(text) {
		return nil
	}
	lower := strings.ToLower(text)
	action := &ExternalActionRequirement{Classification: ExternalActionRequired, Type: ExternalActionInterviewInvitation, RequiredAction: "Перейти по внешней ссылке и пройти голосовое интервью."}
	if destination := externalURLRE.FindString(text); destination != "" {
		action.Destination = strings.TrimRight(destination, ".,;:!?)]}")
	}
	if match := interviewDurationRE.FindStringSubmatch(text); len(match) == 2 {
		action.Duration = strings.ReplaceAll(match[1], "-", "–")
	}
	if strings.Contains(lower, "7–10 дней") || strings.Contains(lower, "7-10 дней") {
		action.ResultTiming = "Результат обещан в личном кабинете HH в течение 7–10 дней."
	}
	if !at.IsZero() && !now.IsZero() {
		action.Aging = "known"
	}
	return action
}

func hasExternalDestination(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "getprofi") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

func TerminalEmployerMessage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.NewReplacer("\u00a0", " ", "\u202f", " ").Replace(lower)
	return candidatecontext.ContainsAny(lower,
		"вакансия закрыта", "вакансия закрыта для откликов", "позиция закрыта", "уже закрыли эту позицию", "эту позицию уже закрыли", "позиция уже закрыта",
		"набор завершен", "набор завершён", "найм завершен", "найм завершён", "подбор завершен", "подбор завершён", "место уже занято", "вакансия снята",
		"к сожалению, не готовы", "к сожалению, не можем продолжить", "не готовы продолжить", "выбрали другого кандидата", "другому кандидату сделали предложение",
		"position is closed", "vacancy is closed", "hiring completed", "role has been filled", "we have filled the position", "the position has been filled", "we decided not to proceed", "we will not proceed", "we chose another candidate")
}

func TerminalConversationState(c conversation.EmployerConversation) (State, bool) {
	if c.Status == conversation.StatusRejected || c.Status == conversation.StatusClosed {
		return c.Status, true
	}
	if latest := LatestMeaningfulMessage(c); latest != nil && latest.Sender == conversation.SenderEmployer && TerminalEmployerMessage(latest.Text) {
		lower := strings.ToLower(latest.Text)
		if candidatecontext.ContainsAny(lower, "не готовы", "не можем", "выбрали другого", "другому кандидату", "отказ", "position has been filled", "role has been filled", "not proceed", "another candidate") {
			return conversation.StatusRejected, true
		}
		return conversation.StatusClosed, true
	}
	return "", false
}
