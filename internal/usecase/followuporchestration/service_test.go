package followuporchestration

import (
	"context"
	"testing"
	"time"

	"hh-ai-responder/internal/usecase/employerreply"
	"hh-ai-responder/internal/usecase/followupdraft"
)

type followupLoader struct {
	initial LoadedSnapshot
	fresh   LoadedSnapshot
	loads   int
	reloads int
}

func (f *followupLoader) Load(context.Context, string, time.Time) (LoadedSnapshot, error) {
	f.loads++
	return f.initial, nil
}

func (f *followupLoader) Reload(context.Context, string, time.Time) (LoadedSnapshot, error) {
	f.reloads++
	return f.fresh, nil
}

type followupEvaluator struct {
	initial followupdraft.Eligibility
	fresh   followupdraft.Eligibility
	calls   int
}

func (f *followupEvaluator) Evaluate(value LoadedSnapshot, _ time.Time) followupdraft.Eligibility {
	f.calls++
	if value.Fingerprint == "fresh" {
		return f.fresh
	}
	return f.initial
}

type followupPreparer struct {
	result followupdraft.Result
	calls  int
	ctx    context.Context
}

func (f *followupPreparer) Prepare(ctx context.Context, _ followupdraft.Input) (followupdraft.Result, error) {
	f.calls++
	f.ctx = ctx
	return f.result, nil
}

type followupDrafts struct {
	saved *Draft
	finds int
	saves int
}

func (f *followupDrafts) FindReusable(context.Context, string, string, employerreply.Context) (Draft, bool, error) {
	f.finds++
	if f.saved == nil {
		return Draft{}, false, nil
	}
	return *f.saved, true, nil
}

func (f *followupDrafts) Save(_ context.Context, value Draft) error {
	f.saves++
	f.saved = &value
	return nil
}

type followupClarifications struct{ calls int }

func (f *followupClarifications) Persist(context.Context, ClarificationInput) error {
	f.calls++
	return nil
}

func eligible() followupdraft.Eligibility {
	return followupdraft.Eligibility{ApplicationID: "a", ConversationID: "c", Status: followupdraft.EligibilityStatusEligible, Reason: "ready"}
}

func snapshot(fingerprint string) LoadedSnapshot {
	return LoadedSnapshot{ApplicationID: "a", ConversationID: "c", Fingerprint: fingerprint, Generation: followupdraft.Input{Context: employerreply.Context{}}}
}

func TestServicePrepareDoesNotCallProviderWhenIneligible(t *testing.T) {
	loader := &followupLoader{initial: snapshot("initial")}
	evaluator := &followupEvaluator{initial: followupdraft.Eligibility{Status: "too_early", Reason: "wait"}}
	preparer := &followupPreparer{}
	service := NewService(Dependencies{Snapshots: loader, Eligibility: evaluator, FollowUp: preparer})
	result, err := service.Prepare(context.Background(), "a", time.Now())
	if err != nil || result.Decision.Action != employerreply.ActionManualReview || preparer.calls != 0 {
		t.Fatalf("result=%+v err=%v providerCalls=%d", result, err, preparer.calls)
	}
}

func TestServicePrepareReusesMatchingDraftWithoutProvider(t *testing.T) {
	loader := &followupLoader{initial: snapshot("initial")}
	evaluator := &followupEvaluator{initial: eligible()}
	preparer := &followupPreparer{}
	drafts := &followupDrafts{saved: &Draft{Text: "saved", DecisionReason: "existing"}}
	service := NewService(Dependencies{Snapshots: loader, Eligibility: evaluator, FollowUp: preparer, Drafts: drafts})
	result, err := service.Prepare(context.Background(), "a", time.Now())
	if err != nil || !result.Reused || result.Outcome != OutcomeReused || result.Decision.Draft != "saved" || preparer.calls != 0 {
		t.Fatalf("result=%+v err=%v providerCalls=%d", result, err, preparer.calls)
	}
}

func TestServicePrepareFreshInvalidationDoesNotPersistOrRegenerate(t *testing.T) {
	loader := &followupLoader{initial: snapshot("initial"), fresh: snapshot("fresh")}
	evaluator := &followupEvaluator{initial: eligible(), fresh: followupdraft.Eligibility{Status: "candidate_action_required", Reason: "candidate replied"}}
	preparer := &followupPreparer{result: followupdraft.Result{Decision: employerreply.Decision{Action: employerreply.ActionDraftReply, Draft: "proposal"}}}
	drafts := &followupDrafts{}
	service := NewService(Dependencies{Snapshots: loader, Eligibility: evaluator, FollowUp: preparer, Drafts: drafts})
	result, err := service.Prepare(context.Background(), "a", time.Now())
	if err != nil || result.Decision.Action != employerreply.ActionManualReview || preparer.calls != 1 || loader.reloads != 1 || drafts.saves != 0 {
		t.Fatalf("stale result=%+v err=%v provider=%d reloads=%d saves=%d", result, err, preparer.calls, loader.reloads, drafts.saves)
	}
}

func TestServicePreparePersistsFreshEligibleDraft(t *testing.T) {
	loader := &followupLoader{initial: snapshot("initial"), fresh: snapshot("fresh")}
	evaluator := &followupEvaluator{initial: eligible(), fresh: eligible()}
	preparer := &followupPreparer{result: followupdraft.Result{Decision: employerreply.Decision{Action: employerreply.ActionDraftReply, Draft: "proposal"}}}
	drafts := &followupDrafts{}
	ctx := context.WithValue(context.Background(), struct{}{}, "caller")
	service := NewService(Dependencies{Snapshots: loader, Eligibility: evaluator, FollowUp: preparer, Drafts: drafts})
	result, err := service.Prepare(ctx, "a", time.Now())
	if err != nil || result.Outcome != OutcomeDraftReady || preparer.calls != 1 || preparer.ctx != ctx || drafts.saves != 1 || drafts.saved.InputFingerprint != "fresh" {
		t.Fatalf("result=%+v err=%v provider=%d saves=%d", result, err, preparer.calls, drafts.saves)
	}
}
