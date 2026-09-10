package postgresstorage

import (
	"encoding/json"
	"reflect"
	"testing"

	"hh-ai-responder/internal/semantic"
)

func TestSemanticVectorEncodingAndScanning(t *testing.T) {
	want := []float32{1, -0.25, 3.5}
	var scanned semanticVector
	if err := scanned.Scan(formatSemanticVector(want)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scanned.Values, want) {
		t.Fatalf("scanned=%v want=%v", scanned.Values, want)
	}
	if _, err := parseSemanticVector("[1, nope]"); err == nil {
		t.Fatal("invalid vector was accepted")
	}
}

func TestSemanticDocumentValidationKeepsSchemaDimensionContract(t *testing.T) {
	document := semantic.CandidateSemanticDocument{CandidateID: "candidate", EntityType: semantic.CandidateSemanticEntityProject, EntityID: "project", Content: "content", ContentHash: "hash", EmbeddingModel: "model", EmbeddingDimensions: semantic.CandidateSemanticEmbeddingDimensions, Embedding: make([]float32, semantic.CandidateSemanticEmbeddingDimensions)}
	if err := validateSemanticDocument(document); err != nil {
		t.Fatal(err)
	}
	document.EmbeddingDimensions--
	if err := validateSemanticDocument(document); err == nil {
		t.Fatal("dimension mismatch was accepted")
	}
}

func TestSemanticMetadataCodecPreservesNullableJSONContract(t *testing.T) {
	raw, err := nullableJSON(map[string]interface{}{"title": "API"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := decodeNullableJSON(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != "API" {
		t.Fatalf("decoded=%v", decoded)
	}
	if err := decodeNullableJSON(nil, &decoded); err != nil {
		t.Fatal(err)
	}
	if raw, err := json.Marshal(nil); err != nil || string(raw) != "null" {
		t.Fatalf("nil JSON=%s err=%v", raw, err)
	}
}
