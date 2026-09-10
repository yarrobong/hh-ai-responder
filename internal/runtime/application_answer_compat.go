package runtime

import (
	stdcontext "context"

	applicationanswer "hh-ai-responder/internal/usecase/applicationanswer"
)

func applicationAnswerContext(ai StructuredAIClient) stdcontext.Context {
	// The legacy facade has no context parameter. The live AI client already
	// owns the caller lifecycle; test/compatibility clients retain the historic
	// detached context behavior until that facade is migrated.
	if client, ok := any(ai).(*AIClient); ok && client != nil && client.ctx != nil {
		return client.ctx
	}
	return stdcontext.Background()
}

func applicationAnswerInput(value ApplicationContext, question string, stories []CandidateStory) applicationanswer.Input {
	return applicationanswer.Input{
		Application:        value.Application,
		MatchResult:        value.MatchResult,
		VacancyDescription: value.Conversation.VacancyDescription,
		CandidateContext:   value.CandidateContext,
		Question:           question,
		Stories:            append([]CandidateStory{}, stories...),
		RelevantExamples:   applicationAnswerExamples(value.RelevantExamples),
		RelevantKnowledge:  applicationAnswerKnowledge(value.RelevantKnowledge),
	}
}

func applicationAnswerExamples(values []SafeSemanticSelection) []applicationanswer.SafeSemanticSelection {
	result := make([]applicationanswer.SafeSemanticSelection, 0, len(values))
	for _, value := range values {
		result = append(result, applicationanswer.SafeSemanticSelection{EntityType: value.EntityType, EntityID: value.EntityID, Score: value.Score, Title: value.Title, Text: value.Text, ContentHash: value.ContentHash, EmbeddingModel: value.EmbeddingModel, EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, value.EvidenceRefs...)})
	}
	return result
}

func applicationAnswerKnowledge(value RelevantKnowledgeSnapshot) applicationanswer.RelevantKnowledgeSnapshot {
	result := applicationanswer.RelevantKnowledgeSnapshot{ForbiddenClaims: append([]string{}, value.ForbiddenClaims...)}
	for _, fact := range value.Facts {
		converted := applicationanswer.RelevantKnowledgeFact{Key: fact.Key, NormalizedValue: fact.NormalizedValue, TruthStatus: fact.TruthStatus}
		for _, provenance := range fact.Provenance {
			converted.Provenance = append(converted.Provenance, applicationanswer.RelevantKnowledgeProvenance{Source: provenance.Source, Reference: provenance.Reference, Evidence: append([]string{}, provenance.Evidence...)})
		}
		result.Facts = append(result.Facts, converted)
	}
	for _, selection := range value.SemanticSelections {
		result.SemanticSelections = append(result.SemanticSelections, applicationanswer.RelevantSemanticKnowledge{EntityType: selection.EntityType, EntityID: selection.EntityID, Score: selection.Score, Title: selection.Title, Excerpt: selection.Excerpt, EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, selection.EvidenceRefs...), ContentHash: selection.ContentHash, Model: selection.Model})
	}
	return result
}

// These wrappers preserve existing root-package tests/callers while the
// application-answer algorithms live in the importable use case.
func marshalApplicationContext(value ApplicationContext, question string, stories []CandidateStory) string {
	return applicationanswer.MarshalInput(applicationAnswerInput(value, question, stories))
}

func validateStoryClaims(draft string, context CandidateContext, stories []CandidateStory, application JobApplication, description string) error {
	return applicationanswer.ValidateStoryClaims(draft, applicationanswer.Input{Application: application, CandidateContext: context, Stories: stories, VacancyDescription: description})
}
