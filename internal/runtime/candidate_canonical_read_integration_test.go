package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCanonicalResolverUsesSafeProjectionAndPreservesTruthStates(t *testing.T) {
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Skills = []CandidateSkill{
		{Name: "ConfirmedSkill", Level: SkillLevelWorking, ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "explicit confirmation")},
		{Name: "UnknownSkill", Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUnknown}},
	}
	verified := CandidateSkillDetailed{ID: "verified-skill", Name: "VerifiedSkill", Level: SkillLevelBasic, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceHHResume, TruthStatusVerified, at, "HH resume")}
	hypothesis := CandidateSkillDetailed{ID: "hypothesis-skill", Name: "HypothesisSkill", Level: SkillLevelAdvanced, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceDerived, TruthStatusHypothesis, at, "AI proposal")}
	project := CandidateProject{ID: "project-evidence", Name: "Integration project", Technologies: []string{"VerifiedSkill"}, Results: []string{"connected services"}, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "user project")}
	story := CandidateStory{ID: "story-1", Title: "HypothesisSkill story", Summary: "Narrative only", Skills: []string{"HypothesisSkill"}}
	kb := CandidateKnowledgeBase{Profile: profile, Skills: []CandidateSkillDetailed{verified, hypothesis}, Projects: []CandidateProject{project}}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, Knowledge: kb, Stories: []CandidateStory{story}})
	if err != nil || len(diagnostics.Conflicts) != 0 {
		t.Fatalf("canonical candidate build failed: candidate=%+v diagnostics=%+v err=%v", candidate, diagnostics, err)
	}
	resolver := NewCandidateContextResolverFromCandidate(candidate)
	context, err := resolver.ResolveForEmployerMessage("Есть ли опыт VerifiedSkill?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !contextHasName(context.RelevantSkills, "VerifiedSkill") || len(context.MissingInformation) != 0 {
		t.Fatalf("verified fact was not answerable: %+v", context)
	}
	raw, _ := json.Marshal(context)
	text := string(raw)
	for _, forbidden := range []string{"HypothesisSkill", "UnknownSkill", "HypothesisSkill story"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unsafe candidate value %q leaked into employer context: %s", forbidden, text)
		}
	}

	negativeProfile := NewCandidateProfile(at)
	negative := CandidateSkillDetailed{ID: "docker-negative", Name: "Docker", Level: SkillLevelUnknown, Negative: true, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "explicit negative")}
	negativeCandidate, _, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: negativeProfile, Knowledge: CandidateKnowledgeBase{Profile: negativeProfile, Skills: []CandidateSkillDetailed{negative}}})
	if err != nil {
		t.Fatal(err)
	}
	negativeContext, err := NewCandidateContextResolverFromCandidate(negativeCandidate).ResolveForEmployerMessage("Работали ли вы с Docker?", nil)
	if err != nil || !contextHasName(negativeContext.AllowedFacts, "Подтверждено отсутствие навыка: Docker") || contextHasName(negativeContext.RelevantSkills, "Docker") {
		t.Fatalf("confirmed negative did not block positive claim: context=%+v err=%v", negativeContext, err)
	}
}

func TestCanonicalResolverDisputedSkillFailsClosed(t *testing.T) {
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	profile := NewCandidateProfile(at)
	left := CandidateSkillDetailed{ID: "docker-basic", Name: "Docker", Level: SkillLevelBasic, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "one assertion")}
	right := CandidateSkillDetailed{ID: "docker-advanced", Name: "Docker", Level: SkillLevelAdvanced, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "another assertion")}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, Knowledge: CandidateKnowledgeBase{Profile: profile, Skills: []CandidateSkillDetailed{left, right}}})
	if err != nil || len(diagnostics.Conflicts) == 0 {
		t.Fatalf("dispute was not diagnosed: candidate=%+v diagnostics=%+v err=%v", candidate, diagnostics, err)
	}
	if _, err := NewCandidateContextResolverFromCandidate(candidate).GetEmployerSafeContext("Docker"); err == nil {
		t.Fatal("disputed skill must withhold employer context")
	}
}

func TestCanonicalResolverProjectionMatchesLegacySafeRead(t *testing.T) {
	kb := contextTestKnowledge()
	legacy, err := kb.GetEmployerSafeCandidateKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: kb.Profile, Knowledge: *kb})
	if err != nil || len(diagnostics.Conflicts) != 0 {
		t.Fatalf("canonical build failed: diagnostics=%+v err=%v", diagnostics, err)
	}
	canonical, err := NewCandidateContextResolver(kb).employerSafeView()
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, _ := json.Marshal(legacy)
	canonicalJSON, _ := json.Marshal(canonical)
	if string(legacyJSON) != string(canonicalJSON) {
		t.Fatalf("legacy and canonical safe projections diverged\nlegacy=%s\ncanonical=%s", legacyJSON, canonicalJSON)
	}
}
