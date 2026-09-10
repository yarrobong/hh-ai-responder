# Executive summary

V2.Fix-2: **COMPLETE / CLOSED**

The PostgreSQL automatic application-attempt authority now treats an empty
`automatic_application_attempts` table as a valid authoritative empty state.
Only actual repository/database/scan failures remain fail-closed errors. R14
blocking-state, replay, reservation, reconciliation, and dry-run protections
were preserved.

LIVE HH WRITES: 0

# Root cause

`internal/adapters/storage/postgres.ApplicationAttemptRepository.FindBlocking`
passed its `QueryRow` result directly to `scanAttempt`. When the query found
no blocking row, pgx returned typed `pgx.ErrNoRows`. The PostgreSQL adapter let
that driver sentinel escape, while
`internal/usecase/applicationattemptpolicy.Gate` recognizes expected absence
only as the canonical `applicationattempt.ErrAttemptNotFound`.

The resulting path was:

```text
empty PostgreSQL table
→ pgx.ErrNoRows
→ attempt gate sees store error
→ auto-apply stops before vacancy analysis
```

The direct SQL reproduction against `hh_ai_responder_s3` confirmed the table
was empty (`0` rows); source tracing identified the unnormalized return at the
PostgreSQL `FindBlocking` boundary. No HH request was needed for that
reproduction.

# Previous behavior

Empty PostgreSQL table:

```text
automatic_application_attempts = 0 rows
```

Repository result:

```text
FindBlocking(vacancyID) → zero Attempt, pgx.ErrNoRows
```

Gate result:

```text
ATTEMPT_STORE_UNAVAILABLE / fail closed
```

# Correct contract

Empty or unrelated vacancy for `FindBlocking`:

```text
zero Attempt, applicationattempt.ErrAttemptNotFound
```

The gate maps that expected absence to `NO_ATTEMPT` and allows the later
application gates to run.

Blocking states remain:

```text
SENDING
ACCEPTED
DELIVERY_UNCERTAIN
TARGET_RESPONSE_CONFIRMED
```

`GetByID` remains an explicit lookup: an unknown ID returns the canonical
`applicationattempt.ErrAttemptNotFound`. It was not globally changed to
absence-as-success.

Connection, transaction, SQL, scan, context, invalid-state, duplicate
invariant, and initialization failures remain errors and therefore remain
fail-closed.

# Implementation

Added the narrow `findBlockingAttempt` PostgreSQL boundary helper. It converts
only typed `pgx.ErrNoRows` to `applicationattempt.ErrAttemptNotFound` and wraps
all other errors as repository failures. Both `FindBlocking` and the final
reservation conflict lookup use this helper. The query remains parameterized,
bounded with `LIMIT 1`, and deterministically ordered by `created_at,
attempt_id`.

No schema or migration change was made.

# PostgreSQL / JSON parity

The JSON repository already returned `ErrAttemptNotFound` for empty and
unrelated blocking lookups. Focused tests now cover empty, unrelated, matching
blocking, and explicit unknown-ID behavior for JSON; PostgreSQL tests cover
typed no-row normalization, real empty-table lookup, and preservation of real
database errors.

# Blocking-state regression

Existing policy tests continue to verify that all four R14 blocking states
stop the gate, while `REJECTED` and `NOT_SENT` remain replayable history. JSON
reservation/replay tests, concurrent reservation tests, persistence-failure
tests, and reconciliation tests remained passing. The final atomic PostgreSQL
reservation step was retained.

# Explicit lookup behavior

`Get` / `GetByID` still maps a typed PostgreSQL no-row result to
`applicationattempt.ErrAttemptNotFound`. The real empty-store integration
check exercised an unknown explicit ID and preserved this behavior.

# Real PostgreSQL empty-store verification

Database: `hh_ai_responder_s3`

Before rows:

```text
automatic_application_attempts: 0
```

Lookup:

```text
real PostgreSQL vacancy ID → FindBlocking → ErrAttemptNotFound
```

The opt-in integration test passed using the real local PostgreSQL instance.
The lookup returned no blocking attempt and no error at the gate contract;
`GetByID` for an unknown ID remained canonical not-found.

After rows:

```text
automatic_application_attempts: 0
```

No placeholder attempt or other attempt row was created.

# Bounded auto-apply verification

Safety configuration:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
auto-chat=false
auto-touch=false
auto-job-status=false
```

The first bounded pass used a one-vacancy limit and recorded
`attempt-authority errors=0`; the first candidates were rejected by existing
deterministic policy before AI. A second bounded pass used five vacancies and
also recorded `errors=0`, with:

```text
AI evaluated: 3
matched: 0
review required: 0
would apply: 0
applied: 0
```

Real Mistral vacancy analysis was reached naturally. Existing candidate and
vacancy policy rejected the evaluated candidates later, so no filter was
weakened and no application was prepared for dispatch. No HH mutation was
attempted.

# Reservation / concurrency

The final atomic reservation remains after preparation and before dispatch.
Existing concurrent-reservation coverage passed, including the at-most-one
successful reservation invariant for two concurrent logical attempts.

# Reconciliation / reliability regression

Existing application reconciliation tests passed. Positive provider evidence
can confirm an attempt; absent or weak provider evidence does not release an
uncertain attempt. Persistence uncertainty remains blocking/fail-closed.
Reliability inspection continues to return an empty collection for an empty
store and explicit unknown attempt details remain not-found.

# V2.Fix-1 regression

PASS. HH conversation decoder tests remained passing.

# Database integrity

Post-run read-only counts in `hh_ai_responder_s3`:

| Entity | Count |
|---|---:|
| Candidate | 1 |
| Vacancies | 279 |
| Applications | 98 |
| Application events | 308 |
| Conversations | 259 |
| Messages | 791 |
| Semantic documents | 3 |
| Application attempts | 0 |
| Legacy auto-chat attempts | 0 |

Unexpected changes: **NONE**.

# Verification

| Check | Result |
|---|---|
| focused PostgreSQL / JSON / gate tests | PASS |
| real PostgreSQL empty-table integration | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `gofmt -l .` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |
| Docker | NOT APPLICABLE |
| LIVE HH WRITES | 0 |

# Decision

V2.Fix-2: **CLOSED**

V2.Resume: **READY**

V2.Resume was not run automatically.
