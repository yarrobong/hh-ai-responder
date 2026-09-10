package candidateacquisition

import (
	"strings"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatemutation"
)

var acquisitionChoices = map[string]bool{
	"commercial": true, "pet_project": true, "educational": true, "personal": true,
	"studied_only": true, "explicitly_not_used": true, "free_text": true, "dismissed": true,
}

func ValidChoice(value string) bool { return acquisitionChoices[value] }

func CandidateSaysUnknown(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return lower == "не знаю" || lower == "не помню" || strings.Contains(lower, "не могу вспомнить")
}

// ValidateAnswer checks only the transport-independent shape. It intentionally
// does not interpret free text or decide whether a fact is true.
func ValidateAnswer(request CandidateClarificationRequest, answer CandidateAnswer) error {
	if !request.IsPending() || strings.TrimSpace(answer.Raw) == "" {
		return ErrInvalidAnswer
	}
	if answer.Kind == "" {
		answer.Kind = "free_text"
	}
	if answer.Kind != "choice" && answer.Kind != "free_text" {
		return ErrInvalidAnswer
	}
	if answer.Kind == "choice" && !ValidChoice(answer.ChoiceID) {
		return ErrInvalidAnswer
	}
	return nil
}

// DecideAnswer is the acquisition policy boundary. A raw explicit answer may
// create a confirmed mutation intent; an AI interpretation can only create an
// untrusted proposal intent that requires later user review.
func DecideAnswer(input AnswerInput) (AnswerDecision, error) {
	classification, err := ClassifyAnswer(input.Request, input.Answer)
	if err != nil {
		return AnswerDecision{}, err
	}
	if classification.Kind == AnswerNeedsClarification || classification.Kind == AnswerDismisses || classification.Kind == AnswerConfirmsChoice {
		if classification.Intent != nil && input.Origin != OriginTrustedExplicitUser {
			return AnswerDecision{}, ErrInvalidAnswer
		}
		return classification, nil
	}
	if input.Origin == OriginTrustedExplicitUser || input.Origin == OriginHHResumeEvidence {
		return AnswerDecision{}, ErrInvalidAnswer
	}
	if input.Interpretation == nil {
		return AnswerDecision{}, ErrKnowledgeAnswerMismatch
	}
	if err := ValidateInterpretation(*input.Interpretation, input.Request); err != nil {
		return AnswerDecision{}, err
	}
	if len(input.Interpretation.Proposals) == 0 {
		return AnswerDecision{}, ErrKnowledgeAnswerMismatch
	}
	proposal := input.Interpretation.Proposals[0]
	return AnswerDecision{Kind: AnswerCreatesProposal, Intent: &MutationIntent{
		Kind: MutationCreateProposal, Actor: candidatemutation.ActorAI, Source: candidate.KnowledgeSourceCandidateInterview,
		UnknownID: input.Request.UnknownID, ClarificationID: input.Request.ID, Topic: input.Request.Topic,
		Answer: input.Answer, Proposal: proposal, RequiresUserReview: true,
	}, Reason: "AI interpretation is retained as a proposal and requires explicit candidate confirmation"}, nil
}

func ClassifyAnswer(request CandidateClarificationRequest, answer CandidateAnswer) (AnswerDecision, error) {
	if err := ValidateAnswer(request, answer); err != nil {
		return AnswerDecision{}, err
	}
	if CandidateSaysUnknown(answer.Raw) {
		return AnswerDecision{Kind: AnswerNeedsClarification, RequiresClarifying: true, Reason: "answer does not contain enough candidate evidence"}, nil
	}
	if answer.Kind == "choice" && answer.ChoiceID == "dismissed" {
		return AnswerDecision{Kind: AnswerDismisses, Intent: &MutationIntent{
			Kind: MutationResolveUnknown, Actor: candidatemutation.ActorUser, Source: candidate.KnowledgeSourceUserConfirmed,
			UnknownResolution: candidatemutation.DismissUnknown, UnknownID: request.UnknownID, ClarificationID: request.ID, Topic: request.Topic, Answer: answer,
		}, Reason: "candidate explicitly declined to save this knowledge"}, nil
	}
	if answer.Kind == "choice" && answer.ChoiceID != "free_text" {
		return AnswerDecision{Kind: AnswerConfirmsChoice, Intent: &MutationIntent{
			Kind: MutationConfirmSkillUsage, Actor: candidatemutation.ActorUser, Source: candidate.KnowledgeSourceUserConfirmed,
			UnknownID: request.UnknownID, ClarificationID: request.ID, Topic: request.Topic, Answer: answer,
			SkillUsageContext: candidate.CanonicalSkillUsageContext(answer.ChoiceID), Negative: answer.ChoiceID == "explicitly_not_used",
		}, Reason: "candidate explicitly selected a structured answer"}, nil
	}
	return AnswerDecision{Kind: AnswerNeedsInterpretation, Reason: "free-text answer requires a typed interpretation"}, nil
}

func ValidateInterpretation(value CandidateKnowledgeInterpretation, request CandidateClarificationRequest) error {
	if value.Proposals == nil || len(value.Proposals) > 8 {
		return ErrInvalidAnswer
	}
	for _, proposal := range value.Proposals {
		if status := strings.TrimSpace(proposal.TruthStatus); status != "" && !strings.EqualFold(status, string(candidate.TruthStatusHypothesis)) {
			return ErrInvalidAnswer
		}
		switch proposal.Type {
		case "skill_usage":
			if strings.TrimSpace(proposal.Skill) == "" || canonical(proposal.Skill) != canonical(request.Topic) {
				return ErrKnowledgeAnswerMismatch
			}
			switch proposal.UsageContext {
			case candidate.CanonicalSkillUsageCommercial, candidate.CanonicalSkillUsagePetProject,
				candidate.CanonicalSkillUsageEducational, candidate.CanonicalSkillUsagePersonal,
				candidate.CanonicalSkillUsageStudiedOnly, candidate.CanonicalSkillUsageUnknown,
				candidate.CanonicalSkillUsageExplicitlyNotUsed:
			default:
				return ErrInvalidAnswer
			}
		case "story":
			if proposal.Story == nil || strings.TrimSpace(proposal.Story.Title) == "" || strings.TrimSpace(proposal.Story.Situation+proposal.Story.Task+proposal.Story.Action+proposal.Story.Result) == "" {
				return ErrKnowledgeAnswerMismatch
			}
		default:
			return ErrKnowledgeAnswerMismatch
		}
	}
	return nil
}
