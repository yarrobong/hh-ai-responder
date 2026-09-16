package careeragent

import (
	"context"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
)

func TestNormalizeResumesUsesStableIDsAndRegistryOverrides(t *testing.T) {
	values := NormalizeResumes([]candidate.ResumeItem{{Id: 2, Hash: "b", Title: "Support"}, {Id: 1, Hash: "a", Title: "Python"}})
	if len(values) != 2 || values[0].ID != "hh-resume-a" || values[1].ID != "hh-resume-b" {
		t.Fatalf("unexpected normalized registry: %+v", values)
	}
	values = ApplyRegistryOverrides(values, RegistryOverrides{Enabled: map[string]bool{"hh-resume-a": false}})
	if values[0].Enabled {
		t.Fatal("disabled resume override was not applied")
	}
}

func TestPlanSearchesDeduplicatesQueriesAndSkipsDisabledResumes(t *testing.T) {
	resumes := []ResumeProfile{{ID: "r1", Hash: "hash", Title: "Python Django developer", DesiredRole: "Python Django developer", Enabled: true}, {ID: "r2", Title: "Support", Enabled: false}}
	profiles := PlanSearches(resumes, CandidateSignals{}, SearchConstraints{MaxProfiles: 8, SearchPeriodDays: 3})
	if len(profiles) == 0 || len(profiles) > 8 {
		t.Fatalf("unexpected planned profiles: %+v", profiles)
	}
	seen := map[string]bool{}
	for _, profile := range profiles {
		if seen[profile.Query] {
			t.Fatalf("duplicate query %q", profile.Query)
		}
		seen[profile.Query] = true
		if profile.Params.Get("search_period") != "3" || profile.Params.Get("resume") != "hash" {
			t.Fatalf("planner params not normalized: %+v", profile.Params)
		}
	}
}

func TestPlanSearchesKeepsSameQueryWhenResumeFilterDiffers(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "a", Hash: "hash-a", Title: "Python backend", Enabled: true}, {ID: "b", Hash: "hash-b", Title: "Python backend", Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 8, SearchPeriodDays: 7})
	byQuery := map[string]map[string]bool{}
	for _, profile := range profiles {
		if byQuery[profile.Query] == nil {
			byQuery[profile.Query] = map[string]bool{}
		}
		byQuery[profile.Query][profile.Params.Get("resume")] = true
	}
	kept := false
	for _, resumes := range byQuery {
		if len(resumes) > 1 {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("same query with distinct resume filters must remain distinct profiles: %+v", profiles)
	}
}

func TestPlanSearchesRoundRobinsEnabledResumesBeforeSecondDirection(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "support", Hash: "support-hash", Title: "Technical support", SearchHints: []string{"application support"}, Enabled: true},
		{ID: "python", Hash: "python-hash", Title: "Python backend", SearchHints: []string{"Django backend"}, Enabled: true},
		{ID: "integration", Hash: "integration-hash", Title: "Integration specialist", SearchHints: []string{"API integration"}, Enabled: true},
	}
	profiles := PlanSearches(resumes, CandidateSignals{}, SearchConstraints{MaxProfiles: 5})
	if len(profiles) != 5 {
		t.Fatalf("profiles=%d, want 5: %+v", len(profiles), profiles)
	}
	seenResumes := map[string]bool{}
	for _, profile := range profiles[:3] {
		seenResumes[profile.ResumeID] = true
	}
	if len(seenResumes) != 3 {
		t.Fatalf("profile budget was dominated by one resume: %+v", profiles)
	}
	for _, profile := range profiles {
		if profile.Reason == "" {
			t.Fatalf("profile reason is empty: %+v", profile)
		}
	}
}

func TestPlanSearchesCarriesConstraintsAndSkipsExcludedQuery(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "r1", Title: "Python developer", Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 8, SearchPeriodDays: 3, IncludeKeywords: []string{"Django"}, ExcludeKeywords: []string{"developer"}})
	for _, profile := range profiles {
		if profile.Query == "Python developer" {
			t.Fatalf("excluded query should not be planned: %+v", profiles)
		}
	}
	profiles = PlanSearches([]ResumeProfile{{ID: "r1", Title: "Python backend", Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 8, SearchPeriodDays: 3, IncludeKeywords: []string{"Django", "Django"}})
	if len(profiles) == 0 || profiles[0].Params.Get("career_agent_include") != "Django" {
		t.Fatalf("normalized include constraint missing: %+v", profiles)
	}
}

func TestRouteResumePrefersHardSkillAndReviewsCloseChoice(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"SQL"}, Enabled: true},
	}
	selected := RouteResume(VacancyInput{ID: 10, Title: "Python Django backend", RequiredSkills: []string{"Django"}}, resumes)
	if selected.Status != RouteSelected || selected.SelectedResumeID != "python" || selected.Confidence == ConfidenceLow {
		t.Fatalf("unexpected route: %+v", selected)
	}
	unknown := RouteResume(VacancyInput{ID: 12, Title: "Python backend", RequiredSkills: []string{"Kubernetes"}}, resumes)
	if unknown.Status != RouteSelected || allRequirementsMet(unknown.HardRequirements) {
		t.Fatalf("unknown hard requirement should remain explicit: %+v", unknown)
	}
	close := RouteResume(VacancyInput{ID: 11, Title: "SQL support", RequiredSkills: []string{"SQL"}}, []ResumeProfile{{ID: "a", Title: "Support", Skills: []string{"SQL"}, Enabled: true}, {ID: "b", Title: "Support", Skills: []string{"SQL"}, Enabled: true}})
	if close.Status != RouteReviewRequired {
		t.Fatalf("close route should require review: %+v", close)
	}
	if noSignal := RouteResume(VacancyInput{ID: 13, Title: "Unrelated role"}, resumes); noSignal.Status != RouteReviewRequired {
		t.Fatalf("unrelated role should require review: %+v", noSignal)
	}
}

func TestRouteResumeDoesNotLetExplicitHardBlockerLoseToScore(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "blocked", Title: "Python backend", Skills: []string{"Python"}, ExcludeKeywords: []string{"office"}, Enabled: true},
		{ID: "compatible", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
	}
	decision := RouteResume(VacancyInput{ID: 99, Title: "Python backend", Description: "office role"}, resumes)
	if decision.Status != RouteSelected || decision.SelectedResumeID != "compatible" {
		t.Fatalf("hard-blocked high score won routing: %+v", decision)
	}
	for _, score := range decision.AlternativeScores {
		if score.ResumeID == "blocked" && len(score.HardBlockers) == 0 {
			t.Fatalf("hard blocker was not retained in alternatives: %+v", decision)
		}
	}
	for _, resume := range resumes {
		resume.Enabled = false
		if got := RouteResume(VacancyInput{ID: 100, Title: "Python backend"}, []ResumeProfile{resume}); got.Status != RouteNoResume {
			t.Fatalf("disabled resume was selectable: %+v", got)
		}
	}
}

func TestRouteResumeDoesNotInferRelocationIncompatibilityFromResumeCity(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 101, Title: "Python backend", Location: "Москва"}, []ResumeProfile{{ID: "r", Title: "Python backend", Location: "Екатеринбург", Skills: []string{"Python"}, Enabled: true}})
	if decision.Status != RouteSelected {
		t.Fatalf("resume city was incorrectly treated as a hard blocker: %+v", decision)
	}
}

type fakeSearcher struct {
	pages map[string][]SearchPage
	calls int
}

func (f *fakeSearcher) Search(_ context.Context, profile SearchProfile, cursor string) (SearchPage, error) {
	f.calls++
	pages := f.pages[profile.ID]
	if cursor == "" {
		return pages[0], nil
	}
	return pages[1], nil
}

func TestDiscoverPaginatesDeduplicatesAndRetainsSources(t *testing.T) {
	profiles := []SearchProfile{{ID: "one"}, {ID: "two"}}
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	searcher := &fakeSearcher{pages: map[string][]SearchPage{
		"one": {{Items: []VacancyCandidate{{ID: 1, Title: "Python", PublishedAt: now}}, NextCursor: "next"}, {Items: []VacancyCandidate{{ID: 2, Title: "Support", PublishedAt: now}}}},
		"two": {{Items: []VacancyCandidate{{ID: 1, Title: "Duplicate", PublishedAt: now}, {ID: 3, Title: "Other", PublishedAt: now}}}},
	}}
	result, err := Discover(context.Background(), profiles, searcher, DiscoveryOptions{Now: func() time.Time { return now }, MaxVacancies: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.RawResults != 4 || result.Summary.Unique != 3 || result.Summary.Pages != 3 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	if len(result.Items[0].SearchProfileIDs) != 2 {
		t.Fatalf("duplicate source provenance lost: %+v", result.Items[0])
	}
}

func TestFeedbackIDIsStableAndNonEmpty(t *testing.T) {
	value := Feedback{VacancyID: 123, ResumeID: "resume", Type: FeedbackGoodMatch}
	first, second := FeedbackID(value), FeedbackID(value)
	if first == "" || first != second {
		t.Fatalf("feedback id is not stable: %q / %q", first, second)
	}
}
