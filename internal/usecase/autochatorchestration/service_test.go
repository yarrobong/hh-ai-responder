package autochatorchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"hh-ai-responder/internal/usecase/autochatreply"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

type sourceFake struct {
	chats       []Chat
	histories   map[int64]History
	historyErrs map[int64]error
	ignored     []int64
	maxPages    int
}

func (f *sourceFake) AwaitingChats(_ context.Context, maxPages int) ([]Chat, error) {
	f.maxPages = maxPages
	return append([]Chat(nil), f.chats...), nil
}

func (f *sourceFake) ReadHistory(ctx context.Context, chatID, _ int64) (History, error) {
	if err := ctx.Err(); err != nil {
		return History{}, err
	}
	if err := f.historyErrs[chatID]; err != nil {
		return History{}, err
	}
	return f.histories[chatID], nil
}

func (f *sourceFake) IgnoreChat(chatID int64) { f.ignored = append(f.ignored, chatID) }

type preparerFake struct {
	proposals map[int64]autochatreply.ProposedResult
	errs      map[int64]error
	calls     []int64
}

func (f *preparerFake) Prepare(_ context.Context, input autochatreply.Input) (autochatreply.ProposedResult, error) {
	f.calls = append(f.calls, input.ChatID)
	if err := f.errs[input.ChatID]; err != nil {
		return autochatreply.ProposedResult{}, err
	}
	return f.proposals[input.ChatID], nil
}

type actionFake struct {
	sendResults  map[int64]ActionResult
	sendErrors   map[int64]error
	leaveResults map[int64]ActionResult
	leaveErrors  map[int64]error
	sendCalls    []int64
	leaveCalls   []int64
}

func (f *actionFake) SendChatMessage(_ context.Context, chatID int64, _ string) (ActionResult, error) {
	f.sendCalls = append(f.sendCalls, chatID)
	return f.sendResults[chatID], f.sendErrors[chatID]
}

func (f *actionFake) LeaveChat(_ context.Context, chatID int64) (ActionResult, error) {
	f.leaveCalls = append(f.leaveCalls, chatID)
	return f.leaveResults[chatID], f.leaveErrors[chatID]
}

type auditFake struct{ events []AuditEvent }

func (f *auditFake) Append(_ context.Context, event AuditEvent) error {
	f.events = append(f.events, event)
	return nil
}

type loggerFake struct{}

func (loggerFake) Debug(string, ...any) {}
func (loggerFake) Info(string, ...any)  {}
func (loggerFake) Warn(string, ...any)  {}
func (loggerFake) Error(string, ...any) {}

func testChat(id int64) Chat {
	return Chat{
		ID: id, ContactName: "HR", EmployerMessage: "Есть ли опыт?", VacancyName: "Backend",
		VacancyURL: "https://example.test/vacancy", CompanyName: "Example", VacancyCompensation: "100000 руб.",
		ResumeHash: "resume", ResumeTitle: "Python developer", ApplicantID: 99,
		Candidate: autochatreply.Candidate{FirstName: "Test", LastName: "Candidate", Context: candidatecontext.CandidateContext{}},
	}
}

func testHistory() History {
	return History{WriteAllowed: true, Messages: []autochatreply.HistoryMessage{{Timestamp: time.Unix(1, 0), Author: "HR", Text: "Есть ли опыт?"}}}
}

func runTestService(source *sourceFake, preparer *preparerFake, actions *actionFake, audit *auditFake, options Options) Result {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Unix(2, 0).UTC() }
	}
	options.AllowAutomaticReplies = true
	result, err := NewService(Dependencies{Chats: source, ReplyPreparer: preparer, Actions: actions, Audit: audit, Logger: loggerFake{}}, options).Run(context.Background(), Input{})
	if err != nil {
		panic(err)
	}
	return result
}

func TestRunAutoModeRequiresExplicitReplyApproval(t *testing.T) {
	source := &sourceFake{chats: []Chat{testChat(99)}, histories: map[int64]History{99: testHistory()}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{99: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &actionFake{}
	result, err := NewService(Dependencies{Chats: source, ReplyPreparer: preparer, Actions: actions}, Options{Mode: "auto"}).Run(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions.sendCalls) != 0 || result.ManualReview != 1 || result.Items[0].Outcome != ItemManualReview {
		t.Fatalf("automatic employer reply was not blocked: result=%+v sends=%v", result, actions.sendCalls)
	}
}

func TestRunReplyExecutesOneSendAndNoLeave(t *testing.T) {
	source := &sourceFake{chats: []Chat{testChat(1)}, histories: map[int64]History{1: testHistory()}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{1: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &actionFake{sendResults: map[int64]ActionResult{1: {Outcome: ActionAccepted}}}
	audit := &auditFake{}
	result := runTestService(source, preparer, actions, audit, Options{Mode: "auto"})
	if len(actions.sendCalls) != 1 || len(actions.leaveCalls) != 0 || result.Replied != 1 || len(audit.events) != 1 || audit.events[0].Type != "chat_reply" {
		t.Fatalf("result=%+v sends=%v leaves=%v audit=%+v", result, actions.sendCalls, actions.leaveCalls, audit.events)
	}
}

func TestRunDiscardLeaveExecutesOneLeaveAndNoSend(t *testing.T) {
	chat := testChat(2)
	chat.IsDiscard = true
	source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{}}
	actions := &actionFake{leaveResults: map[int64]ActionResult{2: {Outcome: ActionAccepted}}}
	result := runTestService(source, &preparerFake{}, actions, &auditFake{}, Options{Mode: "auto"})
	if len(actions.leaveCalls) != 1 || len(actions.sendCalls) != 0 || result.Left != 1 {
		t.Fatalf("result=%+v sends=%v leaves=%v", result, actions.sendCalls, actions.leaveCalls)
	}
}

func TestRunDryRunAndReviewDoNotCallActions(t *testing.T) {
	for _, options := range []Options{{Mode: "auto", DryRun: true}, {Mode: "review"}} {
		source := &sourceFake{chats: []Chat{testChat(3)}, histories: map[int64]History{3: testHistory()}}
		preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{3: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
		actions := &actionFake{}
		result := runTestService(source, preparer, actions, &auditFake{}, options)
		if len(actions.sendCalls) != 0 || len(actions.leaveCalls) != 0 || result.ManualReview != 1 {
			t.Fatalf("options=%+v result=%+v sends=%v leaves=%v", options, result, actions.sendCalls, actions.leaveCalls)
		}
	}
}

func TestRunManualReviewAndNoReplyHaveNoActions(t *testing.T) {
	source := &sourceFake{chats: []Chat{testChat(4), testChat(5)}, histories: map[int64]History{4: testHistory(), 5: testHistory()}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{
		4: {Outcome: autochatreply.OutcomeManualReview, Text: "Не знаю", ReviewReason: "unknown fact"},
		5: {Outcome: autochatreply.OutcomeNoReply},
	}}
	actions := &actionFake{}
	result := runTestService(source, preparer, actions, &auditFake{}, Options{Mode: "auto"})
	if len(actions.sendCalls) != 0 || len(actions.leaveCalls) != 0 || result.ManualReview != 1 || result.Items[1].Outcome != ItemNoReply {
		t.Fatalf("result=%+v sends=%v leaves=%v", result, actions.sendCalls, actions.leaveCalls)
	}
}

func TestRunAmbiguousSendIsNotRetriedAndDoesNotFallBackToLeave(t *testing.T) {
	source := &sourceFake{chats: []Chat{testChat(6)}, histories: map[int64]History{6: testHistory()}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{6: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &actionFake{sendResults: map[int64]ActionResult{6: {Outcome: ActionDeliveryUncertain}}, sendErrors: map[int64]error{6: errors.New("delivery uncertain")}}
	result := runTestService(source, preparer, actions, &auditFake{}, Options{Mode: "auto"})
	if len(actions.sendCalls) != 1 || len(actions.leaveCalls) != 0 || result.Items[0].Outcome != ItemDeliveryUncertain {
		t.Fatalf("result=%+v sends=%v leaves=%v", result, actions.sendCalls, actions.leaveCalls)
	}
}

func TestRunContinuesAfterItemFailureAndSkipsUnsafeHistory(t *testing.T) {
	source := &sourceFake{
		chats:     []Chat{testChat(7), testChat(8), testChat(9)},
		histories: map[int64]History{7: testHistory(), 8: {WriteAllowed: false}, 9: testHistory()},
	}
	preparer := &preparerFake{
		proposals: map[int64]autochatreply.ProposedResult{7: {Outcome: autochatreply.OutcomeReply, Text: "Да"}, 9: {Outcome: autochatreply.OutcomeReply, Text: "Да"}},
		errs:      map[int64]error{7: errors.New("provider offline")},
	}
	actions := &actionFake{sendResults: map[int64]ActionResult{9: {Outcome: ActionAccepted}}}
	result := runTestService(source, preparer, actions, &auditFake{}, Options{Mode: "auto"})
	if len(actions.sendCalls) != 1 || actions.sendCalls[0] != 9 || len(source.ignored) != 1 || source.ignored[0] != 8 || result.Errors != 1 || result.Skipped != 1 {
		t.Fatalf("result=%+v sends=%v ignored=%v calls=%v", result, actions.sendCalls, source.ignored, preparer.calls)
	}
}

func TestRunHistoryLimitAndCancellationPreventLaterMutation(t *testing.T) {
	tooLong := testHistory()
	tooLong.Messages = make([]autochatreply.HistoryMessage, 20)
	source := &sourceFake{chats: []Chat{testChat(10), testChat(11)}, histories: map[int64]History{10: tooLong, 11: testHistory()}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{11: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &actionFake{sendResults: map[int64]ActionResult{11: {Outcome: ActionAccepted}}}
	result := runTestService(source, preparer, actions, &auditFake{}, Options{Mode: "auto", MaxHistoryMessages: 20})
	if len(actions.sendCalls) != 1 || actions.sendCalls[0] != 11 || result.Skipped != 1 {
		t.Fatalf("limit result=%+v sends=%v ignored=%v", result, actions.sendCalls, source.ignored)
	}

	ctx, cancel := context.WithCancel(context.Background())
	source = &sourceFake{chats: []Chat{testChat(12), testChat(13)}, histories: map[int64]History{12: testHistory(), 13: testHistory()}}
	preparer = &preparerFake{proposals: map[int64]autochatreply.ProposedResult{12: {Outcome: autochatreply.OutcomeReply, Text: "Да"}, 13: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions = &actionFake{sendResults: map[int64]ActionResult{12: {Outcome: ActionAccepted}, 13: {Outcome: ActionAccepted}}}
	cancel()
	_, err := NewService(Dependencies{Chats: source, ReplyPreparer: preparer, Actions: actions}, Options{Mode: "auto"}).Run(ctx, Input{})
	if err != nil || len(actions.sendCalls) != 0 {
		t.Fatalf("cancellation err=%v sends=%v calls=%v", err, actions.sendCalls, preparer.calls)
	}
}
