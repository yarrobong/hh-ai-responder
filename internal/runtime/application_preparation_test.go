package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

type preparationReaderFixture struct {
	preparation careeragent.ApplicationPreparation
	err         error
}

func (f preparationReaderFixture) GetPreparation(context.Context, int) (careeragent.ApplicationPreparation, error) {
	if f.err != nil {
		return careeragent.ApplicationPreparation{}, f.err
	}
	return f.preparation, nil
}

func (preparationReaderFixture) GetRun(context.Context, string) (careeragent.AgentRun, error) {
	return careeragent.AgentRun{}, careeragent.ErrAgentRunNotFound
}

func (preparationReaderFixture) ListRuns(context.Context, careeragent.RunQuery) ([]careeragent.AgentRun, error) {
	return nil, nil
}

func (preparationReaderFixture) ListPreparations(context.Context, careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error) {
	return nil, nil
}

func validPreparationApprovalFixture(now time.Time) (APIApplicationApproval, careeragent.ApplicationPreparation) {
	preparation := careeragent.ApplicationPreparation{
		ID:                    "preparation-42",
		VacancyID:             42,
		ResumeID:              "resume-hash-7",
		ResumeProviderID:      "resume-provider-7",
		ResumeFingerprint:     "resume-hash-7",
		CandidateID:           "candidate-1",
		CandidateVersion:      3,
		CandidateSnapshotHash: "candidate-snapshot-hash",
		RouteStatus:           careeragent.ResumeRouteMatch,
		RouteConfidence:       "high",
		Evidence:              []byte(`{"route":"selected"}`),
		CoverLetter:           "Здравствуйте! Готов обсудить интеграции и поддержку API.",
		Status:                careeragent.PreparationStatusReady,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)
	approval := validAPIApplicationApproval(now)
	approval.PreparationID = preparation.ID
	approval.PreparationHash = preparation.InputFingerprint
	return approval, preparation
}

func TestPreparationApprovalBindingRequiresExactDurablePreparation(t *testing.T) {
	now := time.Now().UTC()
	approval, preparation := validPreparationApprovalFixture(now)
	reader := preparationReaderFixture{preparation: preparation}

	if err := validatePreparationApprovalBinding(context.Background(), reader, approval, 42, "resume-provider-7"); err != nil {
		t.Fatalf("valid preparation approval rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*APIApplicationApproval, *careeragent.ApplicationPreparation)
		want   error
	}{
		{name: "stale status", mutate: func(_ *APIApplicationApproval, value *careeragent.ApplicationPreparation) {
			value.Status = careeragent.PreparationStatusStale
		}, want: errAPIApplicationPreparationStale},
		{name: "changed letter hash", mutate: func(value *APIApplicationApproval, _ *careeragent.ApplicationPreparation) {
			value.ContentHash = contentHash("changed")
		}, want: errAPIApplicationPreparation},
		{name: "changed candidate version", mutate: func(_ *APIApplicationApproval, value *careeragent.ApplicationPreparation) { value.CandidateVersion++ }, want: errAPIApplicationPreparationStale},
		{name: "wrong vacancy", mutate: func(value *APIApplicationApproval, _ *careeragent.ApplicationPreparation) { value.VacancyID = 43 }, want: errAPIApplicationPreparation},
		{name: "wrong preparation hash", mutate: func(value *APIApplicationApproval, _ *careeragent.ApplicationPreparation) {
			value.PreparationHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}, want: errAPIApplicationPreparation},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value, stored := approval, preparation
			test.mutate(&value, &stored)
			err := validatePreparationApprovalBinding(context.Background(), preparationReaderFixture{preparation: stored}, value, 42, "resume-provider-7")
			if err == nil || !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want errors.Is(%v)", err, test.want)
			}
		})
	}
}

func TestPreparationApprovalBindingPreservesLegacyApprovalCompatibility(t *testing.T) {
	now := time.Now().UTC()
	legacy := validAPIApplicationApproval(now)
	if err := validatePreparationApprovalBinding(context.Background(), nil, legacy, legacy.VacancyID, legacy.ProviderResumeID); err != nil {
		t.Fatalf("legacy approval was not accepted without a preparation store: %v", err)
	}
	legacy.PreparationID = "preparation-only"
	if err := validatePreparationApprovalBinding(context.Background(), nil, legacy, legacy.VacancyID, legacy.ProviderResumeID); err == nil {
		t.Fatal("partial preparation reference was accepted")
	}
}

func TestPilotApprovalCarriesPreparationReference(t *testing.T) {
	now := time.Now().UTC()
	approval, preparation := validPreparationApprovalFixture(now)
	coverLetterRequired := false
	artifact := PilotArtifact{
		Version: pilotArtifactVersion, Status: pilotReadyStatus, VacancyID: preparation.VacancyID,
		SelectedResumeID: "hh-resume-provider-id-resume-provider-7", SelectedResumeHash: preparation.ResumeProviderID,
		CoverLetter: preparation.CoverLetter, ContentHash: preparation.CoverLetterHash,
		Nonce: "nonce", PreviewFreshAt: now, FinalDecision: "MATCH",
		Preflight:     PilotPreflightSnapshot{CoverLetterRequired: &coverLetterRequired},
		PreparationID: preparation.ID, PreparationHash: preparation.InputFingerprint,
	}
	got, err := pilotArtifactToAPIApplicationApproval(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if got.PreparationID != approval.PreparationID || got.PreparationHash != approval.PreparationHash {
		t.Fatalf("preparation provenance was not copied: %+v", got)
	}
}
