package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	applicationattempt "hh-ai-responder/internal/applicationattempt"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

func TestReliabilityCLIUsesBoundedReadModelJSON(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	store := jsonstorage.NewApplicationAttemptRepository(filepath.Join(dir, jsonstorage.ApplicationAttemptsFilename))
	attempt, err := applicationattempt.New(321, "resume-1", time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = runReliabilityCommand([]string{"applications", "--limit", "1", "--json"}, Config{CandidateProfilePath: profile}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []struct {
			AttemptID string `json:"attempt_id"`
			State     string `json:"state"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].AttemptID != attempt.AttemptID || payload.Items[0].State != string(applicationattempt.StateSending) {
		t.Fatalf("unexpected CLI JSON: %s", out.String())
	}
}

func TestReliabilityCLIStoreFailureIsNonZeroAndEmptyIsSuccess(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	var out bytes.Buffer
	if err := runReliabilityCommand([]string{"applications", "--json"}, Config{CandidateProfilePath: profile}, &out); err != nil {
		t.Fatalf("missing store should be an empty successful read: %v", err)
	}
	if !strings.Contains(out.String(), `"items": []`) {
		t.Fatalf("empty JSON result = %s", out.String())
	}
	if err := os.WriteFile(filepath.Join(dir, jsonstorage.ApplicationAttemptsFilename), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runReliabilityCommand([]string{"applications", "--json"}, Config{CandidateProfilePath: profile}, &out); err == nil {
		t.Fatal("CLI hid a corrupt store as an empty result")
	}
}

func TestReliabilityCLIManualConfirmationIsLocalAndPreservesTypedEvidence(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	store := jsonstorage.NewApplicationAttemptRepository(filepath.Join(dir, jsonstorage.ApplicationAttemptsFilename))
	attempt, err := applicationattempt.New(137244538, "279225596", time.Date(2026, 9, 18, 21, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(context.Background(), attempt.AttemptID, applicationattempt.StateAccepted, attempt.UpdatedAt.Add(time.Minute), 200, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordReconciliation(context.Background(), attempt.AttemptID, applicationattempt.ReconciliationEvidence{Kind: applicationattempt.EvidenceConflicting, Source: "automatic-conflict", ObservedAt: attempt.UpdatedAt.Add(2 * time.Minute)}, attempt.UpdatedAt.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = runReliabilityCommand([]string{"applications", "manual-confirm", "--negotiation-id", "5587518503", "--conversation-id", "5641842900", "--json", attempt.AttemptID}, Config{CandidateProfilePath: profile, StorageBackend: "json"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status                 string `json:"status"`
		EvidenceSource         string `json:"evidence_source"`
		ProviderNegotiationID  string `json:"provider_negotiation_id"`
		ProviderConversationID string `json:"provider_conversation_id"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != string(applicationreconciliation.StatusConfirmed) || payload.EvidenceSource != applicationattempt.EvidenceSourceManualProviderVerification || payload.ProviderNegotiationID != "5587518503" || payload.ProviderConversationID != "5641842900" {
		t.Fatalf("unexpected manual confirmation output: %s", out.String())
	}
	stored, err := store.Get(context.Background(), attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != applicationattempt.StateTargetResponseConfirmed || stored.Reconciliation == nil || len(stored.Reconciliation.History) != 1 {
		t.Fatalf("manual confirmation did not preserve local audit history: %+v", stored)
	}
}
