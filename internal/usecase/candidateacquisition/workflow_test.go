package candidateacquisition

import (
	"errors"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/candidatemutation"
)

func acquisitionCandidate() candidate.Candidate {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	meta := candidate.KnowledgeMetadata{TruthStatus: candidate.TruthStatusConfirmed, CreatedAt: now, UpdatedAt: now}
	return candidate.Candidate{
		Version: 1,
		ID:      "candidate-acquisition-test",
		Skills: []candidate.CanonicalCandidateSkill{
			{ID: "docker", Name: "Docker", DisplayName: "Docker", Level: candidate.SkillLevelWorking, Metadata: meta},
			{ID: "django", Name: "Django", DisplayName: "Django", Level: candidate.SkillLevelAdvanced, Metadata: meta},
		},
	}
}

func pendingRequest(topic string) CandidateClarificationRequest {
	return CandidateClarificationRequest{
		ID: "clarification-1", Topic: topic, Question: "Есть ли опыт с " + topic + "?", Reason: "synthetic",
		Status: ClarificationPending, CreatedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), GapKey: "gap-1", UnknownID: "unknown-1",
	}
}

func findGap(t *testing.T, value candidate.Candidate, question, subject string) CandidateKnowledgeGap {
	t.Helper()
	gaps, err := DetectCandidateKnowledgeGaps(value, question)
	if err != nil {
		t.Fatal(err)
	}
	for _, gap := range gaps {
		if gap.Subject == subject {
			return gap
		}
	}
	t.Fatalf("gaps=%+v, subject %q not found", gaps, subject)
	return CandidateKnowledgeGap{}
}

func TestDetectGapsDistinguishesPartialAndUnknownKnowledge(t *testing.T) {
	partial := findGap(t, acquisitionCandidate(), "Сколько лет Docker в production?", "Docker")
	if partial.Subject != "Docker" || partial.Field != "usage_context" || partial.DeterministicKey == "" {
		t.Fatalf("partial Docker gap=%+v", partial)
	}
	unknown := findGap(t, acquisitionCandidate(), "Работали ли вы с Kubernetes?", "Kubernetes")
	if unknown.Subject != "Kubernetes" || unknown.Field != "usage_context" {
		t.Fatalf("unknown Kubernetes gap=%+v", unknown)
	}
}

func TestKnownFactDoesNotCreateAcquisitionGap(t *testing.T) {
	gaps, err := DetectCandidateKnowledgeGaps(acquisitionCandidate(), "Есть ли опыт Django?")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("known Django produced gaps: %+v", gaps)
	}
}

func TestGapDedupAndResolvedUnknownFiltering(t *testing.T) {
	value := acquisitionCandidate()
	gap := findGap(t, value, "Работали ли вы с Kubernetes?", "Kubernetes")
	dupes := DeduplicateGaps([]CandidateKnowledgeGap{gap, gap})
	if len(dupes) != 1 {
		t.Fatalf("duplicate gap was not removed: %+v", dupes)
	}
	value.Unknowns = []candidate.CandidateUnknown{{ID: "unknown-1", RelatedEntity: "Kubernetes", GapKey: gap.DeterministicKey, Status: candidate.CandidateUnknownRejected}}
	if got := FilterKnownGaps(value, []CandidateKnowledgeGap{gap}); len(got) != 0 {
		t.Fatalf("resolved unknown was reopened: %+v", got)
	}
}

func TestExplicitChoiceProducesUserMutationIntentIncludingNegativeFact(t *testing.T) {
	request := pendingRequest("Kubernetes")
	decision, err := DecideAnswer(AnswerInput{Request: request, Answer: CandidateAnswer{Kind: "choice", ChoiceID: "explicitly_not_used", Raw: "Не использовал"}, Origin: OriginTrustedExplicitUser})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != AnswerConfirmsChoice || decision.Intent == nil || decision.Intent.Actor != candidatemutation.ActorUser || decision.Intent.Source != candidate.KnowledgeSourceUserConfirmed || !decision.Intent.Negative || decision.Intent.SkillUsageContext != candidate.CanonicalSkillUsageExplicitlyNotUsed {
		t.Fatalf("choice decision=%+v", decision)
	}
}

func TestAIOriginCannotTurnChoiceIntoUserConfirmation(t *testing.T) {
	decision, err := DecideAnswer(AnswerInput{Request: pendingRequest("Kubernetes"), Answer: CandidateAnswer{Kind: "choice", ChoiceID: "commercial", Raw: "Да, коммерчески"}, Origin: OriginAIInterpretation})
	if err == nil || decision.Intent != nil {
		t.Fatalf("AI-origin choice became a user confirmation: decision=%+v err=%v", decision, err)
	}
}

func TestAcquisitionPreservesExactExperienceDuration(t *testing.T) {
	value := acquisitionCandidate()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	value.Profile.TotalExperienceMonths = candidate.ProfileIntFact{Value: 11, ProfileFact: candidate.ProfileFact{
		Source: candidate.CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicit candidate answer"},
	}}
	resolved, err := candidatecontext.NewResolver(value).Resolve(candidatecontext.ResolveInput{Query: "Сколько месяцев общего опыта?", EmployerMessage: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range resolved.ResolvedFacts {
		if fact.Topic == "total_experience" && fact.Value == "11 месяцев" {
			return
		}
	}
	t.Fatalf("exact 11-month fact was not preserved: %+v", resolved)
}

func TestAmbiguousAnswerRemainsUnresolved(t *testing.T) {
	decision, err := DecideAnswer(AnswerInput{Request: pendingRequest("Kubernetes"), Answer: CandidateAnswer{Kind: "free_text", Raw: "Возможно, иногда"}, Origin: OriginOther})
	if !errors.Is(err, ErrKnowledgeAnswerMismatch) || decision.Intent != nil {
		t.Fatalf("ambiguous answer decision=%+v err=%v", decision, err)
	}
}

func TestAIInterpretationCannotConfirmKnowledge(t *testing.T) {
	interpretation := CandidateKnowledgeInterpretation{Proposals: []CandidateKnowledgeProposalDraft{{Type: "skill_usage", Skill: "Kubernetes", UsageContext: candidate.CanonicalSkillUsageExplicitlyNotUsed, TruthStatus: string(candidate.TruthStatusConfirmed)}}}
	decision, err := DecideAnswer(AnswerInput{Request: pendingRequest("Kubernetes"), Answer: CandidateAnswer{Kind: "free_text", Raw: "Я не использовал Kubernetes в production."}, Origin: OriginAIInterpretation, Interpretation: &interpretation})
	if err == nil || decision.Intent != nil {
		t.Fatalf("AI confirmation bypassed trust boundary: decision=%+v err=%v", decision, err)
	}

	interpretation.Proposals[0].TruthStatus = "hypothesis"
	decision, err = DecideAnswer(AnswerInput{Request: pendingRequest("Kubernetes"), Answer: CandidateAnswer{Kind: "free_text", Raw: "Я не использовал Kubernetes в production."}, Origin: OriginAIInterpretation, Interpretation: &interpretation})
	if err != nil || decision.Kind != AnswerCreatesProposal || decision.Intent == nil || !decision.Intent.RequiresUserReview || decision.Intent.Actor != candidatemutation.ActorAI {
		t.Fatalf("AI interpretation did not remain a proposal: decision=%+v err=%v", decision, err)
	}
}

func TestRepeatedAnswerProducesStableDecision(t *testing.T) {
	input := AnswerInput{Request: pendingRequest("Docker"), Answer: CandidateAnswer{Kind: "choice", ChoiceID: "commercial", Raw: "Да, коммерчески"}, Origin: OriginTrustedExplicitUser}
	first, err := DecideAnswer(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecideAnswer(input)
	if err != nil || first.Kind != second.Kind || first.Intent == nil || second.Intent == nil || first.Intent.Kind != second.Intent.Kind || first.Intent.Topic != second.Intent.Topic {
		t.Fatalf("repeated answer changed decision: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestReconcileClosesGapSatisfiedByCurrentCandidate(t *testing.T) {
	request := pendingRequest("Django")
	request.OriginalEmployerQuestion = "Есть ли опыт Django?"
	result, err := Reconcile(ReconciliationInput{Candidate: acquisitionCandidate(), Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Status != ClarificationResolvedExistingKnowledge {
		t.Fatalf("reconciliation=%+v", result)
	}
}
