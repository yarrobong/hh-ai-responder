package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/platform"
	applicationpilot "hh-ai-responder/internal/usecase/applicationpilot"
	applicationprocessing "hh-ai-responder/internal/usecase/applicationprocessing"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	"hh-ai-responder/internal/usecase/applicationsubmission"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

const (
	pilotArtifactVersion   = 1
	pilotReadyStatus       = applicationpilot.StatusReady
	pilotCoverLetterPrompt = `Для этого контролируемого pilot-preview подготовь короткое письмо под эту вакансию.
Используй только явно подтверждённые факты из canonical employer-safe context и выбранного резюме.
Не заявляй длительность или уровень коммерческого опыта, production-опыт, технологии, проекты или обязанности, если они прямо не подтверждены.
Не называй неподтверждённые технологии даже в контексте готовности их изучить; в частности, не упоминай FastAPI, если он не подтверждён.
Не упоминай Career Agent, AI, автоматизацию отклика, оценки или внутренние решения.
Не добавляй технические пояснения, метакомментарии, приветствие-плейсхолдер или markdown.`
)

// PilotArtifact is the private, exact approval material produced by 30A. It
// contains no cookies, tokens, prompts, or candidate context. The file is
// ignored by git and is only a bridge to a later explicit 30B command.
type PilotArtifact struct {
	Version             int                         `json:"version"`
	Status              string                      `json:"status"`
	VacancyID           int                         `json:"vacancy_id"`
	Vacancy             Vacancy                     `json:"vacancy"`
	SelectedResumeID    string                      `json:"selected_resume_id"`
	SelectedResumeHash  string                      `json:"selected_resume_hash"`
	SelectedResumeTitle string                      `json:"selected_resume_title"`
	AlternativeScores   []careeragent.ResumeScore   `json:"alternative_resume_scores,omitempty"`
	AIScore             *int                        `json:"ai_score,omitempty"`
	AIRecommendation    string                      `json:"ai_recommendation,omitempty"`
	AIReasons           []string                    `json:"ai_recommendation_reasons,omitempty"`
	HardRequirements    []HardRequirementEvaluation `json:"hard_requirements,omitempty"`
	HardMissing         []string                    `json:"hard_missing,omitempty"`
	HardUnknown         []string                    `json:"hard_unknown,omitempty"`
	FinalDecision       string                      `json:"final_decision"`
	FinalReason         string                      `json:"final_reason,omitempty"`
	Preflight           PilotPreflightSnapshot      `json:"preflight"`
	CoverLetter         string                      `json:"cover_letter,omitempty"`
	ContentHash         string                      `json:"content_hash,omitempty"`
	Nonce               string                      `json:"nonce,omitempty"`
	NonceUsedAt         *time.Time                  `json:"nonce_used_at,omitempty"`
	PreviewFreshAt      time.Time                   `json:"preview_fresh_at"`
}

type PilotPreflightSnapshot struct {
	ObservedAt          time.Time `json:"observed_at"`
	Active              *bool     `json:"active"`
	AlreadyResponded    *bool     `json:"already_responded"`
	CanApply            *bool     `json:"can_apply"`
	TestRequired        *bool     `json:"test_required"`
	CoverLetterRequired *bool     `json:"cover_letter_required"`
	CoverLetterAllowed  *bool     `json:"cover_letter_allowed"`
	ResponseURL         string    `json:"response_url,omitempty"`
	Area                string    `json:"area,omitempty"`
	WorkSchedule        string    `json:"work_schedule,omitempty"`
	WorkExperience      string    `json:"work_experience,omitempty"`
}

type PilotPreview struct {
	Artifact PilotArtifact
	Status   string
	Reasons  []string
}

func careerAgentPilotPath(cfg Config) string {
	base := strings.TrimSpace(cfg.CareerAgentResultPath)
	if base == "" {
		return "career_agent_pilot.json"
	}
	return base + ".pilot.json"
}

func runCareerAgentPilotCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "send" {
		return runCareerAgentPilotSend(args[1:], cfg, stdout, stderr)
	}
	fs := flag.NewFlagSet("career-agent pilot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	vacancyID := 0
	fs.IntVar(&vacancyID, "vacancy", 0, "HH vacancy id for the read-only pilot preview")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || vacancyID <= 0 {
		return errors.New("usage: career-agent pilot --vacancy <id>")
	}

	// 30A is always read-only, independent of HH_AUTO_* and write flags in the
	// operator environment. HHReadOnly is an additional transport guard.
	configureCareerAgentPilotPreview(&cfg)
	if logger == nil {
		logger = NewLogger(stderr, parseLogLevel(cfg.LogLevel))
	}
	responder, err := NewHHAIResponder(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer responder.closeResources()
	responder.loadCareerAgentRegistry(cfg.ResumeRegistryPath)
	preview, err := responder.buildCareerAgentPilotPreview(vacancyID)
	if err != nil {
		return err
	}
	if err := savePilotArtifact(careerAgentPilotPath(cfg), preview.Artifact); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, renderCareerAgentPilotPreview(preview))
	return err
}

func (r *HHAIResponder) buildCareerAgentPilotPreview(vacancyID int) (PilotPreview, error) {
	ctx := ctxOrBackground(r.ctx)
	reader := r.hhReadClient()
	if reader == nil {
		return PilotPreview{}, errors.New("HH read client is not configured")
	}
	// Do not use the Career Agent search cache here. This is the explicit 30A
	// detail read requested by the operator.
	record, err := reader.ReadVacancyDetail(ctx, vacancyID)
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot vacancy detail read failed: %w", err)
	}
	value, err := mapHHVacancy(record)
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot vacancy detail decode failed: %w", err)
	}
	if value.ID != vacancyID {
		return PilotPreview{}, fmt.Errorf("pilot vacancy identity mismatch: requested %d, received %d", vacancyID, value.ID)
	}
	// Read the response page before routing/AI as part of the explicit fresh
	// 30A snapshot. This keeps the preview complete even when routing itself
	// later requires review.
	preflight, err := r.GetVacancyPreflight(value)
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot application preflight read failed: %w", err)
	}

	// The router is rebuilt from the freshly loaded /applicant/my_resumes data;
	// no resume ID is hardcoded from Stage 29.6.
	route := r.routeResumeForVacancy(value)
	artifact := PilotArtifact{Version: pilotArtifactVersion, Status: applicationpilot.StatusBlocked, VacancyID: vacancyID, Vacancy: value, SelectedResumeID: route.SelectedResumeID, SelectedResumeTitle: route.SelectedResumeTitle, AlternativeScores: append([]careeragent.ResumeScore(nil), route.AlternativeScores...), Preflight: pilotPreflightSnapshot(preflight), PreviewFreshAt: time.Now().UTC()}
	preview := PilotPreview{Artifact: artifact, Status: applicationpilot.StatusBlocked, Reasons: append([]string(nil), route.Reasons...)}
	if route.Status != careeragent.RouteSelected || route.Confidence == careeragent.ConfidenceLow {
		preview.Reasons = append(preview.Reasons, "resume router requires review: "+strings.Join(route.Reasons, "; "))
		return preview, nil
	}
	selectedHash := r.resumeHashForProfile(route.SelectedResumeID)
	if selectedHash == "" {
		preview.Reasons = append(preview.Reasons, "selected resume is not available in the fresh resume read")
		return preview, nil
	}
	selectedResume, candidate, resolver, err := r.activateResume(selectedHash)
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot selected resume read failed: %w", err)
	}
	artifact.SelectedResumeHash = selectedResume.Hash
	artifact.SelectedResumeTitle = selectedResume.Title

	description := value.Description
	if strings.TrimSpace(description) == "" {
		return PilotPreview{}, errors.New("pilot vacancy detail has no description")
	}
	assessment, err := rootApplicationAnalyzer{client: r.ai}.Analyze(ctx, vacancyanalysis.Input{
		Candidate: vacancyanalysis.CandidateFacts{FullName: candidate.FullName, ResumeTitle: selectedResume.Title, Salary: candidate.Salary, Experience: candidate.Experience, Skills: candidate.Skills, Location: candidate.Location, EducationKnown: candidate.EducationKnown, EducationLevel: candidate.EducationLevel, EducationDetails: candidate.EducationDetails, TotalExperienceMonthsKnown: candidate.TotalExperienceMonthsKnown, TotalExperienceMonths: candidate.TotalExperienceMonths, Profile: candidate.Profile, SafeContext: candidate.SafeContext},
		Vacancy:   value, Description: description, Salary: vacancy.FormatCompensation(&value.Compensation), Location: value.Area.Name, WorkSchedule: value.WorkSchedule, IncludeKeywords: append([]string(nil), r.includeKeywords...),
	})
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot AI analysis failed: %w", err)
	}
	score := assessment.Score
	artifact.AIScore = &score
	artifact.AIRecommendation = assessment.Recommendation
	artifact.AIReasons = append([]string(nil), assessment.RecommendationReasons...)

	// The fresh preflight above is authoritative for the pilot snapshot; the AI
	// assessment remains advisory and cannot override it.
	app := applicationprocessing.Applicability{Available: preflight.Available, Archived: preflight.Archived, ArchivedKnown: preflight.ArchivedKnown, AlreadyResponded: preflight.AlreadyResponded, AlreadyRespondedKnown: preflight.AlreadyRespondedKnown, TestPresent: preflight.TestPresent, TestPresentKnown: preflight.TestPresentKnown, LetterRequired: preflight.LetterRequired, LetterRequiredKnown: preflight.LetterRequiredKnown, CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown, Area: preflight.Area, AreaKnown: preflight.AreaKnown, WorkSchedule: preflight.WorkSchedule, WorkScheduleKnown: preflight.WorkScheduleKnown, WorkExperience: preflight.WorkExperience, WorkExperienceKnown: preflight.WorkExperienceKnown, ResponseURL: preflight.ResponseURL}
	candidateFacts := vacancyanalysis.CandidateFacts{FullName: candidate.FullName, ResumeTitle: selectedResume.Title, Salary: candidate.Salary, Experience: candidate.Experience, Skills: candidate.Skills, Location: candidate.Location, EducationKnown: candidate.EducationKnown, EducationLevel: candidate.EducationLevel, EducationDetails: candidate.EducationDetails, TotalExperienceMonthsKnown: candidate.TotalExperienceMonthsKnown, TotalExperienceMonths: candidate.TotalExperienceMonths, Profile: candidate.Profile, SafeContext: candidate.SafeContext}
	assessment, decision, reason, err := (rootApplicationPolicy{responder: r}).ReconcileApplicability(value, app, candidateFacts, assessment)
	if err != nil {
		return PilotPreview{}, fmt.Errorf("pilot local decision failed: %w", err)
	}
	artifact.HardMissing = append([]string(nil), hardRequirementsMissing(assessment)...)
	artifact.HardUnknown = append([]string(nil), hardRequirementsUnknown(assessment)...)
	artifact.HardRequirements = append([]HardRequirementEvaluation(nil), assessment.HardRequirements...)
	artifact.FinalDecision, artifact.FinalReason = string(decision), reason
	preview.Reasons = append(preview.Reasons, reason)

	letterInput := coverLetterInput(value, description, candidate, &assessment, strings.TrimSpace(strings.Join([]string{r.extraLetterPrompt, pilotCoverLetterPrompt}, "\n")), r.coverLetterSemanticExamples(value, description, assessment, resolver))
	letter, letterErr := rootApplicationCoverLetter{client: r.ai}.Generate(ctx, letterInput)
	if letterErr != nil {
		preview.Reasons = append(preview.Reasons, "cover-letter preview failed: "+letterErr.Error())
	} else {
		if qualityErr := validatePilotCoverLetter(letter.Letter); qualityErr != nil {
			preview.Reasons = append(preview.Reasons, "cover-letter preview failed: "+qualityErr.Error())
		} else {
			artifact.CoverLetter = letter.Letter
			hash := sha256.Sum256([]byte(letter.Letter))
			artifact.ContentHash = hex.EncodeToString(hash[:])
		}
	}

	if preflight.ArchivedKnown && preflight.Archived {
		preview.Reasons = append(preview.Reasons, "vacancy is archived")
	}
	if preflight.AlreadyRespondedKnown && preflight.AlreadyResponded {
		preview.Reasons = append(preview.Reasons, "already responded")
	}
	if preflight.CanApplyKnown && !preflight.CanApply {
		preview.Reasons = append(preview.Reasons, "vacancy does not allow an application")
	}
	if preflight.TestPresentKnown && preflight.TestPresent {
		preview.Reasons = append(preview.Reasons, "test is required and pilot test submission is unsupported")
	}
	if !preflight.LetterRequiredKnown {
		preview.Reasons = append(preview.Reasons, "cover-letter requirement is unknown")
	}
	if len(artifact.HardMissing) > 0 {
		preview.Reasons = append(preview.Reasons, "hard requirement missing: "+strings.Join(artifact.HardMissing, "; "))
	}

	if decision == applicationprocessing.DecisionMatch && len(artifact.HardMissing) == 0 && len(artifact.HardUnknown) == 0 && preflight.ArchivedKnown && !preflight.Archived && preflight.AlreadyRespondedKnown && !preflight.AlreadyResponded && preflight.CanApplyKnown && preflight.CanApply && preflight.TestPresentKnown && !preflight.TestPresent && preflight.LetterRequiredKnown && artifact.ContentHash != "" {
		artifact.Nonce, err = generateUUIDv4()
		if err != nil {
			return PilotPreview{}, fmt.Errorf("pilot nonce generation failed: %w", err)
		}
		artifact.Status = pilotReadyStatus
		preview.Status = pilotReadyStatus
	} else {
		artifact.Status = applicationpilot.StatusBlocked
		preview.Status = applicationpilot.StatusBlocked
	}
	preview.Artifact = artifact
	return preview, nil
}

func pilotPreflightSnapshot(value VacancyPreflight) PilotPreflightSnapshot {
	return PilotPreflightSnapshot{ObservedAt: time.Now().UTC(), Active: knownBoolPointer(!value.Archived, value.ArchivedKnown), AlreadyResponded: knownBoolPointer(value.AlreadyResponded, value.AlreadyRespondedKnown), CanApply: knownBoolPointer(value.CanApply, value.CanApplyKnown), TestRequired: knownBoolPointer(value.TestPresent, value.TestPresentKnown), CoverLetterRequired: knownBoolPointer(value.LetterRequired, value.LetterRequiredKnown), CoverLetterAllowed: knownBoolPointer(value.CanApply, value.CanApplyKnown && value.CanApply), ResponseURL: value.ResponseURL, Area: value.Area, WorkSchedule: value.WorkSchedule, WorkExperience: value.WorkExperience}
}

func validatePilotCoverLetter(letter string) error {
	text := strings.ToLower(strings.TrimSpace(letter))
	if text == "" {
		return errors.New("empty cover-letter preview")
	}
	if strings.Contains(text, "приветствуйте") || strings.Contains(text, "career agent") || strings.Contains(text, "ai automation") || strings.Contains(text, "автоматизацией отклика") {
		return errors.New("contains a placeholder or internal automation reference")
	}
	if strings.Contains(text, "```") || strings.Contains(text, "\n-") || strings.HasPrefix(text, "{") {
		return errors.New("contains markdown or structured-output noise")
	}
	return nil
}

func savePilotArtifact(path string, artifact PilotArtifact) error {
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return platform.WritePrivateFileAtomic(path, append(raw, '\n'), ".career-agent-pilot-*.tmp")
}

func loadPilotArtifact(path string) (PilotArtifact, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PilotArtifact{}, err
	}
	var artifact PilotArtifact
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return PilotArtifact{}, fmt.Errorf("invalid pilot artifact: %w", err)
	}
	if artifact.Version != pilotArtifactVersion || artifact.VacancyID <= 0 || strings.TrimSpace(artifact.CoverLetter) == "" || strings.TrimSpace(artifact.ContentHash) == "" || strings.TrimSpace(artifact.Nonce) == "" {
		return PilotArtifact{}, errors.New("pilot artifact is incomplete")
	}
	return artifact, nil
}

func renderCareerAgentPilotPreview(preview PilotPreview) string {
	a := preview.Artifact
	aiScore := "unknown"
	if a.AIScore != nil {
		aiScore = fmt.Sprint(*a.AIScore)
	}
	company := a.Vacancy.Company.Name
	url := a.Vacancy.Links["desktop"]
	if url == "" {
		url = a.Vacancy.Links["desktop"]
	}
	lines := []string{"LIVE APPLICATION PILOT", "Vacancy: " + firstNonEmpty(a.Vacancy.Title, a.Vacancy.Name) + " (" + fmt.Sprint(a.VacancyID) + ")", "Company: " + company, "URL: " + url, "Current vacancy detail: " + renderPilotVacancyDetail(a.Vacancy), "Active: " + pointerWord(a.Preflight.Active), "Selected resume: " + a.SelectedResumeTitle, "Resume ID: " + a.SelectedResumeHash, "Alternative resume scores: " + renderResumeScores(a.AlternativeScores), "AI score: " + aiScore, "Recommendation: " + firstNonEmpty(a.AIRecommendation, "unknown"), "Hard requirements: " + renderHardRequirements(a.HardRequirements), "Hard missing: " + joinOrUnknown(a.HardMissing), "Hard unknown: " + joinOrUnknown(a.HardUnknown), "Already responded: " + pointerWord(a.Preflight.AlreadyResponded), "Can apply: " + pointerWord(a.Preflight.CanApply), "Test required: " + pointerWord(a.Preflight.TestRequired), "Cover letter: required=" + pointerWord(a.Preflight.CoverLetterRequired) + "; allowed=" + pointerWord(a.Preflight.CoverLetterAllowed), "Exact content hash: " + firstNonEmpty(a.ContentHash, "none"), "Nonce: " + firstNonEmpty(a.Nonce, "none"), "Write capability: DISABLED (30A preview)", "Freshness: " + a.PreviewFreshAt.UTC().Format(time.RFC3339), "STATUS: " + preview.Status}
	if len(preview.Reasons) > 0 {
		lines = append(lines, "Reason: "+strings.Join(uniqueStrings(preview.Reasons), "; "))
	}
	if a.CoverLetter != "" {
		lines = append(lines, "Cover-letter exact preview:", a.CoverLetter)
	}
	if preview.Status == pilotReadyStatus {
		lines = append(lines, "STATUS: READY_FOR_EXPLICIT_SEND", "HH writes = 0")
	} else {
		lines = append(lines, "HH writes = 0")
	}
	return strings.Join(lines, "\n") + "\n"
}

func pointerWord(value *bool) string {
	if value == nil {
		return "UNKNOWN"
	}
	if *value {
		return "YES"
	}
	return "NO"
}

func joinOrUnknown(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, "; ")
}

func renderResumeScores(values []careeragent.ResumeScore) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, firstNonEmpty(value.Title, value.ResumeID)+"="+fmt.Sprint(value.Score))
	}
	return strings.Join(parts, ", ")
}

func renderPilotVacancyDetail(value Vacancy) string {
	return fmt.Sprintf("title=%s; company=%s; area=%s; work_format=%s; employment=%s; schedule=%s; experience=%s; description_chars=%d; fetched fresh", firstNonEmpty(value.Title, value.Name), firstNonEmpty(value.Company.Name, "unknown"), firstNonEmpty(value.Area.Name, value.Location, "unknown"), firstNonEmpty(value.WorkFormat, "unknown"), firstNonEmpty(value.EmploymentType, "unknown"), firstNonEmpty(value.WorkSchedule, "unknown"), firstNonEmpty(value.WorkExperience, "unknown"), len([]rune(value.Description)))
}

func renderHardRequirements(values []HardRequirementEvaluation) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Requirement+"="+value.Status)
	}
	return strings.Join(parts, ", ")
}

func runCareerAgentPilotSend(args []string, cfg Config, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("career-agent pilot send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nonce := ""
	vacancyID := 0
	fs.StringVar(&nonce, "nonce", "", "nonce printed by the approved pilot preview")
	fs.IntVar(&vacancyID, "vacancy", 0, "expected HH vacancy id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(nonce) == "" {
		return errors.New("usage: career-agent pilot send --nonce <preview-nonce> [--vacancy <id>]")
	}
	if cfg.DryRun || !cfg.HHWriteEnabled {
		return errors.New("pilot send requires HH_DRY_RUN=false and HH_WRITE_ENABLED=true")
	}
	configureCareerAgentPilotSend(&cfg)
	if logger == nil {
		logger = NewLogger(stderr, parseLogLevel(cfg.LogLevel))
	}
	path := careerAgentPilotPath(cfg)
	artifact, err := loadPilotArtifact(path)
	if err != nil {
		return err
	}
	if nonce != artifact.Nonce || (vacancyID > 0 && vacancyID != artifact.VacancyID) || artifact.Status != pilotReadyStatus || artifact.NonceUsedAt != nil {
		return errors.New("pilot approval is stale, has a mismatched nonce, or is already used")
	}
	responder, err := NewHHAIResponder(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer responder.closeResources()
	responder.loadCareerAgentRegistry(cfg.ResumeRegistryPath)
	contentHash := sha256.Sum256([]byte(artifact.CoverLetter))
	if artifact.ContentHash != hex.EncodeToString(contentHash[:]) {
		return applicationpilot.ErrContentChanged
	}
	current, err := responder.buildCareerAgentPilotIdentity(artifact.VacancyID, artifact.ContentHash)
	if err != nil {
		return err
	}
	if err := applicationpilot.VerifyApproval(applicationpilot.Approval{VacancyID: artifact.VacancyID, ResumeID: artifact.SelectedResumeHash, ContentHash: artifact.ContentHash, Nonce: artifact.Nonce}, current); err != nil {
		return fmt.Errorf("pilot approval verification failed: %w", err)
	}
	if _, _, _, err := responder.activateResume(current.ResumeID); err != nil {
		return fmt.Errorf("pilot approved resume could not be activated: %w", err)
	}
	if err := markPilotNonceUsed(path, &artifact); err != nil {
		return err
	}
	result, _, submitErr := responder.submitPreparedApplication(applicationprocessing.PreparedApplication{VacancyID: artifact.VacancyID, Vacancy: artifact.Vacancy, ResumeID: artifact.SelectedResumeHash, ResumeTitle: artifact.SelectedResumeTitle, CoverLetter: artifact.CoverLetter})
	status := applicationpilot.OutcomeUnknown
	if submitErr != nil && result.Execution.Outcome == applicationsubmission.ExecutionRejected {
		status = applicationpilot.OutcomeFailed
	} else if submitErr == nil && result.Execution.Outcome == applicationsubmission.ExecutionAccepted {
		status = applicationpilot.OutcomeUnknown
		if reconciler := responder.applicationReconciliationService(); reconciler != nil && result.Execution.AttemptID != "" {
			reconciled, reconcileErr := reconciler.Reconcile(context.Background(), result.Execution.AttemptID)
			if reconcileErr == nil && reconciled.Status == applicationreconciliation.StatusConfirmed {
				status = applicationpilot.OutcomeConfirmed
			}
		}
	} else if result.Execution.Outcome == applicationsubmission.ExecutionDeliveryUncertain || result.Execution.TransportTried {
		status = applicationpilot.OutcomeUnknown
	}
	artifact.Status = status
	if err := savePilotArtifact(path, artifact); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "LIVE APPLICATION PILOT\nVacancy: %d\nSubmission outcome: %s\nNO AUTOMATIC RETRY: true\n", artifact.VacancyID, status)
	return errors.Join(submitErr, err)
}

func configureCareerAgentPilotPreview(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.DryRun, cfg.HHWriteEnabled = true, false
	cfg.HHReadOnly = true
	cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = false, false, false, false
	cfg.ChatMode, cfg.AutoApplyMode = "off", "off"
}

func configureCareerAgentPilotSend(cfg *Config) {
	if cfg == nil {
		return
	}
	// The explicit send has one application budget and one total HH-write
	// budget. Other write capabilities stay disabled even when their global
	// environment flags are enabled.
	cfg.DryRun = false
	cfg.HHWriteEnabled = true
	cfg.HHReadOnly = false
	cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = true, false, false, false
	cfg.ChatMode, cfg.AutoApplyMode = "off", "canary"
	cfg.HHMaxWritesPerRun, cfg.HHMaxWritesPerDay, cfg.MaxApplicationsPerRun = 1, 1, 1
}

func (r *HHAIResponder) buildCareerAgentPilotIdentity(vacancyID int, contentHash string) (applicationpilot.CurrentIdentity, error) {
	reader := r.hhReadClient()
	if reader == nil {
		return applicationpilot.CurrentIdentity{}, errors.New("HH read client is not configured")
	}
	record, err := reader.ReadVacancyDetail(ctxOrBackground(r.ctx), vacancyID)
	if err != nil {
		return applicationpilot.CurrentIdentity{}, err
	}
	value, err := mapHHVacancy(record)
	if err != nil {
		return applicationpilot.CurrentIdentity{}, err
	}
	route := r.routeResumeForVacancy(value)
	if route.Status != careeragent.RouteSelected {
		return applicationpilot.CurrentIdentity{}, errors.New("fresh resume router no longer selects the approved resume")
	}
	selectedHash := r.resumeHashForProfile(route.SelectedResumeID)
	preflight, err := r.GetVacancyPreflight(value)
	if err != nil {
		return applicationpilot.CurrentIdentity{}, err
	}
	if preflight.ArchivedKnown && preflight.Archived || preflight.AlreadyRespondedKnown && preflight.AlreadyResponded || preflight.CanApplyKnown && !preflight.CanApply || preflight.TestPresentKnown && preflight.TestPresent {
		return applicationpilot.CurrentIdentity{}, errors.New("fresh pilot preflight blocks the approved application")
	}
	return applicationpilot.CurrentIdentity{VacancyID: value.ID, ResumeID: selectedHash, ContentHash: contentHash}, nil
}

func markPilotNonceUsed(path string, artifact *PilotArtifact) error {
	if artifact == nil || artifact.NonceUsedAt != nil {
		return applicationpilot.ErrNonceConsumed
	}
	now := time.Now().UTC()
	artifact.NonceUsedAt = &now
	return savePilotArtifact(path, *artifact)
}
