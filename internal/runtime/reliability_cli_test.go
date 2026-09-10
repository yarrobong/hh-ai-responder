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
