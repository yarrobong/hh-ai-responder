package autochatorchestration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
	autochatattemptport "hh-ai-responder/internal/ports/autochatattempt"
	"hh-ai-responder/internal/usecase/autochatreply"
)

type durableAttemptStoreFake struct {
	mu         sync.Mutex
	attempts   map[string]domain.Attempt
	recordErr  error
	reserveCnt int
}

func (f *durableAttemptStoreFake) FindBlockingForTrigger(_ context.Context, conversationID, triggerID string) (domain.Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, value := range f.attempts {
		if value.ConversationID == conversationID && value.TriggerMessageID == triggerID && domain.IsBlocking(value.State) {
			return value, nil
		}
	}
	return domain.Attempt{}, domain.ErrAttemptNotFound
}

func (f *durableAttemptStoreFake) Reserve(_ context.Context, value domain.Attempt) (autochatattemptport.ReserveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reserveCnt++
	for _, existing := range f.attempts {
		if domain.ConflictKey(existing.ConversationID, existing.TriggerMessageID) == domain.ConflictKey(value.ConversationID, value.TriggerMessageID) && domain.IsBlocking(existing.State) {
			copy := existing
			return autochatattemptport.ReserveResult{Existing: &copy}, domain.ErrTriggerBlocked
		}
	}
	if f.attempts == nil {
		f.attempts = map[string]domain.Attempt{}
	}
	f.attempts[value.AttemptID] = value
	return autochatattemptport.ReserveResult{Reserved: true, Attempt: value}, nil
}

func (f *durableAttemptStoreFake) RecordOutcome(_ context.Context, id string, state domain.State, at time.Time, providerID string, providerStatus int, errorClass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	value, ok := f.attempts[id]
	if !ok {
		return domain.ErrAttemptNotFound
	}
	updated, err := value.WithOutcome(state, at, providerID, providerStatus, errorClass)
	if err != nil {
		return err
	}
	f.attempts[id] = updated
	return nil
}

type durableActionFake struct {
	mu          sync.Mutex
	sendCalls   int
	leaveCalls  int
	sendResult  ActionResult
	leaveResult ActionResult
	sendErr     error
	leaveErr    error
}

func (f *durableActionFake) SendChatMessage(context.Context, int64, string) (ActionResult, error) {
	return ActionResult{Outcome: ActionNoOp}, nil
}
func (f *durableActionFake) LeaveChat(context.Context, int64) (ActionResult, error) {
	return ActionResult{Outcome: ActionNoOp}, nil
}
func (f *durableActionFake) SendChatMessageForAttempt(context.Context, ActionRequest, string) (ActionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls++
	return f.sendResult, f.sendErr
}
func (f *durableActionFake) LeaveChatForAttempt(context.Context, ActionRequest) (ActionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaveCalls++
	return f.leaveResult, f.leaveErr
}

func durableChat(trigger string) Chat {
	value := testChat(101)
	value.TriggerMessageID = trigger
	return value
}

func durableHistory(trigger string) History {
	value := testHistory()
	value.TriggerMessageID = trigger
	value.Messages[0].ID = trigger
	return value
}

func durableService(source *sourceFake, preparer *preparerFake, actions *durableActionFake, store *durableAttemptStoreFake, options Options) *Service {
	if options.Mode == "" {
		options.Mode = "auto"
	}
	options.DurableAttempts, options.WriteEnabled = true, true
	if options.Now == nil {
		options.Now = func() time.Time { return time.Unix(200, 0).UTC() }
	}
	return NewService(Dependencies{Chats: source, ReplyPreparer: preparer, Actions: actions, Attempts: store}, options)
}

func TestDurableAutoChatBlockingRecordStopsAIAndWrite(t *testing.T) {
	store := &durableAttemptStoreFake{attempts: map[string]domain.Attempt{"old": validDurableAttempt(domain.StateAccepted, "message-1")}}
	chat := durableChat("message-1")
	source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-1")}}
	preparer, actions := &preparerFake{}, &durableActionFake{}
	result, err := durableService(source, preparer, actions, store, Options{}).Run(context.Background(), Input{})
	if err != nil || len(preparer.calls) != 0 || actions.sendCalls != 0 || actions.leaveCalls != 0 || result.Skipped != 1 {
		t.Fatalf("blocked result=%+v prepare=%v actions=%+v err=%v", result, preparer.calls, actions, err)
	}
}

func TestDurableAutoChatNewTriggerIsIndependent(t *testing.T) {
	store := &durableAttemptStoreFake{attempts: map[string]domain.Attempt{"old": validDurableAttempt(domain.StateAccepted, "message-1")}}
	chat := durableChat("message-2")
	source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-2")}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{chat.ID: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &durableActionFake{sendResult: ActionResult{Outcome: ActionAccepted, ProviderID: "out-2"}}
	result, err := durableService(source, preparer, actions, store, Options{}).Run(context.Background(), Input{})
	if err != nil || len(preparer.calls) != 1 || actions.sendCalls != 1 || result.Replied != 1 {
		t.Fatalf("new trigger result=%+v prepare=%v actions=%+v err=%v", result, preparer.calls, actions, err)
	}
}

func TestDurableAutoChatAmbiguousAndCrashResidueBlockReplay(t *testing.T) {
	for name, initial := range map[string]domain.Attempt{
		"ambiguous": {},
		"crash":     validDurableAttempt(domain.StateSending, "message-1"),
	} {
		t.Run(name, func(t *testing.T) {
			store := &durableAttemptStoreFake{}
			if initial.AttemptID != "" {
				store.attempts = map[string]domain.Attempt{"old": initial}
			}
			chat := durableChat("message-1")
			source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-1")}}
			preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{chat.ID: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
			actions := &durableActionFake{sendResult: ActionResult{Outcome: ActionDeliveryUncertain}, sendErr: errors.New("timeout")}
			service := durableService(source, preparer, actions, store, Options{})
			if initial.AttemptID != "" {
				if _, err := service.Run(context.Background(), Input{}); err != nil || len(preparer.calls) != 0 || actions.sendCalls != 0 {
					t.Fatalf("crash residue was replayed: prepare=%v actions=%d err=%v", preparer.calls, actions.sendCalls, err)
				}
				return
			}
			if _, err := service.Run(context.Background(), Input{}); err != nil || actions.sendCalls != 1 {
				t.Fatalf("ambiguous first run: actions=%d err=%v", actions.sendCalls, err)
			}
			if _, err := service.Run(context.Background(), Input{}); err != nil || len(preparer.calls) != 1 || actions.sendCalls != 1 {
				t.Fatalf("ambiguous replay: prepare=%v actions=%d err=%v", preparer.calls, actions.sendCalls, err)
			}
		})
	}
}

func TestDurableAutoChatOutcomePersistenceFailureLeavesBlockingResidue(t *testing.T) {
	store := &durableAttemptStoreFake{recordErr: errors.New("store unavailable")}
	chat := durableChat("message-1")
	source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-1")}}
	preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{chat.ID: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
	actions := &durableActionFake{sendResult: ActionResult{Outcome: ActionAccepted}}
	service := durableService(source, preparer, actions, store, Options{})
	if _, err := service.Run(context.Background(), Input{}); err != nil || actions.sendCalls != 1 {
		t.Fatalf("first persistence-failure run: actions=%d err=%v", actions.sendCalls, err)
	}
	if _, err := service.Run(context.Background(), Input{}); err != nil || len(preparer.calls) != 1 || actions.sendCalls != 1 {
		t.Fatalf("persistence-failure replay: prepare=%v actions=%d err=%v", preparer.calls, actions.sendCalls, err)
	}
}

func TestDurableAutoChatModesNeverReserveWhenNotLive(t *testing.T) {
	for _, options := range []Options{{DryRun: true}, {Mode: "review"}, {WriteEnabled: false}} {
		store := &durableAttemptStoreFake{}
		chat := durableChat("message-1")
		source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-1")}}
		preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{chat.ID: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
		actions := &durableActionFake{}
		service := durableService(source, preparer, actions, store, options)
		if (options.Mode == "" || options.Mode == "auto") && !options.DryRun && options.WriteEnabled == false {
			service.opts.WriteEnabled = false
		}
		if _, err := service.Run(context.Background(), Input{}); err != nil || store.reserveCnt != 0 || actions.sendCalls != 0 || actions.leaveCalls != 0 {
			t.Fatalf("options=%+v reserve=%d actions=%+v err=%v", options, store.reserveCnt, actions, err)
		}
	}
}

func TestDurableAutoChatMissingTriggerFailsClosedBeforeAI(t *testing.T) {
	chat := durableChat("")
	source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: {WriteAllowed: true, Messages: []autochatreply.HistoryMessage{{Text: "question"}}}}}
	preparer, actions, store := &preparerFake{}, &durableActionFake{}, &durableAttemptStoreFake{}
	if _, err := durableService(source, preparer, actions, store, Options{}).Run(context.Background(), Input{}); err != nil || len(preparer.calls) != 0 || actions.sendCalls != 0 {
		t.Fatalf("missing trigger was not fail-closed: prepare=%v actions=%d err=%v", preparer.calls, actions.sendCalls, err)
	}
}

func TestDurableAutoChatConcurrentWorkersHaveAtMostOneWriter(t *testing.T) {
	store := &durableAttemptStoreFake{}
	actions := &durableActionFake{sendResult: ActionResult{Outcome: ActionAccepted}}
	makeService := func() *Service {
		chat := durableChat("message-1")
		source := &sourceFake{chats: []Chat{chat}, histories: map[int64]History{chat.ID: durableHistory("message-1")}}
		preparer := &preparerFake{proposals: map[int64]autochatreply.ProposedResult{chat.ID: {Outcome: autochatreply.OutcomeReply, Text: "Да"}}}
		return durableService(source, preparer, actions, store, Options{})
	}
	services := []*Service{makeService(), makeService()}
	var wg sync.WaitGroup
	for _, service := range services {
		wg.Add(1)
		go func(service *Service) {
			defer wg.Done()
			_, _ = service.Run(context.Background(), Input{})
		}(service)
	}
	wg.Wait()
	if actions.sendCalls > 1 {
		t.Fatalf("concurrent workers sent %d times", actions.sendCalls)
	}
}

func TestDurableAutoChatAmbiguousLeaveBlocksReplay(t *testing.T) {
	store := &durableAttemptStoreFake{}
	chat := durableChat("message-leave")
	chat.IsDiscard = true
	source := &sourceFake{chats: []Chat{chat}}
	actions := &durableActionFake{leaveResult: ActionResult{Outcome: ActionDeliveryUncertain}, leaveErr: errors.New("leave timeout")}
	service := durableService(source, &preparerFake{}, actions, store, Options{})
	if _, err := service.Run(context.Background(), Input{}); err != nil || actions.leaveCalls != 1 {
		t.Fatalf("ambiguous leave: calls=%d err=%v", actions.leaveCalls, err)
	}
	if _, err := service.Run(context.Background(), Input{}); err != nil || actions.leaveCalls != 1 {
		t.Fatalf("ambiguous leave replayed: calls=%d err=%v", actions.leaveCalls, err)
	}
}

func validDurableAttempt(state domain.State, trigger string) domain.Attempt {
	now := time.Unix(100, 0).UTC()
	return domain.Attempt{AttemptID: "attempt-" + trigger, ConversationID: "101", TriggerMessageID: trigger, ActionType: domain.ActionReply, State: state, CreatedAt: now, UpdatedAt: now, RequestKey: "attempt-" + trigger}
}
