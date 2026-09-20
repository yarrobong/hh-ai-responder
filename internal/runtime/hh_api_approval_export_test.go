package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
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

func TestHHAPIApprovalExportRoundTripDryRunNeverPosts(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
			t.Fatalf("round-trip dry-run issued POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/vacancies/42":
			writeHHAPIJSON(t, w, map[string]any{"id": "42", "type": map[string]any{"id": "open"}, "archived": false, "has_test": false, "response_letter_required": false, "negotiations_url": "/negotiations?vacancy_id=42", "suitable_resumes_url": "/resumes/suitable?vacancy_id=42"})
		case "/resumes/suitable":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{map[string]any{"id": "resume-provider-7"}}, "page": 0, "pages": 1, "found": 1})
		case "/negotiations":
			writeHHAPIJSON(t, w, map[string]any{"items": []any{}, "page": 0, "pages": 1, "found": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "career-agent-pilot.json")
	approvalPath := filepath.Join(dir, "api-approval.json")
	pilot := exportPilotFixture(now)
	pilot.CoverLetter = ""
	pilot.ContentHash = contentHash("")
	raw, err := json.Marshal(pilot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var exportOutput bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"approval", "export", "--pilot", pilotPath, "--out", approvalPath}, Config{}, nil, &exportOutput, nil, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil || approval.CoverLetter != "" || approval.ProviderResumeID != "resume-provider-7" {
		t.Fatalf("exported approval=%+v err=%v", approval, err)
	}
	tokenPath := filepath.Join(dir, "token.json")
	if err := hhapi.NewFileTokenStore(tokenPath).Save(context.Background(), hhapi.OAuthTokens{AccessToken: hhAPIAccessTokenSentinel, TokenType: "bearer", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cfg := testHHAPIConfig(t, server.URL, server.URL+"/token", tokenPath, "https://operator.example/callback")
	cfg.HHTransport = "api"
	var applyOutput bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"apply", "42", "--resume-id", "resume-provider-7", "--approval-file", approvalPath}, cfg, nil, &applyOutput, nil, HHAPICommandDeps{HTTPClient: server.Client(), Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(applyOutput.String(), "WOULD_APPLY") || postCount != 0 {
		t.Fatalf("apply output=%q postCount=%d, want WOULD_APPLY and zero POSTs", applyOutput.String(), postCount)
	}
	t.Logf("dry-run output: %s", strings.TrimSpace(applyOutput.String()))
}
