package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	llmport "hh-ai-responder/internal/ports/llm"
	followupdraft "hh-ai-responder/internal/usecase/followupdraft"
	followuporchestration "hh-ai-responder/internal/usecase/followuporchestration"
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
	if s != nil && s.Applications != nil && s.Applications.careerRepository != nil {
		application, err := s.Applications.GetApplication(id)
		if err != nil {
			return err
		}
		application.FollowUpState = ConversationFollowUpDismissed
		if err := s.Applications.UpdateApplication(application); err != nil {
			return err
		}
		return s.dismissFollowUpNotification(id)
	}
	// Persist first; failed disk writes leave the live snapshot unchanged.
	copyStore, err := s.Applications.cloneForSync()
	if err != nil {
		return err
	}
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
	s.Applications.repository = copyStore.repository
	if err := s.Applications.refreshCompatibilityMirror(); err != nil {
		return err
	}
	return s.dismissFollowUpNotification(id)
}

func (s *DashboardServer) dismissFollowUpNotification(id string) error {
	if s.Notifications == nil {
		return nil
	}
	now := time.Now().UTC()
	for i := range s.Notifications.notifications {
		if s.Notifications.notifications[i].Type == NotificationFollowUpAvailable && s.Notifications.notifications[i].RelatedApplicationID == id && s.Notifications.notifications[i].Lifecycle != NotificationDismissed && s.Notifications.notifications[i].Lifecycle != NotificationResolved {
			s.Notifications.notifications[i].AcknowledgedAt = &now
			s.Notifications.notifications[i].Lifecycle = NotificationDismissed
		}
	}
	return s.Notifications.Save()
}

// PrepareFollowUp obtains fresh local evidence itself; a caller cannot pass a
// forged eligible result. Its only side effects are draft/clarification storage.
func (o *AIReplyOrchestrator) PrepareFollowUp(applicationID string, policy FollowUpPolicy, now time.Time) (AIResponseDecision, error) {
	if o == nil || o.applications == nil || o.conversationBuilder == nil || o.drafts == nil {
		return AIResponseDecision{}, errors.New("follow-up context is not configured")
	}
	workflow := followuporchestration.NewService(followuporchestration.Dependencies{
		Snapshots: rootFollowUpSnapshotLoader{orchestrator: o}, Eligibility: rootFollowUpEvaluator{policy: policy},
		FollowUp: rootFollowUpPreparer{orchestrator: o}, Drafts: rootFollowUpDraftStore{store: o.drafts, model: o.model},
		Clarifications: rootFollowUpClarificationWriter{writer: rootEmployerClarificationWriter{store: o.clarifications, acquisition: o.acquisition}},
	})
	result, err := workflow.Prepare(o.aiContext(), applicationID, now)
	return result.Decision, err
}

// currentFollowUpService keeps compatibility callers that replace the root
// StructuredAIClient after construction on the same typed provider boundary.
func (o *AIReplyOrchestrator) currentFollowUpService() *followupdraft.Service {
	completion := llmport.CompletionProvider(nil)
	if provider, ok := any(o.ai).(llmport.CompletionProvider); ok {
		completion = provider
	} else if o.ai != nil {
		completion = legacyCompletionProvider{client: o.ai}
	}
	attempts := 1
	if client, ok := any(o.ai).(*AIClient); ok && client != nil && client.attempts > 0 {
		attempts = client.attempts
	}
	return followupdraft.NewService(followupdraft.Dependencies{Completion: completion}, followupdraft.Options{Model: o.model, Attempts: attempts, ExtraPrompt: o.extraPrompt, SemanticRetryDelay: aiRetryDelay})
}

func followUpFingerprint(in FollowUpInput) string {
	in.Application.UpdatedAt = time.Time{}
	in.Conversation.UpdatedAt = time.Time{}
	raw, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
