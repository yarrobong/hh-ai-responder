package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

// ProposedValue is a complete replacement, never a patch or trusted metadata.
// BaseValue detects intervening updates before confirmation. Proposals and their
// source records are private local data, not an input format for AI responses.
type KnowledgeProposal struct {
	ID            string                  `json:"id"`
	EntityType    string                  `json:"entity_type"`
	EntityID      string                  `json:"entity_id"`
	ProposedValue json.RawMessage         `json:"proposed_value"`
	Reason        string                  `json:"reason"`
	Source        KnowledgeSource         `json:"source"`
	Confidence    *float64                `json:"confidence"`
	Status        KnowledgeProposalStatus `json:"status"`
	CreatedAt     time.Time               `json:"created_at"`
	BaseValue     json.RawMessage         `json:"base_value"`
}

func (p KnowledgeProposal) knowledgeID() string { return p.ID }

func (p KnowledgeProposal) validate() error {
	if err := validateKnowledgeIdentity(p.ID, p.EntityID); err != nil {
		return err
	}
	if strings.TrimSpace(p.Reason) == "" || p.CreatedAt.IsZero() {
		return errors.New("proposal requires reason and created_at")
	}
	switch p.Status {
	case KnowledgeProposalPending, KnowledgeProposalConfirmed, KnowledgeProposalRejected:
	default:
		return errors.New("invalid proposal status")
	}
	if !validKnowledgeSource(p.Source) || p.Source == KnowledgeSourceUnknown || p.Source == KnowledgeSourceUserConfirmed {
		return errors.New("invalid proposal source")
	}
	value, err := decodeKnowledgeFact(p.EntityType, p.ProposedValue)
	if err != nil {
		return err
	}
	meta := value.knowledgeMetadata()
	if value.knowledgeID() != p.EntityID || meta.TruthStatus != TruthStatusHypothesis || meta.ConfirmedAt != nil ||
		len(meta.Sources) != 1 || meta.Sources[0].Type != p.Source || !reflect.DeepEqual(meta.Confidence, p.Confidence) {
		return errors.New("proposal payload must be an unconfirmed assertion with matching identity and provenance")
	}
	if (p.Source == KnowledgeSourceHHResume || p.Source == KnowledgeSourceGithubVerified) && !hasKnowledgeEvidence(meta.Sources[0].Evidence) {
		return errors.New("verified source requires evidence")
	}
	if p.Source == KnowledgeSourceGithubVerified && !validGitHubReference(meta.Sources[0].Reference) {
		return errors.New("GitHub proposal requires a GitHub evidence reference")
	}
	if len(p.BaseValue) > 0 && string(p.BaseValue) != "null" {
		base, err := decodeKnowledgeFact(p.EntityType, p.BaseValue)
		if err != nil {
			return err
		}
		if base.knowledgeID() != p.EntityID {
			return errors.New("proposal base identity mismatch")
		}
	}
	return nil
}

type knowledgeFact interface {
	knowledgeEntity
	knowledgeMetadata() KnowledgeMetadata
}

func (s CandidateSkillDetailed) knowledgeMetadata() KnowledgeMetadata { return s.KnowledgeMetadata }
func (p CandidateProject) knowledgeMetadata() KnowledgeMetadata       { return p.KnowledgeMetadata }
func (a CandidateAchievement) knowledgeMetadata() KnowledgeMetadata   { return a.KnowledgeMetadata }
func (u CandidateUnknown) knowledgeMetadata() KnowledgeMetadata       { return u.KnowledgeMetadata }

func decodeKnowledgeFact(kind string, raw json.RawMessage) (knowledgeFact, error) {
	var value knowledgeFact
	switch kind {
	case "skill":
		value = &CandidateSkillDetailed{}
	case "project":
		value = &CandidateProject{}
	case "achievement":
		value = &CandidateAchievement{}
	default:
		return nil, errors.New("unsupported proposal entity_type")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return nil, errors.New("invalid proposal payload")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing proposal payload")
	}
	if err := validateKnowledgeValue(value); err != nil {
		return nil, err
	}
	// Use value types throughout the updater; no caller-owned pointers survive.
	switch v := value.(type) {
	case *CandidateSkillDetailed:
		return *v, nil
	case *CandidateProject:
		return *v, nil
	case *CandidateAchievement:
		return *v, nil
	}
	return nil, errors.New("unsupported proposal value")
}

func withKnowledgeMetadata(value knowledgeFact, meta KnowledgeMetadata) knowledgeFact {
	switch v := value.(type) {
	case CandidateSkillDetailed:
		v.KnowledgeMetadata = meta
		return v
	case CandidateProject:
		v.KnowledgeMetadata = meta
		return v
	case CandidateAchievement:
		v.KnowledgeMetadata = meta
		return v
	case CandidateUnknown:
		v.KnowledgeMetadata = meta
		return v
	default:
		panic("unsupported internal knowledge type")
	}
}
