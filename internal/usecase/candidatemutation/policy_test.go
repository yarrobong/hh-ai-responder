package candidatemutation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
)

func mutationTime() time.Time { return time.Date(2026, 9, 8, 12, 30, 0, 123456789, time.UTC) }

func mutationSource(source candidate.KnowledgeSource) candidate.KnowledgeSourceRecord {
	return candidate.KnowledgeSourceRecord{Type: source, Evidence: []string{"synthetic evidence"}, ObservedAt: timePtr(mutationTime())}
}

func TestBuildMetadataKeepsAIHypothesisUnconfirmed(t *testing.T) {
	confidence := .99
	meta, err := BuildMetadata(MetadataInput{
		Actor: ActorAI, Source: mutationSource(candidate.KnowledgeSourceDerived), Reason: "AI extraction",
		Confidence: &confidence, EntityType: "skill", Now: mutationTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.TruthStatus != candidate.TruthStatusHypothesis || meta.ConfirmedAt != nil {
		t.Fatalf("AI hypothesis was promoted: %+v", meta)
	}
	if err := meta.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildMetadataRequiresExplicitUserForConfirmation(t *testing.T) {
	_, err := BuildMetadata(MetadataInput{Actor: ActorAI, Source: mutationSource(candidate.KnowledgeSourceUserConfirmed), Reason: "model says so", EntityType: "skill", Now: mutationTime()})
	if err == nil || !strings.Contains(err.Error(), "only an explicit user action") {
		t.Fatalf("unexpected confirmation result: %v", err)
	}
	meta, err := BuildMetadata(MetadataInput{Actor: ActorUser, Source: mutationSource(candidate.KnowledgeSourceUserConfirmed), Reason: "explicit answer", EntityType: "skill", Now: mutationTime()})
	if err != nil || meta.TruthStatus != candidate.TruthStatusConfirmed || meta.ConfirmedAt == nil {
		t.Fatalf("explicit confirmation failed: %+v %v", meta, err)
	}
}

func TestProposalLifecycleIsSingleAndTerminal(t *testing.T) {
	proposal := candidate.KnowledgeProposal{Status: candidate.KnowledgeProposalPending}
	if err := ApplyProposalResolution(&proposal, ConfirmProposal); err != nil {
		t.Fatal(err)
	}
	if proposal.Status != candidate.KnowledgeProposalConfirmed {
		t.Fatalf("unexpected status: %s", proposal.Status)
	}
	if err := ApplyProposalResolution(&proposal, ConfirmProposal); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Fatalf("repeat confirmation was not denied: %v", err)
	}
	proposal.Status = candidate.KnowledgeProposalPending
	if err := ApplyProposalResolution(&proposal, RejectProposal); err != nil || proposal.Status != candidate.KnowledgeProposalRejected {
		t.Fatalf("rejection failed: %s %v", proposal.Status, err)
	}
}

func pendingUnknown() candidate.CandidateUnknown {
	at := mutationTime()
	return candidate.CandidateUnknown{
		ID: "unknown-1", Question: "Worked with Redis?", Status: candidate.CandidateUnknownNeedsConfirmation,
		KnowledgeMetadata: candidate.KnowledgeMetadata{
			TruthStatus: candidate.TruthStatusUnknown, Sources: []candidate.KnowledgeSourceRecord{mutationSource(candidate.KnowledgeSourceUnknown)},
			CreatedAt: at, UpdatedAt: at,
		},
	}
}

func TestUnknownLifecycleAndTruthAreCentralized(t *testing.T) {
	confirmed, err := ResolveUnknown(pendingUnknown(), UnknownResolutionInput{Operation: ConfirmUnknown, Answer: "Да, в pet-проекте", Now: mutationTime()})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != candidate.CandidateUnknownConfirmed || confirmed.TruthStatus != candidate.TruthStatusConfirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("unknown was not explicitly confirmed: %+v", confirmed)
	}
	if _, err := ResolveUnknown(confirmed, UnknownResolutionInput{Operation: DismissUnknown, Now: mutationTime()}); err == nil {
		t.Fatal("resolved unknown was mutated again")
	}
	dismissed, err := ResolveUnknown(pendingUnknown(), UnknownResolutionInput{Operation: DismissUnknown, Now: mutationTime()})
	if err != nil {
		t.Fatal(err)
	}
	if dismissed.Status != candidate.CandidateUnknownDismissed || dismissed.TruthStatus != candidate.TruthStatusUnknown || dismissed.ConfirmedAt != nil {
		t.Fatalf("dismissal changed truth incorrectly: %+v", dismissed)
	}
	for _, operation := range []UnknownResolution{RejectUnknown, SupersedeUnknown} {
		resolved, err := ResolveUnknown(pendingUnknown(), UnknownResolutionInput{Operation: operation, Now: mutationTime()})
		if err != nil || resolved.Status == candidate.CandidateUnknownConfirmed || resolved.TruthStatus != candidate.TruthStatusUnknown || resolved.ConfirmedAt != nil {
			t.Fatalf("%s changed unknown truth incorrectly: %+v %v", operation, resolved, err)
		}
	}
}

func TestStalePolicyAndEventTimestampAreDeterministic(t *testing.T) {
	if err := CheckExpectedVersion(4, 3); !errors.Is(err, ErrStale) {
		t.Fatalf("expected stale error, got %v", err)
	}
	if err := CheckExpectedVersion(4, 0); err != nil {
		t.Fatal(err)
	}
	if err := CheckBaseValue(json.RawMessage(`{"id":"skill-1"}`), json.RawMessage(`{"id":"skill-1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := CheckBaseValue(json.RawMessage(`{"id":"skill-1"}`), json.RawMessage(`{"id":"skill-2"}`)); err == nil || !strings.Contains(err.Error(), "knowledge changed since proposal") {
		t.Fatalf("proposal base mismatch was not rejected: %v", err)
	}
	event, err := BuildEvent(EventInput{ID: "event-1", Now: mutationTime(), Action: "confirm", EntityType: "proposal", EntityID: "proposal-1", Source: candidate.KnowledgeSourceUserConfirmed, Actor: "user", NewValue: json.RawMessage(`{"status":"confirmed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !event.Timestamp.Equal(mutationTime()) || event.ID != "event-1" {
		t.Fatalf("event timestamp or ID changed: %+v", event)
	}
	if _, err := BuildEvent(EventInput{ID: "event-2", Now: mutationTime(), Action: "confirm", EntityType: "proposal", EntityID: "proposal-1", Source: candidate.KnowledgeSourceUserConfirmed, Actor: "user", NewValue: json.RawMessage(`not-json`)}); err == nil {
		t.Fatal("invalid event snapshot was accepted")
	}
}

func TestValidateProposalRejectsConfirmedPayload(t *testing.T) {
	at := mutationTime()
	meta := candidate.KnowledgeMetadata{
		TruthStatus: candidate.TruthStatusHypothesis, Sources: []candidate.KnowledgeSourceRecord{mutationSource(candidate.KnowledgeSourceCandidateInterview)},
		CreatedAt: at, UpdatedAt: at,
	}
	value := candidate.CandidateSkillDetailed{ID: "skill-1", Name: "Redis", Level: candidate.SkillLevelWorking, KnowledgeMetadata: meta}
	proposal := candidate.KnowledgeProposal{ID: "proposal-1", EntityType: "skill", EntityID: value.ID, ProposedValue: mustJSON(value), Reason: "synthetic", Source: candidate.KnowledgeSourceCandidateInterview, Status: candidate.KnowledgeProposalPending, CreatedAt: at, BaseValue: json.RawMessage("null")}
	if err := ValidateProposal(proposal); err != nil {
		t.Fatal(err)
	}
	value.TruthStatus = candidate.TruthStatusConfirmed
	proposal.ProposedValue = mustJSON(value)
	if err := ValidateProposal(proposal); err == nil {
		t.Fatal("confirmed proposal payload was accepted")
	}
}

func mustJSON(value interface{}) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
