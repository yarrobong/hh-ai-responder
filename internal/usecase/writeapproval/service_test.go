package writeapproval

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testInput() CreateInput {
	return CreateInput{
		Draft:           DraftSnapshot{ID: "draft-1", Type: DraftEmployerReply, ConversationID: "conversation-1", Text: "approved text", Status: DraftGenerated},
		ActionType:      ActionConversationReply,
		Conversation:    ConversationSnapshot{ID: "conversation-1", ExternalID: "hh-1", Status: "candidate_action_required", Messages: []MessageSnapshot{{ID: "employer-1", Timestamp: time.Unix(1, 0).UTC(), Sender: "employer", Text: "hello", Source: "hh", Direction: "incoming"}}},
		SourceMessageID: "employer-1", LastMessageID: "employer-1", ReplyPurpose: "conversation_reply", ApprovedBy: "candidate",
		CurrentRelevantKnowledgeHash: "knowledge-1", CurrentCandidateKnowledgeVersion: "candidate-1",
	}
}

func testService() *Service {
	ids := []string{"hh-action-1", "hh-send-1"}
	return NewService(Options{
		Now: func() time.Time { return time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC) },
		IDGenerator: func(string) (string, error) {
			value := ids[0]
			ids = ids[1:]
			return value, nil
		},
	})
}

func createTestApproval(t *testing.T) (Approval, CreateInput) {
	t.Helper()
	input := testInput()
	created, err := testService().Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Approval.SendNonce != "hh-send-1" || created.Approval.ContentHash != ContentHash(input.Draft.Text) {
		t.Fatalf("approval evidence did not freeze content and nonce: %+v", created.Approval)
	}
	return created.Approval, input
}

func TestCreateApprovalUsesTypedEvidenceAndSecureNoncePort(t *testing.T) {
	approval, input := createTestApproval(t)
	if approval.Status != ActionApproved || approval.ApprovedText != input.Draft.Text || approval.ApprovedBy != "candidate" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	if approval.ConversationVersion != ConversationVersion(input.Conversation) {
		t.Fatal("approval did not bind the conversation version")
	}
}

func TestValidateForSendRejectsEditedDraftBeforeAuthorization(t *testing.T) {
	approval, input := createTestApproval(t)
	input.Draft.Text = "edited text"
	result := NewService(Options{}).ValidateForSend(ValidateInput{Approval: approval, Draft: input.Draft, Conversation: input.Conversation, LatestDeliveredMessageID: input.LastMessageID, CurrentRelevantKnowledgeHash: input.CurrentRelevantKnowledgeHash, CurrentCandidateKnowledgeVersion: input.CurrentCandidateKnowledgeVersion})
	if result.Status != ValidationStale || result.Status == ValidationValid || !strings.Contains(strings.Join(result.Reasons, ";"), ErrContentMismatch.Error()) {
		t.Fatalf("edited draft was authorized: %+v", result)
	}
}

func TestValidateForSendRejectsRelevantKnowledgeChangeButIgnoresUnrelatedVersion(t *testing.T) {
	approval, input := createTestApproval(t)
	changed := input
	changed.CurrentRelevantKnowledgeHash = "knowledge-2"
	result := NewService(Options{}).ValidateForSend(ValidateInput{Approval: approval, Draft: changed.Draft, Conversation: changed.Conversation, LatestDeliveredMessageID: changed.LastMessageID, CurrentRelevantKnowledgeHash: changed.CurrentRelevantKnowledgeHash, CurrentCandidateKnowledgeVersion: "unrelated-candidate-version"})
	if result.Status != ValidationStale || !strings.Contains(strings.Join(result.Reasons, ";"), ErrKnowledgeMismatch.Error()) {
		t.Fatalf("relevant knowledge change was not stale: %+v", result)
	}
	unchanged := NewService(Options{}).ValidateForSend(ValidateInput{Approval: approval, Draft: input.Draft, Conversation: input.Conversation, LatestDeliveredMessageID: input.LastMessageID, CurrentRelevantKnowledgeHash: input.CurrentRelevantKnowledgeHash, CurrentCandidateKnowledgeVersion: "unrelated-candidate-version"})
	if unchanged.Status != ValidationValid {
		t.Fatalf("unrelated legacy version changed a scoped approval: %+v", unchanged)
	}
}

func TestCreateApprovalRejectsInvalidAndManualReviewStates(t *testing.T) {
	input := testInput()
	input.Draft.Status = DraftRejected
	if _, err := testService().Create(input); !errors.Is(err, ErrInvalidDraftState) {
		t.Fatalf("invalid draft state error = %v", err)
	}
	input = testInput()
	input.ExistingApprovals = []Approval{{ID: "manual", DraftID: input.Draft.ID, ConversationID: input.Conversation.ID, ActionType: ActionConversationReply, Status: ActionManualReview}}
	created, err := testService().Create(input)
	if err != nil || created.Approval.Status != ActionApproved {
		t.Fatalf("manual-review history incorrectly blocked a fresh approval: %+v %v", created, err)
	}
	input = testInput()
	input.ExistingApprovals = []Approval{{ID: "active", DraftID: input.Draft.ID, ConversationID: input.Conversation.ID, ActionType: ActionConversationReply, LastMessageID: input.LastMessageID, Status: ActionApproved, RelevantKnowledgeHash: input.CurrentRelevantKnowledgeHash}}
	if _, err := testService().Create(input); !errors.Is(err, ErrApprovalExists) {
		t.Fatalf("active approval error = %v", err)
	}
}

func TestReapprovalInvalidatesOldKnowledgeApprovalAndIssuesNewNonce(t *testing.T) {
	old, input := createTestApproval(t)
	input.ExistingApprovals = []Approval{old}
	input.CurrentRelevantKnowledgeHash = "knowledge-2"
	ids := []string{"hh-action-2", "hh-send-2"}
	service := NewService(Options{IDGenerator: func(string) (string, error) { value := ids[0]; ids = ids[1:]; return value, nil }})
	created, err := service.Create(input)
	if err != nil || len(created.Invalidated) != 1 || created.Invalidated[0].ApprovalID != old.ID || created.Approval.SendNonce == old.SendNonce {
		t.Fatalf("old approval was not invalidated independently: %+v %v", created, err)
	}
	old.Status = ActionStale
	old.RelevantKnowledgeHash = input.CurrentRelevantKnowledgeHash
	stale := NewService(Options{}).ValidateForSend(ValidateInput{Approval: old, Draft: input.Draft, Conversation: input.Conversation, LatestDeliveredMessageID: input.LastMessageID, CurrentRelevantKnowledgeHash: input.CurrentRelevantKnowledgeHash})
	if stale.Status != ValidationNeedsReapproval {
		t.Fatalf("old nonce/approval remained send-authorized: %+v", stale)
	}
}

func TestConversationVersionChangesOnRelevantMessageMutation(t *testing.T) {
	input := testInput()
	first := ConversationVersion(input.Conversation)
	input.Conversation.Messages[0].Text = "changed"
	if first == ConversationVersion(input.Conversation) {
		t.Fatal("conversation version ignored a message mutation")
	}
}

func TestInvalidationDecisionsNeverTouchTerminalTransportHistory(t *testing.T) {
	approvals := []Approval{{ID: "sent", DraftID: "draft-1", ConversationID: "conversation-1", Status: ActionSent}, {ID: "approved", DraftID: "draft-1", ConversationID: "conversation-1", Status: ActionApproved}}
	invalidated := InvalidateDraft(approvals, "draft-1", "draft edited after approval")
	if len(invalidated) != 1 || invalidated[0].ApprovalID != "approved" {
		t.Fatalf("terminal approval history was invalidated: %+v", invalidated)
	}
}
