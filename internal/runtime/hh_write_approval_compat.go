package runtime

import (
	"time"

	"hh-ai-responder/internal/usecase/writeapproval"
)

func approvalDraftSnapshot(draft AIDraft) writeapproval.DraftSnapshot {
	return writeapproval.DraftSnapshot{
		ID: draft.ID, Type: writeapproval.DraftType(draft.Type), ApplicationID: draft.ApplicationID,
		ConversationID: draft.ConversationID, InputFingerprint: draft.InputFingerprint,
		EmployerMessageHash: draft.EmployerMessageHash, RelevantKnowledgeHash: draft.RelevantKnowledgeHash,
		Text: draft.Text, Status: writeapproval.DraftStatus(draft.Status),
	}
}

func approvalConversationSnapshot(c EmployerConversation) writeapproval.ConversationSnapshot {
	messages := make([]writeapproval.MessageSnapshot, 0, len(c.Messages))
	for _, message := range c.Messages {
		metadata := map[string]string(nil)
		if message.Metadata != nil {
			metadata = map[string]string{}
			for key, value := range message.Metadata {
				metadata[key] = value
			}
		}
		messages = append(messages, writeapproval.MessageSnapshot{
			HHSystemEvent: message.HHSystemEvent, ContentUnavailable: message.ContentUnavailable,
			Metadata: metadata, ID: message.ID, ExternalID: message.ExternalID,
			Timestamp: message.Timestamp, Sender: string(message.Sender), Text: message.Text,
			Source: string(message.Source), Direction: string(message.Direction),
		})
	}
	return writeapproval.ConversationSnapshot{ID: c.ID, ExternalID: c.HHConversationID, Status: string(c.Status), Messages: messages}
}

func approvalSnapshot(action ApprovedHHAction) writeapproval.Approval {
	return writeapproval.Approval{
		ID: action.ID, ActionType: writeapproval.ActionType(action.ActionType), ConversationID: action.ConversationID,
		ApplicationID: action.ApplicationID, DraftID: action.DraftID, ReplyPurpose: action.ReplyPurpose,
		ApprovedText: action.ApprovedText, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt,
		SourceMessageID: action.SourceMessageID, ConversationVersion: action.ConversationVersion,
		CandidateKnowledgeVersion: action.CandidateKnowledgeVersion, RelevantKnowledgeHash: action.RelevantKnowledgeHash,
		LastMessageID: action.LastMessageID, ContentHash: action.ContentHash, SendNonce: action.SendNonce,
		Status: writeapproval.ActionStatus(action.Status), CreatedAt: action.CreatedAt, UpdatedAt: action.UpdatedAt,
	}
}

func applyApprovalInvalidations(actions *ApprovedHHActionStore, audit *HHWriteAuditStore, invalidations []writeapproval.Invalidation) error {
	for _, invalidation := range invalidations {
		action, err := actions.Get(invalidation.ApprovalID)
		if err != nil {
			return err
		}
		action.Status = HHWriteStale
		action.Error = invalidation.Reason
		action.UpdatedAt = time.Now().UTC()
		if err := actions.put(action); err != nil {
			return err
		}
		if audit != nil {
			_ = audit.Append(HHWriteEvent{ActionID: action.ID, Type: "stale", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "stale", Error: action.Error})
		}
	}
	return nil
}

// Compatibility wrappers keep legacy root tests and projections source
// compatible while the algorithms have one authoritative implementation.
func contentHash(text string) string { return writeapproval.ContentHash(text) }

func conversationVersion(c EmployerConversation) string {
	return writeapproval.ConversationVersion(approvalConversationSnapshot(c))
}
