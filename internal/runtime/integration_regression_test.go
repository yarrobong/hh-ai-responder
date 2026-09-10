package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestEmployerReplyOrchestratorUsesUnifiedStructuredProfileKnowledge(t *testing.T) {
	tests := []struct {
		name  string
		query string
		fact  string
	}{
		{"salary", "Какие у вас зарплатные ожидания?", "Минимум 40-50 тыс., цель 50-100 тыс."},
		{"relocation", "Готовы к релокации?", "Не готов"},
		{"experience", "Сколько месяцев общего опыта?", "11 месяцев"},
		{"english", "Какой у вас уровень English?", "B2"},
		{"education", "Какое у вас образование?", "СПО"},
		{"work mode", "Какой предпочитаете формат работы?", "Удалёнка"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations, conversation, kb, builder := conversationTestBuilder(t)
			kb.Profile = stage15Profile()
			message := conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, test.query, 1))
			drafts := NewAIDraftStore(filepath.Join(t.TempDir(), "drafts.json"))
			ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Готов обсудить этот вопрос.", []string{test.fact}, nil)}
			decision, err := NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{ConversationBuilder: builder, Drafts: drafts}).PrepareEmployerReply(conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != AIActionDraftReply || decision.ReplyRequirement != ReplyRequired || ai.calls != 1 {
				t.Fatalf("structured profile fact was lost in orchestration: decision=%+v calls=%d user=%s", decision, ai.calls, ai.user)
			}
			if message.ID == "" || decision.Draft == "" {
				t.Fatal("orchestration did not process the employer message")
			}
			stored, err := drafts.List()
			if err != nil || len(stored) != 1 {
				t.Fatalf("draft was not created: %+v %v", stored, err)
			}
		})
	}
}

func TestTerminalConversationPolicyIsSharedAcrossStateEligibilityPilotAndNotifications(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	c := pilotConversation(t, store, "closed", now, ConversationCandidateActionRequired, "response", "10001", "Сообщаем, вакансия закрыта.", time.Hour)
	state := (ConversationStateResolver{}).Resolve(JobApplication{}, c, nil, false, nil, now)
	if state.Status != ConversationClosed {
		t.Fatalf("terminal employer message remained actionable: %+v", state)
	}

	kb := contextTestKnowledge()
	resolver := NewCandidateContextResolver(kb)
	ctx, err := NewConversationContextBuilder(store, resolver).BuildForReply(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ReplyRequirement != NoReplyNeeded || ctx.CandidateContext.MessageIntent != EmployerMessageIntentTerminal {
		t.Fatalf("terminal policy was not propagated to context: %+v", ctx)
	}
	eligibility := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Context: &ctx, Now: now})
	if eligibility.Eligible || eligibility.StateStatus == string(ConversationCandidateActionRequired) || !hasEligibilityCode(eligibility.Blockers, "CLOSED_CONVERSATION") {
		t.Fatalf("terminal conversation remained eligible: %+v", eligibility)
	}

	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Неважно", nil, nil)}
	decision, err := NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{ConversationBuilder: NewConversationContextBuilder(store, resolver)}).PrepareEmployerReply(c.ID)
	if err != nil || decision.Action != AIActionNoReplyNeeded || decision.ReplyRequirement != NoReplyNeeded || ai.calls != 0 {
		t.Fatalf("terminal conversation reached AI reply path: decision=%+v err=%v calls=%d", decision, err, ai.calls)
	}

	set, err := BuildPilotCandidateReports(store, nil, NewCandidateClarificationStore(""), resolver, NewAIDraftStore(""), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range append(append(set.Recommended, set.Possible...), set.NotRecommended...) {
		if item.ConversationID == c.ID {
			t.Fatalf("terminal conversation appeared in pilot candidate reports: %+v", item)
		}
	}
	inbox, err := NewHHReadSyncServiceWithOptions(nil, HHReadSyncServiceOptions{Conversations: store}).GetCandidateInbox()
	if err != nil || len(inbox.Items) != 0 {
		t.Fatalf("terminal conversation appeared in inbox: %+v err=%v", inbox, err)
	}
	notifications := NewNotificationStore("")
	engine := NewCandidateNotificationEngine(notifications, nil, store, nil, DefaultFollowUpPolicy(), time.Minute)
	if _, err := engine.Calculate(CareerSnapshot{Conversations: []EmployerConversation{c}}, now); err != nil {
		t.Fatal(err)
	}
	if len(notifications.List()) != 0 {
		t.Fatalf("terminal conversation created notifications: %+v", notifications.List())
	}
}

func TestClosedPositionVariantsUseTheSameDeterministicTerminalPolicy(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	messages := []string{
		"Спасибо за\u00a0интерес к\u00a0вакансии! Мы\u00a0ценим ваше желание работать с\u00a0нами, но\u00a0уже закрыли эту позицию.",
		"Спасибо за интерес к вакансии! Мы ценим ваше желание работать с нами, но уже закрыли эту позицию.",
	}
	for i, message := range messages {
		store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		id := "closed-variant-" + string(rune('a'+i))
		conversation := pilotConversation(t, store, id, now, ConversationCandidateActionRequired, "response", "2000"+string(rune('1'+i)), message, time.Hour)
		state := (ConversationStateResolver{}).Resolve(JobApplication{}, conversation, nil, false, nil, now)
		if state.Status != ConversationClosed {
			t.Fatalf("closed position variant %d was not terminal: %+v", i, state)
		}
		resolver := NewCandidateContextResolver(contextTestKnowledge())
		context, err := NewConversationContextBuilder(store, resolver).BuildForReply(id)
		if err != nil {
			t.Fatal(err)
		}
		if context.ReplyRequirement != NoReplyNeeded || context.CandidateContext.MessageIntent != EmployerMessageIntentTerminal {
			t.Fatalf("closed position variant %d did not use unified no-reply policy: %+v", i, context)
		}
		eligibility := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: conversation, Context: &context, Now: now})
		if eligibility.Eligible || !hasEligibilityCode(eligibility.Blockers, "CLOSED_CONVERSATION") {
			t.Fatalf("closed position variant %d remained eligible: %+v", i, eligibility)
		}
	}
}

func TestOptionalEmployerMessageDoesNotBecomeCandidateAction(t *testing.T) {
	conversations, conversation, kb, builder := conversationTestBuilder(t)
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Спасибо, получили информацию.", 1))
	ctx, err := builder.BuildForReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ReplyRequirement != ReplyOptional {
		t.Fatalf("acknowledgement was not classified as optional: %+v", ctx)
	}
	state := (ConversationStateResolver{}).Resolve(JobApplication{}, conversationWithMessages(t, conversations, conversation.ID), nil, false, nil, time.Now().UTC())
	if state.Status == ConversationCandidateActionRequired {
		t.Fatal("optional reply became candidate_action_required")
	}
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Спасибо!", nil, nil)}
	decision, err := NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{ConversationBuilder: builder}).PrepareEmployerReply(conversation.ID)
	if err != nil || decision.Action != AIActionNoReplyNeeded || ai.calls != 0 {
		t.Fatalf("optional reply reached AI: decision=%+v err=%v calls=%d", decision, err, ai.calls)
	}
	_ = kb
}

func conversationWithMessages(t *testing.T, store *ConversationStore, id string) EmployerConversation {
	t.Helper()
	c, err := store.GetConversation(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReadOnlyPreflightCanBeReadyDuringDryRun(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, false, nil)
	gateway.DryRun = true
	action := approveGatewayDraft(t, gateway, draft)
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if !preflight.Allowed || reader.reads != 1 || writer.calls != 0 {
		t.Fatalf("dry-run preflight was coupled to write capability: %+v reads=%d writes=%d", preflight, reader.reads, writer.calls)
	}
}
