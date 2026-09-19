package vacancyanalysis

import (
	"hh-ai-responder/internal/candidate"
	llmport "hh-ai-responder/internal/ports/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/vacancy"
)

const (
	HardRequirementCategoryEducation       = "education"
	HardRequirementCategoryLocation        = "location"
	HardRequirementCategoryExperienceYears = "experience_years"
	HardRequirementCategorySkill           = "skill"
	HardRequirementCategoryLanguage        = "language"
	HardRequirementCategoryLicense         = "license"
	HardRequirementCategoryCitizenship     = "citizenship"
	HardRequirementCategoryOther           = "other"

	HardRequirementStatusMet     = "met"
	HardRequirementStatusMissing = "missing"
	HardRequirementStatusUnknown = "unknown"

	RecommendationApply      = "APPLY"
	RecommendationDoNotApply = "DO_NOT_APPLY"
	RecommendationUncertain  = "UNCERTAIN"

	RecommendationReasonRoleMismatch    = "ROLE_MISMATCH"
	RecommendationReasonStackMismatch   = "STACK_MISMATCH"
	RecommendationReasonSeniorityGap    = "SENIORITY_GAP"
	RecommendationReasonHardRequirement = "HARD_REQUIREMENT"
	RecommendationReasonLowOverallFit   = "LOW_OVERALL_FIT"
	RecommendationReasonLocationConcern = "LOCATION_CONCERN"
	RecommendationReasonOther           = "OTHER"
)

const (
	RequirementExtractionExplicitHard = "EXPLICIT_HARD"
	RequirementExtractionPreference   = "PREFERENCE"
	RequirementExtractionAmbiguous    = "AMBIGUOUS"
	RequirementExtractionNotRequired  = "NOT_A_REQUIREMENT"

	CandidateEvidenceHHResume           = "HH_RESUME"
	CandidateEvidenceProfile            = "CANDIDATE_PROFILE"
	CandidateEvidenceTrustedProject     = "TRUSTED_PROJECT"
	CandidateEvidenceWorkExperience     = "WORK_EXPERIENCE"
	CandidateEvidenceEducation          = "EDUCATION"
	CandidateEvidenceExplicitConstraint = "EXPLICIT_CONSTRAINT"
	CandidateEvidenceLegacyAggregate    = "LEGACY_AGGREGATE"
	CandidateEvidenceUnknownSource      = "UNKNOWN_SOURCE"
	CandidateEvidenceNone               = "NONE"

	ExperienceClassificationGenericTotal = "GENERIC_TOTAL_DURATION"
	ExperienceClassificationRoleSpecific = "ROLE_SPECIFIC_DURATION"
	ExperienceClassificationTechnology   = "TECHNOLOGY_SPECIFIC_DURATION"
	ExperienceClassificationNotDuration  = "NOT_DURATION"
)

// CandidateFacts is the bounded candidate projection used by vacancy AI. It
// deliberately carries no contacts or storage handles and preserves exact
// experience duration, including the distinction between unknown and zero.
type CandidateFacts struct {
	FullName                   string
	ResumeTitle                string
	Salary                     string
	Experience                 string
	Skills                     string
	Location                   string
	Contacts                   string
	EducationKnown             bool
	EducationLevel             string
	EducationDetails           string
	TotalExperienceMonthsKnown bool
	TotalExperienceMonths      int
	Profile                    candidate.CandidateProfile
	SafeContext                candidatecontext.CandidateContext
}

type Input struct {
	Candidate       CandidateFacts
	Vacancy         vacancy.Vacancy
	Description     string
	Salary          string
	Location        string
	WorkSchedule    string
	IncludeKeywords []string
}

// HardRequirementCandidate is only an extraction candidate from the model.
// Status and candidate evidence are always derived locally.
type HardRequirementCandidate struct {
	Requirement     string `json:"requirement"`
	Category        string `json:"category"`
	VacancyEvidence string `json:"vacancy_evidence"`
}

type RequirementTelemetry struct {
	SourceContext               string   `json:"source_context,omitempty"`
	SourceField                 string   `json:"source_field,omitempty"`
	ExtractionClassification    string   `json:"extraction_classification,omitempty"`
	MandatoryCue                string   `json:"mandatory_cue,omitempty"`
	CandidateEvidenceProvenance string   `json:"candidate_evidence_provenance,omitempty"`
	ExperienceClassification    string   `json:"experience_classification,omitempty"`
	ClassificationDiagnostics   []string `json:"classification_diagnostics,omitempty"`
}

type HardRequirementEvaluation struct {
	Requirement       string                `json:"requirement"`
	Category          string                `json:"category"`
	Status            string                `json:"status"`
	VacancyEvidence   string                `json:"vacancy_evidence"`
	CandidateEvidence string                `json:"candidate_evidence"`
	Soft              bool                  `json:"soft,omitempty"`
	Telemetry         *RequirementTelemetry `json:"telemetry,omitempty"`
}

type AIResponse struct {
	Score                 int                        `json:"score"`
	Apply                 bool                       `json:"apply"` // legacy compatibility; advisory only
	Recommendation        string                     `json:"recommendation,omitempty"`
	RecommendationReasons []string                   `json:"recommendation_reasons,omitempty"`
	Reasons               []string                   `json:"reasons"`
	Missing               []string                   `json:"missing"`
	HardRequirements      []HardRequirementCandidate `json:"hard_requirements"`
	StrongMatch           []string                   `json:"strong_match,omitempty"`
}

// Assessment is validated AI output combined with deterministic evidence
// checks. It is advisory; application eligibility remains a higher-workflow
// decision.
type Assessment struct {
	Score                 int                         `json:"score"`
	Apply                 bool                        `json:"apply"` // legacy compatibility; advisory only
	Recommendation        string                      `json:"recommendation"`
	RecommendationReasons []string                    `json:"recommendation_reasons,omitempty"`
	Reasons               []string                    `json:"reasons"`
	Missing               []string                    `json:"missing"`
	HardRequirements      []HardRequirementEvaluation `json:"hard_requirements"`
	StrongMatch           []string                    `json:"strong_match,omitempty"`
}

type Dependencies struct {
	Completion llmport.CompletionProvider
}
