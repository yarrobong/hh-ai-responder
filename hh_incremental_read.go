package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type hhMetadataContextKey struct{}
type hhProgressContextKey struct{}

// A lightweight inbox refresh is a display optimization, never a freshness
// assertion for safety. Full Sync and ReadConversation(State) bypass it.
func hhListFingerprint(chat ChatListItem, list *ChatsResponse) string {
	if chat.Id <= 0 || chat.LastMessage == nil || chat.LastMessage.ID <= 0 || chat.LastActivityTime.IsZero() {
		return ""
	}
	resources := struct {
		Vacancies    map[string]ChatVacancyResource
		Topics       map[string]ChatNegotiationTopic
		Participants map[string]json.RawMessage
	}{map[string]ChatVacancyResource{}, map[string]ChatNegotiationTopic{}, map[string]json.RawMessage{}}
	for _, id := range chat.Resources.Vacancy {
		resources.Vacancies[id] = list.Resources.Vacancies[id]
	}
	for _, id := range chat.Resources.NegotiationTopic {
		resources.Topics[id] = list.Resources.NegotiationTopics[id]
	}
	for _, id := range chat.ParticipantsIDs {
		resources.Participants[id] = list.Resources.Participants[id]
	}
	raw, err := json.Marshal(struct {
		Chat      ChatListItem
		Resources any
	}{chat, resources})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func hhMetadataUnchanged(ctx context.Context, chat ChatListItem, list *ChatsResponse) bool {
	known, _ := ctx.Value(hhMetadataContextKey{}).(map[string]EmployerConversation)
	old, ok := known[strconv.FormatInt(chat.Id, 10)]
	if !ok {
		return false
	}
	fingerprint := hhListFingerprint(chat, list)
	if fingerprint == "" || old.HHMetadata["sync_list_fingerprint"] != fingerprint || historyStatus(old) != "VALID" {
		return false
	}
	// Periodic full history refresh also bounds stale display in the event of
	// upstream historical edits that are absent from lastMessage/list metadata.
	at, err := time.Parse(time.RFC3339Nano, old.HHMetadata["sync_detail_at"])
	if err != nil || time.Since(at) > 15*time.Minute || time.Since(at) < 0 {
		return false
	}
	for _, m := range old.Messages {
		if m.ExternalID == strconv.FormatInt(chat.LastMessage.ID, 10) {
			return true
		}
	}
	return false
}
func reportHHReadProgress(ctx context.Context, total, done int) {
	if fn, ok := ctx.Value(hhProgressContextKey{}).(func(int, int)); ok {
		fn(total, done)
	}
}
