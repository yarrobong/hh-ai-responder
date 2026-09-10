package applicationanswer

import (
	"encoding/json"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/employerreply"
	"hh-ai-responder/internal/vacancy"
)

// MarshalInput preserves the application-answer context field order and names
// used by the previous root workflow. The supplied question is serialized
// verbatim; it is data, not a conversation message or a test instruction.
func MarshalInput(input Input) string {
	selected := selectRelevantStories(input.Stories, input.Application, input.VacancyDescription)
	value := struct {
		Application        application.JobApplication        `json:"application"`
		Match              vacancy.MatchResult               `json:"match_result"`
		VacancyDescription string                            `json:"vacancy_description,omitempty"`
		Candidate          candidatecontext.CandidateContext `json:"candidate_context"`
		Question           string                            `json:"question,omitempty"`
		Stories            []candidate.CandidateStory        `json:"stories,omitempty"`
		VerifiedFacts      candidatecontext.CandidateContext `json:"verified_candidate_facts"`
		Examples           []SafeSemanticSelection           `json:"relevant_real_examples,omitempty"`
		Knowledge          RelevantKnowledgeSnapshot         `json:"relevant_knowledge_snapshot,omitempty"`
	}{input.Application, input.MatchResult, input.VacancyDescription, input.CandidateContext, input.Question, selected, input.CandidateContext, input.RelevantExamples, input.RelevantKnowledge}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// BuildPrompt returns the exact application-answer system prompt and user
// context request used by the extracted workflow.
func BuildPrompt(input Input, extraPrompt string) (string, string) {
	return employerreply.SystemPrompt("ответ на вопрос вакансии", extraPrompt), MarshalInput(input)
}

func completionRequest(input Input, extraPrompt, model string, maxTokens int, temperature float64) llm.CompletionRequest {
	system, user := BuildPrompt(input, extraPrompt)
	return llm.CompletionRequest{Model: model, Messages: []llm.Message{{Role: llm.RoleSystem, Content: system}, {Role: llm.RoleUser, Content: user}}, MaxTokens: maxTokens, Temperature: temperature, ResponseFormat: employerreply.ResponseFormat()}
}
