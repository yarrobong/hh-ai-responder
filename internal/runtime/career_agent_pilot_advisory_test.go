package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
