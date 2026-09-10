package testanswer

import (
	"errors"
	"time"

	llmport "hh-ai-responder/internal/ports/llm"
)

var (
	ErrProviderNotConfigured = errors.New("test answer completion provider is not configured")
	ErrNilContext            = errors.New("test answer context is nil")
)

// Task is the normalized, read-only test question supplied by the HH workflow.
// Its JSON tags intentionally preserve the existing HH task snapshot shape.
type Task struct {
	ID                 int      `json:"id"`
	Description        string   `json:"description"`
	Multiple           string   `json:"multiple"`
	Open               string   `json:"open"`
	CandidateSolutions []Option `json:"candidateSolutions"`
}

// Option is a choice belonging to one exact Task. The ID is an external
// identity and is never generated or remapped by the model.
type Option struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Title string `json:"title"`
	Value string `json:"value"`
}

// Input contains only the normalized values needed to construct the prompt
// and validate the model result. It contains no HH transport or write state.
type Input struct {
	Tasks       []Task
	Contacts    string
	GitHubURL   string
	ExtraPrompt string
}

// Response is the strict structured response shape returned by the model.
type Response struct {
	Solutions []AIAnswer `json:"solutions"`
}

// AIAnswer is an untrusted model answer before deterministic validation.
type AIAnswer struct {
	TaskID              int    `json:"task_id"`
	SolutionID          *int   `json:"solution_id,omitempty"`
	TextSolution        string `json:"text_solution,omitempty"`
	SolutionIDPresent   bool   `json:"-"`
	TextSolutionPresent bool   `json:"-"`
}

// ProposedAnswer is a structurally validated answer in source task order.
// It is a proposal only; it contains no submit, approval, preflight, or HH
// write capability.
type ProposedAnswer struct {
	TaskID       int
	SolutionID   int
	TextSolution string
	HasChoice    bool
}

// Result contains proposed answers validated against the supplied snapshot.
// Structural validation does not prove factual correctness.
type Result struct {
	Answers []ProposedAnswer
}

type Dependencies struct {
	Completion llmport.CompletionProvider
}

type Options struct {
	Model              string
	Attempts           int
	Temperature        float64
	SemanticRetryDelay time.Duration
}
