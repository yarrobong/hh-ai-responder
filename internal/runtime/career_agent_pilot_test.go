package runtime

import (
	"strings"
	"testing"
	"time"

	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
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
