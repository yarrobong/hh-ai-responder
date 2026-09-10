package conversationpolicy

import (
	"testing"
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

var policyTestTime = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func policyMessage(sender conversation.Sender, text string, offset time.Duration) conversation.Message {
	direction := conversation.DirectionIncoming
	if sender == conversation.SenderCandidate {
		direction = conversation.DirectionOutgoing
	}
	return conversation.Message{ID: text, Timestamp: policyTestTime.Add(offset), Sender: sender, Text: text, Source: conversation.SourceHH, Direction: direction}
}

func policyConversation(message conversation.Message, status conversation.Status) conversation.EmployerConversation {
	return conversation.EmployerConversation{ID: "conversation", Status: status, Messages: []conversation.Message{message}}
}

func TestReplyPolicyCharacterization(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		intent candidatecontext.EmployerMessageIntent
		want   ReplyRequirement
	}{
		{"salary question", "Какой уровень дохода вы рассматриваете?", candidatecontext.EmployerMessageIntentFactualQuestion, ReplyRequired},
		{"relocation question", "Готовы ли Вы к релокации?", candidatecontext.EmployerMessageIntentFactualQuestion, ReplyRequired},
		{"courtesy", "Спасибо, рассмотрим ваше резюме.", candidatecontext.EmployerMessageIntentStatusMessage, ReplyOptional},
		{"rejection", "К сожалению, выбрали другого кандидата.", candidatecontext.EmployerMessageIntentRejection, NoReplyNeeded},
		{"interview", "Приглашаем на собеседование завтра.", candidatecontext.EmployerMessageIntentInterviewInvitation, ReplyRequired},
		{"external action", "Пройдите интервью: https://interview.getprofi.ru/abc", candidatecontext.EmployerMessageIntentInterviewInvitation, NoReplyNeeded},
		{"confirmation instruction", `Напишите именно "Да", если ознакомились с условиями.`, candidatecontext.EmployerMessageIntentInstruction, ReplyRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := policyMessage(conversation.SenderEmployer, test.text, 0)
			result := EvaluateReplyPolicy(ReplyPolicyInput{Conversation: policyConversation(message, ""), Latest: &message, Intent: test.intent})
			if result.Requirement != test.want {
				t.Fatalf("requirement=%s want=%s reason=%s", result.Requirement, test.want, result.Reason)
			}
		})
	}
}

func TestReplyPolicyDoesNotTreatCandidateReplyOrServiceEventAsEmployerQuestion(t *testing.T) {
	candidate := policyMessage(conversation.SenderCandidate, "Готов ответить.", 0)
	if got := EvaluateReplyPolicy(ReplyPolicyInput{Conversation: policyConversation(candidate, ""), Latest: &candidate}).Requirement; got != NoReplyNeeded {
		t.Fatalf("candidate already replied: got %s", got)
	}
	service := conversation.Message{ID: "service", Timestamp: policyTestTime, Sender: conversation.SenderSystem, Text: "status", Source: conversation.SourceHH, Direction: conversation.DirectionIncoming, HHSystemEvent: true}
	c := policyConversation(service, "")
	if got := LatestMeaningfulMessage(c); got != nil {
		t.Fatalf("service event became meaningful message: %+v", got)
	}
}

func TestTerminalStateWinsOverStaleConversationText(t *testing.T) {
	employer := policyMessage(conversation.SenderEmployer, "Есть ли опыт с Python?", 0)
	c := policyConversation(employer, conversation.StatusClosed)
	state, ok := TerminalConversationState(c)
	if !ok || state != conversation.StatusClosed {
		t.Fatalf("terminal state was lost: state=%s ok=%v", state, ok)
	}
	if got := EvaluateReplyPolicy(ReplyPolicyInput{Conversation: c, Latest: &employer, Intent: candidatecontext.EmployerMessageIntentFactualQuestion}).Requirement; got != NoReplyNeeded {
		t.Fatalf("closed conversation remained replyable: %s", got)
	}
}

func TestInstructionClassificationDoesNotClaimExternalAction(t *testing.T) {
	text := `Напишите "Да", если ознакомились с условиями.`
	if !candidatecontext.InstructionNeedsUserConfirmation(text) {
		t.Fatal("instruction was not routed to candidate confirmation")
	}
	if got := candidatecontext.ClassifyEmployerMessage(text); got != candidatecontext.EmployerMessageIntentInstruction {
		t.Fatalf("instruction was classified as %s", got)
	}
}

func TestExternalInterviewActionIsTypedAndReadOnly(t *testing.T) {
	action := ExternalInterviewAction("Пройдите интервью: https://interview.getprofi.ru/abc", policyTestTime, policyTestTime.Add(time.Minute))
	if action == nil || action.Classification != ExternalActionRequired || action.Type != ExternalActionInterviewInvitation || action.Destination == "" {
		t.Fatalf("unexpected external action: %+v", action)
	}
}

func TestConsistencyDoesNotPromoteConversationClaim(t *testing.T) {
	claims := []conversation.Claim{{Text: "Работал с Kubernetes", MessageID: "m1"}}
	warnings := CheckConsistency(ConsistencyInput{Claims: claims})
	if len(warnings) != 1 || warnings[0].Status != ConsistencyUnknown {
		t.Fatalf("unknown claim was not kept unknown: %+v", warnings)
	}
}
