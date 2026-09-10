package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domaincandidate "hh-ai-responder/internal/candidate"
)

// Transitional compatibility aliases during staged package extraction.
// Domain ownership lives in internal/candidate.
type KnowledgeSource = domaincandidate.KnowledgeSource
type TruthStatus = domaincandidate.TruthStatus
type KnowledgeSourceRecord = domaincandidate.KnowledgeSourceRecord
type KnowledgeMetadata = domaincandidate.KnowledgeMetadata
type CandidateSkillDetailed = domaincandidate.CandidateSkillDetailed
type CandidateProjectType = domaincandidate.CandidateProjectType
type CandidateProjectPeriod = domaincandidate.CandidateProjectPeriod
type CandidateProject = domaincandidate.CandidateProject
type CandidateAchievement = domaincandidate.CandidateAchievement
type CandidateUnknownStatus = domaincandidate.CandidateUnknownStatus
type CandidateUnknown = domaincandidate.CandidateUnknown
type CandidateKnowledgeEvent = domaincandidate.CandidateKnowledgeEvent
type KnowledgeEvent = domaincandidate.CandidateKnowledgeEvent

const (
	KnowledgeSourceUserConfirmed        = domaincandidate.KnowledgeSourceUserConfirmed
	KnowledgeSourceHHResume             = domaincandidate.KnowledgeSourceHHResume
	KnowledgeSourceGithubVerified       = domaincandidate.KnowledgeSourceGithubVerified
	KnowledgeSourceCandidateInterview   = domaincandidate.KnowledgeSourceCandidateInterview
	KnowledgeSourceProjectAnalysis      = domaincandidate.KnowledgeSourceProjectAnalysis
	KnowledgeSourceEmployerConversation = domaincandidate.KnowledgeSourceEmployerConversation
	KnowledgeSourceDerived              = domaincandidate.KnowledgeSourceDerived
	KnowledgeSourceUnknown              = domaincandidate.KnowledgeSourceUnknown
	TruthStatusConfirmed                = domaincandidate.TruthStatusConfirmed
	TruthStatusVerified                 = domaincandidate.TruthStatusVerified
	TruthStatusHypothesis               = domaincandidate.TruthStatusHypothesis
	TruthStatusUnknown                  = domaincandidate.TruthStatusUnknown
	CandidateProjectCommercial          = domaincandidate.CandidateProjectCommercial
	CandidateProjectPersonal            = domaincandidate.CandidateProjectPersonal
	CandidateProjectEducation           = domaincandidate.CandidateProjectEducation
	CandidateUnknownNeedsConfirmation   = domaincandidate.CandidateUnknownNeedsConfirmation
	CandidateUnknownConfirmed           = domaincandidate.CandidateUnknownConfirmed
	CandidateUnknownRejected            = domaincandidate.CandidateUnknownRejected
	CandidateUnknownDismissed           = domaincandidate.CandidateUnknownDismissed
	CandidateUnknownSuperseded          = domaincandidate.CandidateUnknownSuperseded
)

func validKnowledgeSource(source KnowledgeSource) bool {
	return domaincandidate.ValidKnowledgeSource(source)
}
func hasKnowledgeEvidence(values []string) bool { return domaincandidate.HasKnowledgeEvidence(values) }

func validateKnowledgeIdentity(id, label string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(label) == "" {
		return errors.New("knowledge entity requires an id and a name, title or question")
	}
	return nil
}

func validateKnowledgeValue(value interface{ Validate() error }) error {
	if proposal, ok := value.(KnowledgeProposal); ok {
		return validateKnowledgeProposal(proposal)
	}
	return domaincandidate.ValidateKnowledgeValue(value)
}

// Keep the root helper's historical error wrapping available to storage code.
func validateKnowledgeJSON(raw json.RawMessage) error {
	if len(raw) > 0 && !json.Valid(raw) {
		return fmt.Errorf("invalid knowledge JSON")
	}
	return nil
}
