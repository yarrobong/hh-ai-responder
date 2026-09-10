package applicationanswer

import (
	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/semantic"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/employerreply"
	"hh-ai-responder/internal/vacancy"
)

// RelevantKnowledgeSnapshot is the already-selected, query-scoped knowledge
// projection supplied by the higher workflow. Semantic relevance is kept
// separate from Candidate evidence.
type RelevantKnowledgeSnapshot struct {
	Facts              []RelevantKnowledgeFact     `json:"facts"`
	ForbiddenClaims    []string                    `json:"forbidden_claims,omitempty"`
	SemanticSelections []RelevantSemanticKnowledge `json:"semantic_selections,omitempty"`
}

type RelevantKnowledgeFact struct {
	Key             string                        `json:"key"`
	NormalizedValue string                        `json:"normalized_value"`
	TruthStatus     candidate.TruthStatus         `json:"truth_status"`
	Provenance      []RelevantKnowledgeProvenance `json:"provenance,omitempty"`
}

type RelevantKnowledgeProvenance struct {
	Source    string   `json:"source"`
	Reference string   `json:"reference,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

type RelevantSemanticKnowledge struct {
	EntityType   semantic.CandidateSemanticEntityType    `json:"entity_type"`
	EntityID     string                                  `json:"entity_id"`
	Score        float64                                 `json:"score"`
	Title        string                                  `json:"title"`
	Excerpt      string                                  `json:"excerpt,omitempty"`
	EvidenceRefs []semantic.CandidateSemanticEvidenceRef `json:"evidence_refs,omitempty"`
	ContentHash  string                                  `json:"content_hash"`
	Model        string                                  `json:"embedding_model"`
}

// SafeSemanticSelection is prompt data rebuilt and validated by the higher
// workflow. Its score is relevance only and never Candidate evidence.
type SafeSemanticSelection struct {
	EntityType     semantic.CandidateSemanticEntityType    `json:"entity_type"`
	EntityID       string                                  `json:"entity_id"`
	Score          float64                                 `json:"score"`
	Title          string                                  `json:"title"`
	Text           string                                  `json:"text"`
	ContentHash    string                                  `json:"content_hash"`
	EmbeddingModel string                                  `json:"embedding_model"`
	EvidenceRefs   []semantic.CandidateSemanticEvidenceRef `json:"evidence_refs,omitempty"`
}

// Input is a detached application-answer snapshot. It contains no reader,
// writer, HH DTO, or Candidate mutation capability.
type Input struct {
	Application        application.JobApplication
	MatchResult        vacancy.MatchResult
	VacancyDescription string
	CandidateContext   candidatecontext.CandidateContext
	Question           string
	Stories            []candidate.CandidateStory
	RelevantExamples   []SafeSemanticSelection
	RelevantKnowledge  RelevantKnowledgeSnapshot
}

type Outcome string

const (
	OutcomeDraft         Outcome = "draft"
	OutcomeNeedCandidate Outcome = "need_candidate_input"
	OutcomeManualReview  Outcome = "manual_review"
	OutcomeNoReply       Outcome = "no_reply_needed"
	OutcomeCourtesyReply Outcome = "courtesy_reply_optional"
)

// Result is a proposed local result. The shared Decision carries the exact
// provider-level action/value representation used by employerreply.
type Result struct {
	Decision employerreply.Decision
	Outcome  Outcome
}
