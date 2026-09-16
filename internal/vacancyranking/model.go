// Package vacancyranking contains the read-only deterministic vacancy
// eligibility and base-ranking model. It deliberately has no HH or AI
// dependencies and does not authorize applications.
package vacancyranking

import (
	"time"

	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

const AlgorithmVersion = "v1"

type Eligibility string

const (
	EligibilityEligible       Eligibility = "eligible"
	EligibilityReviewRequired Eligibility = "review_required"
	EligibilityIneligible     Eligibility = "ineligible"
	EligibilityUnavailable    Eligibility = "unavailable"
)

type FitBand string

const (
	FitCompatible       FitBand = "compatible"
	FitStretch          FitBand = "stretch"
	FitUnlikely         FitBand = "unlikely"
	FitHardIncompatible FitBand = "hard_incompatible"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type AnalysisState string

const (
	AnalysisComplete             AnalysisState = "complete"
	AnalysisPartial              AnalysisState = "partial"
	AnalysisInsufficientEvidence AnalysisState = "insufficient_evidence"
)

type ReasonSource string

const (
	SourceStructuredProvider ReasonSource = "structured_provider"
	SourceDescription        ReasonSource = "description"
	SourceCandidateTruth     ReasonSource = "candidate_truth"
	SourceReviewState        ReasonSource = "review_state"
	SourceApplicationState   ReasonSource = "application_state"
	SourceLegacyMatch        ReasonSource = "legacy_match"
	SourceDerived            ReasonSource = "derived"
)

type ReasonOutcome string

const (
	OutcomePositive  ReasonOutcome = "positive"
	OutcomeConcern   ReasonOutcome = "concern"
	OutcomeUnknown   ReasonOutcome = "unknown"
	OutcomeConflict  ReasonOutcome = "conflict"
	OutcomeExclusion ReasonOutcome = "exclusion"
)

// Reason is the machine-readable explanation unit. Evidence is intentionally
// concise and never stores a complete vacancy description.
type Reason struct {
	Code       string        `json:"code"`
	Outcome    ReasonOutcome `json:"outcome"`
	Source     ReasonSource  `json:"source"`
	Evidence   string        `json:"evidence"`
	Confidence Confidence    `json:"confidence"`
}

type ScoreComponents struct {
	RoleFit         int `json:"role_fit"`
	SkillFit        int `json:"skill_fit"`
	ExperienceFit   int `json:"experience_fit"`
	LocationWorkFit int `json:"location_work_format"`
	SalaryFit       int `json:"salary_fit"`
	Freshness       int `json:"freshness"`
}

type LegacyEvidence struct {
	Available     bool     `json:"available"`
	Score         int      `json:"score,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	MatchedSkills []string `json:"matched_skills,omitempty"`
	UnknownSkills []string `json:"unknown_skills,omitempty"`
	MissingSkills []string `json:"missing_skills,omitempty"`
	MatchedRoles  []string `json:"matched_roles,omitempty"`
	Risks         []string `json:"risks,omitempty"`
}

type Result struct {
	AlgorithmVersion   AlgorithmVersionAlias `json:"algorithm_version"`
	Vacancy            vacancy.Vacancy       `json:"vacancy"`
	Eligibility        Eligibility           `json:"eligibility"`
	FitBand            FitBand               `json:"fit_band"`
	Confidence         Confidence            `json:"confidence"`
	AnalysisState      AnalysisState         `json:"analysis_state"`
	BaseRankScore      int                   `json:"base_rank_score"`
	Rankable           bool                  `json:"rankable"`
	ExclusionCode      string                `json:"exclusion_code,omitempty"`
	ReviewState        vacancyreview.State   `json:"review_state"`
	ApplicationLinked  bool                  `json:"application_linked"`
	ChangedSinceReview *bool                 `json:"changed_since_review,omitempty"`
	Freshness          vacancy.Freshness     `json:"freshness"`
	Components         ScoreComponents       `json:"score_components"`
	PositiveReasons    []Reason              `json:"positive_reasons"`
	Concerns           []Reason              `json:"concerns"`
	Unknowns           []Reason              `json:"unknowns"`
	HardReasons        []Reason              `json:"hard_reasons"`
	Legacy             LegacyEvidence        `json:"legacy_match"`
}

// AlgorithmVersionAlias keeps the JSON shape explicit while allowing the
// version constant to remain a plain string for callers.
type AlgorithmVersionAlias = string

type EvaluateInput struct {
	Vacancy   vacancy.Vacancy
	Candidate candidateContext
	Effective vacancyreview.EffectiveState
	Now       time.Time
}

// candidateContext is kept private; callers use NewCandidateContext so the
// projection boundary remains in this package.
type candidateContext struct {
	Skills          []candidateSkill
	Roles           []string
	TotalExperience *int
	OfficeLocation  string
	WorkMode        string
	Relocation      string
	BusinessTrips   string
	SalaryMinimum   *int
	Languages       []string
}

type candidateSkill struct {
	Name     string
	Negative bool
	Known    bool
}
