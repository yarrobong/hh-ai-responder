package employerreply

import llmvalue "hh-ai-responder/internal/llm"

const responseSchemaJSON = `{"type":"object","additionalProperties":false,"required":["action","draft","reply_requirement","reason","confidence","used_facts","missing_information","forbidden_claims_checked","conversation_topics_used","warnings"],"properties":{"action":{"type":"string","enum":["draft_reply","need_candidate_input","no_reply_needed","courtesy_reply_optional","manual_review"]},"reply_requirement":{"type":"string","enum":["REPLY_REQUIRED","REPLY_OPTIONAL","NO_REPLY_NEEDED"]},"draft":{"type":"string"},"reason":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1},"used_facts":{"type":"array","items":{"type":"string"}},"missing_information":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["topic","question"],"properties":{"topic":{"type":"string"},"question":{"type":"string"}}}},"forbidden_claims_checked":{"type":"boolean"},"conversation_topics_used":{"type":"array","items":{"type":"string"}},"warnings":{"type":"array","items":{"type":"string"}}}}`

// ResponseFormat returns the exact structured-output contract used by the
// employer-reply flow. Business parsing remains below the provider boundary.
func ResponseFormat() *llmvalue.ResponseFormat {
	return &llmvalue.ResponseFormat{Type: "json_schema", JSONSchema: &llmvalue.JSONSchema{Name: "ai_response_decision", Schema: []byte(responseSchemaJSON), Strict: true}}
}
