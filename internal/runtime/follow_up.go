package runtime

import (
	"errors"
	"flag"
	"strconv"
	"time"
)

type FollowUpPolicy struct {
	AfterApplicationWithoutReply      time.Duration `json:"after_application_without_reply"`
	AfterCandidateMessageWithoutReply time.Duration `json:"after_candidate_message_without_reply"`
	MaxFollowUps                      int           `json:"max_followups"`
	MinimumInterval                   time.Duration `json:"minimum_interval"`
}

func (p FollowUpPolicy) Validate() error {
	if p.AfterApplicationWithoutReply <= 0 || p.AfterCandidateMessageWithoutReply <= 0 || p.MinimumInterval <= 0 || p.MaxFollowUps < 0 {
		return errors.New("follow-up delays must be positive and max-followups non-negative")
	}
	return nil
}
func followUpPolicyFlags(fs *flag.FlagSet, p *FollowUpPolicy) {
	fs.DurationVar(&p.AfterApplicationWithoutReply, "follow-up-after-application", p.AfterApplicationWithoutReply, "Follow-up delay after application (e.g. 120h)")
	fs.DurationVar(&p.AfterCandidateMessageWithoutReply, "follow-up-after-message", p.AfterCandidateMessageWithoutReply, "Follow-up delay after candidate message")
	fs.DurationVar(&p.MinimumInterval, "follow-up-minimum-interval", p.MinimumInterval, "Minimum interval between confirmed follow-ups")
	fs.IntVar(&p.MaxFollowUps, "follow-up-max", p.MaxFollowUps, "Maximum confirmed follow-ups; 0 disables suggestions")
}
func followUpPolicyEnv(p FollowUpPolicy, getenv func(string) string, flags map[string]bool) (FollowUpPolicy, error) {
	for _, o := range []struct {
		flag, env string
		target    *time.Duration
	}{{"follow-up-after-application", "HH_FOLLOW_UP_AFTER_APPLICATION", &p.AfterApplicationWithoutReply}, {"follow-up-after-message", "HH_FOLLOW_UP_AFTER_MESSAGE", &p.AfterCandidateMessageWithoutReply}, {"follow-up-minimum-interval", "HH_FOLLOW_UP_MINIMUM_INTERVAL", &p.MinimumInterval}} {
		if v := getenv(o.env); v != "" && !flags[o.flag] {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return p, errors.New("invalid " + o.env)
			}
			*o.target = parsed
		}
	}
	if v := getenv("HH_FOLLOW_UP_MAX"); v != "" && !flags["follow-up-max"] {
		n, err := strconv.Atoi(v)
		if err != nil {
			return p, errors.New("invalid HH_FOLLOW_UP_MAX")
		}
		p.MaxFollowUps = n
	}
	return p, p.Validate()
}

type FollowUpStatus string

const (
	FollowUpNotEligible             FollowUpStatus = "not_eligible"
	FollowUpEligible                FollowUpStatus = "eligible"
	FollowUpTooEarly                FollowUpStatus = "too_early"
	FollowUpCandidateActionRequired FollowUpStatus = "candidate_action_required"
	FollowUpConversationClosed      FollowUpStatus = "conversation_closed"
	FollowUpManualReview            FollowUpStatus = "manual_review"
)

type FollowUpCandidate struct {
	ApplicationID     string         `json:"application_id"`
	ConversationID    string         `json:"conversation_id"`
	CompanyName       string         `json:"company_name"`
	VacancyTitle      string         `json:"vacancy_title"`
	Reason            string         `json:"reason"`
	EligibleAt        *time.Time     `json:"eligible_at"`
	WaitingSince      *time.Time     `json:"waiting_since"`
	DaysWaiting       float64        `json:"days_waiting"`
	PreviousFollowUps int            `json:"previous_followups"`
	Status            FollowUpStatus `json:"status"`
	Warnings          []string       `json:"warnings"`
}
type FollowUpInput struct {
	Application          JobApplication
	Conversation         EmployerConversation
	AppliedAt            *time.Time
	PreviousFollowUps    []time.Time // Explicit delivered events only, never drafts or inferred messages.
	PendingClarification bool
	Warnings             []string
	Dismissed            bool
}

// FollowUpEngine has no stores, client, callback, timer or HH capability.
type FollowUpEngine struct{ Policy FollowUpPolicy }

func (e FollowUpEngine) Evaluate(in FollowUpInput, now time.Time) FollowUpCandidate {
	a, c := in.Application, in.Conversation
	r := FollowUpCandidate{ApplicationID: a.ID, ConversationID: c.ID, CompanyName: a.CompanyName, VacancyTitle: a.VacancyTitle, Warnings: []string{}, PreviousFollowUps: len(in.PreviousFollowUps)}
	finish := func(s FollowUpStatus, reason string) FollowUpCandidate { r.Status = s; r.Reason = reason; return r }
	if err := e.Policy.Validate(); err != nil {
		r.Warnings = append(r.Warnings, "invalid_policy")
		return finish(FollowUpManualReview, "Invalid follow-up policy")
	}
	if in.Dismissed || c.FollowUpState == ConversationFollowUpDismissed || a.FollowUpState == ConversationFollowUpDismissed {
		return finish(FollowUpNotEligible, "Dismissed locally")
	}
	state := (ConversationStateResolver{}).Resolve(a, c, in.AppliedAt, in.PendingClarification, in.Warnings, now)
	r.Warnings = state.Warnings
	switch state.Status {
	case ConversationManualReview:
		return finish(FollowUpManualReview, "Resolve data warnings or clarification first")
	case ConversationRejected, ConversationOffer, ConversationClosed:
		return finish(FollowUpConversationClosed, "Conversation is terminal")
	case ConversationInterview:
		return finish(FollowUpNotEligible, "Interview or next activity requires candidate control")
	case ConversationCandidateActionRequired:
		return finish(FollowUpCandidateActionRequired, "Employer is waiting for your response")
	}
	if a.NextAction == string(NextActionPrepareInterview) || c.NextAction == string(NextActionPrepareInterview) {
		return finish(FollowUpNotEligible, "Next interview activity is already planned")
	}
	if len(c.Summary.Commitments) > 0 {
		return finish(FollowUpManualReview, "Review existing conversation commitments first")
	}
	if in.AppliedAt == nil {
		return finish(FollowUpManualReview, "Application delivery time is not confirmed")
	}
	if a.Status != ApplicationApplied && a.Status != ApplicationEmployerReplied {
		return finish(FollowUpNotEligible, "Application is not in a sent active state")
	}
	if a.NextAction == string(NextActionWaitingCandidateReply) || c.NextAction == string(NextActionWaitingCandidateReply) {
		return finish(FollowUpCandidateActionRequired, "Pending candidate action")
	}
	if state.Status != ConversationWaitingEmployer || state.WaitingSince == nil {
		return finish(FollowUpNotEligible, "Not waiting for employer")
	}
	r.WaitingSince = state.WaitingSince
	r.DaysWaiting = now.Sub(*r.WaitingSince).Hours() / 24
	delay := e.Policy.AfterApplicationWithoutReply
	// The application/cover letter itself does not start the shorter message clock.
	if state.LatestMessage != nil && state.LatestMessage.Timestamp.After(*in.AppliedAt) {
		delay = e.Policy.AfterCandidateMessageWithoutReply
	}
	eligible := r.WaitingSince.Add(delay)
	for _, sent := range in.PreviousFollowUps {
		if sent.IsZero() || sent.After(now) {
			return finish(FollowUpManualReview, "Invalid follow-up history timestamp")
		}
		if next := sent.Add(e.Policy.MinimumInterval); next.After(eligible) {
			eligible = next
		}
	}
	r.EligibleAt = &eligible
	if len(in.PreviousFollowUps) >= e.Policy.MaxFollowUps {
		return finish(FollowUpNotEligible, "Maximum follow-ups reached")
	}
	if now.Before(eligible) {
		return finish(FollowUpTooEarly, "Wait until the next eligible date")
	}
	return finish(FollowUpEligible, "Можно подготовить короткий follow-up: ответа работодателя пока нет")
}
