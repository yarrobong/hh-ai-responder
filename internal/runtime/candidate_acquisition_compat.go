package runtime

import (
	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

// Candidate acquisition values are owned by the use-case package. These
// aliases preserve the root package API used by the JSON, dashboard, and AI
// adapters while keeping storage out of the use case.
type CandidateKnowledgeGap = candidateacquisition.CandidateKnowledgeGap
type CandidateKnowledgeGapPriority = candidateacquisition.CandidateKnowledgeGapPriority
type CandidateClarificationStatus = candidateacquisition.CandidateClarificationStatus
type CandidateClarificationCategory = candidateacquisition.CandidateClarificationCategory
type CandidateClarificationAnswerShape = candidateacquisition.CandidateClarificationAnswerShape
type CandidateClarificationOption = candidateacquisition.CandidateClarificationOption
type CandidateAnswer = candidateacquisition.CandidateAnswer
type CandidateKnowledgeGapContext = candidateacquisition.CandidateKnowledgeGapContext
type CandidateKnowledgeInterpretation = candidateacquisition.CandidateKnowledgeInterpretation
type CandidateKnowledgeProposalDraft = candidateacquisition.CandidateKnowledgeProposalDraft
type CandidateStoryDraft = candidateacquisition.CandidateStoryDraft
type CandidateClarificationRequest = candidateacquisition.CandidateClarificationRequest
type CandidateClarificationStore = jsonstorage.CandidateClarificationStore

func NewCandidateClarificationStore(path string) *CandidateClarificationStore {
	return jsonstorage.NewCandidateClarificationStore(path)
}

const (
	KnowledgeGapBlockingEmployerReply      = candidateacquisition.KnowledgeGapBlockingEmployerReply
	KnowledgeGapUsefulProfileEnrichment    = candidateacquisition.KnowledgeGapUsefulProfileEnrichment
	KnowledgeGapBackground                 = candidateacquisition.KnowledgeGapBackground
	KnowledgeSubjectSkill                  = candidateacquisition.KnowledgeSubjectSkill
	KnowledgeSubjectExperience             = candidateacquisition.KnowledgeSubjectExperience
	KnowledgeSubjectProject                = candidateacquisition.KnowledgeSubjectProject
	KnowledgeSubjectStory                  = candidateacquisition.KnowledgeSubjectStory
	ClarificationPending                   = candidateacquisition.ClarificationPending
	ClarificationAnswered                  = candidateacquisition.ClarificationAnswered
	ClarificationDismissed                 = candidateacquisition.ClarificationDismissed
	ClarificationResolvedExistingKnowledge = candidateacquisition.ClarificationResolvedExistingKnowledge
	ClarificationSkillExperience           = candidateacquisition.ClarificationSkillExperience
	ClarificationSkillCapability           = candidateacquisition.ClarificationSkillCapability
	ClarificationWorkExperienceDetail      = candidateacquisition.ClarificationWorkExperienceDetail
	ClarificationProjectDetail             = candidateacquisition.ClarificationProjectDetail
	ClarificationBehavioralStory           = candidateacquisition.ClarificationBehavioralStory
	ClarificationPreference                = candidateacquisition.ClarificationPreference
	ClarificationConstraint                = candidateacquisition.ClarificationConstraint
	ClarificationAvailability              = candidateacquisition.ClarificationAvailability
	ClarificationOtherFactual              = candidateacquisition.ClarificationOtherFactual
)

var (
	ErrKnowledgeAnswerMismatch = candidateacquisition.ErrKnowledgeAnswerMismatch
	ErrKnowledgeAnswerUnknown  = candidateacquisition.ErrKnowledgeAnswerUnknown
)

func DetectCandidateKnowledgeGaps(value Candidate, employerQuestion string) ([]CandidateKnowledgeGap, error) {
	return candidateacquisition.DetectCandidateKnowledgeGaps(value, employerQuestion)
}

func ClarificationForKnowledgeGap(gap CandidateKnowledgeGap, refs CandidateKnowledgeGapContext) CandidateClarificationRequest {
	return candidateacquisition.ClarificationForKnowledgeGap(gap, refs)
}
