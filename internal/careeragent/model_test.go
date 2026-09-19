package careeragent

import (
	"context"
	"strings"
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

func TestNormalizeResumesKeepsMultipleHashlessProviderIDsDistinct(t *testing.T) {
	values := NormalizeResumes([]candidate.ResumeItem{
		{ProviderID: "api-resume-id-123", Title: "Backend developer"},
		{ProviderID: "api-resume-id-456", Title: "Backend developer"},
	})
	if len(values) != 2 {
		t.Fatalf("normalized resumes=%+v, want two distinct provider resumes", values)
	}
	if values[0].ProviderID != "api-resume-id-123" || values[1].ProviderID != "api-resume-id-456" || values[0].Hash != "" || values[1].Hash != "" {
		t.Fatalf("normalized provider identity was not preserved: %+v", values)
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

func TestPreliminaryRouteDefersIncompleteCardWithoutTerminalReview(t *testing.T) {
	decision := PreliminaryRouteResume(VacancyInput{
		ID: 201, Title: "Инженер по автоматизации интеграций", SearchProfiles: []SearchProfileEvidence{{ResumeID: "automation"}},
	}, []ResumeProfile{{ID: "automation", Title: "Automation / Integration specialist", Skills: []string{"API"}, Enabled: true}})
	if decision.Status != PreliminaryNeedsDetail || decision.ReasonCode != RouteReasonNeedsDetail {
		t.Fatalf("incomplete card was not deferred: %+v", decision)
	}
}

func TestPreliminaryRouteAllowsClearRouteOnlyWithCompleteStrongEvidence(t *testing.T) {
	decision := PreliminaryRouteResume(VacancyInput{
		ID: 202, Title: "Python backend developer", Description: "Python Django backend REST API developer", KeySkills: []string{"Python", "Django", "REST API"}, DetailAvailable: true,
	}, []ResumeProfile{{ID: "python", Title: "Python backend developer", Skills: []string{"Python", "Django", "REST API"}, Enabled: true}, {ID: "support", Title: "Technical support", Skills: []string{"SQL"}, Enabled: true}})
	if decision.Status != PreliminaryClearRoute {
		t.Fatalf("strong complete card was not clear-routed: %+v", decision)
	}
}

func TestAmbiguousCardBecomesClearOnlyAfterDetailEnrichment(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"SQL"}, Enabled: true},
	}
	card := VacancyInput{ID: 205, Title: "Backend specialist"}
	if got := PreliminaryRouteResume(card, resumes); got.Status != PreliminaryNeedsDetail {
		t.Fatalf("card should defer to detail: %+v", got)
	}
	detail := card
	detail.Description = "Python backend service development"
	detail.KeySkills = []string{"Python"}
	detail.DetailAvailable = true
	if got := RouteResume(detail, resumes); got.Status != RouteSelected || got.SelectedResumeID != "python" {
		t.Fatalf("detail did not clear route: %+v", got)
	}
}

func TestDetailStillAmbiguousRemainsReviewRequired(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		{ID: "b", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
	}
	got := RouteResume(VacancyInput{ID: 206, Title: "Python backend", Description: "Python backend API", DetailAvailable: true}, resumes)
	if got.Status != RouteReviewRequired || got.ReasonCode != RouteReasonAmbiguous {
		t.Fatalf("genuine close fit was not kept for review: %+v", got)
	}
}

func TestRussianEnglishCanonicalGroupsProduceExplainableScore(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 203, Title: "Разработчик бэкенда", Description: "Автоматизация и интеграция REST API"}, []ResumeProfile{{ID: "python", Title: "Python developer backend", Skills: []string{"automation", "integration", "API"}, Enabled: true}})
	if decision.Status != RouteSelected || decision.Score < 12 {
		t.Fatalf("canonical RU/EN route failed: %+v", decision)
	}
	joined := strings.Join(decision.AlternativeScores[0].Reasons, " ")
	for _, expected := range []string{"skill:", "role/title overlap"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("score explanation lacks %q: %+v", expected, decision.AlternativeScores[0])
		}
	}
}

func TestSearchProfileProvenanceIsSoftOnly(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 204, Title: "Unrelated role", SearchProfiles: []SearchProfileEvidence{{ResumeID: "python"}}}, []ResumeProfile{{ID: "python", Title: "Python developer", Enabled: true}, {ID: "support", Title: "Support", Enabled: true}})
	if decision.Status != RouteReviewRequired || decision.ReasonCode != RouteReasonLowEvidence || decision.SelectedResumeID != "" {
		t.Fatalf("provenance selected a resume without fit evidence: %+v", decision)
	}
}

func TestResumeScoringDoesNotLoseOrderingThroughCap(t *testing.T) {
	vacancy := VacancyInput{Title: "Python backend developer", Description: "Python Django PostgreSQL REST API Docker Redis Linux"}
	strong := scoreResume(vacancy, ResumeProfile{ID: "strong", Title: "Python backend", Skills: []string{"Python", "Django", "PostgreSQL", "REST API", "Docker", "Redis", "Linux"}})
	weak := scoreResume(vacancy, ResumeProfile{ID: "weak", Title: "Developer", Skills: []string{"Python"}})
	if strong.RawFit <= weak.RawFit || strong.Score <= weak.Score {
		t.Fatalf("scoring lost ordering: strong=%+v weak=%+v", strong, weak)
	}
	if strong.Score == 100 && weak.Score == 100 {
		t.Fatalf("normalized score still saturated for distinct fits: strong=%+v weak=%+v", strong, weak)
	}
}

func TestResumeScoringSpecificSkillBeatsGenericToken(t *testing.T) {
	vacancy := VacancyInput{Title: "Developer", Description: "Python service"}
	specific := scoreResume(vacancy, ResumeProfile{ID: "specific", Title: "Developer", Skills: []string{"Python"}})
	generic := scoreResume(vacancy, ResumeProfile{ID: "generic", Title: "Developer", Skills: []string{"Developer"}})
	if specific.SkillScore <= generic.SkillScore || specific.RawFit <= generic.RawFit {
		t.Fatalf("generic token was too strong: specific=%+v generic=%+v", specific, generic)
	}
}

func TestResumeScoringDistinguishesSkillMatchStrength(t *testing.T) {
	partial := scoreResume(VacancyInput{Title: "Backend", Description: "API"}, ResumeProfile{ID: "partial", Title: "Backend", Skills: []string{"REST API"}})
	exact := scoreResume(VacancyInput{Title: "Backend", Description: "REST API"}, ResumeProfile{ID: "exact", Title: "Backend", Skills: []string{"REST API"}})
	if partial.SkillScore >= exact.SkillScore || len(partial.SpecificMatches) != 0 {
		t.Fatalf("partial multi-token skill was treated as exact: partial=%+v exact=%+v", partial, exact)
	}
	if len(exact.SpecificMatches) != 1 || !strings.Contains(exact.SpecificMatches[0], "exact normalized phrase") {
		t.Fatalf("exact skill evidence was not recorded: %+v", exact)
	}
	technicalOnly := scoreResume(VacancyInput{Title: "Technical role"}, ResumeProfile{ID: "support", Title: "Support", Skills: []string{"Technical Support"}})
	if technicalOnly.SkillScore != 0 {
		t.Fatalf("single generic token incorrectly satisfied a skill phrase: %+v", technicalOnly)
	}
}

func TestResumeIdentityKeepsFourProfilesDistinct(t *testing.T) {
	profiles := NormalizeResumes([]candidate.ResumeItem{
		{Hash: "python", Title: "Backend-разработчик (Python/Django)", Skills: "Python, Django, PostgreSQL, REST API"},
		{Hash: "backend", Title: "Backend-разработчик", Skills: "PHP, Laravel, VueJS, SOAP"},
		{Hash: "automation", Title: "Специалист по автоматизации и интеграциям", Skills: "Python, API-интеграции, CRM, Webhooks"},
		{Hash: "support", Title: "Технический специалист", Skills: "Техническая поддержка, Диагностика неисправностей, Linux"},
	})
	if len(profiles) != 4 {
		t.Fatalf("profiles collapsed during normalization: %+v", profiles)
	}
	identities := map[string]ResumeIdentity{}
	for _, profile := range profiles {
		identities[profile.ID] = profile.Identity
	}
	seen := map[string]bool{}
	for _, identity := range identities {
		key := strings.Join(identity.PrimaryRoles, ",") + "|" + strings.Join(identity.StrongSkills, ",") + "|" + strings.Join(identity.DomainSignals, ",")
		seen[key] = true
	}
	if len(seen) != 4 {
		t.Fatalf("resume identities are not distinct: %+v", identities)
	}
}

func TestResumeRouterDifferentiatesPythonAndSupportVacancies(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Backend-разработчик (Python/Django)", Skills: []string{"Python", "Django", "PostgreSQL"}, Enabled: true},
		{ID: "support", Title: "Технический специалист", Skills: []string{"Техническая поддержка", "Диагностика неисправностей", "Linux"}, Enabled: true},
		{ID: "automation", Title: "Автоматизация и интеграции", Skills: []string{"API-интеграции", "CRM"}, Enabled: true},
		{ID: "backend", Title: "Backend-разработчик", Skills: []string{"PHP", "Laravel"}, Enabled: true},
	}
	python := RouteResume(VacancyInput{ID: 301, Title: "Python Backend Developer", Description: "Python Django PostgreSQL"}, resumes)
	if python.Status != RouteSelected || python.SelectedResumeID != "python" {
		t.Fatalf("Python vacancy was not differentiated: %+v", python)
	}
	support := RouteResume(VacancyInput{ID: 302, Title: "Инженер технической поддержки", Description: "Диагностика неисправностей Linux"}, resumes)
	if support.Status != RouteSelected || support.SelectedResumeID != "support" {
		t.Fatalf("support vacancy was not differentiated: %+v", support)
	}
}

func TestResumeRouterMeaningfulMarginsAndGenuineAmbiguity(t *testing.T) {
	selected := RouteResume(VacancyInput{ID: 303, Title: "Python Django backend", Description: "Python Django PostgreSQL"}, []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django", "PostgreSQL"}, Enabled: true},
		{ID: "other", Title: "Developer", Skills: []string{"Python"}, Enabled: true},
	})
	if selected.Status != RouteSelected || selected.AbsoluteMargin <= 0 || selected.RelativeMargin <= 0 || selected.TopRawScore <= selected.SecondRawScore {
		t.Fatalf("meaningful raw margin was not exposed: %+v", selected)
	}
	tie := RouteResume(VacancyInput{ID: 304, Title: "Python backend", Description: "Python API"}, []ResumeProfile{
		{ID: "a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		{ID: "b", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
	})
	if tie.Status != RouteReviewRequired || tie.ReasonCode != RouteReasonAmbiguous || tie.AbsoluteMargin != 0 {
		t.Fatalf("genuine tie was not kept ambiguous: %+v", tie)
	}
}

func TestObviousHardBlockedCardDoesNotNeedDetail(t *testing.T) {
	decision := PreliminaryRouteResume(VacancyInput{ID: 207, Title: "PHP developer"}, []ResumeProfile{{ID: "python", Title: "Python developer", ExcludeKeywords: []string{"PHP"}, Enabled: true}})
	if decision.Status != PreliminaryObviousReject {
		t.Fatalf("hard-blocked card requested detail: %+v", decision)
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
