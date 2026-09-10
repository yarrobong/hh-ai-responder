package followupdraft

import (
	"encoding/json"
	"time"

	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

const Task = "Короткий естественный follow-up без давления: 1–3 предложения, учитывай диалог, не повторяй знакомство или сопроводительное письмо. Только черновик, без новых обещаний и договорённостей."

// SystemPrompt preserves the established employer-safe instructions while
// making the follow-up task string owned by this workflow.
func SystemPrompt(extra string) string {
	return employerreply.SystemPrompt(Task, extra)
}

// MarshalInput preserves the established follow-up payload shape and field
// ordering. Conversation material remains user data, never system guidance.
func MarshalInput(value Input) string {
	raw, _ := json.Marshal(struct {
		Context  json.RawMessage `json:"conversation"`
		FollowUp Eligibility     `json:"follow_up"`
		History  []time.Time     `json:"confirmed_follow_up_history"`
	}{json.RawMessage(employerreply.MarshalContext(value.Context)), value.Eligibility, value.ConfirmedFollowUps})
	return string(raw)
}
