package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hh-ai-responder/internal/usecase/employerreply"
	employerreplyworkflow "hh-ai-responder/internal/usecase/employerreplyworkflow"
	followupdraft "hh-ai-responder/internal/usecase/followupdraft"
	followuporchestration "hh-ai-responder/internal/usecase/followuporchestration"
)

type rootFollowUpSnapshotLoader struct {
	orchestrator *AIReplyOrchestrator
}

func (l rootFollowUpSnapshotLoader) Load(ctx context.Context, applicationID string, now time.Time) (followuporchestration.LoadedSnapshot, error) {
	return l.load(ctx, applicationID, now, false)
}

func (l rootFollowUpSnapshotLoader) Reload(ctx context.Context, applicationID string, now time.Time) (followuporchestration.LoadedSnapshot, error) {
	return l.load(ctx, applicationID, now, true)
}

func (l rootFollowUpSnapshotLoader) load(ctx context.Context, applicationID string, now time.Time, strict bool) (followuporchestration.LoadedSnapshot, error) {
	if err := contextErr(ctx); err != nil {
		return followuporchestration.LoadedSnapshot{}, err
	}
	o := l.orchestrator
	if o == nil || o.applications == nil || o.conversationBuilder == nil {
		return followuporchestration.LoadedSnapshot{}, errors.New("follow-up context is not configured")
	}
	a, err := o.applications.GetApplication(applicationID)
	if err != nil {
		return followuporchestration.LoadedSnapshot{}, err
	}
	applications, appsErr := o.applications.ListApplications()
	events, eventsErr := o.applications.ListEvents()
	conversations, conversationsErr := o.conversationBuilder.store.ListConversations()
	d := CareerSnapshot{Applications: applications, Events: events, Conversations: conversations, Consistency: map[string][]string{}}
	if o.clarifications != nil {
		d.Clarifications, err = o.clarifications.List()
		if err != nil && strict {
			return followuporchestration.LoadedSnapshot{}, err
		}
	}
	if strict && (appsErr != nil || eventsErr != nil || conversationsErr != nil) {
		if appsErr != nil {
			return followuporchestration.LoadedSnapshot{}, appsErr
		}
		if eventsErr != nil {
			return followuporchestration.LoadedSnapshot{}, eventsErr
		}
		return followuporchestration.LoadedSnapshot{}, conversationsErr
	}
	in := d.input(a)
	loaded := followuporchestration.LoadedSnapshot{
		ApplicationID: a.ID, ConversationID: a.ConversationID,
		Fingerprint: followUpFingerprint(in),
		EligibilityInput: followuporchestration.EligibilityInput{
			Application: a, AppliedAt: in.AppliedAt, PreviousFollowUps: append([]time.Time{}, in.PreviousFollowUps...),
			PendingClarification: in.PendingClarification, Warnings: append([]string{}, in.Warnings...), Dismissed: in.Dismissed,
		},
	}
	conversationContext, contextErrValue := o.conversationBuilder.BuildForReply(a.ConversationID)
	if contextErrValue != nil {
		if strict {
			return followuporchestration.LoadedSnapshot{}, contextErrValue
		}
		loaded.EligibilityInput.Warnings = append(loaded.EligibilityInput.Warnings, "context_unavailable")
		return loaded, nil
	}
	if len(conversationContext.ConsistencyWarnings) > 0 {
		loaded.EligibilityInput.Warnings = append(loaded.EligibilityInput.Warnings, "consistency_warning")
	}
	loaded.EligibilityInput.Conversation = conversationContext.Conversation
	loaded.Generation = followupdraft.Input{Context: employerReplyInput(conversationContext, "").Context, ConfirmedFollowUps: append([]time.Time{}, in.PreviousFollowUps...)}
	loaded.EmployerMessage, loaded.EmployerMessageID = latestFollowUpEmployerMessage(conversationContext)
	loaded.VacancyID = fmt.Sprint(a.VacancyID)
	return loaded, nil
}

func latestFollowUpEmployerMessage(value ConversationContext) (string, string) {
	message := latestEmployerMessage(value.RecentMessages)
	if message == nil {
		return "", ""
	}
	return message.Text, message.ID
}

type rootFollowUpEvaluator struct{ policy FollowUpPolicy }

func (e rootFollowUpEvaluator) Evaluate(loaded followuporchestration.LoadedSnapshot, now time.Time) followupdraft.Eligibility {
	in := FollowUpInput{
		Application:          loaded.EligibilityInput.Application,
		Conversation:         loaded.EligibilityInput.Conversation,
		AppliedAt:            loaded.EligibilityInput.AppliedAt,
		PreviousFollowUps:    append([]time.Time{}, loaded.EligibilityInput.PreviousFollowUps...),
		PendingClarification: loaded.EligibilityInput.PendingClarification,
		Warnings:             append([]string{}, loaded.EligibilityInput.Warnings...),
		Dismissed:            loaded.EligibilityInput.Dismissed,
	}
	value := (FollowUpEngine{e.policy}).Evaluate(in, now)
	return followupdraft.Eligibility{
		ApplicationID: value.ApplicationID, ConversationID: value.ConversationID,
		CompanyName: value.CompanyName, VacancyTitle: value.VacancyTitle,
		Reason: value.Reason, EligibleAt: value.EligibleAt, WaitingSince: value.WaitingSince,
		DaysWaiting: value.DaysWaiting, PreviousFollowUps: value.PreviousFollowUps,
		Status: string(value.Status), Warnings: append([]string{}, value.Warnings...),
	}
}

type rootFollowUpDraftStore struct {
	store *AIDraftStore
	model string
}

func (s rootFollowUpDraftStore) FindReusable(ctx context.Context, applicationID, fingerprint string, value employerreply.Context) (followuporchestration.Draft, bool, error) {
	if err := contextErr(ctx); err != nil {
		return followuporchestration.Draft{}, false, err
	}
	values, err := s.store.List()
	if err != nil {
		return followuporchestration.Draft{}, false, err
	}
	for _, draft := range values {
		if draft.Type != AIDraftFollowUp || draft.Status != AIDraftGenerated || draft.ApplicationID != applicationID || draft.InputFingerprint != fingerprint {
			continue
		}
		if followupdraft.ValidateSavedDraft(draft.Text, draft.UsedFacts, value) != nil {
			return followuporchestration.Draft{Invalid: true}, true, nil
		}
		return followuporchestration.Draft{ID: draft.ID, ApplicationID: draft.ApplicationID, ConversationID: draft.ConversationID, InputFingerprint: draft.InputFingerprint, Text: draft.Text, DecisionReason: draft.DecisionReason, UsedFacts: append([]string{}, draft.UsedFacts...)}, true, nil
	}
	return followuporchestration.Draft{}, false, nil
}

func (s rootFollowUpDraftStore) Save(ctx context.Context, draft followuporchestration.Draft) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	_, err := s.store.Create(AIDraft{ID: draft.ID, Type: AIDraftFollowUp, ApplicationID: draft.ApplicationID, ConversationID: draft.ConversationID, InputFingerprint: draft.InputFingerprint, Text: draft.Text, DecisionReason: draft.DecisionReason, UsedFacts: append([]string{}, draft.UsedFacts...), Model: s.model})
	return err
}

type rootFollowUpPreparer struct{ orchestrator *AIReplyOrchestrator }

func (p rootFollowUpPreparer) Prepare(ctx context.Context, input followupdraft.Input) (followupdraft.Result, error) {
	if p.orchestrator == nil {
		return followupdraft.Result{}, errors.New("follow-up orchestrator is nil")
	}
	return p.orchestrator.currentFollowUpService().Prepare(ctx, input)
}

type rootFollowUpClarificationWriter struct {
	writer rootEmployerClarificationWriter
}

func (w rootFollowUpClarificationWriter) Persist(ctx context.Context, input followuporchestration.ClarificationInput) error {
	return w.writer.Persist(ctx, employerreplyworkflowClarificationInput(input))
}

// Keep the two workflow packages independent while sharing the root storage
// adapter's exact clarification deduplication behavior.
func employerreplyworkflowClarificationInput(input followuporchestration.ClarificationInput) employerreplyworkflow.ClarificationInput {
	return employerreplyworkflow.ClarificationInput{ConversationID: input.ConversationID, ApplicationID: input.ApplicationID, VacancyID: input.VacancyID, EmployerMessage: input.EmployerMessage, EmployerMessageID: input.EmployerMessageID, Reason: input.Reason, Missing: append([]employerreply.MissingInformation{}, input.Missing...)}
}
