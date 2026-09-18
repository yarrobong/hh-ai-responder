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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/careeragent"
	appconfig "hh-ai-responder/internal/config"
	"hh-ai-responder/internal/platform"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
	attemptpolicy "hh-ai-responder/internal/usecase/applicationattemptpolicy"
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
	SelectedResumeHHID  int64                       `json:"selected_resume_hh_id,omitempty"`
	SelectedResumeHash  string                      `json:"selected_resume_hash"`
	SelectedResumeTitle string                      `json:"selected_resume_title"`
	RouterScore         int                         `json:"router_score,omitempty"`
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
	CandidatesChecked   int                         `json:"candidates_checked,omitempty"`
	BlockedCandidates   []PilotBlockedCandidate     `json:"blocked_candidates,omitempty"`
	SearchStats         PilotSearchStats            `json:"search_stats,omitempty"`
}

type PilotPreflightSnapshot struct {
	ObservedAt                   time.Time `json:"observed_at"`
	Active                       *bool     `json:"active"`
	AlreadyResponded             *bool     `json:"already_responded"`
	AlreadyRespondedValue        string    `json:"already_responded_value"`
	AlreadyRespondedEvidenceCode string    `json:"already_responded_evidence_code"`
	CanApply                     *bool     `json:"can_apply"`
	TestRequired                 *bool     `json:"test_required"`
	CoverLetterRequired          *bool     `json:"cover_letter_required"`
	CoverLetterAllowed           *bool     `json:"cover_letter_allowed"`
	ResponseURL                  string    `json:"response_url,omitempty"`
	Area                         string    `json:"area,omitempty"`
	WorkSchedule                 string    `json:"work_schedule,omitempty"`
	WorkExperience               string    `json:"work_experience,omitempty"`
}

type PilotPreview struct {
	Artifact          PilotArtifact
	Status            string
	Reasons           []string
	CandidatesChecked int
	BlockedCandidates []PilotBlockedCandidate
	SearchStats       PilotSearchStats
}

type PilotBlockedCandidate struct {
	VacancyID     int    `json:"vacancy_id"`
	Title         string `json:"title"`
	BlockedReason string `json:"blocked_reason"`
}

type PilotSearchStats struct {
	Scanned                        int                   `json:"scanned"`
	KnownRespondedSkipped          int                   `json:"known_responded_skipped"`
	FreshAlreadyRespondedSkipped   int                   `json:"fresh_already_responded_skipped"`
	UnrespondedFound               int                   `json:"unresponded_found"`
	UnrespondedEvaluated           int                   `json:"unresponded_evaluated"`
	DetailReads                    int                   `json:"detail_reads"`
	AIEvaluations                  int                   `json:"ai_evaluations"`
	UnresolvedAttemptSkipped       int                   `json:"unresolved_attempt_skipped"`
	PreflightUnknown               int                   `json:"preflight_unknown"`
	FreshUnresponded               int                   `json:"fresh_unresponded"`
	AlreadyRespondedEvidenceCounts map[string]int        `json:"already_responded_evidence_counts,omitempty"`
	PreflightAudits                []PilotPreflightAudit `json:"preflight_audits,omitempty"`
	BlockedReasonCounts            map[string]int        `json:"blocked_reason_counts,omitempty"`
}

type PilotPreflightAudit struct {
	VacancyID                    int    `json:"vacancy_id"`
	Title                        string `json:"title"`
	AlreadyResponded             string `json:"already_responded"`
	AlreadyRespondedEvidenceCode string `json:"already_responded_evidence_code"`
	CanApply                     *bool  `json:"can_apply"`
	TestRequired                 *bool  `json:"test_required"`
	Active                       *bool  `json:"active"`
	ResponseIdentifierPresent    bool   `json:"response_identifier_present"`
	NegotiationIdentifierPresent bool   `json:"negotiation_identifier_present"`
}

const pilotManualReviewStatus = "MANUAL_REVIEW_BEFORE_SEND"

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
	search := false
	fs.BoolVar(&search, "search", false, "find the first fresh pilot-eligible vacancy")
	maxScan := 100
	fs.IntVar(&maxScan, "max-scan", 100, "maximum unique vacancies to inspect cheaply")
	maxCandidates := 20
	fs.IntVar(&maxCandidates, "max-candidates", 20, "maximum fresh candidates to check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || maxScan <= 0 || maxScan > 1000 || maxCandidates <= 0 || maxCandidates > 20 || (vacancyID <= 0 && !search) || (vacancyID > 0 && search) {
		return errors.New("usage: career-agent pilot --search [--max-scan 1..1000] [--max-candidates 1..20] | career-agent pilot --vacancy <id>")
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
	var preview PilotPreview
	if search {
		preview, err = responder.findFirstCareerAgentPilotCandidate(maxScan, maxCandidates)
	} else {
		preview, err = responder.buildCareerAgentPilotPreview(vacancyID)
	}
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

	return r.buildCareerAgentPilotPreviewFromState(value, preflight)
}

// findFirstCareerAgentPilotCandidate is the bounded read-only pilot search.
// It deliberately performs the cheap provider response-state read before
// detail, routing, AI, and cover-letter preparation.
func (r *HHAIResponder) findFirstCareerAgentPilotCandidate(maxScan, maxCandidates int) (PilotPreview, error) {
	if maxScan <= 0 || maxScan > 1000 {
		return PilotPreview{}, errors.New("pilot search scan bound must be from 1 to 1000")
	}
	if maxCandidates <= 0 || maxCandidates > 20 {
		return PilotPreview{}, errors.New("pilot search candidate bound must be from 1 to 20")
	}
	if r == nil || r.hhReadClient() == nil {
		return PilotPreview{}, errors.New("HH read client is not configured")
	}
	r.loadAlreadyRespondedState()
	knownResponded := r.pilotKnownRespondedVacancies(ctxOrBackground(r.ctx))
	attempted := r.pilotAttemptedVacancies(ctxOrBackground(r.ctx))
	r.vacancySearchSources = map[int][]string{}
	r.careerAgentSearchSources = map[int][]careeragent.SearchProfileEvidence{}
	stats := PilotSearchStats{BlockedReasonCounts: map[string]int{}, AlreadyRespondedEvidenceCounts: map[string]int{}}
	blockedCandidates := []PilotBlockedCandidate{}
	seenIDs := map[int]struct{}{}
	var manualReview *PilotPreview

	block := func(candidate Vacancy, reason string) {
		stats.BlockedReasonCounts[reason]++
		blockedCandidates = append(blockedCandidates, PilotBlockedCandidate{VacancyID: candidate.ID, Title: firstNonEmpty(candidate.Title, candidate.Name), BlockedReason: reason})
	}
	evaluate := func(candidate Vacancy) (*PilotPreview, bool, error) {
		if stats.Scanned >= maxScan {
			return nil, true, nil
		}
		stats.Scanned++
		if _, ok := knownResponded[candidate.ID]; ok {
			stats.KnownRespondedSkipped++
			block(candidate, "LOCAL_ALREADY_RESPONDED")
			return nil, false, nil
		}
		stats.UnrespondedFound++

		gate, gateErr := r.automaticApplicationGate(ctxOrBackground(r.ctx), candidate.ID)
		if gateErr != nil {
			stats.UnresolvedAttemptSkipped++
			block(candidate, "UNRESOLVED_ATTEMPT")
			return nil, false, nil
		}
		if gate.Classification == attemptpolicy.BlockingConfirmed || gate.Classification == attemptpolicy.BlockingUnresolved {
			stats.UnresolvedAttemptSkipped++
			block(candidate, "UNRESOLVED_ATTEMPT")
			return nil, false, nil
		}
		preflight, preflightErr := r.GetVacancyPreflight(candidate)
		if preflightErr != nil {
			block(candidate, "PREFLIGHT_READ_FAILED")
			return nil, false, nil
		}
		evidence := preflight.alreadyRespondedEvidence()
		stats.AlreadyRespondedEvidenceCounts[string(evidence.EvidenceCode)]++
		stats.PreflightAudits = append(stats.PreflightAudits, PilotPreflightAudit{
			VacancyID: candidate.ID, Title: firstNonEmpty(candidate.Title, candidate.Name),
			AlreadyResponded: string(evidence.Value), AlreadyRespondedEvidenceCode: string(evidence.EvidenceCode),
			CanApply:                     knownBoolPointer(preflight.CanApply, preflight.CanApplyKnown),
			TestRequired:                 knownBoolPointer(preflight.TestPresent, preflight.TestPresentKnown),
			Active:                       knownBoolPointer(!preflight.Archived, preflight.ArchivedKnown),
			ResponseIdentifierPresent:    preflight.ResponseIdentifierPresent || strings.TrimSpace(preflight.ResponseURL) != "",
			NegotiationIdentifierPresent: preflight.NegotiationIdentifierPresent,
		})
		if evidence.Value == AlreadyRespondedYes {
			stats.FreshAlreadyRespondedSkipped++
			block(candidate, "ALREADY_RESPONDED")
			return nil, false, nil
		}
		if evidence.Value == AlreadyRespondedUnknown {
			stats.PreflightUnknown++
		} else if evidence.Value == AlreadyRespondedNo {
			stats.FreshUnresponded++
		}
		if reason := pilotPreflightBlockReason(preflight); reason != "" {
			block(candidate, reason)
			return nil, false, nil
		}
		if stats.UnrespondedEvaluated >= maxCandidates {
			block(candidate, "DEEP_CANDIDATE_LIMIT")
			return nil, true, nil
		}
		stats.UnrespondedEvaluated++

		stats.DetailReads++
		detail, detailErr := r.fetchCareerAgentDetail(ctxOrBackground(r.ctx), candidate)
		if detailErr != nil {
			block(candidate, "VACANCY_DETAIL_UNAVAILABLE")
			return nil, false, nil
		}
		preview, previewErr := r.buildCareerAgentPilotPreviewFromState(detail, preflight)
		if previewErr != nil {
			block(candidate, pilotErrorReason(previewErr))
			return nil, false, nil
		}
		if preview.Artifact.AIScore != nil {
			stats.AIEvaluations++
		}
		if preview.Status == pilotReadyStatus || preview.Status == pilotManualReviewStatus {
			return &preview, false, nil
		}
		block(candidate, pilotPreviewBlockedReason(preview, r.minMatchScore))
		return nil, false, nil
	}

	for _, period := range pilotSearchPeriods(r.searchPeriodDays) {
		if stats.Scanned >= maxScan || stats.UnrespondedEvaluated >= maxCandidates {
			break
		}
		profiles := r.pilotSearchProfiles(period)
		vacancies, err := r.fetchVacanciesFromSearchProfilesWithLimit(&RunSummaryResult{}, profiles, maxScan, seenIDs)
		if err != nil {
			return PilotPreview{}, fmt.Errorf("pilot fresh search failed: %w", err)
		}
		sort.SliceStable(vacancies, func(i, j int) bool {
			leftAttempted, rightAttempted := false, false
			if _, ok := attempted[vacancies[i].ID]; ok {
				leftAttempted = true
			}
			if _, ok := attempted[vacancies[j].ID]; ok {
				rightAttempted = true
			}
			if leftAttempted != rightAttempted {
				return !leftAttempted
			}
			left, right := vacancies[i].PublishedAt, vacancies[j].PublishedAt
			if left.IsZero() || right.IsZero() || left.Equal(right) {
				return false
			}
			return left.After(right)
		})
		for _, candidate := range vacancies {
			preview, stop, err := evaluate(candidate)
			if err != nil {
				return PilotPreview{}, err
			}
			if preview != nil {
				if preview.Status == pilotReadyStatus {
					return r.finishPilotSearchPreview(*preview, stats, blockedCandidates), nil
				}
				if manualReview == nil {
					copy := *preview
					manualReview = &copy
				}
			}
			if stop {
				break
			}
		}
	}
	if manualReview != nil {
		return r.finishPilotSearchPreview(*manualReview, stats, blockedCandidates), nil
	}
	result := PilotPreview{Status: applicationpilot.StatusBlocked, CandidatesChecked: stats.Scanned, BlockedCandidates: blockedCandidates, SearchStats: stats}
	result.Artifact = PilotArtifact{Version: pilotArtifactVersion, Status: applicationpilot.StatusBlocked, FinalDecision: string(applicationprocessing.DecisionReviewRequired), FinalReason: "no fresh pilot-eligible candidate", PreviewFreshAt: time.Now().UTC(), CandidatesChecked: result.CandidatesChecked, BlockedCandidates: append([]PilotBlockedCandidate(nil), blockedCandidates...), SearchStats: stats}
	return result, nil
}

func (r *HHAIResponder) finishPilotSearchPreview(preview PilotPreview, stats PilotSearchStats, blocked []PilotBlockedCandidate) PilotPreview {
	preview.CandidatesChecked = stats.Scanned
	preview.BlockedCandidates = append([]PilotBlockedCandidate(nil), blocked...)
	preview.SearchStats = stats
	preview.Artifact.CandidatesChecked = stats.Scanned
	preview.Artifact.BlockedCandidates = append([]PilotBlockedCandidate(nil), blocked...)
	preview.Artifact.SearchStats = stats
	return preview
}

func pilotSearchPeriods(current int) []int {
	if current <= 0 {
		current = appconfig.DefaultSearchPeriodDays
	}
	recent := current
	if recent > 3 {
		recent = 3
	}
	if recent == current {
		return []int{current}
	}
	return []int{recent, current}
}

func (r *HHAIResponder) pilotSearchProfiles(period int) []vacancySearchProfile {
	profiles := append([]vacancySearchProfile(nil), r.searchProfiles...)
	if len(profiles) == 0 {
		profiles = []vacancySearchProfile{{Name: "Default search", BaseURL: r.baseURL, Params: r.searchParams, URL: searchProfileURL(r.baseURL, r.searchParams)}}
	}
	for index := range profiles {
		profiles[index].Params = cloneValues(profiles[index].Params)
		profiles[index].Params.Set("order_by", "publication_time")
		profiles[index].Params.Set("search_period", fmt.Sprint(period))
		profiles[index].URL = searchProfileURL(profiles[index].BaseURL, profiles[index].Params)
	}
	return profiles
}

func (r *HHAIResponder) pilotKnownRespondedVacancies(ctx context.Context) map[int]struct{} {
	result := map[int]struct{}{}
	r.alreadyRespondedMu.Lock()
	for id := range r.alreadyResponded {
		if id > 0 {
			result[id] = struct{}{}
		}
	}
	r.alreadyRespondedMu.Unlock()

	var applications []JobApplication
	var conversations []EmployerConversation
	if r.careerRepositories.Applications != nil {
		applications, _ = r.careerRepositories.Applications.List(ctx)
		if r.careerRepositories.Conversations != nil {
			conversations, _ = r.careerRepositories.Conversations.List(ctx)
		}
	} else {
		base := filepath.Dir(strings.TrimSpace(r.candidateProfilePath))
		if base == "." && strings.TrimSpace(r.candidateProfilePath) == "" {
			base = "."
		}
		conversationStore := NewConversationStore(filepath.Join(base, EmployerConversationsFilename))
		applicationStore := NewApplicationStoreWithDependencies(filepath.Join(base, JobApplicationsFilename), conversationStore, nil)
		if conversationStore.Load() == nil {
			conversations, _ = conversationStore.ListConversationsForDashboard()
		}
		if applicationStore.Load() == nil {
			applications, _ = applicationStore.ListApplicationsForDashboard()
		}
	}
	for _, application := range applications {
		if pilotApplicationHasResponseEvidence(application) {
			result[application.VacancyID] = struct{}{}
		}
	}
	for _, conversation := range conversations {
		if conversation.VacancyID > 0 && strings.TrimSpace(conversation.HHConversationID) != "" {
			result[conversation.VacancyID] = struct{}{}
		}
	}
	return result
}

func (r *HHAIResponder) pilotAttemptedVacancies(ctx context.Context) map[int]struct{} {
	result := map[int]struct{}{}
	reader, ok := r.applicationAttempts.(attemptport.Reader)
	if !ok || reader == nil {
		return result
	}
	attempts, err := reader.List(ctx, attemptport.ReadQuery{Limit: 100})
	if err != nil {
		return result
	}
	for _, attempt := range attempts {
		if attempt.VacancyID > 0 {
			result[attempt.VacancyID] = struct{}{}
		}
	}
	return result
}

func pilotApplicationHasResponseEvidence(value JobApplication) bool {
	if value.VacancyID <= 0 {
		return false
	}
	if strings.TrimSpace(value.ExternalID) != "" || len(value.ReconciliationEvidence) > 0 {
		return true
	}
	for key, item := range value.HHMetadata {
		if strings.TrimSpace(item) == "" {
			continue
		}
		key = strings.ToLower(key)
		if strings.Contains(key, "provider") || strings.Contains(key, "negotiation") || strings.Contains(key, "response") {
			return true
		}
	}
	switch value.Status {
	case ApplicationApplied, ApplicationEmployerReplied, ApplicationInterview, ApplicationOffer, ApplicationRejected, ApplicationArchived:
		return true
	default:
		return false
	}
}

func pilotOtherBlockedCount(stats PilotSearchStats) int {
	known := map[string]struct{}{
		"LOCAL_ALREADY_RESPONDED": {}, "ALREADY_RESPONDED": {}, "ALREADY_RESPONDED_UNKNOWN": {}, "PREFLIGHT_READ_FAILED": {},
		"CAN_APPLY_FALSE": {}, "ROUTE_AMBIGUOUS": {},
		"HARD_REQUIREMENT_MISSING": {}, "HARD_REQUIREMENT_UNKNOWN": {},
		"SCORE_BELOW_THRESHOLD": {}, "TEST_REQUIRED_UNSUPPORTED": {},
		"VACANCY_INACTIVE": {},
	}
	other := 0
	for reason, count := range stats.BlockedReasonCounts {
		if _, ok := known[reason]; !ok {
			other += count
		}
	}
	return other
}

func formatPilotEvidenceCounts(values map[string]int) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(values[key]))
	}
	return strings.Join(parts, ", ")
}

func (r *HHAIResponder) buildCareerAgentPilotPreviewFromState(value Vacancy, preflight VacancyPreflight) (PilotPreview, error) {
	ctx := ctxOrBackground(r.ctx)
	if value.ID <= 0 {
		return PilotPreview{}, errors.New("pilot vacancy has invalid id")
	}
	if reason := pilotPreflightBlockReason(preflight); reason != "" {
		return PilotPreview{Status: applicationpilot.StatusBlocked, Artifact: PilotArtifact{Version: pilotArtifactVersion, VacancyID: value.ID, Vacancy: value, Preflight: pilotPreflightSnapshot(preflight), PreviewFreshAt: time.Now().UTC()}, Reasons: []string{reason}}, nil
	}

	// The router is rebuilt from the freshly loaded /applicant/my_resumes data;
	// no resume ID is hardcoded from Stage 29.6.
	route := r.routeResumeForVacancy(value)
	artifact := PilotArtifact{Version: pilotArtifactVersion, Status: applicationpilot.StatusBlocked, VacancyID: value.ID, Vacancy: value, SelectedResumeID: route.SelectedResumeID, SelectedResumeTitle: route.SelectedResumeTitle, RouterScore: route.Score, AlternativeScores: append([]careeragent.ResumeScore(nil), route.AlternativeScores...), Preflight: pilotPreflightSnapshot(preflight), PreviewFreshAt: time.Now().UTC()}
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
	artifact.SelectedResumeHHID = selectedResume.Id
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
	respondedEvidence := preflight.alreadyRespondedEvidence()
	app := applicationprocessing.Applicability{Available: preflight.Available, Archived: preflight.Archived, ArchivedKnown: preflight.ArchivedKnown, AlreadyResponded: respondedEvidence.Value == AlreadyRespondedYes, AlreadyRespondedKnown: respondedEvidence.Value != AlreadyRespondedUnknown, AlreadyRespondedValue: string(respondedEvidence.Value), AlreadyRespondedEvidenceCode: string(respondedEvidence.EvidenceCode), TestPresent: preflight.TestPresent, TestPresentKnown: preflight.TestPresentKnown, LetterRequired: preflight.LetterRequired, LetterRequiredKnown: preflight.LetterRequiredKnown, CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown, Area: preflight.Area, AreaKnown: preflight.AreaKnown, WorkSchedule: preflight.WorkSchedule, WorkScheduleKnown: preflight.WorkScheduleKnown, WorkExperience: preflight.WorkExperience, WorkExperienceKnown: preflight.WorkExperienceKnown, ResponseURL: preflight.ResponseURL}
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
	if !pilotAIEligibleForPreview(assessment, decision, r.minMatchScore) {
		artifact.Status = applicationpilot.StatusBlocked
		preview.Status = applicationpilot.StatusBlocked
		return preview, nil
	}

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
	if preflight.alreadyRespondedEvidence().Value == AlreadyRespondedYes {
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

	if pilotReadyForExplicitSend(assessment, decision, r.minMatchScore) && len(artifact.HardMissing) == 0 && len(artifact.HardUnknown) == 0 && preflight.ArchivedKnown && !preflight.Archived && respondedEvidence.Value == AlreadyRespondedNo && preflight.CanApplyKnown && preflight.CanApply && preflight.TestPresentKnown && !preflight.TestPresent && preflight.LetterRequiredKnown && artifact.ContentHash != "" {
		artifact.Nonce, err = generateUUIDv4()
		if err != nil {
			return PilotPreview{}, fmt.Errorf("pilot nonce generation failed: %w", err)
		}
		artifact.Status = pilotReadyStatus
		preview.Status = pilotReadyStatus
	} else if pilotManualReviewEligible(assessment, decision, r.minMatchScore) && artifact.ContentHash != "" {
		artifact.Status = pilotManualReviewStatus
		preview.Status = pilotManualReviewStatus
		preview.Reasons = append(preview.Reasons, "AI advisory recommendation requires manual review before send")
	} else {
		artifact.Status = applicationpilot.StatusBlocked
		preview.Status = applicationpilot.StatusBlocked
	}
	preview.Artifact = artifact
	return preview, nil
}

func pilotAIEligibleForPreview(assessment VacancyEvaluation, decision applicationprocessing.Decision, minScore int) bool {
	if len(hardRequirementsMissing(assessment)) > 0 || len(hardRequirementsUnknown(assessment)) > 0 || assessment.Score < minScore {
		return false
	}
	if decision == applicationprocessing.DecisionMatch {
		return true
	}
	return decision == applicationprocessing.DecisionReviewRequired && assessmentRecommendation(assessment) == vacancyanalysis.RecommendationUncertain
}

func pilotReadyForExplicitSend(assessment VacancyEvaluation, decision applicationprocessing.Decision, minScore int) bool {
	return pilotAIEligibleForPreview(assessment, decision, minScore) && decision == applicationprocessing.DecisionMatch && assessmentRecommendation(assessment) == vacancyanalysis.RecommendationApply
}

func pilotManualReviewEligible(assessment VacancyEvaluation, decision applicationprocessing.Decision, minScore int) bool {
	return pilotAIEligibleForPreview(assessment, decision, minScore) && decision == applicationprocessing.DecisionReviewRequired && assessmentRecommendation(assessment) == vacancyanalysis.RecommendationUncertain
}

func pilotPreflightBlockReason(preflight VacancyPreflight) string {
	if preflight.alreadyRespondedEvidence().Value == AlreadyRespondedYes {
		return "ALREADY_RESPONDED"
	}
	if preflight.alreadyRespondedEvidence().Value == AlreadyRespondedUnknown {
		return "ALREADY_RESPONDED_UNKNOWN"
	}
	if preflight.CanApplyKnown && !preflight.CanApply {
		return "CAN_APPLY_FALSE"
	}
	if !preflight.CanApplyKnown {
		return "CAN_APPLY_UNKNOWN"
	}
	if !preflight.ArchivedKnown {
		return "VACANCY_ACTIVE_UNKNOWN"
	}
	if preflight.Archived {
		return "VACANCY_INACTIVE"
	}
	if preflight.TestPresentKnown && preflight.TestPresent {
		return "TEST_REQUIRED_UNSUPPORTED"
	}
	if !preflight.TestPresentKnown {
		return "TEST_REQUIRED_UNKNOWN"
	}
	if !preflight.LetterRequiredKnown {
		return "COVER_LETTER_REQUIREMENT_UNKNOWN"
	}
	if preflight.LetterRequired && !preflight.LetterAllowedKnown {
		return "COVER_LETTER_ALLOWED_UNKNOWN"
	}
	if preflight.LetterRequired && !preflight.LetterAllowed {
		return "COVER_LETTER_NOT_ALLOWED"
	}
	return ""
}

func pilotErrorReason(err error) string {
	if err == nil {
		return "PILOT_PREVIEW_FAILED"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "resume router"):
		return "ROUTE_AMBIGUOUS"
	case strings.Contains(text, "selected resume"):
		return "RESUME_UNAVAILABLE"
	case strings.Contains(text, "ai"):
		return "AI_EVALUATION_FAILED"
	case strings.Contains(text, "cover-letter"):
		return "COVER_LETTER_PREVIEW_FAILED"
	default:
		return "PILOT_PREVIEW_FAILED"
	}
}

func pilotPreviewBlockedReason(preview PilotPreview, minScore int) string {
	if len(preview.Artifact.HardMissing) > 0 {
		return "HARD_REQUIREMENT_MISSING"
	}
	if len(preview.Artifact.HardUnknown) > 0 {
		return "HARD_REQUIREMENT_UNKNOWN"
	}
	if preview.Artifact.AIScore != nil && *preview.Artifact.AIScore < minScore {
		return "SCORE_BELOW_THRESHOLD"
	}
	for _, reason := range preview.Reasons {
		lower := strings.ToLower(reason)
		switch {
		case strings.Contains(lower, "resume router") || strings.Contains(lower, "scores are too close"):
			return "ROUTE_AMBIGUOUS"
		case strings.Contains(lower, "cover-letter preview failed"):
			return "COVER_LETTER_PREVIEW_FAILED"
		case strings.Contains(lower, "advisory"):
			return "AI_ADVISORY_CONCERN"
		}
	}
	if preview.Artifact.FinalDecision == string(applicationprocessing.DecisionReviewRequired) {
		return "AI_ADVISORY_CONCERN"
	}
	return "PILOT_PREVIEW_BLOCKED"
}

func pilotPreflightSnapshot(value VacancyPreflight) PilotPreflightSnapshot {
	evidence := value.alreadyRespondedEvidence()
	return PilotPreflightSnapshot{ObservedAt: time.Now().UTC(), Active: knownBoolPointer(!value.Archived, value.ArchivedKnown), AlreadyResponded: knownBoolPointer(evidence.Value == AlreadyRespondedYes, evidence.Value != AlreadyRespondedUnknown), AlreadyRespondedValue: string(evidence.Value), AlreadyRespondedEvidenceCode: string(evidence.EvidenceCode), CanApply: knownBoolPointer(value.CanApply, value.CanApplyKnown), TestRequired: knownBoolPointer(value.TestPresent, value.TestPresentKnown), CoverLetterRequired: knownBoolPointer(value.LetterRequired, value.LetterRequiredKnown), CoverLetterAllowed: knownBoolPointer(value.LetterAllowed, value.LetterAllowedKnown), ResponseURL: value.ResponseURL, Area: value.Area, WorkSchedule: value.WorkSchedule, WorkExperience: value.WorkExperience}
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
	if preview.Status != pilotReadyStatus && a.VacancyID == 0 {
		stats := preview.SearchStats
		lines := []string{"PILOT: NO_ELIGIBLE_CANDIDATE", fmt.Sprintf("Scanned: %d", stats.Scanned), fmt.Sprintf("Known responded: %d", stats.KnownRespondedSkipped), fmt.Sprintf("Fresh AlreadyResponded: %d", stats.FreshAlreadyRespondedSkipped), fmt.Sprintf("Preflight unknown: %d", stats.PreflightUnknown), fmt.Sprintf("Fresh unresponded: %d", stats.FreshUnresponded), fmt.Sprintf("Evidence distribution: %s", formatPilotEvidenceCounts(stats.AlreadyRespondedEvidenceCounts)), fmt.Sprintf("CanApply false: %d", stats.BlockedReasonCounts["CAN_APPLY_FALSE"]), fmt.Sprintf("Route ambiguous: %d", stats.BlockedReasonCounts["ROUTE_AMBIGUOUS"]), fmt.Sprintf("Hard missing: %d", stats.BlockedReasonCounts["HARD_REQUIREMENT_MISSING"]), fmt.Sprintf("Hard unknown: %d", stats.BlockedReasonCounts["HARD_REQUIREMENT_UNKNOWN"]), fmt.Sprintf("Score below threshold: %d", stats.BlockedReasonCounts["SCORE_BELOW_THRESHOLD"]), fmt.Sprintf("Unsupported test: %d", stats.BlockedReasonCounts["TEST_REQUIRED_UNSUPPORTED"]), fmt.Sprintf("Inactive: %d", stats.BlockedReasonCounts["VACANCY_INACTIVE"]), fmt.Sprintf("Other: %d", pilotOtherBlockedCount(stats)), fmt.Sprintf("Unresponded candidates actually evaluated: %d", stats.UnrespondedEvaluated), fmt.Sprintf("Detail reads: %d", stats.DetailReads), fmt.Sprintf("AI evaluations: %d", stats.AIEvaluations)}
		if len(preview.BlockedCandidates) > 0 {
			lines = append(lines, "Blocked candidates:")
			for _, candidate := range preview.BlockedCandidates {
				lines = append(lines, fmt.Sprintf("  %d %s — %s", candidate.VacancyID, candidate.Title, candidate.BlockedReason))
			}
		}
		return strings.Join(append(lines, "Real HH reads: YES", "Real HH writes: 0", "Application POST: 0", "Shadow writes: 0"), "\n") + "\n"
	}
	aiScore := "unknown"
	if a.AIScore != nil {
		aiScore = fmt.Sprint(*a.AIScore)
	}
	company := firstNonEmpty(a.Vacancy.Company.Name, "unknown")
	url := firstNonEmpty(a.Vacancy.Links["desktop"], "unknown")
	resumeID := firstNonEmpty(fmt.Sprint(a.SelectedResumeHHID), a.SelectedResumeHash, a.SelectedResumeID, "unknown")
	if a.SelectedResumeHHID <= 0 {
		resumeID = firstNonEmpty(a.SelectedResumeHash, a.SelectedResumeID, "unknown")
	}
	localDecision := firstNonEmpty(a.FinalDecision, "unknown")
	if a.FinalReason != "" {
		localDecision += " (" + a.FinalReason + ")"
	}
	nonceStatus := "not issued"
	if a.Nonce != "" {
		nonceStatus = "issued; unused"
		if a.NonceUsedAt != nil {
			nonceStatus = "used"
		}
	}
	freshness := a.PreviewFreshAt.UTC().Format(time.RFC3339)
	if !a.Preflight.ObservedAt.IsZero() {
		freshness = "preflight=" + a.Preflight.ObservedAt.UTC().Format(time.RFC3339) + "; preview=" + freshness
	}
	status := "BLOCKED"
	if preview.Status == pilotReadyStatus {
		status = "READY_FOR_EXPLICIT_SEND"
	} else if preview.Status == pilotManualReviewStatus {
		status = pilotManualReviewStatus
	}
	published := "unknown"
	if !a.Vacancy.PublishedAt.IsZero() {
		published = a.Vacancy.PublishedAt.UTC().Format(time.RFC3339)
	}
	stats := preview.SearchStats
	lines := []string{"PILOT: " + status, "Vacancy ID: " + fmt.Sprint(a.VacancyID), "Title: " + firstNonEmpty(a.Vacancy.Title, a.Vacancy.Name, "unknown"), "Company: " + company, "URL: " + url, "Published: " + published, "Selected resume: " + firstNonEmpty(a.SelectedResumeTitle, "unknown"), "Resume ID: " + resumeID, "Router score: " + firstNonEmpty(fmt.Sprint(a.RouterScore), "unknown"), "AI score: " + aiScore, "AI recommendation: " + firstNonEmpty(a.AIRecommendation, "unknown"), "Local decision: " + localDecision, "Hard missing: " + joinOrUnknown(a.HardMissing), "Hard unknown: " + joinOrUnknown(a.HardUnknown), "Active: " + pointerWord(a.Preflight.Active), "Already responded: " + pointerWord(a.Preflight.AlreadyResponded), "Can apply: " + pointerWord(a.Preflight.CanApply), "Test required: " + pointerWord(a.Preflight.TestRequired), "Cover letter required: " + pointerWord(a.Preflight.CoverLetterRequired), "Cover letter allowed: " + pointerWord(a.Preflight.CoverLetterAllowed), "Content hash: " + firstNonEmpty(a.ContentHash, "none"), "Nonce status: " + nonceStatus, "Freshness: " + freshness, fmt.Sprintf("Scanned: %d", stats.Scanned), fmt.Sprintf("Known responded skipped: %d", stats.KnownRespondedSkipped), fmt.Sprintf("Fresh preflight responded skipped: %d", stats.FreshAlreadyRespondedSkipped), fmt.Sprintf("Unresponded evaluated: %d", stats.UnrespondedEvaluated), fmt.Sprintf("Detail reads: %d", stats.DetailReads), fmt.Sprintf("AI evaluations: %d", stats.AIEvaluations), "Real HH reads: YES", "Real HH writes: 0", "HH writes = 0", "Application POST: 0", "Shadow writes: 0"}
	if a.CoverLetter != "" {
		lines = append(lines, "Cover letter:", a.CoverLetter)
	}
	if len(preview.BlockedCandidates) > 0 {
		lines = append(lines, "Blocked candidates:")
		for _, candidate := range preview.BlockedCandidates {
			lines = append(lines, fmt.Sprintf("  %d %s — %s", candidate.VacancyID, candidate.Title, candidate.BlockedReason))
		}
	}
	if len(preview.Reasons) > 0 {
		lines = append(lines, "Reason: "+strings.Join(uniqueStrings(preview.Reasons), "; "))
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
	if preflight.ArchivedKnown && preflight.Archived || preflight.alreadyRespondedEvidence().Value != AlreadyRespondedNo || preflight.CanApplyKnown && !preflight.CanApply || preflight.TestPresentKnown && preflight.TestPresent {
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
