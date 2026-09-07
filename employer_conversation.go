package main

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type ConversationStatus string

const (
	ConversationApplied                 ConversationStatus = "applied"
	ConversationEmployerReplied         ConversationStatus = "employer_replied"
	ConversationCandidateActionRequired ConversationStatus = "candidate_action_required"
	ConversationWaitingEmployer         ConversationStatus = "waiting_employer"
	ConversationInterview               ConversationStatus = "interview"
	ConversationOffer                   ConversationStatus = "offer"
	ConversationRejected                ConversationStatus = "rejected"
	ConversationClosed                  ConversationStatus = "closed"
)

type ConversationSender string
type ConversationSource string
type ConversationDirection string
type ConversationFollowUpState string

const (
	ConversationSenderUnknown     ConversationSender        = "unknown"
	ConversationDirectionUnknown  ConversationDirection     = "unknown"
	ConversationSenderCandidate   ConversationSender        = "candidate"
	ConversationSenderEmployer    ConversationSender        = "employer"
	ConversationSenderSystem      ConversationSender        = "system"
	ConversationSourceHH          ConversationSource        = "hh"
	ConversationSourceHHWrite     ConversationSource        = "hh_write"
	ConversationSourceManual      ConversationSource        = "manual"
	ConversationSourceAIDraft     ConversationSource        = "ai_draft"
	ConversationIncoming          ConversationDirection     = "incoming"
	ConversationOutgoing          ConversationDirection     = "outgoing"
	ConversationFollowUpNone      ConversationFollowUpState = "none"
	ConversationFollowUpEligible  ConversationFollowUpState = "eligible"
	ConversationFollowUpDrafted   ConversationFollowUpState = "drafted"
	ConversationFollowUpSent      ConversationFollowUpState = "sent"
	ConversationFollowUpDismissed ConversationFollowUpState = "dismissed"
)

// Text is the original message, including whitespace. A draft is never evidence
// of delivery, candidate activity, an answer, or an already-sent claim.
type ConversationMessage struct {
	HHSystemEvent      bool                  `json:"hh_system_event,omitempty"`
	ContentUnavailable bool                  `json:"content_unavailable,omitempty"`
	Metadata           map[string]string     `json:"metadata,omitempty"`
	ID                 string                `json:"id"`
	ExternalID         string                `json:"external_id,omitempty"`
	Timestamp          time.Time             `json:"timestamp"`
	Sender             ConversationSender    `json:"sender"`
	Text               string                `json:"text"`
	Source             ConversationSource    `json:"source"`
	Direction          ConversationDirection `json:"direction"`
}

// Claims document what was said, not what is true. Text must be a verbatim
// excerpt of the linked candidate message. Optional structured experience is
// an annotation for consistency checks, never a new KB assertion.
type CandidateConversationClaim struct {
	Text           string                       `json:"text"`
	RelatedSkill   string                       `json:"related_skill,omitempty"`
	RelatedProject string                       `json:"related_project,omitempty"`
	MessageID      string                       `json:"message_id"`
	CreatedAt      time.Time                    `json:"created_at"`
	Experience     *ConversationExperienceClaim `json:"experience,omitempty"`
}

type ConversationExperienceClaim struct {
	Months int `json:"months"`
	// "total_professional" is comparable to the legacy structured total.
	// Other scopes are retained but require manual verification.
	Scope string `json:"scope"`
}

// Summary is supplied explicitly by a future caller/summarizer. Even its
// ImportantFacts and CandidateClaims remain untrusted conversation annotations.
type ConversationSummary struct {
	TopicsDiscussed      []string                     `json:"topics_discussed"`
	EmployerQuestions    []string                     `json:"employer_questions"`
	CandidateAnswers     []string                     `json:"candidate_answers"`
	CandidateClaims      []CandidateConversationClaim `json:"candidate_claims"`
	EmployerRequirements []string                     `json:"employer_requirements"`
	PendingQuestions     []string                     `json:"pending_questions"`
	Commitments          []string                     `json:"commitments"`
	ImportantFacts       []string                     `json:"important_facts"`
}

type EmployerConversation struct {
	ID        string `json:"id"`
	VacancyID int    `json:"vacancy_id"`
	// ApplicationID is the optional canonical SQL-side ownership link. The
	// legacy JSON store may leave it empty; application-side compatibility
	// helpers continue to maintain JobApplication.ConversationID as well.
	ApplicationID          string                    `json:"application_id,omitempty"`
	HHConversationID       string                    `json:"hh_conversation_id,omitempty"`
	CompanyName            string                    `json:"company_name"`
	VacancyTitle           string                    `json:"vacancy_title"`
	VacancyDescription     string                    `json:"vacancy_description,omitempty"`
	Status                 ConversationStatus        `json:"status"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
	HHUpdatedAt            time.Time                 `json:"hh_updated_at,omitempty"`
	LastEmployerMessageAt  *time.Time                `json:"last_employer_message_at"`
	LastCandidateMessageAt *time.Time                `json:"last_candidate_message_at"`
	Messages               []ConversationMessage     `json:"messages"`
	Summary                ConversationSummary       `json:"summary"`
	NextAction             string                    `json:"next_action"`
	WaitingSince           *time.Time                `json:"waiting_since"`
	LastActivityAt         *time.Time                `json:"last_activity_at"`
	FollowUpState          ConversationFollowUpState `json:"follow_up_state"`
	RawStatus              string                    `json:"raw_status,omitempty"`
	HHMetadata             map[string]string         `json:"hh_metadata,omitempty"`
}

// State updates are explicit, complete values. No transition triggers sending,
// scheduling, or infers an offer/interview from untrusted message text.
type ConversationState struct {
	Status        ConversationStatus        `json:"status"`
	NextAction    string                    `json:"next_action"`
	WaitingSince  *time.Time                `json:"waiting_since"`
	FollowUpState ConversationFollowUpState `json:"follow_up_state"`
}

var conversationStateName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (m ConversationMessage) validate() error {
	if m.HHSystemEvent && (m.Source != ConversationSourceHH || strings.TrimSpace(m.Text) != "") {
		return errors.New("HH system event annotation requires a textless HH record")
	}
	if strings.TrimSpace(m.ID) == "" || m.Timestamp.IsZero() || (strings.TrimSpace(m.Text) == "" && m.Sender != ConversationSenderSystem && m.Sender != ConversationSenderUnknown && !(m.Source == ConversationSourceHH && m.ContentUnavailable)) {
		return errors.New("message requires id, timestamp and text")
	}
	if m.Direction != ConversationIncoming && m.Direction != ConversationOutgoing && !(m.Sender == ConversationSenderUnknown && m.Direction == ConversationDirectionUnknown) {
		return errors.New("invalid message direction")
	}
	switch m.Sender {
	case ConversationSenderUnknown:
		if m.Direction != ConversationDirectionUnknown || m.Source != ConversationSourceHH {
			return errors.New("unknown HH sender requires unknown direction")
		}
	case ConversationSenderEmployer:
		if m.Direction != ConversationIncoming {
			return errors.New("employer message must be incoming")
		}
	case ConversationSenderCandidate:
		if m.Direction != ConversationOutgoing {
			return errors.New("candidate message must be outgoing")
		}
	case ConversationSenderSystem:
	default:
		return errors.New("invalid message sender")
	}
	switch m.Source {
	case ConversationSourceHH, ConversationSourceHHWrite, ConversationSourceManual:
	case ConversationSourceAIDraft:
		if m.Sender != ConversationSenderCandidate || m.ExternalID != "" {
			return errors.New("AI draft must be a candidate message without an external delivery id")
		}
	default:
		return errors.New("invalid message source")
	}
	return nil
}

func (c EmployerConversation) validate() error {
	if strings.TrimSpace(c.ID) == "" || c.VacancyID < 0 || c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return errors.New("invalid conversation identity or timestamps")
	}
	// Extensible names, deliberately no fixed state-transition graph.
	if !conversationStateName.MatchString(string(c.Status)) || !conversationStateName.MatchString(string(c.FollowUpState)) {
		return errors.New("invalid conversation status or follow-up state")
	}
	if c.WaitingSince != nil && c.WaitingSince.IsZero() {
		return errors.New("invalid waiting_since")
	}
	ids, external := map[string]ConversationMessage{}, map[string]bool{}
	for _, message := range c.Messages {
		if err := message.validate(); err != nil {
			return err
		}
		if _, exists := ids[message.ID]; exists {
			return errors.New("duplicate message id")
		}
		ids[message.ID] = message
		if message.ExternalID != "" {
			key := string(message.Source) + "\x00" + message.ExternalID
			if external[key] {
				return errors.New("duplicate external message id")
			}
			external[key] = true
		}
	}
	seenClaims := map[string]bool{}
	for _, claim := range c.Summary.CandidateClaims {
		message, exists := ids[claim.MessageID]
		if !exists || message.Sender != ConversationSenderCandidate || message.Source == ConversationSourceAIDraft ||
			strings.TrimSpace(claim.Text) == "" || !strings.Contains(message.Text, claim.Text) || claim.CreatedAt.IsZero() {
			return errors.New("claim requires a verbatim excerpt of a recorded candidate message")
		}
		key := claim.MessageID + "\x00" + claim.Text
		if seenClaims[key] {
			return errors.New("duplicate candidate claim")
		}
		seenClaims[key] = true
		if claim.Experience != nil && (claim.Experience.Months < 0 || !conversationStateName.MatchString(claim.Experience.Scope)) {
			return errors.New("invalid claim experience annotation")
		}
	}
	derived := c
	derived.refreshActivity()
	if !sameConversationTime(c.LastEmployerMessageAt, derived.LastEmployerMessageAt) ||
		!sameConversationTime(c.LastCandidateMessageAt, derived.LastCandidateMessageAt) ||
		!sameConversationTime(c.LastActivityAt, derived.LastActivityAt) ||
		(c.LastActivityAt != nil && c.UpdatedAt.Before(*c.LastActivityAt)) {
		return errors.New("conversation activity timestamps do not match history")
	}
	return nil
}

func sameConversationTime(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

func (c *EmployerConversation) refreshActivity() {
	c.LastEmployerMessageAt, c.LastCandidateMessageAt, c.LastActivityAt = nil, nil, nil
	for _, message := range c.Messages {
		if message.HHSystemEvent || message.Source == ConversationSourceAIDraft || message.Sender == ConversationSenderSystem || message.Sender == ConversationSenderUnknown {
			continue
		}
		setConversationLatest(&c.LastActivityAt, message.Timestamp)
		if message.Sender == ConversationSenderEmployer {
			setConversationLatest(&c.LastEmployerMessageAt, message.Timestamp)
		}
		if message.Sender == ConversationSenderCandidate {
			setConversationLatest(&c.LastCandidateMessageAt, message.Timestamp)
		}
	}
}

func setConversationLatest(target **time.Time, value time.Time) {
	if *target == nil || value.After(**target) {
		copy := value
		*target = &copy
	}
}
