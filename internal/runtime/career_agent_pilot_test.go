package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type reset6AEducationReader struct {
	description       string
	applicability     applicationprocessing.Applicability
	applicabilityRead int
}

func (r *reset6AEducationReader) ReadDescription(context.Context, int) (string, error) {
	return r.description, nil
}

func (r *reset6AEducationReader) ReadApplicability(context.Context, vacancy.Vacancy) (applicationprocessing.Applicability, error) {
	r.applicabilityRead++
	return r.applicability, nil
}

func (r *reset6AEducationReader) ReadTest(context.Context, int) (applicationprocessing.TestSnapshot, error) {
	return applicationprocessing.TestSnapshot{}, nil
}

type reset6AEducationCandidate struct{}

func (reset6AEducationCandidate) ResolveForVacancy(context.Context, vacancy.Vacancy, string) (candidatecontext.CandidateContext, error) {
	return candidatecontext.CandidateContext{}, nil
}

type reset6AEducationAnalyzer struct {
	calls int
}

func (a *reset6AEducationAnalyzer) Analyze(_ context.Context, input vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	a.calls++
	requirement := vacancyanalysis.HardRequirementCandidate{
		Requirement:     "СПО/высшее/магистратура",
		Category:        vacancyanalysis.HardRequirementCategoryEducation,
		VacancyEvidence: "Требуется СПО/высшее/магистратура",
	}
	return vacancyanalysis.Assessment{
		Score:            90,
		Apply:            true,
		Recommendation:   vacancyanalysis.RecommendationApply,
		Reasons:          []string{},
		Missing:          []string{},
		HardRequirements: vacancyanalysis.DeriveHardRequirements(input.Candidate, input.Vacancy, input.Description, []vacancyanalysis.HardRequirementCandidate{requirement}),
	}, nil
}

func TestReset6AEducationFalseNegativeReachesAnalysisAndReadOnlyApplicability(t *testing.T) {
	route := careeragent.RouteResume(careeragent.VacancyInput{
		ID:             137532422,
		Title:          "Специалист технической поддержки (офис)",
		RequiredSkills: []string{"СПО/высшее/магистратура"},
		Description:    "Поддержка пользователей в офисе",
	}, []careeragent.ResumeProfile{{
		ID:          "support",
		Title:       "Технический специалист",
		DesiredRole: "Technical support",
		Skills:      []string{"support", "diagnostics", "СПО"},
		Enabled:     true,
	}})
	if route.Status != careeragent.RouteSelected || route.SelectedResumeTitle != "Технический специалист" {
		t.Fatalf("real-shaped support route was not selected: %+v", route)
	}

	reader := &reset6AEducationReader{
		description: "Требуется СПО/высшее/магистратура",
		applicability: applicationprocessing.Applicability{
			AlreadyRespondedKnown:        true,
			AlreadyRespondedValue:        string(AlreadyRespondedNo),
			AlreadyRespondedEvidenceCode: string(EvidenceExplicitNotResponded),
			CanApply:                     true,
			CanApplyKnown:                true,
			ArchivedKnown:                true,
			TestPresentKnown:             true,
			LetterRequiredKnown:          true,
		},
	}
	analyzer := &reset6AEducationAnalyzer{}
	service := applicationprocessing.NewService(applicationprocessing.Dependencies{
		Vacancies: reader,
		Candidate: reset6AEducationCandidate{},
		Analyzer:  analyzer,
		Policy:    rootApplicationPolicy{responder: &HHAIResponder{minMatchScore: 65}},
	})

	result, err := service.Prepare(context.Background(), applicationprocessing.Request{
		Vacancy: vacancy.Vacancy{ID: 137532422, Name: "Специалист технической поддержки (офис)", Links: map[string]string{"desktop": "https://hh.example/vacancy/137532422"}},
		Candidate: vacancyanalysis.CandidateFacts{
			ResumeTitle:      "Технический специалист",
			EducationKnown:   true,
			EducationLevel:   "среднее профессиональное",
			EducationDetails: "Екатеринбургский монтажный колледж, Информационные системы и программирование",
		},
		LetterFacts: vacancyanalysisToCoverLetterFacts(vacancyanalysis.CandidateFacts{ResumeTitle: "Технический специалист"}),
	})
	if err != nil {
		t.Fatalf("application preparation failed: %v", err)
	}
	if analyzer.calls != 1 {
		t.Fatalf("vacancy-analysis boundary calls=%d, want 1", analyzer.calls)
	}
	if reader.applicabilityRead != 1 || result.Outcome != applicationprocessing.OutcomePrepared {
		t.Fatalf("education false negative still stopped read-only pipeline: outcome=%s reason=%q applicability_reads=%d analysis=%+v", result.Outcome, result.Reason, reader.applicabilityRead, result.Analysis)
	}
}

func vacancyanalysisToCoverLetterFacts(value vacancyanalysis.CandidateFacts) coverletter.CandidateFacts {
	return coverletter.CandidateFacts{ResumeTitle: value.ResumeTitle, Skills: value.Skills}
}

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
		{"already responded", VacancyPreflight{AlreadyResponded: true, AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedYes, EvidenceCode: EvidenceExplicitRespondedMarker}}, "ALREADY_RESPONDED"},
		{"response unknown", VacancyPreflight{}, "ALREADY_RESPONDED_UNKNOWN"},
		{"cannot apply", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApplyKnown: true}, "CAN_APPLY_FALSE"},
		{"active unknown", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: true, CanApplyKnown: true}, "VACANCY_ACTIVE_UNKNOWN"},
		{"inactive", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: true, CanApplyKnown: true, Archived: true, ArchivedKnown: true}, "VACANCY_INACTIVE"},
		{"test required", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: true, CanApplyKnown: true, ArchivedKnown: true, TestPresent: true, TestPresentKnown: true}, "TEST_REQUIRED_UNSUPPORTED"},
		{"letter allowed unknown", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: true, CanApplyKnown: true, ArchivedKnown: true, TestPresentKnown: true, LetterRequired: true, LetterRequiredKnown: true}, "COVER_LETTER_ALLOWED_UNKNOWN"},
		{"letter not allowed", VacancyPreflight{AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: true, CanApplyKnown: true, ArchivedKnown: true, TestPresentKnown: true, LetterRequired: true, LetterRequiredKnown: true, LetterAllowedKnown: true}, "COVER_LETTER_NOT_ALLOWED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pilotPreflightBlockReason(test.state); got != test.reason {
				t.Fatalf("reason=%q, want %q", got, test.reason)
			}
		})
	}
	if got := pilotPreflightBlockReason(VacancyPreflight{AlreadyResponded: responded, AlreadyRespondedKnown: true, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedNo, EvidenceCode: EvidenceExplicitNotResponded}, CanApply: canApply, CanApplyKnown: true, ArchivedKnown: true, TestPresent: testRequired, TestPresentKnown: true, LetterRequired: letterRequired, LetterRequiredKnown: true, LetterAllowed: true, LetterAllowedKnown: true}); got != "" {
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
