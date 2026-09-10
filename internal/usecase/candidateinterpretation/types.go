package candidateinterpretation

import (
	"time"

	llmport "hh-ai-responder/internal/ports/llm"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

// Interpretation is the untrusted DTO consumed by deterministic Candidate
// acquisition policy. The alias keeps the neutral proposal vocabulary owned
// by candidateacquisition, which prevents a package cycle.
type Interpretation = candidateacquisition.CandidateKnowledgeInterpretation

// Input contains only the current gap, raw answer, and the identifiers needed
// to validate references in a story proposal. Known context is validation-only;
// it is not copied into the completion prompt.
type Input struct {
	Gap          candidateacquisition.CandidateKnowledgeGap
	Answer       string
	KnownContext KnownContext
}

type KnownContext struct {
	ExperienceIDs []string
	ProjectIDs    []string
}

type Dependencies struct {
	Completion llmport.CompletionProvider
}

type Options struct {
	Model              string
	Attempts           int
	MaxTokens          int
	Temperature        float64
	SemanticRetryDelay time.Duration
}
