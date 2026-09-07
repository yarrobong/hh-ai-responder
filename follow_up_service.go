package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *DashboardServer) careerSnapshot() CareerSnapshot {
	d := s.careerSnapshotLocal()
	return s.cachedConsistency(d, "")
}

// careerSnapshotLocal is the cheap aggregate projection used by Overview and
// Analytics. Expensive per-conversation consistency checks belong to deep
// diagnostics and conversation detail, not the first paint.
func (s *DashboardServer) careerSnapshotLocal() CareerSnapshot {
	vacancies, _ := s.Vacancies.List()
	applications, _ := s.Applications.ListApplications()
	events, _ := s.Applications.ListEvents()
	conversations, _ := s.Conversations.ListConversations()
	drafts, _ := s.Drafts.List()
	clarifications, _ := s.Clarifications.List()
	return CareerSnapshot{Vacancies: vacancies, Applications: applications, Events: events, Conversations: conversations, Drafts: drafts, Clarifications: clarifications, Sync: s.Sync.SyncState(), Consistency: map[string][]string{}}
}

// One detail view needs only this conversation's consistency analysis.
func (s *DashboardServer) careerSnapshotForConversation(c EmployerConversation) CareerSnapshot {
	d := s.careerSnapshotLocal()
	return s.cachedConsistency(d, c.ID)
}
func withCareerConsistency(d CareerSnapshot, resolver *CandidateContextResolver) CareerSnapshot {
	if d.Consistency == nil {
		d.Consistency = map[string][]string{}
	}
	builder := NewConversationContextBuilder(&ConversationStore{conversations: d.Conversations}, resolver)
	for _, c := range d.Conversations {
		ctx, err := builder.BuildForReply(c.ID)
		if err != nil {
			d.Consistency[c.ID] = []string{"context_unavailable"}
		} else if len(ctx.ConsistencyWarnings) > 0 {
			d.Consistency[c.ID] = []string{"consistency_warning"}
		}
	}
	return d
}
func (s *DashboardServer) followUpCandidate(id string, now time.Time) (FollowUpCandidate, error) {
	a, err := s.Applications.GetApplication(id)
	if err != nil {
		return FollowUpCandidate{}, err
	}
	return (FollowUpEngine{s.FollowUpPolicy}).Evaluate(s.careerSnapshot().input(a), now), nil
}
func (s *DashboardServer) dismissFollowUp(id string) error {
	// Persist first; failed disk writes leave the live snapshot unchanged.
	copyStore := *s.Applications
	copyStore.applications = append([]JobApplication{}, s.Applications.applications...)
	found := false
	for i := range copyStore.applications {
		if copyStore.applications[i].ID == id {
			copyStore.applications[i].FollowUpState = ConversationFollowUpDismissed
			found = true
		}
	}
	if !found {
		return ErrApplicationNotFound
	}
	if err := copyStore.Save(); err != nil {
		return err
	}
	s.Applications.applications = copyStore.applications
	if s.Notifications != nil {
		now := time.Now().UTC()
		for i := range s.Notifications.notifications {
			if s.Notifications.notifications[i].Type == NotificationFollowUpAvailable && s.Notifications.notifications[i].RelatedApplicationID == id && s.Notifications.notifications[i].Lifecycle != NotificationDismissed && s.Notifications.notifications[i].Lifecycle != NotificationResolved {
				s.Notifications.notifications[i].AcknowledgedAt = &now
				s.Notifications.notifications[i].Lifecycle = NotificationDismissed
			}
		}
		if err := s.Notifications.Save(); err != nil {
			return err
		}
	}
	return nil
}

// PrepareFollowUp obtains fresh local evidence itself; a caller cannot pass a
// forged eligible result. Its only side effects are draft/clarification storage.
func (o *AIReplyOrchestrator) PrepareFollowUp(applicationID string, policy FollowUpPolicy, now time.Time) (AIResponseDecision, error) {
	if o == nil || o.applications == nil || o.conversationBuilder == nil || o.drafts == nil {
		return AIResponseDecision{}, errors.New("follow-up context is not configured")
	}
	a, err := o.applications.GetApplication(applicationID)
	if err != nil {
		return AIResponseDecision{}, err
	}
	applications, _ := o.applications.ListApplications()
	events, _ := o.applications.ListEvents()
	conversations, _ := o.conversationBuilder.store.ListConversations()
	d := CareerSnapshot{Applications: applications, Events: events, Conversations: conversations, Consistency: map[string][]string{}}
	if o.clarifications != nil {
		d.Clarifications = o.clarifications.clarifications
	}
	in := d.input(a)
	context, err := o.conversationBuilder.BuildForReply(a.ConversationID)
	if err != nil {
		return o.manualReviewDecision("Follow-up context unavailable", []string{"context_unavailable"}, nil)
	}
	if len(context.ConsistencyWarnings) > 0 {
		in.Warnings = append(in.Warnings, "consistency_warning")
	}
	eligibility := (FollowUpEngine{policy}).Evaluate(in, now)
	if eligibility.Status != FollowUpEligible {
		return o.manualReviewDecision(eligibility.Reason, eligibility.Warnings, nil)
	}
	// Reuse a current draft to avoid repeated clicks manufacturing follow-up history.
	drafts, _ := o.drafts.List()
	for _, draft := range drafts {
		if draft.Type == AIDraftFollowUp && draft.ApplicationID == applicationID && draft.Status == AIDraftGenerated && draft.InputFingerprint == followUpFingerprint(in) {
			if validateAIUsedFacts(draft.UsedFacts, context.CandidateContext) != nil || o.ValidateAIDraft(draft.Text, context.CandidateContext, context.ConversationSummary.CandidateClaims) != nil {
				return o.manualReviewDecision("Saved draft no longer matches candidate knowledge", []string{"stale_ai_draft"}, nil)
			}
			return AIResponseDecision{Action: AIActionDraftReply, Draft: draft.Text, Reason: draft.DecisionReason, Confidence: 1, UsedFacts: draft.UsedFacts, ForbiddenClaimsChecked: true, MissingInformation: []AIMissingInformation{}, Warnings: []string{}, ConversationTopicsUsed: []string{}}, nil
		}
	}
	payload := struct {
		Context  json.RawMessage   `json:"conversation"`
		FollowUp FollowUpCandidate `json:"follow_up"`
		History  []time.Time       `json:"confirmed_follow_up_history"`
	}{json.RawMessage(marshalSafeContext(context)), eligibility, in.PreviousFollowUps}
	raw, err := json.Marshal(payload)
	if err != nil {
		return AIResponseDecision{}, err
	}
	decision, err := o.callDecision("Короткий естественный follow-up без давления: 1–3 предложения, учитывай диалог, не повторяй знакомство или сопроводительное письмо. Только черновик, без новых обещаний и договорённостей.", string(raw), context.CandidateContext, context.ConversationSummary.CandidateClaims, context.ReplyGuidance.AlreadyDiscussedTopics)
	if err != nil {
		return decision, err
	}
	if decision.Action == AIActionNeedCandidate {
		return decision, o.persistClarifications(a.ConversationID, a.ID, decision)
	}
	if decision.Action == AIActionDraftReply {
		if classifyHighRiskChatMessage(decision.Draft) != "" || len([]rune(decision.Draft)) > 600 || len(decision.Warnings) > 0 || validateAIUsedFacts(decision.UsedFacts, context.CandidateContext) != nil || o.ValidateAIDraft(decision.Draft, context.CandidateContext, context.ConversationSummary.CandidateClaims) != nil {
			return o.manualReviewDecision("Follow-up draft requires review", []string{"draft_validation_failed"}, nil)
		}
		// Fresh evaluation before committing the draft also guards future callers.
		fresh := d.input(a)
		if (FollowUpEngine{policy}).Evaluate(fresh, now).Status != FollowUpEligible {
			return o.manualReviewDecision("Follow-up state changed", nil, nil)
		}
		err = o.persistDraft(AIDraft{Type: AIDraftFollowUp, ApplicationID: a.ID, ConversationID: a.ConversationID, InputFingerprint: followUpFingerprint(in), Text: decision.Draft, DecisionReason: decision.Reason, UsedFacts: decision.UsedFacts})
	}
	return decision, err
}
func followUpFingerprint(in FollowUpInput) string {
	in.Application.UpdatedAt = time.Time{}
	in.Conversation.UpdatedAt = time.Time{}
	raw, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
