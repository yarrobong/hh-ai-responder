package careeragent

import (
	"encoding/json"
	"testing"
)

func TestPreparationInputFingerprintIsStableAcrossDurableJSONNormalization(t *testing.T) {
	first := validPreparationFixture()
	first.Evidence = json.RawMessage(`{"b":2,"a":1}`)
	first.TestAnswerDrafts = json.RawMessage(`{"answer":true,"items":[1,2]}`)
	first.StoryIDs = []string{}
	first.KnowledgeRequests = []KnowledgeRequest{}

	second := first
	second.Evidence = json.RawMessage(`{"a":1,"b":2}`)
	second.TestAnswerDrafts = json.RawMessage(`{"items":[1,2],"answer":true}`)
	second.StoryIDs = nil
	second.KnowledgeRequests = nil

	if got, want := PreparationInputFingerprint(first), PreparationInputFingerprint(second); got != want {
		t.Fatalf("fingerprint changed after durable JSON normalization: got=%s want=%s", got, want)
	}
}
