package candidatelearningorchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/usecase/candidateinterpretation"
)

type fakeClarifications struct {
	values      []candidateacquisition.CandidateClarificationRequest
	answerCalls int
	proposalIDs []string
	markCalls   int
	createCalls int
	listErr     error
	markErr     error
}

func (f *fakeClarifications) Get(id string) (candidateacquisition.CandidateClarificationRequest, error) {
	for _, value := range f.values {
		if value.ID == id {
			return value, nil
		}
	}
	return candidateacquisition.CandidateClarificationRequest{}, errors.New("clarification not found")
}
func (f *fakeClarifications) List() ([]candidateacquisition.CandidateClarificationRequest, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]candidateacquisition.CandidateClarificationRequest(nil), f.values...), nil
}
func (f *fakeClarifications) Create(value candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error) {
	f.createCalls++
	if value.Status == "" {
		value.Status = candidateacquisition.ClarificationPending
	}
	f.values = append(f.values, value)
	return value, nil
}
func (f *fakeClarifications) RecordAnswerEvidence(id string, answer candidateacquisition.CandidateAnswer) error {
	f.answerCalls++
	for i := range f.values {
		if f.values[i].ID == id {
			copy := answer
			f.values[i].Answer = &copy
			return nil
		}
	}
	return errors.New("clarification not found")
}
func (f *fakeClarifications) SetProposalIDs(id string, ids []string) error {
	f.proposalIDs = append([]string(nil), ids...)
	for i := range f.values {
		if f.values[i].ID == id {
			f.values[i].ProposalIDs = append([]string(nil), ids...)
			return nil
		}
	}
	return errors.New("clarification not found")
}
func (f *fakeClarifications) MarkResolved(id string, status candidateacquisition.CandidateClarificationStatus, reason string) error {
	f.markCalls++
	if f.markErr != nil {
		return f.markErr
	}
	for i := range f.values {
		if f.values[i].ID == id {
			f.values[i].Status = status
			f.values[i].ResolutionReason = reason
			return nil
		}
	}
	return errors.New("clarification not found")
}

type fakeCandidateReader struct {
	value candidate.Candidate
	err   error
}

func (r fakeCandidateReader) CurrentCandidate(context.Context) (candidate.Candidate, error) {
	return r.value, r.err
}

type fakeInterpreter struct {
	value candidateinterpretation.Interpretation
	err   error
	calls int
}

func (i *fakeInterpreter) Interpret(context.Context, candidateinterpretation.Input) (candidateinterpretation.Interpretation, error) {
	i.calls++
	return i.value, i.err
}

type fakeMutation struct {
	unknownCalls  int
	choiceCalls   int
	proposalCalls int
	dismissCalls  int
	confirmCalls  int
	rejectCalls   int
	confirmErr    error
	proposalErr   error
}

func (m *fakeMutation) CreateUnknown(context.Context, UnknownInput) (MutationResult, error) {
	m.unknownCalls++
	return MutationResult{QuestionID: "unknown-from-mutation"}, nil
}
func (m *fakeMutation) ApplyChoice(context.Context, ChoiceInput) (MutationResult, error) {
	m.choiceCalls++
	return MutationResult{EntityID: "skill-1"}, nil
}
func (m *fakeMutation) CreateProposal(context.Context, ProposalInput) (MutationResult, error) {
	m.proposalCalls++
	if m.proposalErr != nil {
		return MutationResult{}, m.proposalErr
	}
	return MutationResult{ProposalID: "proposal-1"}, nil
}
func (m *fakeMutation) DismissUnknown(context.Context, DismissInput) error {
	m.dismissCalls++
	return nil
}
func (m *fakeMutation) ConfirmProposal(context.Context, string) (MutationResult, error) {
	m.confirmCalls++
	return MutationResult{ProposalID: "proposal-1"}, m.confirmErr
}
func (m *fakeMutation) RejectProposal(context.Context, string) error { m.rejectCalls++; return nil }

func newTestService(store *fakeClarifications, mutation *fakeMutation, interpreter *fakeInterpreter) *Service {
	return NewService(Dependencies{
		Candidate:      fakeCandidateReader{value: candidate.Candidate{Version: 1, ID: "candidate-1"}},
		Clarifications: store, Mutation: mutation, Interpretation: interpreter,
		GenerateID: func(kind string) (string, error) { return kind + "-new", nil },
	})
}

func pendingClarification() candidateacquisition.CandidateClarificationRequest {
	return candidateacquisition.CandidateClarificationRequest{ID: "clarification-1", UnknownID: "unknown-1", Topic: "Redis", Question: "What did you do with Redis?", OriginalEmployerQuestion: "Работали ли вы с Redis?", GapKey: "gap-1", Status: candidateacquisition.ClarificationPending}
}

func proposalInterpretation() candidateinterpretation.Interpretation {
	return candidateinterpretation.Interpretation{Proposals: []candidateacquisition.CandidateKnowledgeProposalDraft{{Type: "skill_usage", Skill: "Redis", UsageContext: candidate.CanonicalSkillUsagePetProject, Level: candidate.SkillLevelUnknown, TruthStatus: "hypothesis"}}}
}

func TestSubmitAnswerCreatesProposalWithoutCandidateMutation(t *testing.T) {
	store := &fakeClarifications{values: []candidateacquisition.CandidateClarificationRequest{pendingClarification()}}
	mutation := &fakeMutation{}
	interpreter := &fakeInterpreter{value: proposalInterpretation()}
	service := newTestService(store, mutation, interpreter)

	result, err := service.SubmitAnswer(context.Background(), "clarification-1", candidateacquisition.CandidateAnswer{Kind: "free_text", Raw: "В собственном проекте использовал Redis"})
	if err != nil || result.Disposition != "proposal_pending_confirmation" || !reflect.DeepEqual(result.ProposalIDs, []string{"proposal-1"}) {
		t.Fatalf("submit answer: %+v, %v", result, err)
	}
	if interpreter.calls != 1 || mutation.proposalCalls != 1 || mutation.confirmCalls != 0 || mutation.choiceCalls != 0 {
		t.Fatalf("answer crossed confirmation boundary: interpreter=%d proposals=%d confirms=%d choices=%d", interpreter.calls, mutation.proposalCalls, mutation.confirmCalls, mutation.choiceCalls)
	}
}

func TestAmbiguousInterpretationDoesNotMutate(t *testing.T) {
	store := &fakeClarifications{values: []candidateacquisition.CandidateClarificationRequest{pendingClarification()}}
	mutation := &fakeMutation{}
	interpreter := &fakeInterpreter{value: candidateinterpretation.Interpretation{Proposals: nil}}
	service := newTestService(store, mutation, interpreter)

	_, err := service.SubmitAnswer(context.Background(), "clarification-1", candidateacquisition.CandidateAnswer{Raw: "Не знаю"})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.proposalCalls != 0 || mutation.confirmCalls != 0 || store.markCalls != 0 {
		t.Fatalf("ambiguous answer created a side effect: proposals=%d confirms=%d marks=%d", mutation.proposalCalls, mutation.confirmCalls, store.markCalls)
	}
}

func TestConfirmAndRejectUseExplicitMutationBoundary(t *testing.T) {
	store := &fakeClarifications{values: []candidateacquisition.CandidateClarificationRequest{{ID: "clarification-1", ProposalIDs: []string{"proposal-1"}, Status: candidateacquisition.ClarificationAnswered}}}
	mutation := &fakeMutation{}
	service := newTestService(store, mutation, nil)

	if _, err := service.ConfirmProposal(context.Background(), "proposal-1"); err != nil {
		t.Fatal(err)
	}
	if mutation.confirmCalls != 1 || store.markCalls != 1 || store.values[0].Status != candidateacquisition.ClarificationResolvedExistingKnowledge {
		t.Fatalf("confirmation lifecycle not applied: confirms=%d marks=%d status=%s", mutation.confirmCalls, store.markCalls, store.values[0].Status)
	}
	if err := service.RejectProposal(context.Background(), "proposal-2"); err != nil {
		t.Fatal(err)
	}
	if mutation.rejectCalls != 1 || mutation.choiceCalls != 0 || mutation.dismissCalls != 0 {
		t.Fatalf("reject crossed mutation boundary: %+v", mutation)
	}
}

func TestCreateClarificationDeduplicatesPendingGap(t *testing.T) {
	store := &fakeClarifications{}
	mutation := &fakeMutation{}
	service := newTestService(store, mutation, nil)
	value := candidate.Candidate{Version: 1, ID: "candidate-1"}
	refs := candidateacquisition.CandidateKnowledgeGapContext{ConversationID: "conversation-1"}

	first, err := service.CreateClarification(context.Background(), value, "Работали ли вы с Kubernetes?", refs)
	if err != nil || len(first.Requests) == 0 {
		t.Fatalf("first clarification: %+v, %v", first, err)
	}
	second, err := service.CreateClarification(context.Background(), value, "Работали ли вы с Kubernetes?", refs)
	if err != nil || len(second.Requests) != 1 || store.createCalls != 1 || mutation.unknownCalls != 1 {
		t.Fatalf("duplicate clarification was created: %+v, %v; creates=%d unknowns=%d", second, err, store.createCalls, mutation.unknownCalls)
	}
}
