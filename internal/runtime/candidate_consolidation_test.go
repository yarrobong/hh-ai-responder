package runtime

import (
	"reflect"
	"testing"

	domaincandidate "hh-ai-responder/internal/candidate"
)

func TestCandidateDomainConsolidationCompatibility(t *testing.T) {
	var domainValue domaincandidate.Candidate
	var rootValue Candidate = domainValue
	if !reflect.DeepEqual(rootValue, domainValue) {
		t.Fatal("root Candidate is not assignment-compatible with domain Candidate")
	}

	if TruthStatusConfirmed != domaincandidate.TruthStatusConfirmed || TruthStatusUnknown != domaincandidate.TruthStatusUnknown {
		t.Fatal("root truth constants diverged from candidate domain")
	}
	if CandidateSourceUserConfirmed != domaincandidate.CandidateSourceUserConfirmed || KnowledgeSourceHHResume != domaincandidate.KnowledgeSourceHHResume {
		t.Fatal("root provenance constants diverged from candidate domain")
	}

	sources := []CandidateSource{CandidateSourceUserConfirmed, CandidateSourceHHResume, CandidateSourceGithubVerified, CandidateSourceDerived, CandidateSourceUnknown}
	for _, source := range sources {
		if got, want := sourcePriority(source), domaincandidate.SourcePriority(domaincandidate.CandidateSource(source)); got != want {
			t.Fatalf("source priority mismatch for %q: root=%d domain=%d", source, got, want)
		}
	}
	for _, name := range []string{" Go ", "Golang", "PostgreSQL"} {
		if got, want := canonicalSkillName(name), domaincandidate.CanonicalSkillName(name); got != want {
			t.Fatalf("skill normalization mismatch for %q: root=%q domain=%q", name, got, want)
		}
	}

	projection, err := CanonicalEmployerSafeProjection(rootValue)
	if err != nil {
		t.Fatal(err)
	}
	domainProjection, err := domaincandidate.CanonicalEmployerSafeProjection(domainValue)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projection, domainProjection) {
		t.Fatalf("root projection adapter diverged: root=%#v domain=%#v", projection, domainProjection)
	}
}
