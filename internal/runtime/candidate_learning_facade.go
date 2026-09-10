package runtime

import (
	"context"
	"errors"

	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/candidateinterpretation"
	candidatelearningorchestration "hh-ai-responder/internal/usecase/candidatelearningorchestration"
)

// CandidateKnowledgeAcquisitionService remains the root compatibility façade.
// The lifecycle algorithm is owned by candidatelearningorchestration; these
// fields and methods preserve the legacy root API used by conversation, CLI,
// and dashboard adapters.
type CandidateKnowledgeAcquisitionService struct {
	Resolver       *CandidateContextResolver
	Mutations      CandidateKnowledgeMutationWriter
	Clarifications ports.CandidateAcquisitionStore
	Extractor      CandidateKnowledgeProposalExtractor
	Stories        []CandidateStory
	orchestration  *candidatelearningorchestration.Service
	reader         *rootCandidateLearningReader
}

type CandidateKnowledgeProposalExtractor interface {
	InterpretCandidateAnswer(gap CandidateKnowledgeGap, raw string, candidate Candidate) (CandidateKnowledgeInterpretation, error)
}

type contextualCandidateKnowledgeProposalExtractor interface {
	InterpretCandidateAnswerContext(context.Context, CandidateKnowledgeGap, string, Candidate) (CandidateKnowledgeInterpretation, error)
}

type CandidateAnswerResult struct {
	ClarificationID string                       `json:"clarification_id"`
	UnknownID       string                       `json:"unknown_id"`
	ProposalIDs     []string                     `json:"proposal_ids,omitempty"`
	Status          CandidateClarificationStatus `json:"status"`
	Disposition     string                       `json:"disposition"`
	Mutation        CandidateMutationResult      `json:"mutation"`
}

type rootCandidateLearningReader struct {
	resolver *CandidateContextResolver
	stories  []CandidateStory
}

func (r *rootCandidateLearningReader) CurrentCandidate(ctx context.Context) (domaincandidate.Candidate, error) {
	if ctx == nil {
		return domaincandidate.Candidate{}, errors.New("candidate context is nil")
	}
	if err := ctx.Err(); err != nil {
		return domaincandidate.Candidate{}, err
	}
	if r == nil || r.resolver == nil {
		return domaincandidate.Candidate{}, errors.New("candidate resolver is not configured")
	}
	if r.resolver.kb != nil {
		knowledge := *r.resolver.kb
		return NewCanonicalCandidateProvider(CanonicalCandidateInput{Profile: knowledge.Profile, Knowledge: knowledge, Stories: append([]CandidateStory{}, r.stories...), CandidateID: "candidate-local"}).Candidate()
	}
	candidate, _, err := r.resolver.canonicalCandidate()
	return candidate, err
}

// rootCandidateLearningInterpreterWithCandidate preserves the full snapshot
// expected by the legacy extractor while keeping it outside orchestration.
type rootCandidateLearningInterpreterWithCandidate struct {
	extractor CandidateKnowledgeProposalExtractor
	reader    candidatelearningorchestration.CandidateReader
}

func (i rootCandidateLearningInterpreterWithCandidate) Interpret(ctx context.Context, input candidateinterpretation.Input) (candidateinterpretation.Interpretation, error) {
	if i.extractor == nil {
		return candidateinterpretation.Interpretation{}, errors.New("free-text interpretation is not configured")
	}
	value, err := i.reader.CurrentCandidate(ctx)
	if err != nil {
		return candidateinterpretation.Interpretation{}, err
	}
	if contextual, ok := i.extractor.(contextualCandidateKnowledgeProposalExtractor); ok {
		return contextual.InterpretCandidateAnswerContext(ctx, input.Gap, input.Answer, value)
	}
	return i.extractor.InterpretCandidateAnswer(input.Gap, input.Answer, value)
}

type rootCandidateLearningMutation struct {
	mutations CandidateKnowledgeMutationWriter
}

func rootMutationResult(result CandidateMutationResult) candidatelearningorchestration.MutationResult {
	return candidatelearningorchestration.MutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID, SemanticIndexWarning: result.SemanticIndexWarning}
}

func (m rootCandidateLearningMutation) CreateUnknown(ctx context.Context, input candidatelearningorchestration.UnknownInput) (candidatelearningorchestration.MutationResult, error) {
	if err := ctx.Err(); err != nil {
		return candidatelearningorchestration.MutationResult{}, err
	}
	request, gap := input.Request, input.Gap
	unknown := CandidateUnknown{ID: request.UnknownID, Question: request.Question, RelatedEntity: firstNonEmpty(gap.SubjectID, gap.Subject), Status: CandidateUnknownNeedsConfirmation, GapKey: gap.DeterministicKey, Source: KnowledgeSourceEmployerConversation, ConversationID: request.ConversationID, ApplicationID: request.ApplicationID, VacancyID: request.VacancyID, EmployerMessageID: request.EmployerMessageID}
	update := KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceEmployerConversation, Evidence: []string{"employer question created a knowledge gap"}}, Reason: "Employer question requires candidate input", UnknownID: request.UnknownID, ClarificationID: request.ID, ConversationID: request.ConversationID, EmployerMessageID: request.EmployerMessageID}
	result, err := m.mutations.UpdateUnknown(unknown, update)
	return rootMutationResult(CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}), err
}

func (m rootCandidateLearningMutation) ApplyChoice(ctx context.Context, input candidatelearningorchestration.ChoiceInput) (candidatelearningorchestration.MutationResult, error) {
	command := CandidateAcquisitionChoiceCommand{Actor: KnowledgeActorUser, Candidate: input.Candidate, UnknownID: input.Request.UnknownID, ClarificationID: input.Request.ID, Answer: input.Answer, Topic: input.Request.Topic, ConversationID: input.Request.ConversationID, EmployerMessageID: input.Request.EmployerMessageID}
	if mutation, ok := m.mutations.(*CandidateMutationService); ok && mutation != nil {
		result, err := mutation.ApplyAcquisitionChoice(ctx, command)
		return rootMutationResult(result), err
	}
	if updater, ok := m.mutations.(*CandidateKnowledgeUpdater); ok && updater != nil {
		result, err := updater.ApplyAcquisitionChoice(command)
		return rootMutationResult(CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}), err
	}
	return candidatelearningorchestration.MutationResult{}, errors.New("structured acquisition requires the candidate mutation service")
}

func (m rootCandidateLearningMutation) CreateProposal(ctx context.Context, input candidatelearningorchestration.ProposalInput) (candidatelearningorchestration.MutationResult, error) {
	if err := ctx.Err(); err != nil {
		return candidatelearningorchestration.MutationResult{}, err
	}
	request, proposal, value := input.Request, input.Proposal, input.Candidate
	update := KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceCandidateInterview, Evidence: []string{"candidate answer retained as raw evidence"}}, Reason: "AI interpretation of candidate answer; explicit candidate confirmation required", UnknownID: request.UnknownID, ClarificationID: request.ID, ConversationID: request.ConversationID, EmployerMessageID: request.EmployerMessageID}
	mutationService, postgres := m.mutations.(*CandidateMutationService)
	switch proposal.Type {
	case "skill_usage":
		fact := CandidateSkillDetailed{Name: proposal.Skill, Level: proposal.Level, Uses: []CanonicalSkillUse{{ID: "use-" + request.UnknownID, Context: proposal.UsageContext, Evidence: []string{input.Answer.Raw}}}}
		if postgres && mutationService != nil {
			result, err := mutationService.UpdateSkill(ctx, UpdateSkillCommand{Actor: KnowledgeActorAI, Value: fact, Update: update})
			return rootMutationResult(result), err
		}
		if updater, ok := m.mutations.(*CandidateKnowledgeUpdater); ok && updater != nil {
			result, err := updater.UpdateSkill(fact, update)
			return rootMutationResult(CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}), err
		}
		return candidatelearningorchestration.MutationResult{}, errors.New("AI acquisition proposals require a candidate mutation service")
	case "story":
		story := proposal.Story
		if story == nil {
			return candidatelearningorchestration.MutationResult{}, errors.New("story proposal is missing story payload")
		}
		fact := CanonicalCandidateStory{ID: "story-" + request.UnknownID, Title: story.Title, Situation: story.Situation, Task: story.Task, Action: story.Action, Result: story.Result}
		if story.ReferencedExperience != "" {
			if !candidateHasExperience(value, story.ReferencedExperience) {
				return candidatelearningorchestration.MutationResult{}, ErrKnowledgeAnswerMismatch
			}
			fact.ProfileRefs = append(fact.ProfileRefs, story.ReferencedExperience)
		}
		if story.ReferencedProject != "" {
			if !candidateHasProject(value, story.ReferencedProject) {
				return candidatelearningorchestration.MutationResult{}, ErrKnowledgeAnswerMismatch
			}
			fact.ProfileRefs = append(fact.ProfileRefs, story.ReferencedProject)
		}
		if mutationService == nil {
			return candidatelearningorchestration.MutationResult{}, errors.New("story acquisition requires PostgreSQL canonical story storage")
		}
		result, err := mutationService.UpdateStory(ctx, UpdateStoryCommand{Actor: KnowledgeActorAI, Value: fact, Update: update})
		return rootMutationResult(result), err
	default:
		return candidatelearningorchestration.MutationResult{}, errors.New("unsupported acquisition proposal type")
	}
}

func (m rootCandidateLearningMutation) DismissUnknown(ctx context.Context, input candidatelearningorchestration.DismissInput) error {
	refs := KnowledgeEventReferences{UnknownID: input.Request.UnknownID, ClarificationID: input.Request.ID, ConversationID: input.Request.ConversationID, EmployerMessageID: input.Request.EmployerMessageID}
	if mutation, ok := m.mutations.(*CandidateMutationService); ok && mutation != nil {
		return mutation.DismissUnknown(ctx, input.Request.UnknownID, input.Evidence, refs)
	}
	if updater, ok := m.mutations.(*CandidateKnowledgeUpdater); ok && updater != nil {
		return updater.DismissUnknown(input.Request.UnknownID, input.Evidence, refs)
	}
	return errors.New("unknown dismissal requires the candidate mutation service")
}

func (m rootCandidateLearningMutation) ConfirmProposal(ctx context.Context, proposalID string) (candidatelearningorchestration.MutationResult, error) {
	if mutation, ok := m.mutations.(*CandidateMutationService); ok && mutation != nil {
		err := mutation.ConfirmProposal(ctx, ResolveKnowledgeProposalCommand{Actor: KnowledgeActorUser, ProposalID: proposalID})
		return candidatelearningorchestration.MutationResult{ProposalID: proposalID}, err
	}
	if updater, ok := m.mutations.(*CandidateKnowledgeUpdater); ok && updater != nil {
		return candidatelearningorchestration.MutationResult{ProposalID: proposalID}, updater.ConfirmKnowledge(proposalID)
	}
	return candidatelearningorchestration.MutationResult{}, errors.New("proposal confirmation requires the candidate mutation service")
}

func (m rootCandidateLearningMutation) RejectProposal(ctx context.Context, proposalID string) error {
	if mutation, ok := m.mutations.(*CandidateMutationService); ok && mutation != nil {
		return mutation.RejectProposal(ctx, ResolveKnowledgeProposalCommand{Actor: KnowledgeActorUser, ProposalID: proposalID})
	}
	if updater, ok := m.mutations.(*CandidateKnowledgeUpdater); ok && updater != nil {
		return updater.RejectKnowledge(proposalID)
	}
	return errors.New("proposal rejection requires the candidate mutation service")
}

func NewCandidateKnowledgeAcquisitionService(resolver *CandidateContextResolver, mutations CandidateKnowledgeMutationWriter, clarifications ports.CandidateAcquisitionStore, extractor CandidateKnowledgeProposalExtractor) *CandidateKnowledgeAcquisitionService {
	reader := &rootCandidateLearningReader{resolver: resolver}
	service := &CandidateKnowledgeAcquisitionService{Resolver: resolver, Mutations: mutations, Clarifications: clarifications, Extractor: extractor, reader: reader}
	service.orchestration = candidatelearningorchestration.NewService(candidatelearningorchestration.Dependencies{
		Candidate: reader, Clarifications: clarifications, Mutation: rootCandidateLearningMutation{mutations: mutations},
		Interpretation: rootCandidateLearningInterpreterWithCandidate{extractor: extractor, reader: reader}, GenerateID: newKnowledgeID,
	})
	return service
}

func (s *CandidateKnowledgeAcquisitionService) SetStories(stories []CandidateStory) {
	if s == nil || s.reader == nil {
		return
	}
	s.Stories = append([]CandidateStory{}, stories...)
	s.reader.stories = append([]CandidateStory{}, stories...)
}

func (s *CandidateKnowledgeAcquisitionService) currentCandidate() (Candidate, error) {
	if s == nil || s.reader == nil {
		return Candidate{}, errors.New("candidate resolver is not configured")
	}
	return s.reader.CurrentCandidate(context.Background())
}

func (s *CandidateKnowledgeAcquisitionService) CreateClarification(candidate Candidate, employerQuestion string, refs CandidateKnowledgeGapContext) ([]CandidateClarificationRequest, error) {
	if s == nil || s.orchestration == nil {
		return nil, errors.New("knowledge acquisition is not configured")
	}
	result, err := s.orchestration.CreateClarification(context.Background(), candidate, employerQuestion, refs)
	return result.Requests, err
}

func (s *CandidateKnowledgeAcquisitionService) SubmitAnswer(ctx context.Context, id string, answer CandidateAnswer) (CandidateAnswerResult, error) {
	if s == nil || s.orchestration == nil {
		return CandidateAnswerResult{}, errors.New("knowledge acquisition is not configured")
	}
	result, err := s.orchestration.SubmitAnswer(ctx, id, answer)
	return CandidateAnswerResult{ClarificationID: result.ClarificationID, UnknownID: result.UnknownID, ProposalIDs: result.ProposalIDs, Status: result.Status, Disposition: result.Disposition, Mutation: CandidateMutationResult{EntityID: result.Mutation.EntityID, ProposalID: result.Mutation.ProposalID, QuestionID: result.Mutation.QuestionID, SemanticIndexWarning: result.Mutation.SemanticIndexWarning}}, err
}

func (s *CandidateKnowledgeAcquisitionService) ConfirmProposal(ctx context.Context, proposalID string) error {
	if s == nil || s.orchestration == nil {
		return errors.New("knowledge acquisition is not configured")
	}
	_, err := s.orchestration.ConfirmProposal(ctx, proposalID)
	return err
}

func (s *CandidateKnowledgeAcquisitionService) RejectProposal(ctx context.Context, proposalID string) error {
	if s == nil || s.orchestration == nil {
		return errors.New("knowledge acquisition is not configured")
	}
	return s.orchestration.RejectProposal(ctx, proposalID)
}

func (s *CandidateKnowledgeAcquisitionService) DismissClarification(ctx context.Context, id, evidence string) error {
	if s == nil || s.orchestration == nil {
		return errors.New("knowledge acquisition is not configured")
	}
	_, err := s.orchestration.DismissClarification(ctx, id, evidence)
	return err
}

func candidateHasExperience(value Candidate, id string) bool {
	for _, item := range value.Experience {
		if item.ID == id {
			return true
		}
	}
	return false
}

func candidateHasProject(value Candidate, id string) bool {
	for _, item := range value.Projects {
		if item.ID == id {
			return true
		}
	}
	return false
}

var _ candidatelearningorchestration.Interpreter = rootCandidateLearningInterpreterWithCandidate{}
