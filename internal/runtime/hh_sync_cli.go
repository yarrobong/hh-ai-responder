package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	"hh-ai-responder/internal/platform"
)

func runHHCommand(args []string, cfg Config, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: hh sync [vacancies|applications|conversations] | hh sync conversation <conversation-id-or-chat-id> | hh inbox | hh workflow | hh draft <conversation-id> | hh pilot-candidates | hh pilot-shortlist | hh pilot-show <conversation-id> | hh write-status | hh eligible | hh eligibility-report | hh eligibility-summary | hh quality-report | hh action preflight|request-preview <action-id> | hh reliability applications|autochat [--limit N] [--state STATE] [--all] [--json] | hh reliability applications|autochat reconcile <attempt-id> [--json] | hh reliability applications manual-confirm <attempt-id> --negotiation-id ID --conversation-id ID")
	}
	if args[0] == "reliability" {
		return runReliabilityCommand(args[1:], cfg, stdout)
	}
	if logger == nil {
		logger = NewLogger(stderr, parseLogLevel(cfg.LogLevel))
	}
	wd, err := osGetwd()
	if err != nil {
		return err
	}
	profilePath := cfg.CandidateProfilePath
	if profilePath == "" {
		profilePath = filepath.Join(wd, "candidate_profile.json")
	}
	conversations, applications, vacancies, clarifications, drafts, closeCareer, err := loadHHLocalStoresForBackend(context.Background(), cfg, wd, profilePath)
	if err != nil {
		return err
	}
	defer closeCareer()
	var canonicalKB *CandidateKnowledgeBase
	var canonicalResolver *CandidateContextResolver
	var semanticRetriever CandidateSemanticRetriever
	if backend, backendErr := normalizeStorageBackend(cfg.StorageBackend); backendErr != nil {
		return backendErr
	} else if backend == storageBackendPostgres {
		persistence, closePersistence, persistenceErr := BuildCandidatePersistence(context.Background(), cfg)
		if persistenceErr != nil {
			return persistenceErr
		}
		defer closePersistence()
		candidate, candidateErr := persistence.Repository.CurrentCandidate(context.Background())
		if candidateErr != nil {
			return candidateErr
		}
		canonicalKB = CandidateKnowledgeSnapshot(candidate, profilePath)
		canonicalResolver = NewCandidateContextResolverFromCandidate(candidate)
		semanticRetriever = persistence.SemanticRetriever
	}

	switch args[0] {
	case "sync":
		if len(args) == 3 && args[1] == "conversation" && strings.TrimSpace(args[2]) != "" {
			ctx := context.Background()
			responder, responderErr := NewHHAIResponder(ctx, dashboardReadConfig(cfg))
			if responderErr != nil {
				return responderErr
			}
			kb := canonicalKB
			if kb == nil {
				kb = NewCandidateKnowledgeBase(profilePath)
				if loadErr := kb.Load(); loadErr != nil {
					return loadErr
				}
			}
			service, closeService, serviceErr := buildHHReadSyncService(context.Background(), cfg, NewHHAIResponderReadClient(responder), vacancies, applications, conversations, NewVacancyAnalyzer(), kb, clarifications, drafts, cfg.HHSyncStatePath)
			if serviceErr != nil {
				return serviceErr
			}
			defer closeService()
			if loadErr := service.LoadState(); loadErr != nil && !errors.Is(loadErr, errPrivateFileNotFound) {
				return loadErr
			}
			externalID := strings.TrimSpace(args[2])
			if local, localErr := conversations.GetConversation(externalID); localErr == nil {
				externalID = local.HHConversationID
			}
			result, syncErr := service.SyncConversation(externalID, ctx)
			if syncErr == nil && len(result.Errors) == 0 {
				// A fresh read can make an older clarification obsolete (for
				// example, after the candidate confirms a profile fact). Close
				// that local workflow record without deleting its history; this
				// keeps the subsequent dry-run preflight aligned with the fresh
				// conversation context.
				resolver := canonicalResolver
				if resolver == nil {
					resolver = NewCandidateContextResolver(kb)
				}
				if changed, reconcileErr := ReconcileCandidateClarifications(clarifications, conversations, resolver); reconcileErr != nil {
					result.Errors = append(result.Errors, safeSyncError(reconcileErr))
				} else if changed > 0 {
					if saveErr := clarifications.Save(); saveErr != nil {
						result.Errors = append(result.Errors, safeSyncError(saveErr))
					}
				}
			}
			if writeErr := writeJSON(stdout, result); writeErr != nil {
				return writeErr
			}
			if syncErr != nil {
				return syncErr
			}
			if len(result.Errors) > 0 {
				return errors.New("HH read-only conversation sync incomplete; see JSON report")
			}
			return nil
		}
		target, targetErr := hhSyncTarget(args)
		if targetErr != nil {
			return targetErr
		}
		ctx := context.Background()
		responder, responderErr := NewHHAIResponder(ctx, dashboardReadConfig(cfg))
		if responderErr != nil {
			return responderErr
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if loadErr := kb.Load(); loadErr != nil {
				return loadErr
			}
		}
		service, closeService, serviceErr := buildHHReadSyncService(context.Background(), cfg, NewHHAIResponderReadClient(responder), vacancies, applications, conversations, NewVacancyAnalyzer(), kb, clarifications, drafts, cfg.HHSyncStatePath)
		if serviceErr != nil {
			return serviceErr
		}
		defer closeService()
		if loadErr := service.LoadState(); loadErr != nil && !errors.Is(loadErr, errPrivateFileNotFound) {
			return loadErr
		}
		return writeHHSyncReport(service, target, stdout)

	case "inbox":
		service := NewHHReadSyncServiceWithOptions(nil, HHReadSyncServiceOptions{
			Conversations: conversations, Clarifications: clarifications, Drafts: drafts,
		})
		inbox, inboxErr := service.GetCandidateInbox()
		if inboxErr != nil {
			return inboxErr
		}
		return writeJSON(stdout, inbox)
	case "workflow":
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if loadErr := kb.Load(); loadErr != nil {
				return loadErr
			}
		}
		applicationValues, _ := applications.ListApplications()
		eventValues, _ := applications.ListEvents()
		conversationValues, _ := conversations.ListConversations()
		clarificationValues, _ := clarifications.List()
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		report := BuildDailyWorkflowReport(CareerSnapshot{Applications: applicationValues, Events: eventValues, Conversations: conversationValues, Clarifications: clarificationValues}, resolver, cfg.FollowUpPolicy, time.Now().UTC())
		return WriteDailyWorkflowReport(stdout, report)
	case "draft":
		if len(args) != 2 || args[1] == "" {
			return errors.New("usage: hh draft <conversation-id>")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if loadErr := kb.Load(); loadErr != nil {
				return loadErr
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		ai := NewAIClient(context.Background(), cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, cfg.AITimeout, cfg.AIConnectTimeout, cfg.AIAttempts)
		orchestrator := NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{
			ConversationStore: conversations, Resolver: resolver, Drafts: drafts,
			Clarifications: clarifications, SemanticRetriever: semanticRetriever,
		})
		decision, decisionErr := orchestrator.PrepareEmployerReply(args[1])
		if decisionErr != nil {
			return decisionErr
		}
		if saveErr := drafts.Save(); saveErr != nil {
			return saveErr
		}
		if saveErr := clarifications.Save(); saveErr != nil {
			return saveErr
		}
		return writeJSON(stdout, decision)
	case "write-status":
		if len(args) != 1 {
			return errors.New("usage: hh write-status")
		}
		actions := NewApprovedHHActionStore(filepath.Join(wd, HHWriteActionsFilename))
		if err := actions.Load(); err != nil {
			return err
		}
		audit := NewHHWriteAuditStore(filepath.Join(wd, HHWriteEventsFilename))
		if err := audit.Load(); err != nil {
			return err
		}
		_, err := io.WriteString(stdout, BuildHHWriteStatusReportWithConversations(cfg, actions, audit, conversations).text()+"\n")
		return err
	case "eligible":
		if len(args) != 1 {
			return errors.New("usage: hh eligible")
		}
		profilePath := cfg.CandidateProfilePath
		if profilePath == "" {
			profilePath = filepath.Join(wd, "candidate_profile.json")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if err := kb.Load(); err != nil {
				return err
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		items, err := BuildHHEligibleConversations(conversations, applications, clarifications, resolver)
		if err != nil {
			return err
		}
		return writeJSON(stdout, items)
	case "eligibility-report":
		return runHHEligibilityReport(args[1:], cfg, conversations, applications, clarifications, profilePath, stdout)
	case "eligibility-summary":
		if len(args) != 1 {
			return errors.New("usage: hh eligibility-summary")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if err := kb.Load(); err != nil {
				return err
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		reports, err := BuildConversationEligibilityReports(conversations, applications, clarifications, resolver)
		if err != nil {
			return err
		}
		return writeJSON(stdout, SummarizeEligibility(reports))
	case "quality-report":
		if len(args) != 1 {
			return errors.New("usage: hh quality-report")
		}
		quality := NewQualityLogStore(filepath.Join(wd, QualityLogFilename))
		if err := quality.Load(); err != nil {
			return err
		}
		return WriteQualityReport(stdout, BuildQualityReport(quality.List()))
	case "pilot-candidates":
		if len(args) != 1 {
			return errors.New("usage: hh pilot-candidates")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if err := kb.Load(); err != nil {
				return err
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		set, err := BuildPilotCandidateReports(conversations, applications, clarifications, resolver, drafts, time.Now().UTC())
		if err != nil {
			return err
		}
		return WritePilotCandidateText(stdout, set)
	case "pilot-shortlist":
		if len(args) != 1 {
			return errors.New("usage: hh pilot-shortlist")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if err := kb.Load(); err != nil {
				return err
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		shortlist, err := BuildPilotShortlistPreviews(conversations, applications, vacancies, clarifications, resolver, time.Now().UTC(), 5)
		if err != nil {
			return err
		}
		return WritePilotShortlistText(stdout, shortlist)
	case "pilot-show":
		if len(args) != 2 || args[1] == "" {
			return errors.New("usage: hh pilot-show <conversation-id>")
		}
		kb := canonicalKB
		if kb == nil {
			kb = NewCandidateKnowledgeBase(profilePath)
			if err := kb.Load(); err != nil {
				return err
			}
		}
		resolver := canonicalResolver
		if resolver == nil {
			resolver = NewCandidateContextResolver(kb)
		}
		set, err := BuildPilotCandidateReports(conversations, applications, clarifications, resolver, drafts, time.Now().UTC())
		if err != nil {
			return err
		}
		var pilot *PilotCandidateReport
		for _, item := range append(append(append([]PilotCandidateReport{}, set.Recommended...), set.Possible...), set.NotRecommended...) {
			if item.ConversationID == args[1] {
				copy := item
				pilot = &copy
				break
			}
		}
		if pilot == nil {
			return errors.New("conversation is not SAFE_FOR_MANUAL_REPLY or was not found")
		}
		conversation, err := conversations.GetConversation(args[1])
		if err != nil {
			return err
		}
		conversation.Messages, err = conversations.GetConversationTimeline(args[1])
		if err != nil {
			return err
		}
		ctx, err := NewConversationContextBuilder(conversations, resolver).BuildForReply(args[1])
		if err != nil {
			return err
		}
		latestDraft, draftErr := latestEmployerReplyDraft(drafts, args[1])
		ai := map[string]any{"latest_draft": nil, "draft_quality": nil}
		if draftErr == nil {
			decision := AIResponseDecision{Action: AIActionDraftReply, Draft: latestDraft.Text, UsedFacts: latestDraft.UsedFacts, Reason: latestDraft.DecisionReason, ForbiddenClaimsChecked: true}
			ai = map[string]any{"latest_draft": latestDraft, "draft_quality": pilotDraftQuality(decision, &latestDraft, ctx)}
		}
		payload := PilotShowPayload(conversation, *pilot, ctx, ai)
		return writeJSON(stdout, payload)
	case "action":
		if len(args) != 3 || (args[1] != "preflight" && args[1] != "request-preview") || args[2] == "" {
			return errors.New("usage: hh action preflight|request-preview <action-id>")
		}
		if args[1] == "request-preview" {
			return runHHActionRequestPreview(cfg, wd, args[2], stdout)
		}
		return runHHActionPreflight(cfg, wd, args[2], stdout)
	default:
		return fmt.Errorf("unknown hh command %q", args[0])
	}
}

func runHHEligibilityReport(args []string, cfg Config, conversations *ConversationStore, applications *ApplicationStore, clarifications *CandidateClarificationStore, profilePath string, out io.Writer) error {
	fs := flag.NewFlagSet("eligibility-report", flag.ContinueOnError)
	fs.SetOutput(out)
	class, reason, limit := "", "", 0
	fs.StringVar(&class, "class", "", "Filter: blocked, manual-review or safe-for-manual-reply")
	fs.StringVar(&reason, "reason", "", "Filter by blocker code")
	fs.IntVar(&limit, "limit", 0, "Maximum number of reports")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || limit < 0 {
		return errors.New("usage: hh eligibility-report [--class blocked|manual-review|safe-for-manual-reply] [--reason CODE] [--limit N]")
	}
	class = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(class), "-", "_"))
	reason = strings.ToUpper(strings.TrimSpace(reason))
	if class != "" && class != string(EligibilityBlocked) && class != string(EligibilityManualReview) && class != string(EligibilitySafe) {
		return errors.New("invalid --class")
	}
	var resolver *CandidateContextResolver
	if backend, err := normalizeStorageBackend(cfg.StorageBackend); err != nil {
		return err
	} else if backend == storageBackendPostgres {
		persistence, closePersistence, err := BuildCandidatePersistence(context.Background(), cfg)
		if err != nil {
			return err
		}
		defer closePersistence()
		candidate, err := persistence.Repository.CurrentCandidate(context.Background())
		if err != nil {
			return err
		}
		resolver = NewCandidateContextResolverFromCandidate(candidate)
	} else {
		kb := NewCandidateKnowledgeBase(profilePath)
		if err := kb.Load(); err != nil {
			return err
		}
		resolver = NewCandidateContextResolver(kb)
	}
	reports, err := BuildConversationEligibilityReports(conversations, applications, clarifications, resolver)
	if err != nil {
		return err
	}
	filtered := make([]ConversationEligibilityReport, 0, len(reports))
	for _, report := range reports {
		if class != "" && string(report.Classification) != class {
			continue
		}
		if reason != "" && !eligibilityReportHasCode(report, reason) {
			continue
		}
		filtered = append(filtered, report)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return writeJSON(out, filtered)
}

func eligibilityReportHasCode(report ConversationEligibilityReport, code string) bool {
	for _, findings := range [][]EligibilityFinding{report.Blockers, report.Warnings, report.Informational} {
		for _, finding := range findings {
			if finding.Code == code {
				return true
			}
		}
	}
	return false
}

func runHHActionPreflight(cfg Config, wd, actionID string, out io.Writer) error {
	actions := NewApprovedHHActionStore(filepath.Join(wd, HHWriteActionsFilename))
	if err := actions.Load(); err != nil {
		return err
	}
	audit := NewHHWriteAuditStore(filepath.Join(wd, HHWriteEventsFilename))
	if err := audit.Load(); err != nil {
		return err
	}
	profilePath := cfg.CandidateProfilePath
	if profilePath == "" {
		profilePath = filepath.Join(wd, "candidate_profile.json")
	}
	conversations, applications, _, clarifications, drafts, closeCareer, err := loadHHLocalStoresForBackend(context.Background(), cfg, wd, profilePath)
	if err != nil {
		return err
	}
	defer closeCareer()
	var resolver *CandidateContextResolver
	if backend, err := normalizeStorageBackend(cfg.StorageBackend); err != nil {
		return err
	} else if backend == storageBackendPostgres {
		persistence, closePersistence, err := BuildCandidatePersistence(context.Background(), cfg)
		if err != nil {
			return err
		}
		defer closePersistence()
		candidate, err := persistence.Repository.CurrentCandidate(context.Background())
		if err != nil {
			return err
		}
		resolver = NewCandidateContextResolverFromCandidate(candidate)
	} else {
		kb := NewCandidateKnowledgeBase(profilePath)
		if err := kb.Load(); err != nil {
			return err
		}
		resolver = NewCandidateContextResolver(kb)
	}
	applications.SetCandidateContextResolver(resolver)
	previewClient := &hhPreviewWriteClient{}
	reader := &dashboardHHReader{cfg: cfg, ctx: context.Background()}
	gateway := NewHHWriteGatewayWithOptions(HHWriteGatewayOptions{Enabled: cfg.HHWriteEnabled, DryRun: cfg.DryRun, RequireFreshRead: true, MaxWritesPerRun: cfg.HHMaxWritesPerRun, MaxWritesPerDay: cfg.HHMaxWritesPerDay, Client: previewClient, ReadClient: reader, Conversations: conversations, Applications: applications, Drafts: drafts, Clarifications: clarifications, Resolver: resolver, FollowUpPolicy: cfg.FollowUpPolicy, ChatURL: cfg.HHChatURL, Actions: actions, Audit: audit})
	preflight := gateway.PreflightAction(context.Background(), actionID)
	return writeJSON(out, map[string]any{"status": preflight.Status, "action_id": actionID, "allowed": preflight.Allowed, "reason_code": preflight.ReasonCode, "reason": preflight.Reason, "reasons": preflight.Reasons, "send_request_performed": preflight.SendRequestPerformed})
}

func runHHActionRequestPreview(cfg Config, wd, actionID string, out io.Writer) error {
	actions := NewApprovedHHActionStore(filepath.Join(wd, HHWriteActionsFilename))
	if err := actions.Load(); err != nil {
		return err
	}
	audit := NewHHWriteAuditStore(filepath.Join(wd, HHWriteEventsFilename))
	if err := audit.Load(); err != nil {
		return err
	}
	profilePath := cfg.CandidateProfilePath
	if profilePath == "" {
		profilePath = filepath.Join(wd, "candidate_profile.json")
	}
	conversations, applications, _, clarifications, drafts, closeCareer, err := loadHHLocalStoresForBackend(context.Background(), cfg, wd, profilePath)
	if err != nil {
		return err
	}
	defer closeCareer()
	var resolver *CandidateContextResolver
	if backend, err := normalizeStorageBackend(cfg.StorageBackend); err != nil {
		return err
	} else if backend == storageBackendPostgres {
		persistence, closePersistence, err := BuildCandidatePersistence(context.Background(), cfg)
		if err != nil {
			return err
		}
		defer closePersistence()
		candidate, err := persistence.Repository.CurrentCandidate(context.Background())
		if err != nil {
			return err
		}
		resolver = NewCandidateContextResolverFromCandidate(candidate)
	} else {
		kb := NewCandidateKnowledgeBase(profilePath)
		if err := kb.Load(); err != nil {
			return err
		}
		resolver = NewCandidateContextResolver(kb)
	}
	applications.SetCandidateContextResolver(resolver)
	previewClient := &hhPreviewWriteClient{}
	gateway := NewHHWriteGatewayWithOptions(HHWriteGatewayOptions{Enabled: cfg.HHWriteEnabled, DryRun: cfg.DryRun, RequireFreshRead: true, MaxWritesPerRun: cfg.HHMaxWritesPerRun, MaxWritesPerDay: cfg.HHMaxWritesPerDay, Client: previewClient, Conversations: conversations, Applications: applications, Drafts: drafts, Clarifications: clarifications, Resolver: resolver, FollowUpPolicy: cfg.FollowUpPolicy, ChatURL: cfg.HHChatURL, Actions: actions, Audit: audit})
	preview, previewErr := gateway.BuildRequestPreview(actionID)
	validation := "VALID"
	validationError := ""
	if previewErr != nil {
		validation = "INVALID"
		validationError = safeWriteError(previewErr)
	} else if err := ValidateHHWriteRequest(preview); err != nil {
		validation = "INVALID"
		validationError = safeWriteError(err)
	}
	capability := "DISABLED"
	if cfg.DryRun {
		capability = "BLOCKED_BY_DRY_RUN"
	} else if !cfg.HHWriteEnabled {
		capability = "BLOCKED_BY_WRITE_DISABLED"
	} else {
		capability = "AVAILABLE_AFTER_APPROVAL_AND_PREFLIGHT"
	}
	result := map[string]any{
		"action_id":                actionID,
		"request_preview":          sanitizeHHWriteRequestPreview(preview),
		"request_validation":       validation,
		"request_validation_error": validationError,
		"write_capability":         capability,
		"send_request_performed":   false,
	}
	return writeJSON(out, result)
}

func runReconcileCommand(cfg Config, dryRun bool, out io.Writer) error {
	wd, err := osGetwd()
	if err != nil {
		return err
	}
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	cfg.StorageBackend = backend
	var conversations *ConversationStore
	var applications *ApplicationStore
	var vacancies *VacancyStore
	if backend == storageBackendJSON {
		conversations = NewConversationStore(filepath.Join(wd, EmployerConversationsFilename))
		applications = NewApplicationStoreWithDependencies(filepath.Join(wd, JobApplicationsFilename), conversations, nil)
		vacancies = NewVacancyStore(filepath.Join(wd, VacanciesFilename))
		for _, load := range []func() error{conversations.Load, applications.Load, vacancies.Load} {
			if err := load(); err != nil {
				return err
			}
		}
	}
	careerRepositories, closeCareerRepositories, err := BuildCareerRepositories(context.Background(), cfg, vacancies, applications, conversations)
	if err != nil {
		return err
	}
	defer closeCareerRepositories()
	profile := cfg.CandidateProfilePath
	if profile == "" {
		profile = filepath.Join(wd, "candidate_profile.json")
	}
	var candidate any
	var candidateClose func()
	if backend == storageBackendPostgres {
		persistence, closePersistence, persistenceErr := BuildCandidatePersistence(context.Background(), cfg)
		if persistenceErr != nil {
			return persistenceErr
		}
		candidateClose = closePersistence
		current, currentErr := persistence.Repository.CurrentCandidate(context.Background())
		if currentErr != nil {
			candidateClose()
			return currentErr
		}
		candidate = current
	} else {
		kb := NewCandidateKnowledgeBase(profile)
		if err := kb.Load(); err == nil {
			candidate = kb
		}
	}
	if candidateClose != nil {
		defer candidateClose()
	}
	result, err := NewCareerDataReconcilerWithRepositories(careerRepositories.Vacancies, careerRepositories.Applications, careerRepositories.Conversations, NewVacancyAnalyzer(), candidate).Reconcile(dryRun)
	if err != nil {
		return err
	}
	return writeJSON(out, result)
}

func runMonitorCommand(cfg Config, out io.Writer) error {
	wd, err := osGetwd()
	if err != nil {
		return err
	}
	lock, err := platform.AcquireProcessLock(filepath.Join(wd, ".career-monitor"), 2*time.Hour)
	if err != nil {
		return err
	}
	defer lock.Release()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = lock.Touch()
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	profilePath := cfg.CandidateProfilePath
	if profilePath == "" {
		profilePath = filepath.Join(wd, "candidate_profile.json")
	}
	conversations, applications, vacancies, clarifications, drafts, closeCareer, err := loadHHLocalStoresForBackend(ctx, cfg, wd, profilePath)
	if err != nil {
		return err
	}
	defer closeCareer()
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	var candidateClose func()
	defer func() {
		if candidateClose != nil {
			candidateClose()
		}
	}()
	var kb *CandidateKnowledgeBase
	var candidate any
	if backend == storageBackendPostgres {
		persistence, closePersistence, persistenceErr := BuildCandidatePersistence(ctx, cfg)
		if persistenceErr != nil {
			return persistenceErr
		}
		candidateClose = closePersistence
		current, currentErr := persistence.Repository.CurrentCandidate(ctx)
		if currentErr != nil {
			return currentErr
		}
		kb = CandidateKnowledgeSnapshot(current, profilePath)
		candidate = current
	} else {
		kb = NewCandidateKnowledgeBase(profilePath)
		if err := kb.Load(); err != nil {
			return err
		}
		candidate = kb
	}
	readerCfg := dashboardReadConfig(cfg)
	responder, err := NewHHAIResponder(ctx, readerCfg)
	if err != nil {
		return err
	}
	syncOptions := HHReadSyncServiceOptions{
		Vacancies: vacancies, Applications: applications, Conversations: conversations,
		Analyzer: NewVacancyAnalyzer(), Candidate: candidate, Clarifications: clarifications,
		Drafts: drafts, StatePath: cfg.HHSyncStatePath,
	}
	var syncService *HHReadSyncService
	if career, ok := careerRepositoriesFromStores(vacancies, applications, conversations); ok {
		syncService = NewHHReadSyncServiceWithRepositories(NewHHAIResponderReadClient(responder), career, syncOptions)
	} else {
		syncService = NewHHReadSyncServiceWithOptions(NewHHAIResponderReadClient(responder), syncOptions)
	}
	if err := syncService.LoadState(); err != nil && !errors.Is(err, errPrivateFileNotFound) {
		return err
	}
	notifications := NewNotificationStore(filepath.Join(wd, NotificationEventsFilename))
	if err := notifications.Load(); err != nil {
		return err
	}
	engine := NewCandidateNotificationEngine(notifications, applications, conversations, clarifications, cfg.FollowUpPolicy, cfg.NotificationCooldown)
	career, ok := careerRepositoriesFromStores(vacancies, applications, conversations)
	if !ok {
		career = CareerRepositories{Vacancies: NewJSONVacancyRepository(vacancies), Applications: NewJSONApplicationRepository(applications), Conversations: NewJSONConversationRepository(conversations)}
	}
	monitor := NewCareerMonitor(syncService, NewCareerDataReconcilerWithRepositories(
		career.Vacancies, career.Applications, career.Conversations, NewVacancyAnalyzer(), candidate,
	), engine, applications, conversations, monitorStatePath(wd), cfg.MonitorInterval)
	monitor.QuietHours = cfg.MonitorQuietHours
	if err := monitor.LoadState(); err != nil {
		return err
	}
	if cfg.RunOnce {
		result, err := monitor.RunOnceBounded(ctx, time.Now().UTC(), cfg.MaxConversationsPerRun)
		if err != nil {
			return err
		}
		conversationSync := result.Sync.Conversations
		status := "completed_full_scope"
		if result.RequestedConversationLimit > 0 {
			status = "completed_bounded_scope"
		}
		return writeJSON(out, map[string]any{
			"state": monitor.State, "notifications": notifications.List(),
			"career_run": map[string]any{
				"status":                     status,
				"requested_conversations":    result.RequestedConversationLimit,
				"processed_conversations":    conversationSync.Fetched,
				"skipped":                    conversationSync.Skipped,
				"errors":                     len(conversationSync.Errors),
				"provider_reads":             conversationSync.Performance.Requests,
				"detail_reads":               conversationSync.DetailedChatsFetched,
				"notifications_created":      result.Notifications.Created,
				"notifications_resolved":     result.Notifications.Resolved,
				"notifications_deduplicated": result.Notifications.Deduplicated,
				"started_at":                 conversationSync.StartedAt,
				"finished_at":                conversationSync.FinishedAt,
			},
		})
	}
	fmt.Fprintf(out, "Career Monitor: read-only HH sync every %s\n", cfg.MonitorInterval)
	return monitor.Run(ctx)
}

func reconcileDryRun(args []string) bool {
	for _, arg := range args {
		if arg == "--dry-run" {
			return true
		}
		if arg == "--dry-run=false" {
			return false
		}
	}
	return false
}

func loadHHLocalStores(wd, profilePath string) (*ConversationStore, *ApplicationStore, *VacancyStore, *CandidateClarificationStore, *AIDraftStore, error) {
	conversations := NewConversationStore(filepath.Join(wd, EmployerConversationsFilename))
	applications := NewApplicationStoreWithDependencies(filepath.Join(wd, JobApplicationsFilename), conversations, nil)
	vacancies := NewVacancyStore(filepath.Join(wd, VacanciesFilename))
	clarifications := NewCandidateClarificationStore(filepath.Join(wd, "candidate_clarifications.json"))
	drafts := NewAIDraftStore(filepath.Join(wd, "ai_drafts.json"))
	for _, load := range []func() error{conversations.Load, applications.Load, vacancies.Load, clarifications.Load, drafts.Load} {
		if err := load(); err != nil {
			return nil, nil, nil, nil, nil, err
		}
	}
	_ = profilePath
	return conversations, applications, vacancies, clarifications, drafts, nil
}

// loadHHLocalStoresForBackend loads only backend-independent operational JSON
// state in PostgreSQL mode and binds the three career façades to the single
// selected repository bundle. Career JSON files are deliberately not read.
func loadHHLocalStoresForBackend(ctx context.Context, cfg Config, wd, profilePath string) (*ConversationStore, *ApplicationStore, *VacancyStore, *CandidateClarificationStore, *AIDraftStore, func(), error) {
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return nil, nil, nil, nil, nil, func() {}, err
	}
	conversations := NewConversationStore(filepath.Join(wd, EmployerConversationsFilename))
	applications := NewApplicationStoreWithDependencies(filepath.Join(wd, JobApplicationsFilename), conversations, nil)
	vacancies := NewVacancyStore(filepath.Join(wd, VacanciesFilename))
	clarifications := NewCandidateClarificationStore(filepath.Join(wd, "candidate_clarifications.json"))
	drafts := NewAIDraftStore(filepath.Join(wd, "ai_drafts.json"))
	if backend == storageBackendJSON {
		if err := clarifications.Load(); err != nil {
			return nil, nil, nil, nil, nil, func() {}, err
		}
		if err := drafts.Load(); err != nil {
			return nil, nil, nil, nil, nil, func() {}, err
		}
		for _, load := range []func() error{conversations.Load, applications.Load, vacancies.Load} {
			if err := load(); err != nil {
				return nil, nil, nil, nil, nil, func() {}, err
			}
		}
		return conversations, applications, vacancies, clarifications, drafts, func() {}, nil
	}
	career, closeCareer, err := BuildCareerRepositories(ctx, cfg, nil, nil, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, func() {}, err
	}
	conversations = newConversationStoreFromRepository(career.Conversations)
	applications = newApplicationStoreFromRepository(career.Applications)
	vacancies = newVacancyStoreFromRepository(career.Vacancies)
	applications.SetConversationStore(conversations)
	if career.Postgres == nil || career.Postgres.Pool() == nil {
		closeCareer()
		return nil, nil, nil, nil, nil, func() {}, errors.New("postgres operational stores require the selected career pool")
	}
	clarifications = jsonstorage.NewCandidateClarificationStoreWithBackend(postgresstorage.NewCandidateClarificationRepository(career.Postgres.Pool()))
	drafts = NewAIDraftStoreWithBackend(postgresstorage.NewAIDraftRepository(career.Postgres.Pool()))
	if err := clarifications.Load(); err != nil {
		closeCareer()
		return nil, nil, nil, nil, nil, func() {}, err
	}
	if err := drafts.Load(); err != nil {
		closeCareer()
		return nil, nil, nil, nil, nil, func() {}, err
	}
	_ = profilePath
	return conversations, applications, vacancies, clarifications, drafts, closeCareer, nil
}

func buildHHReadSyncService(ctx context.Context, cfg Config, client HHReadClient, vacancies *VacancyStore, applications *ApplicationStore, conversations *ConversationStore, analyzer interface {
	Analyze(Vacancy, any) MatchResult
}, candidate any, clarifications *CandidateClarificationStore, drafts *AIDraftStore, statePath string) (*HHReadSyncService, func(), error) {
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return nil, func() {}, err
	}
	cfg.StorageBackend = backend
	var career CareerRepositories
	var closeCareer func()
	if backend == storageBackendPostgres {
		if configured, ok := careerRepositoriesFromStores(vacancies, applications, conversations); ok {
			career, closeCareer = configured, func() {}
		} else {
			career, closeCareer, err = BuildCareerRepositories(ctx, cfg, vacancies, applications, conversations)
			if err != nil {
				return nil, func() {}, err
			}
		}
	} else {
		career, closeCareer, err = BuildCareerRepositories(ctx, cfg, vacancies, applications, conversations)
		if err != nil {
			return nil, func() {}, err
		}
	}
	options := HHReadSyncServiceOptions{
		Vacancies: vacancies, Applications: applications, Conversations: conversations,
		Analyzer: analyzer, Candidate: candidate, Clarifications: clarifications,
		Drafts: drafts, StatePath: statePath,
	}
	if backend == storageBackendPostgres {
		return NewHHReadSyncServiceWithRepositories(client, career, options), closeCareer, nil
	}
	return NewHHReadSyncServiceWithOptions(client, options), closeCareer, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

var osGetwd = func() (string, error) { return os.Getwd() }

func hhSyncTarget(args []string) (string, error) {
	if len(args) == 1 && args[0] == "sync" {
		return "all", nil
	}
	if len(args) == 2 && args[0] == "sync" {
		switch args[1] {
		case "vacancies", "applications", "conversations":
			return args[1], nil
		}
	}
	return "", errors.New("usage: hh sync [vacancies|applications|conversations] | hh sync conversation <conversation-id-or-chat-id>")
}
func writeHHSyncReport(service *HHReadSyncService, target string, out io.Writer) error {
	var output any
	var err error
	var failed bool
	switch target {
	case "all":
		var result HHSyncAllResult
		result, err = service.SyncAll()
		output = result
		failed = len(result.Vacancies.Errors)+len(result.Applications.Errors)+len(result.Conversations.Errors) > 0
	case "vacancies", "applications", "conversations":
		var result SyncResult
		switch target {
		case "vacancies":
			result, err = service.SyncVacancies()
		case "applications":
			result, err = service.SyncApplications()
		case "conversations":
			result, err = service.SyncConversations()
		}
		output = result
		failed = len(result.Errors) > 0
	default:
		return errors.New("unknown hh sync target")
	}
	if writeErr := writeJSON(out, output); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return err
	}
	if failed {
		return errors.New("HH read-only sync incomplete; see JSON report")
	}
	return nil
}
