package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

func manualPilotFixture(now time.Time) PilotArtifact {
	falseValue := false
	trueValue := true
	score := 82
	letter := "Здравствуйте! Готов обсудить интеграции и поддержку API.\n"
	return PilotArtifact{
		Version:          pilotArtifactVersion,
		Status:           pilotManualReviewStatus,
		VacancyID:        42,
		SelectedResumeID: "hh-resume-provider-id-resume-provider-7",
		AIScore:          &score,
		AIRecommendation: "UNCERTAIN",
		AIReasons:        []string{"provider eligibility needs operator review"},
		FinalDecision:    "REVIEW_REQUIRED",
		HardMissing:      []string{},
		HardUnknown:      []string{},
		Preflight: PilotPreflightSnapshot{
			ObservedAt: now, Active: &trueValue, AlreadyResponded: &falseValue,
			AlreadyRespondedValue: "NO", CanApply: &trueValue,
			TestRequired: &falseValue, CoverLetterRequired: &falseValue,
		},
		CoverLetter:    letter,
		ContentHash:    contentHash(letter),
		PreviewFreshAt: now,
	}
}

func TestPilotProviderResumeIDResolvesBrowserIdentityWithoutUsingHHID(t *testing.T) {
	const resumeHash = "b29ec17dff103a8bc60039ed1f356c62486c37"
	tests := []struct {
		name      string
		artifact  PilotArtifact
		want      string
		wantError bool
	}{
		{
			name: "browser-shaped hash identity",
			artifact: PilotArtifact{
				SelectedResumeID:   "hh-resume-" + resumeHash,
				SelectedResumeHash: resumeHash,
				SelectedResumeHHID: 272272326,
			},
			want: resumeHash,
		},
		{
			name: "browser ID and hash mismatch",
			artifact: PilotArtifact{
				SelectedResumeID:   "hh-resume-other",
				SelectedResumeHash: resumeHash,
				SelectedResumeHHID: 272272326,
			},
			wantError: true,
		},
		{
			name: "numeric-only identity",
			artifact: PilotArtifact{
				SelectedResumeID:   "272272326",
				SelectedResumeHHID: 272272326,
			},
			wantError: true,
		},
		{
			name: "explicit provider identity",
			artifact: PilotArtifact{
				SelectedResumeID:   "hh-resume-provider-id-resume-provider-7",
				SelectedResumeHHID: 272272326,
			},
			want: "resume-provider-7",
		},
		{
			name: "conflicting explicit provider and hash identities",
			artifact: PilotArtifact{
				SelectedResumeID:   "hh-resume-provider-id-resume-provider-7",
				SelectedResumeHash: resumeHash,
			},
			want: "resume-provider-7",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := pilotProviderResumeID(test.artifact)
			if test.wantError {
				if err == nil {
					t.Fatalf("pilotProviderResumeID()=%q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pilotProviderResumeID() error=%v", err)
			}
			if got != test.want {
				t.Fatalf("pilotProviderResumeID()=%q, want %q", got, test.want)
			}
			if got == "272272326" {
				t.Fatal("numeric browser HHID was used as API provider identity")
			}
		})
	}
}

func TestPilotProviderResumeIDUsesExplicitProviderIdentity(t *testing.T) {
	artifact := PilotArtifact{
		SelectedResumeID:         "hh-resume-provider-id-provider-7",
		SelectedResumeProviderID: "provider-7",
		SelectedResumeHash:       "provider-7",
	}
	got, err := pilotProviderResumeID(artifact)
	if err != nil || got != "provider-7" {
		t.Fatalf("pilotProviderResumeID()=%q error=%v, want provider-7", got, err)
	}
}

func TestPilotProviderResumeIDKeepsLegacyRouterHashResolution(t *testing.T) {
	artifact := PilotArtifact{
		SelectedResumeID:         "hh-resume-hash-7",
		SelectedResumeHash:       "hash-7",
		SelectedResumeProviderID: "provider-7",
		ResumeSelectionBasis:     pilotResumeSelectionRouter,
	}
	got, err := pilotProviderResumeID(artifact)
	if err != nil || got != "provider-7" {
		t.Fatalf("pilotProviderResumeID()=%q error=%v, want provider-7", got, err)
	}
}

func TestPilotArtifactToAPIApplicationApprovalUsesBrowserHashProviderIdentity(t *testing.T) {
	const resumeHash = "b29ec17dff103a8bc60039ed1f356c62486c37"
	pilot := exportPilotFixture(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	pilot.SelectedResumeID = "hh-resume-" + resumeHash
	pilot.SelectedResumeHash = resumeHash
	pilot.SelectedResumeHHID = 272272326
	approval, err := pilotArtifactToAPIApplicationApproval(pilot)
	if err != nil {
		t.Fatal(err)
	}
	if approval.ProviderResumeID != resumeHash || approval.ProviderResumeID == "272272326" {
		t.Fatalf("approval provider resume ID=%q, want browser hash %q", approval.ProviderResumeID, resumeHash)
	}
}

func TestValidateManualPilotArtifactAcceptsValidAIAdvisoryAndRejectsSafetyBlockers(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	base := manualPilotFixture(now)
	tests := []struct {
		name   string
		mutate func(*PilotArtifact)
	}{
		{name: "blocked", mutate: func(value *PilotArtifact) { value.Status = "BLOCKED" }},
		{name: "reject", mutate: func(value *PilotArtifact) { value.Status = "REJECT" }},
		{name: "match", mutate: func(value *PilotArtifact) { value.FinalDecision = "MATCH" }},
		{name: "hard missing", mutate: func(value *PilotArtifact) { value.HardMissing = []string{"FastAPI"} }},
		{name: "hard unknown", mutate: func(value *PilotArtifact) { value.HardUnknown = []string{"experience"} }},
		{name: "missing AI score", mutate: func(value *PilotArtifact) { value.AIScore = nil }},
		{name: "missing AI recommendation", mutate: func(value *PilotArtifact) { value.AIRecommendation = "" }},
		{name: "test required", mutate: func(value *PilotArtifact) { required := true; value.Preflight.TestRequired = &required }},
		{name: "test unknown", mutate: func(value *PilotArtifact) { value.Preflight.TestRequired = nil }},
		{name: "duplicate", mutate: func(value *PilotArtifact) {
			responded := true
			value.Preflight.AlreadyResponded = &responded
			value.Preflight.AlreadyRespondedValue = "YES"
		}},
		{name: "duplicate unknown", mutate: func(value *PilotArtifact) {
			value.Preflight.AlreadyResponded = nil
			value.Preflight.AlreadyRespondedValue = "UNKNOWN"
		}},
		{name: "inactive", mutate: func(value *PilotArtifact) { active := false; value.Preflight.Active = &active }},
		{name: "can apply unknown", mutate: func(value *PilotArtifact) { value.Preflight.CanApply = nil }},
		{name: "can apply false", mutate: func(value *PilotArtifact) { canApply := false; value.Preflight.CanApply = &canApply }},
		{name: "missing provider resume", mutate: func(value *PilotArtifact) { value.SelectedResumeID = "" }},
		{name: "conflicting provider resume", mutate: func(value *PilotArtifact) {
			value.SelectedResumeProviderID = "resume-provider-7"
			value.SelectedResumeID = "hh-resume-provider-id-other-resume"
		}},
		{name: "stale preview", mutate: func(value *PilotArtifact) {
			value.PreviewFreshAt = now.Add(-apiApplicationApprovalMaxAge - time.Nanosecond)
		}},
		{name: "stale preflight", mutate: func(value *PilotArtifact) {
			value.Preflight.ObservedAt = now.Add(-apiApplicationApprovalMaxAge - time.Nanosecond)
		}},
		{name: "empty source content hash", mutate: func(value *PilotArtifact) { value.ContentHash = "" }},
		{name: "mismatched source content hash", mutate: func(value *PilotArtifact) { value.ContentHash = strings.Repeat("0", 64) }},
		{name: "unexpected source nonce", mutate: func(value *PilotArtifact) { value.Nonce = "pilot-nonce-must-not-exist" }},
		{name: "used source nonce", mutate: func(value *PilotArtifact) { used := now; value.NonceUsedAt = &used }},
	}
	for _, recommendation := range []string{"APPLY", "UNCERTAIN", "DO_NOT_APPLY"} {
		t.Run("accepts AI recommendation "+recommendation, func(t *testing.T) {
			value := base
			value.AIRecommendation = recommendation
			if _, err := validateManualPilotArtifact(value, value.CoverLetter, now); err != nil {
				t.Fatalf("manual pilot recommendation %q rejected: %v", recommendation, err)
			}
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if _, err := validateManualPilotArtifact(value, value.CoverLetter, now); err == nil {
				t.Fatal("manual pilot validation unexpectedly succeeded")
			}
		})
	}

	withoutProviderFacts := base
	if _, err := validateManualPilotArtifact(withoutProviderFacts, withoutProviderFacts.CoverLetter, now); err != nil {
		t.Fatalf("manual pilot validation required API-only facts: %v", err)
	}

	emptyLetter := base
	emptyLetter.CoverLetter = ""
	emptyLetter.ContentHash = contentHash("")
	if _, err := validateManualPilotArtifact(emptyLetter, "", now); err != nil {
		t.Fatalf("known-not-required empty letter was rejected: %v", err)
	}
	for _, required := range []*bool{nil, func() *bool { value := true; return &value }()} {
		value := emptyLetter
		value.Preflight.CoverLetterRequired = required
		if _, err := validateManualPilotArtifact(value, "", now); err == nil {
			t.Fatalf("empty letter accepted with cover-letter requirement=%v", required)
		}
	}
}

func TestBuildManualAPIApplicationApprovalPreservesProvenanceAndIssuesPassedFreshNonce(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pilot := manualPilotFixture(now)
	providerResumeID, err := validateManualPilotArtifact(pilot, pilot.CoverLetter, now)
	if err != nil {
		t.Fatal(err)
	}
	approval := buildManualAPIApplicationApproval(pilot, providerResumeID, pilot.CoverLetter, "pilot-hash", "fresh-approval-nonce", now)
	if approval.Status != pilotManualReviewStatus || approval.FinalDecision != "REVIEW_REQUIRED" || approval.OriginalFinalDecision != "REVIEW_REQUIRED" || approval.ApprovalBasis != manualApprovalBasis || !approval.OperatorApproved {
		t.Fatalf("approval decision/provenance = %+v", approval)
	}
	if approval.Nonce != "fresh-approval-nonce" || approval.Nonce == pilot.Nonce || approval.OperatorApprovalTimestamp != now || approval.PilotArtifactHash != "pilot-hash" {
		t.Fatalf("approval nonce/timestamp/hash = %+v", approval)
	}
	if approval.OriginalAIScore == nil || *approval.OriginalAIScore != *pilot.AIScore || approval.OriginalAIRecommendation != pilot.AIRecommendation || !reflect.DeepEqual(approval.OriginalAIRecommendationReasons, pilot.AIReasons) {
		t.Fatalf("approval AI provenance = %+v", approval)
	}
	if approval.CoverLetter != pilot.CoverLetter || approval.ContentHash != contentHash(pilot.CoverLetter) {
		t.Fatalf("approval letter binding = %+v", approval)
	}
}

func TestPilotArtifactLoaderKeepsAutomaticNonceRequirementSeparateFromManualReview(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "manual-pilot.json")
	raw, err := json.Marshal(manualPilotFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, source, err := loadPilotArtifactForManualApproval(path); err != nil || len(source) != len(raw) {
		t.Fatalf("manual loader artifact/source = err=%v bytes=%d, want accepted exact source", err, len(source))
	}
	if _, err := loadPilotArtifact(path); err == nil {
		t.Fatal("automatic loader accepted a manual pilot without a nonce")
	}
}

func TestHHAPIApprovalReviewPreservesManualProvenanceAndUsesNoHTTP(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "manual-pilot.json")
	approvalPath := filepath.Join(dir, "manual-approval.json")
	pilot := manualPilotFixture(now)
	raw, err := json.Marshal(pilot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runHHAPICommandWithDeps(context.Background(), []string{"approval", "review", "--pilot", pilotPath, "--out", approvalPath}, Config{}, nil, &out, nil, HHAPICommandDeps{Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	pilotHash := sha256.Sum256(raw)
	if approval.FinalDecision != "REVIEW_REQUIRED" || approval.OriginalFinalDecision != "REVIEW_REQUIRED" || approval.ApprovalBasis != manualApprovalBasis || !approval.OperatorApproved || approval.Nonce == "" || approval.Nonce == pilot.Nonce || approval.ContentHash != contentHash(approval.CoverLetter) || approval.OriginalAIRecommendation != "UNCERTAIN" || approval.PilotArtifactHash != hex.EncodeToString(pilotHash[:]) {
		t.Fatalf("manual approval=%+v", approval)
	}
	afterPilot, err := os.ReadFile(pilotPath)
	if err != nil || !bytes.Equal(afterPilot, raw) {
		t.Fatalf("source pilot changed: err=%v equal=%t", err, bytes.Equal(afterPilot, raw))
	}
	info, err := os.Stat(approvalPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("approval mode/info: err=%v info=%v", err, info)
	}
	if strings.Contains(out.String(), pilotPath) || strings.Contains(out.String(), approvalPath) || strings.Contains(out.String(), "resume-provider-7") || strings.Contains(out.String(), pilot.CoverLetter) {
		t.Fatalf("review output was not sanitized: %q", out.String())
	}
}

func TestHHAPIApprovalReviewBindsExactLetterFileAndChecksSourceIntegrityFirst(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "manual-pilot.json")
	letterPath := filepath.Join(dir, "reviewed-letter.txt")
	approvalPath := filepath.Join(dir, "manual-approval.json")
	pilot := manualPilotFixture(now)
	raw, err := json.Marshal(pilot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	letterBytes := []byte("Здравствуйте, API-интеграции обсудим предметно.\n")
	if err := os.WriteFile(letterPath, letterBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runHHAPICommandWithDeps(context.Background(), []string{"approval", "review", "--pilot", pilotPath, "--out", approvalPath, "--letter-file", letterPath}, Config{}, nil, io.Discard, nil, HHAPICommandDeps{Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	pilotHash := sha256.Sum256(raw)
	if approval.CoverLetter != string(letterBytes) || approval.ContentHash != contentHash(string(letterBytes)) || approval.PilotArtifactHash != hex.EncodeToString(pilotHash[:]) {
		t.Fatalf("letter binding=%+v", approval)
	}
	if err := os.WriteFile(letterPath, []byte("changed after approval"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, []byte(`{"version":1,"status":"REJECT"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := loadAPIApplicationApproval(approvalPath)
	if err != nil || unchanged.CoverLetter != string(letterBytes) || unchanged.ContentHash != contentHash(string(letterBytes)) {
		t.Fatalf("approval changed after source edits: approval=%+v err=%v", unchanged, err)
	}
}

func TestHHAPIApprovalReviewAppliesEmptyLetterPolicyB(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		required    *bool
		override    string
		wantSuccess bool
	}{
		{name: "known not required", required: func() *bool { value := false; return &value }(), wantSuccess: true},
		{name: "required", required: func() *bool { value := true; return &value }()},
		{name: "unknown", required: nil},
		{name: "non-empty override", required: func() *bool { value := false; return &value }(), override: "Letter reviewed exactly.", wantSuccess: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			pilot := manualPilotFixture(now)
			pilot.CoverLetter = ""
			pilot.ContentHash = contentHash("")
			pilot.Preflight.CoverLetterRequired = test.required
			pilotPath := filepath.Join(dir, "pilot.json")
			approvalPath := filepath.Join(dir, "approval.json")
			raw, err := json.Marshal(pilot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"approval", "review", "--pilot", pilotPath, "--out", approvalPath}
			if test.override != "" {
				letterPath := filepath.Join(dir, "letter.txt")
				if err := os.WriteFile(letterPath, []byte(test.override), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--letter-file", letterPath)
			}
			err = runHHAPICommandWithDeps(context.Background(), args, Config{}, nil, io.Discard, nil, HHAPICommandDeps{Now: func() time.Time { return now }})
			if test.wantSuccess && err != nil {
				t.Fatalf("review error=%v", err)
			}
			if !test.wantSuccess && err == nil {
				t.Fatal("empty-letter review unexpectedly succeeded")
			}
		})
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
		{name: "resume identity conflict", mutate: func(value *PilotArtifact) {
			value.SelectedResumeProviderID = "resume-provider-7"
			value.SelectedResumeID = "hh-resume-provider-id-other-resume"
		}},
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

func TestHHAPIApprovalExportBindsExactReplacementLetterForAutomaticMatch(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "career-agent-pilot.json")
	approvalPath := filepath.Join(dir, "api-approval.json")
	letterPath := filepath.Join(dir, "reviewed-letter.txt")
	pilot := exportPilotFixture(now)
	raw, err := json.MarshalIndent(pilot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("Здравствуйте!\n\nТочная операторская версия.\n")
	if err := os.WriteFile(letterPath, replacement, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", approvalPath, "--letter-file", letterPath}, Config{}, nil, io.Discard, nil, HHAPICommandDeps{}); err != nil {
		t.Fatal(err)
	}
	approval, err := loadAPIApplicationApproval(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if approval.CoverLetter != string(replacement) || approval.ContentHash != contentHash(string(replacement)) {
		t.Fatalf("replacement letter binding=%+v", approval)
	}
	if approval.Version != 1 || approval.VacancyID != pilot.VacancyID || approval.ProviderResumeID != "resume-provider-7" || approval.SelectedResumeID != "resume-provider-7" || approval.Nonce != pilot.Nonce || !approval.PreviewFreshAt.Equal(pilot.PreviewFreshAt) || approval.Status != "READY_FOR_EXPLICIT_SEND" || approval.FinalDecision != "MATCH" {
		t.Fatalf("automatic approval identity/state changed: %+v", approval)
	}
	if approval.ApprovalBasis != "" || approval.OperatorApproved || !approval.OperatorApprovalTimestamp.IsZero() || approval.OriginalAIScore != nil || approval.OriginalAIRecommendation != "" || approval.OriginalFinalDecision != "" || approval.PilotArtifactHash != "" {
		t.Fatalf("manual approval metadata was introduced: %+v", approval)
	}
	if after, err := os.ReadFile(pilotPath); err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("source pilot changed: err=%v equal=%t", err, bytes.Equal(after, raw))
	}
	if err := validateAPIApplicationApproval(approval, pilot.VacancyID, "resume-provider-7", now); err != nil {
		t.Fatalf("replacement approval failed normal validation: %v", err)
	}
	changed := approval
	changed.CoverLetter += " изменено"
	if err := validateAPIApplicationApproval(changed, pilot.VacancyID, "resume-provider-7", now); !errors.Is(err, errAPIApplicationApprovalContent) {
		t.Fatalf("changed replacement letter validation error=%v, want content error", err)
	}
}

func TestHHAPIApprovalExportValidatesOriginalPilotBeforeReplacement(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	validReplacement := []byte("Здравствуйте! Операторская версия.\n")
	tests := []struct {
		name   string
		mutate func(*PilotArtifact)
		want   string
	}{
		{name: "source content hash mismatch", mutate: func(value *PilotArtifact) { value.ContentHash = strings.Repeat("0", 64) }, want: "content hash"},
		{name: "source invalid cover letter", mutate: func(value *PilotArtifact) {
			value.CoverLetter = "```json\n{}\n```"
			value.ContentHash = contentHash(value.CoverLetter)
		}, want: "cover letter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			pilotPath := filepath.Join(dir, "career-agent-pilot.json")
			approvalPath := filepath.Join(dir, "api-approval.json")
			letterPath := filepath.Join(dir, "reviewed-letter.txt")
			pilot := exportPilotFixture(now)
			test.mutate(&pilot)
			raw, err := json.Marshal(pilot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(letterPath, validReplacement, 0o600); err != nil {
				t.Fatal(err)
			}
			err = runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", approvalPath, "--letter-file", letterPath}, Config{}, nil, io.Discard, nil, HHAPICommandDeps{})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("export error=%v, want substring %q", err, test.want)
			}
			if _, statErr := os.Stat(approvalPath); !os.IsNotExist(statErr) {
				t.Fatalf("approval artifact exists after rejected source pilot: statErr=%v", statErr)
			}
		})
	}
}

func TestHHAPIApprovalExportReplacementLetterEmptyPolicy(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		required *bool
		wantErr  bool
	}{
		{name: "known not required", required: func() *bool { value := false; return &value }()},
		{name: "required", required: func() *bool { value := true; return &value }(), wantErr: true},
		{name: "unknown", required: nil, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			pilotPath := filepath.Join(dir, "career-agent-pilot.json")
			approvalPath := filepath.Join(dir, "api-approval.json")
			letterPath := filepath.Join(dir, "reviewed-letter.txt")
			pilot := exportPilotFixture(now)
			pilot.Preflight.CoverLetterRequired = test.required
			raw, err := json.Marshal(pilot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(letterPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			err = runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", approvalPath, "--letter-file=" + letterPath}, Config{}, nil, io.Discard, nil, HHAPICommandDeps{})
			if test.wantErr {
				if err == nil {
					t.Fatal("empty replacement unexpectedly accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			approval, loadErr := loadAPIApplicationApproval(approvalPath)
			if loadErr != nil || approval.CoverLetter != "" || approval.ContentHash != contentHash("") {
				t.Fatalf("empty replacement approval=%+v err=%v", approval, loadErr)
			}
		})
	}
}

func TestHHAPIApprovalExportRejectsInvalidReplacementArgumentsAndContent(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	pilotPath := filepath.Join(dir, "career-agent-pilot.json")
	validLetterPath := filepath.Join(dir, "valid-letter.txt")
	raw, err := json.Marshal(exportPilotFixture(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pilotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validLetterPath, []byte("Здравствуйте!\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	argumentTests := []struct {
		name string
		args []string
	}{
		{name: "duplicate letter file", args: []string{"--pilot", pilotPath, "--out", filepath.Join(dir, "duplicate.json"), "--letter-file", validLetterPath, "--letter-file", validLetterPath}},
		{name: "empty letter file path", args: []string{"--pilot", pilotPath, "--out", filepath.Join(dir, "empty-path.json"), "--letter-file="}},
		{name: "unknown option", args: []string{"--pilot", pilotPath, "--out", filepath.Join(dir, "unknown.json"), "--unexpected", "value"}},
	}
	for _, test := range argumentTests {
		t.Run(test.name, func(t *testing.T) {
			if err := runHHAPICommandWithDeps(nil, append([]string{"approval", "export"}, test.args...), Config{}, nil, io.Discard, nil, HHAPICommandDeps{}); err == nil {
				t.Fatal("invalid export arguments unexpectedly succeeded")
			}
		})
	}

	contentTests := []struct {
		name    string
		content []byte
	}{
		{name: "unsafe content", content: []byte("```json\n{}\n```")},
		{name: "oversized content", content: []byte(strings.Repeat("a", maxReviewedCoverLetterSize+1))},
	}
	for _, test := range contentTests {
		t.Run(test.name, func(t *testing.T) {
			letterPath := filepath.Join(dir, test.name+".txt")
			approvalPath := filepath.Join(dir, test.name+".json")
			if err := os.WriteFile(letterPath, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := runHHAPICommandWithDeps(nil, []string{"approval", "export", "--pilot", pilotPath, "--out", approvalPath, "--letter-file", letterPath}, Config{}, nil, io.Discard, nil, HHAPICommandDeps{}); err == nil {
				t.Fatal("invalid replacement content unexpectedly succeeded")
			}
		})
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
