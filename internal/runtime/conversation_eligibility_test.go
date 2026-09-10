package runtime

import (
	"testing"
	"time"
)

func eligibilityTestConversation(status ConversationStatus, messages []ConversationMessage) EmployerConversation {
	now := time.Now().UTC().Add(-time.Minute)
	return EmployerConversation{ID: "conversation-1", VacancyID: 42, HHConversationID: "12345", RawStatus: "response", Status: status, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, Messages: messages}
}

func eligibilityTestContext() *ConversationContext {
	return &ConversationContext{CandidateContext: CandidateContext{MissingInformation: []CandidateMissingInformation{}}, UnresolvedQuestions: []ConversationClarification{}, ConsistencyWarnings: []ConversationConsistencyWarning{}}
}

func TestEvaluateReplyEligibilityAllowsDirectHHReplyWithoutApplication(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	c := eligibilityTestConversation(ConversationCandidateActionRequired, []ConversationMessage{
		{ID: "system", Timestamp: now.Add(-3 * time.Minute), Sender: ConversationSenderSystem, Direction: ConversationIncoming, Source: ConversationSourceHH, HHSystemEvent: true},
		{ID: "employer", Timestamp: now, Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Source: ConversationSourceHH, Text: "Расскажите о вашем опыте"},
	})
	decision := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Context: eligibilityTestContext(), Now: now})
	if !decision.Eligible || decision.Classification != EligibilitySafe {
		t.Fatalf("expected safe direct reply, got %#v", decision)
	}
}

func TestSystemEventDoesNotBecomeLatestHumanMessage(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	c := eligibilityTestConversation(ConversationCandidateActionRequired, []ConversationMessage{
		{ID: "employer", Timestamp: now.Add(-time.Minute), Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Source: ConversationSourceHH, Text: "Добрый день"},
		{ID: "workflow", Timestamp: now, Sender: ConversationSenderSystem, Direction: ConversationIncoming, Source: ConversationSourceHH, HHSystemEvent: true},
	})
	decision := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Context: eligibilityTestContext(), Now: now})
	if !decision.Eligible {
		t.Fatalf("workflow event incorrectly blocked reply: %#v", decision)
	}
}

func TestUnknownHHStatusRequiresReview(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	c := eligibilityTestConversation(ConversationCandidateActionRequired, []ConversationMessage{{ID: "employer", Timestamp: now, Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Source: ConversationSourceHH, Text: "Вопрос"}})
	c.RawStatus = "new_hh_state"
	decision := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Context: eligibilityTestContext(), Now: now})
	if decision.Classification != EligibilityManualReview || decision.Eligible {
		t.Fatalf("expected manual review for unknown HH status, got %#v", decision)
	}
	if !hasEligibilityCode(decision.Blockers, "UNKNOWN_HH_STATUS") {
		t.Fatalf("missing UNKNOWN_HH_STATUS: %#v", decision.Blockers)
	}
}

func TestInstructionAndExternalInvitationHaveSeparateGates(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	for _, test := range []struct {
		name, message, code, contextStatus string
	}{
		{name: "instruction", message: `Вы внимательно ознакомились с вакансией и ее условиями? Если ознакомились - напишите именно "Да"`, code: "USER_CONFIRMATION_REQUIRED", contextStatus: CandidateContextStatusUserConfirmationRequired},
		{name: "external invitation", message: "Приглашаем на следующий этап. Перейдите по ссылке https://interview.getprofi.me/test. Интервью занимает около 30-40 минут.", code: "EXTERNAL_ACTION_REQUIRED", contextStatus: CandidateContextStatusAnswerable},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := eligibilityTestConversation(ConversationCandidateActionRequired, []ConversationMessage{{ID: "employer", Timestamp: now, Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Source: ConversationSourceHH, Text: test.message}})
			resolver := NewCandidateContextResolver(contextTestKnowledge())
			store := NewConversationStore("")
			if _, err := store.UpsertConversation(c); err != nil {
				t.Fatal(err)
			}
			ctx, err := NewConversationContextBuilder(store, resolver).BuildForReply(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			decision := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Context: &ctx, Now: now})
			if decision.Classification == EligibilitySafe || !hasEligibilityCode(decision.Blockers, test.code) {
				t.Fatalf("missing semantic gate %s: %+v", test.code, decision)
			}
			if test.name == "instruction" && ctx.CandidateContext.UserConfirmationRequired != true {
				t.Fatalf("instruction did not require confirmation: %+v", ctx.CandidateContext)
			}
			if test.name == "external invitation" {
				action := externalInterviewAction(test.message, now, now.Add(time.Hour))
				if action == nil || action.Classification != ExternalActionRequired || action.Type != ExternalActionInterviewInvitation || action.Destination == "" || action.Duration == "" {
					t.Fatalf("external action details are incomplete: %+v", action)
				}
			}
		})
	}
}

func TestEvaluateFollowUpHasStrongerRequirementsThanReply(t *testing.T) {
	now := time.Now().UTC().Add(-time.Hour)
	c := eligibilityTestConversation(ConversationWaitingEmployer, []ConversationMessage{{ID: "candidate", Timestamp: now, Sender: ConversationSenderCandidate, Direction: ConversationOutgoing, Source: ConversationSourceHH, Text: "Спасибо, буду ждать"}})
	a := JobApplication{ID: "application-1", ExternalID: "neg-1", VacancyID: 42, Source: ApplicationSourceHH, Status: ApplicationEmployerReplied}
	input := EligibilityEvaluationInput{Conversation: c, Application: a, Applications: []JobApplication{a}, Context: eligibilityTestContext(), AppliedAt: &now, Now: now.Add(time.Hour)}
	decision := EvaluateFollowUpEligibility(input)
	if !decision.Eligible {
		t.Fatalf("expected follow-up to be eligible with confirmed application, got %#v", decision)
	}

	input.Applications = nil
	decision = EvaluateFollowUpEligibility(input)
	if decision.Eligible || !hasEligibilityCode(decision.Blockers, "FOLLOW_UP_REQUIRES_APPLICATION") {
		t.Fatalf("expected follow-up relation blocker, got %#v", decision)
	}
}

func hasEligibilityCode(findings []EligibilityFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
