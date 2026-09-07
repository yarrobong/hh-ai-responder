package main

import "time"

// Candidate is the in-memory canonical candidate aggregate for the migration
// boundary. It is a derived runtime read model; legacy stores remain the
// writers. Source-specific assertions remain visible through Claims and
// SourceIDs.
type Candidate struct {
	Version            int                            `json:"version"`
	ID                 string                         `json:"id"`
	CreatedAt          time.Time                      `json:"created_at,omitempty"`
	UpdatedAt          time.Time                      `json:"updated_at,omitempty"`
	Identity           CanonicalCandidateIdentity     `json:"identity"`
	Contacts           []CanonicalCandidateContact    `json:"contacts,omitempty"`
	ExternalReferences []CanonicalExternalReference   `json:"external_references,omitempty"`
	Education          []CanonicalCandidateEducation  `json:"education,omitempty"`
	Languages          []CanonicalCandidateLanguage   `json:"languages,omitempty"`
	Experience         []CanonicalCandidateExperience `json:"experience,omitempty"`
	Skills             []CanonicalCandidateSkill      `json:"skills,omitempty"`
	Projects           []CanonicalCandidateProject    `json:"projects,omitempty"`
	Achievements       []CandidateAchievement         `json:"achievements,omitempty"`
	Preferences        []CanonicalCandidatePreference `json:"preferences,omitempty"`
	Constraints        []CanonicalCandidateConstraint `json:"constraints,omitempty"`
	Stories            []CanonicalCandidateStory      `json:"stories,omitempty"`
	Claims             []CanonicalCandidateClaim      `json:"claims,omitempty"`
	Unknowns           []CandidateUnknown             `json:"unknowns,omitempty"`
	Proposals          []KnowledgeProposal            `json:"proposals,omitempty"`
	Events             []CandidateKnowledgeEvent      `json:"events,omitempty"`
	Profile            CanonicalProfileSnapshot       `json:"profile_snapshot"`
	ResumeFacts        *ResumeFacts                   `json:"resume_facts,omitempty"`
}

type CanonicalProfileSnapshot struct {
	TotalExperienceMonths ProfileIntFact                   `json:"total_experience_months"`
	WorkPreferences       WorkPreferences                  `json:"work_preferences"`
	Communication         EmployerCommunicationPreferences `json:"communication_preferences"`
}

type CanonicalCandidateIdentity struct {
	FullName         string            `json:"full_name,omitempty"`
	FullNameMetadata KnowledgeMetadata `json:"full_name_metadata"`
	Location         string            `json:"location,omitempty"`
	LocationMetadata KnowledgeMetadata `json:"location_metadata"`
}

type CanonicalCandidateContact struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Value    string            `json:"value"`
	Metadata KnowledgeMetadata `json:"metadata"`
}

type CanonicalExternalReference struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	URL      string            `json:"url"`
	Metadata KnowledgeMetadata `json:"metadata"`
}

type CanonicalCandidateEducation struct {
	ID          string            `json:"id"`
	Level       string            `json:"level,omitempty"`
	Institution string            `json:"institution,omitempty"`
	Specialty   string            `json:"specialty,omitempty"`
	Details     string            `json:"details,omitempty"`
	ClaimID     string            `json:"claim_id,omitempty"`
	Metadata    KnowledgeMetadata `json:"metadata"`
}

type CanonicalCandidateLanguage struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Level    string            `json:"level,omitempty"`
	ClaimID  string            `json:"claim_id,omitempty"`
	Metadata KnowledgeMetadata `json:"metadata"`
}

type CanonicalCandidateExperience struct {
	ID               string              `json:"id"`
	Company          string              `json:"company,omitempty"`
	Position         string              `json:"position,omitempty"`
	StartDate        string              `json:"start_date,omitempty"`
	EndDate          string              `json:"end_date,omitempty"`
	EmploymentType   string              `json:"employment_type,omitempty"`
	Description      string              `json:"description,omitempty"`
	Responsibilities []string            `json:"responsibilities,omitempty"`
	SkillsUsed       []CanonicalSkillUse `json:"skills_used,omitempty"`
	Achievements     []string            `json:"achievements,omitempty"`
	StoryIDs         []string            `json:"story_ids,omitempty"`
	ClaimID          string              `json:"claim_id,omitempty"`
	Metadata         KnowledgeMetadata   `json:"metadata"`
}

type CanonicalCandidateSkill struct {
	ID               string                             `json:"id"`
	SourceIDs        []string                           `json:"source_ids,omitempty"`
	Name             string                             `json:"name"`
	DisplayName      string                             `json:"display_name,omitempty"`
	Level            SkillLevel                         `json:"level"`
	Category         string                             `json:"category,omitempty"`
	Capabilities     []CanonicalSkillCapability         `json:"capabilities,omitempty"`
	CannotClaim      []string                           `json:"cannot_claim,omitempty"`
	LastUsed         string                             `json:"last_used,omitempty"`
	Uses             []CanonicalSkillUse                `json:"uses,omitempty"`
	ClaimIDs         []string                           `json:"claim_ids,omitempty"`
	Negative         bool                               `json:"negative,omitempty"`
	State            CanonicalClaimState                `json:"state"`
	Metadata         KnowledgeMetadata                  `json:"metadata"`
	SourceAssertions []CanonicalCandidateSkillAssertion `json:"source_assertions,omitempty"`
}

type CanonicalCandidateSkillAssertion struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Detailed    bool              `json:"detailed"`
	Category    string            `json:"category,omitempty"`
	Level       SkillLevel        `json:"level"`
	Projects    []string          `json:"projects,omitempty"`
	CanDo       []string          `json:"can_do,omitempty"`
	CannotClaim []string          `json:"cannot_claim,omitempty"`
	LastUsed    string            `json:"last_used,omitempty"`
	Negative    bool              `json:"negative,omitempty"`
	Metadata    KnowledgeMetadata `json:"metadata"`
}

type CanonicalSkillCapability struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	ClaimID string `json:"claim_id,omitempty"`
}

type CanonicalSkillUsageContext string

const (
	CanonicalSkillUsageCommercial        CanonicalSkillUsageContext = "commercial"
	CanonicalSkillUsagePetProject        CanonicalSkillUsageContext = "pet_project"
	CanonicalSkillUsageEducational       CanonicalSkillUsageContext = "educational"
	CanonicalSkillUsagePersonal          CanonicalSkillUsageContext = "personal"
	CanonicalSkillUsageStudiedOnly       CanonicalSkillUsageContext = "studied_only"
	CanonicalSkillUsageUnknown           CanonicalSkillUsageContext = "unknown"
	CanonicalSkillUsageExplicitlyNotUsed CanonicalSkillUsageContext = "explicitly_not_used"
)

type CanonicalSkillUse struct {
	ID           string                     `json:"id"`
	SkillID      string                     `json:"skill_id,omitempty"`
	ProjectID    string                     `json:"project_id,omitempty"`
	ExperienceID string                     `json:"experience_id,omitempty"`
	Context      CanonicalSkillUsageContext `json:"context"`
	Evidence     []string                   `json:"evidence,omitempty"`
	ClaimID      string                     `json:"claim_id,omitempty"`
}

type CanonicalCandidateProject struct {
	ID             string                 `json:"id"`
	SourceIDs      []string               `json:"source_ids,omitempty"`
	Name           string                 `json:"name"`
	Type           CandidateProjectType   `json:"type,omitempty"`
	Role           string                 `json:"role,omitempty"`
	Period         CandidateProjectPeriod `json:"period"`
	Description    string                 `json:"description,omitempty"`
	Technologies   []string               `json:"technologies,omitempty"`
	Tasks          []string               `json:"tasks,omitempty"`
	Results        []string               `json:"results,omitempty"`
	RelatedSkills  []string               `json:"related_skills,omitempty"`
	SkillUses      []CanonicalSkillUse    `json:"skill_uses,omitempty"`
	AchievementIDs []string               `json:"achievement_ids,omitempty"`
	StoryIDs       []string               `json:"story_ids,omitempty"`
	ClaimIDs       []string               `json:"claim_ids,omitempty"`
	Metadata       KnowledgeMetadata      `json:"metadata"`
	ProfileSource  *ProjectFact           `json:"profile_source,omitempty"`
	DetailedSource *CandidateProject      `json:"detailed_source,omitempty"`
}

type CanonicalCandidatePreference struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Value    string            `json:"value"`
	ClaimID  string            `json:"claim_id,omitempty"`
	Metadata KnowledgeMetadata `json:"metadata"`
}

type CanonicalCandidateConstraint struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Value    string            `json:"value"`
	ClaimID  string            `json:"claim_id,omitempty"`
	Metadata KnowledgeMetadata `json:"metadata"`
}

// CandidateStory is legacy input and remains unchanged. CanonicalCandidateStory
// preserves narrative text without treating it as proof of a fact.
type CanonicalCandidateStory struct {
	ID            string   `json:"id,omitempty"`
	Title         string   `json:"title"`
	Situation     string   `json:"situation,omitempty"`
	Context       string   `json:"context,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Description   string   `json:"description,omitempty"`
	Story         string   `json:"story,omitempty"`
	Task          string   `json:"task,omitempty"`
	Problem       string   `json:"problem,omitempty"`
	Action        string   `json:"action,omitempty"`
	Actions       string   `json:"actions,omitempty"`
	Contribution  string   `json:"contribution,omitempty"`
	Result        string   `json:"result,omitempty"`
	Outcome       string   `json:"outcome,omitempty"`
	Achievement   string   `json:"achievement,omitempty"`
	Achievements  string   `json:"achievements,omitempty"`
	Technologies  []string `json:"technologies,omitempty"`
	Skills        []string `json:"skills,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Relevance     []string `json:"relevance,omitempty"`
	RelevantFor   []string `json:"relevant_for,omitempty"`
	RelevantRoles []string `json:"relevant_roles,omitempty"`
	ProfileRefs   []string `json:"profile_refs,omitempty"`
}

type CanonicalClaimPolarity string

const (
	CanonicalClaimPositive CanonicalClaimPolarity = "positive"
	CanonicalClaimNegative CanonicalClaimPolarity = "negative"
)

type CanonicalClaimState string

const (
	CanonicalClaimActive     CanonicalClaimState = "active"
	CanonicalClaimDisputed   CanonicalClaimState = "disputed"
	CanonicalClaimSuperseded CanonicalClaimState = "superseded"
)

type CanonicalCandidateClaim struct {
	ID            string                 `json:"id"`
	SubjectType   string                 `json:"subject_type"`
	SubjectID     string                 `json:"subject_id"`
	Field         string                 `json:"field"`
	Value         string                 `json:"value"`
	Polarity      CanonicalClaimPolarity `json:"polarity"`
	State         CanonicalClaimState    `json:"state"`
	ConflictSetID string                 `json:"conflict_set_id,omitempty"`
	SupersedesID  string                 `json:"supersedes_id,omitempty"`
	Metadata      KnowledgeMetadata      `json:"metadata"`
}

// KnowledgeEvent is the canonical spelling while the existing storage type
// remains the compatibility implementation.
type KnowledgeEvent = CandidateKnowledgeEvent

type CanonicalCandidateInput struct {
	Profile     CandidateProfile
	Knowledge   CandidateKnowledgeBase
	Stories     []CandidateStory
	ResumeFacts *ResumeFacts
	CandidateID string
	Contacts    string
	GitHubURL   string
}

type CanonicalCandidateDiagnostics struct {
	Conflicts            []string `json:"conflicts,omitempty"`
	AmbiguousAliases     []string `json:"ambiguous_aliases,omitempty"`
	UnresolvedReferences []string `json:"unresolved_references,omitempty"`
	UnsupportedStoryRefs []string `json:"unsupported_story_references,omitempty"`
	LossyMappings        []string `json:"lossy_mappings,omitempty"`
}

func (d *CanonicalCandidateDiagnostics) addUnique(target *[]string, value string) {
	for _, existing := range *target {
		if existing == value {
			return
		}
	}
	*target = append(*target, value)
}
