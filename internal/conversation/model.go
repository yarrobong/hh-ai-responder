package conversation

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrConversationNotFound is shared by persistence adapters and callers of
// the normalized conversation contract.
var ErrConversationNotFound = errors.New("conversation not found")

// Status is the persisted conversation status value. Derived workflow
// classification and eligibility decisions remain outside this package.
type Status string

const (
	StatusApplied                 Status = "applied"
	StatusEmployerReplied         Status = "employer_replied"
	StatusCandidateActionRequired Status = "candidate_action_required"
	StatusWaitingEmployer         Status = "waiting_employer"
	StatusInterview               Status = "interview"
	StatusOffer                   Status = "offer"
	StatusRejected                Status = "rejected"
	StatusClosed                  Status = "closed"
)

// FollowUpState is the persisted lifecycle marker shared by application and
// conversation snapshots. Follow-up eligibility and orchestration remain
// outside this package.
type FollowUpState string

const (
	FollowUpNone      FollowUpState = "none"
	FollowUpEligible  FollowUpState = "eligible"
	FollowUpDrafted   FollowUpState = "drafted"
	FollowUpSent      FollowUpState = "sent"
	FollowUpDismissed FollowUpState = "dismissed"
)

// State is a complete persisted conversation state update. Applying it to a
// store or repository is infrastructure behavior and remains outside here.
type State struct {
	Status        Status        `json:"status"`
	NextAction    string        `json:"next_action"`
	WaitingSince  *time.Time    `json:"waiting_since"`
	FollowUpState FollowUpState `json:"follow_up_state"`
}

// EmployerConversation is the persisted employer-conversation aggregate
// snapshot. Relations to applications and vacancies are identifiers only.
type EmployerConversation struct {
	ID        string `json:"id"`
	VacancyID int    `json:"vacancy_id"`
	// ApplicationID is the optional canonical SQL-side ownership link. The
	// legacy JSON store may leave it empty; application-side compatibility
	// helpers continue to maintain JobApplication.ConversationID as well.
	ApplicationID          string            `json:"application_id,omitempty"`
	HHConversationID       string            `json:"hh_conversation_id,omitempty"`
	CompanyName            string            `json:"company_name"`
	VacancyTitle           string            `json:"vacancy_title"`
	VacancyDescription     string            `json:"vacancy_description,omitempty"`
	Status                 Status            `json:"status"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
	HHUpdatedAt            time.Time         `json:"hh_updated_at,omitempty"`
	LastEmployerMessageAt  *time.Time        `json:"last_employer_message_at"`
	LastCandidateMessageAt *time.Time        `json:"last_candidate_message_at"`
	Messages               []Message         `json:"messages"`
	Summary                Summary           `json:"summary"`
	NextAction             string            `json:"next_action"`
	WaitingSince           *time.Time        `json:"waiting_since"`
	LastActivityAt         *time.Time        `json:"last_activity_at"`
	FollowUpState          FollowUpState     `json:"follow_up_state"`
	RawStatus              string            `json:"raw_status,omitempty"`
	HHMetadata             map[string]string `json:"hh_metadata,omitempty"`
}

var stateName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Validate checks aggregate identity, persisted values, message history,
// claims, and activity timestamps. It does not classify the conversation or
// consult applications, candidates, HH, AI, or storage.
func (c EmployerConversation) Validate() error {
	if strings.TrimSpace(c.ID) == "" || c.VacancyID < 0 || c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return errors.New("invalid conversation identity or timestamps")
	}
	if !stateName.MatchString(string(c.Status)) || !stateName.MatchString(string(c.FollowUpState)) {
		return errors.New("invalid conversation status or follow-up state")
	}
	if c.WaitingSince != nil && c.WaitingSince.IsZero() {
		return errors.New("invalid waiting_since")
	}
	ids, external := map[string]Message{}, map[string]bool{}
	for _, message := range c.Messages {
		if err := message.Validate(); err != nil {
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
		if !exists || message.Sender != SenderCandidate || message.Source == SourceAIDraft ||
			strings.TrimSpace(claim.Text) == "" || !strings.Contains(message.Text, claim.Text) || claim.CreatedAt.IsZero() {
			return errors.New("claim requires a verbatim excerpt of a recorded candidate message")
		}
		key := claim.MessageID + "\x00" + claim.Text
		if seenClaims[key] {
			return errors.New("duplicate candidate claim")
		}
		seenClaims[key] = true
		if claim.Experience != nil && (claim.Experience.Months < 0 || !stateName.MatchString(claim.Experience.Scope)) {
			return errors.New("invalid claim experience annotation")
		}
	}
	derived := c
	derived.RefreshActivity()
	if !sameTime(c.LastEmployerMessageAt, derived.LastEmployerMessageAt) ||
		!sameTime(c.LastCandidateMessageAt, derived.LastCandidateMessageAt) ||
		!sameTime(c.LastActivityAt, derived.LastActivityAt) ||
		(c.LastActivityAt != nil && c.UpdatedAt.Before(*c.LastActivityAt)) {
		return errors.New("conversation activity timestamps do not match history")
	}
	return nil
}

// RefreshActivity recomputes persisted activity projections from the message
// history without changing message order or any workflow state.
func (c *EmployerConversation) RefreshActivity() {
	c.LastEmployerMessageAt, c.LastCandidateMessageAt, c.LastActivityAt = nil, nil, nil
	for _, message := range c.Messages {
		if message.HHSystemEvent || message.Source == SourceAIDraft || message.Sender == SenderSystem || message.Sender == SenderUnknown {
			continue
		}
		setLatest(&c.LastActivityAt, message.Timestamp)
		if message.Sender == SenderEmployer {
			setLatest(&c.LastEmployerMessageAt, message.Timestamp)
		}
		if message.Sender == SenderCandidate {
			setLatest(&c.LastCandidateMessageAt, message.Timestamp)
		}
	}
}

// DeliveredMessages returns the human conversation timeline. System events,
// AI drafts, and messages without a trusted human participant are not part of
// conversation policy. The returned slice is sorted by timestamp and does not
// alias the input slice.
func DeliveredMessages(values []Message) []Message {
	result := make([]Message, 0, len(values))
	for _, message := range values {
		if message.HHSystemEvent || message.Source == SourceAIDraft ||
			(message.Sender != SenderCandidate && message.Sender != SenderEmployer) {
			continue
		}
		result = append(result, message)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
}

func sameTime(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

// SameTime compares optional persisted timestamps using the model's nil
// semantics. It is exposed for compatibility code that compares snapshots.
func SameTime(a, b *time.Time) bool {
	return sameTime(a, b)
}

func setLatest(target **time.Time, value time.Time) {
	if *target == nil || value.After(**target) {
		copy := value
		*target = &copy
	}
}
