package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func followUpFixture() (FollowUpInput, time.Time) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	sent := now.Add(-5 * 24 * time.Hour)
	a := JobApplication{ID: "a", VacancyID: 42, ConversationID: "c", Source: ApplicationSourceHH, RawStatus: "RESPONSE", Status: ApplicationApplied, CreatedAt: sent, UpdatedAt: sent, HHMetadata: map[string]string{"applied_at": sent.Format(time.RFC3339Nano)}}
	c := EmployerConversation{ID: "c", VacancyID: 42, HHConversationID: "hc", RawStatus: "RESPONSE", Status: ConversationWaitingEmployer, CreatedAt: sent, UpdatedAt: sent, FollowUpState: ConversationFollowUpNone, WaitingSince: &sent, Messages: []ConversationMessage{}}
	return FollowUpInput{Application: a, Conversation: c, AppliedAt: &sent}, now
}
func followUpMessage(id string, sender ConversationSender, t time.Time) ConversationMessage {
	dir := ConversationIncoming
	if sender == ConversationSenderCandidate {
		dir = ConversationOutgoing
	}
	return ConversationMessage{ID: id, ExternalID: id, Sender: sender, Direction: dir, Source: ConversationSourceHH, Text: "Fixture message", Timestamp: t}
}
func TestConversationStateResolverDirectionAndTerminal(t *testing.T) {
	in, now := followUpFixture()
	for _, sender := range []ConversationSender{ConversationSenderEmployer, ConversationSenderCandidate} {
		c := in.Conversation
		c.Messages = []ConversationMessage{followUpMessage("last", sender, now.Add(-time.Hour)), followUpMessage("earlier", ConversationSenderEmployer, now.Add(-2*time.Hour))}
		r := (ConversationStateResolver{}).Resolve(in.Application, c, in.AppliedAt, false, nil, now)
		want := ConversationCandidateActionRequired
		if sender == ConversationSenderCandidate {
			want = ConversationWaitingEmployer
		}
		if r.Status != want || r.LatestMessage.ID != "last" {
			t.Fatalf("wrong state/order: %+v", r)
		}
		if sender == ConversationSenderCandidate && (r.WaitingSince == nil || !r.WaitingSince.Equal(c.Messages[0].Timestamp)) {
			t.Fatal("wrong waiting_since")
		}
	}
	c := in.Conversation
	c.RawStatus = "DISCARD"
	a := in.Application
	a.RawStatus = "DISCARD"
	a.Status = ApplicationRejected
	c.Messages = []ConversationMessage{followUpMessage("last", ConversationSenderCandidate, now.Add(-time.Hour))}
	r := (ConversationStateResolver{}).Resolve(a, c, in.AppliedAt, false, nil, now)
	if r.Status != ConversationRejected {
		t.Fatal(r)
	}
	a.RawStatus = "OFFER"
	r = (ConversationStateResolver{}).Resolve(a, c, in.AppliedAt, false, nil, now)
	if r.Status != ConversationManualReview {
		t.Fatal("conflicting sources not reviewed")
	}
}
func TestFollowUpEligibilityRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*FollowUpInput, *FollowUpPolicy, time.Time)
		want FollowUpStatus
	}{
		{"five days", func(*FollowUpInput, *FollowUpPolicy, time.Time) {}, FollowUpEligible},
		{"one day", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) { v := n.Add(-24 * time.Hour); i.AppliedAt = &v }, FollowUpTooEarly},
		{"employer last", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.Messages = []ConversationMessage{followUpMessage("e", ConversationSenderEmployer, n.Add(-6*24*time.Hour))}
		}, FollowUpCandidateActionRequired},
		{"candidate three days", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.Messages = []ConversationMessage{followUpMessage("c", ConversationSenderCandidate, n.Add(-3*24*time.Hour))}
		}, FollowUpEligible},
		{"candidate hours", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.Messages = []ConversationMessage{followUpMessage("c", ConversationSenderCandidate, n.Add(-2*time.Hour))}
		}, FollowUpTooEarly},
		{"rejected", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Application.Status = ApplicationRejected
			i.Application.RawStatus = "DISCARD"
			i.Conversation.RawStatus = "DISCARD"
		}, FollowUpConversationClosed},
		{"offer", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Application.Status = ApplicationOffer
			i.Application.RawStatus = "OFFER"
			i.Conversation.RawStatus = "OFFER"
		}, FollowUpConversationClosed},
		{"interview", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Application.Status = ApplicationInterview
			i.Application.RawStatus = "INTERVIEW"
			i.Conversation.RawStatus = "INTERVIEW"
		}, FollowUpNotEligible},
		{"clarification", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) { i.PendingClarification = true }, FollowUpManualReview},
		{"manual review", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) { i.Warnings = []string{"review"} }, FollowUpManualReview},
		{"unknown next action", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Application.NextAction = "Проверить вручную"
		}, FollowUpManualReview},
		{"max", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.PreviousFollowUps = []time.Time{n.Add(-10 * 24 * time.Hour), n.Add(-6 * 24 * time.Hour)}
		}, FollowUpNotEligible},
		{"recent followup", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.PreviousFollowUps = []time.Time{n.Add(-time.Hour)}
		}, FollowUpTooEarly},
		{"dismiss", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Application.FollowUpState = ConversationFollowUpDismissed
		}, FollowUpNotEligible},
		{"no delivery evidence", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) { i.AppliedAt = nil }, FollowUpManualReview},
		{"unknown status", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) { i.Application.RawStatus = "NEW_UNDOCUMENTED" }, FollowUpManualReview},
		{"incomplete history", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.HHMetadata = map[string]string{"history_incomplete": "true"}
		}, FollowUpManualReview},
		{"future message", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.Messages = []ConversationMessage{followUpMessage("future", ConversationSenderCandidate, n.Add(time.Hour))}
		}, FollowUpManualReview},
		{"timestamp tie", func(i *FollowUpInput, p *FollowUpPolicy, n time.Time) {
			i.Conversation.Messages = []ConversationMessage{followUpMessage("one", ConversationSenderCandidate, n), followUpMessage("two", ConversationSenderEmployer, n)}
		}, FollowUpManualReview},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, now := followUpFixture()
			p := DefaultFollowUpPolicy()
			tc.edit(&in, &p, now)
			got := (FollowUpEngine{p}).Evaluate(in, now)
			if got.Status != tc.want {
				t.Fatalf("got %+v want %s", got, tc.want)
			}
		})
	}
}
func TestFollowUpDraftAndDismissLocalOnly(t *testing.T) {
	s := dashboardTestServer(t)
	in, now := followUpFixture()
	s.Applications.applications = []JobApplication{in.Application}
	s.Conversations.conversations = []EmployerConversation{in.Conversation}
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Здравствуйте! Буду рад обратной связи по моему отклику.", nil, nil)}
	s.Orchestrator.ai = ai
	before, _ := json.Marshal(s.Conversations.conversations)
	decision, err := s.Orchestrator.PrepareFollowUp("a", DefaultFollowUpPolicy(), now)
	requireKnowledgeOK(t, err)
	if decision.Action != AIActionDraftReply || ai.calls != 1 || len(s.Drafts.drafts) != 1 || s.Drafts.drafts[0].Status != AIDraftGenerated || s.Drafts.drafts[0].Type != AIDraftFollowUp {
		t.Fatalf("not a draft-only result: %+v", decision)
	}
	after, _ := json.Marshal(s.Conversations.conversations)
	if string(before) != string(after) || len(s.Applications.events) != 0 {
		t.Fatal("draft changed delivered history")
	}
	if !strings.Contains(ai.user, "confirmed_follow_up_history") || !strings.Contains(ai.user, "days_waiting") || !strings.Contains(ai.user, "candidate_context") {
		t.Fatal("missing follow-up context")
	}
	_, err = s.Orchestrator.PrepareFollowUp("a", DefaultFollowUpPolicy(), now)
	requireKnowledgeOK(t, err)
	if ai.calls != 1 {
		t.Fatal("unchanged draft regenerated")
	}
	requireKnowledgeOK(t, s.dismissFollowUp("a"))
	requireKnowledgeOK(t, s.Applications.Load())
	if (FollowUpEngine{s.FollowUpPolicy}).Evaluate(s.careerSnapshot().input(s.Applications.applications[0]), now).Status != FollowUpNotEligible {
		t.Fatal("dismiss not persisted")
	}
	_, err = s.Orchestrator.PrepareFollowUp("a", DefaultFollowUpPolicy(), now)
	requireKnowledgeOK(t, err)
	if ai.calls != 1 {
		t.Fatal("AI called for dismissed follow-up")
	}
	inbox, err := s.inbox()
	requireKnowledgeOK(t, err)
	if len(inbox.FollowUps) != 0 {
		t.Fatal("dismissed suggestion remains")
	}
	for _, path := range []string{"/api/follow-ups/a/send", "/api/follow-ups/a/approve", "/api/follow-ups/a/submit"} {
		if w := dashboardRequest(s, "POST", path, "{}"); w.Code != 404 {
			t.Fatal("HH action route exists")
		}
	}
}
func TestFollowUpNoHHCapability(t *testing.T) {
	// Keep the pure engine isolated: imports cannot add networking or orchestration.
	f, err := parser.ParseFile(token.NewFileSet(), "follow_up.go", nil, 0)
	requireKnowledgeOK(t, err)
	allowed := map[string]bool{`"errors"`: true, `"flag"`: true, `"strconv"`: true, `"time"`: true}
	for _, i := range f.Imports {
		if !allowed[i.Path.Value] {
			t.Fatalf("engine dependency gained a capability: %s", i.Path.Value)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			switch id.Name {
			case "HHAIResponder", "HHReadClient", "HHRequester", "AIReplyOrchestrator", "NewTicker", "AfterFunc":
				t.Fatalf("engine gained side effects: %s", id.Name)
			}
		}
		return true
	})
	if reflect.TypeOf(FollowUpEngine{}).NumField() != 1 {
		t.Fatal("review new engine capabilities")
	}
}
func TestCareerAuditCorruptionConflictsAndEmpty(t *testing.T) {
	in, now := followUpFixture()
	c := in.Conversation
	c.Status = ConversationWaitingEmployer
	c.Messages = []ConversationMessage{followUpMessage("e", ConversationSenderEmployer, now.Add(-time.Hour))}
	c.RawStatus = "UNRECOGNIZED"
	d := CareerSnapshot{Conversations: []EmployerConversation{c}}
	r := BuildCareerAuditReport(d, DefaultFollowUpPolicy(), now)
	for _, code := range []string{"conversation_without_application", "unknown_hh_status", "status_last_message_conflict"} {
		if r.WarningCounts[code] != 1 {
			t.Fatalf("missing %s: %+v", code, r)
		}
	}
	d.Applications = []JobApplication{in.Application, in.Application}
	d.Conversations[0].Messages = append(c.Messages, c.Messages[0])
	d.Applications[0].ConversationID = "missing"
	r = BuildCareerAuditReport(d, DefaultFollowUpPolicy(), now)
	for _, code := range []string{"duplicate_local_id", "duplicate_message_external_id", "missing_conversation", "application_without_vacancy", "application_without_match_result"} {
		if r.WarningCounts[code] == 0 {
			t.Fatal("missing audit finding", code)
		}
	}
	r = BuildCareerAuditReport(CareerSnapshot{}, DefaultFollowUpPolicy(), now)
	if len(r.Warnings) != 0 || r.FollowUpEligible != 0 {
		t.Fatal("empty dataset failed")
	}
	wd := t.TempDir()
	raw, _ := json.Marshal(conversationStoreFile{Version: 1, Conversations: []EmployerConversation{c, c}})
	requireKnowledgeOK(t, os.WriteFile(filepath.Join(wd, EmployerConversationsFilename), raw, 0600))
	r = BuildCareerAuditReport(readCareerAuditSnapshot(wd, ""), DefaultFollowUpPolicy(), now)
	if r.WarningCounts["duplicate_hh_external_id"] == 0 {
		t.Fatal("diagnostic load hid invalid store")
	}
}
func TestFollowUpPolicyCLIOverridesAndValidation(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "HH_FOLLOW_UP_AFTER_APPLICATION":
			return "bad"
		case "HH_FOLLOW_UP_MAX":
			return "-1"
		}
		return ""
	}
	if _, err := parseDashboardOptions(nil, env, io.Discard); err == nil {
		t.Fatal("invalid environment accepted")
	}
	o, err := parseDashboardOptions([]string{"--follow-up-after-application", "168h", "--follow-up-max", "4"}, env, io.Discard)
	requireKnowledgeOK(t, err)
	if o.FollowUpPolicy.AfterApplicationWithoutReply != 168*time.Hour || o.FollowUpPolicy.MaxFollowUps != 4 {
		t.Fatal("CLI precedence lost")
	}
	for _, args := range [][]string{{"--follow-up-after-message", "0s"}, {"--follow-up-minimum-interval", "-1h"}, {"--follow-up-max", "-2"}} {
		if _, err := parseDashboardOptions(args, func(string) string { return "" }, io.Discard); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}
func TestFollowUpDashboardHealthAndEmptyRoutes(t *testing.T) {
	s := dashboardTestServer(t)
	for _, path := range []string{"/health", "/api/health", "/api/follow-ups", "/api/dashboard", "/api/inbox"} {
		if w := dashboardRequest(s, "GET", path, ""); w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
func TestReadOnlyRequesterRejectsWrite(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer server.Close()
	requester := NewHHRequester(t.Context(), server.Client(), 0)
	requester.readOnly = true
	req, _ := http.NewRequest(http.MethodPost, server.URL, nil)
	if _, err := requester.Do(req); err == nil || calls != 0 {
		t.Fatal("read-only request boundary allowed a write")
	}
}

func TestFollowUpDashboardGenerateDismissAndDetail(t *testing.T) {
	s := dashboardTestServer(t)
	in, _ := followUpFixture()
	sent := time.Now().UTC().Add(-6 * 24 * time.Hour)
	in.Application.CreatedAt = sent
	in.Application.UpdatedAt = sent
	in.Application.HHMetadata["applied_at"] = sent.Format(time.RFC3339Nano)
	in.Conversation.CreatedAt = sent
	in.Conversation.UpdatedAt = sent
	in.Conversation.WaitingSince = &sent
	s.Applications.applications = []JobApplication{in.Application}
	s.Conversations.conversations = []EmployerConversation{in.Conversation}
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Здравствуйте! Буду рад обратной связи по отклику.", nil, nil)}
	s.Orchestrator.ai = ai
	decision := dashboardDecode[AIResponseDecision](t, dashboardRequest(s, "POST", "/api/follow-ups/a/draft", "{}"))
	if decision.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("draft route did not generate: %+v", decision)
	}
	var detail struct {
		FollowUp FollowUpCandidate `json:"follow_up"`
		Draft    *AIDraft          `json:"follow_up_draft"`
	}
	detail = dashboardDecode[struct {
		FollowUp FollowUpCandidate `json:"follow_up"`
		Draft    *AIDraft          `json:"follow_up_draft"`
	}](t, dashboardRequest(s, "GET", "/api/applications/a", ""))
	if detail.Draft == nil || detail.FollowUp.Status != FollowUpEligible {
		t.Fatal("detail lost follow-up")
	}
	if w := dashboardRequest(s, "POST", "/api/follow-ups/a/dismiss", "{}"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	requireKnowledgeOK(t, s.Applications.Load())
	inbox := dashboardDecode[CandidateInbox](t, dashboardRequest(s, "GET", "/api/inbox", ""))
	if len(inbox.FollowUps) != 0 {
		t.Fatal("dismissed suggestion shown")
	}
	if len(s.Applications.events) != 0 || len(s.Conversations.conversations[0].Messages) != 0 {
		t.Fatal("local endpoint fabricated sent history")
	}
}
func TestFollowUpInvalidAIDraftIsNotPersisted(t *testing.T) {
	s := dashboardTestServer(t)
	in, now := followUpFixture()
	s.Applications.applications = []JobApplication{in.Application}
	s.Conversations.conversations = []EmployerConversation{in.Conversation}
	for _, draft := range []string{"Я эксперт Kubernetes production.", strings.Repeat("Текст ", 130)} {
		ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", draft, nil, nil)}
		s.Orchestrator.ai = ai
		d, err := s.Orchestrator.PrepareFollowUp("a", DefaultFollowUpPolicy(), now)
		requireKnowledgeOK(t, err)
		if d.Action != AIActionManualReview || len(s.Drafts.drafts) != 0 {
			t.Fatal("unsafe draft persisted")
		}
	}
}
