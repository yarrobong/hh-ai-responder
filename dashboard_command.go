package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const dashboardDefaultHost = "127.0.0.1"
const dashboardDefaultPort = 8080

func dailyRefreshStateFor(wd string) string { return filepath.Join(wd, DailyRefreshStateFilename) }

type DashboardOptions struct {
	FollowUpPolicy FollowUpPolicy
	Host           string
	Port           int
}

func parseDashboardOptions(args []string, getenv func(string) string, out io.Writer) (DashboardOptions, error) {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(out)
	host, port := dashboardDefaultHost, strconv.Itoa(dashboardDefaultPort)
	if v := getenv("HH_WEB_HOST"); v != "" {
		host = v
	}
	if v := getenv("HH_WEB_PORT"); v != "" {
		port = v
	}
	policy := DefaultFollowUpPolicy()
	followUpPolicyFlags(fs, &policy)
	fs.StringVar(&host, "host", host, "Dashboard bind address (loopback only)")
	fs.StringVar(&port, "port", port, "Dashboard port")
	if err := fs.Parse(args); err != nil {
		return DashboardOptions{}, err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var policyErr error
	policy, policyErr = followUpPolicyEnv(policy, getenv, set)
	if policyErr != nil {
		return DashboardOptions{}, policyErr
	}
	if fs.NArg() != 0 {
		return DashboardOptions{}, errors.New("usage: web [--host 127.0.0.1] [--port 8080]")
	}
	// This MVP has no authentication. Never expose private records to a LAN.
	if host == "localhost" {
		host = dashboardDefaultHost
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return DashboardOptions{}, errors.New("dashboard host must be a loopback IP or localhost")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return DashboardOptions{}, errors.New("dashboard port must be between 1 and 65535")
	}
	return DashboardOptions{Host: host, Port: n, FollowUpPolicy: policy}, nil
}

// Lazy construction keeps browsing available without HH cookies. The only
// capability returned to the dashboard is the existing GET-only adapter.
type dashboardHHReader struct {
	mu     sync.Mutex
	cfg    Config
	ctx    context.Context
	client HHReadClient
}

type dashboardHHWriter struct {
	cfg       Config
	ctx       context.Context
	responder *HHAIResponder
}

func (c *dashboardHHWriter) writer() (*HHAIResponder, error) {
	if c.responder != nil {
		return c.responder, nil
	}
	cfg := c.cfg
	cfg.HHReadOnly = false
	cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = false, false, false, false
	cfg.ChatMode = "off"
	cfg.OutputPath = ""
	r, err := NewHHAIResponder(c.ctx, cfg)
	if err != nil {
		return nil, errors.New("HH writer unavailable; check local HH configuration and cookies")
	}
	c.responder = r
	return r, nil
}

func (c *dashboardHHWriter) SendConversationMessage(ctx context.Context, conversationID, text, idempotencyKey string) (HHWriteTransportResult, error) {
	if err := ctx.Err(); err != nil {
		return HHWriteTransportResult{}, err
	}
	r, err := c.writer()
	if err != nil {
		return HHWriteTransportResult{}, err
	}
	chatID := parseHHChatID(conversationID)
	if chatID <= 0 {
		return HHWriteTransportResult{}, errors.New("HH conversation external id is not numeric")
	}
	return r.sendConversationMessage(chatID, text, idempotencyKey)
}

func parseHHChatID(value string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id
}

func dashboardReadConfig(cfg Config) Config {
	cfg.HHReadOnly = true
	cfg.HHWriteEnabled = false
	cfg.DryRun, cfg.AutoApply, cfg.AutoChat, cfg.AutoTouch, cfg.AutoJobStatus = true, false, false, false, false
	cfg.ChatMode = "off"
	// The full responder constructor otherwise merges and saves a legacy profile
	// and opens an event log. Dashboard already owns its candidate knowledge.
	cfg.CandidateProfilePath, cfg.CandidateStoriesPath, cfg.OutputPath = "", "", ""
	return cfg
}

func (c *dashboardHHReader) reader() (HHReadClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client, nil
	}
	cfg := dashboardReadConfig(c.cfg)
	r, err := NewHHAIResponder(c.ctx, cfg)
	if err != nil {
		return nil, errors.New("HH reader unavailable; check local HH configuration and cookies")
	}
	c.client = NewHHAIResponderReadClient(r)
	return c.client, nil
}
func (c *dashboardHHReader) ReadVacancies(ctx context.Context, cursor string) (HHVacancyPage, error) {
	r, err := c.reader()
	if err != nil {
		return HHVacancyPage{}, err
	}
	return r.ReadVacancies(ctx, cursor)
}
func (c *dashboardHHReader) ReadApplications(ctx context.Context, cursor string) (HHApplicationPage, error) {
	r, err := c.reader()
	if err != nil {
		return HHApplicationPage{}, err
	}
	return r.ReadApplications(ctx, cursor)
}
func (c *dashboardHHReader) ReadConversations(ctx context.Context, cursor string) (HHConversationPage, error) {
	r, err := c.reader()
	if err != nil {
		return HHConversationPage{}, err
	}
	return r.ReadConversations(ctx, cursor)
}

func (c *dashboardHHReader) ReadConversation(ctx context.Context, externalID string) (HHConversationRecord, error) {
	r, err := c.reader()
	if err != nil {
		return HHConversationRecord{}, err
	}
	reader, ok := r.(HHConversationRecordReader)
	if !ok {
		return HHConversationRecord{}, errors.New("targeted HH conversation read is unavailable")
	}
	return reader.ReadConversation(ctx, externalID)
}

func (c *dashboardHHReader) ReadConversationState(ctx context.Context, externalID string) (HHConversationReadState, error) {
	r, err := c.reader()
	if err != nil {
		return HHConversationReadState{}, err
	}
	reader, ok := r.(HHConversationPreflightReader)
	if !ok {
		return HHConversationReadState{}, errors.New("HH conversation preflight is unavailable")
	}
	return reader.ReadConversationState(ctx, externalID)
}

func loadDashboard(ctx context.Context, wd string, cfg Config) (*DashboardServer, error) {
	start := time.Now()
	defer perfRecord("app.startup", start, 1)
	profile := cfg.CandidateProfilePath
	if profile == "" {
		profile = filepath.Join(wd, "candidate_profile.json")
	}
	conversations, applications, vacancies, clarifications, drafts, err := loadHHLocalStores(wd, profile)
	if err != nil {
		return nil, err
	}
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return nil, err
	}
	var candidatePersistence CandidatePersistence
	var candidateClose func()
	if backend == storageBackendPostgres {
		candidatePersistence, candidateClose, err = BuildCandidatePersistence(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	var kb *CandidateKnowledgeBase
	var resolver *CandidateContextResolver
	var mutation *CandidateMutationService
	if backend == storageBackendPostgres {
		candidate, candidateErr := candidatePersistence.Repository.CurrentCandidate(ctx)
		if candidateErr != nil {
			return nil, candidateErr
		}
		kb = CandidateKnowledgeSnapshot(candidate, profile)
		resolver = NewCandidateContextResolverFromCandidate(candidate)
		mutation = candidatePersistence.Mutations
	} else {
		kb = NewCandidateKnowledgeBase(profile)
		if err := kb.Load(); err != nil {
			return nil, err
		}
		resolver = NewCandidateContextResolver(kb)
	}
	applications.SetCandidateContextResolver(resolver)
	updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser})
	// Free-text answers remain unknowns requiring confirmation, never confirmed facts.
	proposalUpdater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorAI})
	ai := NewAIClient(ctx, cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, cfg.AITimeout, cfg.AIConnectTimeout, cfg.AIAttempts)
	var orchestrator *AIReplyOrchestrator
	if mutation != nil {
		orchestrator = NewAIReplyOrchestrator(ai, conversations, applications, resolver, drafts, clarifications, mutation, candidatePersistence.SemanticRetriever)
	} else {
		orchestrator = NewAIReplyOrchestrator(ai, conversations, applications, resolver, drafts, clarifications, proposalUpdater, candidatePersistence.SemanticRetriever)
	}
	syncPath := cfg.HHSyncStatePath
	if syncPath == "" {
		syncPath = filepath.Join(wd, HHSyncStateFilename)
	}
	readClient := &dashboardHHReader{cfg: cfg, ctx: ctx}
	service := NewHHReadSyncService(readClient, vacancies, applications, conversations, NewVacancyAnalyzer(), kb, clarifications, drafts, syncPath)
	if err := service.LoadState(); err != nil {
		return nil, err
	}
	notifications := NewNotificationStore(filepath.Join(wd, NotificationEventsFilename))
	if err := notifications.Load(); err != nil {
		return nil, err
	}
	qualityLog := NewQualityLogStore(filepath.Join(wd, QualityLogFilename))
	if err := qualityLog.Load(); err != nil {
		return nil, err
	}
	if cfg.FollowUpPolicy == (FollowUpPolicy{}) {
		cfg.FollowUpPolicy = DefaultFollowUpPolicy()
	}
	actions := NewApprovedHHActionStore(filepath.Join(wd, HHWriteActionsFilename))
	if err := actions.Load(); err != nil {
		return nil, err
	}
	audit := NewHHWriteAuditStore(filepath.Join(wd, HHWriteEventsFilename))
	if err := audit.Load(); err != nil {
		return nil, err
	}
	observations := NewPilotObservationStore(filepath.Join(wd, HHPilotObservationsFilename))
	if err := observations.Load(); err != nil {
		return nil, err
	}
	var writer HHWriteClient
	if cfg.HHWriteEnabled && !cfg.DryRun {
		writer = &dashboardHHWriter{cfg: cfg, ctx: ctx}
	}
	lifecycle := NewDashboardLifecycleStore()
	gateway := NewHHWriteGateway(HHWriteGatewayOptions{Enabled: cfg.HHWriteEnabled, DryRun: cfg.DryRun, RequireFreshRead: true, MaxWritesPerRun: cfg.HHMaxWritesPerRun, MaxWritesPerDay: cfg.HHMaxWritesPerDay, Client: writer, ReadClient: readClient, Conversations: conversations, Applications: applications, Drafts: drafts, Clarifications: clarifications, Observations: observations, Resolver: resolver, Orchestrator: orchestrator, FollowUpPolicy: cfg.FollowUpPolicy, ChatURL: cfg.HHChatURL, Actions: actions, Audit: audit, Lifecycle: lifecycle})
	return NewDashboardServer(DashboardDependencies{FollowUpPolicy: cfg.FollowUpPolicy, Vacancies: vacancies, Applications: applications, Conversations: conversations, Knowledge: kb, Drafts: drafts, Clarifications: clarifications, Sync: service, Orchestrator: orchestrator, Resolver: resolver, Updater: updater, ProposalUpdater: proposalUpdater, Mutation: mutation, CandidateClose: candidateClose, Notifications: notifications, QualityLog: qualityLog, WriteGateway: gateway, Lifecycle: lifecycle, ConversationDisplayTTL: cfg.ConversationDisplayTTL, BackgroundInboxRefresh: cfg.BackgroundInboxRefresh, DailyRefreshStatePath: dailyRefreshStateFor(wd)})
}

func runDashboardCommand(args []string, cfg Config, out io.Writer) error {
	opts, err := parseDashboardOptions(args, os.Getenv, out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg.FollowUpPolicy = opts.FollowUpPolicy
	dashboard, err := loadDashboard(ctx, wd, cfg)
	if err != nil {
		return err
	}
	if dashboard.CandidateClose != nil {
		defer dashboard.CandidateClose()
	}
	dashboard.backgroundContext = ctx
	dashboard.startBackgroundInboxRefresh(ctx, cfg.MonitorInterval)
	address := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: dashboard, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-done:
		}
	}()
	capability := "HH read-only"
	if cfg.HHWriteEnabled && !cfg.DryRun {
		capability = "HH manual approval writes"
	}
	fmt.Fprintf(out, "Career Agent Dashboard: http://%s (%s)\n", address, capability)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
