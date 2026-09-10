package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	applicationattempt "hh-ai-responder/internal/applicationattempt"
	autochatattempt "hh-ai-responder/internal/autochatattempt"
	autochatreconciliation "hh-ai-responder/internal/usecase/autochatreconciliation"
)

func dashboardTestServer(t *testing.T) *DashboardServer {
	t.Helper()
	s, err := loadDashboard(context.Background(), t.TempDir(), Config{AIBaseURL: "http://127.0.0.1:1", AIModel: "fixture", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.Sync.client = &syncFakeReadClient{}
	return s
}
func dashboardRequest(s *DashboardServer, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Career-Agent", "local")
		r.Header.Set("Origin", "http://127.0.0.1:8080")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func dashboardDecode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var result T
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func dashboardFixture(t *testing.T, s *DashboardServer) (JobApplication, EmployerConversation) {
	t.Helper()
	v, err := s.Vacancies.Create(Vacancy{ID: 42, Title: "Python developer", Company: Company{Name: "Fixture Company"}, Description: "Python integration", Requirements: []string{"Python"}, Salary: "100000", Location: "Test city", WorkFormat: "remote", Source: "hh", MatchResult: &MatchResult{Score: 78, Recommendation: &ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "Needs review"}}})
	requireKnowledgeOK(t, err)
	c, err := s.Conversations.UpsertConversation(EmployerConversation{VacancyID: v.ID, HHConversationID: "123", RawStatus: "RESPONSE", CompanyName: v.Company.Name, VacancyTitle: v.Title, Status: ConversationCandidateActionRequired})
	requireKnowledgeOK(t, err)
	conversationTestAppend(t, s.Conversations, c.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть опыт Django?", 1))
	a, err := s.Applications.CreateApplication(JobApplication{VacancyID: v.ID, VacancyTitle: v.Title, CompanyName: v.Company.Name})
	requireKnowledgeOK(t, err)
	requireKnowledgeOK(t, s.Applications.UpdateStatus(a.ID, ApplicationApplied))
	requireKnowledgeOK(t, s.Applications.AttachConversation(a.ID, c.ID))
	a, err = s.Applications.GetApplication(a.ID)
	requireKnowledgeOK(t, err)
	return a, c
}

func TestDashboardActionSelectionUsesLifecycleBeforeTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	failed := DashboardAction{ApprovedHHAction: ApprovedHHAction{ID: "failed", Status: HHWriteFailed, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(time.Hour)}}
	approved := DashboardAction{ApprovedHHAction: ApprovedHHAction{ID: "approved", Status: HHWriteApproved, SendNonce: "fresh-nonce"}, LifecycleStatus: HHWriteApproved, Safety: "READY_TO_SEND", RequestValidation: "VALID"}
	approved.CreatedAt = now
	approved.UpdatedAt = now
	current, history, reason := selectDashboardActions([]DashboardAction{failed, approved})
	if current == nil || current.ID != "approved" {
		t.Fatalf("lifecycle selection chose the wrong current action: current=%+v reason=%s", current, reason)
	}
	if len(history) != 1 || history[0].ID != "failed" {
		t.Fatalf("historical failed action was not separated: %+v", history)
	}
	if !strings.Contains(reason, "lifecycle priority") {
		t.Fatalf("selection reason does not explain lifecycle choice: %q", reason)
	}
}

func TestDashboardActionSelectionDoesNotPromoteHistoricalFailure(t *testing.T) {
	action := DashboardAction{ApprovedHHAction: ApprovedHHAction{ID: "failed", Status: HHWriteFailed}}
	current, history, _ := selectDashboardActions([]DashboardAction{action})
	if current != nil || len(history) != 1 || history[0].ID != "failed" {
		t.Fatalf("failed action must remain history when no current action exists: current=%+v history=%+v", current, history)
	}
}

func TestDashboardDefaultBindAndOptions(t *testing.T) {
	o, err := parseDashboardOptions(nil, func(string) string { return "" }, io.Discard)
	requireKnowledgeOK(t, err)
	if o.Host != "127.0.0.1" || o.Port != 8080 {
		t.Fatalf("unsafe defaults %+v", o)
	}
	env := func(k string) string {
		if k == "HH_WEB_PORT" {
			return "invalid"
		}
		if k == "HH_WEB_HOST" {
			return "0.0.0.0"
		}
		return ""
	}
	o, err = parseDashboardOptions([]string{"--host", "localhost", "--port", "9090"}, env, io.Discard)
	requireKnowledgeOK(t, err)
	if o.Host != "127.0.0.1" || o.Port != 9090 {
		t.Fatal("CLI did not override environment")
	}
	for _, args := range [][]string{{"--host", "0.0.0.0"}, {"--host", "evil.example"}, {"--port", "0"}, {"--port", "65536"}, {"--port", "invalid"}, {"extra"}, {"--unknown"}} {
		if _, err := parseDashboardOptions(args, func(string) string { return "" }, io.Discard); err == nil {
			t.Fatalf("accepted invalid options %v", args)
		}
	}
	if _, err := parseDashboardOptions(nil, env, io.Discard); err == nil {
		t.Fatal("invalid env accepted")
	}
}
func TestDashboardServerStartsAndEmptyStores(t *testing.T) {
	s := dashboardTestServer(t)
	server := httptest.NewServer(s)
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/dashboard")
	requireKnowledgeOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("server status %d", response.StatusCode)
	}
	for _, path := range []string{"/", "/today", "/inbox", "/applications", "/applications/missing", "/vacancies", "/knowledge", "/knowledge/questions", "/analytics", "/sync", "/app.js", "/styles.css", "/api/dashboard", "/api/applications", "/api/vacancies", "/api/conversations", "/api/inbox", "/api/today", "/api/knowledge", "/api/knowledge/questions", "/api/pilot-candidates", "/api/analytics?period=today", "/api/sync/status"} {
		w := dashboardRequest(s, "GET", path, "")
		if w.Code != 200 {
			t.Errorf("empty %s -> %d: %s", path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("private response is cacheable: %s", path)
		}
	}
	var stats struct {
		Metrics DashboardMetrics `json:"metrics"`
	}
	stats = dashboardDecode[struct {
		Metrics DashboardMetrics `json:"metrics"`
	}](t, dashboardRequest(s, "GET", "/api/dashboard", ""))
	if stats.Metrics != (DashboardMetrics{}) {
		t.Fatalf("non-empty stats %+v", stats.Metrics)
	}
}
func TestDashboardReadAPIsAndFilters(t *testing.T) {
	s := dashboardTestServer(t)
	a, c := dashboardFixture(t, s)
	rows := dashboardDecode[[]DashboardApplication](t, dashboardRequest(s, "GET", "/api/applications?search=Python&company=Fixture&recommendation=maybe&status=candidate_action_required&sort=match_score", ""))
	if len(rows) != 1 || rows[0].ID != a.ID || rows[0].AppliedAt == nil || rows[0].MatchResult.Score != 78 || rows[0].LastContact == nil {
		t.Fatalf("bad list %+v", rows)
	}
	if got := dashboardDecode[[]DashboardApplication](t, dashboardRequest(s, "GET", "/api/applications?waiting_employer=true", "")); len(got) != 0 {
		t.Fatal("waiting filter failed")
	}
	detail := dashboardDecode[map[string]json.RawMessage](t, dashboardRequest(s, "GET", "/api/applications/"+a.ID, ""))
	for _, key := range []string{"vacancy", "match_result", "timeline", "conversation_detail", "candidate_context", "ai"} {
		if len(detail[key]) == 0 || string(detail[key]) == "null" {
			t.Errorf("detail missing %s", key)
		}
	}
	inbox := dashboardDecode[CandidateInbox](t, dashboardRequest(s, "GET", "/api/inbox", ""))
	if len(inbox.Items) != 1 || inbox.Items[0].Conversation.ID != c.ID || inbox.Items[0].LatestMessage == nil {
		t.Fatal("inbox omitted conversation")
	}
	conversation := dashboardDecode[struct {
		Conversation EmployerConversation `json:"conversation"`
		Context      ConversationContext  `json:"context"`
	}](t, dashboardRequest(s, "GET", "/api/conversations/"+c.ID, ""))
	if len(conversation.Conversation.Messages) != 1 || conversation.Context.ConversationID != c.ID {
		t.Fatal("conversation detail missing context/history")
	}
	vacancies := dashboardDecode[[]Vacancy](t, dashboardRequest(s, "GET", "/api/vacancies?recommendation=maybe&min_score=70&max_score=90", ""))
	if len(vacancies) != 1 {
		t.Fatal("vacancy filters failed")
	}
	dashboardDecode[Vacancy](t, dashboardRequest(s, "GET", "/api/vacancies/42", ""))
	stats := dashboardDecode[struct {
		Metrics DashboardMetrics `json:"metrics"`
	}](t, dashboardRequest(s, "GET", "/api/dashboard", ""))
	m := stats.Metrics
	if m.TotalVacancies != 1 || m.AnalyzedVacancies != 1 || m.Applications != 1 || m.EmployerReplies != 1 || m.NeedMyResponse != 1 || m.NewMessages != 1 || m.ResponseRate != 100 {
		t.Fatalf("wrong aggregate %+v", m)
	}
}
func TestDashboardKnowledgeReadAndConfirmationFlow(t *testing.T) {
	s := dashboardTestServer(t)
	result, err := s.ProposalUpdater.UpdateSkill(CandidateSkillDetailed{Name: "Kubernetes", Level: SkillLevelUnknown}, pipelineTestUpdate(KnowledgeSourceCandidateInterview))
	requireKnowledgeOK(t, err)
	if result.ProposalID == "" {
		t.Fatal("missing proposal")
	}
	dashboardDecode[map[string]any](t, dashboardRequest(s, "GET", "/api/knowledge", ""))
	safe, err := s.Knowledge.GetEmployerSafeKnowledge()
	requireKnowledgeOK(t, err)
	if len(safe.Skills) != 0 {
		t.Fatal("proposal became a fact before confirmation")
	}
	dashboardDecode[map[string]any](t, dashboardRequest(s, "POST", "/api/knowledge/proposals/"+result.ProposalID+"/confirm", "{}"))
	requireKnowledgeOK(t, s.Knowledge.Load())
	safe, err = s.Knowledge.GetEmployerSafeKnowledge()
	requireKnowledgeOK(t, err)
	if len(safe.Skills) != 1 || safe.Skills[0].TruthStatus != TruthStatusConfirmed {
		t.Fatal("updater confirmation did not persist")
	}
	if w := dashboardRequest(s, "POST", "/api/knowledge/proposals/"+result.ProposalID+"/confirm", "{}"); w.Code != 409 {
		t.Fatal("repeat confirmation accepted")
	}
	clarification, err := s.Clarifications.Create(CandidateClarificationRequest{Topic: "Python", Question: "Что делали с Python?", Reason: "Need evidence", Status: ClarificationPending})
	requireKnowledgeOK(t, err)
	dashboardDecode[map[string]any](t, dashboardRequest(s, "POST", "/api/knowledge/clarifications/"+clarification.ID+"/answer", `{"answer":"Изучал на личном проекте"}`))
	requireKnowledgeOK(t, s.Knowledge.Load())
	requireKnowledgeOK(t, s.Clarifications.Load())
	got, err := s.Clarifications.Get(clarification.ID)
	requireKnowledgeOK(t, err)
	if got.Status != ClarificationAnswered || len(s.Knowledge.Unknowns) != 1 || s.Knowledge.Unknowns[0].TruthStatus == TruthStatusConfirmed {
		t.Fatal("answer bypassed clarification flow")
	}
	unknown := s.Knowledge.Unknowns[0]
	dashboardDecode[map[string]any](t, dashboardRequest(s, "POST", "/api/knowledge/unknowns/"+unknown.ID+"/answer", `{"answer":"Уточняю: это учебный проект"}`))
	if s.Knowledge.Unknowns[0].Hypothesis != "Уточняю: это учебный проект" || s.Knowledge.Unknowns[0].Status != CandidateUnknownNeedsConfirmation {
		t.Fatal("unknown answer was not staged")
	}
	second, err := s.ProposalUpdater.UpdateSkill(CandidateSkillDetailed{Name: "Docker", Level: SkillLevelUnknown}, pipelineTestUpdate(KnowledgeSourceCandidateInterview))
	requireKnowledgeOK(t, err)
	dashboardDecode[map[string]any](t, dashboardRequest(s, "POST", "/api/knowledge/proposals/"+second.ProposalID+"/reject", "{}"))
	if s.Knowledge.Proposals[len(s.Knowledge.Proposals)-1].Status != KnowledgeProposalRejected {
		t.Fatal("rejection not recorded")
	}
}
func TestDashboardInvalidRequestsAndMissingIDs(t *testing.T) {
	s := dashboardTestServer(t)
	for _, path := range []string{"/api/vacancies/999", "/api/vacancies/not-a-number", "/api/applications/missing", "/api/conversations/missing"} {
		if w := dashboardRequest(s, "GET", path, ""); w.Code != 404 {
			t.Errorf("%s -> %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/conversations/missing/draft", "/api/drafts/missing/reject", "/api/knowledge/clarifications/missing/answer", "/api/knowledge/unknowns/missing/answer", "/api/knowledge/proposals/missing/confirm"} {
		body := "{}"
		if strings.HasSuffix(path, "/answer") {
			body = `{"answer":"test"}`
		}
		if w := dashboardRequest(s, "POST", path, body); w.Code != 404 {
			t.Errorf("%s -> %d", path, w.Code)
		}
	}
	for _, body := range []string{"", "null", "[]", "{", `{"unknown":true}`, `{} {}`, `{"answer":"` + strings.Repeat("a", 17<<10) + `"}`} {
		if w := dashboardRequest(s, "POST", "/api/sync", body); w.Code != 400 {
			t.Errorf("invalid JSON -> %d", w.Code)
		}
	}
	for _, path := range []string{"/api/analytics?period=year", "/api/vacancies?min_score=-1", "/api/vacancies?min_score=90&max_score=10", "/api/vacancies?max_score=no", "/api/applications?status=invalid", "/api/applications?recommendation=auto_apply", "/api/applications?sort=invalid", "/api/applications?waiting_employer=yes"} {
		if w := dashboardRequest(s, "GET", path, ""); w.Code != 400 {
			t.Errorf("invalid filter %s -> %d", path, w.Code)
		}
	}
	if w := dashboardRequest(s, "POST", "/api/dashboard", "{}"); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("method check missing")
	}
}
func TestDashboardNoHHWriteEndpoints(t *testing.T) {
	s := dashboardTestServer(t)
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		for _, path := range []string{"/api/send", "/api/apply", "/api/reply", "/api/hh/write", "/api/conversations/123/send", "/api/conversations/123/reply", "/api/applications/123/apply", "/api/resume/touch", "/send", "/apply", "/reply", "/hh/write"} {
			if w := dashboardRequest(s, method, path, "{}"); w.Code != 404 {
				t.Errorf("HH write route exists %s %s: %d", method, path, w.Code)
			}
		}
	}
}

func dashboardApprovedAction(t *testing.T, s *DashboardServer, c EmployerConversation) ApprovedHHAction {
	t.Helper()
	draft, err := s.Drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: c.ID, Text: "Безопасный fixture-ответ", DecisionReason: "fixture"})
	requireKnowledgeOK(t, err)
	requireKnowledgeOK(t, s.Drafts.Save())
	action, err := s.WriteGateway.ApproveDraft(draft.ID, "dashboard_test")
	requireKnowledgeOK(t, err)
	now := time.Now().UTC()
	action.LastPreflightAt = &now
	action.LastPreflightOK = true
	requireKnowledgeOK(t, s.WriteGateway.Actions.put(action))
	requireKnowledgeOK(t, s.WriteGateway.Actions.Save())
	return action
}

func TestDashboardSendDryRunReachesGatewayAndNeverTransport(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	if err := s.Knowledge.AddSkill(CandidateSkillDetailed{Name: "Django", Level: SkillLevelWorking, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}); err != nil {
		t.Fatal(err)
	}
	action := dashboardApprovedAction(t, s, c)
	s.WriteGateway.ReadClient = &gatewayReadFake{state: HHConversationReadState{
		ExternalID: c.HHConversationID, LastMessageID: action.LastMessageID,
		State: string(ConversationCandidateActionRequired), ReplyRequirement: ReplyRequired,
		MessageCount: len(c.Messages) + 1,
	}}
	s.WriteGateway.DryRun = true
	s.WriteGateway.Enabled = false
	before := BuildHHWriteMetrics(s.WriteGateway.Audit.List())

	w := dashboardRequest(s, http.MethodPost, "/api/actions/"+action.ID+"/send", `{"nonce":"`+action.SendNonce+`","ui_event":"send_ui_clicked"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("dry-run Send status=%d body=%s", w.Code, w.Body.String())
	}
	var failure struct {
		Error  string        `json:"error"`
		Code   string        `json:"code"`
		Reason string        `json:"reason"`
		Result HHWriteResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Error != "Send failed before HH request" || failure.Code != "BLOCKED_BY_DRY_RUN" || failure.Reason != "BLOCKED_BY_DRY_RUN" || failure.Result.TransportAttempted {
		t.Fatalf("unsafe/opaque dry-run result: %+v", failure)
	}
	if failure.Result.WriteCapability != "BLOCKED_BY_DRY_RUN" {
		t.Fatalf("dry-run capability was not explicit: %+v", failure.Result)
	}
	var lifecycle []DashboardLifecycleEvent
	lifecycle = dashboardDecode[[]DashboardLifecycleEvent](t, dashboardRequest(s, http.MethodGet, "/api/diagnostics/lifecycle?action_id="+url.QueryEscape(action.ID), ""))
	var types []string
	for _, event := range lifecycle {
		types = append(types, event.Type)
	}
	if !reflect.DeepEqual(types, []string{"send_ui_clicked", "send_api_received", "gateway_invoked", "preflight_passed", "capability_blocked", "send_failed"}) {
		t.Fatalf("unexpected local Send lifecycle: %v", types)
	}
	after := BuildHHWriteMetrics(s.WriteGateway.Audit.List())
	if after.WriteAttemptsTotal != before.WriteAttemptsTotal || after.SuccessfulWrites != before.SuccessfulWrites || after.FailedWrites != before.FailedWrites {
		t.Fatalf("dry-run diagnostic changed HH write metrics: before=%+v after=%+v", before, after)
	}
}

func TestDashboardDryRunDoubleClickKeepsFreshNonceAndTransportMetrics(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	if err := s.Knowledge.AddSkill(CandidateSkillDetailed{Name: "Django", Level: SkillLevelWorking, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}); err != nil {
		t.Fatal(err)
	}
	action := dashboardApprovedAction(t, s, c)
	s.WriteGateway.ReadClient = &gatewayReadFake{state: HHConversationReadState{
		ExternalID: c.HHConversationID, LastMessageID: action.LastMessageID,
		State: string(ConversationCandidateActionRequired), ReplyRequirement: ReplyRequired,
		MessageCount: len(c.Messages) + 1,
	}}
	s.WriteGateway.DryRun = true
	s.WriteGateway.Enabled = false
	before := BuildHHWriteMetrics(s.WriteGateway.Audit.List())

	for attempt := 0; attempt < 2; attempt++ {
		w := dashboardRequest(s, http.MethodPost, "/api/actions/"+action.ID+"/send", `{"nonce":"`+action.SendNonce+`","ui_event":"send_ui_clicked"}`)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"BLOCKED_BY_DRY_RUN"`) || !strings.Contains(w.Body.String(), `"transport_attempted":false`) {
			t.Fatalf("double-click attempt %d was not safely blocked: %d %s", attempt+1, w.Code, w.Body.String())
		}
	}
	stored, err := s.WriteGateway.Actions.Get(action.ID)
	if err != nil || stored.NonceUsedAt != nil || stored.Status != HHWriteApproved {
		t.Fatalf("double-click consumed nonce or changed action: %+v %v", stored, err)
	}
	after := BuildHHWriteMetrics(s.WriteGateway.Audit.List())
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("double-click changed transport metrics: before=%+v after=%+v", before, after)
	}
}

func TestDashboardSendRejectsMissingNonceAsStructuredError(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	action := dashboardApprovedAction(t, s, c)
	w := dashboardRequest(s, http.MethodPost, "/api/actions/"+action.ID+"/send", `{"ui_event":"send_ui_clicked"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"MISSING_NONCE"`) || !strings.Contains(w.Body.String(), "Send failed before HH request") {
		t.Fatalf("missing nonce was hidden: %d %s", w.Code, w.Body.String())
	}
	if got := s.WriteGateway.Actions.List()[len(s.WriteGateway.Actions.List())-1].Status; got != HHWriteApproved {
		t.Fatalf("missing nonce changed action status to %s", got)
	}
}

func TestDashboardSendStaleActionAndServerErrorAreNotSwallowed(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	c, err := s.Conversations.GetConversation(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.HHConversationID = "123"
	if _, err := s.Conversations.UpsertConversation(c); err != nil {
		t.Fatal(err)
	}
	action := dashboardApprovedAction(t, s, c)
	action.Status = HHWriteStale
	requireKnowledgeOK(t, s.WriteGateway.Actions.put(action))
	requireKnowledgeOK(t, s.WriteGateway.Actions.Save())
	w := dashboardRequest(s, http.MethodPost, "/api/actions/"+action.ID+"/send", `{"nonce":"`+action.SendNonce+`"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"STALE_ACTION"`) {
		t.Fatalf("stale action was hidden: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.sendFailure(w, action.ID, HHWriteResult{ActionID: action.ID, TransportAttempted: true}, errors.New("fixture server error"), "SEND_FAILED", http.StatusInternalServerError)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Send failed after HH request") || !strings.Contains(w.Body.String(), "fixture server error") {
		t.Fatalf("server error was hidden: %d %s", w.Code, w.Body.String())
	}
}

func TestDashboardFrontendSendFlowIsSingleExplicitPOST(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("web", "app.js"))
	requireKnowledgeOK(t, err)
	source := string(raw)
	for _, required := range []string{
		`data-action="send-action"`,
		`type="button"`,
		`method: "POST"`,
		`ui_event: "send_ui_clicked"`,
		`nonce: button.dataset.nonce || ""`,
		`Send blocked before HH request`,
		`Reason: HH_DRY_RUN=true`,
		`if (busy) return;`,
		`event.preventDefault();`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("frontend Send flow missing %q", required)
		}
	}
}
func TestDashboardOriginHostAndContentProtection(t *testing.T) {
	s := dashboardTestServer(t)
	tests := []struct {
		host, origin, site, content, header string
		code                                int
	}{{"evil.example", "", "", "application/json", "local", 403}, {"127.0.0.1:8080", "https://evil.example", "", "application/json", "local", 403}, {"127.0.0.1:8080", "null", "", "application/json", "local", 403}, {"127.0.0.1:8080", "", "cross-site", "application/json", "local", 403}, {"127.0.0.1:8080", "", "", "text/plain", "local", 400}, {"127.0.0.1:8080", "", "", "application/json", "", 403}}
	for _, tt := range tests {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/sync", strings.NewReader("{}"))
		r.Host = tt.host
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Site", tt.site)
		r.Header.Set("Content-Type", tt.content)
		r.Header.Set("X-Career-Agent", tt.header)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("protection %+v: %d", tt, w.Code)
		}
	}
	w := dashboardRequest(s, "GET", "/", "")
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("CSP missing")
	}
}
func TestDashboardEmployerHTMLEscaping(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	attack := `<img src=x onerror="window.pwned=1"><script>alert('x')</script>`
	conversationTestAppend(t, s.Conversations, c.ID, conversationTestMessage("attack", ConversationSenderEmployer, attack, 2))
	w := dashboardRequest(s, "GET", "/api/conversations/"+c.ID, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "<img") || !strings.Contains(w.Body.String(), `\u003cimg`) {
		t.Fatalf("HTML not escaped %d", w.Code)
	}
	got := dashboardDecode[struct {
		Conversation EmployerConversation `json:"conversation"`
	}](t, w)
	if got.Conversation.Messages[1].Text != attack {
		t.Fatal("original employer message was corrupted")
	}
	page := dashboardRequest(s, "GET", "/conversations/"+c.ID, "")
	if strings.Contains(page.Body.String(), attack) {
		t.Fatal("untrusted message interpolated into HTML shell")
	}
}

// This fake exposes exactly HHReadClient's three read capabilities and records
// calls. It has no HH write capability for the dashboard to accidentally use.
type dashboardReadSpy struct {
	syncFakeReadClient
	vacancyCalls, applicationCalls, conversationCalls int
}

func (s *dashboardReadSpy) ReadVacancies(c context.Context, p string) (HHVacancyPage, error) {
	s.vacancyCalls++
	return s.syncFakeReadClient.ReadVacancies(c, p)
}
func (s *dashboardReadSpy) ReadApplications(c context.Context, p string) (HHApplicationPage, error) {
	s.applicationCalls++
	return s.syncFakeReadClient.ReadApplications(c, p)
}
func (s *dashboardReadSpy) ReadConversations(c context.Context, p string) (HHConversationPage, error) {
	s.conversationCalls++
	return s.syncFakeReadClient.ReadConversations(c, p)
}
func TestDashboardSyncCallsExistingService(t *testing.T) {
	s := dashboardTestServer(t)
	spy := &dashboardReadSpy{syncFakeReadClient: syncFakeReadClient{vacancies: []HHVacancyRecord{syncFixtureVacancy("Python")}}}
	s.Sync.client = spy
	result := dashboardDecode[SyncResult](t, dashboardRequest(s, "POST", "/api/sync/vacancies", "{}"))
	if result.Created != 1 || result.Fetched != 1 || spy.vacancyCalls != 1 {
		t.Fatalf("did not call sync service %+v", result)
	}
	persisted := NewVacancyStore(s.Vacancies.path)
	requireKnowledgeOK(t, persisted.Load())
	v, err := persisted.Get(42)
	requireKnowledgeOK(t, err)
	if v.MatchResult == nil {
		t.Fatal("existing analyzer wasn't used")
	}
	dashboardDecode[SyncResult](t, dashboardRequest(s, "POST", "/api/sync/applications", "{}"))
	dashboardDecode[SyncResult](t, dashboardRequest(s, "POST", "/api/sync/conversations", "{}"))
	dashboardDecode[HHSyncAllResult](t, dashboardRequest(s, "POST", "/api/sync", "{}"))
	if spy.vacancyCalls != 2 || spy.applicationCalls != 2 || spy.conversationCalls != 2 {
		t.Fatal("sync targets not dispatched")
	}
	status := dashboardDecode[struct {
		State   HHSyncState     `json:"state"`
		Running bool            `json:"running"`
		Result  HHSyncAllResult `json:"last_result"`
	}](t, dashboardRequest(s, "GET", "/api/sync/status", ""))
	if status.Running || status.State.LastSuccess.IsZero() || status.Result.Vacancies.Unchanged != 1 {
		t.Fatal("sync state/result missing")
	}
	spy.vacancyErr = errors.New("fixture failure")
	failed := dashboardDecode[SyncResult](t, dashboardRequest(s, "POST", "/api/sync/vacancies", "{}"))
	if len(failed.Errors) == 0 {
		t.Fatal("sync errors hidden")
	}
}
func TestDashboardDraftCallsOrchestratorWithoutHHSending(t *testing.T) {
	s := dashboardTestServer(t)
	a, c := dashboardFixture(t, s)
	kb := contextTestKnowledge()
	kb.ProfilePath = s.Knowledge.ProfilePath
	*s.Knowledge = *kb
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil)}
	s.Orchestrator = NewAIReplyOrchestratorWithOptions(ai, AIReplyOrchestratorOptions{
		ConversationStore: s.Conversations, Resolver: s.Resolver, Drafts: s.Drafts,
		Clarifications: s.Clarifications, Updater: s.ProposalUpdater,
	})
	spy := &dashboardReadSpy{}
	s.Sync.client = spy
	before, err := s.Conversations.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	beforeApp, err := s.Applications.GetApplication(a.ID)
	requireKnowledgeOK(t, err)
	decision := dashboardDecode[AIResponseDecision](t, dashboardRequest(s, "POST", "/api/conversations/"+c.ID+"/draft", "{}"))
	if decision.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("orchestrator not called %+v", decision)
	}
	after, err := s.Conversations.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	afterApp, err := s.Applications.GetApplication(a.ID)
	requireKnowledgeOK(t, err)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeApp, afterApp) {
		t.Fatal("draft mutated HH/application history")
	}
	if spy.vacancyCalls+spy.applicationCalls+spy.conversationCalls != 0 {
		t.Fatal("draft unexpectedly contacted HH")
	}
	requireKnowledgeOK(t, s.Drafts.Load())
	drafts, err := s.Drafts.List()
	requireKnowledgeOK(t, err)
	if len(drafts) != 1 || drafts[0].Status != AIDraftGenerated {
		t.Fatal("draft not persisted locally")
	}
	dashboardDecode[map[string]any](t, dashboardRequest(s, "POST", "/api/drafts/"+drafts[0].ID+"/reject", "{}"))
	requireKnowledgeOK(t, s.Drafts.Load())
	draft, err := s.Drafts.Get(drafts[0].ID)
	requireKnowledgeOK(t, err)
	if draft.Status != AIDraftRejected {
		t.Fatal("draft rejection not persisted")
	}
}

func TestDashboardKeepsStaleActionInLifecycleAndAllowsReapproval(t *testing.T) {
	s := dashboardTestServer(t)
	_, conversation := dashboardFixture(t, s)
	if err := s.Knowledge.AddSkill(CandidateSkillDetailed{Name: "Django", Level: SkillLevelWorking, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}); err != nil {
		t.Fatal(err)
	}
	draft, err := s.Drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: conversation.ID, Text: "Да, есть опыт работы с Django.", DecisionReason: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	action, err := s.WriteGateway.ApproveDraft(draft.ID, "dashboard_test")
	if err != nil {
		t.Fatal(err)
	}
	s.Knowledge.Skills[0].Level = SkillLevelAdvanced
	detail := dashboardDecode[struct {
		AI map[string]json.RawMessage `json:"ai"`
	}](t, dashboardRequest(s, "GET", "/api/conversations/"+conversation.ID, ""))
	var linked struct {
		Actions []DashboardAction `json:"actions"`
	}
	if err := json.Unmarshal(detail.AI["actions"], &linked.Actions); err != nil {
		t.Fatal(err)
	}
	if len(linked.Actions) != 1 || linked.Actions[0].LifecycleStatus != HHWriteStale || len(linked.Actions[0].RelevantKnowledgeDiff) == 0 {
		t.Fatalf("stale action disappeared or has no semantic diff: %+v", linked.Actions)
	}
	if w := dashboardRequest(s, "POST", "/api/drafts/"+draft.ID+"/approve", `{"approved_by":"dashboard_test"}`); w.Code != 200 {
		t.Fatalf("reapprove failed: %d %s", w.Code, w.Body.String())
	}
	newAction, err := s.WriteGateway.Actions.Get(action.ID)
	if err != nil || newAction.Status != HHWriteStale {
		t.Fatalf("old stale action was unexpectedly changed: %+v %v", newAction, err)
	}
	var approved int
	for _, value := range s.WriteGateway.Actions.List() {
		if value.Status == HHWriteApproved {
			approved++
		}
	}
	if approved != 1 {
		t.Fatalf("reapproval did not create one active approved action: %d", approved)
	}
}
func TestDashboardUnknownSkillDraftProducesClarification(t *testing.T) {
	s := dashboardTestServer(t)
	_, c := dashboardFixture(t, s)
	conversationTestAppend(t, s.Conversations, c.ID, conversationTestMessage("unknown", ConversationSenderEmployer, "Есть опыт Kubernetes?", 2))
	decision := dashboardDecode[AIResponseDecision](t, dashboardRequest(s, "POST", "/api/conversations/"+c.ID+"/draft", "{}"))
	if decision.Action != AIActionNeedCandidate {
		t.Fatalf("unknown skill not gated %+v", decision)
	}
	requireKnowledgeOK(t, s.Clarifications.Load())
	cs, err := s.Clarifications.List()
	requireKnowledgeOK(t, err)
	if len(cs) == 0 {
		t.Fatal("clarification not saved")
	}
}
func TestDashboardAnalyticsDatesCohortsAndNoInventedReplies(t *testing.T) {
	s := dashboardTestServer(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for i, status := range []ApplicationStatus{ApplicationApplied, ApplicationRejected, ApplicationInterview} {
		a, err := s.Applications.CreateApplication(JobApplication{VacancyID: i + 1, VacancyTitle: "Fixture", CompanyName: "Fixture", Status: status, CreatedAt: now.AddDate(0, 0, -40)})
		requireKnowledgeOK(t, err)
		if i == 0 {
			s.Applications.events = append(s.Applications.events, ApplicationEvent{ID: "dated-application", ApplicationID: a.ID, Timestamp: now.Add(-time.Hour), Type: ApplicationEventApplied, Description: "fixture"})
		}
	}
	result, err := s.analytics("today", now)
	requireKnowledgeOK(t, err)
	data := result.(map[string]any)
	m := data["metrics"].(DashboardMetrics)
	if m.Applications != 1 || m.EmployerReplies != 0 || m.ResponseRate != 0 || len(data["daily"].([]DashboardDay)) != 1 {
		t.Fatalf("today metrics wrong %+v", m)
	}
	all, err := s.analytics("all", now)
	requireKnowledgeOK(t, err)
	m = all.(map[string]any)["metrics"].(DashboardMetrics)
	if m.Applications != 3 || m.Rejected != 1 || m.Interviews != 1 || m.EmployerReplies != 0 {
		t.Fatalf("inferred replies from status %+v", m)
	}
	week, err := s.analytics("7d", now)
	requireKnowledgeOK(t, err)
	if len(week.(map[string]any)["daily"].([]DashboardDay)) != 7 {
		t.Fatal("7-day range is not inclusive calendar days")
	}
}
func TestDashboardConcurrentAccessAndBusyState(t *testing.T) {
	s := dashboardTestServer(t)
	var failures atomic.Int32
	var wg sync.WaitGroup
	for range 15 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := dashboardRequest(s, "GET", "/api/dashboard", "")
			if w.Code != 200 && w.Code != 409 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatal("concurrent read failed")
	}
	s.mu.Lock()
	s.running.Store(true)
	w := dashboardRequest(s, "POST", "/api/sync", "{}")
	if w.Code != 409 {
		t.Fatal("duplicate operation was queued")
	}
	status := dashboardRequest(s, "GET", "/api/sync/status", "")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"running":true`) {
		t.Fatal("sync status blocked behind long operation")
	}
	s.running.Store(false)
	s.mu.Unlock()
}
func TestDashboardStartupDoesNotReadHHOrCreatePrivateFiles(t *testing.T) {
	dir := t.TempDir()
	var requests atomic.Int32
	hh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		t.Error("startup contacted HH")
		w.WriteHeader(500)
	}))
	defer hh.Close()
	_, err := loadDashboard(context.Background(), dir, Config{SearchURL: hh.URL + "/search/vacancy", CookiesPath: filepath.Join(dir, "missing-cookies.txt"), DryRun: false, AutoApply: true, AutoChat: true, AutoTouch: true, AutoJobStatus: true})
	requireKnowledgeOK(t, err)
	if requests.Load() != 0 {
		t.Fatal("startup contacted HH")
	}
	files, err := os.ReadDir(dir)
	requireKnowledgeOK(t, err)
	if len(files) != 0 {
		t.Fatal("read-only startup created files")
	}
}

func TestDashboardReaderForcesDryRunEvenWithLiveCLIConfig(t *testing.T) {
	original := Config{DryRun: false, AutoApply: true, AutoChat: true, AutoTouch: true, AutoJobStatus: true, ChatMode: "auto", CookiesPath: "fixture-cookies.txt", CandidateProfilePath: "fixture-profile.json", OutputPath: "fixture-events.json"}
	safe := dashboardReadConfig(original)
	if !safe.DryRun || safe.AutoApply || safe.AutoChat || safe.AutoTouch || safe.AutoJobStatus || safe.ChatMode != "off" {
		t.Fatal("web inherited HH write capabilities")
	}
	if safe.CandidateProfilePath != "" || safe.OutputPath != "" {
		t.Fatal("reader constructor would write unrelated local data")
	}
	if safe.CookiesPath != original.CookiesPath || original.DryRun {
		t.Fatal("web changed CLI configuration or cookies compatibility")
	}
}

func TestDashboardReliabilityAPIIsBoundedAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	attemptPath := filepath.Join(dir, jsonstorage.ApplicationAttemptsFilename)
	store := jsonstorage.NewApplicationAttemptRepository(attemptPath)
	attempt, err := applicationattempt.New(123, "resume-1", time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	requireKnowledgeOK(t, err)
	_, err = store.Reserve(context.Background(), attempt)
	requireKnowledgeOK(t, err)
	s, err := loadDashboard(context.Background(), dir, Config{CandidateProfilePath: profile, AIBaseURL: "http://127.0.0.1:1", AIModel: "fixture", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	requireKnowledgeOK(t, err)
	response := dashboardRequest(s, http.MethodGet, "/api/reliability/application-attempts?limit=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("reliability GET status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []struct {
			AttemptID    string `json:"attempt_id"`
			State        string `json:"state"`
			DisplayLabel string `json:"display_label"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].AttemptID != attempt.AttemptID || payload.Items[0].State != string(applicationattempt.StateSending) || payload.Items[0].DisplayLabel == "Не отправлено: запрос к HH не выполнялся" {
		t.Fatalf("unexpected reliability payload: %+v", payload.Items)
	}
	if response := dashboardRequest(s, http.MethodPost, "/api/reliability/application-attempts", "{}"); response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("reliability route is not GET-only: %d allow=%q", response.Code, response.Header().Get("Allow"))
	}
	if response := dashboardRequest(s, http.MethodGet, "/api/reliability/application-attempts/"+attempt.AttemptID+"/reconcile", ""); response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("reconciliation route accepts GET: %d allow=%q", response.Code, response.Header().Get("Allow"))
	}
	if response := dashboardRequest(s, http.MethodPost, "/api/reliability/application-attempts/"+attempt.AttemptID+"/reconcile", "{}"); response.Code == http.StatusOK {
		t.Fatal("reconciliation unexpectedly completed against the fixture HH client")
	}
	for _, path := range []string{"/api/reliability/application-attempts?limit=101", "/api/reliability/application-attempts?state=UNKNOWN"} {
		if response := dashboardRequest(s, http.MethodGet, path, ""); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid reliability filter %s returned %d", path, response.Code)
		}
	}
}

func TestDashboardAutoChatReconcileResponseDoesNotExposeRequestKey(t *testing.T) {
	value, err := json.Marshal(reliabilityAutoChatReconcileResult{
		AttemptID: "auto-1", ConversationID: "conversation-1", TriggerMessageID: "trigger-1",
		ActionType: autochatattempt.ActionReply, ProviderOutgoingMessageID: "provider-1",
		PreviousState: autochatattempt.StateDeliveryUncertain, NewState: autochatattempt.StateTargetReplyConfirmed,
		Status: autochatreconciliation.StatusConfirmed, Evidence: autochatattempt.ReconciliationEvidence{Kind: autochatattempt.EvidenceExactOutgoingMessage, ProviderMessageID: "provider-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(value), "request_key") || strings.Contains(string(value), "sensitive") {
		t.Fatalf("reconciliation response contains a local request key: %s", value)
	}
}

func TestDashboardReliabilityStoreFailureIsNotEmpty(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	if err := os.WriteFile(filepath.Join(dir, jsonstorage.ApplicationAttemptsFilename), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := loadDashboard(context.Background(), dir, Config{CandidateProfilePath: profile, AIBaseURL: "http://127.0.0.1:1", AIModel: "fixture", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	requireKnowledgeOK(t, err)
	response := dashboardRequest(s, http.MethodGet, "/api/reliability/application-attempts", "")
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), "unavailable") {
		t.Fatalf("corrupt store was hidden: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardAutoChatReliabilityKeepsTriggerAndMasksRequestKey(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "candidate_profile.json")
	store := jsonstorage.NewAutoChatAttemptRepository(filepath.Join(dir, jsonstorage.AutoChatAttemptsFilename))
	value := autochatattempt.Attempt{AttemptID: "auto-1", ConversationID: "conversation-1", TriggerMessageID: "message-M1", ActionType: autochatattempt.ActionReply, State: autochatattempt.StateSending, CreatedAt: time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC), RequestKey: "sensitive-request-key-M1"}
	if _, err := store.Reserve(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(context.Background(), value.AttemptID, autochatattempt.StateDeliveryUncertain, value.CreatedAt.Add(time.Minute), "", 502, "uncertain"); err != nil {
		t.Fatal(err)
	}
	s, err := loadDashboard(context.Background(), dir, Config{CandidateProfilePath: profile, AIBaseURL: "http://127.0.0.1:1", AIModel: "fixture", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	requireKnowledgeOK(t, err)
	response := dashboardRequest(s, http.MethodGet, "/api/reliability/autochat-attempts", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), value.RequestKey) || !strings.Contains(response.Body.String(), value.TriggerMessageID) {
		t.Fatalf("unsafe or incomplete auto-chat read: status=%d body=%s", response.Code, response.Body.String())
	}
	detail := dashboardRequest(s, http.MethodGet, "/api/reliability/autochat-attempts/auto-1", "")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), value.RequestKey) || !strings.Contains(detail.Body.String(), "sens…y-M1") {
		t.Fatalf("auto-chat detail leaked or omitted correlation masking: status=%d body=%s", detail.Code, detail.Body.String())
	}
}
