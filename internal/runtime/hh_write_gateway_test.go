package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHHWriteRequestPreviewMatchesObservedChatikContract(t *testing.T) {
	text := "Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить."
	idempotencyKey := strings.Repeat("a", 32)
	preview, body, err := buildHHWriteRequestPreview("https://chatik.hh.ru", "5599440665", text, idempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Method != http.MethodPost || preview.Endpoint != "https://chatik.hh.ru/chatik/api/send" || preview.DestinationType != "chat_id" || preview.DestinationID != "5599440665" || preview.ContentType != "application/json" {
		t.Fatalf("unexpected request contract: %+v", preview)
	}
	if preview.Payload["chatId"] != int64(5599440665) || preview.Payload["text"] != text || preview.Payload["idempotencyKey"] != idempotencyKey {
		t.Fatalf("payload was changed: %#v", preview.Payload)
	}
	if err := ValidateHHWriteRequest(preview); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != text || strings.Contains(string(body), "**") || strings.Contains(string(body), "<p>") {
		t.Fatalf("payload was transformed: %s", body)
	}
	if !strings.Contains(string(body), "40–50") || !strings.Contains(string(body), "50–100") || !strings.Contains(string(body), "—") {
		t.Fatalf("payload lost required Unicode text: %s", body)
	}
}

func TestHHWriteRequestValidationRejectsContractMismatches(t *testing.T) {
	valid, _, err := buildHHWriteRequestPreview("https://chatik.hh.ru", "123", "plain text", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*HHWriteRequestPreview)
	}{
		{"wrong method", func(v *HHWriteRequestPreview) { v.Method = http.MethodGet }},
		{"wrong endpoint", func(v *HHWriteRequestPreview) { v.Endpoint = "https://chatik.hh.ru/chatik/api/chat_data" }},
		{"wrong destination type", func(v *HHWriteRequestPreview) { v.DestinationType = "topic_id" }},
		{"mismatched chat id", func(v *HHWriteRequestPreview) { v.Payload["chatId"] = int64(456) }},
		{"empty message", func(v *HHWriteRequestPreview) { v.Payload["text"] = " " }},
		{"wrong content type", func(v *HHWriteRequestPreview) { v.ContentType = "application/x-www-form-urlencoded" }},
		{"short idempotency key", func(v *HHWriteRequestPreview) { v.Payload["idempotencyKey"] = "short" }},
		{"long idempotency key", func(v *HHWriteRequestPreview) { v.Payload["idempotencyKey"] = strings.Repeat("x", 41) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			candidate.Payload = map[string]any{}
			for key, value := range valid.Payload {
				candidate.Payload[key] = value
			}
			tc.edit(&candidate)
			if err := ValidateHHWriteRequest(candidate); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

func TestHHWriteRequestIdempotencyKeyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int
		valid  bool
	}{
		{name: "29 invalid", length: 29, valid: false},
		{name: "30 valid", length: 30, valid: true},
		{name: "40 valid", length: 40, valid: true},
		{name: "41 invalid", length: 41, valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := strings.Repeat("a", tc.length)
			preview, _, err := buildHHWriteRequest("https://chatik.hh.ru", "5599440665", "salary", key)
			if tc.valid {
				if err != nil {
					t.Fatalf("boundary key was rejected: %v", err)
				}
				if got := len(preview.Payload["idempotencyKey"].(string)); got != tc.length {
					t.Fatalf("key length changed: got=%d want=%d", got, tc.length)
				}
			} else if err == nil {
				t.Fatal("boundary key was accepted")
			}
		})
	}
}

func TestHHWriteLiveTransportUsesCanonicalPreviewRequest(t *testing.T) {
	oldLogger := logger
	logger = NewLogger(io.Discard, LevelError)
	t.Cleanup(func() { logger = oldLogger })
	text := "Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить."
	nonce := strings.Repeat("a", 40)
	canonical, expectedBody, err := buildHHWriteRequest("http://placeholder.invalid", "5599440665", text, nonce)
	if err != nil {
		t.Fatal(err)
	}
	preview, previewBody, err := buildHHWriteRequestPreview("http://placeholder.invalid", "5599440665", text, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Method != preview.Method || canonical.Endpoint != preview.Endpoint || canonical.ContentType != preview.ContentType || !bytes.Equal(expectedBody, previewBody) {
		t.Fatalf("preview and canonical request differ: canonical=%+v preview=%+v", canonical, preview)
	}

	var gotMethod, gotPath, gotContentType string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotContentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"messageId":"hh-message-live-parity"}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	jar := &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	jar.SetCookies(base, []*http.Cookie{{Name: "_xsrf", Value: "fixture-token"}})
	responder := &HHAIResponder{ctx: context.Background(), baseURL: base, chatURL: server.URL, client: server.Client(), jar: jar}
	responder.requester = NewHHRequester(context.Background(), server.Client(), 0)
	if _, err := responder.sendConversationMessage(5599440665, text, nonce); err != nil {
		t.Fatal(err)
	}
	if gotMethod != canonical.Method || gotPath != "/chatik/api/send" || gotContentType != canonical.ContentType || !bytes.Equal(gotBody, expectedBody) {
		t.Fatalf("live request differs from canonical preview: method=%s path=%s content-type=%s body=%s want=%s", gotMethod, gotPath, gotContentType, gotBody, expectedBody)
	}
}

func TestHHWriteTransportErrorStoresSanitizedResponseAndNoLocalMessage(t *testing.T) {
	oldLogger := logger
	logger = NewLogger(io.Discard, LevelError)
	t.Cleanup(func() { logger = oldLogger })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chatik/api/send" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("plain text")) {
			t.Fatalf("request body was not plain JSON: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "request-400")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid topic","access_token":"should-not-persist"}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	jar := &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	jar.SetCookies(base, []*http.Cookie{{Name: "_xsrf", Value: "fixture-token"}})
	responder := &HHAIResponder{ctx: context.Background(), baseURL: base, chatURL: server.URL, client: server.Client(), jar: jar}
	responder.requester = NewHHRequester(context.Background(), server.Client(), 0)
	_, err := responder.sendConversationMessage(5599440665, "plain text", strings.Repeat("c", 32))
	var transportErr *HHWriteTransportError
	if !errors.As(err, &transportErr) || transportErr.Category != HHWriteErrorBadRequest || transportErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected transport error: %#v", err)
	}
	if transportErr.ResponseBody == "" || strings.Contains(transportErr.ResponseBody, "access_token") || strings.Contains(transportErr.ResponseBody, "should-not-persist") {
		t.Fatalf("response was not sanitized: %q", transportErr.ResponseBody)
	}
	if transportErr.HHErrorFields["error"] != "invalid topic" || transportErr.CorrelationIDs["X-Request-ID"] != "request-400" {
		t.Fatalf("safe response diagnostics were not captured: %+v", transportErr)
	}
	if transportErr.DeliveryUncertain {
		t.Fatal("known 400 rejection was marked delivery-uncertain")
	}
}

type gatewayReadFake struct {
	state HHConversationReadState
	reads int
}

func (f *gatewayReadFake) ReadVacancies(context.Context, string) (HHVacancyPage, error) {
	return HHVacancyPage{}, nil
}
func (f *gatewayReadFake) ReadApplications(context.Context, string) (HHApplicationPage, error) {
	return HHApplicationPage{}, nil
}
func (f *gatewayReadFake) ReadConversations(context.Context, string) (HHConversationPage, error) {
	return HHConversationPage{}, nil
}
func (f *gatewayReadFake) ReadConversationState(context.Context, string) (HHConversationReadState, error) {
	f.reads++
	return f.state, nil
}

type gatewayWriteFake struct {
	calls           int
	err             error
	conversationIDs []string
	texts           []string
	idempotencyKeys []string
}

func (f *gatewayWriteFake) SendConversationMessage(_ context.Context, conversationID, text, idempotencyKey string) (HHWriteTransportResult, error) {
	f.calls++
	f.conversationIDs = append(f.conversationIDs, conversationID)
	f.texts = append(f.texts, text)
	f.idempotencyKeys = append(f.idempotencyKeys, idempotencyKey)
	if f.err != nil {
		return HHWriteTransportResult{}, f.err
	}
	return HHWriteTransportResult{ExternalMessageID: "hh-message-99", Timestamp: time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)}, nil
}

func TestHHWriteGatewayPreviewMatchesRequestPassedToHHWriteClient(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	preview, err := gateway.BuildRequestPreview(action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Send(context.Background(), action.ID); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || writer.conversationIDs[0] != preview.DestinationID || writer.texts[0] != preview.Payload["text"] || writer.idempotencyKeys[0] != preview.Payload["idempotencyKey"] {
		t.Fatalf("request passed to HHWriteClient differs from preview: preview=%+v writer=%+v", preview, writer)
	}
}

func newGatewayFixture(t *testing.T, enabled bool, writer HHWriteClient) (*HHWriteGateway, *gatewayReadFake, *gatewayWriteFake, EmployerConversation, AIDraft) {
	t.Helper()
	dir := t.TempDir()
	conversations := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	if err := conversations.Load(); err != nil {
		t.Fatal(err)
	}
	c, err := conversations.UpsertConversation(EmployerConversation{VacancyID: 42, HHConversationID: "123", CompanyName: "Fixture", VacancyTitle: "Python", RawStatus: "response"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversations.AppendMessage(c.ID, conversationTestMessage("hh-employer-1", ConversationSenderEmployer, "Есть ли опыт работы с Python", 1)); err != nil {
		t.Fatal(err)
	}
	c, _ = conversations.GetConversation(c.ID)
	drafts := NewAIDraftStore(filepath.Join(dir, "ai_drafts.json"))
	if err := drafts.Load(); err != nil {
		t.Fatal(err)
	}
	draft, err := drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: c.ID, Text: "Да, есть опыт работы с Python.", DecisionReason: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err := drafts.Save(); err != nil {
		t.Fatal(err)
	}
	reader := &gatewayReadFake{state: HHConversationReadState{ExternalID: c.HHConversationID, LastMessageID: latestDeliveredMessageID(c), MessageCount: 1}}
	var fake *gatewayWriteFake
	if provided, ok := writer.(*gatewayWriteFake); ok {
		fake = provided
	}
	if writer == nil {
		fake = &gatewayWriteFake{}
		writer = fake
	}
	kb := knowledgeTestBase(t)
	if err := kb.AddSkill(CandidateSkillDetailed{Name: "Python", Level: SkillLevelWorking, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}); err != nil {
		t.Fatal(err)
	}
	resolver := NewCandidateContextResolver(kb)
	gateway := NewHHWriteGatewayWithOptions(HHWriteGatewayOptions{Enabled: enabled, RequireFreshRead: true, Client: writer, ReadClient: reader, Conversations: conversations, Drafts: drafts, Resolver: resolver, Actions: NewApprovedHHActionStore(filepath.Join(dir, HHWriteActionsFilename)), Audit: NewHHWriteAuditStore(filepath.Join(dir, HHWriteEventsFilename))})
	return gateway, reader, fake, c, draft
}

func approveGatewayDraft(t *testing.T, gateway *HHWriteGateway, draft AIDraft) ApprovedHHAction {
	t.Helper()
	action, err := gateway.ApproveDraft(draft.ID, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if action.ApprovedText != draft.Text || action.ContentHash != contentHash(draft.Text) || action.Status != HHWriteApproved {
		t.Fatalf("approval did not freeze exact text: %+v", action)
	}
	return action
}

func TestHHWriteGatewayDisabledAndExactApproval(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, false, nil)
	action := approveGatewayDraft(t, gateway, draft)
	result, err := gateway.Send(context.Background(), action.ID)
	if err == nil || result.Status != HHWriteStale || writer.calls != 0 {
		t.Fatalf("disabled gateway sent or did not fail closed: result=%+v err=%v calls=%d", result, err, writer.calls)
	}
}

func TestHHWriteGatewayApprovalDoesNotReadOrWriteHH(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	if _, err := gateway.ApproveDraft(draft.ID, "candidate"); err != nil {
		t.Fatal(err)
	}
	if reader.reads != 0 || writer.calls != 0 {
		t.Fatalf("approval crossed the HH boundary: reads=%d writes=%d", reader.reads, writer.calls)
	}
}

func TestHHWriteGatewayReapprovalCreatesIndependentFreshAction(t *testing.T) {
	gateway, _, _, _, draft := newGatewayFixture(t, true, nil)
	old := approveGatewayDraft(t, gateway, draft)
	usedAt := time.Now().UTC()
	old.Status = HHWriteManualReview
	old.NonceUsedAt = &usedAt
	old.Error = "historical transport failed"
	if err := gateway.Actions.put(old); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Actions.Save(); err != nil {
		t.Fatal(err)
	}

	newAction := approveGatewayDraft(t, gateway, draft)
	if newAction.ID == old.ID || newAction.SendNonce == old.SendNonce {
		t.Fatalf("reapproval reused identity or nonce: old=%+v new=%+v", old, newAction)
	}
	if newAction.ApprovedText != old.ApprovedText || newAction.ContentHash != old.ContentHash || newAction.RelevantKnowledgeHash == "" {
		t.Fatalf("reapproval did not preserve exact approved data: old=%+v new=%+v", old, newAction)
	}
	retained, err := gateway.Actions.Get(old.ID)
	if err != nil || retained.Status != HHWriteManualReview || retained.NonceUsedAt == nil || retained.Error != old.Error {
		t.Fatalf("old audit action was changed: %+v %v", retained, err)
	}
}

func TestHHWriteGatewayAllowsOnlyOneActiveActionPerEmployerMessagePurpose(t *testing.T) {
	gateway, _, _, conversation, draft := newGatewayFixture(t, true, nil)
	first := approveGatewayDraft(t, gateway, draft)
	second, err := gateway.Drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: conversation.ID, Text: "Подтверждаю, опыт работы с Python есть.", DecisionReason: "second fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.ApproveDraft(second.ID, "candidate"); err == nil {
		t.Fatal("created a second active action for the same employer message and purpose")
	}
	active := 0
	for _, action := range gateway.Actions.List() {
		if action.ConversationID == conversation.ID && action.LastMessageID == first.LastMessageID && hhActionIsActive(action.Status) {
			active++
		}
	}
	if active != 1 || first.ReplyPurpose != "conversation_reply" || len(first.SendNonce) < 30 || len(first.SendNonce) > 40 {
		t.Fatalf("active action uniqueness or approval metadata failed: active=%d action=%+v", active, first)
	}
}

func TestHHWriteGatewayReplacesStaleDuplicateActionAndPreservesHistory(t *testing.T) {
	gateway, _, _, conversation, draft := newGatewayFixture(t, true, nil)
	old := approveGatewayDraft(t, gateway, draft)
	old.Status = HHWriteStale
	old.Error = "stale after review"
	if err := gateway.Actions.put(old); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Actions.Save(); err != nil {
		t.Fatal(err)
	}
	second, err := gateway.Drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: conversation.ID, Text: "Да, подтверждаю опыт работы с Python.", DecisionReason: "replacement fixture"})
	if err != nil {
		t.Fatal(err)
	}
	newAction, err := gateway.ApproveDraft(second.ID, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := gateway.Actions.Get(old.ID)
	if err != nil || retained.Status != HHWriteStale || retained.Error != old.Error {
		t.Fatalf("stale action history was changed: %+v err=%v", retained, err)
	}
	if newAction.ID == old.ID || newAction.SendNonce == old.SendNonce || newAction.LastMessageID != old.LastMessageID || newAction.ReplyPurpose != old.ReplyPurpose {
		t.Fatalf("replacement action did not get fresh deduplication metadata: old=%+v new=%+v", old, newAction)
	}
}

func TestHHWriteGatewayDryRunFreshValidationDoesNotConsumeNonceOrMetrics(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, false, nil)
	gateway.DryRun = true
	action := approveGatewayDraft(t, gateway, draft)
	before := BuildHHWriteMetrics(gateway.Audit.List())

	for attempt := 0; attempt < 2; attempt++ {
		result, err := gateway.Send(context.Background(), action.ID)
		if err == nil || result.Error != "BLOCKED_BY_DRY_RUN" || result.WriteCapability != "BLOCKED_BY_DRY_RUN" || result.TransportAttempted {
			t.Fatalf("dry-run attempt %d was not blocked before transport: %+v err=%v", attempt+1, result, err)
		}
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil || stored.Status != HHWriteApproved || stored.NonceUsedAt != nil {
		t.Fatalf("dry-run consumed or changed transport nonce: %+v %v", stored, err)
	}
	after := BuildHHWriteMetrics(gateway.Audit.List())
	if after.WriteAttemptsTotal != before.WriteAttemptsTotal || after.SuccessfulWrites != before.SuccessfulWrites || after.FailedWrites != before.FailedWrites {
		t.Fatalf("dry-run changed transport metrics: before=%+v after=%+v", before, after)
	}
	if reader.reads != 2 || writer.calls != 0 {
		t.Fatalf("dry-run did not re-run read-only fresh validation for each Send: reads=%d writes=%d", reader.reads, writer.calls)
	}
}

func TestHHWriteGatewayInvalidIdempotencyKeyFailsBeforeHHWriteClient(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, false, nil)
	gateway.DryRun = true
	action := approveGatewayDraft(t, gateway, draft)
	action.SendNonce = strings.Repeat("x", 29)
	if err := gateway.Actions.put(action); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Actions.Save(); err != nil {
		t.Fatal(err)
	}
	result, err := gateway.Send(context.Background(), action.ID)
	if err == nil || result.Error != "REQUEST_VALIDATION_FAILED" || writer.calls != 0 {
		t.Fatalf("invalid idempotency key was not rejected before client: result=%+v err=%v calls=%d", result, err, writer.calls)
	}
}

func TestHHWriteGatewayPersistedPreflightIsNotFreshAfterRestart(t *testing.T) {
	gateway, _, _, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	now := time.Now().UTC().Add(-time.Minute)
	action.LastPreflightAt = &now
	action.LastPreflightOK = true
	if gateway.PreflightIsFresh(action) {
		t.Fatal("persisted preflight unexpectedly unlocked this process")
	}
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if !preflight.Allowed {
		t.Fatalf("fresh preflight failed: %+v", preflight)
	}
	updated, err := gateway.Actions.Get(action.ID)
	if err != nil || !gateway.PreflightIsFresh(updated) {
		t.Fatalf("fresh preflight was not recorded for this process: %+v %v", updated, err)
	}
}

func TestHHWriteGatewayEditAndEmployerMessageMakeApprovalStale(t *testing.T) {
	gateway, reader, _, conversation, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.EditDraft(draft.ID, "Изменённый точный текст."); err != nil {
		t.Fatal(err)
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil || stored.Status != HHWriteStale {
		t.Fatalf("edit did not stale action: %+v %v", stored, err)
	}

	gateway, reader, _, conversation, draft = newGatewayFixture(t, true, nil)
	action = approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.Conversations.AppendMessage(conversation.ID, conversationTestMessage("hh-employer-2", ConversationSenderEmployer, "Новый вопрос", 2)); err != nil {
		t.Fatal(err)
	}
	reader.state.LastMessageID = "hh-employer-2"
	if _, err := gateway.Send(context.Background(), action.ID); err == nil {
		t.Fatal("new employer message did not stale approval")
	}
	stored, _ = gateway.Actions.Get(action.ID)
	if stored.Status != HHWriteStale {
		t.Fatalf("new employer message left action sendable: %+v", stored)
	}
}

func TestHHWriteGatewayLocalDraftStalenessStopsBeforeFreshPreflight(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.EditDraft(draft.ID, "Локально изменённый текст."); err != nil {
		t.Fatal(err)
	}
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if preflight.Allowed || reader.reads != 0 || writer.calls != 0 {
		t.Fatalf("local draft staleness crossed the preflight boundary: preflight=%+v reads=%d writes=%d", preflight, reader.reads, writer.calls)
	}
}

func TestHHWriteGatewaySuccessIsIdempotentAndAppendsOnlyAfterHHSuccess(t *testing.T) {
	gateway, _, writer, conversation, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	first, err := gateway.Send(context.Background(), action.ID)
	if err != nil || !first.Success || writer.calls != 1 {
		t.Fatalf("valid send failed: %+v %v calls=%d", first, err, writer.calls)
	}
	second, err := gateway.Send(context.Background(), action.ID)
	if err != nil || !second.Success || writer.calls != 1 {
		t.Fatalf("second send was not idempotent: %+v %v calls=%d", second, err, writer.calls)
	}
	c, _ := gateway.Conversations.GetConversation(conversation.ID)
	if len(c.Messages) != 2 || c.Messages[1].Source != ConversationSourceHHWrite || c.Messages[1].ExternalID != "hh-message-99" {
		t.Fatalf("successful HH write was not imported as outgoing message: %+v", c.Messages)
	}

	gateway, _, failedWriter, conversation, draft := newGatewayFixture(t, true, &gatewayWriteFake{err: errors.New("known HH rejection")})
	action = approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.Send(context.Background(), action.ID); err == nil {
		t.Fatal("known write failure was accepted")
	}
	c, _ = gateway.Conversations.GetConversation(conversation.ID)
	if len(c.Messages) != 1 || failedWriter.calls != 1 {
		t.Fatalf("failed write created a false outgoing message: messages=%d calls=%d", len(c.Messages), failedWriter.calls)
	}
}

func TestHHWriteGatewayTerminalPreflightShortCircuitsAllFreshChecks(t *testing.T) {
	cases := []struct {
		status HHWriteActionStatus
		code   string
	}{
		{HHWriteDeliveryConfirmed, HHPreflightReasonDeliveryAlreadyConfirmed},
		{HHWriteSentUnconfirmed, HHPreflightReasonSentUnconfirmed},
		{HHWriteDeliveryUncertain, HHPreflightReasonDeliveryUncertain},
		{HHWriteFailed, HHPreflightReasonFailedAfterTransport},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			gateway, reader, _, _, draft := newGatewayFixture(t, true, nil)
			action := approveGatewayDraft(t, gateway, draft)
			action.Status = tc.status
			action.ExternalMessageID = "hh-message-terminal"
			used := time.Now().UTC()
			action.NonceUsedAt = &used
			if err := gateway.Actions.put(action); err != nil {
				t.Fatal(err)
			}
			if err := gateway.Actions.Save(); err != nil {
				t.Fatal(err)
			}
			preflight := gateway.PreflightAction(context.Background(), action.ID)
			if preflight.Allowed || preflight.Status != "BLOCKED" || preflight.ReasonCode != tc.code || preflight.SendRequestPerformed || len(preflight.Reasons) != 1 {
				t.Fatalf("terminal preflight was not short-circuited: %+v", preflight)
			}
			if reader.reads != 0 {
				t.Fatalf("terminal preflight performed a fresh HH read: %d", reader.reads)
			}
			for _, reason := range preflight.Reasons {
				if strings.Contains(reason, "knowledge") || strings.Contains(reason, "salary") || strings.Contains(reason, "conversation") {
					t.Fatalf("terminal preflight leaked mutable diagnostics: %q", reason)
				}
			}
		})
	}
}

func TestHHWriteGatewayHTTP400PersistsTerminalFailureAndCLIProjection(t *testing.T) {
	transportErr := &HHWriteTransportError{
		Category:            HHWriteErrorBadRequest,
		Status:              http.StatusBadRequest,
		ResponseContentType: "application/json; charset=utf-8",
		ResponseBody:        `{"code":400,"error":[{"description":"\"idempotency_key\" must be from 30 to 40 symbols","key":"BAD_IDEMPOTENCY_KEY_VALUE"}]}`,
		HHErrorFields: map[string]string{
			"code":                 "400",
			"error[0].description": `"idempotency_key" must be from 30 to 40 symbols`,
			"error[0].key":         "BAD_IDEMPOTENCY_KEY_VALUE",
		},
		Err: errors.New("HH returned status 400"),
	}
	gateway, _, writer, _, draft := newGatewayFixture(t, true, &gatewayWriteFake{err: transportErr})
	action := approveGatewayDraft(t, gateway, draft)
	result, err := gateway.Send(context.Background(), action.ID)
	if err == nil || result.Status != HHWriteFailed || !result.TransportAttempted || writer.calls != 1 || len(writer.idempotencyKeys) != 1 || writer.idempotencyKeys[0] != action.SendNonce {
		t.Fatalf("HTTP 400 was not recorded as a transport failure: result=%+v err=%v calls=%d", result, err, writer.calls)
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil || stored.Status != HHWriteFailed || stored.NonceUsedAt == nil || stored.SendNonce == "" {
		t.Fatalf("failed action was not terminal with a consumed nonce: %+v %v", stored, err)
	}

	reloadedAudit := NewHHWriteAuditStore(gateway.Audit.path)
	if err := reloadedAudit.Load(); err != nil {
		t.Fatal(err)
	}
	events := reloadedAudit.List()
	var started, response, failed *HHWriteEvent
	for i := range events {
		if events[i].ActionID != action.ID {
			continue
		}
		switch events[i].Type {
		case "send_started":
			started = &events[i]
		case "transport_response":
			response = &events[i]
		case string(HHWriteFailed):
			failed = &events[i]
		}
	}
	if started == nil || response == nil || failed == nil || response.HTTPStatus != http.StatusBadRequest || response.ResponseContentType != transportErr.ResponseContentType || response.ResponseBody == "" || response.HHErrorFields["error[0].key"] != "BAD_IDEMPOTENCY_KEY_VALUE" {
		t.Fatalf("durable 400 lifecycle is incomplete: started=%+v response=%+v failed=%+v", started, response, failed)
	}
	metrics := BuildHHWriteMetrics(events)
	if metrics.WriteAttemptsTotal != 1 || metrics.FailedWrites != 1 || metrics.SuccessfulWrites != 0 {
		t.Fatalf("durable metrics are inconsistent: %+v", metrics)
	}
	if _, err := gateway.Send(context.Background(), action.ID); err == nil || writer.calls != 1 {
		t.Fatalf("failed action was retried: err=%v calls=%d", err, writer.calls)
	}

	// Read the same durable files through the CLI command path, as a separate
	// process would. This guards against the Dashboard's in-memory projection
	// hiding a persistence problem from `hh write-status`.
	oldGetwd := osGetwd
	osGetwd = func() (string, error) { return filepath.Dir(gateway.Audit.path), nil }
	t.Cleanup(func() { osGetwd = oldGetwd })
	var output strings.Builder
	if err := runHHCommand([]string{"write-status"}, Config{DryRun: true}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	status := output.String()
	for _, expected := range []string{"write_attempts_total: 1", "failed_writes: 1", "successful_writes: 0"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("CLI projection missed durable failure %q: %s", expected, status)
		}
	}
}

func TestHHWriteAuditSaveFailureIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewHHWriteAuditStore(path)
	store.events = []HHWriteEvent{{ActionID: "action", ConversationID: "conversation", Type: "send_started", Result: "send_started", CreatedAt: time.Now().UTC()}}
	if err := store.Save(); err == nil {
		t.Fatal("audit save failure was swallowed")
	}
}

func TestHHWriteGatewayRecordsPilotObservationAfterSend(t *testing.T) {
	gateway, _, _, conversation, draft := newGatewayFixture(t, true, nil)
	observations := NewPilotObservationStore("")
	gateway.Observations = observations
	action := approveGatewayDraft(t, gateway, draft)
	result, err := gateway.Send(context.Background(), action.ID)
	if err != nil || !result.Success {
		t.Fatalf("send failed: %+v %v", result, err)
	}
	values := observations.List()
	if len(values) != 1 || values[0].ActionID != action.ID || !values[0].DraftGenerated || !values[0].PreflightPassed || values[0].ConversationStateAfter == "" {
		t.Fatalf("pilot observation missing: %+v", values)
	}
	if values[0].DeliveryState != string(result.Status) || values[0].ReconciliationSuccess {
		t.Fatalf("unexpected observation delivery state: %+v", values[0])
	}
	updated, _ := gateway.Conversations.GetConversation(conversation.ID)
	if len(updated.Messages) != 2 {
		t.Fatalf("observation test did not use the normal local reconciliation path: %+v", updated.Messages)
	}
}

func TestHHWriteGatewayUncertainResultIsNotRetried(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, true, &gatewayWriteFake{err: &HHWriteError{Err: errors.New("network timeout"), DeliveryUncertain: true}})
	action := approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.Send(context.Background(), action.ID); err == nil {
		t.Fatal("uncertain result was accepted")
	}
	if _, err := gateway.Send(context.Background(), action.ID); err == nil || writer.calls != 1 {
		t.Fatalf("uncertain delivery was retried: err=%v calls=%d", err, writer.calls)
	}
	stored, _ := gateway.Actions.Get(action.ID)
	if stored.Status != HHWriteDeliveryUncertain {
		t.Fatalf("unexpected uncertain status %s", stored.Status)
	}
}

func TestHHWriteGatewayDryRunOverrideAlwaysBlocks(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, true, nil)
	gateway.DryRun = true
	action := approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.Send(context.Background(), action.ID); err == nil || writer.calls != 0 {
		t.Fatalf("dry-run override allowed a write: err=%v calls=%d", err, writer.calls)
	}
}

func TestHHWriteGatewayMissingPersistedNonceFailsClosed(t *testing.T) {
	gateway, _, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	action.SendNonce = ""
	if err := gateway.Actions.put(action); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Actions.Save(); err != nil {
		t.Fatal(err)
	}
	result, err := gateway.Send(context.Background(), action.ID)
	if err == nil || result.Error != "MISSING_NONCE" || writer.calls != 0 {
		t.Fatalf("missing nonce was not fail-closed: result=%+v err=%v calls=%d", result, err, writer.calls)
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SendNonce != "" || stored.Status != HHWriteApproved {
		t.Fatalf("missing nonce was regenerated or action mutated: %+v", stored)
	}
}

func TestHHWriteGatewayNonceSurvivesStoreReload(t *testing.T) {
	gateway, _, _, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	reloaded := NewApprovedHHActionStore(gateway.Actions.path)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	stored, err := reloaded.Get(action.ID)
	if err != nil || stored.SendNonce == "" || stored.SendNonce != action.SendNonce {
		t.Fatalf("approved nonce did not survive restart: action=%+v err=%v", stored, err)
	}
}

func TestHHWriteGatewayPreviewReadsFreshStateWithoutWrite(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if !preflight.Allowed || reader.reads != 1 || writer.calls != 0 {
		t.Fatalf("preview did not perform only fresh read: preflight=%+v reads=%d calls=%d", preflight, reader.reads, writer.calls)
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil || stored.LastPreflightAt == nil || !stored.LastPreflightOK {
		t.Fatalf("preview metadata was not persisted: %+v %v", stored, err)
	}
}

func TestHHWriteGatewayUnrelatedKnowledgeChangeDoesNotMakeApprovalStale(t *testing.T) {
	gateway, reader, _, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	reader.state.LastMessageID = action.LastMessageID
	if err := gateway.Resolver.kb.AddSkill(CandidateSkillDetailed{Name: "New confirmed skill", Level: SkillLevelWorking, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}); err != nil {
		t.Fatal(err)
	}
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if !preflight.Allowed {
		t.Fatalf("unrelated candidate knowledge change made action stale: %+v", preflight)
	}
}

func TestHHWriteGatewayRelevantKnowledgeChangeMakesApprovalStale(t *testing.T) {
	gateway, reader, _, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	reader.state.LastMessageID = action.LastMessageID
	if len(gateway.Resolver.kb.Skills) == 0 {
		t.Fatal("fixture did not create a detailed skill")
	}
	gateway.Resolver.kb.Skills[0].Level = SkillLevelAdvanced
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	found := false
	for _, reason := range preflight.Reasons {
		found = found || reason == "candidate knowledge relevant to this draft changed"
	}
	if preflight.Allowed || !found {
		t.Fatalf("relevant candidate knowledge change did not stale action: %+v", preflight)
	}
	if reader.reads != 0 {
		t.Fatalf("relevant knowledge staleness performed fresh HH preflight: %d", reader.reads)
	}
}

func TestHHWriteGatewayKnowledgeMetadataUpdateDoesNotMakeApprovalStale(t *testing.T) {
	gateway, reader, _, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	reader.state.LastMessageID = action.LastMessageID
	gateway.Resolver.kb.Skills[0].UpdatedAt = gateway.Resolver.kb.Skills[0].UpdatedAt.Add(24 * time.Hour)
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	if !preflight.Allowed {
		t.Fatalf("metadata-only knowledge update made action stale: %+v", preflight)
	}
}

func TestHHWriteGatewayRelevantSalaryChangeMakesApprovalStale(t *testing.T) {
	gateway, reader, _, conversation, draft := newGatewayFixture(t, true, nil)
	message := conversationTestMessage("hh-salary", ConversationSenderEmployer, "Укажите зарплатные ожидания", 2)
	if _, err := gateway.Conversations.AppendMessage(conversation.ID, message); err != nil {
		t.Fatal(err)
	}
	updatedConversation, err := gateway.Conversations.GetConversation(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader.state.LastMessageID = latestDeliveredMessageID(updatedConversation)
	if err := gateway.Drafts.updateText(draft.ID, "Рассматриваю предложения от 40–50 тыс. рублей.", AIDraftSourceUserEdited); err != nil {
		t.Fatal(err)
	}
	draft, err = gateway.Drafts.Get(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	gateway.Resolver.kb.Profile.EmployerCommunicationPreferences.Salary = ProfileStringFact{Value: "40–50 тыс. рублей", ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"confirmed salary expectations"}}}
	action := approveGatewayDraft(t, gateway, draft)
	current, _ := gateway.Drafts.Get(draft.ID)
	if current.Text != action.ApprovedText {
		t.Fatal("salary fixture did not preserve approved text")
	}
	gateway.Resolver.kb.Profile.EmployerCommunicationPreferences.Salary.Value = "60–70 тыс. рублей"
	preflight := gateway.PreflightAction(context.Background(), action.ID)
	found := false
	for _, reason := range preflight.Reasons {
		found = found || reason == "candidate knowledge relevant to this draft changed"
	}
	if preflight.Allowed || !found {
		t.Fatalf("salary change did not stale action: %+v", preflight)
	}
}

func TestHHWriteGatewayLimitAndNonce(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	gateway.MaxWritesPerRun = 1
	first := approveGatewayDraft(t, gateway, draft)
	if first.SendNonce == "" {
		t.Fatal("approval did not create a one-time send nonce")
	}
	if _, err := gateway.Send(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := gateway.Actions.Get(first.ID)
	if err != nil || stored.NonceUsedAt == nil {
		t.Fatalf("send nonce was not consumed: %+v %v", stored, err)
	}
	if _, err := gateway.Send(context.Background(), first.ID); err != nil || writer.calls != 1 {
		t.Fatalf("repeated send was not idempotently blocked: err=%v calls=%d", err, writer.calls)
	}

	secondConversation, err := gateway.Conversations.UpsertConversation(EmployerConversation{VacancyID: 43, HHConversationID: "456", CompanyName: "Fixture 2", VacancyTitle: "Python"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Conversations.AppendMessage(secondConversation.ID, conversationTestMessage("hh-employer-2", ConversationSenderEmployer, "Есть ли опыт работы с Python", 2)); err != nil {
		t.Fatal(err)
	}
	secondConversation, _ = gateway.Conversations.GetConversation(secondConversation.ID)
	secondDraft, err := gateway.Drafts.Create(AIDraft{Type: AIDraftEmployerReply, ConversationID: secondConversation.ID, Text: "Да, есть опыт работы с Python.", DecisionReason: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	reader.state = HHConversationReadState{ExternalID: "456", LastMessageID: latestDeliveredMessageID(secondConversation), MessageCount: 1}
	second := approveGatewayDraft(t, gateway, secondDraft)
	if _, err := gateway.Send(context.Background(), second.ID); err == nil || writer.calls != 1 {
		t.Fatalf("per-run limit did not block second write: err=%v calls=%d", err, writer.calls)
	}
}

func TestHHWriteGatewaySentUnconfirmedCanOnlyReconcile(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	result, err := gateway.Send(context.Background(), action.ID)
	if err != nil || result.Status != HHWriteSentUnconfirmed || writer.calls != 1 {
		t.Fatalf("successful write without read confirmation had wrong state: %+v %v calls=%d", result, err, writer.calls)
	}
	reader.state.MessageIDs = []string{"hh-message-99"}
	reconciled, err := gateway.ReconcileDelivery(context.Background(), action.ID)
	if err != nil || reconciled.Status != HHWriteDeliveryConfirmed || writer.calls != 1 {
		t.Fatalf("reconciliation did not confirm delivery without retry: %+v %v calls=%d", reconciled, err, writer.calls)
	}
}

func TestHHWriteGatewayAmbiguousReconciliationConfirmsWithoutRetry(t *testing.T) {
	gateway, reader, writer, _, draft := newGatewayFixture(t, true, &gatewayWriteFake{err: &HHWriteError{Err: errors.New("network timeout"), DeliveryUncertain: true}})
	action := approveGatewayDraft(t, gateway, draft)
	if _, err := gateway.Send(context.Background(), action.ID); err == nil || writer.calls != 1 {
		t.Fatalf("ambiguous write was not recorded: err=%v calls=%d", err, writer.calls)
	}
	stored, err := gateway.Actions.Get(action.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.ExternalMessageID = "hh-message-ambiguous"
	if err := gateway.Actions.put(stored); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Actions.Save(); err != nil {
		t.Fatal(err)
	}
	reader.state.MessageIDs = []string{"hh-message-ambiguous"}
	reconciled, err := gateway.ReconcileDelivery(context.Background(), action.ID)
	if err != nil || reconciled.Status != HHWriteDeliveryConfirmed || writer.calls != 1 {
		t.Fatalf("ambiguous delivery was not confirmed read-only: %+v err=%v calls=%d", reconciled, err, writer.calls)
	}
}
