package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
)

func TestCareerAgentPilotAmbiguousRouteUsesBoundedAdvisoryOnly(t *testing.T) {
	var aiCalls atomic.Int32
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"recommendation":"APPLY","reasons":["advisory fit"],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	responder := &HHAIResponder{
		ctx: context.Background(),
		ai:  NewAIClient(context.Background(), aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		resumes: []ResumeItem{
			{Hash: "hash-a", Title: "Python backend"},
			{Hash: "hash-b", Title: "Python backend"},
		},
		careerAgentResumes: []careeragent.ResumeProfile{
			{ID: "resume-a", Hash: "hash-a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
			{ID: "resume-b", Hash: "hash-b", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		},
		resumeFactsByHash: map[string]ResumeFacts{
			"hash-a": {ExperienceText: "Python backend"},
			"hash-b": {ExperienceText: "Python backend"},
		},
		minMatchScore: 65,
	}

	value := Vacancy{
		ID: 901, Name: "Python backend", Title: "Python backend",
		Description: "Python backend API", DataCompleteness: DataCompletenessFull,
	}
	preflight := VacancyPreflight{
		VacancyID: 901, Available: true, ArchivedKnown: true,
		TestPresentKnown: true, LetterRequiredKnown: true, LetterAllowedKnown: true,
		CanApply: true, CanApplyKnown: true,
		AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded},
	}

	preview, err := responder.buildCareerAgentPilotPreviewFromState(value, preflight)
	if err != nil {
		t.Fatal(err)
	}
	if route := responder.routeResumeForVacancy(value); route.Status != careeragent.RouteReviewRequired || route.ReasonCode != careeragent.RouteReasonAmbiguous {
		t.Fatalf("route changed from review-required ambiguity: %+v", route)
	}
	if preview.Status != applicationpilot.StatusBlocked || preview.Artifact.FinalDecision != string(applicationprocessing.DecisionReviewRequired) {
		t.Fatalf("ambiguous route was not kept blocked for review: %+v", preview)
	}
	if preview.Artifact.CoverLetter != "" || preview.Artifact.ContentHash != "" || preview.Artifact.Nonce != "" {
		t.Fatalf("advisory route created approval material: %+v", preview.Artifact)
	}
	if aiCalls.Load() != 1 {
		t.Fatalf("advisory route used %d AI calls, want exactly one", aiCalls.Load())
	}
}

func TestCareerAgentPilotOptionalCoverLetterOmissionSkipsGenerationAndPersistsEmptyPreparation(t *testing.T) {
	var aiCalls atomic.Int32
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"recommendation":"APPLY","reasons":["matches"],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	store := &durableWorkflowStoreFixture{}
	responder := &HHAIResponder{
		ctx:                         context.Background(),
		ai:                          NewAIClient(context.Background(), aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		careerWorkflowStore:         store,
		careerAgentMode:             "pilot",
		careerAgentCandidateID:      "candidate-1",
		careerAgentCandidateVersion: 3,
		careerAgentCandidateHash:    strings.Repeat("a", 64),
		resumes:                     []ResumeItem{{ProviderID: "provider-a", Hash: "hash-a", Title: "Python backend", Skills: "Python"}},
		careerAgentResumes:          []careeragent.ResumeProfile{{ID: "resume-a", ProviderID: "provider-a", Hash: "hash-a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true}},
		resumeFactsByHash:           map[string]ResumeFacts{"provider-a": {ExperienceText: "Python backend"}},
		minMatchScore:               65,
	}

	value := Vacancy{ID: 901, Name: "Python backend", Title: "Python backend", Description: "Python backend API", DataCompleteness: DataCompletenessFull}
	preflight := VacancyPreflight{
		VacancyID: 901, Available: true, ArchivedKnown: true,
		TestPresentKnown: true, LetterRequiredKnown: true, LetterRequired: false,
		LetterAllowedKnown: true, LetterAllowed: true, CanApplyKnown: true, CanApply: true,
		AlreadyRespondedEvidence:    AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded},
		SuitableResumesScanComplete: true, SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true,
		VacancyTypeKnown: true, VacancyTypeID: "open",
	}
	profile := responder.careerAgentResumes[0]
	preview, err := responder.buildCareerAgentPilotPreviewFromStateWithSelectionOptions(value, preflight, &profile, true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != pilotManualReviewStatus || preview.Artifact.CoverLetter != "" || preview.Artifact.ContentHash != contentHash("") {
		t.Fatalf("optional omission preview=%+v, want manual review with canonical empty content", preview)
	}
	if aiCalls.Load() != 1 {
		t.Fatalf("optional omission used %d AI calls, want analysis only", aiCalls.Load())
	}
	if len(store.preparations) != 1 {
		t.Fatalf("preparations=%d, want one durable preparation", len(store.preparations))
	}
	preparation := store.preparations[0]
	if preparation.CoverLetter != "" || preparation.CoverLetterHash != contentHash("") || preview.Artifact.PreparationID != preparation.ID || preview.Artifact.PreparationHash != preparation.InputFingerprint {
		t.Fatalf("empty preparation binding was not persisted: preparation=%+v artifact=%+v", preparation, preview.Artifact)
	}
	if err := preparation.Validate(); err != nil {
		t.Fatalf("empty preparation failed validation: %v", err)
	}
}
