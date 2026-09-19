# RESET-8A AI policy classification and telemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the two proven hard-requirement classification defects from the RESET-8 audit and add bounded, backward-compatible provenance telemetry while preserving all existing safety and decision semantics.

**Architecture:** Keep AI extraction advisory and candidate-free. Enrich each locally derived hard-requirement evaluation with deterministic source/classification telemetry, then keep the existing `vacancyDecisionWithReason` precedence as the sole final policy. Execute the changes against a committed sanitized 12-case replay fixture before any live shadow run.

**Tech Stack:** Go, standard-library JSON/regexp/string handling, existing `go test`/race/vet/build checks, JSON career-agent reports, `httptest` fixtures where transport behavior is tested.

**Spec:** `docs/superpowers/specs/2026-09-19-reset-8-ai-policy-audit-design.md`

## Global Constraints

- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` for every validation and replay command.
- Preserve score threshold `65`.
- Preserve Stage 29.6 precedence: hard `MISSING` → `REJECT`; score `<65` → `REJECT`; hard `UNKNOWN` → `REVIEW_REQUIRED`; advisory recommendation → `REVIEW_REQUIRED`; otherwise `MATCH`.
- Preserve AI recommendation advisory semantics.
- Do not modify Search Planner, RESET-6 Router scoring, detail sequencing, HH transport/auth, cookies/session handling, application send, nonce, reconciliation, write gateway, or cover-letter generation.
- Do not save cookies, authenticated raw HTML, complete private prompts, secrets, or fabricated candidate evidence.
- `UNKNOWN` means insufficient trusted evidence; it must never be converted to `MISSING` or `MET` for convenience.
- Bare stack fragments remain observationally `AMBIGUOUS` until bounded source context proves a mandatory/preference cue; no mass decision-semantic change is allowed for them in RESET-8A.
- The two required replay transitions are classification/reason corrections only: 137531969 remains `REJECT` for score; 136577315 remains `REVIEW_REQUIRED` for advisory recommendation.
- `MATCH` count is not an optimization target.

## Review Focus

- Role/technology duration `2 года AI/ML/NLP` must not use generic total experience; Task 4 pins it to `UNKNOWN`.
- Optional wording must not create a hard blocker, while `обязателен` and office-only wording remain hard; Tasks 4 and 5 pin both sides.
- Absent stack evidence must stay `UNKNOWN`, not fabricated `MET` or `MISSING`; Task 7 pins this.
- The first valid local gate must remain stable after telemetry is added; Task 8 replays all 12 rows and checks exact transitions.
- Dry-run must still block every HH write and Application POST; Task 9 runs the existing write-safety suite and controlled shadow validation.

---

## File map

| File | Responsibility in RESET-8A |
|---|---|
| `internal/usecase/vacancyanalysis/types.go` | Backward-compatible telemetry types attached to locally derived hard requirements. |
| `internal/usecase/vacancyanalysis/requirement_context.go` | Shared optional/mandatory cue and bounded source-context classification. |
| `internal/usecase/vacancyanalysis/validation.go` | Duration classification, work-mode/location classification, local status derivation, and telemetry assembly. |
| `internal/usecase/vacancyanalysis/requirement_context_test.go` | Unit tests for cue classification, source windows, negation, and telemetry labels. |
| `internal/usecase/vacancyanalysis/service_test.go` | Service-level tests proving AI output is locally re-evaluated and status is not trusted from provider output. |
| `internal/runtime/vacancy_match.go` | Row-level local policy gate telemetry and existing precedence; no precedence changes. |
| `internal/runtime/career_agent_events.go` | No semantic change; verify route event/detail sequencing remains represented correctly. Modify only if the replay needs a backward-compatible optional field. |
| `internal/runtime/reset8a_replay_test.go` | Deterministic frozen 12-case replay and exact old/new transition assertions. |
| `internal/runtime/testdata/reset8a/reset8-selected.json` | Sanitized, committed input fixture for the 12 baseline `ROUTE_SELECTED` evaluations. |
| `internal/runtime/reset8a_telemetry_test.go` | Report compatibility, provenance fields, bounded context, and local-gate serialization checks. |
| `docs/superpowers/specs/2026-09-19-reset-8-ai-policy-audit-design.md` | Corrected architecture, expected transitions, and audit-only stack policy. |
| `docs/superpowers/plans/2026-09-19-reset-8a-implementation-plan.md` | This implementation plan. |

Protected files and paths are intentionally absent from the modification map:
Search Planner, RESET-6 route scoring, `BrowserHHClient`/auth, cookies/session
handling, application send, nonce, reconciliation, write gateway, and
cover-letter generation.

## Task 1: Freeze the sanitized 12-case replay set

**Files:**

- Create: `internal/runtime/testdata/reset8a/reset8-selected.json`
- Create: `internal/runtime/reset8a_replay_test.go`
- Test source: `/tmp/reset8-selected.json` and `/tmp/reset8-baseline-meta.json` from the RESET-8 audit; do not copy raw authenticated reports into the repository.

**Interfaces:**

- Consumes: the 12 selected rows from the baseline fixture, including selected resume identity, router score/runner-up/confidence, AI score/recommendation/reasons, provider hard-requirement candidates, locally derived old statuses, local final decision/reason, and the trusted candidate facts needed to recompute status.
- Produces: a typed `reset8AReplayCase` loader and a deterministic table of exactly 12 vacancy IDs.

- [ ] **Step 1: Write the fixture loader test.**

Add a test that loads `testdata/reset8a/reset8-selected.json` with `os.ReadFile`
and `json.Decoder`, then asserts:

```go
if len(cases) != 12 {
	t.Fatalf("replay cases=%d, want 12", len(cases))
}
if cases[0].VacancyID != 137546982 || cases[11].VacancyID != 137500466 {
	t.Fatalf("unexpected replay ordering: first=%d last=%d", cases[0].VacancyID, cases[11].VacancyID)
}
for _, item := range cases {
	if item.SelectedResumeID == "" || item.AIScore < 0 || item.AIRecommendation == "" {
		t.Fatalf("incomplete replay case %d: %+v", item.VacancyID, item)
	}
}
```

- [ ] **Step 2: Run the loader test and verify it fails before the fixture exists.**

Run:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run TestReset8AReplayFixture -count=1
```

Expected: FAIL because the fixture and loader are not yet present.

- [ ] **Step 3: Add the sanitized fixture and loader.**

The fixture must contain only local replay data. It must not contain cookies,
authenticated HTML, complete prompts, secrets, or unbounded vacancy bodies.
Use explicit JSON fields for:

```text
vacancy_id, title, role_family
selected_resume_id, selected_resume_title
router_score, runner_up_score, router_confidence, router_reason
ai_score, ai_recommendation, ai_recommendation_reasons, ai_reasons
hard_requirement_candidates
trusted_candidate_facts
old_hard_statuses, old_local_gate, old_final_decision, old_final_reason
```

Each requirement candidate also carries only the bounded source data needed by
the deterministic replay:

```text
source_field
source_context (sanitized, max 240 runes)
relevant_work_mode_or_location_field
relevant_structured_experience_field
```

The office case must retain the bounded semantic context
`если не любите удалёнку, есть офис в Москве`; `Москва (офис)` alone is not a
valid replay input for the work-mode classifier.

Define the Go loader in `reset8a_replay_test.go` with a fixed schema and reject
unknown or missing required fields. Do not make the test call the live HH
provider or the AI provider.

- [ ] **Step 4: Run the fixture test and verify it passes.**

Run:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run TestReset8AReplayFixture -count=1
```

Expected: PASS with exactly the 12 baseline IDs and no network calls.

- [ ] **Step 5: Commit the frozen input independently.**

```bash
git add internal/runtime/testdata/reset8a/reset8-selected.json internal/runtime/reset8a_replay_test.go
git commit -m "test: freeze RESET-8A selected replay set"
```

## Task 2: Add backward-compatible requirement telemetry types

**Files:**

- Modify: `internal/usecase/vacancyanalysis/types.go`
- Modify: `internal/runtime/vacancy_match.go`
- Create: `internal/runtime/reset8a_telemetry_test.go`

**Interfaces:**

- Consumes: existing `HardRequirementCandidate`, `HardRequirementEvaluation`, `CareerAgentVacancyResult`, and `RunSummaryResult`.
- Produces: optional JSON fields that older readers can ignore and that are populated only from deterministic local classification.

- [ ] **Step 1: Write serialization tests for the new optional fields.**

Test a hard requirement with the following expected telemetry shape:

```go
telemetry := RequirementTelemetry{
	SourceContext: "Опыт в AI/ML/NLP не менее 2 лет.",
	SourceField: "description",
	ExtractionClassification: "EXPLICIT_HARD",
	MandatoryCue: "minimum_duration",
	CandidateEvidenceProvenance: "CANDIDATE_PROFILE",
	ExperienceClassification: "TECHNOLOGY_SPECIFIC_DURATION",
	ClassificationDiagnostics: []string{"role_marker:ai_ml_nlp"},
}
```

Assert that old JSON without telemetry still unmarshals successfully and that
the new JSON omits no required legacy fields. Assert `len(SourceContext) <=
240` runes and that no field contains `cookie`, `authorization`, or a prompt
header.

- [ ] **Step 2: Run the serialization tests and verify they fail.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'TestReset8ATelemetry|TestHardRequirement.*Telemetry' -count=1
```

Expected: FAIL because the telemetry types/fields do not exist.

- [ ] **Step 3: Add the types without changing decision behavior.**

In `internal/usecase/vacancyanalysis/types.go`, add string constants and a
`RequirementTelemetry` struct with `omitempty` JSON fields:

```go
type RequirementTelemetry struct {
	SourceContext               string   `json:"source_context,omitempty"`
	SourceField                 string   `json:"source_field,omitempty"`
	ExtractionClassification    string   `json:"extraction_classification,omitempty"`
	MandatoryCue                string   `json:"mandatory_cue,omitempty"`
	CandidateEvidenceProvenance string   `json:"candidate_evidence_provenance,omitempty"`
	ExperienceClassification    string   `json:"experience_classification,omitempty"`
	ClassificationDiagnostics   []string `json:"classification_diagnostics,omitempty"`
}
```

Add `Telemetry *RequirementTelemetry` to `HardRequirementEvaluation` with
`json:"telemetry,omitempty"`. Add `LocalPolicyGate string` to
`CareerAgentVacancyResult` with `json:"local_policy_gate,omitempty"`.
Keep `FinalReasonCode` as the row-level final reason. Do not change existing
status, decision, threshold, or precedence code in this task.

- [ ] **Step 4: Populate no fields yet and run compatibility tests.**

Run:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime ./internal/usecase/vacancyanalysis -count=1
```

Expected: PASS; old fixtures remain valid and new fields are optional.

- [ ] **Step 5: Commit the telemetry contract.**

```bash
git add internal/usecase/vacancyanalysis/types.go internal/runtime/vacancy_match.go internal/runtime/reset8a_telemetry_test.go
git commit -m "feat: add RESET-8A requirement telemetry contract"
```

## Task 3: Add the failing frozen transition gate before semantic fixes

**Files:**

- Modify: `internal/runtime/reset8a_replay_test.go`
- Test data: `internal/runtime/testdata/reset8a/reset8-selected.json`

**Interfaces:**

- Consumes: the Task 1 fixture, Task 2 telemetry contract, the production local derivation path, and `vacancyDecisionWithReason`.
- Produces: a RED test that records the two required corrections before any duration, cue, or work-mode implementation changes.

- [ ] **Step 1: Write the exact transition assertions.**

The test must invoke the same local derivation and decision functions used by
production. It must not duplicate Stage 29.6 precedence. Assert:

```text
137531969: AI/ML/NLP MISSING -> expected UNKNOWN
             HARD_REQUIREMENT_MISSING -> FIT_SCORE_BELOW_THRESHOLD
             REJECT -> REJECT

136577315: office UNKNOWN -> no hard blocker
             HARD_REQUIREMENT_UNKNOWN -> AI_ADVISORY_CONCERN
             REVIEW_REQUIRED -> REVIEW_REQUIRED
```

Require the other ten rows to retain their old final decision and reason.

- [ ] **Step 2: Run the replay and verify RED on current production semantics.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run TestReset8AReplayTransitions -count=1
```

Expected: FAIL only on the two required transitions, with the current observed
old states printed. Any fixture, loader, or unrelated failure must be fixed
before semantic work begins.

- [ ] **Step 3: Commit the RED replay gate.**

```bash
git add internal/runtime/reset8a_replay_test.go internal/runtime/testdata/reset8a/reset8-selected.json
git commit -m "test: add RESET-8A red transition gate"
```

## Task 4: Correct generic versus role/technology-specific duration

**Files:**

- Modify: `internal/usecase/vacancyanalysis/validation.go`
- Test: `internal/runtime/candidate_facts_test.go`
- Test: `internal/usecase/vacancyanalysis/service_test.go`

**Interfaces:**

- Consumes: `descriptionExperienceMinimumMonths`, `containsRoleSpecificExperienceMarker`, `genericDescriptionExperienceStatus`, and trusted `CandidateFacts`.
- Produces: role/technology duration requirements that remain `UNKNOWN` without specific evidence, while generic total-duration requirements retain current `MISSING`/`MET` semantics.

- [ ] **Step 1: Add failing duration fixtures.**

Extend `TestDescriptionExperienceUsesExplicitTotalDurationPolicy` with:

```go
{name: "AI ML NLP duration is role specific", months: 11, description: "Опыт в AI/ML/NLP не менее 2 лет.", wantStatus: hardRequirementStatusUnknown},
{name: "compound AI agent duration is role specific", months: 11, description: "2 года AI-agent разработки", wantStatus: hardRequirementStatusUnknown},
```

Retain and assert the existing generic case:

```go
{name: "generic three years is missing", months: 11, description: "Общий опыт коммерческой разработки от 3 лет.", wantStatus: hardRequirementStatusMissing},
```

Add a fixture proving no specific positive evidence is invented:

```go
got := deriveHardRequirements(
	LegacyCandidateContext{TotalExperienceMonthsKnown: true, TotalExperienceMonths: 11},
	Vacancy{},
	"Опыт в AI/ML/NLP не менее 2 лет.",
	[]HardRequirementCandidate{{Requirement: "2 года AI/ML/NLP", Category: hardRequirementCategoryExperienceYears, VacancyEvidence: "Опыт в AI/ML/NLP не менее 2 лет."}},
)
if got[0].Status != hardRequirementStatusUnknown || strings.HasPrefix(got[0].CandidateEvidence, "Candidate total experience:") {
	t.Fatalf("specific duration used generic candidate evidence: %+v", got[0])
}
```

- [ ] **Step 2: Run the duration tests and verify the new fixtures fail.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime ./internal/usecase/vacancyanalysis -run 'TestDescriptionExperience|Test.*Duration|Test.*Experience' -count=1
```

Expected: FAIL for AI/ML/NLP because the current marker list does not recognize
that compound domain.

- [ ] **Step 3: Implement token-aware role markers.**

Update the shared duration classifier in `validation.go` so `AI`, `ML`, and
`NLP` are recognized as a compound technology/domain marker rather than by
unsafe substring matching. Keep existing markers for Python, Django, DevOps,
SRE, Java, backend, and related roles. Record
`TECHNOLOGY_SPECIFIC_DURATION` and a diagnostic such as
`role_marker:ai_ml_nlp` in telemetry. Do not add a vacancy-ID-specific branch.

The classification rule must be:

```text
role/technology-specific duration + no trusted specific evidence -> UNKNOWN
generic total duration + trusted total below explicit minimum -> MISSING
generic total duration + trusted total meets minimum -> MET
```

- [ ] **Step 4: Run focused tests and the full vacancy-analysis suite.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime ./internal/usecase/vacancyanalysis -run 'TestDescriptionExperience|Test.*Duration|Test.*Experience' -count=1
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/usecase/vacancyanalysis -count=1
```

Expected: PASS; role-specific duration remains `UNKNOWN`, generic 3-year
duration remains `MISSING` for trusted 11-month total.

- [ ] **Step 5: Commit the classifier correction.**

```bash
git add internal/usecase/vacancyanalysis/validation.go internal/runtime/candidate_facts_test.go internal/usecase/vacancyanalysis/service_test.go
git commit -m "fix: preserve unknown for technology-specific experience"
```

## Task 5: Centralize optional and mandatory cue classification

**Files:**

- Create: `internal/usecase/vacancyanalysis/requirement_context.go`
- Create: `internal/usecase/vacancyanalysis/requirement_context_test.go`
- Modify: `internal/usecase/vacancyanalysis/validation.go`

**Interfaces:**

- Consumes: requirement text, `vacancy_evidence`, category-specific source text, and existing vacancy description/work schedule fields.
- Produces: `RequirementContextClassification` with `EXPLICIT_HARD`, `PREFERENCE`, `AMBIGUOUS`, or `NOT_A_REQUIREMENT`, a bounded cue label, and deterministic diagnostics used by all local layers.

- [ ] **Step 1: Write failing cue tests.**

Table-drive these cases:

```go
{text: "Знание Kafka желательно", wantClass: "PREFERENCE"}
{text: "Опыт Kubernetes будет плюсом", wantClass: "PREFERENCE"}
{text: "Опыт с RabbitMQ приветствуется", wantClass: "PREFERENCE"}
{text: "Высшее образование предпочтительно", wantClass: "PREFERENCE"}
{text: "Опыт FastAPI не обязательно", wantClass: "PREFERENCE"}
{text: "Must have PostgreSQL", wantClass: "EXPLICIT_HARD"}
{text: "Требуется опыт от 3 лет", wantClass: "EXPLICIT_HARD"}
{text: "Обязателен опыт работы с ETL", wantClass: "EXPLICIT_HARD"}
{text: "Опыт будет преимуществом, но не обязателен", wantClass: "PREFERENCE"}
```

Assert that a cue in one sentence does not classify a neighboring sentence,
and that `не обязательно` is not lost through substring matching.

- [ ] **Step 2: Run the cue tests and verify they fail.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/usecase/vacancyanalysis -run 'TestRequirementContext|TestOptional|TestMandatory' -count=1
```

Expected: FAIL because the current optional checks are split between multiple
functions and do not expose one shared classification result.

- [ ] **Step 3: Implement the shared classifier.**

Create `classifyRequirementContext(source string, evidence string)`, using
normalized sentence windows and phrase/token boundaries. Return the first
bounded source context, the matched cue, and diagnostics. Apply precedence:

```text
explicit negated/optional cue in the same source sentence -> PREFERENCE
explicit mandatory cue or numeric minimum in the same sentence -> EXPLICIT_HARD
source exists but cue is not decisive -> AMBIGUOUS
source is an office/remote option, not a requirement -> NOT_A_REQUIREMENT
```

Use the same function from candidate filtering and final optional checks. Keep
existing hard requirements safe: optional text is removed from the hard set;
mandatory text remains eligible for local status derivation. Do not use raw
substring matching that treats `не обязательно` as `обязательно`. The
classifier may change hard-blocking semantics only when the cue is present in
the same bounded source sentence/context. Isolated words outside that context
remain `AMBIGUOUS` telemetry and do not create a new decision rule. Bare stack
fragments continue through the current safe local path: absent candidate
evidence remains `UNKNOWN`.

- [ ] **Step 4: Run focused and compatibility tests.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/usecase/vacancyanalysis -run 'TestRequirementContext|TestOptional|TestMandatory' -count=1
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'Test.*Hard.*Requirement|Test.*Experience' -count=1
```

Expected: PASS; absent candidate evidence remains `UNKNOWN`, and explicit
mandatory cues remain hard.

- [ ] **Step 5: Commit cue centralization.**

```bash
git add internal/usecase/vacancyanalysis/requirement_context.go internal/usecase/vacancyanalysis/requirement_context_test.go internal/usecase/vacancyanalysis/validation.go
git commit -m "refactor: centralize requirement cue classification"
```

## Task 6: Fix office availability versus office requirement

**Files:**

- Create: `internal/usecase/vacancyanalysis/work_mode.go`
- Create: `internal/usecase/vacancyanalysis/work_mode_test.go`
- Modify: `internal/usecase/vacancyanalysis/validation.go`

**Interfaces:**

- Consumes: requirement/evidence text, vacancy work schedule, description, area, and candidate location.
- Produces: a deterministic work-mode classification used by location hard-requirement filtering without inferring relocation or willingness.

- [ ] **Step 1: Write failing work-mode fixtures.**

```go
{text: "Если не любите удалёнку, есть офис в Москве", want: "OFFICE_AVAILABLE"}
{text: "Работа только из офиса в Москве", want: "OFFICE_REQUIRED"}
{text: "Обязательна готовность к работе из офиса", want: "OFFICE_REQUIRED"}
{text: "Удалённая работа доступна", want: "REMOTE_AVAILABLE"}
{text: "Готовность к релокации в Москву обязательна", want: "RELOCATION_REQUIRED"}
```

Assert that `OFFICE_AVAILABLE` returns `NOT_A_REQUIREMENT` when it is emitted
as a location candidate and that candidate relocation is never inferred.

- [ ] **Step 2: Run work-mode tests and verify they fail.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/usecase/vacancyanalysis -run 'TestWorkMode|TestLocation' -count=1
```

Expected: FAIL for the office-availability fixture because the current logic
keeps any sentence containing `офис` as potentially blocking.

- [ ] **Step 3: Implement the classifier and wire it into local filtering.**

Define `WorkModeClassification` values `OFFICE_REQUIRED`,
`RELOCATION_REQUIRED`, `LOCATION_REQUIRED`, `OFFICE_AVAILABLE`,
`REMOTE_AVAILABLE`, and `WORK_MODE_OPTION`. Use explicit mandatory/only cues
and sentence context. Update `locationRequirementIsNonBlocking` and
`isOptionalRequirementCandidate` to classify “there is an office” as an
available option, while preserving explicit office-only and relocation
requirements as hard candidates.

The candidate-side result for a different city remains `UNKNOWN` unless the
vacancy explicitly requires relocation/location and trusted candidate facts
prove a mismatch. Never turn a different city into a negative willingness fact.

- [ ] **Step 4: Run focused and preflight tests.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/usecase/vacancyanalysis -run 'TestWorkMode|TestLocation' -count=1
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'Test.*Location|Test.*Preflight|Test.*Hard.*Requirement' -count=1
```

Expected: PASS; office availability is not a hard requirement and office-only
wording remains hard.

- [ ] **Step 5: Commit the work-mode correction.**

```bash
git add internal/usecase/vacancyanalysis/work_mode.go internal/usecase/vacancyanalysis/work_mode_test.go internal/usecase/vacancyanalysis/validation.go
git commit -m "fix: distinguish office availability from office requirement"
```

## Task 7: Populate bounded provenance and classification diagnostics

**Files:**

- Modify: `internal/usecase/vacancyanalysis/validation.go`
- Modify: `internal/usecase/vacancyanalysis/requirement_context.go`
- Modify: `internal/usecase/vacancyanalysis/types.go`
- Modify: `internal/runtime/vacancy_match.go`
- Test: `internal/runtime/reset8a_telemetry_test.go`

**Interfaces:**

- Consumes: local requirement candidate, vacancy source fields, candidate profile/evidence with any retained `ProfileFact` metadata, duration/work-mode classification, and the existing local decision result.
- Produces: populated `HardRequirementEvaluation.Telemetry`, row-level `LocalPolicyGate`, and the existing `FinalReasonCode` without exposing private source material.

- [ ] **Step 1: Write failing provenance tests.**

Cover these exact source mappings:

```text
experience_years -> vacancy.WorkExperience or bounded description context
location         -> Area.Name or WorkSchedule/description
skill/other      -> bounded description context
generic duration -> GENERIC_TOTAL_DURATION
Python/AI/ML/NLP duration -> TECHNOLOGY_SPECIFIC_DURATION
trusted total months -> CANDIDATE_PROFILE
candidate skill match -> HH_RESUME or CANDIDATE_PROFILE
no candidate evidence -> NONE
```

Assert the office option row has `NOT_A_REQUIREMENT`, the AI/ML/NLP row has
`TECHNOLOGY_SPECIFIC_DURATION`, and all contexts are bounded to 240 runes.

- [ ] **Step 2: Run telemetry tests and verify they fail.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'TestReset8ATelemetry|Test.*Provenance' -count=1
```

Expected: FAIL because local evaluation currently exposes only free-text
`candidate_evidence` and short `vacancy_evidence`.

- [ ] **Step 3: Implement telemetry assembly.**

Add deterministic helpers with explicit return values:

```go
func boundedSourceContext(source, evidence string, maxRunes int) (context string, found bool)
func candidateEvidenceProvenance(match CandidateEvidenceMatch) string
func experienceClassification(requirement, evidence string) string
func requirementTelemetry(candidate CandidateFacts, value vacancy.Vacancy, description string, requirement HardRequirementCandidate) RequirementTelemetry
```

Define `CandidateEvidenceMatch` as an internal result that carries the matched
trusted fact and its actual source metadata before formatting
`candidate_evidence`. The resolver may emit only:

```text
HH_RESUME, CANDIDATE_PROFILE, TRUSTED_PROJECT, WORK_EXPERIENCE, EDUCATION,
EXPLICIT_CONSTRAINT, LEGACY_AGGREGATE, UNKNOWN_SOURCE, NONE
```

Use `HH_RESUME`/`CANDIDATE_PROFILE` only when the retained `ProfileFact` or
canonical evidence explicitly identifies that source. If the match comes from
legacy aggregate `Skills`/`Experience` strings without exact origin, emit
`LEGACY_AGGREGATE` or `UNKNOWN_SOURCE`; do not infer `HH_RESUME` from a skill
match or `CANDIDATE_PROFILE` from total months. No CandidateFacts redesign is
permitted in this task. Use `maxRunes=240`, normalize line breaks, and return
an empty context plus a diagnostic when exact source context cannot be found.
Absent candidate evidence is `NONE`.

After `vacancyDecisionWithReason` returns, set `trace.LocalPolicyGate` to the
exact gate constant (`HARD_REQUIREMENT_MISSING`,
`FIT_SCORE_BELOW_THRESHOLD`, `HARD_REQUIREMENT_UNKNOWN`,
`AI_ADVISORY_CONCERN`, or `MATCH_CONFIRMED`). Preserve
`trace.FinalReasonCode` and all summary counters.

- [ ] **Step 4: Run compatibility and privacy checks.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime ./internal/usecase/vacancyanalysis -count=1
git diff --check
```

Expected: PASS; old report readers still parse the JSON, new fields are
optional, and no secret/private prompt markers appear in fixture output.

- [ ] **Step 5: Commit telemetry population.**

```bash
git add internal/usecase/vacancyanalysis/validation.go internal/usecase/vacancyanalysis/requirement_context.go internal/usecase/vacancyanalysis/types.go internal/runtime/vacancy_match.go internal/runtime/reset8a_telemetry_test.go
git commit -m "feat: emit RESET-8A requirement provenance telemetry"
```

## Task 8: Run the frozen 12-case before/after replay

**Files:**

- Modify: `internal/runtime/reset8a_replay_test.go`
- Modify: `internal/runtime/testdata/reset8a/reset8-selected.json` only if the fixture schema needs a backward-compatible telemetry expectation field; never alter baseline old values.

**Interfaces:**

- Consumes: frozen 12-case fixture, local vacancy analysis/derivation, and `vacancyDecisionWithReason`.
- Produces: a deterministic transition table in test output and hard failure for any unexpected transition.

- [ ] **Step 1: Run the RED transition assertions after the semantic fixes.**

The replay must compare vacancy ID, old/new hard statuses, old/new local gate,
old/new decision, and old/new reason. Require these exact transitions:

```text
137531969: AI/ML/NLP MISSING -> UNKNOWN;
             HARD_REQUIREMENT_MISSING -> FIT_SCORE_BELOW_THRESHOLD;
             REJECT -> REJECT

136577315: Москва (офис) UNKNOWN -> no hard requirement;
             HARD_REQUIREMENT_UNKNOWN -> AI_ADVISORY_CONCERN;
             REVIEW_REQUIRED -> REVIEW_REQUIRED
```

Require the other ten rows to retain their old final decision/reason. Any
unexpected transition is a stop condition: explain it, add or adjust a
regression fixture, and review before continuing.

- [ ] **Step 2: Update the replay to use the corrected local classifier.**

Build each local `Vacancy`, `CandidateFacts`, and `HardRequirementCandidate`
from the fixture, call the same exported/compatibility derivation path used by
production, then call `vacancyDecisionWithReason`. Do not duplicate the
policy precedence inside the test. Print a compact table on failure:

```text
vacancy | old statuses/gate/decision/reason | new statuses/gate/decision/reason
```

- [ ] **Step 3: Run the frozen replay and full relevant tests.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'TestReset8AReplay|Test.*Hard.*Requirement|Test.*Decision' -count=1
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test -race ./...
```

Expected: PASS. The two required transitions occur exactly; `MATCH` need not
increase; all other row transitions are unchanged or individually explained
by an explicit fixture assertion.

- [ ] **Step 4: Commit the replay gate.**

```bash
git add internal/runtime/reset8a_replay_test.go internal/runtime/testdata/reset8a/reset8-selected.json
git commit -m "test: enforce RESET-8A twelve-case transitions"
```

## Task 9: Controlled shadow validation and final safety gate

**Files:**

- No production files are added in this task.
- Read-only outputs: `/tmp/reset8a-new.json`, `/tmp/reset8a-new-selected.json`, `/tmp/reset8a-comparison.md`.

**Interfaces:**

- Consumes: the implementation branch, production-default caps, frozen replay result, and the existing shadow command.
- Produces: a before/after comparison without HH writes and a final review artifact.

- [ ] **Step 1: Run the frozen replay one final time.**

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run TestReset8AReplayTransitions -count=1
```

Expected: PASS with the two required reason transitions.

- [ ] **Step 2: Run a controlled live shadow with production defaults.**

Use the existing command shape and only read-only flags:

```bash
go build -o /tmp/reset8a-hh-ai-responder ./cmd/hh-ai-responder
HH_DRY_RUN=true HH_WRITE_ENABLED=false \
HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 \
HH_MAX_VACANCIES_PER_RUN=100 \
STORAGE_BACKEND=json \
/tmp/reset8a-hh-ai-responder -u "" \
  --career-agent-result /tmp/reset8a-new.json career-agent --shadow
```

Sanitize the result to `/tmp/reset8a-new-selected.json` and compare AI count,
recommendation distribution, hard missing/unknown rows, final reason counts,
MATCH/REVIEW/REJECT, `shadow_write_count`, `would_apply`, and `applied` with
the RESET-8 baseline. Do not treat live IDs as a substitute for the frozen
replay.

- [ ] **Step 3: Run the complete verification gate.**

```bash
gofmt -d .
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test -race ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go vet ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go build ./...
git diff --check
node --check web/app.js
```

Expected: all commands exit 0; no production HH write or Application POST is
observed; no protected path is changed.

- [ ] **Step 4: Review the final diff and protected-path audit.**

Run:

```bash
git diff --name-only 34d41914f183ecc19223d4e1c2435dc762f91148..HEAD
git status --short --untracked-files=all
git diff --stat 34d41914f183ecc19223d4e1c2435dc762f91148..HEAD
```

Confirm only the planned classifier, telemetry, tests, fixture, and docs files
changed. Preserve unrelated existing untracked files and do not stage/delete
them.

There is no implementation commit in Task 9. Tasks 1–8 already provide the
reviewable commits. If the validation produces a new tracked report such as
`docs/validation/VALIDATION_RESET_8A.md`, create only this additional docs
commit after the checks pass:

```bash
git add docs/validation/VALIDATION_RESET_8A.md
git commit -m "docs: validate RESET-8A policy classification"
```

If no tracked validation artifact is created, do not create an empty commit.
Push only the reviewed implementation/docs commits after the replay and full
verification gate pass:

```bash
git push origin main
git rev-parse HEAD
git rev-parse origin/main
git status --short --untracked-files=all
```

## Expected RESET-8A outcome

The implementation is acceptable when:

```text
137531969: hard MISSING -> UNKNOWN; final REJECT / FIT_SCORE_BELOW_THRESHOLD
136577315: office hard UNKNOWN removed; final REVIEW_REQUIRED / AI_ADVISORY_CONCERN
score threshold remains 65
Stage 29.6 precedence remains unchanged
bare stack fragments remain audit-only unless bounded context proves semantics
UNKNOWN/MISSING/MET remain evidence-distinct
frozen 12-case replay is explainable
Real HH writes = 0
Application POST = 0
```

An increase in `MATCH` is neither required nor sufficient. Any unexpected
transition among the other ten frozen cases, any unsafe positive candidate
evidence, any unbounded/private telemetry, or any write-path activity stops the
rollout for review.
