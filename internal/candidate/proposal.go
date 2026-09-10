package candidate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"
)

type KnowledgeProposalStatus string

const (
	KnowledgeProposalPending   KnowledgeProposalStatus = "pending"
	KnowledgeProposalConfirmed KnowledgeProposalStatus = "confirmed"
	KnowledgeProposalRejected  KnowledgeProposalStatus = "rejected"
)

type KnowledgeProposal struct {
	ID                string                  `json:"id"`
	EntityType        string                  `json:"entity_type"`
	EntityID          string                  `json:"entity_id"`
	ProposedValue     json.RawMessage         `json:"proposed_value"`
	Reason            string                  `json:"reason"`
	Source            KnowledgeSource         `json:"source"`
	Confidence        *float64                `json:"confidence"`
	Status            KnowledgeProposalStatus `json:"status"`
	CreatedAt         time.Time               `json:"created_at"`
	BaseValue         json.RawMessage         `json:"base_value"`
	UnknownID         string                  `json:"unknown_id,omitempty"`
	ClarificationID   string                  `json:"clarification_id,omitempty"`
	ConversationID    string                  `json:"conversation_id,omitempty"`
	EmployerMessageID string                  `json:"employer_message_id,omitempty"`
}

func (p KnowledgeProposal) KnowledgeID() string { return p.ID }
func (p KnowledgeProposal) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.EntityType) == "" || strings.TrimSpace(p.EntityID) == "" || len(p.ProposedValue) == 0 || !json.Valid(p.ProposedValue) || strings.TrimSpace(p.Reason) == "" || p.CreatedAt.IsZero() {
		return errors.New("invalid knowledge proposal")
	}
	if p.Source == "" {
		return errors.New("knowledge proposal requires a source")
	}
	switch p.Status {
	case KnowledgeProposalPending, KnowledgeProposalConfirmed, KnowledgeProposalRejected:
	default:
		return errors.New("invalid knowledge proposal status")
	}
	return nil
}

// ValidateKnowledgeProposal contains the storage-independent proposal
// invariants required before a proposal can participate in canonical
// assembly. Mutation workflows may add transition rules, but do not duplicate
// this persisted-value validation.
func ValidateKnowledgeProposal(proposal KnowledgeProposal) error {
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.Source == KnowledgeSourceUnknown || proposal.Source == KnowledgeSourceUserConfirmed || proposal.Source == KnowledgeSourceEmployerConversation {
		return errors.New("invalid proposal source")
	}
	valueID, metadata, err := proposalPayload(proposal.EntityType, proposal.ProposedValue)
	if err != nil {
		return err
	}
	if valueID != proposal.EntityID || metadata.TruthStatus != TruthStatusHypothesis || metadata.ConfirmedAt != nil || len(metadata.Sources) != 1 || metadata.Sources[0].Type != proposal.Source || !reflect.DeepEqual(metadata.Confidence, proposal.Confidence) {
		return errors.New("proposal payload must be an unconfirmed assertion with matching identity and provenance")
	}
	if (proposal.Source == KnowledgeSourceHHResume || proposal.Source == KnowledgeSourceGithubVerified) && !HasKnowledgeEvidence(metadata.Sources[0].Evidence) {
		return errors.New("verified source requires evidence")
	}
	if proposal.Source == KnowledgeSourceGithubVerified && !validGitHubReference(metadata.Sources[0].Reference) {
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

func proposalPayload(entityType string, raw json.RawMessage) (string, KnowledgeMetadata, error) {
	var value interface {
		Validate() error
		KnowledgeID() string
		KnowledgeMetadataValue() KnowledgeMetadata
	}
	switch entityType {
	case "skill":
		value = &CandidateSkillDetailed{}
	case "project":
		value = &CandidateProject{}
	case "achievement":
		value = &CandidateAchievement{}
	case "story":
		value = &CanonicalCandidateStory{}
	default:
		return "", KnowledgeMetadata{}, errors.New("unsupported proposal entity_type")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return "", KnowledgeMetadata{}, errors.New("invalid proposal payload")
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		return "", KnowledgeMetadata{}, errors.New("trailing proposal payload")
	}
	if err := ValidateKnowledgeValue(value); err != nil {
		return "", KnowledgeMetadata{}, err
	}
	return value.KnowledgeID(), value.KnowledgeMetadataValue(), nil
}

func validGitHubReference(reference string) bool {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}
