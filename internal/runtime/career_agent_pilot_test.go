package runtime

import (
	"strings"
	"testing"
	"time"

	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
)

func TestCareerAgentPilotPreviewIsReadOnly(t *testing.T) {
	cfg := Config{
		DryRun:            false,
		HHWriteEnabled:    true,
		AutoApply:         true,
		AutoChat:          true,
		AutoTouch:         true,
		AutoJobStatus:     true,
		ChatMode:          "auto",
		AutoApplyMode:     "auto",
		HHMaxWritesPerRun: 10,
	}
	configureCareerAgentPilotPreview(&cfg)
	if !cfg.DryRun || cfg.HHWriteEnabled || !cfg.HHReadOnly || cfg.AutoApply || cfg.AutoChat || cfg.AutoTouch || cfg.AutoJobStatus || cfg.ChatMode != "off" || cfg.AutoApplyMode != "off" {
		t.Fatalf("preview must disable every HH writer: %+v", cfg)
	}
}

func TestCareerAgentPilotSendIsApplicationOnlyAndOneWrite(t *testing.T) {
	cfg := Config{AutoChat: true, AutoTouch: true, AutoJobStatus: true, HHMaxWritesPerRun: 10, HHMaxWritesPerDay: 10, MaxApplicationsPerRun: 10}
	configureCareerAgentPilotSend(&cfg)
	if cfg.DryRun || !cfg.HHWriteEnabled || cfg.HHReadOnly || !cfg.AutoApply || cfg.AutoChat || cfg.AutoTouch || cfg.AutoJobStatus || cfg.ChatMode != "off" || cfg.AutoApplyMode != "canary" {
		t.Fatalf("send must isolate the application writer: %+v", cfg)
	}
	if cfg.HHMaxWritesPerRun != 1 || cfg.HHMaxWritesPerDay != 1 || cfg.MaxApplicationsPerRun != 1 {
		t.Fatalf("send limits=%+v", cfg)
	}
}

func TestCareerAgentPilotPreviewRendersFreshStateAndZeroWrites(t *testing.T) {
	active := true
	responded := false
	canApply := true
	testRequired := false
	letterRequired := true
	letterAllowed := true
	preview := PilotPreview{
		Status: applicationpilot.StatusReady,
		Artifact: PilotArtifact{
			VacancyID:           137428040,
			Vacancy:             Vacancy{Title: "Python Backend Developer", Company: Company{Name: "Example"}, Links: map[string]string{"desktop": "https://hh.example/vacancy/137428040"}},
			SelectedResumeTitle: "Backend developer",
			SelectedResumeHash:  "resume-hash",
			Preflight:           PilotPreflightSnapshot{Active: &active, AlreadyResponded: &responded, CanApply: &canApply, TestRequired: &testRequired, CoverLetterRequired: &letterRequired, CoverLetterAllowed: &letterAllowed},
			PreviewFreshAt:      time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC),
			CoverLetter:         "Короткое письмо.",
		},
	}
	output := renderCareerAgentPilotPreview(preview)
	for _, fragment := range []string{"Active: YES", "Already responded: NO", "Can apply: YES", "Test required: NO", "HH writes = 0"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("preview output missing %q:\n%s", fragment, output)
		}
	}
}

func TestValidatePilotCoverLetterRejectsNoiseAndInternalReferences(t *testing.T) {
	for _, letter := range []string{"Приветствуйте!", "Письмо\n- пункт", "Использую Career Agent для отклика."} {
		if err := validatePilotCoverLetter(letter); err == nil {
			t.Fatalf("validatePilotCoverLetter(%q) unexpectedly passed", letter)
		}
	}
	if err := validatePilotCoverLetter("Здравствуйте! Мой опыт Python и Django соответствует задачам вакансии."); err != nil {
		t.Fatalf("valid pilot letter rejected: %v", err)
	}
}

func TestPilotPreflightBlockReasonFailsClosed(t *testing.T) {
	responded := false
	canApply := true
	testRequired := false
	letterRequired := false
	tests := []struct {
		name   string
		state  VacancyPreflight
		reason string
	}{
		{"already responded", VacancyPreflight{AlreadyResponded: true, AlreadyRespondedKnown: true}, "ALREADY_RESPONDED"},
		{"response unknown", VacancyPreflight{}, "ALREADY_RESPONDED_UNKNOWN"},
		{"cannot apply", VacancyPreflight{AlreadyRespondedKnown: true, CanApplyKnown: true}, "CAN_APPLY_FALSE"},
		{"active unknown", VacancyPreflight{AlreadyRespondedKnown: true, CanApply: true, CanApplyKnown: true}, "VACANCY_ACTIVE_UNKNOWN"},
		{"inactive", VacancyPreflight{AlreadyRespondedKnown: true, CanApply: true, CanApplyKnown: true, Archived: true, ArchivedKnown: true}, "VACANCY_INACTIVE"},
		{"test required", VacancyPreflight{AlreadyRespondedKnown: true, CanApply: true, CanApplyKnown: true, ArchivedKnown: true, TestPresent: true, TestPresentKnown: true}, "TEST_REQUIRED_UNSUPPORTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pilotPreflightBlockReason(test.state); got != test.reason {
				t.Fatalf("reason=%q, want %q", got, test.reason)
			}
		})
	}
	if got := pilotPreflightBlockReason(VacancyPreflight{AlreadyResponded: responded, AlreadyRespondedKnown: true, CanApply: canApply, CanApplyKnown: true, ArchivedKnown: true, TestPresent: testRequired, TestPresentKnown: true, LetterRequired: letterRequired, LetterRequiredKnown: true}); got != "" {
		t.Fatalf("eligible preflight reason=%q", got)
	}
}

func TestPilotAIEligibilityAllowsOnlyCleanAdvisoryReview(t *testing.T) {
	clean := VacancyEvaluation{Score: 65, Recommendation: vacancyanalysis.RecommendationUncertain}
	if !pilotAIEligibleForPreview(clean, applicationprocessing.DecisionReviewRequired, 65) {
		t.Fatal("clean advisory review should be eligible for explicit pilot preview")
	}
	if pilotAIEligibleForPreview(VacancyEvaluation{Score: 64, Recommendation: vacancyanalysis.RecommendationUncertain}, applicationprocessing.DecisionReviewRequired, 65) {
		t.Fatal("below-threshold advisory review must be blocked")
	}
	if pilotAIEligibleForPreview(VacancyEvaluation{Score: 90, Recommendation: vacancyanalysis.RecommendationUncertain, HardRequirements: []HardRequirementEvaluation{{Requirement: "Kafka", Status: hardRequirementStatusUnknown}}}, applicationprocessing.DecisionReviewRequired, 65) {
		t.Fatal("hard unknown advisory review must be blocked")
	}
	if pilotAIEligibleForPreview(VacancyEvaluation{Score: 90, Recommendation: vacancyanalysis.RecommendationDoNotApply}, applicationprocessing.DecisionReviewRequired, 65) {
		t.Fatal("do-not-apply review must be blocked")
	}
}
