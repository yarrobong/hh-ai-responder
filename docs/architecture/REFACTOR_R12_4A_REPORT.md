# Before

## ApplyVacancies

Before R12.4a, `ApplyVacancies` performed the complete batch workflow: it
loaded local already-responded state, fetched and deduplicated search results,
applied deterministic filters, read descriptions, resolved Candidate context,
called the typed vacancy-analysis leaf, derived the authoritative deterministic
decision, created unknown-requirement questions, read vacancy applicability,
prepared cover letters and tests, and then submitted through the legacy HH
write edge. The root also projected local events and maintained write-related
counters.

## Triggers

| Trigger | Mode | Can submit | Cadence | Limits |
|---|---|---:|---|---|
| `Run()` default loop with `autoApply` | automatic | yes, when existing write gates allow | actual code: 12h after completion | vacancy/run/application and HH gateway limits |
| `HH_RUN_ONCE` / `run-once` | one-shot | yes, when existing write gates allow | once | same per-run limits |
| direct `ApplyVacancies()` callers/tests | caller-controlled | according to existing flags | caller-controlled | same configured limits |

The nearby comment still says “every 24h”, while the executable loop uses
`time.After(12 * time.Hour)`. R12.4a does not alter this cadence or comment.

## Selection and filters

Search profiles are fetched in configured order and vacancies are deduplicated
by ID. Existing local already-responded state, labels, archive/response state,
response-count threshold, desktop-link requirement, vacancy-per-run limit,
empty-description check, salary/currency threshold, and configured exclusion
keywords remain authoritative. No new matching heuristic or blacklist was
introduced.

## Candidate context

The canonical Candidate context is resolved before AI analysis for each
eligible vacancy. The resolver remains the source of safe, query-specific
Candidate claims; exact experience-month values are carried through without
rounding. Unknown claims remain unknown.

## AI analysis

Vacancy analysis is still performed only after deterministic early filtering and
uses `internal/usecase/vacancyanalysis`. Root composition supplies the typed
leaf with the existing model/options. There is no raw completion call in the
new package.

## Decision

The root compatibility policy continues to derive `MATCH`, `REJECT`, and
`REVIEW_REQUIRED` from the typed assessment. Confirmed hard failures reject;
unknown hard requirements cannot become a match because the AI recommended it.

## Cover letter

When the existing policy requires a letter, or force-letter is enabled, the
typed `internal/usecase/coverletter` leaf prepares and validates local text.
Semantic retrieval remains optional prompt context. A letter is never treated
as write authorization.

## Tests

When early applicability reports a test, the typed test-answer leaf receives
the exact normalized task/option snapshot. The prepared result preserves task
and provider option identities. Final fresh metadata comparison remains at the
root submission edge.

## Submission

`internal/usecase/applicationprocessing` performs no HH mutation. The root
still builds the legacy payload, performs the existing fresh test metadata
comparison, calls `SendResponse`, and projects accepted/error/preview events.
The existing R11 write gate and dry-run behavior remain in force.

# Characterization

| Case | Existing behavior | Preserved |
|---|---|---:|
| zero vacancies | fetch completes; run summary is emitted | YES |
| archived/already responded/labeled | deterministic skip before AI | YES |
| response unavailable / missing desktop link | deterministic skip | YES |
| excluded role/technology, salary below threshold, empty description | deterministic skip before AI | YES |
| salary unknown | existing deterministic policy decides; no currency conversion added | YES |
| office/location and remote handling | existing preflight/structured requirement policy | YES |
| experience requirements | existing typed analysis and deterministic hard requirement policy | YES |
| AI recommends but hard requirement fails | no prepared application | YES |
| unknown hard requirement | review and best-effort Candidate question; never apply | YES |
| malformed/provider AI failure | item error/skip; batch continues as before | YES |
| required cover letter | typed cover-letter leaf is invoked | YES |
| cover-letter failure | item fails before submission | YES |
| absent/required test | test leaf is skipped/invoked according to applicability | YES |
| incomplete/invalid test answer | no prepared test plan | YES |
| stale test metadata | final submission is blocked | YES |
| per-run limits | counters remain in root and are not converted to prepared-count authorization | YES |
| item failure | current batch continues except existing stop conditions | YES |
| cancellation | caller context is propagated; cancellation is returned | YES |
| dry-run/write-disabled | preparation remains available; HH mutation count is zero in dry-run | YES |

# Current flow classification

| Step | Before owner | After R12.4a owner |
|---|---|---|
| search/read discovery and batch order | root | root |
| local already-responded state and batch limits | root | root |
| deterministic vacancy selection | root helpers | root `Policy` adapter consumed by `applicationprocessing` |
| description read | root | `applicationprocessing.Service` via narrow reader |
| Candidate context assembly | root | `applicationprocessing.Service` via resolver capability |
| typed vacancy analysis | root AI wrapper | `applicationprocessing.Service` via typed analyzer |
| authoritative decision | root | injected deterministic `Policy` |
| Candidate clarification intent | root | service delegates to clarification capability; root compatibility adapter retains existing R12.3-compatible question behavior |
| early applicability read | root | service via narrow read capability |
| cover-letter preparation | root | service via typed cover-letter capability |
| test metadata/answer preparation | root write method | service via typed test-answer capability |
| final fresh preflight | root | root, immediately before legacy submission edge |
| HH mutation | root | root only |
| local result/event projection | root | root |

# Application processing

Package: `internal/usecase/applicationprocessing`

Service: `Service.Prepare(context.Context, Request) (Result, error)` processes
one vacancy. Batch order, counters, stop conditions, and event projection stay
outside the service.

Input includes the domain vacancy, selected resume identity, bounded Candidate
facts, cover-letter facts, prompts, and existing runtime options.

Result contains semantic outcomes (`PREPARED`, `SKIPPED`,
`NEEDS_CANDIDATE_INPUT`, `MANUAL_REVIEW`, `ALREADY_RESPONDED`, and
`UNAVAILABLE`), assessment/context evidence, an early applicability snapshot,
and optionally a `PreparedApplication`.

Dependencies are narrow interfaces for vacancy reads, Candidate context,
typed vacancy analysis, typed cover letters, typed test answers, optional
semantic hints, Candidate clarification, and deterministic policy.

# Selection

The service calls `Policy.EarlyReject` before the description read, then reads
the description and calls `Policy.DescriptionReject` before resolving context
or calling AI. The root adapter delegates those checks to the existing
deterministic policy functions.

# Deterministic filters

No filters were duplicated as business logic in the new package. The service
invokes the injected policy; the root adapter owns compatibility with existing
salary, exclusion, response-count, location/work-format, structured
requirement, and hard-requirement functions.

# Candidate safety

Truth authority: canonical Candidate context and the existing resolver.

Unknown hard requirements: remain unknown and produce
`NEEDS_CANDIDATE_INPUT` through the clarification capability.

Mutation: **NONE in `applicationprocessing`**. Clarification creation is
best-effort and cannot create a prepared application.

# Vacancy analysis

Leaf: `internal/usecase/vacancyanalysis`.

Final authority: deterministic Go policy supplied by the root adapter. The AI
assessment cannot override a confirmed hard Candidate failure or turn an
unknown fact into a confirmed match.

# Cover letter

The typed cover-letter leaf is called only after a deterministic match and
only when the existing letter-required or force-letter policy says it is
needed. Its validated output is local prepared content.

# Test preparation

Metadata: the early read capability reads the current test identity and exact
tasks/options when applicability says a test is present.

Leaf: `internal/usecase/testanswer`.

Structural validation: the leaf requires complete, unique, source-ordered
answers and validates each choice ID against the supplied task snapshot.

Fresh-send validation: **REMAINS R11.3b** at the root write edge. The original
snapshot is compared to a fresh provider snapshot immediately before sending;
identities are never remapped.

# Prepared application

Fields: vacancy identity/domain value, selected resume identity/title, local
cover letter, prepared test metadata/tasks/answers, analysis, and Candidate
context.

Write authorization: **NO**. The value contains no provider POST result,
delivery evidence, gateway outcome, approval, or fresh-state grant.

# Limits

Vacancy and application counters remain root-owned. The application limit is
checked after the same analysis/applicability point as the former flow, before
cover-letter/test preparation. Actual submission counters are not moved into
the preparation package.

# Error behavior

Read, AI, cover-letter, and test errors return from the one-vacancy service and
are converted by the root batch to the existing per-item error/continue path.
Candidate clarification errors are intentionally non-authorizing and do not
permit an application. Context cancellation is passed through all capability
calls; no `context.Background()` was introduced by the new package. No new
orchestration retries were added.

# Root compatibility

`ApplyVacancies` remains the batch owner. It now delegates the non-transport
selection/analysis/preparation sequence to `applicationprocessing.Service` and
retains counters, run stop conditions, local events, final preflight, and
submission. This is intentionally not claimed to be a fully thin root; the
remaining write-containing orchestration is the R12.4b subject.

# R12.4b inventory

Submission entry: root `applyPreparedApplication`, then existing `ApplyVacancy`
or the legacy payload path for a prepared test.

Fresh vacancy preflight: existing `SendResponse`/`requireLiveApplicationPreflight`
path remains root-owned.

Fresh test metadata: existing `hhwritepreflight.CompareTestMetadata` remains
immediately before the atomic response.

Write-enabled/dry-run: existing flags and dry-run short circuit remain.

Gateway: existing `SendResponse` and `hhwritegateway` path.

Atomic request: unchanged legacy vacancy-plus-test payload shape.

Outcome/local projection/retry: existing root transport/result/error handling;
no new retry was introduced.

# Capability containment

HH read: narrow root adapters only.

AI: **TYPED LEAVES** (`vacancyanalysis`, `coverletter`, `testanswer`).

Candidate truth mutation: **NONE in the new package**.

HH write: **NONE in `applicationprocessing`**.

# Tests

`internal/usecase/applicationprocessing/service_test.go` uses only fakes. It
covers early rejection without AI/leaf calls, hard-failure rejection, unknown
hard-fact clarification, normal letter/test preparation, exact provider task
and option identities, cancellation, and incomplete-answer blocking.

# Dependencies

`applicationprocessing` imports only domain/value and typed use-case packages.
It does not import the root package, dashboard/CLI, concrete JSON/Postgres
storage, `internal/ports/hhwrite`, HH write adapters, `hhwritegateway`, or a
raw completion provider.

# Behavior

Selection, Candidate truth, unknown handling, typed AI leaves, deterministic
decision authority, cover letters, test validation, resume identity, limits,
item continuation, dry-run, write-enabled behavior, and atomic response shape
remain unchanged. No AIDraft approval was added. Live HH writes from the new
package: **0**.

# Verification

`go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`,
`git diff --check`, and `node --check web/app.js`: **PASS**. Focused package
and regression commands: **PASS**. Docker build: **SKIPPED** because the Docker
CLI is installed but the daemon is unavailable (`Cannot connect to the Docker
daemon at unix:///var/run/docker.sock`).

# R12 status

R12.4a: **EXTRACTED**

# Ready

R12.4b — Legacy Automatic Application Submission Orchestration: **READY**
