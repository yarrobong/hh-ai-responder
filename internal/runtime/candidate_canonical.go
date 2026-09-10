package runtime

import domaincandidate "hh-ai-responder/internal/candidate"

// Transitional compatibility aliases during staged package extraction.
// Domain ownership lives in internal/candidate.
type Candidate = domaincandidate.Candidate
type CanonicalProfileSnapshot = domaincandidate.CanonicalProfileSnapshot
type CanonicalCandidateIdentity = domaincandidate.CanonicalCandidateIdentity
type CanonicalCandidateContact = domaincandidate.CanonicalCandidateContact
type CanonicalExternalReference = domaincandidate.CanonicalExternalReference
type CanonicalCandidateEducation = domaincandidate.CanonicalCandidateEducation
type CanonicalCandidateLanguage = domaincandidate.CanonicalCandidateLanguage
type CanonicalCandidateExperience = domaincandidate.CanonicalCandidateExperience
type CanonicalCandidateSkill = domaincandidate.CanonicalCandidateSkill
type CanonicalCandidateSkillAssertion = domaincandidate.CanonicalCandidateSkillAssertion
type CanonicalSkillCapability = domaincandidate.CanonicalSkillCapability
type CanonicalSkillUsageContext = domaincandidate.CanonicalSkillUsageContext
type CanonicalSkillUse = domaincandidate.CanonicalSkillUse
type CanonicalCandidateProject = domaincandidate.CanonicalCandidateProject
type CanonicalCandidatePreference = domaincandidate.CanonicalCandidatePreference
type CanonicalCandidateConstraint = domaincandidate.CanonicalCandidateConstraint
type CanonicalCandidateStory = domaincandidate.CanonicalCandidateStory
type CanonicalCandidateClaim = domaincandidate.CanonicalCandidateClaim
type CanonicalClaimPolarity = domaincandidate.CanonicalClaimPolarity
type CanonicalClaimState = domaincandidate.CanonicalClaimState

const (
	CanonicalSkillUsageCommercial        = domaincandidate.CanonicalSkillUsageCommercial
	CanonicalSkillUsagePetProject        = domaincandidate.CanonicalSkillUsagePetProject
	CanonicalSkillUsageEducational       = domaincandidate.CanonicalSkillUsageEducational
	CanonicalSkillUsagePersonal          = domaincandidate.CanonicalSkillUsagePersonal
	CanonicalSkillUsageStudiedOnly       = domaincandidate.CanonicalSkillUsageStudiedOnly
	CanonicalSkillUsageUnknown           = domaincandidate.CanonicalSkillUsageUnknown
	CanonicalSkillUsageExplicitlyNotUsed = domaincandidate.CanonicalSkillUsageExplicitlyNotUsed
	CanonicalClaimPositive               = domaincandidate.CanonicalClaimPositive
	CanonicalClaimNegative               = domaincandidate.CanonicalClaimNegative
	CanonicalClaimActive                 = domaincandidate.CanonicalClaimActive
	CanonicalClaimDisputed               = domaincandidate.CanonicalClaimDisputed
	CanonicalClaimSuperseded             = domaincandidate.CanonicalClaimSuperseded
)

// CanonicalCandidateInput intentionally remains root-owned: it joins the
// legacy profile, storage aggregate and HH parsing values at the composition
// boundary. The Candidate result itself is the domain-owned alias above.
type CanonicalCandidateInput struct {
	Profile     CandidateProfile
	Knowledge   CandidateKnowledgeBase
	Stories     []CandidateStory
	ResumeFacts *ResumeFacts
	CandidateID string
	Contacts    string
	GitHubURL   string
}

type CanonicalCandidateDiagnostics = domaincandidate.CanonicalCandidateDiagnostics
