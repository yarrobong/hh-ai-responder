package employerreply

import (
	"encoding/json"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/semantic"
	candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

type Action string

const (
	ActionDraftReply    Action = "draft_reply"
	ActionNeedCandidate Action = "need_candidate_input"
	ActionNoReplyNeeded Action = "no_reply_needed"
	ActionCourtesyReply Action = "courtesy_reply_optional"
	ActionManualReview  Action = "manual_review"
)

type MissingInformation struct {
	Topic    string `json:"topic"`
	Question string `json:"question"`
}

type KnowledgeRequestStatus string

const (
	KnowledgeRequestPending  KnowledgeRequestStatus = "pending"
	KnowledgeRequestResolved KnowledgeRequestStatus = "resolved"
)

// KnowledgeRequest is a reviewable request for candidate input. It is not a
// Candidate Knowledge mutation and carries no send capability.
type KnowledgeRequest struct {
	Topic             string                 `json:"topic"`
	Question          string                 `json:"question"`
	Source            string                 `json:"source"`
	Status            KnowledgeRequestStatus `json:"status"`
	DeterministicKey  string                 `json:"deterministic_key,omitempty"`
	UnknownID         string                 `json:"unknown_id,omitempty"`
	ClarificationID   string                 `json:"clarification_id,omitempty"`
	ConversationID    string                 `json:"conversation_id,omitempty"`
	ApplicationID     string                 `json:"application_id,omitempty"`
	VacancyID         int                    `json:"vacancy_id,omitempty"`
	EmployerMessageID string                 `json:"employer_message_id,omitempty"`
	MissingPart       string                 `json:"missing_part,omitempty"`
}

// Decision is the employer-reply business result. It is intentionally not a
// send request and contains no approval, nonce, delivery, or HH capability.
type Decision struct {
	Action                 Action                              `json:"action"`
	ReplyRequirement       conversationpolicy.ReplyRequirement `json:"reply_requirement,omitempty"`
	Draft                  string                              `json:"draft,omitempty"`
	Reason                 string                              `json:"reason"`
	Confidence             float64                             `json:"confidence"`
	UsedFacts              []string                            `json:"used_facts"`
	MissingInformation     []MissingInformation                `json:"missing_information"`
	KnowledgeRequests      []KnowledgeRequest                  `json:"knowledge_requests,omitempty"`
	ForbiddenClaimsChecked bool                                `json:"forbidden_claims_checked"`
	ConversationTopicsUsed []string                            `json:"conversation_topics_used"`
	Warnings               []string                            `json:"warnings"`
}

// VacancyContext is the intentionally small vacancy projection used in the
// employer-reply prompt. It is not a vacancy reader or a preflight value.
type VacancyContext struct {
	VacancyID   int    `json:"vacancy_id"`
	CompanyName string `json:"company_name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type Clarification struct {
	Question  string `json:"question"`
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
	Source    string `json:"source"`
}

type ReplyGuidance struct {
	Mode                   string   `json:"mode"`
	AvoidReintroduction    bool     `json:"avoid_reintroduction"`
	AlreadyDiscussedTopics []string `json:"already_discussed_topics"`
	MentionedProjects      []string `json:"mentioned_projects"`
}

// SemanticHint is a relevance-only projection. Its score and text are never
// treated as Candidate evidence by this package.
type SemanticHint struct {
	EntityType     semantic.CandidateSemanticEntityType    `json:"entity_type"`
	EntityID       string                                  `json:"entity_id"`
	Score          float64                                 `json:"score"`
	Title          string                                  `json:"title"`
	Text           string                                  `json:"text"`
	ContentHash    string                                  `json:"content_hash"`
	EmbeddingModel string                                  `json:"embedding_model"`
	EvidenceRefs   []semantic.CandidateSemanticEvidenceRef `json:"evidence_refs,omitempty"`
}

// Context is assembled by the higher-level conversation workflow. Conversation
// is retained for deterministic policy checks and is excluded from the prompt;
// all prompt fields below are detached projections.
type Context struct {
	Conversation        conversation.EmployerConversation       `json:"-"`
	ConversationID      string                                  `json:"conversation_id"`
	Status              conversation.Status                     `json:"status"`
	NextAction          string                                  `json:"next_action"`
	Vacancy             VacancyContext                          `json:"vacancy"`
	RecentMessages      []conversation.Message                  `json:"recent_messages"`
	ConversationSummary conversation.Summary                    `json:"conversation_summary"`
	CandidateContext    candidatecontext.CandidateContext       `json:"candidate_context"`
	UnresolvedQuestions []Clarification                         `json:"pending_questions"`
	ForbiddenClaims     []string                                `json:"forbidden_claims"`
	ConsistencyWarnings []conversationpolicy.ConsistencyWarning `json:"consistency_warnings"`
	ReplyGuidance       ReplyGuidance                           `json:"reply_guidance"`
	ReplyRequirement    conversationpolicy.ReplyRequirement     `json:"reply_requirement"`
	HistoryTrust        string                                  `json:"history_trust"`
	RelevantExamples    []SemanticHint                          `json:"relevant_real_examples,omitempty"`
	RelevantKnowledge   json.RawMessage                         `json:"relevant_knowledge_snapshot,omitempty"`
}

type Input struct {
	Context Context
	Task    string
}
