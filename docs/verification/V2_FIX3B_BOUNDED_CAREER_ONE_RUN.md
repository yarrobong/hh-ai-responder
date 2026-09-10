# Executive summary

V2.Fix-3b: **COMPLETE / CLOSED**

An explicit `monitor --run-once --max-conversations-per-run N` contract now
bounds HH conversation detail expansion and the downstream Career evaluation
scope. The default and scheduled monitor paths remain unbounded. The final
real run completed read-only in 687 ms with three selected conversations, one
HH list GET, zero detail GETs because all three details were locally reusable,
and zero errors.

# Previous behavior

The Career one-run path called `CareerMonitor.RunOnce`, then
`careeriteration.Service.Run`, then `HHReadSyncService.RefreshInbox`. The HH
reader fetched one conversation-list page and expanded every chat on that page
before the Career pipeline continued. The production reader used the
configured request interval (`1200ms` by default) and read concurrency
(`4`, validated in the `1..8` range); detail work was concurrent within a
page, while page/cursor progression was sequential.

The existing path had no conversation/detail bound and the normal Career
monitor intentionally covered the full provider scope. In the observed
baseline this expanded the broad conversation set (approximately 259 local
conversations) and could remain active for the V2 observation window. Context
cancellation was already propagated through the read path; the evidence did
not indicate a deadlock.

# Root cause classification

**MISSING OPERATOR BOUND**

Evidence:

- `CareerMonitor.Run` invokes the unbounded iteration path.
- `CareerMonitor.RunOnce` previously had no operator scope argument.
- `careeriteration.Service` called `RefreshInbox` without a limit.
- `HHAIResponderReadClient` created detail jobs for every item returned by the
  list page; no bound was applied before those jobs were started.
- Existing cancellation checks and request contexts caused blocked reads to
  exit correctly in regression tests.

# Expensive work unit

The expensive unit is one provider conversation detail expansion
(`GET /chatik/api/chat_data`) per selected conversation, after one list GET
(`GET /chatik/api/chats`) per required page. The bound is applied to the
provider-order prefix before detail jobs are created. The downstream Career
snapshot is also scoped to the selected provider conversation IDs, so the
bounded run does not present the full local conversation set as evaluated
input.

# Bounded contract

Command/config:

```text
monitor --run-once --max-conversations-per-run N
HH_MAX_CONVERSATIONS_PER_RUN=N
```

Default:

`N=0` means the current full-scope behavior. Existing invocations without the
new option are unchanged.

Bounded mode:

For `N>0`, conversations are selected deterministically in the current HH
provider order. At most `N` conversation records are expanded/imported and
passed to bounded Career state resolution, audit, notification, and related
timeline evaluation. A completed bounded scope is reported as
`completed_bounded_scope`, not as a full sync.

Negative and malformed values are rejected locally. CLI values retain
precedence over environment values.

# Scheduled behavior

Changed: **NO**

The recurring `CareerMonitor.Run` explicitly invokes the unbounded path. The
new cap is activated only by the explicit operator one-run call. The existing
normal scheduler therefore continues to process the full intended scope.

# Pagination

Bounded reads use the current provider order and preserve opaque cursors. The
read loop stops before requesting another page once the selected count reaches
the configured bound. Fix-3a behavior is unchanged: the first chat-list page
omits `from`, and later pages use the provider's opaque `nextFrom` value.

Regression coverage includes a bound satisfied on page 1 and a bound crossing
an opaque-cursor page boundary.

# Context cancellation

The existing context propagation was retained. The bounded path checks
`ctx.Err()` before each page, before each detail job's provider request, and
uses request contexts for transport cancellation. A cancellation regression
test verifies prompt exit and no subsequent page/detail work.

# Implementation

- Added `MaxConversationsPerRun` with CLI/env parsing and non-negative
  validation; zero remains unbounded.
- Added an optional bounded conversation read port capability so the limit is
  applied in both the HH adapter and the production responder read wrapper,
  before detail expansion.
- Added bounded Career monitor/use-case plumbing and provider-order selected
  conversation IDs.
- Scoped bounded Career downstream evaluation to the selected conversations
  and their related applications, vacancies, clarifications, and timelines.
- Added explicit one-run result accounting: requested, processed, skipped,
  errors, provider reads, detail reads, and notification counters.
- No schema migration was added. Fix-3c was not touched.

# Tests

Bound respected: **PASS** — 20 available, bound 3, detail calls 3 or fewer.

Unbounded compatibility: **PASS** — the fixture's complete page sequence is
still read when no bound is configured.

Pagination: **PASS** — page 2 is not requested when page 1 satisfies the
bound; opaque cursor crossing is covered.

Cancellation: **PASS** — the delayed provider exits on cancellation without
subsequent requests.

Provider error: **PASS** — provider errors remain errors/results and are not
converted into successful bounded completion.

Production reader transport: **PASS** — a 40-chat fixture with bound 3 made
one list GET and three detail GETs.

# Real bounded Career run

Requested bound: **3**

Command:

```text
monitor --run-once --max-conversations-per-run 3
```

Runtime safety overrides were explicitly set to `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`. PostgreSQL was the configured source of truth for
Candidate and Career repositories.

List GETs: **1**

Detail GETs: **0** — all three selected conversations were eligible for the
existing local detail reuse path; the bound test separately proves three
detail GETs is the maximum when reuse is unavailable.

Processed: **3**

Errors: **0**

AI: **0 calls**, naturally unnecessary for the selected records.

Result: **completed_bounded_scope**; notifications created 0, deduplicated 3,
resolved 85 existing local notifications. No HH write endpoint was used.

Elapsed: **687 ms**

HH mutations: **0**

# PostgreSQL integrity

The final read-only verification reported:

| Entity | Count | Result |
| --- | ---: | --- |
| Candidate | 1 | PASS |
| Vacancies | 279 | unchanged by this run |
| Applications | 98 | PASS |
| Conversations | 259 | PASS |
| Application attempts | 0 | PASS |
| Auto-chat attempts | 0 | PASS |
| Orphan applications | 0 | PASS |
| Orphan conversations | 0 | PASS |
| Orphan messages | 0 | PASS |
| Duplicate vacancy provider identities | 0 | PASS |
| Duplicate conversation provider identities | 0 | PASS |

The final run classified all three selected conversation records as unchanged
and reused their existing details. Candidate truth was not mutated; the
database counts and relations remained unchanged.

# Semantic

**READY** — 3 documents, 1024 dimensions, unchanged embedding space, no
reindex.

# Fix regressions

Fix-1: **PASS** — archived decoder remains compatible with bool, null, missing,
and `{"@hidden": bool}`.

Fix-2: **PASS** — application-attempt semantics and attempt policy were not
changed.

Fix-3a: **PASS** — first-page `from` omission and opaque later cursor behavior
remain covered.

# Verification

tests: **PASS** — `go test -count=1 ./...`

race: **PASS** — `go test -race ./...`

vet: **PASS** — `go vet ./...`

build: **PASS** — `go build ./...` and `go build ./cmd/hh-ai-responder`

gofmt: **PASS** — changed Go files formatted; `gofmt -l .` clean

diff: **PASS** — `git diff --check`

node: **PASS** — `node --check web/app.js` and
`node --check internal/runtime/web/app.js`

canonical: **PASS** — PostgreSQL Candidate/Career path used; no JSON Career
fallback in the real run

Docker: **NOT APPLICABLE**

LIVE HH WRITES: **0**

# Decision

V2.Fix-3b: **CLOSED**

V2.Fix-3c — Application Preparation Acceptance Harness: **READY**

Fix-3c was not started automatically.
