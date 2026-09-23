package runtime

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hh-ai-responder/internal/ports"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	autochatreconciliation "hh-ai-responder/internal/usecase/autochatreconciliation"
	"hh-ai-responder/internal/usecase/inboxrefresh"
	reliabilityinspection "hh-ai-responder/internal/usecase/reliabilityinspection"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
	"hh-ai-responder/internal/vacancyranking"
	"hh-ai-responder/internal/vacancyreview"
)

//go:embed web/index.html web/app.js web/styles.css
var dashboardAssets embed.FS

type DashboardDependencies struct {
	FollowUpPolicy            FollowUpPolicy
	Vacancies                 *VacancyStore
	Applications              *ApplicationStore
	Conversations             *ConversationStore
	Knowledge                 *CandidateKnowledgeBase
	Drafts                    *AIDraftStore
	Clarifications            *CandidateClarificationStore
	Sync                      *HHReadSyncService
	Orchestrator              *AIReplyOrchestrator
	CandidateLearning         *CandidateKnowledgeAcquisitionService
	Resolver                  *CandidateContextResolver
	Updater, ProposalUpdater  *CandidateKnowledgeUpdater
	Mutation                  *CandidateMutationService
	CandidateClose            func()
	CareerClose               func()
	Notifications             *NotificationStore
	WriteGateway              *HHWriteGateway
	Lifecycle                 *DashboardLifecycleStore
	ConversationDisplayTTL    time.Duration
	BackgroundInboxRefresh    bool
	DailyRefreshStatePath     string
	DailyRefreshState         DailyRefreshState
	QualityLog                *QualityLogStore
	Reliability               *reliabilityinspection.Service
	ReliabilityBackend        string
	ApplicationReconciliation *applicationreconciliation.Service
	AutoChatReconciliation    *autochatreconciliation.Service
	ReliabilityNotifications  reliabilitynotifications.Sink
	ControlledReconciliation  ControlledReconciler
	RankedQueue               *vacancyranking.QueueService
	VacancyReviews            vacancyreview.Store
	CareerWorkflow            ports.CareerWorkflowStore
	Daily                     *DailyCareerAgentService
}

type DashboardServer struct {
	backgroundContext context.Context
	asyncTargets      map[string]bool
	files             map[string]dashboardFile
	contextCache      map[string]dashboardContextEntry
	viewCache         map[string]dashboardViewEntry
	generation        uint64
	DashboardDependencies
	// mu serializes refreshes and all mutable dashboard operations. Proven
	// read projections acquire RLock after the refresh check below; their
	// inputs are request-local values or independently synchronized stores.
	mu                       sync.RWMutex
	cacheMu                  sync.RWMutex
	refreshMu                sync.Mutex
	notificationMu           sync.Mutex
	notificationMetaMu       sync.RWMutex
	running                  atomic.Bool
	statusMu                 sync.RWMutex
	syncState                HHSyncState
	syncResult               any
	draftTargets             map[string]bool
	draftMu                  sync.Mutex
	draftWorkersActive       atomic.Int64
	draftWorkersStarted      atomic.Int64
	draftWorkersMaxActive    atomic.Int64
	decisions                map[string]AIResponseDecision
	lastNotificationStats    DailyNotificationStats
	lastNotificationCreated  map[string]bool
	displayTTL               time.Duration
	backgroundRefreshStarted atomic.Bool
	inboxRefresh             *inboxrefresh.Service
}

func NewDashboardServer(d DashboardDependencies) (*DashboardServer, error) {
	if d.Vacancies == nil || d.Applications == nil || d.Conversations == nil || d.Knowledge == nil || d.Drafts == nil || d.Clarifications == nil || d.Sync == nil || d.Orchestrator == nil || d.Resolver == nil || d.Updater == nil || d.ProposalUpdater == nil {
		return nil, errors.New("dashboard requires local stores and services")
	}
	if d.FollowUpPolicy == (FollowUpPolicy{}) {
		d.FollowUpPolicy = DefaultFollowUpPolicy()
	}
	if err := d.FollowUpPolicy.Validate(); err != nil {
		return nil, err
	}
	if d.Lifecycle == nil {
		d.Lifecycle = NewDashboardLifecycleStore()
	}
	if d.QualityLog == nil {
		d.QualityLog = NewQualityLogStore("")
	}
	if d.ReliabilityNotifications == nil && d.Notifications != nil {
		d.ReliabilityNotifications = reliabilitynotifications.NewProjector(d.Notifications)
	}
	if err := d.QualityLog.Load(); err != nil {
		return nil, err
	}
	if d.ConversationDisplayTTL <= 0 {
		d.ConversationDisplayTTL = defaultConversationDisplayTTL
	}
	if strings.TrimSpace(d.DailyRefreshStatePath) == "" {
		if d.Notifications != nil && d.Notifications.path != "" {
			d.DailyRefreshStatePath = filepath.Join(filepath.Dir(d.Notifications.path), DailyRefreshStateFilename)
		} else {
			d.DailyRefreshStatePath = DailyRefreshStateFilename
		}
	}
	state, err := loadDailyRefreshState(d.DailyRefreshStatePath)
	if err != nil {
		return nil, err
	}
	d.DailyRefreshState = state
	s := &DashboardServer{DashboardDependencies: d, syncState: d.Sync.SyncState(), decisions: map[string]AIResponseDecision{}, lastNotificationCreated: map[string]bool{}, displayTTL: d.ConversationDisplayTTL}
	// Confirmation/rejection must use the trusted user mutation adapter. The
	// orchestrator's acquisition instance may deliberately use an AI-actor
	// updater for proposal creation, which must not be reused for confirmation.
	if s.CandidateLearning == nil && s.Mutation != nil {
		s.CandidateLearning = NewCandidateKnowledgeAcquisitionService(s.Resolver, s.Mutation, s.Clarifications, nil)
	} else if s.CandidateLearning == nil && s.Updater != nil {
		s.CandidateLearning = NewCandidateKnowledgeAcquisitionService(s.Resolver, s.Updater, s.Clarifications, nil)
	}
	s.inboxRefresh = s.newInboxRefreshService()
	if err := s.refreshLocalFiles(); err != nil {
		return nil, err
	}
	s.Sync.externalCommitMu = &dashboardTimedLocker{locker: &s.mu}
	return s, nil
}

func dashboardJSON(w http.ResponseWriter, code int, value any) {
	dashboardJSONTimed(w, code, value, "")
}

func dashboardJSONTimed(w http.ResponseWriter, code int, value any, operation string) {
	start := time.Now()
	// Marshal before headers, with the standard encoder's HTML escaping enabled.
	raw, err := json.Marshal(value)
	if operation != "" {
		perfRecord(operation, start, len(raw))
	}
	if err != nil {
		code, raw = 500, []byte(`{"error":"Cannot encode local data"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(append(raw, '\n'))
}
func dashboardError(w http.ResponseWriter, code int, message string) {
	dashboardJSON(w, code, map[string]string{"error": message})
}

func (s *DashboardServer) knowledgeSnapshot() *CandidateKnowledgeBase {
	if s == nil {
		return NewCandidateKnowledgeBase("")
	}
	if s.Mutation != nil && s.Mutation.reader != nil {
		if candidate, err := s.Mutation.reader.CurrentCandidate(context.Background()); err == nil {
			return CandidateKnowledgeSnapshot(candidate, s.Knowledge.ProfilePath)
		}
	}
	return s.Knowledge
}

func (s *DashboardServer) refreshCandidateResolver() {
	if s == nil || s.Mutation == nil || s.Mutation.reader == nil {
		return
	}
	if candidate, err := s.Mutation.reader.CurrentCandidate(context.Background()); err == nil {
		s.Resolver = NewCandidateContextResolverFromCandidate(candidate)
		s.Applications.SetCandidateContextResolver(s.Resolver)
	}
}

func (s *DashboardServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer perfRecord("dashboard.http", start, 1)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	// Host checks stop DNS rebinding; Origin and a non-simple header stop CSRF.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			dashboardError(w, 403, "Local host required")
			return
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "http" || u.Host != r.Host || u.Path != "" {
			dashboardError(w, 403, "Same origin required")
			return
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		dashboardError(w, 403, "Same origin required")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.servePage(w, r)
		return
	}
	p := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	method := dashboardRouteMethod(p)
	if method == "" {
		dashboardError(w, 404, "Not found")
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		dashboardError(w, 405, "Method not allowed")
		return
	}
	if method == http.MethodPost {
		if r.Header.Get("X-Career-Agent") != "local" {
			dashboardError(w, 403, "Local action header required")
			return
		}
		if media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); media != "application/json" {
			dashboardError(w, 400, "Expected application/json")
			return
		}
	}
	if r.URL.Path == "/api/sync/status" {
		dashboardJSON(w, 200, s.syncStatus())
		return
	}
	if method == http.MethodGet && len(p) >= 2 && p[0] == "reliability" {
		s.readReliabilityAPI(w, r, p)
		return
	}
	if method == http.MethodPost && (p[0] == "sync" || len(p) == 3 && p[0] == "conversations" && p[2] == "sync") {
		s.handleSync(w, r, p)
		return
	}
	if method == http.MethodPost && len(p) == 3 && p[0] == "actions" && p[2] == "preflight" {
		if !s.prepareTargetedPreflight(w, r, p[1]) {
			return
		}
	}
	// Daily orchestration owns its own overlap guard and durable lifecycle. Do
	// not hold the dashboard mutation lock while it performs read-only HH sync
	// and AI analysis; otherwise a manual run would freeze unrelated views.
	if method == http.MethodPost && len(p) == 2 && p[0] == "career" && p[1] == "run" {
		s.runDailyCareerAgentAPI(w, r)
		return
	}
	if method == http.MethodGet && dashboardReadOnlyPath(p) {
		if err := s.refreshForRead(); err != nil {
			dashboardError(w, 500, "Cannot reload local data")
			return
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		s.serveCachedAPI(w, r, p)
		return
	}

	// Queue local store work instead of making the user guess when a previous
	// operation will finish. HH network work is handled by the sync service's
	// singleflight calls and global priority limiter.
	mutexWaitStart := time.Now()
	s.mu.Lock()
	perfRecord("dashboard.mutex_wait", mutexWaitStart, 1)
	if method == http.MethodGet && len(p) > 0 && p[0] == "notifications" {
		perfRecord("dashboard.notifications.mutex_wait", mutexWaitStart, 1)
	}
	mutexHoldStart := time.Now()
	defer perfRecord("dashboard.mutex_hold", mutexHoldStart, 1)
	if method == http.MethodGet && len(p) > 0 && p[0] == "notifications" {
		defer perfRecord("dashboard.notifications.mutex_hold", mutexHoldStart, 1)
		handlerStart := time.Now()
		defer perfRecord("dashboard.notifications.handler", handlerStart, 1)
	}
	defer s.mu.Unlock()
	if err := s.refreshLocalFiles(); err != nil {
		dashboardError(w, 500, "Cannot reload local data")
		return
	}
	if method == http.MethodGet {
		s.serveCachedAPI(w, r, p)
		return
	}
	s.invalidateViews()
	defer s.refreshLocalFiles()
	if method == http.MethodPost && len(p) == 4 && p[0] == "reliability" && p[3] == "reconcile" {
		s.reconcileReliabilityAPI(w, r, p)
		return
	}
	s.writeAPI(w, r, p)
}

func dashboardReadOnlyPath(p []string) bool {
	if len(p) == 1 {
		return p[0] == "dashboard"
	}
	if len(p) == 2 && (p[0] == "inbox" && p[1] == "overview" || p[0] == "notifications" && p[1] == "overview") {
		return true
	}
	return p[0] == "career" && (len(p) == 2 || len(p) == 3)
}

func dashboardRouteMethod(p []string) string {
	if len(p) == 1 {
		switch p[0] {
		case "generation", "performance", "health", "eligibility", "pilot-candidates", "pilot-shortlist", "follow-ups", "dashboard", "today", "vacancies", "applications", "conversations", "inbox", "knowledge", "analytics", "notifications", "quality", "actions":
			return "GET"
		case "sync":
			return "POST"
		}
	}
	if len(p) == 2 {
		switch p[0] {
		case "career":
			if p[1] == "review-queue" || p[1] == "runs" || p[1] == "metrics" || p[1] == "attention" {
				return "GET"
			}
		case "notifications":
			if p[1] == "overview" {
				return "GET"
			}
		case "inbox":
			if p[1] == "overview" {
				return "GET"
			}
		case "health":
			if p[1] == "deep" {
				return "GET"
			}
		case "diagnostics":
			if p[1] == "lifecycle" {
				return "GET"
			}
		case "vacancies", "applications", "conversations":
			if p[1] != "" {
				return "GET"
			}
		case "knowledge":
			if p[1] == "questions" {
				return "GET"
			}
		case "sync":
			if p[1] == "status" {
				return "GET"
			}
			if p[1] == "vacancies" || p[1] == "applications" || p[1] == "conversations" || p[1] == "inbox" {
				return "POST"
			}
		}
	}
	if len(p) == 3 && p[0] == "career" && p[1] == "vacancies" && p[2] != "" {
		return "GET"
	}
	if len(p) == 2 && p[0] == "career" && p[1] == "run" {
		return "POST"
	}
	if len(p) == 3 && p[0] == "vacancies" && p[1] != "" && p[2] == "ranking" {
		return "GET"
	}
	if len(p) == 4 && p[0] == "vacancies" && p[1] != "" && p[2] == "review" && (p[3] == "seen" || p[3] == "interesting" || p[3] == "dismiss") {
		return "POST"
	}
	if len(p) == 2 && p[0] == "reliability" && (p[1] == "application-attempts" || p[1] == "autochat-attempts") {
		return "GET"
	}
	if len(p) == 3 && p[0] == "reliability" && (p[1] == "application-attempts" || p[1] == "autochat-attempts") && p[2] != "" {
		return "GET"
	}
	if len(p) == 4 && p[0] == "reliability" && (p[1] == "application-attempts" || p[1] == "autochat-attempts") && p[2] != "" && p[3] == "reconcile" {
		return "POST"
	}
	if len(p) == 3 && p[0] == "follow-ups" && p[1] != "" && (p[2] == "draft" || p[2] == "dismiss") {
		return "POST"
	}
	if len(p) == 3 && p[0] == "notifications" && p[1] != "" && (p[2] == "ack" || p[2] == "seen" || p[2] == "dismiss" || p[2] == "resolve" || p[2] == "snooze" || p[2] == "open" || p[2] == "irrelevant") {
		return "POST"
	}
	if len(p) == 3 && p[0] == "conversations" && p[1] != "" && (p[2] == "classification-feedback" || p[2] == "irrelevant") {
		return "POST"
	}
	if len(p) == 3 && p[0] == "drafts" && p[1] != "" && p[2] == "feedback" {
		return "POST"
	}
	if len(p) == 3 && p[0] == "actions" && p[1] != "" && (p[2] == "send" || p[2] == "cancel" || p[2] == "preflight" || p[2] == "reconcile") {
		return "POST"
	}
	if len(p) == 3 && p[1] != "" {
		if p[0] == "conversations" && (p[2] == "draft" || p[2] == "sync") || p[0] == "drafts" && (p[2] == "reject" || p[2] == "edit" || p[2] == "approve") {
			return "POST"
		}
	}
	if len(p) == 4 && p[0] == "knowledge" && p[2] != "" {
		if (p[1] == "clarifications" || p[1] == "unknowns") && p[3] == "answer" || p[1] == "proposals" && (p[3] == "confirm" || p[3] == "reject") {
			return "POST"
		}
	}
	return ""
}

func (s *DashboardServer) servePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	file := "web/index.html"
	contentType := "text/html; charset=utf-8"
	switch path {
	case "/app.js":
		file, contentType = "web/app.js", "text/javascript; charset=utf-8"
	case "/styles.css":
		file, contentType = "web/styles.css", "text/css; charset=utf-8"
	default:
		valid := path == "/health" || path == "/" || path == "/today" || path == "/inbox" || path == "/applications" || path == "/vacancies" || path == "/knowledge" || path == "/knowledge/questions" || path == "/analytics" || path == "/sync" || path == "/reliability" || path == "/career"
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) == 2 && parts[1] != "" && (parts[0] == "applications" || parts[0] == "vacancies" || parts[0] == "conversations") {
			valid = true
		}
		if len(parts) == 3 && parts[0] == "reliability" && parts[1] != "" && parts[2] != "" {
			valid = true
		}
		if len(parts) == 3 && parts[0] == "career" && parts[1] == "vacancies" && parts[2] != "" {
			valid = true
		}
		if !valid {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		dashboardError(w, 405, "Method not allowed")
		return
	}
	raw, err := dashboardAssets.ReadFile(file)
	if err != nil {
		dashboardError(w, 500, "Page unavailable")
		return
	}
	w.Header().Set("Content-Type", contentType)
	if r.Method == "GET" {
		_, _ = w.Write(raw)
	}
}

func (s *DashboardServer) readAPI(w http.ResponseWriter, r *http.Request, p []string) {
	var result any
	var err error
	switch p[0] {
	case "health":
		if len(p) == 2 && p[1] == "deep" {
			report := BuildCareerAuditReport(s.careerSnapshot(), s.FollowUpPolicy, time.Now())
			raw, _ := json.Marshal(report)
			var health map[string]any
			_ = json.Unmarshal(raw, &health)
			health["mode"] = "deep"
			health["write_gateway"] = s.writeHealth()
			if reports, eligibilityErr := BuildConversationEligibilityReports(s.Conversations, s.Applications, s.Clarifications, s.Resolver); eligibilityErr == nil {
				health["manual_reply_eligibility"] = map[string]any{"summary": SummarizeEligibility(reports), "conversations": reports}
			} else {
				health["manual_reply_eligibility"] = map[string]any{"error": "eligibility diagnostics unavailable"}
			}
			result = health
		} else {
			result = s.healthFast()
		}
	case "eligibility":
		reports, eligibilityErr := BuildConversationEligibilityReports(s.Conversations, s.Applications, s.Clarifications, s.Resolver)
		if eligibilityErr != nil {
			dashboardError(w, 500, "Eligibility diagnostics unavailable")
			return
		}
		result = reports
	case "pilot-candidates":
		result, err = BuildPilotCandidateReports(s.Conversations, s.Applications, s.Clarifications, s.Resolver, s.Drafts, time.Now().UTC())
	case "pilot-shortlist":
		result, err = BuildPilotShortlistPreviews(s.Conversations, s.Applications, s.Vacancies, s.Clarifications, s.Resolver, time.Now().UTC(), 5)
	case "follow-ups":
		result = s.careerSnapshot().FollowUps(s.FollowUpPolicy, time.Now())
	case "dashboard", "analytics":
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "all"
		}
		if period != "all" && period != "today" && period != "7d" && period != "30d" {
			dashboardError(w, 400, "Invalid period")
			return
		}
		result, err = s.analytics(period, time.Now())
	case "vacancies":
		if len(p) >= 2 && (p[1] == "ranked" || len(p) == 3 && p[2] == "ranking") && s.RankedQueue == nil {
			dashboardError(w, 503, "Ranked vacancy queue unavailable")
			return
		}
		if len(p) == 2 && p[1] == "ranked" {
			result, err = s.rankedQueue(r.URL.Query())
		} else if len(p) == 3 && p[2] == "ranking" {
			result, err = s.rankedVacancyDetail(p[1])
		} else if len(p) == 2 {
			var id int
			id, err = strconv.Atoi(p[1])
			if err != nil {
				dashboardError(w, 404, "Vacancy not found")
				return
			}
			result, err = s.Vacancies.Get(id)
		} else {
			result, err = s.vacancyList(r.URL.Query())
			if err != nil {
				dashboardError(w, 400, "Invalid vacancy filters")
				return
			}
		}
	case "applications":
		if len(p) == 2 {
			result, err = s.applicationDetail(p[1])
		} else {
			result, err = s.applicationList(r.URL.Query())
			if err != nil {
				dashboardError(w, 400, "Invalid application filters")
				return
			}
		}
	case "conversations":
		if len(p) == 2 {
			result, err = s.conversationDetail(p[1])
		} else {
			result, err = s.Conversations.ListConversations()
		}
	case "actions":
		if s.WriteGateway == nil {
			result = []ApprovedHHAction{}
		} else {
			if err = s.WriteGateway.Actions.Reload(); err != nil {
				break
			}
			result = s.WriteGateway.Actions.List()
		}
	case "diagnostics":
		result = s.Lifecycle.List(r.URL.Query().Get("action_id"))
	case "inbox":
		if len(p) == 2 && p[1] == "overview" {
			handlerStart := time.Now()
			defer perfRecord("dashboard.inbox_overview.handler", handlerStart, 1)
			result, err = s.inboxOverview()
		} else {
			result, err = s.inbox()
		}
	case "today":
		result, err = s.today()
	case "knowledge":
		clarifications, e := s.Clarifications.List()
		err = e
		knowledge := s.knowledgeSnapshot()
		result = map[string]any{"skills": knowledge.Skills, "projects": knowledge.Projects, "achievements": knowledge.Achievements, "unknowns": knowledge.Unknowns, "proposals": knowledge.Proposals, "clarifications": clarifications}
	case "notifications":
		refreshStart := time.Now()
		_ = s.refreshNotifications(time.Now().UTC())
		perfRecord("dashboard.notifications.refresh", refreshStart, 1)
		projectionStart := time.Now()
		if len(p) == 2 && p[1] == "overview" {
			result = s.activeNotificationsOverview()
		} else {
			result = s.activeNotifications()
		}
		perfRecord("dashboard.notifications.projection", projectionStart, 1)
	case "quality":
		result = BuildQualityReport(s.QualityLog.List())
	case "reliability":
		s.readReliabilityAPI(w, r, p)
		return
	case "career":
		s.readCareerAPI(w, r, p)
		return
	}
	if err != nil {
		if errors.Is(err, errRankedQueueUnavailable) {
			dashboardError(w, 503, "Ranked vacancy queue unavailable")
			return
		}
		if errors.Is(err, errInvalidRankedQueueFilter) {
			dashboardError(w, 400, "Invalid ranked queue filters")
			return
		}
		if errors.Is(err, ErrApplicationNotFound) || errors.Is(err, ErrConversationNotFound) || errors.Is(err, ErrVacancyNotFound) {
			dashboardError(w, 404, "Record not found")
		} else {
			dashboardError(w, 500, "Cannot read local data")
		}
		return
	}
	if p[0] == "notifications" {
		dashboardJSONTimed(w, 200, result, "dashboard.notifications.serialization")
		return
	}
	dashboardJSON(w, 200, result)
}

func decodeDashboardBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		dashboardError(w, 400, "Invalid JSON object (limit 16 KiB)")
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		dashboardError(w, 400, "Invalid JSON fields")
		return false
	}
	if dec.Decode(new(any)) != io.EOF {
		dashboardError(w, 400, "Expected one JSON object")
		return false
	}
	return true
}

func (s *DashboardServer) writeAPI(w http.ResponseWriter, r *http.Request, p []string) {
	var body struct {
		Answer         string `json:"answer"`
		AnswerKind     string `json:"answer_kind,omitempty"`
		ChoiceID       string `json:"choice_id,omitempty"`
		Until          string `json:"until,omitempty"`
		Text           string `json:"text,omitempty"`
		ApprovedBy     string `json:"approved_by,omitempty"`
		Nonce          string `json:"nonce,omitempty"`
		UIEvent        string `json:"ui_event,omitempty"`
		Correct        *bool  `json:"correct,omitempty"`
		State          string `json:"state,omitempty"`
		CorrectedState string `json:"corrected_state,omitempty"`
		Accepted       *bool  `json:"accepted,omitempty"`
		EditedText     string `json:"edited_text,omitempty"`
		Reason         string `json:"reason_category,omitempty"`
		ReviewReason   string `json:"reason,omitempty"`
	}
	answerAction := len(p) == 4 && p[3] == "answer"
	draftEdit := len(p) == 3 && p[0] == "drafts" && p[2] == "edit"
	if draftEdit {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Text) == "" {
			dashboardError(w, 400, "Text is required")
			return
		}
	} else if len(p) == 3 && p[0] == "drafts" && p[2] == "approve" {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.ApprovedBy) == "" {
			body.ApprovedBy = "dashboard_user"
		}
	} else if len(p) == 3 && p[0] == "actions" && (p[2] == "send" || p[2] == "cancel" || p[2] == "preflight" || p[2] == "reconcile") {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
	} else if answerAction {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Answer) == "" && strings.TrimSpace(body.ChoiceID) == "" {
			dashboardError(w, 400, "Answer is required")
			return
		}
	} else if len(p) == 3 && p[0] == "notifications" && (p[2] == "snooze" || p[2] == "seen" || p[2] == "dismiss" || p[2] == "resolve" || p[2] == "ack" || p[2] == "open" || p[2] == "irrelevant") {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
	} else if len(p) == 3 && p[0] == "conversations" && (p[2] == "classification-feedback" || p[2] == "irrelevant") {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
	} else if len(p) == 3 && p[0] == "drafts" && p[2] == "feedback" {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
	} else if len(p) == 4 && p[0] == "vacancies" && p[2] == "review" {
		if !decodeDashboardBody(w, r, &body) {
			return
		}
	} else {
		if !decodeDashboardBody(w, r, &struct{}{}) {
			return
		}
	}
	var result any = map[string]bool{"ok": true}
	var err error
	switch p[0] {
	case "vacancies":
		if len(p) != 4 || p[2] != "review" {
			dashboardError(w, 400, "Invalid vacancy review action")
			return
		}
		result, err = s.recordVacancyReview(r.Context(), p[1], p[3], body.ReviewReason)
	case "notifications":
		if s.Notifications == nil {
			err = errors.New("notifications are not configured")
			break
		}
		notification, notificationErr := s.notificationByID(p[1])
		if notificationErr != nil {
			err = notificationErr
			break
		}
		if p[2] == "open" {
			err = s.recordNotificationFeedback(notification, "opened")
		} else if p[2] == "irrelevant" {
			err = s.Notifications.Dismiss(p[1])
			if err == nil {
				err = s.recordNotificationFeedback(notification, "marked_irrelevant")
			}
		} else if p[2] == "snooze" {
			until, parseErr := time.Parse(time.RFC3339, body.Until)
			if parseErr != nil || !until.After(time.Now().UTC()) {
				dashboardError(w, 400, "A future RFC3339 until is required")
				return
			}
			err = s.Notifications.Snooze(p[1], until)
		} else if p[2] == "seen" {
			err = s.Notifications.MarkSeen(p[1])
		} else if p[2] == "resolve" {
			err = s.Notifications.Resolve(p[1])
		} else {
			err = s.Notifications.Dismiss(p[1])
		}
		if err == nil && p[2] != "open" && p[2] != "irrelevant" {
			event := map[string]string{"ack": "dismissed", "dismiss": "dismissed", "seen": "seen", "resolve": "resolved", "snooze": "snoozed"}[p[2]]
			if event != "" {
				err = s.recordNotificationFeedback(notification, event)
			}
		}
	case "follow-ups":
		if p[2] == "dismiss" {
			err = s.dismissFollowUp(p[1])
		} else {
			if s.WriteGateway != nil {
				if application, e := s.Applications.GetApplication(p[1]); e == nil && application.ConversationID != "" {
					_ = s.WriteGateway.InvalidateConversationApprovals(application.ConversationID)
				}
			}
			result, err = s.Orchestrator.PrepareFollowUp(p[1], s.FollowUpPolicy, time.Now())
			if err == nil {
				if a, e := s.Applications.GetApplication(p[1]); e == nil {
					s.decisions[a.ConversationID] = result.(AIResponseDecision)
				}
			}
			if err == nil {
				err = s.Drafts.Save()
			}
			if err == nil {
				err = s.Clarifications.Save()
			}
		}
	case "conversations":
		if p[2] == "classification-feedback" {
			if body.Correct == nil {
				dashboardError(w, 400, "correct is required")
				return
			}
			corrected := body.State
			if corrected == "" {
				corrected = body.CorrectedState
			}
			err = s.recordClassificationFeedback(p[1], *body.Correct, corrected)
			break
		}
		if p[2] == "irrelevant" {
			err = s.markConversationIrrelevant(p[1])
			break
		}
		if _, e := s.Conversations.GetConversation(p[1]); e != nil {
			dashboardError(w, 404, "Conversation not found")
			return
		}
		if s.WriteGateway != nil {
			_ = s.WriteGateway.InvalidateConversationApprovals(p[1])
		}
		result, err = s.Orchestrator.PrepareEmployerReply(p[1])
		if err == nil {
			err = s.Drafts.Save()
		}
		if err == nil {
			err = s.Clarifications.Save()
		}
		if err == nil {
			s.decisions[p[1]] = result.(AIResponseDecision)
		}
	case "drafts":
		draft, e := s.Drafts.Get(p[1])
		if e != nil {
			dashboardError(w, 404, "Draft not found")
			return
		}
		if p[2] == "feedback" {
			if body.Accepted == nil {
				dashboardError(w, 400, "accepted is required")
				return
			}
			err = s.recordDraftFeedback(draft, *body.Accepted, body.EditedText, body.Reason)
			break
		} else if p[2] == "edit" {
			if s.WriteGateway == nil {
				err = errors.New("write gateway is not configured")
				break
			}
			result, err = s.WriteGateway.EditDraft(p[1], body.Text)
			break
		}
		if p[2] == "approve" {
			if s.WriteGateway == nil {
				err = errors.New("write gateway is not configured")
				break
			}
			result, err = s.WriteGateway.ApproveDraft(p[1], body.ApprovedBy)
			break
		}
		if draft.Status != AIDraftGenerated {
			dashboardError(w, 409, "Only generated drafts can be rejected")
			return
		}
		err = s.Drafts.SetStatus(p[1], AIDraftRejected)
		if err == nil && s.QualityLog != nil {
			err = s.QualityLog.Record(QualityLogEvent{EventType: "draft_feedback", ConversationID: draft.ConversationID, EmployerMessageID: draft.InputMessageID, DraftID: draft.ID, Timestamp: time.Now().UTC(), DraftRejected: true, DecisionReasonCodes: []string{"user_feedback"}})
		}
		if err == nil {
			err = s.Drafts.Save()
		}
	case "actions":
		if p[2] == "reconcile" && s.ControlledReconciliation != nil {
			result, err = s.ControlledReconciliation.ReconcileDelivery(r.Context(), p[1])
			break
		}
		if s.WriteGateway == nil {
			err = errors.New("write gateway is not configured")
			break
		}
		if p[2] == "preflight" {
			action, actionErr := s.WriteGateway.Actions.Get(p[1])
			if actionErr != nil {
				dashboardError(w, 404, "Action not found")
				return
			}
			// Terminal actions are local-only diagnostics. In particular, do not
			// trigger a fresh HH read for a delivered/uncertain/failed action: it
			// cannot become sendable and must never be retried automatically.
			if _, terminal := terminalHHWritePreflight(action); terminal {
				result = s.WriteGateway.PreflightAction(r.Context(), p[1])
				break
			}

			result = s.WriteGateway.PreflightAction(r.Context(), p[1])
		} else if p[2] == "reconcile" {
			result, err = s.WriteGateway.ReconcileDelivery(r.Context(), p[1])
		} else if p[2] == "send" {
			if body.UIEvent == "send_ui_clicked" {
				s.Lifecycle.Record(p[1], "send_ui_clicked", "button click included in the single Send API request")
			}
			s.Lifecycle.Record(p[1], "send_api_received", "POST /api/actions/{id}/send")
			if body.Nonce == "" {
				sendResult := HHWriteResult{ActionID: p[1], Status: HHWriteApproved, Error: "MISSING_NONCE", Timestamp: time.Now().UTC()}
				result = sendResult
				s.sendFailure(w, p[1], sendResult, errors.New("missing or invalid send nonce"), "MISSING_NONCE", http.StatusConflict)
				return
			}
			action, actionErr := s.WriteGateway.Actions.Get(p[1])
			if actionErr != nil {
				sendResult := HHWriteResult{ActionID: p[1], Error: "action not found", Timestamp: time.Now().UTC()}
				result = sendResult
				s.sendFailure(w, p[1], sendResult, actionErr, "ACTION_NOT_FOUND", http.StatusNotFound)
				return
			}
			if action.SendNonce == "" || body.Nonce != action.SendNonce {
				sendResult := HHWriteResult{ActionID: p[1], Status: action.Status, Error: "MISSING_NONCE", Timestamp: time.Now().UTC()}
				result = sendResult
				s.sendFailure(w, p[1], sendResult, errors.New("missing or invalid send nonce"), "MISSING_NONCE", http.StatusConflict)
				return
			}
			sendResult, sendErr := s.WriteGateway.Send(r.Context(), p[1])
			result, err = sendResult, sendErr
			if err != nil {
				code := http.StatusInternalServerError
				errorCode := "SEND_FAILED"
				if sendResult.Status == HHWriteStale {
					code, errorCode = http.StatusConflict, "STALE_ACTION"
				}
				if sendResult.Status == HHWriteManualReview || sendResult.Status == HHWriteDeliveryUncertain {
					code, errorCode = http.StatusConflict, "MANUAL_REVIEW_REQUIRED"
				}
				if sendResult.Error == "BLOCKED_BY_DRY_RUN" {
					code, errorCode = http.StatusConflict, "BLOCKED_BY_DRY_RUN"
				} else if sendResult.Error == "BLOCKED_BY_WRITE_DISABLED" {
					code, errorCode = http.StatusConflict, "WRITE_DISABLED"
				}
				s.sendFailure(w, p[1], sendResult, err, errorCode, code)
				return
			}
		} else {
			result, err = s.WriteGateway.Cancel(p[1])
		}
	case "knowledge":
		s.decisions = map[string]AIResponseDecision{}
		knowledge := s.knowledgeSnapshot()
		switch p[1] {
		case "clarifications":
			c, e := s.Clarifications.Get(p[2])
			if e != nil {
				dashboardError(w, 404, "Clarification not found")
				return
			}
			if c.Status != ClarificationPending {
				dashboardError(w, 409, "Clarification already resolved")
				return
			}
			result, err = s.Orchestrator.ResolveCandidateClarificationAnswer(p[2], CandidateAnswer{Kind: body.AnswerKind, ChoiceID: body.ChoiceID, Raw: body.Answer})
		case "unknowns":
			var unknown *CandidateUnknown
			for _, u := range knowledge.Unknowns {
				if u.ID == p[2] {
					copy := u
					unknown = &copy
					break
				}
			}
			if unknown == nil {
				dashboardError(w, 404, "Question not found")
				return
			}
			if unknown.Status != CandidateUnknownNeedsConfirmation {
				dashboardError(w, 409, "Question already resolved")
				return
			}
			unknown.Hypothesis = body.Answer
			unknown.KnowledgeMetadata = KnowledgeMetadata{}
			if s.Mutation != nil {
				mutation, mutationErr := s.Mutation.AskUnknown(r.Context(), AskCandidateUnknownCommand{Actor: KnowledgeActorAI, Value: *unknown, Update: KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceCandidateInterview, Evidence: []string{"candidate dashboard answer"}}, Reason: "Candidate answer requires separate proposal confirmation"}})
				result = KnowledgeUpdateResult{EntityID: mutation.EntityID, ProposalID: mutation.ProposalID, QuestionID: mutation.QuestionID}
				err = mutationErr
			} else {
				result, err = s.ProposalUpdater.UpdateUnknown(*unknown, KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceCandidateInterview, Evidence: []string{"candidate dashboard answer"}}, Reason: "Candidate answer requires separate proposal confirmation"})
			}
		case "proposals":
			found := false
			for _, v := range knowledge.Proposals {
				if v.ID == p[2] {
					found = true
					if v.Status != KnowledgeProposalPending {
						dashboardError(w, 409, "Proposal already resolved")
						return
					}
					break
				}
			}
			if !found {
				dashboardError(w, 404, "Proposal not found")
				return
			}
			if p[3] == "confirm" {
				if s.CandidateLearning != nil {
					err = s.CandidateLearning.ConfirmProposal(r.Context(), p[2])
				} else if s.Mutation != nil {
					err = s.Mutation.ConfirmProposal(r.Context(), ResolveKnowledgeProposalCommand{Actor: KnowledgeActorUser, ProposalID: p[2]})
				} else {
					err = s.Updater.ConfirmKnowledge(p[2])
				}
			} else {
				if s.CandidateLearning != nil {
					err = s.CandidateLearning.RejectProposal(r.Context(), p[2])
				} else if s.Mutation != nil {
					err = s.Mutation.RejectProposal(r.Context(), ResolveKnowledgeProposalCommand{Actor: KnowledgeActorUser, ProposalID: p[2]})
				} else {
					err = s.Updater.RejectKnowledge(p[2])
				}
			}
		}
		if err != nil {
			dashboardError(w, 409, "Knowledge update refused; reload and review the proposal and answer")
			return
		}
		if s.Mutation == nil {
			err = s.Knowledge.Save()
		}
		if err == nil {
			err = s.Clarifications.Save()
		}
		if err == nil && s.Mutation != nil {
			s.refreshCandidateResolver()
		}
	}
	if err != nil {
		dashboardError(w, 500, "Operation failed; check local configuration and saved state")
		return
	}
	dashboardJSON(w, 200, result)
}

func (s *DashboardServer) sendFailure(w http.ResponseWriter, actionID string, result HHWriteResult, err error, code string, status int) {
	reason := result.Error
	if reason == "" && err != nil {
		reason = safeWriteError(err)
	}
	if reason == "" {
		reason = "unknown send failure"
	}
	s.Lifecycle.Record(actionID, "send_failed", reason)
	message := "Send failed before HH request"
	if result.TransportAttempted {
		message = "Send failed after HH request"
	}
	dashboardJSON(w, status, map[string]any{
		"error":     message,
		"code":      code,
		"reason":    reason,
		"status":    result.Status,
		"result":    result,
		"action_id": actionID,
	})
}

func (s *DashboardServer) writeHealth() map[string]any {
	result := map[string]any{"capability": "DISABLED", "hh_write_enabled": false, "dry_run": true, "last_write": nil, "uncertain_deliveries": 0, "failed_writes": 0}
	if s.WriteGateway == nil {
		return result
	}
	result["hh_write_enabled"] = s.WriteGateway.Enabled
	result["dry_run"] = s.WriteGateway.DryRun
	if s.WriteGateway.WriteEnabled() {
		result["capability"] = "MANUAL_APPROVAL"
	} else if s.WriteGateway.DryRun {
		result["capability"] = "BLOCKED_BY_DRY_RUN"
	}
	if err := s.WriteGateway.Audit.Reload(); err != nil {
		// Health must not silently present an old in-memory projection when the
		// durable audit cannot be read.
		result["audit_error"] = safeWriteError(err)
	}
	_ = s.WriteGateway.Actions.Reload()
	events := s.WriteGateway.Audit.List()
	metrics := BuildHHWriteMetrics(events)
	status := BuildHHWriteStatusReportWithConversations(Config{HHWriteEnabled: s.WriteGateway.Enabled, DryRun: s.WriteGateway.DryRun, HHMaxWritesPerRun: s.WriteGateway.MaxWritesPerRun, HHMaxWritesPerDay: s.WriteGateway.MaxWritesPerDay}, s.WriteGateway.Actions, s.WriteGateway.Audit, s.WriteGateway.Conversations)
	result["metrics"] = metrics
	result["limits"] = map[string]int{"per_run": s.WriteGateway.MaxWritesPerRun, "per_day": s.WriteGateway.MaxWritesPerDay}
	result["pending_approved_actions"] = status.PendingApproved
	result["pending_approved_action_details"] = status.PendingDetails
	result["excluded_approved_actions"] = status.ExcludedApproved
	var last *HHWriteEvent
	for _, event := range events {
		copy := event
		if last == nil || copy.CreatedAt.After(last.CreatedAt) {
			last = &copy
		}
		if event.Result == string(HHWriteDeliveryUncertain) {
			result["uncertain_deliveries"] = result["uncertain_deliveries"].(int) + 1
		}
		if event.Result == string(HHWriteFailed) {
			result["failed_writes"] = result["failed_writes"].(int) + 1
		}
	}
	result["last_write"] = last
	result["last_successful_write"] = metrics.LastSuccessful
	result["last_failed_write"] = metrics.LastFailed
	result["first_pilot"] = BuildHHFirstPilotSummary(events)
	result["send_lifecycle"] = s.Lifecycle.List("")
	return result
}

// healthFast is intentionally a projection of already-loaded local state. It
// does not run eligibility or consistency analysis; those belong to /health/deep.
func (s *DashboardServer) healthFast() map[string]any {
	vacancies, _ := s.Vacancies.List()
	applications, _ := s.Applications.ListApplications()
	conversations, _ := s.Conversations.ListConversations()
	result := map[string]any{
		"generated_at": time.Now().UTC(),
		"mode":         "fast",
		"process":      "running",
		"counts": map[string]int{
			"vacancies":     len(vacancies),
			"applications":  len(applications),
			"conversations": len(conversations),
		},
		"sync":       s.Sync.SyncState(),
		"sync_state": s.syncStatus(),
	}
	result["write_gateway"] = map[string]any{"capability": "DISABLED", "hh_write_enabled": false, "dry_run": true}
	if s.WriteGateway != nil {
		capability := "BLOCKED_BY_DRY_RUN"
		if s.WriteGateway.WriteEnabled() {
			capability = "MANUAL_APPROVAL"
		}
		result["write_gateway"] = map[string]any{"capability": capability, "hh_write_enabled": s.WriteGateway.Enabled, "dry_run": s.WriteGateway.DryRun}
	}
	return result
}

func (s *DashboardServer) refreshNotifications(now time.Time) error {
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	if s.Notifications == nil {
		return nil
	}
	engine := NewCandidateNotificationEngine(s.Notifications, s.Applications, s.Conversations, s.Clarifications, s.FollowUpPolicy, 15*time.Minute)
	beforeIDs := map[string]bool{}
	for _, notification := range s.Notifications.List() {
		beforeIDs[notification.ID] = true
	}
	created := map[string]bool{}
	before, _ := json.Marshal(s.Notifications.notifications)
	snapshotStart := time.Now()
	snapshot := s.loadNotificationSnapshot()
	perfRecord("dashboard.notifications.snapshot", snapshotStart, len(snapshot.Conversations))
	calculationStart := time.Now()
	if _, err := engine.Calculate(snapshot, now); err != nil {
		return err
	}
	perfRecord("dashboard.notifications.calculate", calculationStart, len(s.Notifications.notifications))
	s.lastNotificationStats = DailyNotificationStats{Created: engine.LastCreated, Resolved: engine.LastResolved, Deduplicated: engine.LastDeduplicated}
	if s.WriteGateway != nil && s.WriteGateway.Audit != nil {
		_ = s.WriteGateway.Audit.Reload()
		for _, event := range s.WriteGateway.Audit.List() {
			if event.Result != string(HHWriteDeliveryUncertain) && event.Result != string(HHWriteSentUnconfirmed) {
				continue
			}
			fingerprint := "delivery-uncertain/" + event.ActionID + "/" + event.Result
			if _, exists := s.Notifications.FindFingerprint(fingerprint); exists {
				continue
			}
			id, idErr := newKnowledgeID("notification")
			if idErr != nil {
				continue
			}
			s.Notifications.notifications = append(s.Notifications.notifications, CandidateNotification{ID: id, Type: NotificationDeliveryUncertain, Priority: NotificationPriorityCritical, Lifecycle: NotificationNew, RelatedApplicationID: event.ApplicationID, RelatedConversationID: event.ConversationID, Fingerprint: fingerprint, Message: "Доставка последнего сообщения не подтверждена; повторная отправка запрещена до reconciliation.", CreatedAt: now})
		}
	}

	after, _ := json.Marshal(s.Notifications.notifications)
	if s.QualityLog != nil {
		for _, notification := range s.Notifications.List() {
			if beforeIDs[notification.ID] {
				continue
			}
			created[notification.RelatedConversationID] = true
			_ = s.QualityLog.Record(QualityLogEvent{ObservationKey: "notification_created/" + notification.ID, EventType: "notification_created", ConversationID: notification.RelatedConversationID, NotificationID: notification.ID, Timestamp: notification.CreatedAt, NotificationCreated: true, DecisionReasonCodes: []string{"notification_created"}})
		}
	}
	if string(before) == string(after) {
		s.notificationMetaMu.Lock()
		s.lastNotificationCreated = created
		s.notificationMetaMu.Unlock()
		return nil
	}
	s.notificationMetaMu.Lock()
	s.lastNotificationCreated = created
	s.notificationMetaMu.Unlock()
	if err := s.Notifications.Save(); err != nil {
		return err
	}
	return nil
}

func (s *DashboardServer) activeNotifications() map[string]any {
	return s.activeNotificationsWithLimit(0)
}

func (s *DashboardServer) activeNotificationsOverview() map[string]any {
	return s.activeNotificationsWithLimit(notificationOverviewLimit)
}

func appendNotificationCreationMap(value map[string]bool) map[string]bool {
	result := make(map[string]bool, len(value))
	for key, created := range value {
		result[key] = created
	}
	return result
}

type notificationOverviewItem struct {
	ID                      string                    `json:"id"`
	Type                    CandidateNotificationType `json:"type"`
	RelatedConversationID   string                    `json:"related_conversation_id,omitempty"`
	RelatedAttemptID        string                    `json:"related_attempt_id,omitempty"`
	RelatedVacancyID        int                       `json:"related_vacancy_id,omitempty"`
	RelatedTriggerMessageID string                    `json:"related_trigger_message_id,omitempty"`
	RelatedActionType       string                    `json:"related_action_type,omitempty"`
	DetailPath              string                    `json:"detail_path,omitempty"`
	Message                 string                    `json:"message"`
	CreatedAt               time.Time                 `json:"created_at"`
	Priority                NotificationPriority      `json:"priority"`
	Lifecycle               NotificationLifecycle     `json:"lifecycle"`
}

func projectNotificationOverview(value CandidateNotification) notificationOverviewItem {
	return notificationOverviewItem{
		ID: value.ID, Type: value.Type, RelatedConversationID: value.RelatedConversationID,
		RelatedAttemptID: value.RelatedAttemptID, RelatedVacancyID: value.RelatedVacancyID,
		RelatedTriggerMessageID: value.RelatedTriggerMessageID, RelatedActionType: value.RelatedActionType,
		DetailPath: value.DetailPath, Message: value.Message, CreatedAt: value.CreatedAt,
		Priority: value.Priority, Lifecycle: value.Lifecycle,
	}
}

func (s *DashboardServer) activeNotificationsWithLimit(limit int) map[string]any {
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	if s.Notifications == nil {
		return map[string]any{"notifications": []CandidateNotification{}, "unread": 0}
	}
	now := time.Now().UTC()
	active := []CandidateNotification{}
	shownEvents := []QualityLogEvent{}
	unread := 0
	for _, notification := range s.Notifications.List() {
		lifecycle := notification.Lifecycle
		if lifecycle == "" {
			lifecycle = NotificationNew
		}
		if lifecycle == NotificationSnoozed && (notification.SnoozedUntil == nil || !notification.SnoozedUntil.After(now)) {
			lifecycle = NotificationNew
		}
		if lifecycle == NotificationDismissed || lifecycle == NotificationResolved || notification.AcknowledgedAt != nil || lifecycle == NotificationSnoozed {
			continue
		}
		shownEvents = append(shownEvents, notificationFeedbackEvent(notification, "shown"))
		active = append(active, notification)
		if lifecycle == NotificationNew {
			unread++
		}
	}
	if s.QualityLog != nil {
		feedbackStart := time.Now()
		_ = s.QualityLog.RecordMany(shownEvents)
		perfRecord("dashboard.notifications.feedback", feedbackStart, len(shownEvents))
	}
	sort.SliceStable(active, func(i, j int) bool {
		priority := map[NotificationPriority]int{NotificationPriorityCritical: 0, NotificationPriorityHigh: 1, NotificationPriorityMedium: 2, NotificationPriorityLow: 3}
		if priority[active[i].Priority] != priority[active[j].Priority] {
			return priority[active[i].Priority] < priority[active[j].Priority]
		}
		return active[i].CreatedAt.After(active[j].CreatedAt)
	})
	if limit <= 0 {
		return map[string]any{"notifications": active, "unread": unread}
	}
	if len(active) > limit {
		active = active[:limit]
	}
	projected := make([]notificationOverviewItem, 0, len(active))
	for _, notification := range active {
		projected = append(projected, projectNotificationOverview(notification))
	}
	return map[string]any{"notifications": projected, "unread": unread}
}

func (s *DashboardServer) notificationByID(id string) (CandidateNotification, error) {
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	if s.Notifications == nil {
		return CandidateNotification{}, errors.New("notifications are not configured")
	}
	for _, notification := range s.Notifications.List() {
		if notification.ID == id {
			return notification, nil
		}
	}
	return CandidateNotification{}, errors.New("notification not found")
}

func workflowQualityReasonCodes(workflow CareerWorkflowProjection) []string {
	codes := []string{"state:" + string(workflow.State)}
	if workflow.ExternalAction != nil {
		codes = append(codes, "external_action")
	}
	if workflow.FollowUp != nil && workflow.FollowUp.Eligible {
		codes = append(codes, "follow_up_eligible")
	}
	if len(workflow.Diagnostics) > 0 {
		codes = append(codes, "diagnostics_present")
	}
	return codes
}

func qualityContextStatus(item CandidateInboxItem) string {
	if len(item.PendingClarifications) > 0 || item.Workflow.State == WorkflowNeedsClarification {
		return "needs_clarification"
	}
	if len(item.Warnings) > 0 {
		return "review_required"
	}
	return "ready"
}

func latestEmployerID(c EmployerConversation) string {
	if latest := latestDeliveredMessage(c); latest != nil && latest.Sender == ConversationSenderEmployer {
		return latest.ID
	}
	return ""
}

func (s *DashboardServer) recordQualityObservations(inbox CandidateInbox, now time.Time) {
	if s == nil || s.QualityLog == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	events := []QualityLogEvent{}
	s.notificationMetaMu.RLock()
	notificationCreated := appendNotificationCreationMap(s.lastNotificationCreated)
	s.notificationMetaMu.RUnlock()
	draftByConversation := map[string]bool{}
	if drafts, err := s.Drafts.List(); err == nil {
		for _, draft := range drafts {
			if draft.ConversationID != "" && draft.Status == AIDraftGenerated {
				draftByConversation[draft.ConversationID] = true
			}
		}
	}
	for _, item := range inbox.Items {
		latest := latestDeliveredMessage(item.Conversation)
		policy := ConversationReplyRequirement("")
		if latest != nil && latest.Sender == ConversationSenderEmployer {
			policy = conversationReplyRequirement(item.Conversation, latest, classifyEmployerMessage(latest.Text))
		}
		key := "classification/" + item.Conversation.ID + "/" + item.Workflow.LastEmployerMessageHash + "/" + string(item.Workflow.State)
		events = append(events, QualityLogEvent{ObservationKey: key, EventType: "classification", ConversationID: item.Conversation.ID, EmployerMessageID: latestEmployerID(item.Conversation), Timestamp: now, WorkflowState: item.Workflow.State, ReplyPolicy: policy, CandidateContextStatus: qualityContextStatus(item), DraftGenerated: draftByConversation[item.Conversation.ID] || len(item.AIDrafts) > 0, NotificationCreated: notificationCreated[item.Conversation.ID], FollowUpEligible: item.Workflow.FollowUp != nil && item.Workflow.FollowUp.Eligible, DecisionReasonCodes: workflowQualityReasonCodes(item.Workflow)})
	}
	if drafts, err := s.Drafts.List(); err == nil {
		for _, draft := range drafts {
			if draft.ConversationID == "" {
				continue
			}
			events = append(events, QualityLogEvent{ObservationKey: "draft_generated/" + draft.ID, EventType: "draft_generated", ConversationID: draft.ConversationID, DraftID: draft.ID, EmployerMessageID: draft.InputMessageID, Timestamp: draft.CreatedAt, WorkflowState: WorkflowNeedsReply, DraftGenerated: true, DecisionReasonCodes: []string{"draft_generated"}})
		}
	}
	_ = s.QualityLog.RecordMany(events)
}

func (s *DashboardServer) recordNotificationFeedback(notification CandidateNotification, action string) error {
	if s == nil || s.QualityLog == nil {
		return nil
	}
	return s.QualityLog.Record(notificationFeedbackEvent(notification, action))
}

func notificationFeedbackEvent(notification CandidateNotification, action string) QualityLogEvent {
	return QualityLogEvent{ObservationKey: "notification/" + action + "/" + notification.ID, EventType: "notification_feedback", ConversationID: notification.RelatedConversationID, NotificationID: notification.ID, Timestamp: time.Now().UTC(), NotificationEvent: action, DecisionReasonCodes: []string{"user_feedback"}}
}

func (s *DashboardServer) recordClassificationFeedback(id string, correct bool, corrected string) error {
	c, err := s.Conversations.GetConversation(id)
	if err != nil {
		return err
	}
	corrected = strings.TrimSpace(corrected)
	valid := map[string]bool{"NEEDS_REPLY": true, "NEEDS_USER_ACTION": true, "WAITING_FOR_EMPLOYER": true, "INTERVIEW": true, "EXTERNAL_ACTION": true, "NO_REPLY_NEEDED": true, "TERMINAL": true, "needs_reply": true, "needs_action": true, "waiting": true, "interview": true, "external_action": true, "no_reply_needed": true, "terminal": true, "other": true}
	if !correct && !valid[corrected] {
		return errors.New("invalid corrected workflow state")
	}
	workflow := classifyCareerWorkflow(JobApplication{}, c, nil, time.Now().UTC(), nil, nil)
	latest := latestDeliveredMessage(c)
	policy := ConversationReplyRequirement("")
	if latest != nil && latest.Sender == ConversationSenderEmployer {
		policy = conversationReplyRequirement(c, latest, classifyEmployerMessage(latest.Text))
	}
	followUpEligible := false
	for _, candidate := range s.careerSnapshotLocal().FollowUps(s.FollowUpPolicy, time.Now().UTC()) {
		if candidate.ConversationID == id && candidate.Status == FollowUpEligible {
			followUpEligible = true
			break
		}
	}
	return s.QualityLog.Record(QualityLogEvent{EventType: "classification", ConversationID: id, EmployerMessageID: latestEmployerID(c), Timestamp: time.Now().UTC(), WorkflowState: workflow.State, ReplyPolicy: policy, CandidateContextStatus: "feedback_only", ClassificationCorrect: &correct, CorrectedState: corrected, FollowUpEligible: followUpEligible, DecisionReasonCodes: []string{"user_feedback"}})
}

var draftQualityReasons = map[string]bool{"слишком формально": true, "слишком длинно": true, "звучит как AI": true, "неверный факт": true, "пропущен факт": true, "не понял вопрос": true, "плохая формулировка": true, "другое": true}

func (s *DashboardServer) recordDraftFeedback(draft AIDraft, accepted bool, edited, reason string) error {
	reason = strings.TrimSpace(reason)
	if !accepted {
		if !draftQualityReasons[reason] {
			return errors.New("invalid draft reason category")
		}
		if strings.TrimSpace(edited) == "" || len([]rune(edited)) > 600 || profileContainsSecret([]byte(edited)) {
			return errors.New("edited draft is empty, too long or contains forbidden data")
		}
	}
	original := draft.OriginalText
	if original == "" {
		original = draft.Text
	}
	if profileContainsSecret([]byte(original)) {
		return errors.New("draft contains forbidden data")
	}
	event := QualityLogEvent{EventType: "draft_feedback", ConversationID: draft.ConversationID, EmployerMessageID: draft.InputMessageID, DraftID: draft.ID, Timestamp: time.Now().UTC(), DraftAcceptedUnchanged: &accepted, DecisionReasonCodes: []string{"user_feedback"}}
	if !accepted {
		event.DraftEdited, event.OriginalDraft, event.EditedDraft, event.DraftReasonCategory = true, original, edited, reason
	}
	return s.QualityLog.Record(event)
}

func (s *DashboardServer) markConversationIrrelevant(id string) error {
	c, err := s.Conversations.GetConversation(id)
	if err != nil {
		return err
	}
	workflow := classifyCareerWorkflow(JobApplication{}, c, nil, time.Now().UTC(), nil, nil)
	key := latestEmployerMessageHash(c)
	if key == "" {
		key = "state:" + string(workflow.State)
	}
	s.DailyRefreshState.normalize()
	s.DailyRefreshState.IrrelevantItems[id] = key
	if err := saveDailyRefreshState(s.dailyRefreshStateFile(), s.DailyRefreshState); err != nil {
		return err
	}
	for _, notification := range s.Notifications.List() {
		if notification.RelatedConversationID == id && notification.Lifecycle != NotificationResolved && notification.Lifecycle != NotificationDismissed {
			_ = s.Notifications.Dismiss(notification.ID)
			_ = s.recordNotificationFeedback(notification, "marked_irrelevant")
		}
	}
	if s.QualityLog != nil {
		return s.QualityLog.Record(QualityLogEvent{EventType: "notification_feedback", ConversationID: id, Timestamp: time.Now().UTC(), NotificationEvent: "marked_irrelevant", DecisionReasonCodes: []string{"user_feedback"}})
	}
	return nil
}

// Sorting here is presentation only; the authoritative inbox classification is
// provided by HHReadSyncService. Add waiting conversations for a complete inbox.
func (s *DashboardServer) inbox() (CandidateInbox, error) {
	inbox, err := s.Sync.GetCandidateInbox()
	if err != nil {
		return inbox, err
	}
	all, err := s.Conversations.ListConversations()
	if err != nil {
		return inbox, err
	}
	seen := map[string]bool{}
	for _, item := range inbox.Items {
		seen[item.Conversation.ID] = true
	}
	for _, c := range all {
		if seen[c.ID] {
			continue
		}
		item := CandidateInboxItem{Conversation: c}
		if len(c.Messages) > 0 {
			m := c.Messages[len(c.Messages)-1]
			item.LatestMessage = &m
		}
		inbox.Items = append(inbox.Items, item)
	}
	// Inbox is a first-paint projection. Do not build all conversation context
	// consistency diagnostics here; the conversation page and deep audit own
	// those checks. Resolver state and persisted warnings remain authoritative.
	snapshot := s.careerSnapshotLocal()
	applications := map[string]JobApplication{}
	for _, application := range snapshot.Applications {
		if application.ConversationID != "" {
			applications[application.ConversationID] = application
		}
	}
	followUps := map[string]FollowUpCandidate{}
	for _, followUp := range snapshot.FollowUps(s.FollowUpPolicy, time.Now().UTC()) {
		followUps[followUp.ConversationID] = followUp
	}
	for i := range inbox.Items {
		application := applications[inbox.Items[i].Conversation.ID]
		state := snapshot.ResolveConversation(inbox.Items[i].Conversation, time.Now())
		inbox.Items[i].Conversation.Status = state.Status
		inbox.Items[i].Conversation.WaitingSince = state.WaitingSince
		inbox.Items[i].Warnings = uniqueStrings(append(inbox.Items[i].Warnings, state.Warnings...))
		inbox.Items[i].LatestMessage = nil
		timeline := deliveredMessages(inbox.Items[i].Conversation.Messages)
		if len(timeline) > 0 {
			latest := timeline[len(timeline)-1]
			inbox.Items[i].LatestMessage = &latest
		}
		followUp, hasFollowUp := followUps[inbox.Items[i].Conversation.ID]
		if !hasFollowUp {
			followUp = FollowUpCandidate{ConversationID: inbox.Items[i].Conversation.ID, Status: FollowUpNotEligible}
		}
		inbox.Items[i].Workflow = classifyCareerWorkflow(application, inbox.Items[i].Conversation, inbox.Items[i].PendingClarifications, time.Now().UTC(), &followUp, s.Resolver)
		inbox.Items[i].Bucket = communicationInboxBucket(inbox.Items[i])
	}
	sortWorkflowInbox(inbox.Items)
	inbox.Sections = buildInboxSections(inbox.Items)
	inbox.Buckets = buildInboxBuckets(inbox.Items)
	inbox.ImportantCount = 0
	for _, item := range inbox.Items {
		if item.Workflow.State != WorkflowNoReplyNeeded && item.Workflow.State != WorkflowTerminal {
			inbox.ImportantCount++
		}
	}
	inbox.FollowUps = []FollowUpCandidate{}
	for _, f := range snapshot.FollowUps(s.FollowUpPolicy, time.Now().UTC()) {
		if f.Status == FollowUpEligible {
			inbox.FollowUps = append(inbox.FollowUps, f)
		}
	}
	s.scheduleInboxDrafts(inbox.Items)
	s.recordQualityObservations(inbox, time.Now().UTC())
	return inbox, nil
}
func inboxPriority(i CandidateInboxItem) int {
	if i.LatestMessage != nil && i.LatestMessage.Sender == ConversationSenderEmployer && outstandingEmployerMessage(i.Conversation) {
		return 0
	}
	if i.Conversation.Status == ConversationCandidateActionRequired {
		return 1
	}
	if len(i.PendingClarifications) > 0 {
		return 2
	}
	if len(i.Warnings) > 0 {
		return 3
	}
	if len(i.AIDrafts) > 0 {
		return 4
	}
	if i.Conversation.Status == ConversationWaitingEmployer {
		return 5
	}
	return 6
}
