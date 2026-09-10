// Package conversationpolicy contains deterministic, read-only conversation
// policy. It consumes normalized conversation and candidate values only; HH
// transport, storage, AI, and write capability remain outside this package.
package conversationpolicy

import (
	"strings"
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

// State is the semantic conversation state. Persisted conversation status is
// reused deliberately; this package does not introduce a second state enum.
type State = conversation.Status
type ConversationState = State
type ConversationResolution = Resolution
type EmployerMessageIntent = candidatecontext.EmployerMessageIntent

const (
	StateApplied                                   = conversation.StatusApplied
	StateEmployerReplied                           = conversation.StatusEmployerReplied
	StateCandidateActionRequired                   = conversation.StatusCandidateActionRequired
	StateWaitingEmployer                           = conversation.StatusWaitingEmployer
	StateInterview                                 = conversation.StatusInterview
	StateOffer                                     = conversation.StatusOffer
	StateRejected                                  = conversation.StatusRejected
	StateClosed                                    = conversation.StatusClosed
	StateManualReview                        State = "manual_review"
	EmployerMessageIntentFactualQuestion           = candidatecontext.EmployerMessageIntentFactualQuestion
	EmployerMessageIntentInstruction               = candidatecontext.EmployerMessageIntentInstruction
	EmployerMessageIntentInterviewInvitation       = candidatecontext.EmployerMessageIntentInterviewInvitation
	EmployerMessageIntentStatusMessage             = candidatecontext.EmployerMessageIntentStatusMessage
	EmployerMessageIntentRejection                 = candidatecontext.EmployerMessageIntentRejection
	EmployerMessageIntentAcknowledgement           = candidatecontext.EmployerMessageIntentAcknowledgement
	EmployerMessageIntentGeneralMessage            = candidatecontext.EmployerMessageIntentGeneralMessage
	EmployerMessageIntentCompound                  = candidatecontext.EmployerMessageIntentCompound
	EmployerMessageIntentTerminal                  = candidatecontext.EmployerMessageIntentTerminal
)

func ClassifyEmployerMessage(text string) EmployerMessageIntent {
	return candidatecontext.ClassifyEmployerMessage(text)
}

func InstructionNeedsUserConfirmation(text string) bool {
	return candidatecontext.InstructionNeedsUserConfirmation(text)
}

type StatusEvidence struct {
	Status   State
	Known    bool
	Required bool
}

type ResolveStateInput struct {
	Conversation          conversation.EmployerConversation
	ApplicationState      State
	ApplicationNextAction string
	RawStatusEvidence     []StatusEvidence
	AppliedAt             *time.Time
	Pending               bool
	Warnings              []string
	Now                   time.Time
}

type Resolution struct {
	Status        State                 `json:"status"`
	WaitingSince  *time.Time            `json:"waiting_since"`
	LatestMessage *conversation.Message `json:"latest_message,omitempty"`
	Warnings      []string              `json:"warnings"`
}

// ResolveState derives the semantic state from normalized inputs. Raw HH
// status mapping belongs to the adapter that prepares StatusEvidence.
func ResolveState(input ResolveStateInput) Resolution {
	c := input.Conversation
	r := Resolution{Status: StateApplied, Warnings: append([]string{}, input.Warnings...)}
	for _, message := range c.Messages {
		if message.ContentUnavailable && !message.HHSystemEvent {
			r.Warnings = append(r.Warnings, "message_content_unavailable")
		}
		if message.Sender == conversation.SenderUnknown {
			r.Warnings = append(r.Warnings, "unknown_message_sender")
		}
	}
	messages := conversation.DeliveredMessages(c.Messages)
	for i := range messages {
		message := messages[i]
		if message.Timestamp.IsZero() || (!input.Now.IsZero() && message.Timestamp.After(input.Now)) {
			r.Warnings = append(r.Warnings, "invalid_message_timestamp")
		}
		if (message.Sender == conversation.SenderCandidate && message.Direction != conversation.DirectionOutgoing) ||
			(message.Sender == conversation.SenderEmployer && message.Direction != conversation.DirectionIncoming) {
			r.Warnings = append(r.Warnings, "invalid_message_direction")
		}
		if i > 0 && message.Timestamp.Equal(messages[i-1].Timestamp) && message.Sender != messages[i-1].Sender {
			r.Warnings = append(r.Warnings, "ambiguous_last_message_order")
		}
		r.LatestMessage = &message
	}

	states := make([]State, 0, len(input.RawStatusEvidence)+2)
	for _, evidence := range input.RawStatusEvidence {
		if !evidence.Known {
			if evidence.Required {
				r.Warnings = append(r.Warnings, "unknown_hh_status")
			}
			continue
		}
		states = append(states, evidence.Status)
	}
	if input.ApplicationState != "" {
		states = append(states, input.ApplicationState)
	}
	switch c.Status {
	case conversation.StatusRejected:
		states = append(states, conversation.StatusRejected)
	case conversation.StatusOffer:
		states = append(states, conversation.StatusOffer)
	case conversation.StatusClosed:
		states = append(states, conversation.StatusClosed)
	case conversation.StatusInterview:
		states = append(states, conversation.StatusInterview)
	}

	closed := State("")
	for _, state := range states {
		if terminal := terminalForState(state); terminal != "" {
			if closed != "" && closed != terminal {
				r.Warnings = append(r.Warnings, "conflicting_terminal_status")
			}
			closed = terminal
		}
	}
	if messageTerminal, ok := TerminalConversationState(c); ok {
		if closed != "" && closed != messageTerminal {
			r.Warnings = append(r.Warnings, "conflicting_terminal_status")
		}
		closed = messageTerminal
	}
	if closed != "" {
		r.Status = closed
		messageTerminal := false
		if latest := LatestMeaningfulMessage(c); latest != nil && latest.Sender == conversation.SenderEmployer {
			messageTerminal = TerminalEmployerMessage(latest.Text)
		}
		for _, evidence := range input.RawStatusEvidence {
			if evidence.Known && terminalForState(evidence.Status) == "" && !messageTerminal {
				r.Warnings = append(r.Warnings, "raw_status_conflicts_with_terminal")
			}
		}
	} else if r.LatestMessage != nil {
		if r.LatestMessage.Sender == conversation.SenderEmployer {
			if requirement := EvaluateReplyPolicy(ReplyPolicyInput{Conversation: c, Latest: r.LatestMessage, Intent: candidatecontext.ClassifyEmployerMessage(r.LatestMessage.Text)}).Requirement; requirement == ReplyRequired {
				r.Status = StateCandidateActionRequired
			} else {
				r.Status = StateEmployerReplied
			}
		} else {
			r.Status = StateWaitingEmployer
			at := r.LatestMessage.Timestamp
			r.WaitingSince = &at
		}
	} else if input.AppliedAt != nil {
		r.Status = StateWaitingEmployer
		at := *input.AppliedAt
		r.WaitingSince = &at
	}
	if input.Pending || len(c.Summary.PendingQuestions) > 0 {
		r.Warnings = append(r.Warnings, "unresolved_clarification")
	}
	for _, action := range []string{input.ApplicationNextAction, c.NextAction} {
		switch action {
		case "", "waiting_employer_reply", "waiting_candidate_reply", "prepare_interview", "follow_up_possible", "manual_review":
		default:
			r.Warnings = append(r.Warnings, "unknown_next_action")
		}
	}
	if c.NextAction == "manual_review" || c.Status == StateManualReview {
		r.Warnings = append(r.Warnings, "manual_review")
	}
	for key, value := range c.HHMetadata {
		if (strings.HasPrefix(key, "warning") || key == "history_incomplete") && value != "" && value != "false" {
			r.Warnings = append(r.Warnings, "sync_consistency_warning")
		}
	}
	if input.AppliedAt != nil && (input.AppliedAt.IsZero() || (!input.Now.IsZero() && input.AppliedAt.After(input.Now))) {
		r.Warnings = append(r.Warnings, "invalid_application_timestamp")
	}
	r.Warnings = uniqueStrings(r.Warnings)
	if len(r.Warnings) > 0 {
		r.Status = StateManualReview
		r.WaitingSince = nil
	}
	return r
}

func ResolveConversationState(input ResolveStateInput) Resolution { return ResolveState(input) }

func terminalForState(state State) State {
	switch state {
	case conversation.StatusRejected:
		return conversation.StatusRejected
	case conversation.StatusOffer:
		return conversation.StatusOffer
	case conversation.StatusClosed:
		return conversation.StatusClosed
	case conversation.StatusInterview:
		return conversation.StatusInterview
	default:
		return ""
	}
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// LatestMeaningfulMessage is the single policy-level latest-message helper.
func LatestMeaningfulMessage(c conversation.EmployerConversation) *conversation.Message {
	messages := conversation.DeliveredMessages(c.Messages)
	if len(messages) == 0 {
		return nil
	}
	message := messages[len(messages)-1]
	return &message
}
