package runtime

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
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

func TestResolveExplicitPilotResumeRequiresOneExactEnabledIdentity(t *testing.T) {
	profiles := []careeragent.ResumeProfile{
		{ID: "hh-resume-provider-id-provider-1", ProviderID: "provider-1", Hash: "hash-1", Title: "Support", Enabled: true},
		{ID: "hh-resume-provider-id-provider-2", ProviderID: "provider-2", Hash: "hash-2", Title: "Backend", Enabled: false},
	}

	for _, test := range []struct {
		name string
		id   string
		want string
		err  string
	}{
		{name: "stable id", id: "hh-resume-provider-id-provider-1", want: "Support"},
		{name: "provider id", id: "provider-1", want: "Support"},
		{name: "hash", id: "hash-1", want: "Support"},
		{name: "missing", id: "unknown", err: "not found"},
		{name: "disabled", id: "provider-2", err: "disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveExplicitPilotResume(profiles, test.id)
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("error=%v, want substring %q", err, test.err)
				}
				return
			}
			if err != nil || got.Title != test.want {
				t.Fatalf("profile=%+v error=%v, want title %q", got, err, test.want)
			}
		})
	}
}

func TestResolveExplicitPilotResumeRejectsAmbiguousExactIdentity(t *testing.T) {
	profiles := []careeragent.ResumeProfile{
		{ID: "resume-a", ProviderID: "provider-a", Hash: "shared", Title: "A", Enabled: true},
		{ID: "resume-b", ProviderID: "provider-b", Hash: "shared", Title: "B", Enabled: true},
	}
	if _, err := resolveExplicitPilotResume(profiles, "shared"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error=%v, want ambiguous identity error", err)
	}
}

func TestParseCareerAgentPilotArgsRejectsSearchWithExplicitResume(t *testing.T) {
	_, _, _, _, _, err := parseCareerAgentPilotArgs([]string{"--search", "--resume-id", "provider-1"})
	if err == nil || !strings.Contains(err.Error(), "--resume-id") {
		t.Fatalf("error=%v, want explicit resume/search rejection", err)
	}
}

func TestParseCareerAgentPilotArgsRejectsDuplicateExplicitResume(t *testing.T) {
	_, _, _, _, _, err := parseCareerAgentPilotArgs([]string{"--vacancy", "42", "--resume-id", "provider-1", "--resume-id=provider-1"})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("error=%v, want duplicate identity rejection", err)
	}
}

func TestVerifyExplicitPilotResumeIdentityRejectsProviderOrHashConflict(t *testing.T) {
	profile := careeragent.ResumeProfile{ProviderID: "provider-1", Hash: "hash-1"}
	for _, actual := range []ResumeItem{
		{ProviderID: "provider-2", Hash: "hash-1"},
		{ProviderID: "provider-1", Hash: "hash-2"},
		{},
	} {
		if err := verifyExplicitPilotResumeIdentity(profile, actual); err == nil {
			t.Fatalf("actual=%+v was accepted despite identity conflict", actual)
		}
	}
}

func TestExplicitPilotManualReviewGateNeverBecomesReadyForSend(t *testing.T) {
	preflight := VacancyPreflight{
		ArchivedKnown: true, CanApplyKnown: true, CanApply: true,
		TestPresentKnown: true, TestPresent: false, LetterRequiredKnown: true,
		SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true,
		SuitableResumesScanComplete: true,
	}
	if !pilotExplicitManualReviewReady(true, nil, nil, preflight, AlreadyRespondedNo, "hash") {
		t.Fatal("safe explicit selection was not eligible for manual review")
	}
	if pilotExplicitManualReviewReady(false, nil, nil, preflight, AlreadyRespondedNo, "hash") {
		t.Fatal("router-selected flow entered the explicit manual-review gate")
	}
	if pilotExplicitResumePreflightBlockReason(preflight) != "" {
		t.Fatalf("valid explicit provider preflight was blocked: %s", pilotExplicitResumePreflightBlockReason(preflight))
	}
}

func TestExplicitPilotAIAdvisoryCannotBlockManualReview(t *testing.T) {
	for _, test := range []struct {
		name       string
		explicit   bool
		assessment VacancyEvaluation
		decision   applicationprocessing.Decision
		blocked    bool
	}{
		{
			name:       "explicit low score",
			explicit:   true,
			assessment: VacancyEvaluation{Score: 60, Recommendation: vacancyanalysis.RecommendationDoNotApply},
			decision:   applicationprocessing.DecisionReject,
			blocked:    false,
		},
		{
			name:       "explicit unknown score",
			explicit:   true,
			assessment: VacancyEvaluation{Recommendation: vacancyanalysis.RecommendationUncertain},
			decision:   applicationprocessing.DecisionReviewRequired,
			blocked:    false,
		},
		{
			name:       "automatic low score",
			explicit:   false,
			assessment: VacancyEvaluation{Score: 60, Recommendation: vacancyanalysis.RecommendationDoNotApply},
			decision:   applicationprocessing.DecisionReject,
			blocked:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pilotAIBlocksPreview(test.explicit, test.assessment, test.decision, 65); got != test.blocked {
				t.Fatalf("pilotAIBlocksPreview()=%v, want %v", got, test.blocked)
			}
		})
	}
}

func TestExplicitPilotManualReviewKeepsHardBlockers(t *testing.T) {
	preflight := VacancyPreflight{
		ArchivedKnown: true, CanApplyKnown: true, CanApply: true,
		TestPresentKnown: true, TestPresent: false, LetterRequiredKnown: true,
		SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true,
		SuitableResumesScanComplete: true,
	}
	for _, test := range []struct {
		name        string
		hardMissing []string
		hardUnknown []string
	}{
		{name: "hard missing", hardMissing: []string{"Python"}},
		{name: "hard unknown", hardUnknown: []string{"SQL"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if pilotExplicitManualReviewReady(true, test.hardMissing, test.hardUnknown, preflight, AlreadyRespondedNo, "hash") {
				t.Fatal("explicit manual review ignored a hard blocker")
			}
		})
	}
}

func TestExplicitPilotRequiresExactProviderSuitabilityEvidence(t *testing.T) {
	base := VacancyPreflight{}
	for _, test := range []struct {
		name string
		edit func(*VacancyPreflight)
	}{
		{name: "scan incomplete", edit: func(value *VacancyPreflight) {
			value.SelectedResumeSuitableKnown = true
			value.SelectedResumeSuitable = true
		}},
		{name: "suitability unknown", edit: func(value *VacancyPreflight) { value.SuitableResumesScanComplete = true }},
		{name: "not suitable", edit: func(value *VacancyPreflight) {
			value.SuitableResumesScanComplete = true
			value.SelectedResumeSuitableKnown = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.edit(&value)
			if pilotExplicitResumePreflightBlockReason(value) == "" {
				t.Fatal("incomplete provider suitability evidence was accepted")
			}
		})
	}
	for _, value := range []VacancyPreflight{
		{SuitableResumesScanComplete: true, SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true, ResponseIdentifierPresent: true},
		{SuitableResumesScanComplete: true, SelectedResumeSuitableKnown: true, SelectedResumeSuitable: true, VacancyTypeKnown: true, VacancyTypeID: "direct"},
	} {
		if reason := pilotExplicitResumePreflightBlockReason(value); reason == "" {
			t.Fatalf("unsupported response path was accepted: %+v", value)
		}
	}
}

func TestExplicitPilotResponsePathClassification(t *testing.T) {
	base := VacancyPreflight{
		SuitableResumesScanComplete: true,
		SelectedResumeSuitableKnown: true,
		SelectedResumeSuitable:      true,
		VacancyTypeKnown:            true,
		VacancyTypeID:               "open",
	}

	tests := []struct {
		name        string
		mutate      func(*VacancyPreflight)
		wantBlocked bool
	}{
		{
			name: "standard applicant response URL is allowed",
			mutate: func(value *VacancyPreflight) {
				value.ResponseURL = "https://hh.ru/applicant/vacancy_response?vacancyId=123"
				value.ResponseIdentifierPresent = true
			},
			wantBlocked: false,
		},
		{
			name: "external response URL is blocked",
			mutate: func(value *VacancyPreflight) {
				value.ResponseURL = "https://external.example.com/apply"
				value.ResponseIdentifierPresent = true
			},
			wantBlocked: true,
		},
		{
			name:        "direct vacancy is blocked",
			mutate:      func(value *VacancyPreflight) { value.VacancyTypeID = "direct" },
			wantBlocked: true,
		},
		{
			name:        "closed vacancy is blocked",
			mutate:      func(value *VacancyPreflight) { value.VacancyTypeID = "closed" },
			wantBlocked: true,
		},
		{
			name: "API alternate URL remains diagnostic only",
			mutate: func(value *VacancyPreflight) {
				value.ApplyAlternateURL = "https://hh.ru/applicant/vacancy_response?vacancyId=123"
				value.ApplyAlternateURLPresent = true
			},
			wantBlocked: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			blocked := pilotExplicitResumePreflightBlockReason(value) != ""
			if blocked != test.wantBlocked {
				t.Fatalf("blocked=%v, want %v; preflight=%+v", blocked, test.wantBlocked, value)
			}
		})
	}
}

func TestExplicitPilotSuitabilityBridgeMergesOnlyFreshSuitabilityEvidence(t *testing.T) {
	providerID := "b29ec17dff103a8bc60039ed1f356c62486c37"
	source := &apiApplicationPreflightFake{
		detail:       hhread.VacancyRecord{ID: 137532422, SuitableResumesURL: "https://api.example/suitable"},
		suitableScan: &hhread.SuitableResumeScan{IDs: []string{providerID}, PagesChecked: 1, Complete: true},
	}
	responder := newBrowserExplicitSuitabilityResponder(source)
	preflight := VacancyPreflight{
		Archived: true, ArchivedKnown: true,
		AlreadyResponded: true, AlreadyRespondedKnown: true,
		CanApply: false, CanApplyKnown: true,
		TestPresent: true, TestPresentKnown: true,
		ResponseURL: "https://hh.ru/applicant/vacancy_response?vacancyId=137532422",
	}
	profile := careeragent.ResumeProfile{ProviderID: providerID, Hash: "browser-hash", Enabled: true}

	if err := responder.bridgeExplicitPilotSuitability(context.Background(), 137532422, profile, &preflight); err != nil {
		t.Fatal(err)
	}
	if !preflight.SuitableResumesScanComplete || !preflight.SelectedResumeSuitableKnown || !preflight.SelectedResumeSuitable || preflight.SuitableResumeIDsDiscovered != 1 {
		t.Fatalf("suitability evidence=%+v", preflight)
	}
	if !preflight.Archived || !preflight.ArchivedKnown || !preflight.AlreadyResponded || !preflight.AlreadyRespondedKnown || preflight.CanApply || !preflight.CanApplyKnown || !preflight.TestPresent || !preflight.TestPresentKnown || preflight.ResponseURL == "" {
		t.Fatalf("bridge changed browser evidence=%+v", preflight)
	}
}

func TestExplicitPilotSuitabilityBridgeRequiresExactProviderID(t *testing.T) {
	providerID := "b29ec17dff103a8bc60039ed1f356c62486c37"
	profile := careeragent.ResumeProfile{ProviderID: providerID, Hash: "browser-hash", Enabled: true}

	for _, test := range []struct {
		name string
		scan *hhread.SuitableResumeScan
		want string
	}{
		{name: "selected provider is present", scan: &hhread.SuitableResumeScan{IDs: []string{providerID}, PagesChecked: 1, Complete: true}, want: ""},
		{name: "selected provider is absent", scan: &hhread.SuitableResumeScan{IDs: []string{"other-provider"}, PagesChecked: 1, Complete: true}, want: "explicit resume provider suitability scan unavailable"},
		{name: "scan is incomplete", scan: &hhread.SuitableResumeScan{IDs: []string{providerID}, PagesChecked: 1, Complete: false}, want: "explicit resume provider suitability scan unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := &apiApplicationPreflightFake{
				detail:       hhread.VacancyRecord{ID: 137532422, SuitableResumesURL: "https://api.example/suitable"},
				suitableScan: test.scan,
			}
			responder := newBrowserExplicitSuitabilityResponder(source)
			preflight := VacancyPreflight{}
			err := responder.bridgeExplicitPilotSuitability(context.Background(), 137532422, profile, &preflight)
			if test.want == "" {
				if err != nil || !preflight.SelectedResumeSuitable {
					t.Fatalf("err=%v preflight=%+v", err, preflight)
				}
				return
			}
			if err == nil || pilotExplicitResumePreflightBlockReason(preflight) != test.want {
				t.Fatalf("err=%v preflight=%+v reason=%q", err, preflight, pilotExplicitResumePreflightBlockReason(preflight))
			}
		})
	}
}

func TestExplicitPilotSuitabilityBridgeBlocksWithoutAPITransport(t *testing.T) {
	profile := careeragent.ResumeProfile{ProviderID: "b29ec17dff103a8bc60039ed1f356c62486c37", Enabled: true}
	preflight := VacancyPreflight{}
	responder := &HHAIResponder{ctx: context.Background(), transport: transportBrowser}

	if err := responder.bridgeExplicitPilotSuitability(context.Background(), 137532422, profile, &preflight); err == nil || pilotExplicitResumePreflightBlockReason(preflight) != "explicit resume provider suitability scan unavailable" {
		t.Fatalf("err=%v preflight=%+v reason=%q", err, preflight, pilotExplicitResumePreflightBlockReason(preflight))
	}
}

func TestExplicitPilotSuitabilityBridgeRejectsProviderIdentityResolutionFailure(t *testing.T) {
	responder := newBrowserExplicitSuitabilityResponder(&apiApplicationPreflightFake{})
	preflight := VacancyPreflight{}
	profile := careeragent.ResumeProfile{HHID: 272272326, Title: "Технический специалист", Enabled: true}

	if err := responder.bridgeExplicitPilotSuitability(context.Background(), 137532422, profile, &preflight); err == nil || pilotExplicitResumePreflightBlockReason(preflight) != "explicit resume provider suitability scan unavailable" {
		t.Fatalf("err=%v preflight=%+v reason=%q", err, preflight, pilotExplicitResumePreflightBlockReason(preflight))
	}
}

func TestExplicitPilotSuitabilityBridgeIsNotUsedByNonExplicitSelection(t *testing.T) {
	calls := 0
	responder := &HHAIResponder{
		ctx:       context.Background(),
		transport: transportBrowser,
		apiReadFactory: func(url.Values) (hhreadport.HHReadSource, error) {
			calls++
			return nil, errors.New("unexpected suitability bridge call")
		},
	}
	if err := responder.bridgeExplicitPilotSuitabilityForSelection(context.Background(), 137532422, nil, &VacancyPreflight{}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("non-explicit selection invoked API suitability bridge %d times", calls)
	}
}

func newBrowserExplicitSuitabilityResponder(source *apiApplicationPreflightFake) *HHAIResponder {
	return &HHAIResponder{
		ctx:       context.Background(),
		transport: transportBrowser,
		apiReadFactory: func(url.Values) (hhreadport.HHReadSource, error) {
			return source, nil
		},
	}
}

func TestExplicitPilotPreflightBindsTheSelectedProviderResume(t *testing.T) {
	source := &apiApplicationPreflightFake{detail: hhread.VacancyRecord{
		ID: 42, SuitableResumesURL: "https://api.example/suitable", TypeID: "open", TypeIDKnown: true,
		ArchivedKnown: true, Archived: false, UserTestPresentKnown: true,
	}, suitableIDs: []string{"provider-selected"}}
	responder := newAPIApplicationPreflightResponder(source)
	responder.resumeIdentifier = "default-provider"
	responder.careerAgentResumes = []careeragent.ResumeProfile{{ID: "resume-selected", ProviderID: "provider-selected", Enabled: true}}
	profile := responder.careerAgentResumes[0]
	preflight, err := responder.getCareerAgentPilotPreflight(Vacancy{ID: 42}, &profile)
	if err != nil || !preflight.SelectedResumeSuitableKnown || !preflight.SelectedResumeSuitable {
		t.Fatalf("preflight=%+v error=%v, want selected provider suitable", preflight, err)
	}
	if responder.resumeIdentifier != "default-provider" {
		t.Fatalf("resume identifier was not restored: %q", responder.resumeIdentifier)
	}
}

func TestExplicitPilotArtifactAlwaysUsesManualReviewTelemetry(t *testing.T) {
	artifact := PilotArtifact{
		ResumeSelectionBasis:     pilotResumeSelectionOperatorExplicit,
		SelectedResumeID:         "hh-resume-provider-id-provider-1",
		SelectedResumeProviderID: "provider-1",
		RouterStatus:             careeragent.RouteReviewRequired,
		RouterReasonCode:         careeragent.RouteReasonOutOfScope,
	}
	if artifact.ResumeSelectionBasis != "OPERATOR_EXPLICIT" || artifact.RouterReasonCode != careeragent.RouteReasonOutOfScope {
		t.Fatalf("artifact telemetry=%+v", artifact)
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

func TestPilotManualReviewEligibilityIsIndependentOfAIAdvisory(t *testing.T) {
	tests := []struct {
		name           string
		score          int
		recommendation string
	}{
		{name: "low score uncertain", score: 45, recommendation: vacancyanalysis.RecommendationUncertain},
		{name: "below threshold do not apply", score: 55, recommendation: vacancyanalysis.RecommendationDoNotApply},
		{name: "threshold apply", score: 65, recommendation: vacancyanalysis.RecommendationApply},
		{name: "high score apply", score: 75, recommendation: vacancyanalysis.RecommendationApply},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := VacancyEvaluation{Score: test.score, Recommendation: test.recommendation}
			if !pilotManualReviewEligible(assessment, applicationprocessing.DecisionReject, 65) {
				t.Fatalf("manual review blocked advisory score=%d recommendation=%s", test.score, test.recommendation)
			}
		})
	}
}

func TestPilotManualReviewEligibilityKeepsHardRequirementBlockers(t *testing.T) {
	for _, status := range []string{hardRequirementStatusMissing, hardRequirementStatusUnknown} {
		t.Run(status, func(t *testing.T) {
			assessment := VacancyEvaluation{Score: 75, Recommendation: vacancyanalysis.RecommendationApply, HardRequirements: []HardRequirementEvaluation{{Requirement: "Kafka", Status: status}}}
			if pilotManualReviewEligible(assessment, applicationprocessing.DecisionReject, 65) {
				t.Fatalf("manual review ignored hard requirement status %q", status)
			}
		})
	}
}

func TestPilotAutomaticReadinessKeepsItsAIGates(t *testing.T) {
	if pilotReadyForExplicitSend(VacancyEvaluation{Score: 45, Recommendation: vacancyanalysis.RecommendationUncertain}, applicationprocessing.DecisionReject, 65) {
		t.Fatal("low-score advisory unexpectedly became automatically sendable")
	}
	if pilotReadyForExplicitSend(VacancyEvaluation{Score: 75, Recommendation: vacancyanalysis.RecommendationDoNotApply}, applicationprocessing.DecisionReviewRequired, 65) {
		t.Fatal("do-not-apply advisory unexpectedly became automatically sendable")
	}
	if !pilotReadyForExplicitSend(VacancyEvaluation{Score: 75, Recommendation: vacancyanalysis.RecommendationApply}, applicationprocessing.DecisionMatch, 65) {
		t.Fatal("existing strong APPLY automatic path was changed")
	}
}
