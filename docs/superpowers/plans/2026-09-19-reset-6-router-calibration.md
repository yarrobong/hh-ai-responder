# RESET-6 Resume Router Calibration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Each task uses a red-green-refactor cycle and ends with fresh verification.

**Goal:** Replace catch-all resume-route ambiguity with bounded role-family evidence and explicit, safe outcomes so suitable vacancies reach AI assessment without increasing obviously wrong selections.

**Architecture:** Keep routing pure and deterministic in `internal/careeragent`. Derive runtime resume routing profiles from the current enabled registry, classify vacancy role evidence after the existing safe detail-enrichment opportunity, then score resumes with bounded role, specific, generic, and mismatch evidence. Keep the runtime orchestration and HH write boundaries unchanged; add only narrow telemetry propagation and report counters.

**Tech Stack:** Go, existing `internal/careeragent` package, existing runtime event/report JSON, Go table-driven tests, `httptest` fixtures, `jq`/shell only for temporary old-vs-new validation analysis.

**Spec:** `docs/superpowers/specs/2026-09-19-reset-6-router-design.md`

**Implementation start SHA:** recorded in the execution ledger immediately
after the final docs-only plan commit. All protected-path audits use that exact
ledger value; the original RESET-6 source starting commit remains documented
in the spec and validation report.

## Global Constraints

- All RESET-6 runs use `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false`.
- Existing safe detail enrichment order is preserved; detail may precede final routing when the search card lacks role evidence.
- Only final `SELECTED` routes can proceed to AI assessment and application preparation.
- `ROUTE_AMBIGUOUS`, `ROUTE_LOW_EVIDENCE`, `ROLE_OUT_OF_SCOPE`, and `NO_SUITABLE_RESUME` never proceed to application or write paths.
- Do not modify BrowserHHClient/auth, cookies, search transport, application send, nonce, reconciliation, write gateway, cover-letter generation, AI prompt/threshold, or hard-requirement policy.
- Do not add vacancy-ID-specific rules or hardcoded personal candidate facts.
- Generic skills cannot independently produce `SELECTED`.
- A strong title anchor requires specific supporting evidence; it is not sufficient alone.
- A role family is not an automatic resume choice and is selectable only when an enabled resume supports it.
- Do not globally lower the existing absolute/relative margin thresholds as the primary fix.
- Preserve existing CLI flags, environment variables, dry-run behavior, and JSON fields where practical.
- Do not add `.env`, `cookies.txt`, `.hh-browser-profile/`, `career_agent_latest.json.md`, `out/`, session data, or generated authenticated data.

## Review Focus

- A clear Python/backend vacancy must choose the enabled Python/backend resume, not an automation or support resume — `TestRouteResumeSelectsPythonBackendFromStrongRoleEvidence` in Task 3.
- A clear support vacancy must choose support, while generic SQL/Linux/API overlap cannot decide the route — `TestRouteResumeSelectsSupportWithoutGenericCollision` in Task 3.
- A clear unsupported role must be distinguished from an unclear role — `TestRouteResumeDistinguishesOutOfScopeFromLowEvidence` in Task 3.
- Two strong role families must remain ambiguous even if one numeric score is slightly higher — `TestRouteResumeKeepsStrongMixedRoleAmbiguous` in Task 3.
- Detail enrichment must still happen before final classification when the card is incomplete, and an unsafe route must not reach AI — `TestCareerAgentDetailEvidenceFeedsFinalRouter` in Task 4.

## File Map

- Create `internal/careeragent/router_role_family.go` for role-family constants, bounded dictionaries, resume-profile derivation helpers, vacancy evidence extraction, and evidence-aware policy helpers.
- Create `internal/careeragent/router_reset6_test.go` for focused role-family, mismatch, evidence-floor, and mutually-exclusive outcome fixtures.
- Modify `internal/careeragent/model.go` to extend existing serializable routing types and delegate `RouteResume`, `PreliminaryRouteResume`, and `scoreResume` to the focused helpers without changing public call sites.
- Modify `internal/careeragent/model_test.go` only where existing expectations refer to the old catch-all reason code; retain tests for stable IDs, provenance softness, hard blockers, and detail deferral.
- Modify `internal/runtime/vacancy_match.go` to add route-category counters and vacancy-level role-evidence telemetry while preserving existing summary fields.
- Modify `internal/runtime/career_agent_events.go` to forward the new route evidence and category fields in existing route events.
- Modify `internal/runtime/application_processing.go` only to map the mutually-exclusive route reasons to summary counters and terminal review reasons; do not change detail-read or write sequencing.
- Modify `internal/runtime/career_agent_command.go` to render the new route-category counts and role evidence in the human report.
- Create `internal/runtime/reset6_router_runtime_test.go` for detail-before-final-route, no-AI-on-unsafe-route, and report propagation fixtures.
- Create `docs/validation/VALIDATION_RESET_6_ROUTER.md` after implementation and real read-only validation; it is a result artifact, not an input to routing.

## Outcome Semantics

The final router returns existing `Status` compatibility (`SELECTED` or
`REVIEW_REQUIRED`) plus one mutually-exclusive `ReasonCode` category:

1. `ROUTE_LOW_EVIDENCE` — available vacancy data does not establish a role family strongly enough.
2. `ROLE_OUT_OF_SCOPE` — the role family is established, but no enabled resume family supports it.
3. `NO_SUITABLE_RESUME` — an enabled resume family supports the role, but no candidate clears the evidence floor or all candidates have controlled mismatch signals.
4. `ROUTE_AMBIGUOUS` — at least two relevant resumes or role families have competing strong evidence and deterministic selection is unsafe.
5. `ROUTE_SELECTED` — exactly one candidate clears the evidence policy and safety gates.

`NO_ENABLED_RESUME` remains the existing outcome when there are no enabled
resumes at all. `ROUTE_AMBIGUOUS_AFTER_DETAIL` remains a deprecated source
compatibility alias only if needed by existing Go callers; serialized RESET-6
reports use the canonical `ROUTE_AMBIGUOUS` category.

## Execution Pre-flight: preserve the old baseline before any new run

This pre-flight occurs immediately after the implementation-start docs commit
and before any additional `career-agent run` command:

- Preserve the existing pre-change report as `/tmp/reset6-old.json`.
- Save `/tmp/reset6-baseline-meta.json` with the implementation start SHA,
  exact old baseline vacancy IDs, enabled resume IDs, and baseline counters.
- Verify that the old JSON contains the exact vacancy-ID set that will be used
  for every old-vs-new comparison.
- Do not regenerate the old baseline after Task 1 begins. If a later live run
  discovers a different HH result set, it is a validation limitation to report,
  not permission to compare different IDs.

## Task 1: Add role-family and evidence model tests first

**Files:**

- Create: `internal/careeragent/router_reset6_test.go`
- Modify: none in production code during the red step

**Interfaces:**

- The tests target the package-level pure functions and types that the implementation will expose:
  - `ClassifyVacancyRole(VacancyInput, []ResumeProfile) VacancyRoleEvidence`
  - `DeriveResumeIdentity(ResumeProfile) ResumeIdentity`
  - `RouteResume(VacancyInput, []ResumeProfile) RouteDecision`

- [ ] **Step 1: Write failing role-family tests.** Add table-driven tests named exactly:
  - `TestClassifyVacancyRoleRecognizesStrongPythonBackendTitle`
  - `TestClassifyVacancyRoleRecognizesTechnicalSupportTitle`
  - `TestClassifyVacancyRoleDoesNotTreatGenericWordsAsStrongAnchors`
  - `TestClassifyVacancyRoleIgnoresCompanyTextForStrongEvidence`
  - `TestClassifyVacancyRoleRecognizesSysadminOnlyAsVacancyEvidence`

  Assert family, anchor strength, title-anchor list, and that `"Инженер"`,
  `"Специалист"`, and `"Разработчик"` alone produce no strong family.

- [ ] **Step 2: Run the focused tests to verify the expected red failure.**

  Run:

  ```bash
  go test ./internal/careeragent -run 'TestClassifyVacancyRole' -count=1
  ```

  Expected result: compile/test failure because the role-family evidence API is
  not implemented yet. Fix only test typos if the failure is unrelated.

- [ ] **Step 3: Add failing resume-profile tests.** Add:
  - `TestDeriveResumeIdentityBuildsRuntimeRoleFamilies`
  - `TestDeriveResumeIdentityKeepsGenericAndSpecificAnchorsSeparate`
  - `TestDeriveResumeIdentityDerivesNegativeMismatchSignalsOnlyFromTrustedFields`

  Use synthetic fixture resumes whose title and skills are explicit in the test;
  assert no role family appears without title/skill evidence.

- [ ] **Step 4: Run the new profile tests and confirm red.**

  ```bash
  go test ./internal/careeragent -run 'TestDeriveResumeIdentity' -count=1
  ```

- [ ] **Step 5: Keep the red tests uncommitted until Task 2 supplies their
  implementation.** The first commit for this work includes the now-green
  tests and the bounded role-family implementation together.

## Task 2: Implement bounded role-family profiles and vacancy evidence

**Files:**

- Create: `internal/careeragent/router_role_family.go`
- Modify: `internal/careeragent/model.go` (`ResumeIdentity`, `ResumeScore`, `RouteDecision` related fields only)
- Test: `internal/careeragent/router_reset6_test.go`

**Interfaces:**

```go
type RoleFamily string

const (
    RoleFamilyPythonBackend       RoleFamily = "PYTHON_BACKEND"
    RoleFamilyWebBackend          RoleFamily = "WEB_BACKEND"
    RoleFamilyAutomationIntegrations RoleFamily = "AUTOMATION_INTEGRATIONS"
    RoleFamilyTechSupport         RoleFamily = "TECH_SUPPORT"
    RoleFamilySystemAdmin         RoleFamily = "SYSTEM_ADMIN"
    RoleFamilyFrontend            RoleFamily = "FRONTEND"
    RoleFamilyFlutter             RoleFamily = "FLUTTER"
    RoleFamilyOneC                RoleFamily = "ONE_C"
    RoleFamilySystemAnalyst       RoleFamily = "SYSTEM_ANALYST"
)

type RoleEvidenceStrength string

const (
    RoleEvidenceNone   RoleEvidenceStrength = "NONE"
    RoleEvidenceWeak   RoleEvidenceStrength = "WEAK"
    RoleEvidenceStrong RoleEvidenceStrength = "STRONG"
)

type RoleFamilyEvidence struct {
    Family             RoleFamily          `json:"family"`
    Strength           RoleEvidenceStrength `json:"strength"`
    TitleAnchors       []string            `json:"title_anchors,omitempty"`
    ExplicitRoleSignals []string           `json:"explicit_role_signals,omitempty"`
    SpecificSignals    []string            `json:"specific_signals,omitempty"`
    GenericSignals     []string            `json:"generic_signals,omitempty"`
    MismatchSignals    []string            `json:"mismatch_signals,omitempty"`
}

type VacancyRoleEvidence struct {
    Families            []RoleFamilyEvidence `json:"families,omitempty"`
    StrongFamilies      []RoleFamily         `json:"strong_families,omitempty"`
    SpecificSignals     []string             `json:"specific_signals,omitempty"`
    GenericSignals      []string             `json:"generic_signals,omitempty"`
    EvidenceAvailable   bool                 `json:"evidence_available"`
}

func ClassifyVacancyRole(v VacancyInput, resumes []ResumeProfile) VacancyRoleEvidence
```

`ClassifyVacancyRole` must determine vacancy role families from vacancy
evidence independently of `resumes`. The `resumes` parameter may remain for
signature compatibility or bounded catalog context, but it must never remove,
hide, or downgrade an otherwise strongly detected unsupported family. First
classify the vacancy; only then compare the result with enabled resume
families. This is required to distinguish `ROLE_OUT_OF_SCOPE` from
`ROUTE_LOW_EVIDENCE`.

Extend `ResumeIdentity` with runtime-derived primary/secondary role families,
strong/generic anchors, negative mismatch anchors, core skills, and adjacent
skills. Extend `ResumeScore` with matched families, strong-role evidence,
specific-evidence count, generic-evidence ratio, and mismatch signals. Extend
`RouteDecision` with `RoleEvidence` and the canonical route-category fields.

- [ ] **Step 1: Implement only the bounded dictionaries and canonical family helpers.**

  Use exact normalized phrases and canonical token sequences already used by the
  package. Add explicit RU/EN aliases for Python/Django/backend, support,
  sysadmin/Linux administration, automation/integration, frontend/Vue, Flutter,
  1C, CNC/АСУ ТП, DBA, and system analyst. Do not use unconstrained substring
  matching; `"api"` must not match `"capillary"` and generic role words must
  not create strong anchors.

- [ ] **Step 2: Implement `DeriveResumeIdentity` extensions.**

  Derive primary and secondary families only from resume title, desired role,
  skills, and explicit negative keywords. Preserve all existing identity fields
  and stable JSON behavior. A family remains merely descriptive until a vacancy
  has matching role evidence.

- [ ] **Step 3: Implement `ClassifyVacancyRole`.**

  Score title/explicit-role phrases above required/key skills, responsibilities,
  and generic signals. Exclude company, location, salary, and search provenance.
  Return all relevant family evidence so mixed Python/support roles remain
  observable instead of being collapsed to one family.

- [ ] **Step 4: Run Task 1 focused tests and the package suite.**

  ```bash
  go test ./internal/careeragent -run 'TestClassifyVacancyRole|TestDeriveResumeIdentity' -count=1
  go test ./internal/careeragent -count=1
  ```

  Expected result: PASS with no warning output.

- [ ] **Step 5: Refactor only after green.** Keep classifier data in the focused
  file, avoid a new package, and preserve existing public names.

- [ ] **Step 6: Commit.**

  ```bash
  git add internal/careeragent/model.go internal/careeragent/router_role_family.go internal/careeragent/router_reset6_test.go
  git commit -m "feat: add bounded resume role-family evidence"
  ```

## Task 3: Implement evidence-aware scoring and mutually-exclusive outcomes

**Files:**

- Modify: `internal/careeragent/model.go` (`RouteResume`, `PreliminaryRouteResume`, `scoreResume`, route constants/types)
- Modify: `internal/careeragent/router_role_family.go` (pure scoring/policy helpers)
- Test: `internal/careeragent/router_reset6_test.go`
- Modify: `internal/careeragent/model_test.go` only for canonical reason-code expectations

**Interfaces:**

```go
const (
    RouteReasonSelected       = "ROUTE_SELECTED"
    RouteReasonAmbiguous      = "ROUTE_AMBIGUOUS"
    RouteReasonLowEvidence    = "ROUTE_LOW_EVIDENCE"
    RouteReasonOutOfScope     = "ROLE_OUT_OF_SCOPE"
    RouteReasonNoSuitable     = "NO_SUITABLE_RESUME"
)

func RouteResume(vacancy VacancyInput, resumes []ResumeProfile) RouteDecision
func PreliminaryRouteResume(vacancy VacancyInput, resumes []ResumeProfile) PreliminaryRouteDecision
```

- [ ] **Step 1: Write failing decision fixtures before changing scoring.** Add:
  - `TestRouteResumeSelectsPythonBackendFromStrongRoleEvidence`
  - `TestRouteResumeSelectsSupportWithoutGenericCollision`
  - `TestRouteResumeSelectsSysadminWhenMatchingResumeIsEnabled`
  - `TestRouteResumeDistinguishesOutOfScopeFromLowEvidence`
  - `TestRouteResumeKeepsStrongMixedRoleAmbiguous`
  - `TestRouteResumeStrongRoleAnchorNeedsSpecificSupportingEvidence`
  - `TestRouteResumeGenericSkillsAloneNeverSelect`
  - `TestRouteResumeSupportedFamilyWithoutEvidenceFloorIsNoSuitable`
  - `TestRouteResumeRejectsVueOnlyAndFlutterOnlyAsPythonBackendTargets`
  - `TestRouteResumePreservesMarginsAndFullCandidateTelemetry`

  Include the required sysadmin-without-supporting-resume case and assert
  `ROLE_OUT_OF_SCOPE`; use a generic `IT specialist` fixture to assert
  `ROUTE_LOW_EVIDENCE` or a true `ROUTE_AMBIGUOUS` only when two families have
  strong evidence.

- [ ] **Step 2: Run the focused decision tests and verify red.**

  ```bash
  go test ./internal/careeragent -run 'TestRouteResume' -count=1
  ```

  Expected result: failures in the old close-margin/generic-score policy, not
  test compilation errors after Task 2.

- [ ] **Step 3: Refactor `scoreResume` to consume role evidence.**

  Preserve existing `RoleScore`, `SkillScore`, `DomainScore`,
  `ExperienceScore`, `ProvenanceScore`, `GenericEvidenceScore`, `RawFit`, and
  `NormalizedScore` fields. Count each normalized fact once for its evidence
  class. Apply controlled mismatch penalties to the candidate resume only;
  never turn a mismatch into a global vacancy rejection.

- [ ] **Step 4: Implement the policy in explicit order.**

  1. No enabled resumes → existing `NO_ENABLED_RESUME`.
  2. No sufficiently established family → `REVIEW_REQUIRED` + `ROUTE_LOW_EVIDENCE`.
  3. Established family with no enabled supporting family → `ROLE_OUT_OF_SCOPE`.
  4. Supported family but every candidate below the evidence floor or mismatched → `NO_SUITABLE_RESUME`.
  5. Two or more relevant families/resumes with strong competing evidence → `ROUTE_AMBIGUOUS`.
  6. One candidate with strong role evidence and specific support, with no competing strong family → `SELECTED` + `ROUTE_SELECTED`.

  Keep absolute and relative margins as telemetry and as a bounded tie-breaker;
  do not replace the policy with a lower global margin.

- [ ] **Step 5: Update preliminary routing conservatively.** Search cards with
  incomplete role evidence continue to return `NEEDS_DETAIL`; only reliable
  hard blockers may remain early obvious rejects. Final category assignment
  happens after the existing safe detail read when detail is needed.

- [ ] **Step 6: Run focused and full package tests.**

  ```bash
  go test ./internal/careeragent -run 'TestRouteResume|TestPreliminaryRoute|TestResume' -count=1
  go test ./internal/careeragent -count=1
  ```

- [ ] **Step 7: Commit.**

  ```bash
  git add internal/careeragent/model.go internal/careeragent/router_role_family.go internal/careeragent/model_test.go internal/careeragent/router_reset6_test.go
  git commit -m "feat: calibrate resume routing outcomes"
  ```

## Task 4: Propagate role evidence without changing detail or write safety

**Files:**

- Modify: `internal/runtime/vacancy_match.go`
- Modify: `internal/runtime/career_agent_events.go`
- Modify: `internal/runtime/application_processing.go`
- Create: `internal/runtime/reset6_router_runtime_test.go`
- Test: `internal/runtime/main_test.go` and `internal/runtime/career_agent_pipeline_test.go` where existing fixtures are extended

**Interfaces:**

```go
type RunSummaryResult struct {
    // existing fields remain
    RouteReasonCounts map[string]int `json:"route_reason_counts,omitempty"`
}

type CareerAgentVacancyResult struct {
    // existing fields remain
    RoleEvidence []careeragent.RoleFamilyEvidence `json:"role_evidence,omitempty"`
}
```

- [ ] **Step 1: Write failing runtime tests.** Add:
  - `TestCareerAgentDetailEvidenceFeedsFinalRouter`
  - `TestCareerAgentUnsafeRouteStopsBeforeAI`
  - `TestCareerAgentRouteReasonCountersAreMutuallyExclusive`
  - `TestCareerAgentRouteEventCarriesFullTelemetry`

  Use the existing `httptest` HH reader pattern. Assert detail is read before
  final routing when the card lacks responsibilities/skills, and assert no AI
  analyzer or application preparation call occurs for ambiguous,
  low-evidence, out-of-scope, or no-suitable routes.

- [ ] **Step 2: Run the runtime tests to verify red.**

  ```bash
  go test ./internal/runtime -run 'TestCareerAgent(DetailEvidence|UnsafeRoute|RouteReason|RouteEvent)' -count=1
  ```

- [ ] **Step 3: Forward `RouteDecision.RoleEvidence` and candidate telemetry in
  existing route events and vacancy results.** Do not create a new HH read or
  write path.

- [ ] **Step 4: Map only final route categories to summary counters.** Keep
  `FinalAmbiguous` for canonical `ROUTE_AMBIGUOUS`; add separate counters for
  low evidence, out of scope, and no suitable. Do not increment more than one
  category for a vacancy.

- [ ] **Step 5: Preserve detail sequencing and write gates.** The existing
  `fetchCareerAgentDetail` call remains before final `routeResumeForVacancy`
  whenever `PreliminaryRouteResume` returns `NEEDS_DETAIL`. The existing
  `configureCareerAgentMode` and write gateway are untouched.

- [ ] **Step 6: Run runtime package tests and existing pipeline regression tests.**

  ```bash
  go test ./internal/runtime -run 'TestCareerAgent|TestDryRun|TestSelectedResume' -count=1
  go test ./internal/runtime -count=1
  ```

- [ ] **Step 7: Commit.**

  ```bash
  git add internal/runtime/vacancy_match.go internal/runtime/career_agent_events.go internal/runtime/application_processing.go internal/runtime/reset6_router_runtime_test.go internal/runtime/main_test.go internal/runtime/career_agent_pipeline_test.go
  git commit -m "feat: expose router evidence categories"
  ```

## Task 5: Add human-report and accounting output

**Files:**

- Modify: `internal/runtime/career_agent_command.go`
- Modify: `internal/runtime/vacancy_match.go` if summary fields need JSON tags
- Create or extend: `internal/runtime/career_agent_command_test.go` if no focused report test exists

- [ ] **Step 1: Write a failing report-render test.** Assert the human report
  includes separate counts for `ROUTE_AMBIGUOUS`, `NO_SUITABLE_RESUME`,
  `ROLE_OUT_OF_SCOPE`, and `ROUTE_LOW_EVIDENCE`, and that vacancy rows expose
  the final route reason.

- [ ] **Step 2: Run the focused test and verify red.**

  ```bash
  go test ./internal/runtime -run 'TestRenderCareerAgentHumanReportRouteCategories' -count=1
  ```

- [ ] **Step 3: Implement report rendering using structured summary data.**
  Keep existing summary lines and add category lines; do not log full AI prompts,
  private raw responses, cookies, or secrets.

- [ ] **Step 4: Run focused/full runtime tests and commit.**

  ```bash
  go test ./internal/runtime -run 'TestRenderCareerAgentHumanReportRouteCategories' -count=1
  go test ./internal/runtime -count=1
  git add internal/runtime/career_agent_command.go internal/runtime/vacancy_match.go internal/runtime/career_agent_command_test.go
  git commit -m "feat: separate router reason reporting"
  ```

## Task 6: Old-vs-new shadow comparison on the preserved vacancy set

**Files:**

- Temporary, outside the repository: `/tmp/reset6-old.json`, `/tmp/reset6-new.json`, `/tmp/reset6-transition.tsv`
- Read-only source artifacts: `career_agent_latest.json` may be inspected but must not be committed.

- [ ] **Step 1: Verify the execution pre-flight artifacts.** Confirm that
  `/tmp/reset6-old.json` and `/tmp/reset6-baseline-meta.json` were created
  before Task 1 and contain the implementation-start SHA, exact baseline
  vacancy IDs, enabled resume IDs, and baseline counters. Do not regenerate
  them here.

- [ ] **Step 2: Build the changed binary without running writes.** Run the
  repository build after Tasks 1–5 and execute only with:

  ```bash
  HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent run
  cp career_agent_latest.json /tmp/reset6-new.json
  ```

- [ ] **Step 3: Compare the same vacancy IDs.** Use a temporary `jq`/Go
  analysis to emit a transition matrix with old/new status, reason, selected
  resume, top-two scores, absolute/relative margins, role-family evidence,
  specific evidence, generic evidence ratio, and mismatch signals.

  Required transition rows/categories:

  ```text
  AMBIGUOUS -> SELECTED
  AMBIGUOUS -> NO_SUITABLE_RESUME
  AMBIGUOUS -> ROLE_OUT_OF_SCOPE
  AMBIGUOUS -> ROUTE_LOW_EVIDENCE
  AMBIGUOUS -> still AMBIGUOUS
  SELECTED -> same resume
  SELECTED -> changed resume
  SELECTED -> non-selected
  ```

- [ ] **Step 4: Review every `SELECTED -> changed resume` transition.** For
  each, show strong role evidence, specific support, runner-up, why the runner
  up lost, generic evidence ratio, and whether a mismatch signal changed the
  decision. Any obviously wrong change blocks calibration and requires policy
  adjustment plus a new red-green cycle.

- [ ] **Step 5: Categorize all old ambiguous vacancies.** Assign exactly one of
  `TRUE_AMBIGUITY`, `SCORE_COMPRESSION`, `GENERIC_SIGNAL_COLLISION`,
  `MISSING_ROLE_ANCHOR`, `WRONG_COMPETITOR`, or `LOW_EVIDENCE` using the
  vacancy evidence and old candidate telemetry. This is an analytical label,
  not an automatic routing instruction.

## Task 7: Safety sample and validation report

**Files:**

- Create: `docs/validation/VALIDATION_RESET_6_ROUTER.md`

- [ ] **Step 1: Select the required manual samples from the new report.** Review
  at least 10 newly selected, 10 still ambiguous, and 5 combined
  no-suitable/out-of-scope/low-evidence vacancies. For every selected vacancy
  record title, selected resume, family, strong role evidence, specific support,
  runner-up, why runner-up lost, and generic evidence ratio.

- [ ] **Step 2: Write the report from actual artifacts.** Include:

  - original source starting commit, implementation-start SHA, and final commit;
  - enabled resume inventory and derived role families;
  - before/after selected, ambiguous, no-suitable, out-of-scope, and low-evidence counts;
  - all six old ambiguity categories and representative table of at least 20;
  - transition matrix and evidence for every changed selected resume;
  - AI evaluated, MATCH, REVIEW, REJECT, and pilot candidates;
  - safety sample findings;
  - `Real HH writes: 0` and `Application POST: 0`;
  - exact verification command results.

- [ ] **Step 3: Check report consistency.** Reconcile counts against JSON
  `summary`, vacancy rows, terminal accounting, and transition analysis. Do not
  claim fewer false ambiguities unless the manual sample supports it; report
  unchanged ambiguity as valid where evidence is genuinely insufficient.

- [ ] **Step 4: Commit the validation report only after verification.**

  ```bash
  git add docs/validation/VALIDATION_RESET_6_ROUTER.md
  git commit -m "docs: validate RESET-6 router calibration"
  ```

## Task 8: Final verification, protected-path audit, and handoff

- [ ] **Step 1: Run the complete required checks.**

  ```bash
  gofmt -w .
  go test ./...
  go test -race ./...
  go vet ./...
  go build ./...
  git diff --check
  node --check web/app.js
  ```

- [ ] **Step 2: Re-run the read-only command after all formatting/build changes.**

  ```bash
  HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent run
  ```

  Confirm the final JSON/report is from this run, route-category accounting
  sums to the processed unique vacancies, AI was called only for `SELECTED`,
  and writes remain zero.

- [ ] **Step 3: Audit the diff.**

  ```bash
  git diff --name-only "${IMPLEMENTATION_START_SHA}"..HEAD
  git status --short
  git diff --stat "${IMPLEMENTATION_START_SHA}"..HEAD
  ```

  Verify protected files and generated authenticated data are absent from the
  staged diff. Verify no HH POST occurred in runtime tests or the real run.

- [ ] **Step 4: Push only after all checks pass.**

  ```bash
  git push origin main
  git rev-parse HEAD
  git rev-parse origin/main
  ```

- [ ] **Step 5: Final handoff format.** Report exactly:

  ```text
  RESET-6 ROUTER

  Before: selected / ambiguous / no-suitable
  After: selected / ambiguous / no-suitable
  AI evaluated: count
  Matches: count
  Pilot candidates: count/list
  Writes: 0
  POST: 0
  Commit: SHA
  origin/main: SHA
  ```

## Rollback and safety

- The router change is isolated to deterministic scoring, evidence telemetry,
  runtime counters, tests, and validation docs. No transport or write code is
  changed.
- If shadow review finds an obviously wrong selection, stop before push, keep
  the old baseline JSON, revert only the router calibration commits, and add a
  failing fixture for the observed error before retrying.
- A deployment rollback is a source rollback to the last known-good commit;
  no migration or persistent-data rollback is required.
- Generated local reports remain untracked and are not part of rollback or
  release state.
