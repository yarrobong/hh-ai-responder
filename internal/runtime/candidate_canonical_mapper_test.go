package runtime

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func canonicalTimePtr(value time.Time) *time.Time { return &value }
func canonicalFloatPtr(value float64) *float64    { return &value }

func canonicalTestMetadata(source KnowledgeSource, status TruthStatus, at time.Time, evidence ...string) KnowledgeMetadata {
	metadata := KnowledgeMetadata{
		TruthStatus: status,
		Sources:     []KnowledgeSourceRecord{{Type: source, Evidence: append([]string{}, evidence...), ObservedAt: canonicalTimePtr(at)}},
		Evidence:    append([]string{}, evidence...),
		CreatedAt:   at,
		UpdatedAt:   at,
	}
	if status == TruthStatusConfirmed {
		metadata.ConfirmedAt = canonicalTimePtr(at)
	}
	return metadata
}

func canonicalTestProfileFact(source CandidateSource, confirmed bool, at time.Time, evidence ...string) ProfileFact {
	return ProfileFact{Source: source, Confirmed: confirmed, ConfirmedAt: at, Evidence: append([]string{}, evidence...)}
}

func canonicalTestProfile(at time.Time) CandidateProfile {
	profile := NewCandidateProfile(at)
	profile.Identity.FullName = ProfileStringFact{Value: "Тестовый кандидат", ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user input")}
	profile.Identity.Location = ProfileStringFact{Value: "Екатеринбург", ProfileFact: canonicalTestProfileFact(CandidateSourceHHResume, true, at, "HH structured location")}
	profile.Education = []EducationFact{{Level: "higher", Institution: "Университет", Specialty: "Информатика", Details: "очное обучение", ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "diploma")}}
	profile.WorkExperience = []WorkExperienceFact{{Company: "Example", Role: "Integration specialist", Description: "Raw description with line\nbreak", Achievements: "One opaque legacy narrative; do not split", StartDate: "2020-01", EndDate: "2022-02", ProfileFact: canonicalTestProfileFact(CandidateSourceHHResume, true, at, "HH experience")}}
	profile.Projects = []ProjectFact{{Name: "Legacy project", Role: "developer", Description: "project text", Technologies: []string{"Go", "PostgreSQL"}, BusinessImpact: "business result", ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user project")}}
	profile.Skills = []CandidateSkill{
		{Name: "Go", Level: SkillLevelAdvanced, ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user skill")},
		{Name: "Redis", Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUnknown, Evidence: []string{"not established"}}},
	}
	profile.Languages = []LanguageFact{{Name: "English", Level: "B1", ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user language")}}
	profile.TotalExperienceMonths = ProfileIntFact{Value: 26, ProfileFact: canonicalTestProfileFact(CandidateSourceHHResume, true, at, "HH structured total")}
	profile.WorkPreferences.WorkMode = ProfileStringFact{Value: "remote", ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user preference")}
	profile.WorkPreferences.SalaryMinimum = ProfileIntFact{Value: 120000, ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user preference")}
	profile.EmployerCommunicationPreferences.AvoidClaiming = ProfileListFact{Values: []string{"Kubernetes"}, ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user restriction")}
	profile.UnknownPendingFacts = []PendingProfileQuestion{{Topic: "employment_type", Question: "Какой тип занятости подтверждён?", Reason: "legacy profile is silent", CreatedAt: at}}
	return profile
}

func TestBuildCanonicalCandidateProfileOnlyIsDeterministicAndLossless(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	profile := canonicalTestProfile(at)
	input := CanonicalCandidateInput{
		Profile:     profile,
		Stories:     []CandidateStory{{Title: "Example story", Situation: "Situation", Skills: []string{"Redis"}, ProfileRefs: []string{"missing-ref"}}},
		ResumeFacts: &ResumeFacts{ExperienceText: "structured HH experience", EducationKnown: true, EducationLevel: "higher", EducationDetails: "details", TotalExperienceMonthsKnown: true, TotalExperienceMonths: 26},
		CandidateID: "candidate-1",
		Contacts:    "email@example.test; +7 900 000-00-00",
		GitHubURL:   "https://github.com/example",
	}
	original, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want, diagnostics, err := BuildCanonicalCandidate(input)
	if err != nil {
		t.Fatal(err)
	}
	got, diagnosticsAgain, err := BuildCanonicalCandidate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(diagnostics, diagnosticsAgain) {
		t.Fatal("same logical input must produce a deep-equivalent candidate and diagnostics")
	}
	if !reflect.DeepEqual(want.ResumeFacts, input.ResumeFacts) {
		t.Fatal("resume facts were not preserved")
	}
	if want.Identity.FullName != profile.Identity.FullName.Value || want.Identity.Location != profile.Identity.Location.Value {
		t.Fatal("identity was not mapped losslessly")
	}
	if len(want.Experience) != 1 || len(want.Experience[0].Achievements) != 1 || want.Experience[0].Achievements[0] != profile.WorkExperience[0].Achievements {
		t.Fatal("legacy experience narrative was split or changed")
	}
	if findCanonicalSkill(want, "Go").Metadata.TruthStatus != TruthStatusConfirmed {
		t.Fatal("user-confirmed skill was not kept confirmed")
	}
	if !containsCanonicalSkill(want, "Redis") || findCanonicalSkill(want, "Redis").Negative {
		t.Fatal("unknown skill became a negative fact")
	}
	if findCanonicalSkill(want, "Redis").Metadata.TruthStatus != TruthStatusUnknown {
		t.Fatal("unknown skill changed truth status")
	}
	if want.UpdatedAt != at || findCanonicalExperience(want, profile.WorkExperience[0].Company).Metadata.TruthStatus != TruthStatusVerified {
		t.Fatal("persistent timestamp or HH truth status was not preserved")
	}
	if len(want.Contacts) != 1 || want.Contacts[0].Metadata.TruthStatus != TruthStatusUnknown {
		t.Fatal("config contacts were incorrectly promoted to trusted knowledge")
	}
	if len(want.ExternalReferences) != 1 || want.ExternalReferences[0].Metadata.TruthStatus != TruthStatusUnknown {
		t.Fatal("GitHub config input was incorrectly promoted to trusted knowledge")
	}
	if !containsPreference(want.Preferences, "salary_minimum") || containsConstraintKind(want.Constraints, "salary_minimum") {
		t.Fatal("ambiguous salary minimum was strengthened into a hard constraint")
	}
	if len(want.Stories) != 1 || len(want.Claims) == 0 || containsClaimSubject(want.Claims, "story", want.Stories[0].ID) {
		t.Fatal("story narrative was incorrectly converted into a fact claim")
	}
	if len(diagnostics.UnsupportedStoryRefs) != 1 || diagnostics.UnsupportedStoryRefs[0] != "missing-ref" {
		t.Fatal("unsupported story reference was not diagnosed")
	}

	after, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(after) {
		t.Fatal("mapper mutated its input")
	}
	mutated := findCanonicalSkill(want, "Go")
	mutated.CannotClaim = append(mutated.CannotClaim, "mutated output")
	afterOutputMutation, err := json.Marshal(input)
	if err != nil || string(original) != string(afterOutputMutation) {
		t.Fatal("mutating canonical output changed mapper input")
	}
	reordered := input
	reordered.Profile, err = cloneKnowledge(input.Profile)
	if err != nil {
		t.Fatal(err)
	}
	reordered.Profile.Skills[0], reordered.Profile.Skills[1] = reordered.Profile.Skills[1], reordered.Profile.Skills[0]
	reorderedCandidate, reorderedDiagnostics, err := BuildCanonicalCandidate(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, reorderedCandidate) || !reflect.DeepEqual(diagnosticsAgain, reorderedDiagnostics) {
		t.Fatal("non-semantic input ordering changed canonical output")
	}
}

func TestBuildCanonicalCandidatePreservesDetailedKBAndSafeProjectionParity(t *testing.T) {
	at := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	profile := canonicalTestProfile(at)
	metadata := canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "user confirmed detailed evidence")
	detailedSkill := CandidateSkillDetailed{ID: "skill-go-1", Name: "Go", Category: "backend", Level: SkillLevelAdvanced, Projects: []string{"project-1"}, CanDo: []string{"write integrations"}, CannotClaim: []string{"Kubernetes"}, LastUsed: "2026", KnowledgeMetadata: metadata}
	detailedProject := CandidateProject{ID: "project-1", Name: "Detailed project", Type: CandidateProjectCommercial, Role: "backend", Period: CandidateProjectPeriod{Start: "2024", End: "2025"}, Description: "detailed description", Technologies: []string{"Go"}, Tasks: []string{"integrate API"}, Results: []string{"delivered"}, RelatedSkills: []string{"skill-go-1"}, KnowledgeMetadata: metadata}
	detailedAchievement := CandidateAchievement{ID: "achievement-1", Title: "Reduced manual work", Problem: "manual process", Actions: []string{"automated it"}, Result: []string{"fewer manual steps"}, Technologies: []string{"Go"}, ProjectID: detailedProject.ID, KnowledgeMetadata: metadata}
	unknownMeta := canonicalTestMetadata(KnowledgeSourceDerived, TruthStatusUnknown, at, "not confirmed")
	unknown := CandidateUnknown{ID: "unknown-1", Question: "Is Kubernetes used?", Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: unknownMeta}
	event := CandidateKnowledgeEvent{ID: "event-1", Timestamp: at, Action: "import", EntityType: "skill", EntityID: detailedSkill.ID, NewValue: json.RawMessage(`{"id":"skill-go-1"}`), Source: KnowledgeSourceGithubVerified, Actor: "test"}
	proposalSkill := CandidateSkillDetailed{ID: "skill-proposed", Name: "Python", Level: SkillLevelWorking, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceProjectAnalysis, TruthStatusHypothesis, at, "analysis")}
	proposalRaw, err := json.Marshal(proposalSkill)
	if err != nil {
		t.Fatal(err)
	}
	proposal := KnowledgeProposal{ID: "proposal-1", EntityType: "skill", EntityID: proposalSkill.ID, ProposedValue: proposalRaw, Reason: "analysis suggests it", Source: KnowledgeSourceProjectAnalysis, Status: KnowledgeProposalPending, CreatedAt: at}
	proposal.Confidence = canonicalFloatPtr(0.5)
	proposalSkill.Confidence = proposal.Confidence
	proposalRaw, err = json.Marshal(proposalSkill)
	if err != nil {
		t.Fatal(err)
	}
	proposal.ProposedValue = proposalRaw
	kb := CandidateKnowledgeBase{Profile: profile, Skills: []CandidateSkillDetailed{detailedSkill}, Projects: []CandidateProject{detailedProject}, Achievements: []CandidateAchievement{detailedAchievement}, Unknowns: []CandidateUnknown{unknown}, Proposals: []KnowledgeProposal{proposal}, Events: []CandidateKnowledgeEvent{event}}

	canonical, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, Knowledge: kb})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Conflicts) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	goSkill := findCanonicalSkill(canonical, "Go")
	if goSkill.ID != detailedSkill.ID || !containsString(goSkill.SourceIDs, detailedSkill.ID) || !containsString(goSkill.CannotClaim, "Kubernetes") || goSkill.Uses[0].ProjectID != detailedProject.ID {
		t.Fatalf("detailed skill was not preserved: %#v", goSkill)
	}
	if goSkill.Uses[0].Context != CanonicalSkillUsageUnknown {
		t.Fatalf("project relation was incorrectly promoted to commercial usage: %#v", goSkill.Uses[0])
	}
	if len(canonical.Unknowns) != 2 || len(canonical.Proposals) != 1 || len(canonical.Events) != 1 {
		t.Fatal("unknowns, proposals or events were lost")
	}
	current, err := kb.GetEmployerSafeCandidateKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	projected, err := CanonicalEmployerSafeProjection(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, projected) {
		t.Fatalf("canonical safe projection diverges from current projection\ncurrent=%#v\ncanonical=%#v", current, projected)
	}
}

func TestBuildCanonicalCandidateConflictsAreVisibleAndFailClosed(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Skills = []CandidateSkill{{Name: "Docker", Level: SkillLevelAdvanced, ProfileFact: canonicalTestProfileFact(CandidateSourceUserConfirmed, true, at, "user says advanced")}}
	negative := CandidateSkillDetailed{ID: "docker-negative", Name: "Docker", Level: SkillLevelUnknown, Negative: true, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "user explicitly did not use it")}
	positive := CandidateSkillDetailed{ID: "docker-positive", Name: "Docker", Level: SkillLevelBasic, KnowledgeMetadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, "old detailed assertion")}
	canonical, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile, Knowledge: CandidateKnowledgeBase{Skills: []CandidateSkillDetailed{positive, negative}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Conflicts) == 0 || !containsSubstring(diagnostics.Conflicts, "docker") {
		t.Fatalf("positive/negative conflict was hidden: %#v", diagnostics)
	}
	skill := findCanonicalSkill(canonical, "Docker")
	if !skill.Negative || skill.Level != SkillLevelUnknown || skill.State != CanonicalClaimDisputed {
		t.Fatalf("conflict did not fail closed: %#v", skill)
	}
	if !containsString(skill.SourceIDs, negative.ID) || !containsString(skill.SourceIDs, positive.ID) {
		t.Fatal("conflicting source IDs were lost")
	}
}

func TestBuildCanonicalCandidateRejectsInvalidInput(t *testing.T) {
	at := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	profile := NewCandidateProfile(at)
	profile.Skills = []CandidateSkill{{Name: "Go", Level: SkillLevel("not-a-level")}}
	if _, _, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: profile}); err == nil {
		t.Fatal("invalid profile input was accepted")
	}
	validProfile := NewCandidateProfile(at)
	invalidMetadata := KnowledgeMetadata{TruthStatus: TruthStatusVerified, Sources: []KnowledgeSourceRecord{{Type: KnowledgeSourceHHResume, Evidence: []string{"e"}}}}
	invalidSkill := CandidateSkillDetailed{ID: "bad", Name: "Go", Level: SkillLevelWorking, KnowledgeMetadata: invalidMetadata}
	if _, _, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: validProfile, Knowledge: CandidateKnowledgeBase{Skills: []CandidateSkillDetailed{invalidSkill}}}); err == nil {
		t.Fatal("invalid detailed metadata was accepted")
	}
}

func findCanonicalSkill(candidate Candidate, name string) CanonicalCandidateSkill {
	for _, skill := range candidate.Skills {
		if strings.EqualFold(skill.Name, name) {
			return skill
		}
	}
	return CanonicalCandidateSkill{}
}

func containsCanonicalSkill(candidate Candidate, name string) bool {
	return findCanonicalSkill(candidate, name).Name != ""
}
func findCanonicalExperience(candidate Candidate, company string) CanonicalCandidateExperience {
	for _, experience := range candidate.Experience {
		if experience.Company == company {
			return experience
		}
	}
	return CanonicalCandidateExperience{}
}
func containsClaimSubject(claims []CanonicalCandidateClaim, kind, id string) bool {
	for _, claim := range claims {
		if claim.SubjectType == kind && claim.SubjectID == id {
			return true
		}
	}
	return false
}
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func containsPreference(values []CanonicalCandidatePreference, kind string) bool {
	for _, value := range values {
		if value.Kind == kind {
			return true
		}
	}
	return false
}
func containsConstraintKind(values []CanonicalCandidateConstraint, kind string) bool {
	for _, value := range values {
		if value.Kind == kind {
			return true
		}
	}
	return false
}
func containsSubstring(values []string, wanted string) bool {
	for _, value := range values {
		if strings.Contains(value, wanted) {
			return true
		}
	}
	return false
}
