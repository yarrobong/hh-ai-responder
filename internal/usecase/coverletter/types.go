package coverletter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

var (
	ErrEmptyLetter              = errors.New("cover letter completion is empty")
	ErrUnsupportedCandidateFact = errors.New("cover letter contains an unsupported candidate fact")
)

type DraftStatus string

const (
	DraftStatusValid          DraftStatus = "VALID_DRAFT"
	DraftStatusReviewRequired DraftStatus = "REVIEW_REQUIRED"
	DraftStatusHardInvalid    DraftStatus = "HARD_INVALID"
)

// CandidateFacts is the bounded, prompt-facing candidate projection. Profile
// and SafeKnowledge are read-only values; neither is a Candidate writer.
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
	SafeKnowledge              candidate.EmployerSafeCandidateKnowledge
}

// SemanticHint is a relevance-only projection of already validated semantic
// retrieval. Similarity never becomes Candidate evidence in this package.
type SemanticHint struct {
	EntityType     string
	EntityID       string
	Score          float64
	Title          string
	Text           string
	ContentHash    string
	EmbeddingModel string
}

type Input struct {
	Candidate     CandidateFacts
	Stories       []candidate.CandidateStory
	Vacancy       vacancy.Vacancy
	Description   string
	Assessment    *vacancyanalysis.Assessment
	MatchContext  *MatchContext
	SemanticHints []SemanticHint
	ExtraPrompt   string
}

// MatchContext is an already-computed application matching projection. It is
// prompt context only and does not authorize or perform an application.
type MatchContext struct {
	MatchedSkills   []string
	MatchedProjects []string
	MissingSkills   []string
}

// Result is a validated draft only. It cannot be approved, persisted, or
// delivered by this package.
type Result struct {
	Letter         string          `json:"letter"`
	Evidence       []DraftEvidence `json:"evidence,omitempty"`
	UsedStoryIDs   []string        `json:"used_story_ids,omitempty"`
	Status         DraftStatus     `json:"status"`
	Confidence     string          `json:"confidence"`
	FallbackReason string          `json:"fallback_reason,omitempty"`
}

type DraftEvidence struct {
	Claim     string `json:"claim,omitempty"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}

const (
	ConfidenceValidationOnly        = "VALIDATION_ONLY"
	ConfidenceDeterministicFallback = "DETERMINISTIC_FALLBACK"
)

func (r Result) Fingerprint() string {
	hash := sha256.Sum256([]byte(r.Letter))
	return hex.EncodeToString(hash[:])
}
