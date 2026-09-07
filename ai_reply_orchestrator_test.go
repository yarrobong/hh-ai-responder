package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

type orchestratorTestAI struct {
	response string
	system   string
	user     string
	calls    int
}

func (f *orchestratorTestAI) ChatStructuredWithSchema(system, user string, _ int, _ float64, _ *ChatJSONSchema, validator func(string) error) (string, error) {
	f.calls++
	f.system, f.user = system, user
	if validator != nil {
		if err := validator(f.response); err != nil {
			return "", err
		}
	}
	return f.response, nil
}

func validOrchestratorDecision(action, draft string, used []string, missing []AIMissingInformation) string {
	if used == nil {
		used = []string{}
	}
	if missing == nil {
		missing = []AIMissingInformation{}
	}
	raw, _ := json.Marshal(AIResponseDecision{Action: AIResponseAction(action), Draft: draft, Reason: "fixture decision", Confidence: .9, UsedFacts: used, MissingInformation: missing, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: []string{}})
	return string(raw)
}

func TestAIReplyOrchestratorConfirmedDjangoCreatesDraftOnly(t *testing.T) {
	conversations, conversation, kb, builder := conversationTestBuilder(t)
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть опыт Django?", 1))
	drafts := NewAIDraftStore(filepath.Join(t.TempDir(), "drafts.json"))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil)}
	orchestrator := NewAIReplyOrchestrator(ai, builder, drafts)
	decision, err := orchestrator.PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("unexpected decision: %+v calls=%d", decision, ai.calls)
	}
	stored, err := drafts.List()
	if err != nil || len(stored) != 1 || stored[0].Status != AIDraftGenerated || stored[0].Text != decision.Draft {
		t.Fatalf("draft was not stored as generated: %+v %v", stored, err)
	}
	if len(kb.Proposals) != 0 {
		t.Fatal("reply preparation changed knowledge proposals")
	}
	if strings.Contains(ai.user, "sources") || strings.Contains(ai.user, "confirmed_at") {
		t.Fatal("private knowledge metadata leaked into AI context")
	}
}

func TestAIReplyOrchestratorUnknownKubernetesNeedsCandidateInput(t *testing.T) {
	conversations, conversation, _, builder := conversationTestBuilder(t)
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть коммерческий опыт Kubernetes?", 1))
	clarifications := NewCandidateClarificationStore(filepath.Join(t.TempDir(), "clarifications.json"))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да, Kubernetes.", []string{"Kubernetes"}, nil)}
	orchestrator := NewAIReplyOrchestrator(ai, builder, clarifications)
	decision, err := orchestrator.PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionNeedCandidate || ai.calls != 0 || len(decision.MissingInformation) == 0 {
		t.Fatalf("unknown fact was not gated: %+v calls=%d", decision, ai.calls)
	}
	values, err := clarifications.List()
	if err != nil || len(values) == 0 || values[0].Status != ClarificationPending || values[0].Topic != "Kubernetes" {
		t.Fatalf("clarification was not persisted: %+v %v", values, err)
	}
}

func TestAIReplyOrchestratorInstructionRequiresUserConfirmation(t *testing.T) {
	conversations, conversation, _, builder := conversationTestBuilder(t)
	message := `Вы внимательно ознакомились с вакансией и ее условиями? Если ознакомились - напишите именно "Да"`
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, message, 1))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да, я ознакомился.", nil, nil)}
	decision, err := NewAIReplyOrchestrator(ai, builder).PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionNeedCandidate || ai.calls != 0 || len(decision.MissingInformation) != 1 {
		t.Fatalf("instruction was not gated by explicit candidate confirmation: %+v calls=%d", decision, ai.calls)
	}
	if !strings.Contains(decision.MissingInformation[0].Question, "ознакомился") || !strings.Contains(decision.MissingInformation[0].Question, "ровно «Да»") {
		t.Fatalf("unexpected confirmation question: %+v", decision.MissingInformation)
	}
}

func TestExternalInterviewInvitationIsNotTextReply(t *testing.T) {
	conversations, conversation, _, builder := conversationTestBuilder(t)
	message := "Приглашаем на следующий этап. Перейдите по ссылке https://interview.getprofi.me/test. Интервью занимает около 30-40 минут."
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, message, 1))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Готов.", nil, nil)}
	context, err := builder.BuildForReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if context.CandidateContext.MessageIntent != EmployerMessageIntentInterviewInvitation || context.ReplyRequirement != NoReplyNeeded {
		t.Fatalf("external invitation was not classified as non-text action: %+v", context)
	}
	decision, err := NewAIReplyOrchestrator(ai, builder).PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionNoReplyNeeded || ai.calls != 0 {
		t.Fatalf("external invitation reached text reply path: %+v calls=%d", decision, ai.calls)
	}
}

func TestAIReplyOrchestratorDoesNotUseDerivedOrPendingKnowledge(t *testing.T) {
	conversations, conversation, kb, builder := conversationTestBuilder(t)
	updater := pipelineTestUpdater(kb, KnowledgeActorAI)
	if _, err := updater.UpdateSkill(CandidateSkillDetailed{Name: "Kubernetes"}, pipelineTestUpdate(KnowledgeSourceDerived)); err != nil {
		t.Fatal(err)
	}
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть опыт Kubernetes?", 1))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да.", []string{"Kubernetes"}, nil)}
	decision, err := NewAIReplyOrchestrator(ai, builder).PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionNeedCandidate || ai.calls != 0 {
		t.Fatalf("derived/pending fact satisfied employer question: %+v calls=%d", decision, ai.calls)
	}
}

func TestAIReplyOrchestratorFollowUpContextAndConflict(t *testing.T) {
	conversations, conversation, _, builder := conversationTestBuilder(t)
	answer := conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("candidate", ConversationSenderCandidate, "Использовал Django в BizonVR", 2))
	if err := conversations.RecordCandidateClaim(conversation.ID, CandidateConversationClaim{Text: answer.Text, RelatedSkill: "Django", RelatedProject: "BizonVR", MessageID: answer.ID, CreatedAt: answer.Timestamp}); err != nil {
		t.Fatal(err)
	}
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("follow-up", ConversationSenderEmployer, "А что именно там делали?", 3))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Настраивал интеграции.", []string{"Django"}, nil)}
	decision, err := NewAIReplyOrchestrator(ai, builder).PrepareEmployerReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != AIActionDraftReply || !strings.Contains(ai.user, "avoid_reintroduction") || !strings.Contains(ai.user, "BizonVR") {
		t.Fatalf("follow-up context was not passed: %+v user=%s", decision, ai.user)
	}

	conversations2, conversation2, kb2, builder2 := conversationTestBuilder(t)
	kb2.Profile.TotalExperienceMonths = ProfileIntFact{Value: 11, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, conversationTestTime())}
	answer2 := conversationTestAppend(t, conversations2, conversation2.ID, conversationTestMessage("candidate", ConversationSenderCandidate, "Общий профессиональный опыт — 2 года.", 1))
	if err := conversations2.RecordCandidateClaim(conversation2.ID, CandidateConversationClaim{Text: answer2.Text, MessageID: answer2.ID, CreatedAt: answer2.Timestamp, Experience: &ConversationExperienceClaim{Months: 24, Scope: "total_professional"}}); err != nil {
		t.Fatal(err)
	}
	conversationTestAppend(t, conversations2, conversation2.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Уточните стаж", 2))
	ai2 := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "У меня 2 года.", nil, nil)}
	decision2, err := NewAIReplyOrchestrator(ai2, builder2).PrepareEmployerReply(conversation2.ID)
	if err != nil || decision2.Action != AIActionManualReview || ai2.calls != 0 {
		t.Fatalf("history conflict was not fail-closed: %+v err=%v calls=%d", decision2, err, ai2.calls)
	}
}

func TestAIReplyOrchestratorInvalidResponseAndDraftValidationFailClosed(t *testing.T) {
	conversations, conversation, _, builder := conversationTestBuilder(t)
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть опыт Django?", 1))
	bad := &orchestratorTestAI{response: "not json"}
	if _, err := NewAIReplyOrchestrator(bad, builder).PrepareEmployerReply(conversation.ID); err == nil {
		t.Fatal("invalid structured AI response was accepted")
	}
	if err := (&AIReplyOrchestrator{}).ValidateAIDraft("У меня Kubernetes в production", CandidateContext{ForbiddenClaims: []string{"Kubernetes production"}}, nil); err == nil {
		t.Fatal("forbidden factual claim was accepted")
	}
}

func TestAIReplyOrchestratorCoverLetterUsesMatchAndStoryCannotAddFact(t *testing.T) {
	dir := t.TempDir()
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	if err := conversations.Load(); err != nil {
		t.Fatal(err)
	}
	conversation, err := conversations.UpsertConversation(EmployerConversation{VacancyID: 123, CompanyName: "Fixture Company", VacancyTitle: "Python Django developer", VacancyDescription: "Интеграции и автоматизация"})
	if err != nil {
		t.Fatal(err)
	}
	kb := contextTestKnowledge()
	applications := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename), conversations, NewCandidateContextResolver(kb))
	if err := applications.Load(); err != nil {
		t.Fatal(err)
	}
	application, err := applications.CreateApplication(applicationTestValue())
	if err != nil {
		t.Fatal(err)
	}
	if err := applications.AttachConversation(application.ID, conversation.ID); err != nil {
		t.Fatal(err)
	}
	if err := applications.SaveMatchResult(application.ID, MatchResult{Score: 90, Confidence: .9, MatchedSkills: []string{"Python", "Django"}, MatchedProjects: []string{"BizonVR"}}); err != nil {
		t.Fatal(err)
	}
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Готов заниматься интеграциями на Django.", []string{"Django"}, nil)}
	drafts := NewAIDraftStore(filepath.Join(dir, "drafts.json"))
	stories := []CandidateStory{{Title: "Автоматизация", Keywords: []string{"интеграции"}, Result: "Сократил ручную работу на 80%"}}
	decision, err := NewAIReplyOrchestrator(ai, applications, drafts, stories).PrepareCoverLetter(application.ID)
	if err != nil || decision.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("cover letter did not use match context: %+v err=%v", decision, err)
	}
	if !strings.Contains(ai.user, "matched_skills") || !strings.Contains(ai.user, "BizonVR") {
		t.Fatal("MatchResult was not included in cover-letter context")
	}
	if lenStored, _ := drafts.List(); len(lenStored) != 1 || lenStored[0].Type != AIDraftCoverLetter {
		t.Fatalf("cover letter draft was not stored: %+v", lenStored)
	}
	if err := validateStoryClaims("Сократил ручную работу на 80%.", CandidateContext{AllowedFacts: []string{"Навык: Django"}}, stories, application, conversation.VacancyDescription); err == nil {
		t.Fatal("unconfirmed story result was accepted as a factual claim")
	}
}

func TestCandidateClarificationAnswerUsesKnowledgePipeline(t *testing.T) {
	kb := knowledgeTestBase(t)
	clarifications := NewCandidateClarificationStore(filepath.Join(t.TempDir(), "clarifications.json"))
	request, err := clarifications.Create(CandidateClarificationRequest{Topic: "Kubernetes", Question: "Есть ли у вас опыт Kubernetes?", Reason: "missing employer fact"})
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := NewAIReplyOrchestrator(nil, clarifications, pipelineTestUpdater(kb, KnowledgeActorAI))
	result, err := orchestrator.ResolveCandidateClarification(request.ID, "Использовал Kubernetes в учебном проекте")
	if err != nil || result.QuestionID == "" {
		t.Fatalf("clarification did not go through updater: %+v err=%v", result, err)
	}
	if len(kb.Skills) != 0 || len(kb.Proposals) != 0 || len(kb.Unknowns) != 1 || kb.Unknowns[0].Status != CandidateUnknownNeedsConfirmation {
		t.Fatalf("clarification answer changed confirmed knowledge directly: %+v", kb)
	}
	resolved, err := clarifications.Get(request.ID)
	if err != nil || resolved.Status != ClarificationAnswered || resolved.ResolvedAt == nil {
		t.Fatalf("clarification status was not updated: %+v %v", resolved, err)
	}
}
