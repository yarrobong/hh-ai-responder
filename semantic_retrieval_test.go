package main

import (
	"context"
	"strings"
	"testing"
)

type recordingSemanticRetriever struct {
	results  []CandidateSemanticResult
	requests []SemanticRetrievalRequest
	err      error
}

func (r *recordingSemanticRetriever) Retrieve(_ context.Context, request SemanticRetrievalRequest) ([]CandidateSemanticResult, error) {
	r.requests = append(r.requests, request)
	return append([]CandidateSemanticResult{}, r.results...), r.err
}

func semanticSafeCandidate() Candidate {
	metadata := semanticTestMetadata(TruthStatusConfirmed)
	project := CanonicalCandidateProject{
		ID: "project-automation", Name: "Automation project", Role: "Integrator",
		Description: "Automated routine API work", Technologies: []string{"Python"},
		Tasks: []string{"remove manual processing"}, Results: []string{"fewer manual steps"},
		Metadata: metadata, DetailedSource: &CandidateProject{ID: "project-automation", Name: "Automation project", Description: "Automated routine API work", Technologies: []string{"Python"}, KnowledgeMetadata: metadata},
	}
	return Candidate{
		ID: "candidate-semantic", Version: 1,
		Skills:       []CanonicalCandidateSkill{{ID: "python", Name: "Python", DisplayName: "Python", Level: SkillLevelWorking, State: CanonicalClaimActive, Metadata: metadata}},
		Projects:     []CanonicalCandidateProject{project},
		Stories:      []CanonicalCandidateStory{{ID: "story-automation", Title: "Automated routine work", Action: "Automated routine API work", Result: "fewer manual steps", Technologies: []string{"Python"}, ProfileRefs: []string{project.ID}}},
		Achievements: []CandidateAchievement{{ID: "achievement-automation", Title: "Automation result", Problem: "manual processing", Actions: []string{"mapped the process"}, Result: []string{"fewer manual steps"}, Technologies: []string{"Python"}, ProjectID: project.ID, KnowledgeMetadata: metadata}},
	}
}

func semanticResultFor(t *testing.T, candidate Candidate, entityType CandidateSemanticEntityType, entityID string, score float64) CandidateSemanticResult {
	t.Helper()
	drafts, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, draft := range drafts {
		if draft.EntityType == entityType && draft.EntityID == entityID {
			return CandidateSemanticResult{EntityType: entityType, EntityID: entityID, Score: score, Title: draft.Title, ContentHash: draft.ContentHash, Model: "fake-v1", EvidenceRefs: draft.EvidenceRefs}
		}
	}
	t.Fatalf("semantic draft %s/%s not found", entityType, entityID)
	return CandidateSemanticResult{}
}

func TestSemanticRoutingIsConservative(t *testing.T) {
	if ShouldUseCandidateSemanticRetrieval("Здравствуйте, спасибо", CandidateContext{}) {
		t.Fatal("greeting routed to semantic retrieval")
	}
	if ShouldUseCandidateSemanticRetrieval("Какая зарплата и когда готовы выйти?", CandidateContext{}) {
		t.Fatal("operational question routed to semantic retrieval")
	}
	if ShouldUseCandidateSemanticRetrieval("Работали ли вы с Redis?", CandidateContext{}) {
		t.Fatal("direct technical fact question routed to narrative retrieval")
	}
	if !ShouldUseCandidateSemanticRetrieval("Расскажите про пример автоматизации", CandidateContext{}) {
		t.Fatal("experience example was not routed")
	}
	if ShouldUseCandidateSemanticRetrieval("А с этим работали?", CandidateContext{}) {
		t.Fatal("ambiguous follow-up routed without a known topic")
	}
	if !ShouldUseCandidateSemanticRetrieval("А с этим работали?", CandidateContext{RelevantSkills: []string{"Redis"}}) {
		t.Fatal("known-topic follow-up was not routed")
	}
}

func TestStructuredTruthRemainsAuthoritativeOverSemanticNarrative(t *testing.T) {
	kb := contextTestKnowledge()
	kb.Skills = append(kb.Skills, CandidateSkillDetailed{ID: "redis-negative", Name: "Redis", Level: SkillLevelWorking, Negative: true, KnowledgeMetadata: semanticTestMetadata(TruthStatusConfirmed)})
	resolved, err := NewCandidateContextResolver(kb).ResolveForEmployerMessage("Работали ли вы с Redis?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.RelevantSkills) != 0 || len(resolved.ForbiddenClaims) == 0 || !contextHasName(resolved.AllowedFacts, "Подтверждено отсутствие навыка: Redis") {
		t.Fatalf("confirmed negative was not preserved: %+v", resolved)
	}

	disputed := semanticSafeCandidate()
	disputed.Skills[0].State = CanonicalClaimDisputed
	if _, err := CanonicalEmployerSafeProjection(disputed); err == nil {
		t.Fatal("disputed canonical skill was allowed into employer context")
	}
}

func TestConversationContextUsesOneSemanticQueryOnlyForNarrativeQuestion(t *testing.T) {
	candidate := semanticSafeCandidate()
	result := semanticResultFor(t, candidate, CandidateSemanticEntityStory, "story-automation", .94)
	retriever := &recordingSemanticRetriever{results: []CandidateSemanticResult{result}}
	store, conversation := conversationTestStore(t)
	conversationTestAppend(t, store, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Расскажите про опыт автоматизации", 1))
	resolver := NewCandidateContextResolverFromCandidate(candidate)
	builder := NewConversationContextBuilder(store, resolver, retriever)
	got, err := builder.BuildForReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(retriever.requests) != 1 || len(got.RelevantExamples) != 1 {
		t.Fatalf("unexpected semantic routing: requests=%+v examples=%+v", retriever.requests, got.RelevantExamples)
	}
	if retriever.requests[0].Purpose != SemanticRetrievalPurposeEmployerReply || len([]rune(retriever.requests[0].Query)) > semanticQueryMaxRunes {
		t.Fatalf("bad employer semantic request: %+v", retriever.requests[0])
	}

	store2, conversation2 := conversationTestStore(t)
	conversationTestAppend(t, store2, conversation2.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Какая зарплата?", 1))
	second := &recordingSemanticRetriever{results: []CandidateSemanticResult{result}}
	if _, err := NewConversationContextBuilder(store2, resolver, second).BuildForReply(conversation2.ID); err != nil {
		t.Fatal(err)
	}
	if len(second.requests) != 0 {
		t.Fatalf("salary question called semantic provider: %+v", second.requests)
	}
}

func TestSemanticFailureLeavesStructuredConversationContextUsable(t *testing.T) {
	store, conversation := conversationTestStore(t)
	conversationTestAppend(t, store, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Расскажите про опыт автоматизации", 1))
	retriever := &recordingSemanticRetriever{err: ErrSemanticUnavailable}
	got, err := NewConversationContextBuilder(store, NewCandidateContextResolver(contextTestKnowledge()), retriever).BuildForReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CandidateContext.MessageIntent == "" || len(got.RelevantExamples) != 0 {
		t.Fatalf("structured context was not produced after semantic failure: %+v", got)
	}
}

func TestBuildSafeSemanticContextRebuildsCanonicalDataAndDeduplicates(t *testing.T) {
	candidate := semanticSafeCandidate()
	story := semanticResultFor(t, candidate, CandidateSemanticEntityStory, "story-automation", .92)
	project := semanticResultFor(t, candidate, CandidateSemanticEntityProject, "project-automation", .88)
	// The shared project reference makes these two representations one case.
	project.EvidenceRefs = append(project.EvidenceRefs, CandidateSemanticEvidenceRef{EntityType: string(CandidateSemanticEntityProject), EntityID: "project-automation"})
	selected := BuildSafeSemanticContext(candidate, []CandidateSemanticResult{story, project})
	if len(selected) != 1 {
		t.Fatalf("duplicate story/project was not removed: %+v", selected)
	}
	if selected[0].Text == "" || strings.Contains(selected[0].Text, "candidate_semantic_documents") {
		t.Fatalf("unsafe/indexed text leaked: %+v", selected[0])
	}

	stale := story
	stale.ContentHash = "old-hash"
	if got := BuildSafeSemanticContext(candidate, []CandidateSemanticResult{stale}); len(got) != 0 {
		t.Fatalf("stale result survived: %+v", got)
	}
}

func TestBuildSafeSemanticContextDoesNotPromoteUnknownTechnology(t *testing.T) {
	candidate := semanticSafeCandidate()
	candidate.Projects[0].Technologies = []string{"Kubernetes"}
	candidate.Projects[0].Description = "Kubernetes production deployment"
	candidate.Projects[0].DetailedSource.Technologies = []string{"Kubernetes"}
	result := semanticResultFor(t, candidate, CandidateSemanticEntityProject, "project-automation", .99)
	selected := BuildSafeSemanticContext(candidate, []CandidateSemanticResult{result})
	if len(selected) != 1 {
		t.Fatalf("safe project example should remain as narrative: %+v", selected)
	}
	if strings.Contains(strings.ToLower(selected[0].Text), "kubernetes") {
		t.Fatalf("unknown technology crossed into semantic context: %q", selected[0].Text)
	}
}

func TestCoverLetterSemanticContextKeepsRelevantExamplesBounded(t *testing.T) {
	candidate := semanticSafeCandidate()
	candidate.Achievements[0].ProjectID = ""
	candidate.Stories = append(candidate.Stories, CanonicalCandidateStory{ID: "story-unrelated", Title: "Unrelated hobby", Action: "photography", ProfileRefs: []string{"project-automation"}})
	results := []CandidateSemanticResult{
		semanticResultFor(t, candidate, CandidateSemanticEntityAchievement, "achievement-automation", .93),
		semanticResultFor(t, candidate, CandidateSemanticEntityProject, "project-automation", .87),
		semanticResultFor(t, candidate, CandidateSemanticEntityStory, "story-unrelated", .20),
	}
	selected := BuildSafeSemanticContext(candidate, results)
	if len(selected) != 2 || selected[0].EntityType != CandidateSemanticEntityAchievement || selected[1].EntityType != CandidateSemanticEntityProject {
		t.Fatalf("cover letter semantic selection was not relevant/bounded: %+v", selected)
	}
}

func TestSemanticSnapshotFingerprintIncludesSelections(t *testing.T) {
	base := RelevantKnowledgeSnapshot{Facts: []RelevantKnowledgeFact{{Key: "skill", NormalizedValue: "python", TruthStatus: TruthStatusConfirmed}}}
	with := base.AddSafeSemanticSelections([]SafeSemanticSelection{{EntityType: CandidateSemanticEntityStory, EntityID: "story-1", Score: .91, Title: "Automation", Text: "DATA", ContentHash: "hash", EmbeddingModel: "fake-v1"}})
	if RelevantKnowledgeHash(base) == RelevantKnowledgeHash(with) {
		t.Fatal("semantic selection did not change snapshot fingerprint")
	}
	without := with.AddSafeSemanticSelections(nil)
	if RelevantKnowledgeHash(without) == RelevantKnowledgeHash(with) {
		t.Fatal("removing semantic selection did not change snapshot fingerprint")
	}
}

func TestEmployerPromptSeparatesVerifiedFactsFromSemanticExamples(t *testing.T) {
	context := ConversationContext{
		CandidateContext: CandidateContext{AllowedFacts: []string{"Навык: Python; уровень: working"}},
		RelevantExamples: []SafeSemanticSelection{{EntityType: CandidateSemanticEntityStory, EntityID: "story-1", Title: "Automation", Text: "DATA example only", Score: .9, ContentHash: "hash", EmbeddingModel: "fake-v1"}},
	}
	prompt := marshalSafeContext(context)
	if !strings.Contains(prompt, "verified_candidate_facts") || !strings.Contains(prompt, "relevant_real_examples") || !strings.Contains(prompt, "DATA example only") {
		t.Fatalf("semantic examples were not separated in prompt: %s", prompt)
	}
}

func TestSemanticRetrievalRequestIsTypedAndQueryBounded(t *testing.T) {
	query := buildCoverLetterSemanticQuery("Junior Python Developer", strings.Repeat("raw vacancy ", 500), MatchResult{MatchedSkills: []string{"Python", "PostgreSQL"}})
	if len([]rune(query)) > semanticQueryMaxRunes {
		t.Fatalf("cover query is unbounded: %d", len([]rune(query)))
	}
	if len(query) >= len(strings.Repeat("raw vacancy ", 500)) {
		t.Fatal("raw vacancy dump entered compact query")
	}
}
