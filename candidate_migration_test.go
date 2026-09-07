package main

import (
	"testing"
	"time"
)

func TestCandidateMigrationPlanBlocksCriticalMapperDiagnostics(t *testing.T) {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Skills = []CandidateSkill{
		{Name: "Django", Level: SkillLevelWorking, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)},
		{Name: "Django", Level: SkillLevelUnknown, Negative: true, ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)},
	}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, CandidateID: "candidate-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Conflicts) == 0 {
		t.Fatal("fixture did not produce a mapper conflict")
	}
	plan, err := BuildCandidateMigrationPlan(CandidateMigrationSource{Candidate: candidate, Diagnostics: diagnostics}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.SafeToApply || len(plan.Report.Conflicts) == 0 {
		t.Fatalf("critical mapper diagnostics were not blocking: %+v", plan.Report)
	}
}

func TestCandidateMigrationPlanIdenticalDestinationIsNoOp(t *testing.T) {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Identity.FullName = ProfileStringFact{Value: "Candidate", ProfileFact: confirmedProfileFact(CandidateSourceUserConfirmed, at)}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, CandidateID: "candidate-1"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildCandidateMigrationPlan(CandidateMigrationSource{Candidate: candidate, Diagnostics: diagnostics}, &candidate)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.Status != MigrationAlreadyPresent || !plan.Report.SafeToApply {
		t.Fatalf("identical candidate was not classified as a safe no-op: %+v", plan.Report)
	}
	fingerprint, err := candidateFingerprint(candidate)
	if err != nil || fingerprint == "" {
		t.Fatalf("candidate fingerprint missing: %q %v", fingerprint, err)
	}
}

func TestCandidateMigrationPlanDifferentDestinationRefusesOverwrite(t *testing.T) {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(at)
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, CandidateID: "candidate-1"})
	if err != nil {
		t.Fatal(err)
	}
	destination := candidate
	destination.Identity.Location = "different"
	plan, err := BuildCandidateMigrationPlan(CandidateMigrationSource{Candidate: candidate, Diagnostics: diagnostics}, &destination)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.SafeToApply || plan.Report.Status != candidateMigrationConflict {
		t.Fatalf("different destination was not blocked: %+v", plan.Report)
	}
}

func TestCanonicalCandidateValidationPreservesUnknownMeaning(t *testing.T) {
	meta := knowledgeTestMetadata(KnowledgeSourceDerived, TruthStatusUnknown)
	candidate := Candidate{Version: 1, ID: "candidate-1", Unknowns: []CandidateUnknown{{ID: "unknown-1", Question: "Used Redis?", Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: meta}}}
	if err := validateCanonicalCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.Unknowns[0].Status == CandidateUnknownRejected {
		t.Fatal("unknown was converted to a negative state")
	}
}

func TestCanonicalCandidateValidationFailsClosedOnInvalidClaim(t *testing.T) {
	candidate := Candidate{Version: 1, ID: "candidate-1", Claims: []CanonicalCandidateClaim{{
		ID: "claim-1", SubjectType: "skill", SubjectID: "skill-1", Field: "fact",
		Value: "{}", Polarity: CanonicalClaimPolarity("maybe"), State: CanonicalClaimActive,
	}}}
	if err := validateCanonicalCandidate(candidate); err == nil {
		t.Fatal("invalid claim polarity was accepted")
	}
}
