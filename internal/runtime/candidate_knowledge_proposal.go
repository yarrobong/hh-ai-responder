package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/candidatemutation"
)

type KnowledgeProposalStatus = domaincandidate.KnowledgeProposalStatus
type KnowledgeProposal = domaincandidate.KnowledgeProposal

const (
	KnowledgeProposalPending   = domaincandidate.KnowledgeProposalPending
	KnowledgeProposalConfirmed = domaincandidate.KnowledgeProposalConfirmed
	KnowledgeProposalRejected  = domaincandidate.KnowledgeProposalRejected
)

// ProposedValue is a complete replacement, never a patch or trusted metadata.
// Proposal lifecycle remains a root workflow; the value is domain-owned.
func validateKnowledgeProposal(p KnowledgeProposal) error {
	return candidatemutation.ValidateProposal(p)
}

type knowledgeFact interface {
	knowledgeEntity
	KnowledgeMetadataValue() KnowledgeMetadata
}

func decodeKnowledgeFact(kind string, raw json.RawMessage) (knowledgeFact, error) {
	var value knowledgeFact
	switch kind {
	case "skill":
		value = &CandidateSkillDetailed{}
	case "project":
		value = &CandidateProject{}
	case "achievement":
		value = &CandidateAchievement{}
	case "story":
		value = &CanonicalCandidateStory{}
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
	switch v := value.(type) {
	case *CandidateSkillDetailed:
		return *v, nil
	case *CandidateProject:
		return *v, nil
	case *CandidateAchievement:
		return *v, nil
	case *CanonicalCandidateStory:
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
	case CanonicalCandidateStory:
		v.Metadata = meta
		return v
	default:
		panic("unsupported internal knowledge type")
	}
}
