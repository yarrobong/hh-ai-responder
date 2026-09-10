package candidatecontext

import (
	"strings"
	"testing"
	"time"

	domaincandidate "hh-ai-responder/internal/candidate"
)

func testMetadata(status domaincandidate.TruthStatus) domaincandidate.KnowledgeMetadata {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return domaincandidate.KnowledgeMetadata{TruthStatus: status, Evidence: []string{"synthetic fixture"}, CreatedAt: now, UpdatedAt: now}
}

func testProfile() domaincandidate.CanonicalProfileSnapshot {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	confirmed := domaincandidate.ProfileFact{Source: domaincandidate.CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"synthetic fixture"}}
	months := domaincandidate.ProfileIntFact{Value: 11, ProfileFact: confirmed}
	return domaincandidate.CanonicalProfileSnapshot{
		TotalExperienceMonths: months,
		WorkPreferences: domaincandidate.WorkPreferences{
			Relocation:    domaincandidate.ProfileStringFact{Value: "Не готов к релокации", ProfileFact: confirmed},
			WorkMode:      domaincandidate.ProfileStringFact{Value: "Удалённо", ProfileFact: confirmed},
			SalaryMinimum: domaincandidate.ProfileIntFact{Value: 40000, ProfileFact: confirmed},
		},
		Communication: domaincandidate.EmployerCommunicationPreferences{
			Salary: domaincandidate.ProfileStringFact{Value: "Минимум 40 000 ₽", ProfileFact: confirmed},
		},
	}
}

func testCandidate() domaincandidate.Candidate {
	django := domaincandidate.CanonicalCandidateSkill{ID: "django", Name: "Django", DisplayName: "Django", Level: domaincandidate.SkillLevelAdvanced, Metadata: testMetadata(domaincandidate.TruthStatusConfirmed)}
	docker := domaincandidate.CanonicalCandidateSkill{ID: "docker", Name: "Docker", DisplayName: "Docker", Level: domaincandidate.SkillLevelWorking, Metadata: testMetadata(domaincandidate.TruthStatusConfirmed)}
	project := domaincandidate.CandidateProject{ID: "bizon", Name: "BizonVR", Technologies: []string{"Django", "Docker"}, Tasks: []string{"Backend development"}, KnowledgeMetadata: testMetadata(domaincandidate.TruthStatusConfirmed)}
	return domaincandidate.Candidate{
		Version: 1, ID: "synthetic", Profile: testProfile(),
		Skills:   []domaincandidate.CanonicalCandidateSkill{django, docker},
		Projects: []domaincandidate.CanonicalCandidateProject{{ID: project.ID, Name: project.Name, Technologies: project.Technologies, Tasks: project.Tasks, DetailedSource: &project, Metadata: project.KnowledgeMetadata}},
	}
}

func resolveTest(t *testing.T, candidate domaincandidate.Candidate, query string) CandidateContext {
	t.Helper()
	result, err := NewResolver(candidate).Resolve(ResolveInput{Query: query, EmployerMessage: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func factFor(result CandidateContext, topic string) (ResolvedFact, bool) {
	for _, facts := range [][]ResolvedFact{result.ResolvedFacts, result.PartiallyResolvedFacts, result.UnknownAtomicFacts, result.RestrictedFacts} {
		for _, fact := range facts {
			if fact.Topic == topic {
				return fact, true
			}
		}
	}
	return ResolvedFact{}, false
}

func TestResolverConfirmedSkillIsAnswerable(t *testing.T) {
	result := resolveTest(t, testCandidate(), "Есть ли опыт Django?")
	fact, ok := factFor(result, "Django")
	if !ok || fact.Status != ResolvedFactAnswerable {
		t.Fatalf("Django should be answerable: %+v", result)
	}
}

func TestResolverPartialProductionEvidenceStaysPartial(t *testing.T) {
	result := resolveTest(t, testCandidate(), "Сколько лет Docker в production?")
	fact, ok := factFor(result, "Docker")
	if !ok || fact.Status != ResolvedFactPartiallyAnswerable || fact.MissingPart == "" {
		t.Fatalf("Docker production evidence should remain partial: %+v", result)
	}
}

func TestResolverUnknownTechnologyIsNotInferred(t *testing.T) {
	result := resolveTest(t, testCandidate(), "Работали ли вы с Kubernetes?")
	fact, ok := factFor(result, "Kubernetes")
	if !ok || fact.Status != ResolvedFactUnknown || len(result.ResolvedFacts) != 0 {
		t.Fatalf("Kubernetes must remain unknown: %+v", result)
	}
}

func TestResolverSalaryAndNegativeRelocationAreAnswerable(t *testing.T) {
	for _, test := range []struct{ query, topic, want string }{
		{"Какая зарплата?", "salary", "Минимум 40 000 ₽"},
		{"Готовы к релокации?", "relocation", "Не готов к релокации"},
	} {
		result := resolveTest(t, testCandidate(), test.query)
		fact, ok := factFor(result, test.topic)
		if !ok || fact.Status != ResolvedFactAnswerable || fact.Value != test.want {
			t.Fatalf("%s should expose confirmed value: %+v", test.topic, result)
		}
	}
}

func TestResolverPreservesExactExperienceDuration(t *testing.T) {
	result := resolveTest(t, testCandidate(), "Сколько месяцев общего опыта?")
	fact, ok := factFor(result, "total_experience")
	if !ok || fact.Status != ResolvedFactAnswerable || fact.Value != "11 месяцев" {
		t.Fatalf("experience was changed: %+v", result)
	}
	if strings.Contains(strings.ToLower(fact.Value), "1 год") {
		t.Fatalf("11 months became one year: %q", fact.Value)
	}
}

func TestResolverRequirementDoesNotFabricateOneYear(t *testing.T) {
	result, err := NewResolver(testCandidate()).Resolve(ResolveInput{Query: "Требуется 1–3 года опыта с Docker", EmployerMessage: false})
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.ToLower(result.AllowedFacts[0])
	if strings.Contains(raw, "1 год") || strings.Contains(raw, "12 месяцев") {
		t.Fatalf("requirement fabricated candidate duration: %+v", result)
	}
}

func TestResolverHypothesisIsNotSafeEvidence(t *testing.T) {
	candidate := testCandidate()
	candidate.Skills = append(candidate.Skills, domaincandidate.CanonicalCandidateSkill{ID: "k8s", Name: "Kubernetes", DisplayName: "Kubernetes", Level: domaincandidate.SkillLevelAdvanced, Metadata: testMetadata(domaincandidate.TruthStatusHypothesis)})
	result := resolveTest(t, candidate, "Работали ли вы с Kubernetes?")
	fact, ok := factFor(result, "Kubernetes")
	if !ok || fact.Status != ResolvedFactUnknown || len(result.ResolvedFacts) != 0 {
		t.Fatalf("hypothesis became evidence: %+v", result)
	}
}

func TestResolverRestrictedClaimRemainsRestricted(t *testing.T) {
	candidate := testCandidate()
	candidate.Profile.Communication.AvoidClaiming = domaincandidate.ProfileListFact{Values: []string{"Kubernetes production"}, ProfileFact: testProfile().WorkPreferences.Relocation.ProfileFact}
	result := resolveTest(t, candidate, "Есть ли опыт Kubernetes в production?")
	fact, ok := factFor(result, "Kubernetes")
	if !ok || fact.Status != ResolvedFactRestricted {
		t.Fatalf("restricted claim was not preserved: %+v", result)
	}
}

func TestResolverUnknownIsNotFalseAndNonQuestionIsIgnored(t *testing.T) {
	unknown := resolveTest(t, testCandidate(), "Есть ли опыт с BGP?")
	if len(unknown.UnknownAtomicFacts) == 0 || len(unknown.RestrictedFacts) != 0 {
		t.Fatalf("unknown was treated as false: %+v", unknown)
	}
	for _, message := range []string{"Спасибо, будем на связи", "Приглашаем на собеседование завтра", "Рассмотрим ваше резюме и свяжемся"} {
		result := resolveTest(t, testCandidate(), message)
		if len(result.UnknownAtomicFacts) != 0 || len(result.MissingInformation) != 0 {
			t.Fatalf("non-question became a candidate question: %q => %+v", message, result)
		}
	}
}
