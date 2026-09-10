package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type acquisitionFixtureExtractor struct {
	interpretation CandidateKnowledgeInterpretation
}

func (f acquisitionFixtureExtractor) InterpretCandidateAnswer(CandidateKnowledgeGap, string, Candidate) (CandidateKnowledgeInterpretation, error) {
	return f.interpretation, nil
}

func acquisitionCandidate(t *testing.T, kb *CandidateKnowledgeBase) Candidate {
	t.Helper()
	candidate, _, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: kb.Profile, Knowledge: *kb, CandidateID: "candidate-local"})
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestCandidateKnowledgeGapIsTypedAndDeduplicated(t *testing.T) {
	kb := knowledgeTestBase(t)
	candidate := acquisitionCandidate(t, kb)
	gaps, err := DetectCandidateKnowledgeGaps(candidate, "Работали ли вы с Redis?")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].SubjectType != KnowledgeSubjectSkill || gaps[0].Field != "usage_context" || gaps[0].DeterministicKey == "" {
		t.Fatalf("unexpected Redis gap: %+v", gaps)
	}
	again, err := DetectCandidateKnowledgeGaps(candidate, "Работали ли вы с Redis?")
	if err != nil || len(again) != 1 || again[0].DeterministicKey != gaps[0].DeterministicKey {
		t.Fatalf("gap key was not deterministic: %+v %v", again, err)
	}
	if got, err := DetectCandidateKnowledgeGaps(candidate, "Когда сможете выйти на работу?"); err != nil || len(got) != 0 {
		t.Fatalf("operational question became a knowledge gap: %+v %v", got, err)
	}
}

func TestCandidateKnowledgeAcquisitionJSONLifecycle(t *testing.T) {
	kb := knowledgeTestBase(t)
	if err := kb.Save(); err != nil {
		t.Fatal(err)
	}
	clarifications := NewCandidateClarificationStore(filepath.Join(t.TempDir(), "clarifications.json"))
	resolver := NewCandidateContextResolver(kb)
	mutation := NewCandidateMutationService(storageBackendJSON, nil, nil, kb.ProfilePath)
	extractor := acquisitionFixtureExtractor{interpretation: CandidateKnowledgeInterpretation{Proposals: []CandidateKnowledgeProposalDraft{{Type: "skill_usage", Skill: "Redis", UsageContext: CanonicalSkillUsagePetProject, Level: SkillLevelUnknown, TruthStatus: string(TruthStatusHypothesis)}}}}
	service := NewCandidateKnowledgeAcquisitionService(resolver, mutation, clarifications, extractor)
	candidate := acquisitionCandidate(t, kb)
	refs := CandidateKnowledgeGapContext{ConversationID: "conversation-1", ApplicationID: "application-1", VacancyID: "42", EmployerMessageID: "message-1"}
	first, err := service.CreateClarification(candidate, "Работали ли вы с Redis?", refs)
	if err != nil || len(first) != 1 {
		t.Fatalf("create clarification: %+v %v", first, err)
	}
	second, err := service.CreateClarification(candidate, "Работали ли вы с Redis?", refs)
	if err != nil || len(second) != 1 {
		t.Fatalf("deduplicate clarification: %+v %v", second, err)
	}
	values, err := clarifications.List()
	if err != nil || len(values) != 1 || values[0].UnknownID == "" || values[0].GapKey == "" || values[0].OriginalEmployerQuestion == "" {
		t.Fatalf("structured clarification missing provenance: %+v %v", values, err)
	}
	answer, err := service.SubmitAnswer(context.Background(), values[0].ID, CandidateAnswer{Kind: "free_text", Raw: "Использовал Redis в pet-проекте для кэша"})
	if err != nil || answer.Disposition != "proposal_pending_confirmation" || len(answer.ProposalIDs) != 1 {
		t.Fatalf("free-text answer bypassed proposal stage: %+v %v", answer, err)
	}
	if err := service.ConfirmProposal(context.Background(), answer.ProposalIDs[0]); err != nil {
		t.Fatal(err)
	}
	if err := kb.Load(); err != nil {
		t.Fatal(err)
	}
	if len(kb.Skills) != 1 || kb.Skills[0].TruthStatus != TruthStatusConfirmed || len(kb.Skills[0].Uses) != 1 || kb.Skills[0].Uses[0].Context != CanonicalSkillUsagePetProject {
		t.Fatalf("confirmed usage was not stored canonically: %+v", kb.Skills)
	}
	if len(kb.Unknowns) != 1 || kb.Unknowns[0].Status != CandidateUnknownConfirmed {
		t.Fatalf("unknown was not resolved: %+v", kb.Unknowns)
	}
	resolved, err := clarifications.Get(values[0].ID)
	if err != nil || resolved.Status != ClarificationResolvedExistingKnowledge || !resolved.ReadyForRegeneration || resolved.Answer == nil || resolved.Answer.Raw == "" {
		t.Fatalf("clarification provenance/resolution missing: %+v %v", resolved, err)
	}
}

func TestCandidateKnowledgeAcquisitionStructuredChoiceJSON(t *testing.T) {
	kb := knowledgeTestBase(t)
	clarifications := NewCandidateClarificationStore(filepath.Join(t.TempDir(), "clarifications.json"))
	updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser})
	service := NewCandidateKnowledgeAcquisitionService(NewCandidateContextResolver(kb), updater, clarifications, nil)
	request, err := service.CreateClarification(acquisitionCandidate(t, kb), "Работали ли вы с Redis?", CandidateKnowledgeGapContext{ConversationID: "conversation-choice", EmployerMessageID: "message-choice"})
	if err != nil || len(request) != 1 {
		t.Fatalf("create choice clarification: %+v %v", request, err)
	}
	answer, err := service.SubmitAnswer(context.Background(), request[0].ID, CandidateAnswer{Kind: "choice", ChoiceID: "commercial", Raw: "Да, коммерчески"})
	if err != nil || answer.Disposition != "confirmed_choice" {
		t.Fatalf("structured choice was not applied: %+v %v", answer, err)
	}
	if len(kb.Skills) != 1 || len(kb.Skills[0].Uses) != 1 || kb.Skills[0].Uses[0].Context != CanonicalSkillUsageCommercial || kb.Skills[0].TruthStatus != TruthStatusConfirmed {
		t.Fatalf("structured choice was not canonicalized: %+v", kb.Skills)
	}
	if len(kb.Unknowns) != 1 || kb.Unknowns[0].Status != CandidateUnknownConfirmed {
		t.Fatalf("structured choice did not resolve unknown: %+v", kb.Unknowns)
	}
	if len(kb.Events) == 0 || kb.Events[len(kb.Events)-1].ConversationID != "conversation-choice" || kb.Events[len(kb.Events)-1].EmployerMessageID != "message-choice" {
		t.Fatalf("acquisition provenance was not retained in events: %+v", kb.Events)
	}
}

func TestCandidateKnowledgeAcquisitionDismissDoesNotCreateNegativeFact(t *testing.T) {
	kb := knowledgeTestBase(t)
	clarifications := NewCandidateClarificationStore(filepath.Join(t.TempDir(), "clarifications.json"))
	updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser})
	service := NewCandidateKnowledgeAcquisitionService(NewCandidateContextResolver(kb), updater, clarifications, nil)
	requests, err := service.CreateClarification(acquisitionCandidate(t, kb), "Работали ли вы с Redis?", CandidateKnowledgeGapContext{})
	if err != nil || len(requests) != 1 {
		t.Fatalf("create dismissal clarification: %+v %v", requests, err)
	}
	result, err := service.SubmitAnswer(context.Background(), requests[0].ID, CandidateAnswer{Kind: "choice", ChoiceID: "dismissed", Raw: "Не хочу отвечать / не сохранять"})
	if err != nil || result.Disposition != "dismissed" {
		t.Fatalf("dismissal was not applied: %+v %v", result, err)
	}
	if len(kb.Skills) != 0 || len(kb.Unknowns) != 1 || kb.Unknowns[0].Status != CandidateUnknownDismissed || kb.Unknowns[0].TruthStatus != TruthStatusUnknown {
		t.Fatalf("dismissal changed candidate facts: %+v", kb)
	}
}

func TestCandidateKnowledgeAcquisitionRejectsAIConfirmationAndPreservesUnknown(t *testing.T) {
	kb := knowledgeTestBase(t)
	candidate := acquisitionCandidate(t, kb)
	gap := CandidateKnowledgeGap{CandidateID: candidate.ID, SubjectType: KnowledgeSubjectSkill, Subject: "Redis", Field: "usage_context"}
	_, err := decodeCandidateKnowledgeInterpretation(`{"proposals":[{"type":"skill_usage","skill":"Redis","usage_context":"commercial","level":"working","truth_status":"confirmed"}]}`, gap, candidate)
	if err == nil || err.Error() != "AI cannot confirm candidate knowledge" {
		t.Fatalf("AI confirmation was accepted: %v", err)
	}
	if len(kb.Skills) != 0 || len(kb.Proposals) != 0 {
		t.Fatal("failed AI interpretation changed candidate knowledge")
	}
	if got, err := decodeCandidateKnowledgeInterpretation(`{"proposals":[{"type":"skill_usage","skill":"MongoDB","usage_context":"pet_project","level":"unknown","truth_status":"hypothesis"}]}`, gap, candidate); err == nil || !errors.Is(err, ErrKnowledgeAnswerMismatch) {
		t.Fatalf("mismatched answer was accepted: %+v %v", got, err)
	}
	if _, err := json.Marshal(candidate); err != nil {
		t.Fatal(err)
	}
}
