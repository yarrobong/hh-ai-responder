package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/careeragent"
	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/coverletter"
)

func TestApplicationProcessingUsesProviderIdentityForAPIPreparation(t *testing.T) {
	responder := &HHAIResponder{transport: transportAPI}
	request := responder.applicationProcessingRequest(Vacancy{ID: 137765629}, ResumeItem{
		Id:         280551431,
		ProviderID: "280551431",
		Hash:       "hh-resume-a89-content",
		Title:      "Technical Specialist",
	}, LegacyCandidateContext{}, 0)

	if request.ResumeID != "280551431" {
		t.Fatalf("API preparation used mutable/browser hash %q, want provider resume ID 280551431", request.ResumeID)
	}
}

func TestCurrentResumeProviderIdentityUsesAPIProviderNamespace(t *testing.T) {
	responder := &HHAIResponder{
		transport:        transportAPI,
		resumeIdentifier: "280551431",
		resumes:          []ResumeItem{{Id: 280551431, ProviderID: "280551431", Hash: "hh-resume-a89-content"}},
	}
	if got := responder.currentResumeProviderID(); got != "280551431" {
		t.Fatalf("current API resume identity=%q, want provider ID 280551431", got)
	}
}

func TestPilotApprovalUsesProviderIdentityWhenHashAlsoExists(t *testing.T) {
	artifact := PilotArtifact{
		SelectedResumeID: "hh-resume-provider-id-280551431", SelectedResumeProviderID: "280551431", SelectedResumeHash: "hh-resume-a89-content",
		ResumeSelectionBasis: pilotResumeSelectionRouter,
	}
	providerID, err := pilotProviderResumeID(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if providerID != "280551431" {
		t.Fatalf("pilot provider identity=%q, want 280551431", providerID)
	}
	if err := applicationpilot.VerifyApproval(applicationpilot.Approval{VacancyID: 137765629, ResumeID: providerID, ContentHash: "hash", Nonce: "nonce"}, applicationpilot.CurrentIdentity{VacancyID: 137765629, ResumeID: providerID, ContentHash: "hash"}); err != nil {
		t.Fatalf("provider-backed pilot approval was treated as stale: %v", err)
	}
}

func TestDurablePreparationSeparatesInternalAndProviderResumeIdentity(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		careerAgentResumes: []careeragent.ResumeProfile{{
			ID: "resume-internal-a", ProviderID: "280551431", Hash: "hh-resume-a89-content", HHID: 280551431,
			Title: "Technical Specialist", Enabled: true,
		}},
	}
	result := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID: 137765629, ResumeID: "280551431", ResumeTitle: "Technical Specialist", CoverLetter: "Текст только из подтвержденных фактов.",
		CoverLetterStatus: coverletter.DraftStatusValid,
	}}
	trace := CareerAgentVacancyResult{VacancyID: 137765629, SelectedResume: "resume-internal-a", SelectedResumeTitle: "Technical Specialist", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}

	if err := responder.persistCareerAgentPreparation(Vacancy{ID: 137765629}, ResumeItem{
		Id: 280551431, ProviderID: "280551431", Hash: "hh-resume-a89-content", Title: "Technical Specialist",
	}, trace, result); err != nil {
		t.Fatal(err)
	}
	if len(store.preparations) != 1 {
		t.Fatalf("preparations=%d, want 1", len(store.preparations))
	}
	preparation := store.preparations[0]
	if preparation.ResumeID != "resume-internal-a" || preparation.ResumeProviderID != "280551431" {
		t.Fatalf("resume identity namespaces were collapsed: resume_id=%q provider_id=%q", preparation.ResumeID, preparation.ResumeProviderID)
	}
}

func TestDurablePreparationRejectsRouteForDifferentProviderResume(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		careerAgentResumes: []careeragent.ResumeProfile{{
			ID: "resume-internal-a", ProviderID: "280551431", Hash: "hh-resume-a89-content", HHID: 280551431,
			Title: "Technical Specialist", Enabled: true,
		}},
	}
	result := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID: 137765629, ResumeID: "999999999", ResumeTitle: "Other resume", CoverLetter: "Текст.",
	}}
	trace := CareerAgentVacancyResult{VacancyID: 137765629, SelectedResume: "resume-internal-a", SelectedResumeTitle: "Technical Specialist", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}

	err := responder.persistCareerAgentPreparation(Vacancy{ID: 137765629}, ResumeItem{
		Id: 280551431, ProviderID: "280551431", Hash: "hh-resume-a89-content", Title: "Technical Specialist",
	}, trace, result)
	if err == nil {
		t.Fatal("wrong provider resume was persisted as a ready preparation")
	}
	if len(store.preparations) != 0 {
		t.Fatalf("wrong-resume preparation was persisted: %+v", store.preparations)
	}
}

func TestDurablePreparationBlocksCurrentProviderReplacement(t *testing.T) {
	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		careerAgentResumes: []careeragent.ResumeProfile{
			{ID: "resume-internal-a", ProviderID: "280551431", Hash: "hash-a", HHID: 280551431, Title: "Technical Specialist", Enabled: true},
			{ID: "resume-internal-b", ProviderID: "280551432", Hash: "hash-b", HHID: 280551432, Title: "Other resume", Enabled: true},
		},
	}
	result := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{
		VacancyID: 137765629, ResumeID: "280551432", ResumeTitle: "Other resume", CoverLetter: "Текст.",
	}}
	trace := CareerAgentVacancyResult{VacancyID: 137765629, SelectedResume: "resume-internal-a", SelectedResumeTitle: "Technical Specialist", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}

	err := responder.persistCareerAgentPreparation(Vacancy{ID: 137765629}, ResumeItem{
		Id: 280551432, ProviderID: "280551432", Hash: "hash-b", Title: "Other resume",
	}, trace, result)
	if err == nil {
		t.Fatal("current provider replacement was persisted for the old route")
	}
	if len(store.preparations) != 0 {
		t.Fatalf("replacement preparation was persisted: %+v", store.preparations)
	}
}

func TestResumeContentFingerprintChangesPreparationIdentity(t *testing.T) {
	responder := &HHAIResponder{
		careerAgentMode:             "shadow",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		careerAgentResumes: []careeragent.ResumeProfile{{
			ID: "resume-internal-a", ProviderID: "280551431", Hash: "hash-a", HHID: 280551431,
			Title: "Technical Specialist", Enabled: true,
		}},
	}
	trace := CareerAgentVacancyResult{VacancyID: 137765629, SelectedResume: "resume-internal-a", SelectedResumeTitle: "Technical Specialist", FinalDecision: string(VacancyMatch), ResumeConfidence: "high"}
	firstResult := applicationprocessing.Result{Prepared: &applicationprocessing.PreparedApplication{VacancyID: 137765629, ResumeID: "280551431", ResumeTitle: "Technical Specialist", CoverLetter: "Текст."}}
	secondResult := firstResult
	first, err := responder.buildCareerAgentPreparation(Vacancy{ID: 137765629}, ResumeItem{Id: 280551431, ProviderID: "280551431", Hash: "hash-a", Title: "Technical Specialist"}, trace, firstResult)
	if err != nil {
		t.Fatal(err)
	}
	second, err := responder.buildCareerAgentPreparation(Vacancy{ID: 137765629}, ResumeItem{Id: 280551431, ProviderID: "280551431", Hash: "hash-b", Title: "Technical Specialist"}, trace, secondResult)
	if err != nil {
		t.Fatal(err)
	}
	if first.InputFingerprint == second.InputFingerprint {
		t.Fatalf("material resume content change reused preparation fingerprint %q", first.InputFingerprint)
	}
}

func TestPersistedResumeIdentitySurvivesWorkflowReload(t *testing.T) {
	path := t.TempDir() + "/career-workflow.json"
	store := jsonstorage.NewCareerWorkflowRepository(path)
	now := time.Now().UTC()
	preparation := validCanonicalResumePreparationForTest(now)
	if err := store.UpsertPreparation(context.Background(), preparation); err != nil {
		t.Fatal(err)
	}
	reloaded := jsonstorage.NewCareerWorkflowRepository(path)
	got, err := reloaded.GetPreparation(context.Background(), preparation.VacancyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResumeID != "resume-internal-a" || got.ResumeProviderID != "280551431" || got.ResumeFingerprint != "hash-a" {
		t.Fatalf("reloaded preparation changed resume binding: %+v", got)
	}
}

func TestPilotReadyPreparationIsDurableAndIdempotent(t *testing.T) {
	path := t.TempDir() + "/career-workflow.json"
	store := jsonstorage.NewCareerWorkflowRepository(path)
	responder := &HHAIResponder{
		careerWorkflowStore:         store,
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		careerAgentResumes: []careeragent.ResumeProfile{{
			ID: "resume-internal-a", ProviderID: "280551431", Hash: "hh-resume-a89-content", HHID: 280551431,
			Title: "Technical Specialist", Enabled: true,
		}},
	}
	artifact := PilotArtifact{
		VacancyID: 137765629, SelectedResumeProviderID: "280551431", SelectedResumeHash: "hh-resume-a89-content",
		SelectedResumeTitle: "Technical Specialist", FinalDecision: "MATCH", RouterConfidence: "high",
		ContentHash: contentHash("Текст."), CoverLetter: "Текст.", CoverLetterStatus: coverletter.DraftStatusValid,
	}
	selected := ResumeItem{Id: 280551431, ProviderID: "280551431", Hash: "hh-resume-a89-content", Title: "Technical Specialist"}
	for range 2 {
		if _, err := responder.persistCareerAgentPilotPreparation(Vacancy{ID: 137765629}, selected, "resume-internal-a", VacancyEvaluation{}, artifact); err != nil {
			t.Fatal(err)
		}
	}
	preparations, err := store.ListPreparations(context.Background(), careeragent.PreparationQuery{Limit: 10, VacancyID: intPointerForTest(137765629)})
	if err != nil || len(preparations) != 1 {
		t.Fatalf("idempotent pilot persistence created %d preparations: %v", len(preparations), err)
	}
}

func intPointerForTest(value int) *int { return &value }

func validCanonicalResumePreparationForTest(now time.Time) careeragent.ApplicationPreparation {
	preparation := careeragent.ApplicationPreparation{
		ID: "preparation-canonical", VacancyID: 137765629, ResumeID: "resume-internal-a", ResumeProviderID: "280551431", ResumeFingerprint: "hash-a",
		CandidateID: "candidate-1", CandidateVersion: 3, CandidateSnapshotHash: strings.Repeat("a", 64),
		RouteStatus: careeragent.ResumeRouteMatch, RouteConfidence: "high", Evidence: []byte(`{"route":"selected"}`),
		CoverLetter: "Текст только из подтвержденных фактов.", Status: careeragent.PreparationStatusReady, CreatedAt: now, UpdatedAt: now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)
	return preparation
}
