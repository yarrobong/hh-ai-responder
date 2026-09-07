package main

import (
	"sort"
	"strings"
	"time"
)

const ConversationManualReview ConversationStatus = "manual_review"

type ConversationResolution struct {
	Status        ConversationStatus   `json:"status"`
	WaitingSince  *time.Time           `json:"waiting_since"`
	LatestMessage *ConversationMessage `json:"latest_message,omitempty"`
	Warnings      []string             `json:"warnings"`
}
type ConversationStateResolver struct{}

// Resolve is a pure projection. It does not repair stored status or invent dates.
func (ConversationStateResolver) Resolve(a JobApplication, c EmployerConversation, appliedAt *time.Time, pending bool, warnings []string, now time.Time) ConversationResolution {
	r := ConversationResolution{Status: ConversationApplied, Warnings: append([]string{}, warnings...)}
	for _, m := range c.Messages {
		if m.ContentUnavailable && !m.HHSystemEvent {
			r.Warnings = append(r.Warnings, "message_content_unavailable")
		}
		if m.Sender == ConversationSenderUnknown {
			r.Warnings = append(r.Warnings, "unknown_message_sender")
		}
	}
	messages := deliveredMessages(c.Messages)
	for i := range messages {
		m := messages[i]
		if m.Timestamp.IsZero() || m.Timestamp.After(now) {
			r.Warnings = append(r.Warnings, "invalid_message_timestamp")
		}
		if m.Sender == ConversationSenderCandidate && m.Direction != ConversationOutgoing || m.Sender == ConversationSenderEmployer && m.Direction != ConversationIncoming {
			r.Warnings = append(r.Warnings, "invalid_message_direction")
		}
		if i > 0 && m.Timestamp.Equal(messages[i-1].Timestamp) && m.Sender != messages[i-1].Sender {
			r.Warnings = append(r.Warnings, "ambiguous_last_message_order")
		}
		r.LatestMessage = &m
	}
	rawStates := []ApplicationStatus{}
	for _, source := range []struct {
		raw      string
		required bool
	}{{a.RawStatus, a.Source == ApplicationSourceHH}, {c.RawStatus, c.HHConversationID != ""}} {
		if source.raw == "" && !source.required {
			continue
		}
		status, known := MapHHApplicationStatus(source.raw)
		if !known {
			r.Warnings = append(r.Warnings, "unknown_hh_status")
		} else {
			rawStates = append(rawStates, status)
		}
	}
	terminal := func(s ApplicationStatus) ConversationStatus {
		switch s {
		case ApplicationRejected:
			return ConversationRejected
		case ApplicationOffer:
			return ConversationOffer
		case ApplicationArchived:
			return ConversationClosed
		case ApplicationInterview:
			return ConversationInterview
		}
		return ""
	}
	states := append(rawStates, a.Status)
	switch c.Status {
	case ConversationRejected:
		states = append(states, ApplicationRejected)
	case ConversationOffer:
		states = append(states, ApplicationOffer)
	case ConversationClosed:
		states = append(states, ApplicationArchived)
	case ConversationInterview:
		states = append(states, ApplicationInterview)
	}
	closed := ConversationStatus("")
	for _, state := range states {
		if t := terminal(state); t != "" {
			if closed != "" && closed != t {
				r.Warnings = append(r.Warnings, "conflicting_terminal_status")
			}
			closed = t
		}
	}
	if messageTerminal, ok := terminalConversationState(c); ok {
		if closed != "" && closed != messageTerminal {
			r.Warnings = append(r.Warnings, "conflicting_terminal_status")
		}
		closed = messageTerminal
	}
	if closed != "" {
		r.Status = closed
		messageTerminal := false
		if latest := latestDeliveredMessage(c); latest != nil && latest.Sender == ConversationSenderEmployer {
			messageTerminal = terminalEmployerMessage(latest.Text)
		}
		for _, state := range rawStates {
			if terminal(state) == "" && !messageTerminal {
				r.Warnings = append(r.Warnings, "raw_status_conflicts_with_terminal")
			}
		}
	} else if r.LatestMessage != nil {
		if r.LatestMessage.Sender == ConversationSenderEmployer {
			if requirement := conversationReplyRequirement(c, r.LatestMessage, classifyEmployerMessage(r.LatestMessage.Text)); requirement == ReplyRequired {
				r.Status = ConversationCandidateActionRequired
			} else {
				// Acknowledgements and informational employer messages do not
				// create candidate work. Terminal messages were handled above.
				r.Status = ConversationEmployerReplied
			}
		} else {
			r.Status = ConversationWaitingEmployer
			t := r.LatestMessage.Timestamp
			r.WaitingSince = &t
		}
	} else if appliedAt != nil {
		r.Status = ConversationWaitingEmployer
		t := *appliedAt
		r.WaitingSince = &t
	}
	if pending || len(c.Summary.PendingQuestions) > 0 {
		r.Warnings = append(r.Warnings, "unresolved_clarification")
	}
	for _, action := range []string{a.NextAction, c.NextAction} {
		switch action {
		case "", string(NextActionWaitingEmployerReply), string(NextActionWaitingCandidateReply), string(NextActionPrepareInterview), string(NextActionFollowUpPossible), "manual_review":
		default:
			r.Warnings = append(r.Warnings, "unknown_next_action")
		}
	}
	if a.NextAction == "manual_review" || c.NextAction == "manual_review" || c.Status == ConversationManualReview {
		r.Warnings = append(r.Warnings, "manual_review")
	}
	for k, v := range c.HHMetadata {
		if (strings.HasPrefix(k, "warning") || k == "history_incomplete") && v != "" && v != "false" {
			r.Warnings = append(r.Warnings, "sync_consistency_warning")
		}
	}
	if appliedAt != nil && (appliedAt.IsZero() || appliedAt.After(now)) {
		r.Warnings = append(r.Warnings, "invalid_application_timestamp")
	}
	r.Warnings = uniqueStrings(r.Warnings)
	if len(r.Warnings) > 0 {
		r.Status = ConversationManualReview
		r.WaitingSince = nil
	}
	return r
}

func deliveredMessages(values []ConversationMessage) []ConversationMessage {
	result := []ConversationMessage{}
	for _, m := range values {
		if !m.HHSystemEvent && m.Source != ConversationSourceAIDraft && (m.Sender == ConversationSenderCandidate || m.Sender == ConversationSenderEmployer) {
			result = append(result, m)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })
	return result
}
