package writeapproval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContentHash is the existing raw-text SHA-256 contract. Do not trim or
// otherwise normalize the approved artifact.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// ConversationVersion preserves the pre-extraction JSON shape and therefore
// remains compatible with persisted approvals.
func ConversationVersion(c ConversationSnapshot) string {
	raw, _ := json.Marshal(struct {
		ID, HHID, ApplicationID string
		VacancyID               int
		Status                  string
		Messages                []MessageSnapshot
	}{c.ID, c.ExternalID, c.ApplicationID, c.VacancyID, c.Status, cloneMessages(c.Messages)})
	return ContentHash(string(raw))
}

func LatestEmployerMessageHash(c ConversationSnapshot) string {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		message := c.Messages[i]
		if message.Sender == "employer" && !message.HHSystemEvent && !message.ContentUnavailable {
			return ContentHash(message.Text)
		}
	}
	return ""
}

func active(status ActionStatus) bool {
	return status == ActionPending || status == ActionApproved || status == ActionSending
}

func purpose(actionType ActionType, explicit string) string {
	if explicit != "" {
		return explicit
	}
	switch actionType {
	case ActionConversationReply:
		return "conversation_reply"
	case ActionFollowUp:
		return "follow_up"
	default:
		return ""
	}
}
