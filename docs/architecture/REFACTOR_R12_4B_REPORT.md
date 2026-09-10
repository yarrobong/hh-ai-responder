# Before

## ApplyVacancies submission

`ApplyVacancies` remained the automatic batch owner. After discovery, local
already-responded checks, deterministic filters, preparation, match events,
and limit checks, it called the root `applyPreparedApplication` compatibility
helper. The root then built the legacy vacancy-response payload, performed the
live preflight/test freshness work, called `SendResponse`, and interpreted the
legacy response map for counters and events.

## applyPreparedApplication

Before this stage the helper validated the prepared resume/test shape, called
`ApplyVacancy` for applications without a test, and otherwise built the
test-bearing form, read fresh test metadata, compared it with
`hhwritepreflight.CompareTestMetadata`, and called `SendResponse`. This mixed
submission policy, fresh-state proof, payload construction, and compatibility
mapping in the root.

## Fresh preflight

`requireLiveApplicationPreflight` read or reused the root preflight cache and
delegated its known-bit decision to `hhwritepreflight.Service`. R12.4b removes
the cache shortcut from the automatic prepared path: every non-dry-run
submission now performs a fresh targeted applicability read. Manual public
methods retain their existing compatibility behavior.

## Test comparison

The test-bearing legacy path read test metadata and compared test identity,
start metadata, task IDs, open markers, and option IDs. Any mismatch stopped
before the atomic response. No task or option remapping was present or added.

## Gateway

`SendResponse` converts the legacy form to `hhwrite.VacancyResponseRequest`
and calls `hhwritegateway.Service.SubmitVacancyResponse`, which invokes one
`VacancyResponseWriter` capability backed by
`internal/adapters/hh/write.Client`. The provider adapter performs one POST
and classifies accepted, rejected, and uncertain transport outcomes.

## Counters

Batch counters remain in `ApplyVacancies`: processed/seen, deterministic skips,
AI evaluation, matches, review, previews, accepted applications, errors, and
vacancy/application limits. Preparation does not increment an applied counter.
`applicationsInRun` increments for a preview or an accepted response, matching
the existing automatic flow; rejected and uncertain attempts do not increment
the accepted count.

## Events

Preparation and match/preflight events remain root projections. The root still
emits `application_preview` for dry-run and `application` only for the legacy
accepted result, and `application_error` for submission errors. No new event
write is allowed to cause a transport retry; `writeEvent` remains best effort.

## Outcome handling

The gateway remains authoritative for transport classification. Accepted maps
to `SUBMITTED`, provider rejection maps to `REJECTED`, and ambiguous or
post-transport persistence outcomes map to `DELIVERY_UNCERTAIN`. 409, 5xx,
network uncertainty, and malformed provider success evidence do not cause a
second POST. 429 remains rejected and is not retried.

# Characterization

| Case | Existing behavior | Preserved |
|---|---|---:|
| prepared normal vacancy | prepare then one legacy application attempt | YES |
| already responded since preparation | fresh preflight blocks before writer | YES |
| archived/closed/unavailable | preflight blocks before writer | YES |
| fresh test unchanged | exact atomic vacancy-plus-test request can proceed | YES |
| test identity/task/open/option changed | stale test blocks before writer | YES |
| test removed or newly required | applicability mismatch blocks; no re-answer | YES |
| resume identity mismatch | prepared identity is rejected; no fallback resume | YES |
| dry-run | preview, zero writer dispatches, legacy preflight short circuit | YES |
| `HH_WRITE_ENABLED=false` | fresh checks may run, gateway/writer is not reached | YES |
| accepted transport | one dispatch and submitted projection | YES |
| rejected / 429 | one dispatch, rejected/error projection, no retry | YES |
| 409 / 5xx / network uncertainty | one dispatch, uncertainty preserved, no retry | YES |
| malformed success evidence | adapter classification is preserved; no retry | YES |
| cancellation before execution | cancellation propagates and writer is not called | YES |
| application limit reached | batch/preparation owner short-circuits before submission | YES |
| uncertain item then next vacancy | next vacancy may continue; same item is not replayed in the loop | YES |

# Submission orchestration

Package: `internal/usecase/applicationsubmission`

Service: `Service.Submit(context.Context, Input) (Result, error)`.

Input is a detached `PreparedApplication`, optional exact response referer,
caller-supplied current resume identity, and no approval or fresh-state grant.
The detached value preserves vacancy and resume identity, exact cover-letter
text, and test metadata/tasks/answers.

Result is typed and separates semantic status from execution evidence:

`PREVIEW`, `SUBMITTED`, `REJECTED`, `DELIVERY_UNCERTAIN`, `BLOCKED_STALE`,
`BLOCKED_UNAVAILABLE`, `WRITE_DISABLED`, and `NOT_SENT`.

Dependencies are narrow interfaces for fresh vacancy applicability, fresh test
metadata, and `ApplicationExecutor`. The package imports no root package,
dashboard/CLI, storage, concrete HH adapter, AI leaf, or completion provider.

# Fresh vacancy proof

For every non-dry-run `Submit` invocation, the service reads applicability
through its injected reader and passes the known-bit state to the existing
`hhwritepreflight.Service`. Archived, already-responded, or non-applicable
state blocks as unavailable; unknown critical state fails closed as stale or
review-like blocking. A prepared value is never treated as fresh authorization
and there is no TTL or cache grant in this path.

# Fresh test metadata

When prepared answers exist, the service reads the provider test immediately
after fresh vacancy applicability and compares the exact prepared/current
metadata through `hhwritepreflight.CompareTestMetadata`. The comparison covers
test identity, start metadata, task IDs, open markers, and option IDs.

Remapping: **NONE**.

If a test newly appears, disappears, cannot be read, or changes identity/tasks/
options, execution is blocked. No AI re-answer and no automatic re-preparation
is performed.

# R11 integration

Executor: root `rootApplicationExecutor`, the composition adapter for the
submission capability.

Gateway: `hhwritegateway.Service.SubmitVacancyResponse`.

Writer: `internal/ports/hhwrite.VacancyResponseWriter`, backed by
`internal/adapters/hh/write.Client`.

Transport: the existing R11 adapter/gateway classification and persistence
behavior.

Dispatch count: **1 max per `Submit` invocation**. The submission service has
no retry, sleep, backoff, resubmit, or reconciliation loop.

# Outcome semantics

Accepted transport is `SUBMITTED` and is eligible for the existing root local
application projection. Provider rejection is `REJECTED`. Ambiguous transport
is `DELIVERY_UNCERTAIN`; this includes 409, 5xx, network uncertainty, and
malformed success evidence where the adapter cannot prove delivery. 429 is
`REJECTED`. No provider response reconciliation is invented in this stage.

# Replay

Same run: `ApplyVacancies` has no retry edge from an uncertain item back to the
same prepared vacancy, so the item is dispatched at most once and the loop may
continue to the next vacancy.

Next run: no new durable vacancy-response attempt identity was added. A later
independent iteration relies on current HH state/legacy evaluation; ambiguity
without durable provider evidence remains a documented residual risk.

Automatic resend: **NO**.

# Counters and limits

Batch order, `maxVacanciesPerRun`, `maxApplicationsPerRun`, application
attempt/accepted behavior, and per-run summary projection remain root-owned.
The submission package does not count preparation as application success and
does not move limits into the preparation result. The existing gateway limit
behavior for other maintenance operations is unchanged.

# Events / projection

`ApplyVacancies` still owns `vacancy_match`, review/skip, preview, application,
error, and run-summary event projection. A provider acceptance is kept
separate from local persistence: an execution error after transport is not
converted into an optimistic retry. No local Application record lifecycle was
redesigned; HH-imported application metadata remains the existing source.

# Atomic test submission

One request: **YES**.

The executor maps the semantic request to one
`hhwrite.VacancyResponseRequest`; its optional test answers are included in the
same request.

Separate test mutation: **NO**.

# Capability containment

HH read: targeted applicability and, when needed, targeted test metadata only.

AI: **NONE**.

Candidate mutation: **NONE**; no clarification, proposal, or semantic reindex
starts during submission.

HH write: **R11 executor only**; no concrete writer or raw HTTP is imported by
`applicationsubmission`.

# Root compatibility

ApplyVacancies remains the batch discovery/order/counter owner and delegates
each prepared automatic item to `applicationsubmission.Service`.

`applyPreparedApplication` is now a thin compatibility delegate that maps the
typed submission result back to the historical response map and readable test
solutions. It retains no fresh-test comparison, write-policy algorithm,
gateway sequencing, or transport branching.

`SendResponse`, `ApplyVacancy`, and `ApplyVacancyWithTest` remain public/root
compatibility facades for existing manual callers. Their semantics were not
silently redefined; their direct paths remain classified as manual or R11
compatibility rather than automatic `ApplyVacancies` orchestration.

# Remaining ApplyVacancies ownership

Batch order: root.

Counters and stop conditions: root.

Scheduler/cadence: root.

Substantial submission workflow remaining: **NO**. The remaining root work is
batch coordination and local projection, not write choreography. A future
R12.5/R12.6 stage may inventory scheduler/composition cleanup, but this stage
does not begin it.

# R12.5 inventory

Recurring loops, auto-apply cadence, auto-chat cadence, resume touch, job-search
status, career monitor, dashboard inbox ticker, and completion-based semantics
remain outside this extraction and are unchanged. No scheduler boundary was
started.

# Tests

Added pure `internal/usecase/applicationsubmission/service_test.go` coverage
for fresh pass, already responded, archived/non-applicable vacancy, changed
test options, test newly required/removed, dry-run, write-disabled, accepted,
rejected, ambiguous, 429 classification, and pre-execution cancellation. Tests
use only fakes; they do not use HH, LLM, filesystem, Postgres, or concrete
writers.

Existing application-processing, vacancy-preflight, gateway, adapter, root
ApplyVacancies, manual application, dry-run, and no-write tests remain in place.

# Dependencies

`applicationsubmission` depends on `internal/vacancy` and the provider-neutral
`internal/usecase/hhwritepreflight` policy only. Its write edge is the
consumer-owned `ApplicationExecutor` interface. It does not depend on
`applicationprocessing`, `vacancyanalysis`, `coverletter`, `testanswer`,
`hhwritegateway`, concrete storage, concrete HH HTTP, or root composition.

# Retry audit

Safe read retry remains owned by the existing HH read adapter. Next-vacancy and
next-scheduled-run behavior remain root workflow behavior. HH write retry:
**NONE**.

# Behavior

Prepared applications are not authorization. Fresh vacancy proof is required
for non-dry-run submission, fresh test metadata is required when answers exist,
the caller's current resume identity must match the prepared identity,
and atomic vacancy-plus-test submission remains one provider mutation.
Write-disabled and dry-run policies
are supplied through options; no environment lookup exists in the use case.

# Verification

The focused and full Go tests pass after the extraction. Final sequential
verification is recorded below after running the repository checks.

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED unless the local daemon is available.

LIVE HH WRITES: **0**.

# R12 status

R12.4b: **EXTRACTED**.

# Ready

R12.5 — Scheduler Boundary: **READY**.

This stage is complete and stops at the requested R12.4b boundary.
