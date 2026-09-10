# Before

Packages:

```
hh-ai-responder
hh-ai-responder/internal/cli
hh-ai-responder/internal/config
hh-ai-responder/internal/platform
```

Vacancy definition location:

`main.go`, with related value types split between `main.go`, `job_application.go`
and `reconciliation.go`.

Vacancy direct type dependencies:

`NamedObject`, `Company`, `Compensation`, `ChangeTime`, `MatchResult`,
`ApplicationRecommendation`, `DataCompleteness` and
`ReconciliationEvidence`.

Methods:

`Vacancy.MarshalJSON` and `Vacancy.UnmarshalJSON` were defined in `main.go`.
`MatchResult.validate` and `MatchResult.normalize` were defined in
`job_application.go`.

Serialization:

Vacancy emitted both `id` and legacy `vacancyId`; `UnmarshalJSON` preferred a
non-zero `id`, fell back to `vacancyId`, and tracked explicit versus absent or
null `totalResponsesCount` with `TotalResponsesCountKnown`.

# Dependency classification

| Type | Classification | Decision |
| --- | --- | --- |
| `Vacancy` | `VACANCY_OWNED` | Moved to `internal/vacancy`. |
| `NamedObject` | `LEGACY_HH_COMPATIBILITY` | Moved with the current Vacancy JSON shape. |
| `Company` | `LEGACY_HH_COMPATIBILITY` | Moved with the current Vacancy JSON shape. |
| `Compensation` | `LEGACY_HH_COMPATIBILITY` | Moved with the current HH-shaped field and formatting behavior. |
| `ChangeTime` | `LEGACY_HH_COMPATIBILITY` | Moved with the current Vacancy JSON shape. |
| `MatchResult` / `MatchRisk` | `CROSS_DOMAIN_VALUE` | Moved temporarily; application persistence still consumes the value. |
| `ApplicationRecommendation` / `RecommendationDecision` | `CROSS_DOMAIN_VALUE` | Moved temporarily because the value is attached to matching output. |
| `DataCompleteness` | `CROSS_DOMAIN_VALUE` | Moved temporarily; used by vacancy and application reconciliation. |
| `ReconciliationEvidence` | `CROSS_DOMAIN_VALUE` | Moved temporarily as a plain value type; reconciliation behavior stayed in root. |
| HH request/response DTOs | `LEGACY_HH_COMPATIBILITY` | Deferred; transport DTOs remain in package main. |
| AI evaluation types and prompts | `DEFER` | Remain in root because they are AI/transport concerns. |
| Vacancy store and repositories | `INFRASTRUCTURE` | Remain in root; no storage moved. |

# Extraction

Package:

`internal/vacancy`

Files:

- `doc.go`
- `model.go`
- `rules.go`
- `model_test.go`
- `rules_test.go`

Types moved:

`Vacancy`, `NamedObject`, `Company`, `Compensation`, `ChangeTime`,
`MatchResult`, `MatchRisk`, `RecommendationDecision`,
`ApplicationRecommendation`, `DataCompleteness` and
`ReconciliationEvidence`.

Methods moved:

- `Vacancy.MarshalJSON`
- `Vacancy.UnmarshalJSON`
- `MatchResult.Validate` (temporary compatibility export)
- `MatchResult.Normalize` (temporary compatibility export)

Pure functions moved:

- `FormatCompensation`
- `DeterministicRejectReason`

`vacancy_analyzer.go` was audited but not moved: its policy depends on
Candidate models and candidate-safe projections. AI-dependent matching and
prompt code in `vacancy_match.go` also stayed in root. `normalizeSalaryCurrency`
stayed in root because it delegates to configuration policy.

# Deferred Vacancy behavior

HH:

Search parsing, HH DTOs, URLs, requests, cookies and preflight remain in
`package main`.

AI:

Prompts, model contracts, retries, parsing and evaluation remain in root.

Storage:

`vacancy_store.go`, JSON persistence, repositories and PostgreSQL remain in
root. No schema or filename changed.

Candidate-dependent:

Analyzer logic, candidate evidence derivation, location/work-format risk and
experience analysis remain in root.

Config-dependent:

Thresholds and keyword lists are passed into the pure rule at the caller
boundary; `internal/vacancy` imports no configuration package.

# Legacy compatibility inside vacancy

HH-shaped fields/tags temporarily retained:

`WorkSchedule`, `WorkExperience`, `Links`, `TotalResponsesCount`, `Area`,
`Company`, `Compensation`, `CreationTime`, `LastChangeTime`, `UserLabels`,
`@responseLetterRequired`, `UserTestPresent`, `Archived`, `vacancyId` and the
existing response-count knownness behavior.

Cross-domain value types temporarily retained:

`MatchResult`, `MatchRisk`, `ApplicationRecommendation`,
`RecommendationDecision`, `DataCompleteness` and `ReconciliationEvidence`.

Reason:

They are plain value types already embedded in Vacancy, but current
application/reconciliation/storage consumers still use them. Moving them
mechanically preserves one implementation without importing root or creating a
generic shared package. Their eventual owners should be revisited in R4.2 and
later application/reconciliation stages.

# Root compatibility

Aliases:

`vacancy_compat.go` provides aliases for all moved types and constants. It also
keeps a root `FormatCompensation` wrapper and narrow wrappers for the moved
deterministic rule and MatchResult validation/normalization.

Compatibility exports:

`MatchResult.Validate` and `MatchResult.Normalize` exist solely because Go
does not allow methods on a non-local root alias. They are documented as
temporary staged compatibility API.

Duplicated implementations:

NONE. There is one Vacancy struct and one Vacancy JSON implementation.

# JSON compatibility

Marshal:

PASS — custom marshaling moved mechanically and still emits both `id` and
`vacancyId`.

Unmarshal:

PASS — custom unmarshaling moved mechanically.

vacancyId:

PASS — legacy-only input remains supported and domain `id` wins when both are
present.

zero/absent semantics:

PASS — explicit zero response count is distinct from absent and null count.

existing store compatibility:

PASS — root store regression writes and reads a synthetic vacancy through the
root aliases without changing the storage path or schema.

# Dependencies

`go list ./...`:

```text
hh-ai-responder
hh-ai-responder/internal/cli
hh-ai-responder/internal/config
hh-ai-responder/internal/platform
hh-ai-responder/internal/vacancy
```

`go list -deps ./internal/vacancy`:

PASS — standard library dependencies only; no `hh-ai-responder` business or
infrastructure package, PostgreSQL/pgx, HH implementation or AI package.

Forbidden imports:

NONE.

# Tests

Moved:

No existing root-only test was deleted. Existing root tests remain integration
coverage; new package tests cover the extracted public JSON/value contract.

Added:

- ordinary Vacancy marshal/unmarshal and representative round trip;
- `id`/`vacancyId` precedence;
- absent, null and explicit-zero response count;
- embedded MatchResult/ApplicationRecommendation and reconciliation values;
- compensation formatting;
- deterministic salary/keyword rules;
- root alias → matching rule → JSON store regression.

Root integration:

`vacancy_boundary_test.go`.

# main.go

Before LOC:

4798 (pre-R4.1 worktree snapshot; reconstructed from the audited source).

After LOC:

4645.

Vacancy declarations removed:

The `Vacancy` struct, custom JSON methods, `NamedObject`, `Company`,
`ChangeTime`, `Compensation` and `FormatCompensation` were removed. Existing
unrelated R3 configuration/CLI work in `main.go` was preserved.

Root production LOC before/after:

40521 / 40223, counted at repository root excluding test files. The reduction
is intentionally modest; ownership and compatibility are the success criteria.

`internal/vacancy` production LOC:

309, excluding tests.

# Behavior

Vacancy model:

UNCHANGED

Matching:

UNCHANGED — deterministic salary/keyword behavior is mechanically relocated;
AI and candidate-dependent matching remains in root.

Salary:

UNCHANGED

Location/work-format:

UNCHANGED

Application:

UNCHANGED

Candidate:

UNCHANGED

Conversation:

UNCHANGED

AI:

UNCHANGED

HH Read:

UNCHANGED

HH Write:

UNCHANGED; no write path was changed or invoked.

Dashboard:

UNCHANGED

Storage:

UNCHANGED

CLI:

UNCHANGED

# Verification

gofmt:

PASS

Focused package tests:

PASS — `go test ./internal/vacancy ./internal/cli ./internal/config ./internal/platform`.

`go test ./...`:

PASS

The remaining required race/vet/build/browser/Docker checks are recorded after
the final verification run below.

`go test -race ./...`:

PASS

`go vet ./...`:

PASS

`go build ./...`:

PASS

`git diff --check`:

PASS

`node --check web/app.js`:

PASS

Docker:

SKIPPED — Docker is installed, but `docker info` reported that the Docker
daemon is unavailable.

LIVE HH WRITES:

0. `HH_WRITE_ENABLED=false` and `HH_DRY_RUN=true`; no HH write-capable process
was run.

# Architectural debt discovered

- HH-shaped compatibility fields still sit in the domain model and should
  move behind a future HH adapter without changing local JSON migration
  behavior.
- Match results and reconciliation metadata are temporarily vacancy-owned
  value types even though their eventual ownership crosses Application and
  reconciliation boundaries.
- Root aliases and wrappers are intentionally transitional and should be
  removed as callers migrate.

# Pattern for next extraction

What worked:

Characterizing custom JSON first, moving methods with their local types, and
using aliases for staged consumers preserved the existing application with a
small import surface. Pure rules can accept plain thresholds from the caller
without importing configuration.

What should change for Application:

First classify whether MatchResult and ApplicationRecommendation belong to the
application aggregate or remain a separate evaluation value. Do not move
application persistence, reconciliation or conversation links together with a
domain model merely to reduce root LOC.

# Ready for R4.2

R4.2 — Application Domain Boundary

READY
