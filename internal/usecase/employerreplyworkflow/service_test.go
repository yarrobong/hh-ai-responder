package employerreplyworkflow

import (
	"context"
	"errors"
	"testing"

	"hh-ai-responder/internal/usecase/conversationpolicy"
	"hh-ai-responder/internal/usecase/employerreply"
)

type workflowLoader struct {
	value LoadedContext
	loads int
}

func (f *workflowLoader) Load(_ context.Context, _ string) (LoadedContext, error) {
	f.loads++
	return f.value, nil
}

type workflowReply struct {
	decision employerreply.Decision
	calls    int
	ctx      context.Context
}

func (f *workflowReply) Prepare(ctx context.Context, _ employerreply.Input) (employerreply.Decision, error) {
	f.calls++
	f.ctx = ctx
	return f.decision, nil
}

type workflowDrafts struct {
	saved *Draft
	finds int
	saves int
}

func (f *workflowDrafts) FindReusable(context.Context, string, string) (Draft, bool, error) {
	f.finds++
	if f.saved == nil {
		return Draft{}, false, nil
	}
	return *f.saved, true, nil
}

func (f *workflowDrafts) Save(_ context.Context, value Draft) error {
	f.saves++
	f.saved = &value
	return nil
}

type workflowClarifications struct{ calls int }

func (f *workflowClarifications) Persist(context.Context, ClarificationInput) error {
	f.calls++
	return nil
}

func TestServicePreparePersistsDraftAndPropagatesContext(t *testing.T) {
	loader := &workflowLoader{value: LoadedContext{ConversationID: "conversation-1", Input: employerreply.Input{Context: employerreply.Context{ReplyRequirement: conversationpolicy.ReplyRequired}}, InputFingerprint: "fingerprint"}}
	reply := &workflowReply{decision: employerreply.Decision{Action: employerreply.ActionDraftReply, Draft: "draft", Reason: "safe", UsedFacts: []string{"Python"}}}
	drafts := &workflowDrafts{}
	service := NewService(Dependencies{Conversations: loader, Reply: reply, Drafts: drafts}, Options{PromptVersion: "test-v1"})
	ctx := context.WithValue(context.Background(), struct{}{}, "caller")
	result, err := service.Prepare(ctx, Input{ConversationID: "conversation-1", Task: "reply"})
	if err != nil || result.Decision.Action != employerreply.ActionDraftReply || result.Outcome != OutcomeDraftReady {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if reply.calls != 1 || reply.ctx != ctx || drafts.saves != 1 || drafts.saved.InputFingerprint != "fingerprint" {
		t.Fatalf("calls/context/persistence not preserved: calls=%d sameContext=%t drafts=%d", reply.calls, reply.ctx == ctx, drafts.saves)
	}
}

func TestServicePrepareReusesWithoutCallingLeaf(t *testing.T) {
	loader := &workflowLoader{value: LoadedContext{ConversationID: "conversation-1", Input: employerreply.Input{Context: employerreply.Context{ReplyRequirement: conversationpolicy.ReplyRequired}}, InputFingerprint: "fingerprint"}}
	reply := &workflowReply{decision: employerreply.Decision{Action: employerreply.ActionDraftReply}}
	drafts := &workflowDrafts{saved: &Draft{Text: "saved", DecisionReason: "existing"}}
	service := NewService(Dependencies{Conversations: loader, Reply: reply, Drafts: drafts}, Options{})
	result, err := service.Prepare(context.Background(), Input{ConversationID: "conversation-1"})
	if err != nil || !result.Reused || result.Outcome != OutcomeReused || result.Decision.Draft != "saved" {
		t.Fatalf("reuse result=%+v err=%v", result, err)
	}
	if reply.calls != 0 || drafts.saves != 0 {
		t.Fatalf("reused draft caused side effects: calls=%d saves=%d", reply.calls, drafts.saves)
	}
}

func TestServicePreparePersistsCandidateClarification(t *testing.T) {
	loader := &workflowLoader{value: LoadedContext{ConversationID: "conversation-1", Input: employerreply.Input{Context: employerreply.Context{ReplyRequirement: conversationpolicy.ReplyRequired}}, InputFingerprint: "fingerprint"}}
	reply := &workflowReply{decision: employerreply.Decision{Action: employerreply.ActionNeedCandidate, Reason: "unknown", MissingInformation: []employerreply.MissingInformation{{Topic: "kubernetes", Question: "Есть опыт Kubernetes?"}}}}
	clarifications := &workflowClarifications{}
	drafts := &workflowDrafts{}
	service := NewService(Dependencies{Conversations: loader, Reply: reply, Drafts: drafts, Clarifications: clarifications}, Options{})
	result, err := service.Prepare(context.Background(), Input{ConversationID: "conversation-1"})
	if err != nil || result.Outcome != OutcomeNeedsCandidate || clarifications.calls != 1 || drafts.saves != 0 {
		t.Fatalf("clarification result=%+v err=%v calls=%d saves=%d", result, err, clarifications.calls, drafts.saves)
	}
}

func TestServiceRequiresDependencies(t *testing.T) {
	_, err := NewService(Dependencies{}, Options{}).Prepare(context.Background(), Input{ConversationID: "c"})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("expected configuration error, got %v", err)
	}
}
