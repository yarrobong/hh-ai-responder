package runtime

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pilotConversation(t *testing.T, store *ConversationStore, id string, now time.Time, status ConversationStatus, rawStatus, hhID, text string, age time.Duration) EmployerConversation {
	t.Helper()
	at := now.Add(-age)
	c, err := store.UpsertConversation(EmployerConversation{
		ID: id, VacancyID: 42, HHConversationID: hhID, CompanyName: "Pilot Company " + id, VacancyTitle: "Python integration specialist",
		Status: status, RawStatus: rawStatus, HHUpdatedAt: at.Add(-time.Minute), CreatedAt: now.Add(-30 * 24 * time.Hour),
		Messages: []ConversationMessage{{ID: id + "-employer", Timestamp: at, Sender: ConversationSenderEmployer, Text: text, Source: ConversationSourceHH, Direction: ConversationIncoming}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPilotCandidatesAreSafeRankedAndBodyFree(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	pilotConversation(t, store, "fresh", now, ConversationCandidateActionRequired, "response", "10001", "Есть ли опыт Python?", time.Hour)
	pilotConversation(t, store, "stale", now, ConversationCandidateActionRequired, "response", "10002", "Есть ли опыт Python?", 30*24*time.Hour)
	pilotConversation(t, store, "rejected", now, ConversationCandidateActionRequired, "discard", "10003", "Есть ли опыт Python?", time.Hour)
	pilotConversation(t, store, "waiting", now, ConversationWaitingEmployer, "response", "10004", "Есть ли опыт Python?", time.Hour)
	pilotConversation(t, store, "conflict", now, ConversationWaitingEmployer, "response", "10005", "Есть ли опыт Python?", time.Hour)
	// A clarification-producing question is not a pilot candidate even though
	// its destination and message direction are otherwise valid.
	pilotConversation(t, store, "clarification", now, ConversationCandidateActionRequired, "response", "10006", "Есть ли опыт Kubernetes?", time.Hour)

	set, err := BuildPilotCandidateReports(store, nil, NewCandidateClarificationStore(""), NewCandidateContextResolver(contextTestKnowledge()), NewAIDraftStore(""), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommended) != 1 || set.Recommended[0].ConversationID != "fresh" {
		t.Fatalf("fresh safe conversation was not recommended: %+v", set)
	}
	if len(set.NotRecommended) != 1 || set.NotRecommended[0].ConversationID != "stale" {
		t.Fatalf("stale safe conversation was not separated: %+v", set)
	}
	if len(set.Possible) != 0 {
		t.Fatalf("unexpected possible candidates: %+v", set.Possible)
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Есть ли опыт Python?") || strings.Contains(string(raw), "Есть ли опыт Kubernetes?") {
		t.Fatal("pilot candidate report disclosed message body")
	}
	var output bytes.Buffer
	if err := WritePilotCandidateText(&output, set); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Conversation: fresh") || strings.Contains(output.String(), "Есть ли опыт Python?") {
		t.Fatalf("unsafe or unusable pilot CLI report: %s", output.String())
	}
}

func TestPilotShortlistIncludesReadOnlyDraftPreview(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	pilotConversation(t, store, "shortlist", now, ConversationCandidateActionRequired, "response", "10007", "Есть ли опыт Python?", time.Hour)
	shortlist, err := BuildPilotShortlistPreviews(store, nil, nil, NewCandidateClarificationStore(""), NewCandidateContextResolver(contextTestKnowledge()), now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(shortlist.Candidates) != 1 || shortlist.Candidates[0].ConversationID != "shortlist" || shortlist.Candidates[0].ProposedDraft == "" {
		t.Fatalf("read-only shortlist did not include a draft preview: %+v", shortlist)
	}
	if !strings.Contains(shortlist.Candidates[0].WhySuitable[0], "REPLY_REQUIRED") {
		t.Fatalf("shortlist suitability rationale is incomplete: %+v", shortlist.Candidates[0].WhySuitable)
	}
}

func TestPilotSalaryDraftUsesApprovedWording(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	store := NewConversationStore("")
	c := pilotConversation(t, store, "salary-wording", now, ConversationCandidateActionRequired, "response", "10008", "Напишите,пожалуйста,уровень дохода вы рассматриваете?", time.Minute)
	kb := NewCandidateKnowledgeBase("")
	kb.Profile = stage15Profile()
	kb.Profile.EmployerCommunicationPreferences.Salary.Value = "Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно"
	ctx, err := NewConversationContextBuilder(store, NewCandidateContextResolver(kb)).BuildForReply(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить."
	if got := pilotProposedDraft(ctx, pilotLatestHuman(c)); got != want {
		t.Fatalf("salary pilot wording mismatch: %q", got)
	}
}

func TestStatusAndCourtesyMessagesNeverBecomeReplyRequired(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cases := []string{
		"Рассмотрим ваше резюме. Если навыки и опыт подойдут для позиции, мы свяжемся с вами.",
		"Спасибо за отклик! Рассмотрим ваше резюме.",
		"Если подойдёте, мы свяжемся с вами.",
	}
	for i, message := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
			if err := store.Load(); err != nil {
				t.Fatal(err)
			}
			id := "status-" + string(rune('a'+i))
			conversation := pilotConversation(t, store, id, now, ConversationCandidateActionRequired, "response", "3100"+string(rune('1'+i)), message, time.Hour)
			resolver := NewCandidateContextResolver(contextTestKnowledge())
			ctx, err := NewConversationContextBuilder(store, resolver).BuildForReply(conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if ctx.ReplyRequirement == ReplyRequired || ctx.CandidateContext.MessageIntent == EmployerMessageIntentFactualQuestion {
				t.Fatalf("status/courtesy message became actionable: %+v", ctx)
			}
			state := (ConversationStateResolver{}).Resolve(JobApplication{}, conversation, nil, false, nil, now)
			if state.Status == ConversationCandidateActionRequired {
				t.Fatal("status/courtesy message became candidate_action_required")
			}
			shortlist, err := BuildPilotShortlistPreviews(store, nil, nil, NewCandidateClarificationStore(""), resolver, now, 5)
			if err != nil {
				t.Fatal(err)
			}
			if len(shortlist.Candidates) != 0 {
				t.Fatalf("status/courtesy conversation entered pilot shortlist: %+v", shortlist.Candidates)
			}
			notifications := NewNotificationStore("")
			engine := NewCandidateNotificationEngine(notifications, nil, store, nil, DefaultFollowUpPolicy(), time.Minute)
			if _, err := engine.Calculate(CareerSnapshot{Conversations: []EmployerConversation{conversation}}, now); err != nil {
				t.Fatal(err)
			}
			for _, notification := range notifications.List() {
				if notification.RelatedConversationID == conversation.ID && (notification.Type == NotificationCandidateActionRequired || notification.Type == NotificationNewEmployerMessage) {
					t.Fatalf("status/courtesy conversation created an actionable notification: %+v", notification)
				}
			}
		})
	}
}

func TestRelocationPilotDraftUsesOnlyConfirmedFactAndQuestionLocation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := NewConversationStore(filepath.Join(t.TempDir(), "conversations.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	conversation := pilotConversation(t, store, "relocation", now, ConversationCandidateActionRequired, "response", "32001", "Готовы ли Вы к релокации в Республику Татарстан?", time.Hour)
	kb := NewCandidateKnowledgeBase("")
	kb.Profile = stage15Profile()
	resolver := NewCandidateContextResolver(kb)
	ctx, err := NewConversationContextBuilder(store, resolver).BuildForReply(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Спасибо за вопрос. На данный момент к релокации в Республику Татарстан не готов."
	if got := pilotProposedDraft(ctx, pilotLatestHuman(conversation)); got != want {
		t.Fatalf("unexpected relocation draft: %q", got)
	}
	if warnings := pilotTopicWarnings(ctx, conversation.Messages[0].Text); len(warnings) != 0 {
		t.Fatalf("confirmed relocation was treated as actually high-risk: %v", warnings)
	}
	if !hasAnswerableResolvedFact(ctx.CandidateContext, "relocation") {
		t.Fatalf("confirmed relocation fact was not resolved: %+v", ctx.CandidateContext)
	}
}

func TestPilotObservationStoreRejectsSecretsAndDeduplicates(t *testing.T) {
	store := NewPilotObservationStore("")
	value := PilotObservation{ActionID: "action-1", DeliveryState: string(HHWriteDeliveryConfirmed), ConversationStateAfter: string(ConversationWaitingEmployer), CreatedAt: time.Now().UTC()}
	if err := store.Append(value); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(value); err != nil || len(store.List()) != 1 {
		t.Fatalf("observation was not deduplicated: %v %+v", err, store.List())
	}
	value.ActionID = "action-2"
	value.Issues = []string{"api_key leaked"}
	if err := store.Append(value); err == nil {
		t.Fatal("secret-containing observation accepted")
	}
}

func TestHHWriteStatusExcludesApprovedActionWithTerminalConversation(t *testing.T) {
	gateway, _, _, conversation, draft := newGatewayFixture(t, true, nil)
	action := approveGatewayDraft(t, gateway, draft)
	conversation.Status = ConversationClosed
	if _, err := gateway.Conversations.UpsertConversation(conversation); err != nil {
		t.Fatal(err)
	}
	report := BuildHHWriteStatusReportWithConversations(Config{}, gateway.Actions, gateway.Audit, gateway.Conversations)
	if report.PendingApproved != 0 || len(report.PendingDetails) != 0 || len(report.ExcludedApproved) != 1 {
		t.Fatalf("terminal approved action was projected as pending: %+v", report)
	}
	detail := report.ExcludedApproved[0]
	if detail.ActionID != action.ID || detail.Status != HHWriteApproved || detail.Sendability != "NOT_SENDABLE" || !strings.Contains(detail.Reason, "terminal") {
		t.Fatalf("terminal action exclusion is not explainable: %+v", detail)
	}
}
