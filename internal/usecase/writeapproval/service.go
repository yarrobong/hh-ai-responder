package writeapproval

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
)

type IDGenerator func(kind string) (string, error)

type Options struct {
	Now         func() time.Time
	IDGenerator IDGenerator
}

type Service struct {
	now         func() time.Time
	idGenerator IDGenerator
}

func NewService(options Options) *Service {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	generator := options.IDGenerator
	if generator == nil {
		generator = secureID
	}
	return &Service{now: now, idGenerator: generator}
}

func secureID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate write approval id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func (s *Service) Create(input CreateInput) (ApprovalCreated, error) {
	if s == nil {
		return ApprovalCreated{}, errors.New("write approval service is nil")
	}
	if strings.TrimSpace(input.ApprovedBy) == "" {
		return ApprovalCreated{}, ErrApprovedByRequired
	}
	if input.Draft.Status != DraftGenerated && input.Draft.Status != DraftApproved {
		return ApprovalCreated{}, ErrInvalidDraftState
	}
	if input.ActionType != ActionConversationReply && input.ActionType != ActionFollowUp {
		return ApprovalCreated{}, ErrUnsupportedDraft
	}
	if input.ActionType == ActionConversationReply && input.Draft.Type != DraftEmployerReply || input.ActionType == ActionFollowUp && input.Draft.Type != DraftFollowUp {
		return ApprovalCreated{}, ErrUnsupportedDraft
	}
	if strings.TrimSpace(input.Draft.ID) == "" || strings.TrimSpace(input.Draft.ConversationID) == "" || strings.TrimSpace(input.Draft.Text) == "" {
		return ApprovalCreated{}, ErrApprovalInvalid
	}
	if input.Conversation.ID != input.Draft.ConversationID {
		return ApprovalCreated{}, ErrApprovalInvalid
	}

	wantPurpose := purpose(input.ActionType, input.ReplyPurpose)
	result := ApprovalCreated{}
	for _, old := range input.ExistingApprovals {
		oldPurpose := purpose(old.ActionType, old.ReplyPurpose)
		if old.ConversationID == input.Conversation.ID && oldPurpose == wantPurpose && old.LastMessageID == input.LastMessageID && active(old.Status) && old.DraftID != input.Draft.ID {
			return ApprovalCreated{}, ErrActiveApproval
		}
		if old.DraftID != input.Draft.ID {
			continue
		}
		if old.Status == ActionApproved && old.RelevantKnowledgeHash != "" && input.CurrentRelevantKnowledgeHash != "" && old.RelevantKnowledgeHash != input.CurrentRelevantKnowledgeHash {
			result.Invalidated = append(result.Invalidated, Invalidation{ApprovalID: old.ID, Reason: "candidate knowledge relevant to this draft changed"})
			continue
		}
		if old.Status == ActionApproved || old.Status == ActionSending {
			return ApprovalCreated{}, ErrApprovalExists
		}
	}

	now := s.now().UTC()
	actionID, err := s.idGenerator("hh-action")
	if err != nil {
		return ApprovalCreated{}, err
	}
	nonce, err := s.idGenerator("hh-send")
	if err != nil {
		return ApprovalCreated{}, err
	}
	approval := Approval{
		ID: actionID, ActionType: input.ActionType, ConversationID: input.Conversation.ID,
		ApplicationID: input.Draft.ApplicationID, EmployerMessageHash: LatestEmployerMessageHash(input.Conversation), DraftID: input.Draft.ID, ReplyPurpose: wantPurpose,
		ApprovedText: input.Draft.Text, ApprovedBy: input.ApprovedBy, ApprovedAt: now,
		SourceMessageID: input.SourceMessageID, ConversationVersion: ConversationVersion(input.Conversation),
		CandidateKnowledgeVersion: input.CurrentCandidateKnowledgeVersion,
		RelevantKnowledgeHash:     input.CurrentRelevantKnowledgeHash, LastMessageID: input.LastMessageID,
		ContentHash: ContentHash(input.Draft.Text), SendNonce: nonce, Status: ActionApproved,
		CreatedAt: now, UpdatedAt: now,
	}
	result.Approval = approval
	return result, nil
}

func (s *Service) ValidateForSend(input ValidateInput) ValidationResult {
	result := ValidationResult{Status: ValidationValid, Reasons: []string{}}
	a := input.Approval
	if a.Status != ActionApproved {
		result.Status = ValidationNeedsReapproval
		result.Reasons = append(result.Reasons, "approval is not active")
	}
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.DraftID) == "" || strings.TrimSpace(a.ConversationID) == "" {
		result.Status = ValidationInvalid
		result.Reasons = append(result.Reasons, "approval evidence is incomplete")
	}
	if input.PresentedNonce != "" && input.PresentedNonce != a.SendNonce {
		result.Status = ValidationInvalid
		result.Reasons = append(result.Reasons, ErrNonceMismatch.Error())
	}
	if a.DraftID != input.Draft.ID || a.ConversationID != input.Conversation.ID {
		result.Status = ValidationInvalid
		result.Reasons = append(result.Reasons, "approval is bound to a different draft or conversation")
	}
	if input.Draft.Text != a.ApprovedText || ContentHash(input.Draft.Text) != a.ContentHash {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, ErrContentMismatch.Error())
	}
	if a.EmployerMessageHash != "" && a.EmployerMessageHash != LatestEmployerMessageHash(input.Conversation) {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, "employer message changed after approval")
	}
	if strings.TrimSpace(input.Conversation.ExternalID) == "" {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, "conversation has no strong HH external identifier")
	}
	if a.ConversationVersion != "" && a.ConversationVersion != ConversationVersion(input.Conversation) {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, "conversation changed after approval")
	}
	if a.LastMessageID != input.LatestDeliveredMessageID {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, "conversation changed after approval")
	}
	if a.RelevantKnowledgeHash != "" {
		if input.CurrentRelevantKnowledgeHash == "" {
			result.Status = ValidationStale
			result.Reasons = append(result.Reasons, "candidate knowledge snapshot is unavailable")
		} else if input.CurrentRelevantKnowledgeHash != a.RelevantKnowledgeHash {
			result.Status = ValidationStale
			result.Reasons = append(result.Reasons, ErrKnowledgeMismatch.Error())
		}
	} else if a.CandidateKnowledgeVersion != "" && a.CandidateKnowledgeVersion != input.CurrentCandidateKnowledgeVersion {
		result.Status = ValidationStale
		result.Reasons = append(result.Reasons, "candidate knowledge changed after approval")
	}
	if len(result.Reasons) > 0 {
		if result.Status == ValidationValid {
			result.Status = ValidationStale
		}
		return result
	}
	result.Evidence = AuthorizationEvidence{ApprovalID: a.ID, DraftID: a.DraftID, ConversationID: a.ConversationID, ActionType: a.ActionType, ApprovedText: a.ApprovedText, ContentHash: a.ContentHash, EmployerMessageHash: a.EmployerMessageHash, RelevantKnowledgeHash: a.RelevantKnowledgeHash, ConversationVersion: a.ConversationVersion, LastMessageID: a.LastMessageID, SendNonce: a.SendNonce}
	return result
}

// InvalidateDraft returns local invalidation decisions without persisting or
// touching an action store. Terminal transport states retain their history.
func InvalidateDraft(approvals []Approval, draftID, reason string) []Invalidation {
	result := []Invalidation{}
	for _, approval := range approvals {
		if approval.DraftID == draftID && approval.Status != ActionSent && approval.Status != ActionCancelled {
			result = append(result, Invalidation{ApprovalID: approval.ID, Reason: reason})
		}
	}
	return result
}

func InvalidateConversation(approvals []Approval, conversationID, reason string) []Invalidation {
	result := []Invalidation{}
	for _, approval := range approvals {
		if approval.ConversationID == conversationID && approval.Status == ActionApproved {
			result = append(result, Invalidation{ApprovalID: approval.ID, Reason: reason})
		}
	}
	return result
}
