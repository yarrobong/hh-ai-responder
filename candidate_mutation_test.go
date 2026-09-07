package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func mutationTestUpdate(source KnowledgeSource, reason string) KnowledgeUpdate {
	return KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: source, Evidence: []string{reason}}, Reason: reason}
}

func TestCanonicalMutationPreservesTruthBoundaries(t *testing.T) {
	candidate := Candidate{ID: "candidate-test", Version: 1}
	proposal, err := mutateCanonicalSkill(&candidate, CandidateSkillDetailed{Name: "Redis", Level: SkillLevelWorking}, mutationTestUpdate(KnowledgeSourceDerived, "AI analysis"), KnowledgeActorAI, nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ProposalID == "" || len(candidate.Skills) != 0 || len(candidate.Claims) != 0 {
		t.Fatalf("AI mutation became a fact: result=%+v candidate=%+v", proposal, candidate)
	}
	if _, err := resolveCanonicalProposal(&candidate, proposal.ProposalID, true); err != nil {
		t.Fatal(err)
	}
	if len(candidate.Skills) != 1 || candidate.Skills[0].Metadata.TruthStatus != TruthStatusConfirmed {
		t.Fatalf("confirmed proposal was not materialized: %+v", candidate)
	}
	if len(candidate.Events) < 2 {
		t.Fatalf("knowledge mutation did not append proposal/fact events: %+v", candidate.Events)
	}

	if _, err := mutateCanonicalSkill(&candidate, CandidateSkillDetailed{ID: candidate.Skills[0].ID, Name: "Redis", Level: SkillLevelUnknown}, mutationTestUpdate(KnowledgeSourceHHResume, "resume"), KnowledgeActorImporter, nil); err != nil {
		t.Fatal(err)
	}
	if candidate.Skills[0].Metadata.TruthStatus != TruthStatusConfirmed {
		t.Fatal("HH import overwrote a user-confirmed skill")
	}
	if len(candidate.Proposals) < 2 {
		t.Fatal("HH conflict did not remain reviewable")
	}
}

func TestCanonicalNegativeCorrectionRetainsOldClaim(t *testing.T) {
	candidate := Candidate{ID: "candidate-test", Version: 1}
	first, err := mutateCanonicalSkill(&candidate, CandidateSkillDetailed{Name: "Kubernetes", Level: SkillLevelWorking}, mutationTestUpdate(KnowledgeSourceUserConfirmed, "user confirms"), KnowledgeActorUser, nil)
	if err != nil || first.EntityID == "" {
		t.Fatalf("positive confirmation failed: %+v %v", first, err)
	}
	_, err = mutateCanonicalSkill(&candidate, CandidateSkillDetailed{ID: candidate.Skills[0].ID, Name: "Kubernetes", Level: SkillLevelUnknown, Negative: true}, mutationTestUpdate(KnowledgeSourceUserConfirmed, "user corrects fact"), KnowledgeActorUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Claims) != 2 || candidate.Claims[0].State != CanonicalClaimSuperseded || candidate.Claims[1].Polarity != CanonicalClaimNegative || !candidate.Skills[0].Negative {
		t.Fatalf("negative correction lost assertion history: %+v", candidate)
	}
}

func TestCandidateMutationServiceRejectsAIConfirmation(t *testing.T) {
	service := NewCandidateMutationService(storageBackendJSON, nil, nil, "")
	err := service.ConfirmProposal(context.Background(), ResolveKnowledgeProposalCommand{Actor: KnowledgeActorAI, ProposalID: "proposal"})
	if err == nil || !strings.Contains(err.Error(), "only an explicit user action") {
		t.Fatalf("AI confirmation was not rejected safely: %v", err)
	}
}

func TestVerifiedMetadataNeverBecomesConfirmed(t *testing.T) {
	now := time.Now().UTC()
	meta := verifiedKnowledgeMetadata(KnowledgeSourceHHResume, "resume", now)
	if meta.TruthStatus != TruthStatusVerified || meta.ConfirmedAt != nil {
		t.Fatalf("invalid verified metadata: %+v", meta)
	}
}
