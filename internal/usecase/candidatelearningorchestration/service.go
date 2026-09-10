package candidatelearningorchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidateacquisition"
	"hh-ai-responder/internal/usecase/candidateinterpretation"
)

type Service struct {
	candidate      CandidateReader
	clarifications ClarificationStore
	mutation       Mutation
	interpretation Interpreter
	generateID     IDGenerator
}

func NewService(dependencies Dependencies) *Service {
	generateID := dependencies.GenerateID
	if generateID == nil {
		generateID = randomID
	}
	return &Service{candidate: dependencies.Candidate, clarifications: dependencies.Clarifications, mutation: dependencies.Mutation, interpretation: dependencies.Interpretation, generateID: generateID}
}

func (s *Service) configured() error {
	if s == nil || s.clarifications == nil || s.mutation == nil {
		return errors.New("candidate learning orchestration is not configured")
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("candidate learning context is nil")
	}
	return ctx.Err()
}

func randomID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return kind + "-" + hex.EncodeToString(value[:]), nil
}

func (s *Service) CreateClarification(ctx context.Context, value candidate.Candidate, employerQuestion string, refs candidateacquisition.CandidateKnowledgeGapContext) (CreateClarificationResult, error) {
	if err := s.configured(); err != nil {
		return CreateClarificationResult{}, err
	}
	if err := checkContext(ctx); err != nil {
		return CreateClarificationResult{}, err
	}
	gaps, err := candidateacquisition.DetectCandidateKnowledgeGaps(value, employerQuestion)
	if err != nil {
		return CreateClarificationResult{}, err
	}
	gaps = candidateacquisition.FilterKnownGaps(value, gaps)
	requests := make([]candidateacquisition.CandidateClarificationRequest, 0, len(gaps))
	for _, gap := range gaps {
		if err := checkContext(ctx); err != nil {
			return CreateClarificationResult{}, err
		}
		old, found, err := s.findByGap(gap.DeterministicKey)
		if err != nil {
			return CreateClarificationResult{}, err
		}
		if found && old.Status != candidateacquisition.ClarificationDismissed {
			requests = append(requests, old)
			continue
		}
		request := candidateacquisition.ClarificationForKnowledgeGap(gap, refs)
		request.ID, err = s.generateID("clarification")
		if err != nil {
			return CreateClarificationResult{}, err
		}
		unknown, err := s.mutation.CreateUnknown(ctx, UnknownInput{Candidate: value, Gap: gap, Request: request})
		if err != nil {
			return CreateClarificationResult{}, err
		}
		if unknown.QuestionID != "" {
			request.UnknownID = unknown.QuestionID
		}
		created, err := s.clarifications.Create(request)
		if err != nil {
			return CreateClarificationResult{}, err
		}
		requests = append(requests, created)
	}
	return CreateClarificationResult{Requests: requests}, nil
}

func (s *Service) findByGap(key string) (candidateacquisition.CandidateClarificationRequest, bool, error) {
	values, err := s.clarifications.List()
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, err
	}
	for _, value := range values {
		if value.GapKey == key {
			return value, true, nil
		}
	}
	return candidateacquisition.CandidateClarificationRequest{}, false, nil
}

func (s *Service) SubmitAnswer(ctx context.Context, id string, answer candidateacquisition.CandidateAnswer) (AnswerResult, error) {
	if err := s.configured(); err != nil {
		return AnswerResult{}, err
	}
	if err := checkContext(ctx); err != nil {
		return AnswerResult{}, err
	}
	request, err := s.clarifications.Get(id)
	if err != nil {
		return AnswerResult{}, err
	}
	if !request.IsPending() {
		return AnswerResult{}, errors.New("clarification is already resolved")
	}
	if answer.ReceivedAt.IsZero() {
		answer.ReceivedAt = time.Now().UTC()
	}
	if answer.Kind == "" {
		answer.Kind = "free_text"
	}
	if err := candidateacquisition.ValidateAnswer(request, answer); err != nil {
		return AnswerResult{}, err
	}
	classification, err := candidateacquisition.ClassifyAnswer(request, answer)
	if err != nil {
		return AnswerResult{}, err
	}
	if err := s.clarifications.RecordAnswerEvidence(id, answer); err != nil {
		return AnswerResult{}, err
	}
	result := AnswerResult{ClarificationID: id, UnknownID: request.UnknownID, Status: candidateacquisition.ClarificationPending, ProposalIDs: []string{}}
	if classification.Kind == candidateacquisition.AnswerNeedsClarification {
		result.Disposition = "unknown"
		return result, nil
	}
	current, err := s.currentCandidate(ctx)
	if err != nil {
		return AnswerResult{}, err
	}
	if classification.Kind == candidateacquisition.AnswerDismisses {
		if err := s.mutation.DismissUnknown(ctx, DismissInput{Request: request, Evidence: answer.Raw}); err != nil {
			return AnswerResult{}, err
		}
		if err := s.clarifications.MarkResolved(id, candidateacquisition.ClarificationDismissed, "candidate chose not to save this knowledge"); err != nil {
			return AnswerResult{}, err
		}
		result.Status, result.Disposition = candidateacquisition.ClarificationDismissed, "dismissed"
		return result, nil
	}
	if classification.Kind == candidateacquisition.AnswerConfirmsChoice {
		mutation, err := s.mutation.ApplyChoice(ctx, ChoiceInput{Candidate: current, Request: request, Answer: answer})
		if err != nil {
			return AnswerResult{}, err
		}
		if err := s.clarifications.MarkResolved(id, candidateacquisition.ClarificationResolvedExistingKnowledge, "explicit structured candidate choice"); err != nil {
			return AnswerResult{}, err
		}
		result.Mutation, result.Status, result.Disposition = mutation, candidateacquisition.ClarificationResolvedExistingKnowledge, "confirmed_choice"
		return result, nil
	}
	if s.interpretation == nil {
		return result, errors.New("free-text interpretation is not configured")
	}
	gap := answerGap(current, request)
	interpretation, err := s.interpretation.Interpret(ctx, candidateinterpretation.Input{Gap: gap, Answer: answer.Raw, KnownContext: knownContext(current)})
	if err != nil {
		return result, err
	}
	if _, err := candidateacquisition.DecideAnswer(candidateacquisition.AnswerInput{Request: request, Answer: answer, Origin: candidateacquisition.OriginAIInterpretation, Interpretation: (*candidateacquisition.CandidateKnowledgeInterpretation)(&interpretation)}); err != nil {
		return result, err
	}
	if len(interpretation.Proposals) == 0 {
		return result, candidateacquisition.ErrKnowledgeAnswerMismatch
	}
	for _, proposal := range interpretation.Proposals {
		if err := checkContext(ctx); err != nil {
			return AnswerResult{}, err
		}
		mutation, err := s.mutation.CreateProposal(ctx, ProposalInput{Candidate: current, Request: request, Answer: answer, Proposal: proposal})
		if err != nil {
			return AnswerResult{}, err
		}
		if mutation.ProposalID != "" {
			result.ProposalIDs = append(result.ProposalIDs, mutation.ProposalID)
			result.Mutation = mutation
		}
	}
	if len(result.ProposalIDs) == 0 {
		return result, candidateacquisition.ErrKnowledgeAnswerMismatch
	}
	if err := s.clarifications.SetProposalIDs(id, result.ProposalIDs); err != nil {
		return AnswerResult{}, err
	}
	result.Status, result.Disposition = candidateacquisition.ClarificationAnswered, "proposal_pending_confirmation"
	return result, nil
}

func (s *Service) currentCandidate(ctx context.Context) (candidate.Candidate, error) {
	if s.candidate == nil {
		return candidate.Candidate{}, errors.New("candidate reader is not configured")
	}
	return s.candidate.CurrentCandidate(ctx)
}

func answerGap(value candidate.Candidate, request candidateacquisition.CandidateClarificationRequest) candidateacquisition.CandidateKnowledgeGap {
	gap := candidateacquisition.CandidateKnowledgeGap{CandidateID: value.ID, Subject: request.Topic, SubjectType: candidateacquisition.KnowledgeSubjectSkill, Field: "usage_context", Context: request.OriginalEmployerQuestion, DeterministicKey: request.GapKey}
	if request.Category == candidateacquisition.ClarificationBehavioralStory {
		gap.SubjectType, gap.Field = candidateacquisition.KnowledgeSubjectStory, "customer_conflict"
	}
	return gap
}

func knownContext(value candidate.Candidate) candidateinterpretation.KnownContext {
	result := candidateinterpretation.KnownContext{}
	for _, item := range value.Experience {
		result.ExperienceIDs = append(result.ExperienceIDs, item.ID)
	}
	for _, item := range value.Projects {
		result.ProjectIDs = append(result.ProjectIDs, item.ID)
	}
	return result
}

func (s *Service) ConfirmProposal(ctx context.Context, proposalID string) (MutationResult, error) {
	if err := s.configured(); err != nil {
		return MutationResult{}, err
	}
	if err := checkContext(ctx); err != nil {
		return MutationResult{}, err
	}
	result, err := s.mutation.ConfirmProposal(ctx, proposalID)
	if err != nil {
		return MutationResult{}, err
	}
	requests, err := s.clarifications.List()
	if err != nil {
		return MutationResult{}, err
	}
	for _, request := range requests {
		for _, id := range request.ProposalIDs {
			if id == proposalID {
				if err := s.clarifications.MarkResolved(request.ID, candidateacquisition.ClarificationResolvedExistingKnowledge, "candidate confirmed interpretation"); err != nil {
					return MutationResult{}, err
				}
				return result, nil
			}
		}
	}
	return result, nil
}

func (s *Service) RejectProposal(ctx context.Context, proposalID string) error {
	if err := s.configured(); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	return s.mutation.RejectProposal(ctx, proposalID)
}

func (s *Service) DismissClarification(ctx context.Context, id, evidence string) (AnswerResult, error) {
	if err := s.configured(); err != nil {
		return AnswerResult{}, err
	}
	if err := checkContext(ctx); err != nil {
		return AnswerResult{}, err
	}
	request, err := s.clarifications.Get(id)
	if err != nil {
		return AnswerResult{}, err
	}
	if !request.IsPending() {
		return AnswerResult{}, errors.New("clarification is already resolved")
	}
	if strings.TrimSpace(evidence) == "" {
		evidence = "candidate chose not to save this knowledge"
	}
	if err := s.mutation.DismissUnknown(ctx, DismissInput{Request: request, Evidence: evidence}); err != nil {
		return AnswerResult{}, err
	}
	if err := s.clarifications.MarkResolved(id, candidateacquisition.ClarificationDismissed, evidence); err != nil {
		return AnswerResult{}, err
	}
	return AnswerResult{ClarificationID: id, UnknownID: request.UnknownID, Status: candidateacquisition.ClarificationDismissed, Disposition: "dismissed"}, nil
}
