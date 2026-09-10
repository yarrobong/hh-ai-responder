package candidatemutation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
)

var (
	ErrInvalidCommand    = errors.New("invalid candidate mutation command")
	ErrInvalidTransition = errors.New("invalid candidate mutation transition")
	ErrNotFound          = errors.New("candidate mutation target not found")
	ErrStale             = errors.New("candidate mutation is stale")
)

// Actor is supplied by trusted application code. Model output must never be
// used as an actor.
type Actor string

const (
	ActorUser     Actor = "user"
	ActorImporter Actor = "importer"
	ActorAI       Actor = "ai"
)

// MetadataInput contains all mutation-time provenance. The value is supplied
// as JSON only so the policy does not depend on an application value interface.
type MetadataInput struct {
	Actor        Actor
	Source       candidate.KnowledgeSourceRecord
	Reason       string
	Confidence   *float64
	EntityType   string
	ValueJSON    json.RawMessage
	Now          time.Time
	VerifyGitHub func(source candidate.KnowledgeSourceRecord, entityType string, value json.RawMessage) error
}

// BuildMetadata applies the shared truth and provenance policy. In particular,
// confidence and derived evidence never promote a hypothesis to truth.
func BuildMetadata(input MetadataInput) (candidate.KnowledgeMetadata, error) {
	if !candidate.ValidKnowledgeSource(input.Source.Type) || strings.TrimSpace(input.Reason) == "" || input.Now.IsZero() {
		return candidate.KnowledgeMetadata{}, fmt.Errorf("%w: update requires a valid source, reason and timestamp", ErrInvalidCommand)
	}
	if input.Confidence != nil && (math.IsNaN(*input.Confidence) || math.IsInf(*input.Confidence, 0) || *input.Confidence < 0 || *input.Confidence > 1) {
		return candidate.KnowledgeMetadata{}, fmt.Errorf("%w: confidence must be a finite number between 0 and 1, or null", ErrInvalidCommand)
	}
	source := input.Source
	source.Evidence = append([]string(nil), source.Evidence...)
	meta := candidate.KnowledgeMetadata{
		Confidence: input.Confidence, TruthStatus: candidate.TruthStatusHypothesis,
		Sources: []candidate.KnowledgeSourceRecord{source}, Evidence: append([]string(nil), source.Evidence...),
		CreatedAt: input.Now, UpdatedAt: input.Now,
	}
	switch source.Type {
	case candidate.KnowledgeSourceUserConfirmed:
		if input.Actor != ActorUser {
			return meta, errors.New("only an explicit user action may create user_confirmed knowledge")
		}
		meta.TruthStatus, meta.ConfirmedAt = candidate.TruthStatusConfirmed, timePtr(input.Now)
	case candidate.KnowledgeSourceHHResume:
		if input.Actor == ActorAI {
			return meta, errors.New("HH provenance requires a trusted resume importer")
		}
		if !candidate.HasKnowledgeEvidence(source.Evidence) {
			return meta, errors.New("HH resume source requires evidence")
		}
		meta.TruthStatus = candidate.TruthStatusVerified
	case candidate.KnowledgeSourceGithubVerified:
		if !validGitHubReference(source.Reference) || !candidate.HasKnowledgeEvidence(source.Evidence) || input.VerifyGitHub == nil {
			return meta, errors.New("GitHub source requires reference, evidence and a trusted verifier")
		}
		if err := input.VerifyGitHub(source, input.EntityType, append(json.RawMessage(nil), input.ValueJSON...)); err != nil {
			return meta, errors.New("GitHub evidence verification failed")
		}
	case candidate.KnowledgeSourceUnknown:
		meta.TruthStatus = candidate.TruthStatusUnknown
	}
	if input.EntityType == "unknown" {
		meta.TruthStatus, meta.ConfirmedAt = candidate.TruthStatusUnknown, nil
	}
	if err := meta.Validate(); err != nil {
		return meta, err
	}
	return meta, nil
}

func timePtr(value time.Time) *time.Time { copy := value; return &copy }

func validGitHubReference(reference string) bool {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

type ProposalResolution string

const (
	ConfirmProposal ProposalResolution = "confirm"
	RejectProposal  ProposalResolution = "reject"
)

type ProposalInput struct {
	ID                string
	EntityType        string
	EntityID          string
	ProposedValue     json.RawMessage
	Reason            string
	Source            candidate.KnowledgeSource
	Confidence        *float64
	CreatedAt         time.Time
	BaseValue         json.RawMessage
	UnknownID         string
	ClarificationID   string
	ConversationID    string
	EmployerMessageID string
}

// BuildProposal is the shared proposal-construction policy used by both
// persistence adapters. IDs and timestamps remain orchestration inputs.
func BuildProposal(input ProposalInput) (candidate.KnowledgeProposal, error) {
	proposal := candidate.KnowledgeProposal{
		ID: input.ID, EntityType: input.EntityType, EntityID: input.EntityID,
		ProposedValue: append(json.RawMessage(nil), input.ProposedValue...), Reason: input.Reason,
		Source: input.Source, Confidence: input.Confidence, Status: candidate.KnowledgeProposalPending,
		CreatedAt: input.CreatedAt, BaseValue: append(json.RawMessage(nil), input.BaseValue...),
		UnknownID: input.UnknownID, ClarificationID: input.ClarificationID, ConversationID: input.ConversationID,
		EmployerMessageID: input.EmployerMessageID,
	}
	if err := ValidateProposal(proposal); err != nil {
		return candidate.KnowledgeProposal{}, err
	}
	return proposal, nil
}

// BuildConfirmedMetadata is the shared explicit-confirmation truth transition.
func BuildConfirmedMetadata(meta candidate.KnowledgeMetadata, proposalSource candidate.KnowledgeSource, proposalID string, now time.Time) (candidate.KnowledgeMetadata, error) {
	if now.IsZero() || strings.TrimSpace(proposalID) == "" {
		return candidate.KnowledgeMetadata{}, fmt.Errorf("%w: confirmation requires proposal ID and timestamp", ErrInvalidCommand)
	}
	meta.UpdatedAt, meta.ConfirmedAt = now, timePtr(now)
	meta.TruthStatus = candidate.TruthStatusConfirmed
	if proposalSource == candidate.KnowledgeSourceGithubVerified || proposalSource == candidate.KnowledgeSourceHHResume {
		meta.TruthStatus = candidate.TruthStatusVerified
	}
	meta.Sources = append(append([]candidate.KnowledgeSourceRecord(nil), meta.Sources...), candidate.KnowledgeSourceRecord{
		Type: candidate.KnowledgeSourceUserConfirmed, Evidence: []string{"user explicitly confirmed proposal " + proposalID}, ObservedAt: timePtr(now),
	})
	if err := meta.Validate(); err != nil {
		return candidate.KnowledgeMetadata{}, err
	}
	return meta, nil
}

// ProposalMetadata removes any confirmation marker before an assertion is
// staged for review. It is deliberately separate from BuildConfirmedMetadata:
// proposing and confirming are different explicit operations.
func ProposalMetadata(meta candidate.KnowledgeMetadata) candidate.KnowledgeMetadata {
	meta.TruthStatus, meta.ConfirmedAt = candidate.TruthStatusHypothesis, nil
	return meta
}

// ApplyProposalResolution is the one proposal lifecycle transition table.
func ApplyProposalResolution(proposal *candidate.KnowledgeProposal, operation ProposalResolution) error {
	if proposal == nil {
		return fmt.Errorf("%w: proposal is nil", ErrInvalidCommand)
	}
	if proposal.Status != candidate.KnowledgeProposalPending {
		return errors.New("knowledge proposal is already resolved")
	}
	switch operation {
	case ConfirmProposal:
		proposal.Status = candidate.KnowledgeProposalConfirmed
	case RejectProposal:
		proposal.Status = candidate.KnowledgeProposalRejected
	default:
		return fmt.Errorf("%w: unsupported proposal operation %q", ErrInvalidTransition, operation)
	}
	return nil
}

type UnknownResolution string

const (
	ConfirmUnknown   UnknownResolution = "confirm"
	RejectUnknown    UnknownResolution = "reject"
	DismissUnknown   UnknownResolution = "dismiss"
	SupersedeUnknown UnknownResolution = "supersede"
)

// UnknownResolutionInput describes an explicit resolution. Now is mandatory
// to keep the policy deterministic in tests and in replay-like workflows.
type UnknownResolutionInput struct {
	Operation UnknownResolution
	Answer    string
	Evidence  string
	Now       time.Time
}

// ResolveUnknown applies the one unknown lifecycle and truth policy. It does
// not create events or claims; those remain persistence/application concerns.
func ResolveUnknown(current candidate.CandidateUnknown, input UnknownResolutionInput) (candidate.CandidateUnknown, error) {
	if current.Status != candidate.CandidateUnknownNeedsConfirmation {
		return current, errors.New("candidate unknown is already resolved")
	}
	if input.Now.IsZero() {
		return current, fmt.Errorf("%w: unknown resolution timestamp is required", ErrInvalidCommand)
	}
	answer := strings.TrimSpace(input.Answer)
	evidence := strings.TrimSpace(input.Evidence)
	if input.Operation == ConfirmUnknown && answer == "" {
		return current, fmt.Errorf("%w: explicit answer is required", ErrInvalidCommand)
	}
	if evidence == "" {
		switch input.Operation {
		case ConfirmUnknown:
			evidence = answer
		case DismissUnknown:
			evidence = "candidate declined to answer"
		default:
			evidence = "explicit candidate resolution"
		}
	}
	result := current
	result.UpdatedAt = input.Now
	result.Sources = []candidate.KnowledgeSourceRecord{{Type: candidate.KnowledgeSourceUserConfirmed, Evidence: []string{evidence}, ObservedAt: timePtr(input.Now)}}
	result.Evidence = []string{evidence}
	result.ConfirmedAt = nil
	switch input.Operation {
	case ConfirmUnknown:
		result.Status, result.TruthStatus = candidate.CandidateUnknownConfirmed, candidate.TruthStatusConfirmed
		result.Hypothesis = input.Answer
		result.ConfirmedAt = timePtr(input.Now)
	case RejectUnknown:
		result.Status, result.TruthStatus = candidate.CandidateUnknownRejected, candidate.TruthStatusUnknown
	case DismissUnknown:
		result.Status, result.TruthStatus = candidate.CandidateUnknownDismissed, candidate.TruthStatusUnknown
	case SupersedeUnknown:
		result.Status, result.TruthStatus = candidate.CandidateUnknownSuperseded, candidate.TruthStatusUnknown
	default:
		return current, fmt.Errorf("%w: unsupported unknown operation %q", ErrInvalidTransition, input.Operation)
	}
	if err := result.Validate(); err != nil {
		return current, err
	}
	return result, nil
}

// CheckExpectedVersion separates mutation stale policy from storage
// transaction conflicts. A zero expected version means no precondition.
func CheckExpectedVersion(currentVersion, expectedVersion int) error {
	if expectedVersion != 0 && currentVersion != expectedVersion {
		return fmt.Errorf("%w: candidate version is %d, expected %d", ErrStale, currentVersion, expectedVersion)
	}
	return nil
}

// CheckBaseValue applies the shared proposal stale guard. The adapters decide
// how to load the current value; the comparison itself is deterministic.
func CheckBaseValue(expected, current json.RawMessage) error {
	if len(expected) == 0 || string(expected) == "null" {
		return nil
	}
	var compactExpected, compactCurrent bytes.Buffer
	if err := json.Compact(&compactExpected, expected); err != nil {
		return fmt.Errorf("%w: invalid proposal base value", ErrStale)
	}
	if err := json.Compact(&compactCurrent, current); err != nil {
		return fmt.Errorf("%w: invalid current proposal value", ErrStale)
	}
	if !bytes.Equal(compactExpected.Bytes(), compactCurrent.Bytes()) {
		return errors.New("knowledge changed since proposal; create and review a new proposal")
	}
	return nil
}

type EventInput struct {
	ID                string
	Now               time.Time
	Action            string
	EntityType        string
	EntityID          string
	OldValue          json.RawMessage
	NewValue          json.RawMessage
	Source            candidate.KnowledgeSource
	Actor             string
	UnknownID         string
	ClarificationID   string
	ProposalID        string
	ConversationID    string
	EmployerMessageID string
}

// BuildEvent is the shared logical event mapping. It never writes the event
// and requires the adapter to supply the ID and timestamp explicitly.
func BuildEvent(input EventInput) (candidate.CandidateKnowledgeEvent, error) {
	if strings.TrimSpace(input.ID) == "" || input.Now.IsZero() || strings.TrimSpace(input.Action) == "" || strings.TrimSpace(input.EntityType) == "" || strings.TrimSpace(input.EntityID) == "" || strings.TrimSpace(input.Actor) == "" {
		return candidate.CandidateKnowledgeEvent{}, errors.New("incomplete knowledge event")
	}
	event := candidate.CandidateKnowledgeEvent{
		ID: input.ID, Timestamp: input.Now, Action: input.Action, EntityType: input.EntityType, EntityID: input.EntityID,
		OldValue: append(json.RawMessage(nil), input.OldValue...), NewValue: append(json.RawMessage(nil), input.NewValue...),
		Source: input.Source, Actor: input.Actor, UnknownID: input.UnknownID, ClarificationID: input.ClarificationID,
		ProposalID: input.ProposalID, ConversationID: input.ConversationID, EmployerMessageID: input.EmployerMessageID,
	}
	if err := event.Validate(); err != nil {
		return candidate.CandidateKnowledgeEvent{}, err
	}
	return event, nil
}

// ValidateProposal owns the storage-independent proposal payload checks.
func ValidateProposal(proposal candidate.KnowledgeProposal) error {
	if strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.EntityType) == "" || strings.TrimSpace(proposal.EntityID) == "" || len(proposal.ProposedValue) == 0 || !json.Valid(proposal.ProposedValue) || strings.TrimSpace(proposal.Reason) == "" || proposal.CreatedAt.IsZero() {
		return errors.New("invalid knowledge proposal")
	}
	if proposal.Source == "" || proposal.Source == candidate.KnowledgeSourceUnknown || proposal.Source == candidate.KnowledgeSourceUserConfirmed || proposal.Source == candidate.KnowledgeSourceEmployerConversation {
		return errors.New("invalid proposal source")
	}
	switch proposal.Status {
	case candidate.KnowledgeProposalPending, candidate.KnowledgeProposalConfirmed, candidate.KnowledgeProposalRejected:
	default:
		return errors.New("invalid proposal status")
	}
	valueID, meta, err := proposalPayload(proposal.EntityType, proposal.ProposedValue)
	if err != nil {
		return err
	}
	if valueID != proposal.EntityID || meta.TruthStatus != candidate.TruthStatusHypothesis || meta.ConfirmedAt != nil || len(meta.Sources) != 1 || meta.Sources[0].Type != proposal.Source || !reflect.DeepEqual(meta.Confidence, proposal.Confidence) {
		return errors.New("proposal payload must be an unconfirmed assertion with matching identity and provenance")
	}
	if (proposal.Source == candidate.KnowledgeSourceHHResume || proposal.Source == candidate.KnowledgeSourceGithubVerified) && !candidate.HasKnowledgeEvidence(meta.Sources[0].Evidence) {
		return errors.New("verified source requires evidence")
	}
	if proposal.Source == candidate.KnowledgeSourceGithubVerified && !validGitHubReference(meta.Sources[0].Reference) {
		return errors.New("GitHub proposal requires a GitHub evidence reference")
	}
	if len(proposal.BaseValue) > 0 && string(proposal.BaseValue) != "null" {
		baseID, _, err := proposalPayload(proposal.EntityType, proposal.BaseValue)
		if err != nil {
			return err
		}
		if baseID != proposal.EntityID {
			return errors.New("proposal base identity mismatch")
		}
	}
	return nil
}

func proposalPayload(entityType string, raw json.RawMessage) (string, candidate.KnowledgeMetadata, error) {
	var value interface {
		Validate() error
		KnowledgeID() string
		KnowledgeMetadataValue() candidate.KnowledgeMetadata
	}
	switch entityType {
	case "skill":
		value = &candidate.CandidateSkillDetailed{}
	case "project":
		value = &candidate.CandidateProject{}
	case "achievement":
		value = &candidate.CandidateAchievement{}
	case "story":
		value = &candidate.CanonicalCandidateStory{}
	default:
		return "", candidate.KnowledgeMetadata{}, errors.New("unsupported proposal entity_type")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return "", candidate.KnowledgeMetadata{}, errors.New("invalid proposal payload")
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		return "", candidate.KnowledgeMetadata{}, errors.New("trailing proposal payload")
	}
	if err := candidate.ValidateKnowledgeValue(value); err != nil {
		return "", candidate.KnowledgeMetadata{}, err
	}
	return value.KnowledgeID(), value.KnowledgeMetadataValue(), nil
}
