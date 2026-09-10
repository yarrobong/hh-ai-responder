package candidateacquisition

import (
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatemutation"
)

type CandidateKnowledgeGap struct {
	CandidateID      string      `json:"candidate_id"`
	SubjectType      string      `json:"subject_type"`
	Subject          string      `json:"subject"`
	SubjectID        string      `json:"subject_id,omitempty"`
	Field            string      `json:"field"`
	Question         string      `json:"question"`
	Context          string      `json:"context,omitempty"`
	Priority         GapPriority `json:"priority"`
	DeterministicKey string      `json:"deterministic_key"`
}

type GapPriority string

const (
	GapBlockingEmployerReply   GapPriority = "blocking_employer_reply"
	GapUsefulProfileEnrichment GapPriority = "useful_profile_enrichment"
	GapBackground              GapPriority = "background"
)

// CandidateKnowledgeGapPriority is retained as a descriptive public name for
// callers migrating from the root package.
type CandidateKnowledgeGapPriority = GapPriority

const (
	KnowledgeGapBlockingEmployerReply   = GapBlockingEmployerReply
	KnowledgeGapUsefulProfileEnrichment = GapUsefulProfileEnrichment
	KnowledgeGapBackground              = GapBackground
)

const (
	KnowledgeSubjectSkill      = "skill"
	KnowledgeSubjectExperience = "experience"
	KnowledgeSubjectProject    = "project"
	KnowledgeSubjectStory      = "behavioral_story"
)

type CandidateClarificationStatus string

const (
	ClarificationPending                   CandidateClarificationStatus = "pending"
	ClarificationAnswered                  CandidateClarificationStatus = "answered"
	ClarificationDismissed                 CandidateClarificationStatus = "dismissed"
	ClarificationResolvedExistingKnowledge CandidateClarificationStatus = "resolved_existing_knowledge"
)

type CandidateClarificationCategory string

const (
	ClarificationSkillExperience      CandidateClarificationCategory = "skill_experience"
	ClarificationSkillCapability      CandidateClarificationCategory = "skill_capability"
	ClarificationWorkExperienceDetail CandidateClarificationCategory = "work_experience_detail"
	ClarificationProjectDetail        CandidateClarificationCategory = "project_detail"
	ClarificationBehavioralStory      CandidateClarificationCategory = "behavioral_story"
	ClarificationPreference           CandidateClarificationCategory = "preference"
	ClarificationConstraint           CandidateClarificationCategory = "constraint"
	ClarificationAvailability         CandidateClarificationCategory = "availability_operational"
	ClarificationOtherFactual         CandidateClarificationCategory = "other_factual"
)

type CandidateClarificationAnswerShape struct {
	Kind    string                         `json:"kind"`
	Options []CandidateClarificationOption `json:"options,omitempty"`
	Prompt  string                         `json:"prompt,omitempty"`
}

type CandidateClarificationOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CandidateAnswer struct {
	Kind       string    `json:"kind"`
	ChoiceID   string    `json:"choice_id,omitempty"`
	Raw        string    `json:"raw"`
	ReceivedAt time.Time `json:"received_at"`
}

type CandidateKnowledgeGapContext struct {
	ConversationID    string
	ApplicationID     string
	VacancyID         string
	EmployerMessageID string
}

type CandidateClarificationRequest struct {
	ID                       string                            `json:"id"`
	ConversationID           string                            `json:"conversation_id,omitempty"`
	ApplicationID            string                            `json:"application_id,omitempty"`
	Topic                    string                            `json:"topic"`
	Question                 string                            `json:"question"`
	Reason                   string                            `json:"reason"`
	Status                   CandidateClarificationStatus      `json:"status"`
	CreatedAt                time.Time                         `json:"created_at"`
	ResolvedAt               *time.Time                        `json:"resolved_at,omitempty"`
	ResolutionReason         string                            `json:"resolution_reason,omitempty"`
	UnknownID                string                            `json:"unknown_id,omitempty"`
	GapKey                   string                            `json:"gap_key,omitempty"`
	VacancyID                string                            `json:"vacancy_id,omitempty"`
	EmployerMessageID        string                            `json:"employer_message_id,omitempty"`
	Category                 CandidateClarificationCategory    `json:"category,omitempty"`
	OriginalEmployerQuestion string                            `json:"original_employer_question,omitempty"`
	SuggestedAnswerShape     CandidateClarificationAnswerShape `json:"suggested_answer_shape,omitempty"`
	Answer                   *CandidateAnswer                  `json:"answer,omitempty"`
	ProposalID               string                            `json:"proposal_id,omitempty"`
	ProposalIDs              []string                          `json:"proposal_ids,omitempty"`
	ReadyForRegeneration     bool                              `json:"ready_for_regeneration,omitempty"`
}

type CandidateStoryDraft struct {
	Title                string `json:"title"`
	Situation            string `json:"situation,omitempty"`
	Task                 string `json:"task,omitempty"`
	Action               string `json:"action,omitempty"`
	Result               string `json:"result,omitempty"`
	ReferencedExperience string `json:"referenced_experience,omitempty"`
	ReferencedProject    string `json:"referenced_project,omitempty"`
}

// CandidateKnowledgeProposalDraft is an untrusted interpretation DTO. It is
// intentionally unable to carry a confirmed truth status through the policy.
type CandidateKnowledgeProposalDraft struct {
	Type         string                               `json:"type"`
	Skill        string                               `json:"skill,omitempty"`
	UsageContext candidate.CanonicalSkillUsageContext `json:"usage_context,omitempty"`
	Level        candidate.SkillLevel                 `json:"level,omitempty"`
	TruthStatus  string                               `json:"truth_status,omitempty"`
	Story        *CandidateStoryDraft                 `json:"story,omitempty"`
}

type CandidateKnowledgeInterpretation struct {
	Proposals []CandidateKnowledgeProposalDraft `json:"proposals"`
}

// EvidenceOrigin preserves the distinction between the raw answer and any
// interpretation adapter output. It is workflow provenance, not a truth
// status; candidate.KnowledgeMetadata remains the authority for truth.
type EvidenceOrigin string

const (
	OriginTrustedExplicitUser EvidenceOrigin = "trusted_explicit_user"
	OriginAIInterpretation    EvidenceOrigin = "ai_interpretation"
	OriginHHResumeEvidence    EvidenceOrigin = "hh_resume_evidence"
	OriginOther               EvidenceOrigin = "other"
)

type AnswerInput struct {
	Request        CandidateClarificationRequest
	Answer         CandidateAnswer
	Origin         EvidenceOrigin
	Interpretation *CandidateKnowledgeInterpretation
}

type AnswerDecisionKind string

const (
	AnswerNeedsClarification  AnswerDecisionKind = "needs_clarification"
	AnswerNeedsInterpretation AnswerDecisionKind = "needs_interpretation"
	AnswerDismisses           AnswerDecisionKind = "dismiss"
	AnswerConfirmsChoice      AnswerDecisionKind = "confirm_choice"
	AnswerCreatesProposal     AnswerDecisionKind = "create_proposal"
)

type MutationIntentKind string

const (
	MutationConfirmSkillUsage MutationIntentKind = "confirm_skill_usage"
	MutationResolveUnknown    MutationIntentKind = "resolve_unknown"
	MutationCreateProposal    MutationIntentKind = "create_proposal"
)

// MutationIntent identifies the operation for the root adapter. It contains
// no persistence callback and no backend-specific value. Lifecycle validation
// remains in candidatemutation when the adapter executes this intent.
type MutationIntent struct {
	Kind               MutationIntentKind
	Actor              candidatemutation.Actor
	Source             candidate.KnowledgeSource
	UnknownResolution  candidatemutation.UnknownResolution
	UnknownID          string
	ClarificationID    string
	Topic              string
	Answer             CandidateAnswer
	SkillUsageContext  candidate.CanonicalSkillUsageContext
	Negative           bool
	Proposal           CandidateKnowledgeProposalDraft
	RequiresUserReview bool
}

type AnswerDecision struct {
	Kind               AnswerDecisionKind
	Intent             *MutationIntent
	RequiresClarifying bool
	Reason             string
}

var (
	ErrKnowledgeAnswerMismatch = errors.New("candidate answer does not resolve the requested knowledge gap")
	ErrKnowledgeAnswerUnknown  = errors.New("candidate answer remains unknown")
	ErrInvalidAnswer           = errors.New("invalid candidate acquisition answer")
)

func (r CandidateClarificationRequest) IsPending() bool {
	return r.Status == ClarificationPending
}

func (r CandidateClarificationRequest) HasActiveGap() bool {
	return strings.TrimSpace(r.GapKey) != "" && r.Status == ClarificationPending
}
