package candidateparity

import (
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
)

func TestCompareCandidateSemanticTreatsEquivalentInstantsAsEqual(t *testing.T) {
	location := time.FixedZone("UTC+5", 5*60*60)
	left := candidate.Candidate{ID: "candidate-local", Version: 1, CreatedAt: time.Date(2026, 9, 10, 12, 0, 0, 123456789, location)}
	right := left
	right.CreatedAt = time.Date(2026, 9, 10, 7, 0, 0, 123456789, time.UTC)
	assertEquivalent(t, left, right)
}

func TestCompareCandidateSemanticRejectsDifferentInstant(t *testing.T) {
	left := candidate.Candidate{ID: "candidate-local", Version: 1, CreatedAt: time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC)}
	right := left
	right.CreatedAt = left.CreatedAt.Add(time.Nanosecond)
	assertDifferent(t, left, right, "candidate.created_at")
}

func TestCompareCandidateSemanticRejectsZeroTimestampAgainstInstant(t *testing.T) {
	left := candidate.Candidate{ID: "candidate-local", Version: 1, Events: []candidate.CandidateKnowledgeEvent{{ID: "event-1", Timestamp: time.Time{}, Action: "event", EntityType: "skill", EntityID: "skill-1"}}}
	right := left
	right.Events = append([]candidate.CandidateKnowledgeEvent(nil), left.Events...)
	right.Events[0].Timestamp = time.Unix(1, 0).UTC()
	assertDifferent(t, left, right, "candidate.events[0].timestamp")
}

func TestCompareCandidateSemanticDoesNotGuessHumanDateStrings(t *testing.T) {
	left := candidate.Candidate{ID: "candidate-local", Version: 1, Experience: []candidate.CanonicalCandidateExperience{{ID: "experience-1", StartDate: "2026-09-10T12:00:00+05:00"}}}
	right := left
	right.Experience = append([]candidate.CanonicalCandidateExperience(nil), left.Experience...)
	right.Experience[0].StartDate = "2026-09-10T07:00:00Z"
	assertDifferent(t, left, right, "candidate.experience.experience-1.start_date")
}

func TestCompareCandidateSemanticTreatsStoriesAsUnorderedIdentityCollection(t *testing.T) {
	left := candidate.Candidate{ID: "candidate-local", Version: 1, Stories: []candidate.CanonicalCandidateStory{
		{ID: "story-b", Title: "B", Summary: "Second story"},
		{ID: "story-a", Title: "A", Summary: "First story"},
	}}
	right := candidate.Candidate{ID: "candidate-local", Version: 1, Stories: []candidate.CanonicalCandidateStory{
		{ID: "story-a", Title: "A", Summary: "First story"},
		{ID: "story-b", Title: "B", Summary: "Second story"},
	}}
	assertEquivalent(t, left, right)
}

func TestCompareCandidateSemanticRejectsStoryContentChange(t *testing.T) {
	left := candidateWithStories(
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "First story"},
	)
	right := candidateWithStories(
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "Changed story"},
	)
	assertDifferent(t, left, right, "candidate.stories.story-a.summary")
}

func TestCompareCandidateSemanticRejectsMissingStory(t *testing.T) {
	left := candidateWithStories(
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "First story"},
		candidate.CanonicalCandidateStory{ID: "story-b", Title: "B", Summary: "Second story"},
	)
	right := candidateWithStories(
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "First story"},
	)
	assertDifferent(t, left, right, "candidate.stories.story-b")
}

func TestCompareCandidateSemanticRejectsDuplicateStoryIdentity(t *testing.T) {
	value := candidateWithStories(
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "First story"},
		candidate.CanonicalCandidateStory{ID: "story-a", Title: "A duplicate", Summary: "Duplicate story"},
	)
	result := CompareCandidateSemantic(value, value)
	if result.Equivalent || !hasDifference(result, "candidate.stories[story-a]", "duplicate_identity") {
		t.Fatalf("duplicate story identity was accepted: %+v", result)
	}
}

func TestCompareCandidateSemanticPreservesOrderedEventHistory(t *testing.T) {
	first := candidate.CandidateKnowledgeEvent{ID: "event-1", Timestamp: time.Unix(1, 0).UTC(), Action: "first", EntityType: "skill", EntityID: "skill-1"}
	second := candidate.CandidateKnowledgeEvent{ID: "event-2", Timestamp: time.Unix(2, 0).UTC(), Action: "second", EntityType: "skill", EntityID: "skill-1"}
	left := candidate.Candidate{ID: "candidate-local", Version: 1, Events: []candidate.CandidateKnowledgeEvent{first, second}}
	right := candidate.Candidate{ID: "candidate-local", Version: 1, Events: []candidate.CandidateKnowledgeEvent{second, first}}
	assertDifferent(t, left, right, "candidate.events[0].action")
}

func TestCompareCandidateSemanticRequiresProvenanceAndEvidenceEquality(t *testing.T) {
	metadata := candidate.KnowledgeMetadata{
		Sources:  []candidate.KnowledgeSourceRecord{{Type: candidate.KnowledgeSourceUserConfirmed, Reference: "profile", Evidence: []string{"confirmed by candidate"}}},
		Evidence: []string{"same evidence"},
	}
	left := candidateWithStories(candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "Story", Metadata: metadata})
	right := left
	right.Stories = append([]candidate.CanonicalCandidateStory(nil), left.Stories...)
	right.Stories[0].Metadata.Sources = append([]candidate.KnowledgeSourceRecord(nil), left.Stories[0].Metadata.Sources...)
	right.Stories[0].Metadata.Sources[0].Reference = "different-source"
	assertDifferent(t, left, right, "candidate.stories.story-a.metadata.sources[0].reference")

	right = left
	right.Stories = append([]candidate.CanonicalCandidateStory(nil), left.Stories...)
	right.Stories[0].Metadata.Evidence = append([]string(nil), left.Stories[0].Metadata.Evidence...)
	right.Stories[0].Metadata.Evidence = []string{"changed evidence"}
	assertDifferent(t, left, right, "candidate.stories.story-a.metadata.evidence[0]")
}

func TestCompareCandidateSemanticEquivalentPostgresRoundTripIsAlreadyMigrated(t *testing.T) {
	location := time.FixedZone("source", 5*60*60)
	source := candidate.Candidate{
		ID:        "candidate-local",
		Version:   1,
		UpdatedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, location),
		Stories: []candidate.CanonicalCandidateStory{
			{ID: "crm-integrations", Title: "CRM", Summary: "Integrated services"},
			{ID: "ekb-metro", Title: "Metro", Summary: "Improved support"},
		},
	}
	persisted := candidate.Candidate{
		ID:        "candidate-local",
		Version:   1,
		UpdatedAt: time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC),
		Stories: []candidate.CanonicalCandidateStory{
			{ID: "ekb-metro", Title: "Metro", Summary: "Improved support"},
			{ID: "crm-integrations", Title: "CRM", Summary: "Integrated services"},
		},
	}
	assertEquivalent(t, source, persisted)
}

func TestCompareCandidateSemanticChangedCandidateIsConflict(t *testing.T) {
	left := candidateWithStories(candidate.CanonicalCandidateStory{ID: "story-a", Title: "A", Summary: "Known fact"})
	right := left
	right.Identity.Location = "Different location"
	assertDifferent(t, left, right, "candidate.identity.location")
}

func candidateWithStories(stories ...candidate.CanonicalCandidateStory) candidate.Candidate {
	return candidate.Candidate{ID: "candidate-local", Version: 1, Stories: stories}
}

func assertEquivalent(t *testing.T, left, right candidate.Candidate) {
	t.Helper()
	result := CompareCandidateSemantic(left, right)
	if !result.Equivalent {
		t.Fatalf("candidates were not semantically equivalent: %+v", result)
	}
}

func assertDifferent(t *testing.T, left, right candidate.Candidate, path string) {
	t.Helper()
	result := CompareCandidateSemantic(left, right)
	if result.Equivalent {
		t.Fatalf("semantic difference was ignored: %+v", result)
	}
	if !hasPath(result, path) {
		t.Fatalf("difference path %q not reported: %+v", path, result)
	}
}

func hasPath(result Result, path string) bool {
	for _, difference := range result.Differences {
		if difference.Path == path {
			return true
		}
	}
	return false
}

func hasDifference(result Result, path, kind string) bool {
	for _, difference := range result.Differences {
		if difference.Path == path && difference.Kind == kind {
			return true
		}
	}
	return false
}
