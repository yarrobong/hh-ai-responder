package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exportPilotFixture(now time.Time) PilotArtifact {
	letter := "Здравствуйте! Готов обсудить интеграции и поддержку API."
	required := false
	return PilotArtifact{
		Version:          pilotArtifactVersion,
		Status:           pilotReadyStatus,
		VacancyID:        42,
		SelectedResumeID: "hh-resume-provider-id-resume-provider-7",
		FinalDecision:    "MATCH",
		Preflight:        PilotPreflightSnapshot{CoverLetterRequired: &required},
		CoverLetter:      letter,
		ContentHash:      contentHash(letter),
		Nonce:            "pilot-nonce-7",
		PreviewFreshAt:   now,
	}
}

func TestPilotArtifactToAPIApplicationApprovalPreservesExactApprovedMaterial(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pilot := exportPilotFixture(now)
	approval, err := pilotArtifactToAPIApplicationApproval(pilot)
	if err != nil {
		t.Fatal(err)
	}
	if approval.VacancyID != pilot.VacancyID || approval.ProviderResumeID != "resume-provider-7" || approval.SelectedResumeID != "resume-provider-7" || approval.CoverLetter != pilot.CoverLetter || approval.ContentHash != contentHash(pilot.CoverLetter) || approval.Nonce != pilot.Nonce || !approval.PreviewFreshAt.Equal(pilot.PreviewFreshAt) {
		t.Fatalf("approval=%+v, pilot=%+v", approval, pilot)
	}
}

func TestPilotArtifactToAPIApplicationApprovalFailsClosedForIdentityAndState(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	base := exportPilotFixture(now)
	tests := []struct {
		name   string
		mutate func(*PilotArtifact)
	}{
		{name: "not ready", mutate: func(value *PilotArtifact) { value.Status = "BLOCKED" }},
		{name: "not match", mutate: func(value *PilotArtifact) { value.FinalDecision = "REVIEW_REQUIRED" }},
		{name: "unknown resume representation", mutate: func(value *PilotArtifact) { value.SelectedResumeID = "hh-resume-hash-only" }},
		{name: "resume identity conflict", mutate: func(value *PilotArtifact) { value.SelectedResumeHHID = 99 }},
		{name: "used nonce", mutate: func(value *PilotArtifact) { used := now; value.NonceUsedAt = &used }},
		{name: "content hash mismatch", mutate: func(value *PilotArtifact) { value.ContentHash = strings.Repeat("0", 64) }},
		{name: "invalid cover letter", mutate: func(value *PilotArtifact) { value.CoverLetter = "```json\n{}\n```" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if _, err := pilotArtifactToAPIApplicationApproval(value); err == nil {
				t.Fatal("export unexpectedly succeeded")
			}
		})
	}
}

func TestPilotArtifactToAPIApplicationApprovalAllowsOnlyProvenEmptyLetter(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	value := exportPilotFixture(now)
	value.CoverLetter = ""
	value.ContentHash = contentHash("")
	approval, err := pilotArtifactToAPIApplicationApproval(value)
	if err != nil || approval.CoverLetter != "" || approval.ContentHash != contentHash("") {
		t.Fatalf("approval=%+v err=%v, want exact empty letter", approval, err)
	}
	required := true
	value.Preflight.CoverLetterRequired = &required
	if _, err := pilotArtifactToAPIApplicationApproval(value); err == nil {
		t.Fatal("empty letter exported when fresh pilot required one")
	}
}

func TestHHAPIApprovalExportWritesPrivateAtomicArtifactAndSanitizedOutput(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "career-agent-pilot.json")
	outPath := filepath.Join(dir, "api-approval.json")
	raw, err := json.Marshal(exportPilotFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", outPath}, Config{}, nil, &out, nil, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("approval mode=%o, want 600", info.Mode().Perm())
	}
	approval, err := loadAPIApplicationApproval(outPath)
	if err != nil || approval.ProviderResumeID != "resume-provider-7" {
		t.Fatalf("approval=%+v err=%v", approval, err)
	}
	if strings.Contains(out.String(), "resume-provider-7") || strings.Contains(out.String(), pilotPath) || strings.Contains(out.String(), outPath) {
		t.Fatalf("output was not sanitized: %q", out.String())
	}
}

func TestHHAPIApprovalExportRejectsInvalidPilotFile(t *testing.T) {
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "pilot.json")
	if err := os.WriteFile(pilotPath, []byte(`{"version":1,"status":"BLOCKED"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", filepath.Join(dir, "out.json")}, Config{}, nil, bytes.NewBuffer(nil), nil, HHAPICommandDeps{})
	if err == nil {
		t.Fatalf("invalid pilot export error=%v", err)
	}
}
