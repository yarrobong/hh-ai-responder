// Package semantic contains infrastructure-neutral values used by the
// Candidate semantic index. It deliberately has no database, provider, or
// Candidate orchestration dependencies.
package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultEmbeddingDimensions preserves the historical OpenAI-compatible
// configuration while allowing a caller to select another supported space.
const DefaultEmbeddingDimensions = 1536

// CandidateSemanticEmbeddingDimensions is retained as a source-compatibility
// alias for older callers. Runtime semantic behavior uses the configured
// EmbeddingContract.Dimensions instead of this constant.
const CandidateSemanticEmbeddingDimensions = DefaultEmbeddingDimensions

const MaxEmbeddingDimensions = 16000

var ErrEmbeddingDimensionMismatch = errors.New("embedding dimensions do not match the configured contract")
var ErrSemanticIndexIncompatible = errors.New("semantic index is incompatible with the active embedding space; reindex required")

type SemanticIndexIncompatibilityError struct {
	CandidateID string
	ActiveSpace string
	FoundSpace  string
}

func (e *SemanticIndexIncompatibilityError) Error() string {
	return fmt.Sprintf("candidate %q has semantic space %q; active space %q requires reindex", e.CandidateID, firstNonEmpty(e.FoundSpace, "unknown"), firstNonEmpty(e.ActiveSpace, "unknown"))
}

func (e *SemanticIndexIncompatibilityError) Unwrap() error { return ErrSemanticIndexIncompatible }

// DimensionMismatchError identifies a provider or local vector contract
// failure without exposing provider credentials or response bodies.
type DimensionMismatchError struct {
	Expected int
	Actual   int
	Source   string
}

func (e *DimensionMismatchError) Error() string {
	return fmt.Sprintf("%s returned %d dimensions, expected %d", firstNonEmpty(e.Source, "embedding vector"), e.Actual, e.Expected)
}

func (e *DimensionMismatchError) Unwrap() error { return ErrEmbeddingDimensionMismatch }

// EmbeddingContract is the complete non-secret identity of one semantic
// embedding space. Endpoint is runtime-only and is never persisted in a
// semantic document; SpaceID is a deterministic fingerprint of the sanitized
// provider/model/endpoint/dimension tuple.
type EmbeddingContract struct {
	Provider   string
	Model      string
	Dimensions int
	SpaceID    string
	Endpoint   string `json:"-"`
}

func NewEmbeddingContract(provider, model string, dimensions int, endpoint string) (EmbeddingContract, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	endpoint = SanitizeEmbeddingEndpoint(endpoint)
	if provider == "" || model == "" {
		return EmbeddingContract{}, errors.New("embedding provider and model are required")
	}
	if dimensions <= 0 || dimensions > MaxEmbeddingDimensions {
		return EmbeddingContract{}, fmt.Errorf("embedding dimensions must be between 1 and %d", MaxEmbeddingDimensions)
	}
	identity := strings.Join([]string{provider, model, strconv.Itoa(dimensions), endpoint}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return EmbeddingContract{Provider: provider, Model: model, Dimensions: dimensions, SpaceID: "sha256:" + hex.EncodeToString(digest[:]), Endpoint: endpoint}, nil
}

// SanitizeEmbeddingEndpoint removes credential-bearing URL components before
// an endpoint contributes to a persisted space identity. Query parameters are
// omitted entirely because their names and values may contain secrets.
func SanitizeEmbeddingEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		// Do not return an unparsed credential-bearing value. This fallback is
		// intentionally conservative and retains only the authority/path.
		fallback := raw
		if index := strings.IndexAny(fallback, "?#"); index >= 0 {
			fallback = fallback[:index]
		}
		if schemeEnd := strings.Index(fallback, "://"); schemeEnd >= 0 {
			authorityEnd := strings.IndexByte(fallback[schemeEnd+3:], '/')
			if authorityEnd < 0 {
				authorityEnd = len(fallback) - (schemeEnd + 3)
			}
			authority := fallback[schemeEnd+3 : schemeEnd+3+authorityEnd]
			if at := strings.LastIndexByte(authority, '@'); at >= 0 {
				fallback = fallback[:schemeEnd+3] + authority[at+1:] + fallback[schemeEnd+3+authorityEnd:]
			}
		}
		return strings.TrimRight(fallback, "/")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/")
}

func ValidateVector(values []float32, expected int, source string) error {
	if expected <= 0 || expected > MaxEmbeddingDimensions {
		return fmt.Errorf("invalid configured embedding dimensions %d", expected)
	}
	if len(values) == 0 {
		return &DimensionMismatchError{Expected: expected, Actual: 0, Source: firstNonEmpty(source, "embedding vector")}
	}
	if len(values) != expected {
		return &DimensionMismatchError{Expected: expected, Actual: len(values), Source: firstNonEmpty(source, "embedding vector")}
	}
	for i, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%s contains a non-finite value at index %d", firstNonEmpty(source, "embedding vector"), i)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

const (
	CandidateSemanticEntityStory       CandidateSemanticEntityType = "story"
	CandidateSemanticEntityProject     CandidateSemanticEntityType = "project"
	CandidateSemanticEntityAchievement CandidateSemanticEntityType = "achievement"
)

type CandidateSemanticEntityType string

type CandidateSemanticEvidenceRef struct {
	EntityType string   `json:"entity_type"`
	EntityID   string   `json:"entity_id"`
	Relation   string   `json:"relation,omitempty"`
	ClaimIDs   []string `json:"claim_ids,omitempty"`
}

type CandidateSemanticTruthSnapshot struct {
	TruthStatus string                         `json:"truth_status,omitempty"`
	ClaimIDs    []string                       `json:"claim_ids,omitempty"`
	Evidence    []string                       `json:"evidence,omitempty"`
	References  []CandidateSemanticEvidenceRef `json:"references,omitempty"`
}

// CandidateSemanticDocument is the already-built, already-embedded index
// representation. It is not Candidate truth and contains no provider or
// PostgreSQL-specific types.
type CandidateSemanticDocument struct {
	ID                  int64
	CandidateID         string
	EntityType          CandidateSemanticEntityType
	EntityID            string
	Content             string
	ContentHash         string
	Embedding           []float32
	EmbeddingProvider   string
	EmbeddingModel      string
	EmbeddingDimensions int
	EmbeddingSpaceID    string
	TruthSnapshot       CandidateSemanticTruthSnapshot
	IndexedAt           time.Time
	SourceUpdatedAt     time.Time
	Metadata            map[string]interface{}
}

type CandidateSemanticResult struct {
	EntityType   CandidateSemanticEntityType
	EntityID     string
	Score        float64
	Title        string
	Excerpt      string
	EvidenceRefs []CandidateSemanticEvidenceRef
	ContentHash  string
	Model        string
	Provider     string
	Dimensions   int
	SpaceID      string
}

type CandidateSemanticSearchQuery struct {
	CandidateID         string
	Embedding           []float32
	EmbeddingProvider   string
	EmbeddingModel      string
	EmbeddingDimensions int
	EmbeddingSpaceID    string
	EntityTypes         []CandidateSemanticEntityType
	Limit               int
	MinScore            *float64
}
