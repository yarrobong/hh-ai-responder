# RESET-7 Search Planner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Calibrate the existing HH Search Planner so bounded discovery covers every eligible trusted role family, reports discovery separately from processed/router outcomes, and remains strictly read-only.

**Architecture:** Keep `careeragent.PlanSearches` as the single planner and extend its profile metadata, eligible-family derivation, canonicalization and fair budget allocation. Keep BrowserHHClient/auth, manual URL precedence, detail/preflight, RESET-6 routing, AI/local policy and all write paths unchanged; add only bounded read pagination and observational telemetry around the existing runtime discovery loop.

**Tech Stack:** Go 1.25, existing `internal/careeragent` planner/router types, existing HH web/browser read transport, `httptest.Server`, deterministic fixtures, mocked AI endpoints, JSON reports.

**Spec:** `docs/superpowers/specs/2026-09-19-reset-7-search-planner-design.md`

## Global Constraints

- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` for every RESET-7 validation command.
- Real HH writes: `0`; Application POST: `0`.
- Use the existing `careeragent.PlanSearches` architecture; do not create a second search mechanism.
- Manual `HH_SEARCH_URL` / `HH_SEARCH_URLS` precedence and parameter preservation remain unchanged.
- RESET-6 router policy, AI prompt, AI threshold, hard-requirement policy, detail/preflight, BrowserHHClient/auth, cookies, application send, nonce, reconciliation, write gateway and cover-letter generation are protected.
- `HH_MAX_SEARCH_PROFILES` remains the auto-profile budget and keeps default `16`.
- Add positive read-only bounds `HH_MAX_SEARCH_PAGES_PER_PROFILE=3` and `HH_MAX_SEARCH_PAGES_PER_RUN=48`.
- `processed_by_router` is distinct from `distinct_discovered`; router outcomes and router yields use only the processed population.
- `raw_hits - distinct_profile_vacancies` is not an overlap definition.
- Unvalidated provider `OR` semantics are never enabled in live planner output; fallback uses one proven trusted role phrase or is omitted.
- No planner self-learning, automatic profile deletion or automatic profile weighting is introduced.
- Canonical profile identity is based on role family, normalized query and provider-affecting parameters; `SourceResumeIDs` and eligibility metadata are merged after canonical dedup and never prevent collapse.
- Existing trusted `CandidateSignals` remain supporting evidence for bounded variants inside an already eligible family; they never activate a new family or create a generic profile.
- Production query candidates include bounded deterministic RU/EN aliases for every supported family; examples in this plan are not an exhaustive replacement for existing Russian role phrases.

## Review Focus

- A run discovers more vacancies than it processes; `distinct_discovered` must not become the router denominator. Test: `TestRunSummarySeparatesDiscoveryFromProcessedRouterPopulation` in Task 5.
- A strongly evidenced `WEB_BACKEND` can be RESET-6 secondary but must receive a targeted slot; a generic secondary family must not. Test: `TestDeriveEligibleSearchFamiliesPromotesStrongSecondaryWebBackend` in Task 1.
- A vacancy can be found by multiple profiles; profile overlap must differ from within-profile repeated hits and union contribution. Test: `TestProfileTelemetrySeparatesOverlapAndUnionContribution` in Task 4.
- A provider read cap can stop before an empty page; the report must say truncated and incomplete. Tests: `TestProfilePageCapStopsOnlyCurrentProfile` and `TestRunPageCapMarksUnstartedProfiles` in Task 3.
- URL encoding or a mock parser cannot prove HH `OR` semantics; unvalidated multi-phrase fallback must not be emitted. Test: `TestBroadFallbackUsesOneProvenPhraseUntilORValidation` in Task 6.

## Implementation preflight and baseline

Before Task 1, update this plan with the approved corrections, run the repository
checks with `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false`, and commit/push this
plan alone as a docs-only commit. Record that commit as
`IMPLEMENTATION_START_SHA=<new docs-only plan commit>`. All implementation
diffs, protected-path audits and rollback scope use that SHA as their baseline;
`74cab00` is only the parent of the plan commit and must not be used as the
source-code implementation baseline.

---

### Task 1: Add eligible search-family and profile metadata contracts

**Files:**
- Modify: `internal/careeragent/model.go: SearchProfile, SearchProfileEvidence, related planner types`
- Test: `internal/careeragent/search_planner_test.go`
- Modify: `internal/careeragent/model_test.go` only where existing JSON/profile compatibility assertions need the new optional fields

**Interfaces:**
- Preserve: `func PlanSearches(resumes []ResumeProfile, signals CandidateSignals, constraints SearchConstraints) []SearchProfile`
- Add: `type SearchProfileType string`
- Add: `type EligibleSearchFamily struct { Family RoleFamily; ResumeID string; Evidence []string }`
- Add: `func DeriveEligibleSearchFamilies(resume ResumeProfile) []EligibleSearchFamily`
- Add: `func canonicalSearchProfileKey(profile SearchProfile) string`
- Add test-local helpers: `func eligibleFamilyPresent([]EligibleSearchFamily, RoleFamily) bool` and `func eligibleFamilyByName([]EligibleSearchFamily, RoleFamily) *EligibleSearchFamily`.
- Extend `SearchProfile` with optional `ProfileType`, `RoleFamily`, `SourceResumeIDs`, and `EligibilityEvidence` JSON fields while retaining `ID`, `ResumeID`, `ResumeTitle`, `Query`, `Reason`, `SearchPeriodDays`, and `Params`.
- Extend `SearchProfileEvidence` with optional `ProfileType`, `Reason`, `RoleFamily`, `Query`, and `SourceResumeIDs`; existing `ID`, `ResumeID`, and `Label` remain populated for compatibility.

- [ ] **Step 1: Write the failing tests for eligible-family evidence and metadata compatibility**

```go
func TestDeriveEligibleSearchFamiliesPromotesStrongSecondaryWebBackend(t *testing.T) {
	resume := ResumeProfile{
		ID:      "python",
		Title:   "Backend-разработчик (Python/Django) / автоматизация и интеграции",
		Skills:  []string{"Python", "Django Framework", "PHP", "Laravel", "REST API"},
		Enabled: true,
	}
	families := DeriveEligibleSearchFamilies(resume)
	if !eligibleFamilyPresent(families, RoleFamilyPythonBackend) {
		t.Fatalf("Python backend was not eligible: %+v", families)
	}
	web := eligibleFamilyByName(families, RoleFamilyWebBackend)
	if web == nil || len(web.Evidence) == 0 {
		t.Fatalf("strong secondary WEB_BACKEND evidence was not promoted: %+v", families)
	}
}

func TestDeriveEligibleSearchFamiliesDoesNotPromoteGenericSecondary(t *testing.T) {
	resume := ResumeProfile{
		ID: "support", Title: "Технический специалист",
		Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true,
	}
	for _, family := range DeriveEligibleSearchFamilies(resume) {
		if family.Family == RoleFamilyWebBackend || family.Family == RoleFamilySystemAdmin {
			t.Fatalf("generic evidence created an unsupported search family: %+v", family)
		}
	}
}

func TestSearchProfileMetadataIsOptionalForLegacyJSON(t *testing.T) {
	var profile SearchProfile
	if err := json.Unmarshal([]byte(`{"id":"search-1","resume_id":"r","query":"Python backend","reason":"resume role/title","params":{}}`), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID != "search-1" || profile.Query != "Python backend" || profile.ProfileType != "" {
		t.Fatalf("legacy profile was not accepted: %+v", profile)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/careeragent -run 'TestDeriveEligibleSearchFamilies|TestSearchProfileMetadataIsOptionalForLegacyJSON' -count=1`

Expected: FAIL because the eligible-family function and metadata fields do not yet exist.

- [ ] **Step 3: Implement the smallest contract and evidence projection**

Add the following constants and fields without changing RESET-6 role-family constants:

```go
type SearchProfileType string

const (
	SearchProfileTargeted      SearchProfileType = "TARGETED"
	SearchProfileAdjacent      SearchProfileType = "ADJACENT"
	SearchProfileBroadFallback SearchProfileType = "BROAD_FALLBACK"
	SearchProfileManual        SearchProfileType = "MANUAL"
)

type EligibleSearchFamily struct {
	Family   RoleFamily `json:"family"`
	ResumeID string     `json:"resume_id"`
	Evidence []string   `json:"evidence,omitempty"`
}
```

Implement `DeriveEligibleSearchFamilies` by projecting the existing
`reset6RoleFamilyRules` vocabulary over trusted resume title/desired-role,
search hints, strong role anchors and core role-specific evidence. Do not read
vacancy data, AI output or previous search results. Require an explicit
family-specific anchor; Git/Linux/SQL/API alone must not qualify a family.
Keep `canonicalSearchProfileKey` deterministic and based only on role family,
normalized query and provider-affecting parameters. After a collision, merge
`SourceResumeIDs` and eligibility evidence with deduplicated stable ordering,
then construct any stable profile ID from the canonical key plus normalized
merged metadata.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/careeragent -run 'TestDeriveEligibleSearchFamilies|TestSearchProfileMetadataIsOptionalForLegacyJSON' -count=1`

Expected: PASS.

Run: `go test ./internal/careeragent -count=1`

Expected: PASS with all existing RESET-6 router tests unchanged.

- [ ] **Step 5: Commit the reviewable contract unit**

```bash
git add internal/careeragent/model.go internal/careeragent/search_planner_test.go internal/careeragent/model_test.go
git commit -m "feat: add eligible search family contracts"
```

### Task 2: Generate role-specific profiles with fair budget and canonical dedup

**Files:**
- Modify: `internal/careeragent/model.go: PlanSearches, role expansion helpers, stable profile construction`
- Test: `internal/careeragent/search_planner_test.go`

**Interfaces:**
- Preserve: `PlanSearches` signature and `SearchConstraints` CLI behavior.
- Produce: `SearchProfile` values with `ProfileType`, `Reason`, `RoleFamily`, `SourceResumeIDs`, and `EligibilityEvidence` populated for auto profiles.
- Preserve `ResumeID`, `ResumeTitle`, `Query`, `SearchPeriodDays`, and provider params for legacy consumers.

- [ ] **Step 1: Write failing tests for query families, generic-term policy, dedup and budget fairness**

```go
func TestPlanSearchesCoversEligibleFamiliesBeforeVariants(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "automation", Title: "Инженер внедрения и интеграций", Skills: []string{"API-интеграции", "Webhooks"}, Enabled: true},
		{ID: "python", Title: "Backend-разработчик (Python/Django)", Skills: []string{"Python", "Django Framework", "PHP", "Laravel"}, Enabled: true},
		{ID: "support", Title: "Технический специалист", Skills: []string{"Техническая поддержка", "Диагностика неисправностей"}, Enabled: true},
	}
	profiles := PlanSearches(resumes, CandidateSignals{}, SearchConstraints{MaxProfiles: 4, SearchPeriodDays: 7})
	seen := map[RoleFamily]bool{}
	for _, profile := range profiles {
		seen[profile.RoleFamily] = true
	}
	for _, family := range []RoleFamily{RoleFamilyAutomationIntegrations, RoleFamilyPythonBackend, RoleFamilyWebBackend, RoleFamilyTechSupport} {
		if !seen[family] {
			t.Fatalf("eligible family did not receive a profile: %s; profiles=%+v", family, profiles)
		}
	}
}

func TestPlanSearchesDoesNotCreateGenericOnlyProfiles(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "r", Title: "Разработчик", Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	if len(profiles) != 0 {
		t.Fatalf("generic skills created broad search profiles: %+v", profiles)
	}
}

func TestPlanSearchesCollapsesEquivalentQueriesAndBoundsLanguageVariants(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "python", Title: "Python backend", SearchHints: []string{"Python backend", "python   backend"}, Skills: []string{"Python", "Django"}, Enabled: true}}, CandidateSignals{}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	if len(profiles) > 6 {
		t.Fatalf("language/technology variants exceeded bound: %d profiles=%+v", len(profiles), profiles)
	}
	keys := map[string]bool{}
	for _, profile := range profiles {
		key := canonicalSearchProfileKey(profile)
		if keys[key] {
			t.Fatalf("equivalent profile was not collapsed: %+v", profile)
		}
		keys[key] = true
	}
}

func TestBroadFallbackUsesOneTrustedPhraseWithoutUnvalidatedOR(t *testing.T) {
	profiles := PlanSearches([]ResumeProfile{{ID: "r", Title: "Backend-разработчик", Skills: []string{"Python"}, Enabled: true}}, CandidateSignals{Roles: []string{"Backend-разработчик"}}, SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range profiles {
		if profile.ProfileType == SearchProfileBroadFallback && strings.Contains(profile.Query, " OR ") {
			t.Fatalf("unvalidated OR fallback was emitted: %+v", profile)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests to verify the old title-driven planner fails them**

Run: `go test ./internal/careeragent -run 'TestPlanSearchesCoversEligibleFamiliesBeforeVariants|TestPlanSearchesDoesNotCreateGenericOnlyProfiles|TestPlanSearchesCollapsesEquivalentQueriesAndBoundsLanguageVariants|TestBroadFallbackUsesOneTrustedPhraseWithoutUnvalidatedOR' -count=1`

Expected: FAIL on family coverage/budget metadata because the current planner does not allocate eligible-family queues.

- [ ] **Step 3: Implement role-specific candidate queues and stable budget allocation**

Replace the current title-first expansion inside `PlanSearches` with deterministic candidates generated only from `DeriveEligibleSearchFamilies`. Gate these bounded role phrases by trusted evidence. Existing trusted `CandidateSignals.Roles` and explicitly configured role-specific signals may add bounded query variants only after the corresponding resume-derived family is eligible; they must not activate a family on their own:

```go
PYTHON_BACKEND:          "Python backend", "Python developer", "Django developer", "Python-разработчик", "Backend-разработчик"
WEB_BACKEND:             "Backend developer", "PHP backend", "Laravel developer", "backend-разработчик", "веб-разработчик"
AUTOMATION_INTEGRATIONS: "automation engineer", "integration specialist", "implementation specialist", "инженер внедрения", "специалист по интеграциям"
TECH_SUPPORT:            "technical support", "support engineer", "application support", "специалист технической поддержки", "инженер технической поддержки"
```

Each candidate must retain a family role phrase and may add at most one
specific trusted anchor. Generic skills never create a candidate. Candidate
Profile variants must remain bounded, deterministic and provenance-preserving.
Build
primary eligible-family reservations first, then round-robin variants across
families and source resumes, then eligible adjacent profiles, then one
single-phrase fallback. Use `canonicalSearchProfileKey` before and after
budget ordering so equivalent candidates collapse without losing
`SourceResumeIDs` or eligibility evidence. Keep the existing area/resume,
publication order, period and page-size params. Do not alter the manual URL
path.

Add regression fixtures proving that an eligible `TECH_SUPPORT` family plus a
trusted role variant can emit that variant, while an unsupported
`CandidateSignals` role without a supporting enabled resume emits no family or
profile. Also prove that Russian phrases remain available alongside English
aliases for technical support, implementation, integrations, Python and
backend families.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/careeragent -run 'TestPlanSearchesCoversEligibleFamiliesBeforeVariants|TestPlanSearchesDoesNotCreateGenericOnlyProfiles|TestPlanSearchesCollapsesEquivalentQueriesAndBoundsLanguageVariants|TestBroadFallbackUsesOneTrustedPhraseWithoutUnvalidatedOR' -count=1`

Expected: PASS.

Run: `go test ./internal/careeragent -count=1`

Expected: PASS with RESET-6 routing behavior unchanged.

- [ ] **Step 5: Commit the planner unit**

```bash
git add internal/careeragent/model.go internal/careeragent/search_planner_test.go
git commit -m "feat: calibrate role-specific search planning"
```

### Task 3: Add deterministic provider-read bounds and explicit truncation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/defaults.go`
- Modify: `internal/config/flags.go`
- Modify: `internal/config/validation.go`
- Test: `internal/config/config_test.go`
- Modify: `internal/runtime/runtime.go: Config, legacyConfigFromPackage, HHAIResponder, fetchVacanciesFromSearchProfilesWithLimit`
- Test: `internal/runtime/multi_search_test.go`

**Interfaces:**
- Add config fields `MaxSearchPagesPerProfile int` and `MaxSearchPagesPerRun int`.
- Add flags `--max-search-pages-per-profile` and `--max-search-pages-per-run`.
- Add env settings `HH_MAX_SEARCH_PAGES_PER_PROFILE` and `HH_MAX_SEARCH_PAGES_PER_RUN` with CLI-over-env precedence.
- Keep `fetchVacanciesFromSearchProfilesWithLimit(summary, profiles, maxUnique, seenIDs)` signature; add cap state internally so pilot callers retain compatibility.
- Produce explicit run/profile truncation status without touching any HH write path.
- Add test-local fixture `func newMultiSearchResponder(t *testing.T, pages map[int][]Vacancy) (*HHAIResponder, *httptest.Server)` and configure its responder with `maxSearchPagesPerProfile` and `maxSearchPagesPerRun`.
- `RunSummaryResult` exposes `DiscoveryTruncated bool`, `DiscoveryComplete bool`, `SearchPagesFetched int`, and `SearchPagesTruncated int`; profile summaries expose corresponding profile status.

- [ ] **Step 1: Write failing config and pagination tests**

```go
func TestConfigDefaultsBoundSearchPagination(t *testing.T) {
	cfg, err := Load(nil, lookupFrom(map[string]string{}), ".")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSearchPagesPerProfile != 3 || cfg.MaxSearchPagesPerRun != 48 {
		t.Fatalf("unexpected search read bounds: profile=%d run=%d", cfg.MaxSearchPagesPerProfile, cfg.MaxSearchPagesPerRun)
	}
}

func TestConfigRejectsNonPositiveSearchPaginationBounds(t *testing.T) {
	_, err := Load(nil, lookupFrom(map[string]string{"HH_MAX_SEARCH_PAGES_PER_PROFILE": "0"}), ".")
	if err == nil || !strings.Contains(err.Error(), "max-search-pages-per-profile") {
		t.Fatalf("non-positive page bound was accepted: %v", err)
	}
}

func TestProfilePageCapStopsOnlyCurrentProfile(t *testing.T) {
	responder, server := newMultiSearchResponder(t, map[int][]Vacancy{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 3}, {ID: 4}},
		2: {{ID: 5}, {ID: 6}},
		3: {{ID: 7}},
	})
	defer server.Close()
	responder.maxSearchPagesPerProfile = 2
	responder.maxSearchPagesPerRun = 48
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.DiscoveryTruncated || summary.DiscoveryComplete || summary.SearchPagesTruncated == 0 {
		t.Fatalf("profile-cap truncation was not explicit: %+v", summary)
	}
}

func TestRunPageCapMarksUnstartedProfiles(t *testing.T) {
	responder, server := newMultiSearchResponder(t, map[int][]Vacancy{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 3}, {ID: 4}},
	})
	defer server.Close()
	responder.maxSearchPagesPerProfile = 48
	responder.maxSearchPagesPerRun = 2
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.DiscoveryTruncated || summary.DiscoveryComplete || summary.SearchPagesTruncated == 0 {
		t.Fatalf("run-cap truncation was not explicit: %+v", summary)
	}
	for _, profile := range summary.SearchProfiles {
		if profile.PagesFetched == 0 && !profile.Truncated {
			t.Fatalf("unstarted profile was not marked truncated: %+v", profile)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/config ./internal/runtime -run 'TestConfigDefaultsBoundSearchPagination|TestConfigRejectsNonPositiveSearchPaginationBounds|TestProfilePageCapStopsOnlyCurrentProfile|TestRunPageCapMarksUnstartedProfiles' -count=1`

Expected: FAIL because the config fields and read-bound state do not exist.

- [ ] **Step 3: Implement config plumbing and bounded page reads**

Add defaults `DefaultMaxSearchPagesPerProfile = 3` and
`DefaultMaxSearchPagesPerRun = 48`. Parse env only when the corresponding CLI
flag was not visited, validate both values as positive, and copy them through
`legacyConfigFromPackage` into `HHAIResponder`.

In `fetchVacanciesFromSearchProfilesWithLimit`, count every provider page GET.
Before each GET, stop when either cap is reached. Set profile
`Truncated=true` and the exact reason for a profile cap; mark the run and all
unstarted profiles when the run cap prevents further reads. Do not treat a cap
stop as an empty page or complete discovery. Leave `maxUnique` behavior intact
for pilot calls and keep all reads GET-only.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/config ./internal/runtime -run 'TestConfigDefaultsBoundSearchPagination|TestConfigRejectsNonPositiveSearchPaginationBounds|TestProfilePageCapStopsOnlyCurrentProfile|TestRunPageCapMarksUnstartedProfiles' -count=1`

Expected: PASS.

Run: `go test ./internal/config ./internal/runtime -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the read-bound unit**

```bash
git add internal/config/config.go internal/config/defaults.go internal/config/flags.go internal/config/validation.go internal/config/config_test.go internal/runtime/runtime.go internal/runtime/multi_search_test.go
git commit -m "feat: bound search pagination reads"
```

### Task 4: Preserve rich provenance and compute per-profile discovery accounting

**Files:**
- Modify: `internal/runtime/career_agent_runtime.go`
- Modify: `internal/runtime/runtime.go: vacancySearchProfile and discovery loop`
- Modify: `internal/runtime/vacancy_match.go: SearchProfileSummary and source evidence`
- Modify: `internal/careeragent/model.go: SearchProfileEvidence`
- Test: `internal/runtime/multi_search_test.go`
- Test: `internal/runtime/career_agent_runtime_test.go` if the existing runtime test file contains planner conversion fixtures; otherwise create it

**Interfaces:**
- `vacancySearchProfile` carries profile ID, name, query, type, reason, role family and source resume IDs.
- `SearchProfileSummary` retains legacy `Name`, `URL`, and `VacanciesFetched`, and adds `ID`, `Query`, `ProfileType`, `RoleFamily`, `SourceResumeIDs`, `RawHits`, `DistinctProfileVacancies`, `ExclusiveVacancies`, `OverlapVacancies`, `UnionNewContribution`, `ProcessedVacancies`, `PagesFetched`, `Truncated`, and `TruncationReason`.
- `careerAgentSearchSources[vacancyID]` retains every matching `SearchProfileEvidence` with the rich metadata.
- `FoundByProfiles` remains the legacy list of profile names in vacancy results.
- Add test-local fixtures `type profilePage struct { ProfileID string; Items []Vacancy }`, `func newProfileFixtureResponder(t *testing.T, pages []profilePage) (*HHAIResponder, *httptest.Server)`, and `func searchProfileSummaryByID(RunSummaryResult, string) *SearchProfileSummary`.

- [ ] **Step 1: Write the failing overlap/provenance test**

```go
func TestProfileTelemetrySeparatesOverlapAndUnionContribution(t *testing.T) {
	responder, server := newProfileFixtureResponder(t, []profilePage{
		{ProfileID: "a", Items: []Vacancy{{ID: 1}, {ID: 2}, {ID: 2}, {ID: 3}}},
		{ProfileID: "b", Items: []Vacancy{{ID: 2}, {ID: 3}, {ID: 4}}},
	})
	defer server.Close()
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := searchProfileSummaryByID(summary, "a")
	b := searchProfileSummaryByID(summary, "b")
	if a.RawHits != 4 || a.DistinctProfileVacancies != 3 || a.ExclusiveVacancies != 1 || a.OverlapVacancies != 2 || a.UnionNewContribution != 3 {
		t.Fatalf("profile a accounting is wrong: %+v", a)
	}
	if b.RawHits != 3 || b.DistinctProfileVacancies != 3 || b.ExclusiveVacancies != 1 || b.OverlapVacancies != 2 || b.UnionNewContribution != 1 {
		t.Fatalf("profile b accounting is wrong: %+v", b)
	}
	if a.RawHits-a.DistinctProfileVacancies == a.OverlapVacancies {
		t.Fatalf("within-profile repeated hit was incorrectly used as overlap: %+v", a)
	}
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `go test ./internal/runtime -run TestProfileTelemetrySeparatesOverlapAndUnionContribution -count=1`

Expected: FAIL because the current summary has no per-profile identity or overlap/contribution counters.

- [ ] **Step 3: Implement metadata propagation and two-pass profile accounting**

Copy planner metadata in `rebuildCareerAgentSearchProfiles` into
`vacancySearchProfile`. When a provider hit is seen, update a profile-local ID
set and the global first-seen union. After all profiles that were actually
scanned are known, compute exclusive and overlap counts from the complete
source-set map, not from raw-hit subtraction. Preserve all source IDs on a
deduplicated vacancy and keep legacy name arrays.

Keep `VacanciesFetched` equal to `RawHits` for old JSON consumers. Populate
`RawHits`, `DistinctProfileVacancies`, `ExclusiveVacancies`,
`OverlapVacancies`, `UnionNewContribution`, page counts and truncation fields
with new snake-case JSON names. Profiles not started due to the run cap receive
zero counts plus explicit `Truncated=true` and a run-cap reason.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/runtime -run 'TestProfileTelemetrySeparatesOverlapAndUnionContribution|TestBuildCareerAgentSearchProfiles' -count=1`

Expected: PASS.

Run: `go test ./internal/runtime -count=1`

Expected: PASS with manual search and dedup tests unchanged.

- [ ] **Step 5: Commit the provenance/accounting unit**

```bash
git add internal/careeragent/model.go internal/runtime/career_agent_runtime.go internal/runtime/runtime.go internal/runtime/vacancy_match.go internal/runtime/multi_search_test.go internal/runtime/career_agent_runtime_test.go
git commit -m "feat: add per-profile discovery provenance"
```

### Task 5: Separate discovery coverage from processed/router telemetry

**Files:**
- Modify: `internal/runtime/vacancy_match.go: RunSummaryResult and report models`
- Modify: `internal/runtime/application_processing.go: ApplyVacancies router entry and outcome accounting`
- Modify: `internal/runtime/career_agent_command.go: JSON/human report rendering`
- Test: `internal/runtime/reset7_telemetry_test.go`
- Modify: `internal/runtime/multi_search_test.go` for existing summary expectations

**Interfaces:**
- Add run-level fields `RawHits`, `DistinctDiscovered`, `ProcessedByRouter`, `NotProcessedDueToRunCap`, `NotProcessedByOtherPreRouterGate`, `DiscoveryComplete`, `SearchPagesFetched`, `SearchPagesTruncated`, `RouterOutcomeCounts`, `FinalDecisionCounts`, and explicit router-yield values while retaining old summary fields for JSON compatibility.
- Add helpers with exact responsibilities: `recordRouterEntry(summary *RunSummaryResult, vacancy Vacancy, sources []careeragent.SearchProfileEvidence)`, `recordRouterOutcome(summary *RunSummaryResult, routeCode string, finalDecision string)`, and `finalizeDiscoveryCoverage(summary *RunSummaryResult)`.
- Per-profile `ProcessedVacancies` is incremented once per profile source for a vacancy that entered RESET-6 routing; run-level route outcomes are counted once per vacancy.
- `RouterOutcomeCounts map[string]int`, `FinalDecisionCounts map[string]int`, and `RouterYields map[string]float64` are the denominator-aware report maps used by tests and renderers.

- [ ] **Step 1: Write the failing denominator and outcome tests**

```go
func TestRunSummarySeparatesDiscoveryFromProcessedRouterPopulation(t *testing.T) {
	summary := RunSummaryResult{
		VacanciesFetchedRaw: 12,
		VacanciesAfterDedup: 8,
		VacancyLimitSkipped: 3,
	}
	summary.ProcessedByRouter = 4
	summary.RouterOutcomeCounts = map[string]int{
		careeragent.RouteReasonSelected: 1,
		careeragent.RouteReasonAmbiguous: 1,
		careeragent.RouteReasonNoSuitable: 1,
		careeragent.RouteReasonOutOfScope: 1,
	}
	finalizeDiscoveryCoverage(&summary)
	if summary.DistinctDiscovered != 8 || summary.ProcessedByRouter != 4 || summary.NotProcessedDueToRunCap != 3 {
		t.Fatalf("discovery and processed counters were mixed: %+v", summary)
	}
	if got := summary.RouterYields[careeragent.RouteReasonSelected]; got != 0.25 {
		t.Fatalf("selected yield used the wrong denominator: %v", got)
	}
}

func TestProcessedRouterOutcomesExcludeUnroutedVacancies(t *testing.T) {
	summary := RunSummaryResult{}
	recordRouterOutcome(&summary, careeragent.RouteReasonSelected, "MATCH")
	recordRouterOutcome(&summary, careeragent.RouteReasonAmbiguous, "REVIEW_REQUIRED")
	if summary.RouterOutcomeCounts[careeragent.RouteReasonSelected] != 1 || summary.FinalDecisionCounts["MATCH"] != 1 {
		t.Fatalf("router outcomes were not recorded: %+v", summary)
	}
	if summary.FinalDecisionCounts["REJECT"] != 0 || summary.FinalDecisionCounts["REVIEW_REQUIRED"] != 1 {
		t.Fatalf("unprocessed outcomes leaked into router counters: %+v", summary)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/runtime -run 'TestRunSummarySeparatesDiscoveryFromProcessedRouterPopulation|TestProcessedRouterOutcomesExcludeUnroutedVacancies' -count=1`

Expected: FAIL because the new population counters and denominator-aware helpers do not exist.

- [ ] **Step 3: Implement population boundaries and report rendering**

At the moment `ApplyVacancies` loops over the complete deduplicated discovery
union and applies `HH_MAX_VACANCIES_PER_RUN` before detail/router. Increment
`ProcessedByRouter` exactly when `preliminaryRouteForVacancy` is entered, and
increment `NotProcessedDueToRunCap` for distinct vacancies stopped by that
existing cap. Compute `NotProcessedByOtherPreRouterGate` from distinct
discovered minus router-entered minus run-cap-skipped, preserving separate
existing counters for already-responded, attempt and cheap-filter paths.

Record `ROUTE_SELECTED`, `ROUTE_AMBIGUOUS`, `NO_SUITABLE_RESUME`,
`ROLE_OUT_OF_SCOPE`, and `ROUTE_LOW_EVIDENCE` only from RESET-6 route results.
Record `MATCH`, `REJECT`, and `REVIEW_REQUIRED` only for the same router-entered
population after local policy. Keep `AI evaluated` as a subset counter, not as
the route denominator. Compute selected/out-of-scope/low-evidence yields with
`ProcessedByRouter`; use per-profile `ProcessedVacancies` for profile yields.
Render discovery coverage, truncation and router coverage as separate report
sections. Do not remove old fields used by existing JSON consumers.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/runtime -run 'TestRunSummarySeparatesDiscoveryFromProcessedRouterPopulation|TestProcessedRouterOutcomesExcludeUnroutedVacancies' -count=1`

Expected: PASS.

Run: `go test ./internal/runtime -count=1`

Expected: PASS, including existing accounting and RESET-6 runtime tests.

- [ ] **Step 5: Commit the telemetry unit**

```bash
git add internal/runtime/vacancy_match.go internal/runtime/application_processing.go internal/runtime/career_agent_command.go internal/runtime/reset7_telemetry_test.go internal/runtime/multi_search_test.go
git commit -m "feat: separate discovery and router telemetry"
```

### Task 6: Preserve manual profiles and gate fallback semantics

**Files:**
- Modify: `internal/runtime/career_agent_command.go: manualCareerAgentSearchProfiles`
- Modify: `internal/careeragent/model.go: fallback candidate construction`
- Test: `internal/careeragent/search_planner_test.go`
- Test: `internal/runtime/multi_search_test.go`
- Modify: `README.md`
- Modify: `example.env`

**Interfaces:**
- Manual profiles use `ProfileType=MANUAL` and `Reason=MANUAL_PROFILE` while retaining current URL, area, resume, host, page removal, order, period and page-size behavior.
- Auto fallback emits no literal multi-phrase `OR` query until controlled BrowserHHClient validation proves HH alternative semantics.
- Document `HH_MAX_SEARCH_PAGES_PER_PROFILE=3`, `HH_MAX_SEARCH_PAGES_PER_RUN=48`, manual URL precedence, truncation, discovery-vs-router populations and per-profile accounting.
- The runtime test imports the package as `careeragent`; the planner fallback test may be placed in `internal/careeragent/search_planner_test.go` if the implementation keeps it in that package.

- [ ] **Step 1: Write failing manual/fallback tests**

```go
func TestManualProfilesGetManualMetadataWithoutChangingPrecedence(t *testing.T) {
	profiles, _, err := buildVacancySearchProfilesWithOptions([]string{
		"https://hh.example/search/vacancy?text=python&area=3&resume=resume-hash",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	career := manualCareerAgentSearchProfiles(profiles)
	if len(career) != 1 || career[0].ProfileType != careeragent.SearchProfileManual || career[0].Reason != "MANUAL_PROFILE" {
		t.Fatalf("manual provenance was not explicit: %+v", career)
	}
	if career[0].Params.Get("area") != "3" || career[0].Params.Get("resume") != "resume-hash" || career[0].Params.Get("items_on_page") != "50" {
		t.Fatalf("manual provider params changed: %+v", career[0].Params)
	}
}

func TestBroadFallbackUsesOneProvenPhraseUntilORValidation(t *testing.T) {
	profiles := careeragent.PlanSearches([]careeragent.ResumeProfile{{ID: "support", Title: "Техническая поддержка", Enabled: true}}, careeragent.CandidateSignals{}, careeragent.SearchConstraints{MaxProfiles: 16, SearchPeriodDays: 7})
	for _, profile := range profiles {
		if profile.ProfileType == careeragent.SearchProfileBroadFallback && strings.Contains(profile.Query, " OR ") {
			t.Fatalf("unvalidated provider OR semantics leaked into planner: %+v", profile)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/careeragent ./internal/runtime -run 'TestManualProfilesGetManualMetadataWithoutChangingPrecedence|TestBroadFallbackUsesOneProvenPhraseUntilORValidation' -count=1`

Expected: FAIL because manual metadata and fallback gating are not yet explicit.

- [ ] **Step 3: Implement compatibility and documentation**

Mark `manualCareerAgentSearchProfiles` records as manual without altering its
query parsing. Keep explicit manual profiles as a complete override over auto
profiles. Make the production fallback builder select one trusted role phrase
or return no fallback; do not add an `OR` enable flag. Keep generic include and
exclude values out of role-profile generation, and preserve them only where
existing downstream compatibility requires them.

Update README and example env with the two positive read-cap settings and the
separate metric definitions. State that mock URL encoding is not provider
semantics and that the multi-phrase fallback is disabled until a controlled
read-only BrowserHHClient validation succeeds.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/careeragent ./internal/runtime -run 'TestManualProfilesGetManualMetadataWithoutChangingPrecedence|TestBroadFallbackUsesOneProvenPhraseUntilORValidation|TestBuildVacancySearchProfilesPreservesAreaAndResume' -count=1`

Expected: PASS.

Run: `go test ./internal/careeragent ./internal/runtime ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the compatibility unit**

```bash
git add internal/careeragent/model.go internal/careeragent/search_planner_test.go internal/runtime/career_agent_command.go internal/runtime/multi_search_test.go README.md example.env
git commit -m "feat: preserve manual search and gate fallback semantics"
```

### Task 7: Run frozen-baseline, provider-semantics, union-recall and safety validation

**Files:**
- Create: `docs/validation/VALIDATION_RESET_7_SEARCH.md`
- Read-only inputs: `/tmp/reset7-old.json`, `/tmp/reset7-old-profiles.json`, `/tmp/reset7-baseline-meta.json`
- Inspect only: protected paths listed in the RESET-7 spec

**Interfaces:**
- Produce a validation document with discovery coverage, per-profile accounting, processed/router outcomes, truncation status, old/new union review, manual samples and write audit.
- Do not modify `BrowserHHClient`, auth, cookies, router policy, AI prompt, write gateway, application send, nonce, reconciliation or cover-letter code.

- [ ] **Step 1: Write the failing validation assertions before the live run**

Create a temporary read-only validation shell assertion set outside the repository:

```bash
test "${HH_DRY_RUN}" = "true"
test "${HH_WRITE_ENABLED}" = "false"
jq -e '.safety.real_hh_writes == 0 and .safety.application_post == 0' /tmp/reset7-baseline-meta.json
jq -e '.distinct_discovered >= .enabled_resume_count and .safety.accounting_pass == true' /tmp/reset7-baseline-meta.json
```

Expected: the new report assertions fail before the implementation because the
new report fields and per-profile telemetry are absent.

- [ ] **Step 2: Run the controlled read-only BrowserHHClient fallback probe**

Use the existing browser transport with fresh trusted phrases and no write
capability. Compare one phrase, a second phrase, and the literal alternative
query only as read-only observations. Accept `OR` semantics only if the
provider result demonstrably behaves as the union of alternatives; URL
encoding and HTTP status alone are insufficient. If semantics are not proven,
record `OR_NOT_ENABLED` and keep the one-proven-phrase/omitted fallback.

- [ ] **Step 3: Run the new planner against the frozen baseline contract**

Run with:

```bash
HH_DRY_RUN=true \
HH_WRITE_ENABLED=false \
HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 \
HH_MAX_VACANCIES_PER_RUN=100 \
STORAGE_BACKEND=json \
./hh-ai-responder -u "" --career-agent-result /tmp/reset7-new.json career-agent --shadow
```

Verify `raw_hits`, `distinct_discovered`, `processed_by_router`,
`not_processed_due_to_run_cap`, page caps, `discovery_complete`, profile
overlap/contribution fields, route outcomes and zero writes. A truncated run is
reported as incomplete and is not treated as an empty or negative discovery.

- [ ] **Step 4: Perform bounded union recall review and manual samples**

Compare old and new vacancy ID sets using `jq` without exposing cookies or raw
private bodies. Classify old IDs retained, old IDs missing, new IDs from
targeted profiles and new IDs from fallback. Review every old supported-role ID
missing from new discovery, all `ROUTE_SELECTED` results, up to 10 targeted
vacancies, up to 10 fallback vacancies, and representative out-of-scope and
low-evidence results. Record the reason and whether the result is a timing
change, truncation artifact or actual recall regression.

- [ ] **Step 5: Run the protected-path audit and rollback check**

Run:

```bash
git diff "${IMPLEMENTATION_START_SHA}"..HEAD -- internal/browsersession internal/adapters/hh/read/browser.go internal/runtime/hh_write_gateway.go internal/runtime/application_processing.go internal/runtime/vacancy_preflight.go internal/careeragent/router_role_family.go
git diff --check
```

The first command must show no changes to auth/browser transport, write
gateway, application submission, preflight, or RESET-6 router policy outside
the explicitly telemetry-only runtime lines. If any real write, Application
POST, protected-path change, manual precedence regression, silent truncation or
obvious supported-role recall regression appears, stop the rollout and revert
only RESET-7 commits in reverse order with reviewable `git revert` commits; do
not use `git reset --hard` or delete user files. Restore the previous planner
and report schema while retaining the frozen `/tmp/reset7-*` baseline.

- [ ] **Step 6: Write the validation report and run the final repository checks**

Record the exact command, SHA, timestamp, profile metadata, discovery coverage,
per-profile counts, router denominator, union review, samples, fallback probe
result and write audit in `docs/validation/VALIDATION_RESET_7_SEARCH.md`.

Run:

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./...
git diff --check
```

Expected: all commands pass, `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, real
HH writes `0`, Application POST `0`, and only the intended RESET-7 files are
changed.

- [ ] **Step 7: Commit the validation evidence**

```bash
git add docs/validation/VALIDATION_RESET_7_SEARCH.md
git commit -m "docs: validate RESET-7 search planner read-only run"
```

## Plan self-review

- Planner architecture remains `PlanSearches`; no second discovery mechanism is introduced.
- Eligible `WEB_BACKEND` coverage is explicit and independent of the stored RESET-6 primary/secondary ordering.
- Generic skills cannot create profiles; unsupported families are not globally blacklisted.
- Manual URL precedence and provider parameters remain covered by runtime tests.
- Read fan-out has positive per-profile and per-run page caps, with explicit truncation.
- Per-profile overlap uses complete source sets; union contribution is first-seen and raw-minus-distinct is not used as overlap.
- Discovery and router populations have separate counters and denominators.
- Fallback `OR` semantics require controlled BrowserHHClient evidence and are disabled otherwise.
- Frozen baseline, union recall, manual sample, protected-path audit, rollback and zero-write checks are included, using the docs-only `IMPLEMENTATION_START_SHA` rather than `74cab00`.
- No production implementation is part of this plan artifact.
