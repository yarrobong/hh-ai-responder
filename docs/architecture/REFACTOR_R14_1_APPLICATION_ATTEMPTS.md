# Executive summary

R14.1: **COMPLETE**

Safety improvement: automatic vacancy applications now have a durable local
logical attempt reserved before the existing HH executor can dispatch. A
persisted `SENDING`, `ACCEPTED`, or `DELIVERY_UNCERTAIN` attempt blocks a new
automatic dispatch for the same vacancy. Reservation failures call the HH
executor zero times. Outcome persistence failures preserve the conservative
`SENDING` record and return explicit persistence uncertainty.

This is local replay protection only. `AttemptID` is a local correlation ID;
it is not a provider idempotency key and does not provide exactly-once
delivery. Vacancy reconciliation remains R14.2.

# Before

Automatic application:

```text
ApplyVacancies
  -> applicationprocessing
  -> applicationsubmission fresh vacancy/test proof
  -> dry-run/write-enabled gates
  -> existing ApplicationExecutor
  -> HH vacancy-response writer
  -> in-memory result/event projection
```

Cross-run gap: an ambiguous POST, process crash, or local event failure left
no durable logical application attempt. A later scheduled run could pass a
fresh provider read and dispatch a new POST for the same logical target.

# Attempt model

AttemptID: UUIDv4-shaped local correlation ID generated for every newly
reserved logical dispatch attempt. It is never sent as a provider nonce or
idempotency key.

Target: `VacancyID` + `ResumeID` are stored on every attempt. The conflict key
is vacancy-wide (`VacancyID`) because current HH response applicability and
`AlreadyResponded` state are vacancy-scoped and the provider operation is one
vacancy response. A different resume therefore cannot bypass an active local
attempt for the same vacancy.

States:

- `SENDING` — durably persisted before possible HH dispatch.
- `ACCEPTED` — existing transport semantics accepted the response; this is
  not delivery confirmation.
- `REJECTED` — existing transport semantics positively rejected the request.
- `NOT_SENT` — reservation existed, but execution definitively did not send.
- `DELIVERY_UNCERTAIN` — provider mutation may have happened or success
  evidence is insufficient.

Transitions:

```text
SENDING -> ACCEPTED
SENDING -> REJECTED
SENDING -> NOT_SENT
SENDING -> DELIVERY_UNCERTAIN
```

Terminal identity fields (`AttemptID`, `VacancyID`, `ResumeID`, `CreatedAt`)
are immutable. R14.1 does not add reconciliation transitions.

# Blocking semantics

SENDING: blocks. A crash before or during transport is unresolved; absence of
provider evidence is not treated as `NOT_SENT`.

ACCEPTED: blocks. The current system already treats this transport result as
submitted; no automatic second response is allowed.

DELIVERY_UNCERTAIN: blocks. 409, 5xx, network uncertainty, malformed or
insufficient success evidence, and post-dispatch persistence uncertainty do
not authorize another POST.

REJECTED: replayable on a later independent automatic run. There is no
immediate retry inside the same attempt.

NOT_SENT: replayable on a later independent automatic run when execution
proved that no provider dispatch occurred.

# Reservation sequence

```text
PreparedApplication
    -> fresh vacancy proof
    -> fresh test metadata comparison
    -> existing dry-run/write-enabled gates
    -> durable SENDING reservation
    -> exactly one existing HH submission execution
    -> durable terminal outcome
    -> existing result/event projection
```

The dry-run short circuit remains before the final submission executor, so a
preview creates no live attempt. `HH_WRITE_ENABLED=false` returns before the
executor and also creates no attempt. Fresh vacancy/test checks and resume
identity validation remain in `applicationsubmission`; the attempt wrapper
does not authorize or bypass them.

Integration point

`internal/usecase/applicationattempt.Executor` implements the existing
`applicationsubmission.ApplicationExecutor` capability. The automatic runtime
constructs it as:

```text
applicationsubmission
    -> automatic application-attempt executor
    -> existing root application executor
    -> newLegacyWriteService
    -> hhwritegateway
    -> internal/adapters/hh/write
    -> VacancyResponseWriter
```

Manual `ApplyVacancy`, `ApplyVacancyWithTest`, and `SendResponse` paths do not
use this wrapper.

JSON persistence

Path: `application_attempts.json` beside the configured candidate profile;
when no profile path is configured, beside the configured
already-responded-state file; otherwise the stable filename is relative to
the current data root. It is separate from `job_applications.json`.

Atomic reservation: the JSON adapter holds the existing private process/file
lock across reload, blocking-state check, append, and atomic write/rename.
Every reservation and outcome update reloads from disk, so a reconstructed
store observes prior state. Corrupt/read-failed data and save failures fail
closed; the HH writer is not reached.

Restart: a persisted `SENDING` attempt survives reconstruction and blocks a
new reservation for the same vacancy. A corrupt attempt file leaves the live
automatic capability unavailable rather than treating it as an empty store.
Dry-run can still retain its existing preview behavior because it exits before
reservation.

PostgreSQL persistence

Table: `automatic_application_attempts`.

Constraint/transaction: migration `000006_application_attempts` creates a
partial unique index on `vacancy_id` for `SENDING`, `ACCEPTED`, and
`DELIVERY_UNCERTAIN`. Reservation uses `INSERT ... ON CONFLICT DO NOTHING
RETURNING`; the database constraint is the concurrency authority. Outcome
updates lock the attempt row in a transaction and validate the domain
transition before updating only mutable outcome fields.

Migration: embedded under `internal/runtime/migrations`, applied by the
existing `ApplyPostgresMigrations` mechanism when the PostgreSQL automatic
attempt store is composed. No application read-model columns are changed.

JSON/Postgres parity

Both adapters use the same target conflict key, blocking/replayable states,
`SENDING`-only transition model, immutable identity, and fail-closed
reservation semantics. Events and application repositories are not attempt
authority in either backend.

Dry-run / write-disabled

`HH_DRY_RUN=true`: no provider write and no durable attempt reservation.
`HH_WRITE_ENABLED=false`: no provider write and no durable attempt
reservation. Existing preview/disabled statuses remain in
`applicationsubmission`.

Outcome persistence failure

The sequence is `SENDING` persisted, one executor call, then
`RecordOutcome`. If the final save fails, the wrapper does not fabricate
`ACCEPTED` or `NOT_SENT`; it returns `ErrOutcomePersistenceUncertain` and
maps the runtime result to `DELIVERY_UNCERTAIN`. The durable prior state stays
`SENDING`, so the next automatic invocation is blocked.

Crash behavior

Before dispatch: if reservation cannot be persisted, writer calls are zero.

During dispatch: a `SENDING` record survives a crash and blocks replay even if
the crash happened before the network call. This possible false-positive
block is accepted as the R14.1 safety tradeoff.

After dispatch: an accepted, rejected, not-sent, or ambiguous result is
persisted exactly once after the single executor call. If that persistence is
uncertain, `SENDING` remains the conservative durable state. No retry or
provider reconciliation is added.

Manual application compatibility

The new wrapper is wired only by `applicationSubmissionService`, which is the
automatic `ApplyVacancies` compatibility path. Manual application commands
continue to use the existing gateway/adapter path and retain their previous
semantics. Controlled chat action IDs, nonces, stores, and reconciliation are
unchanged.

Provider semantics

Provider idempotency key: **NONE**

Exactly-once guarantee: **NO**

The local attempt record prevents unsafe local automatic replay. It does not
prove provider-level exactly-once delivery. R14.2 is required to resolve
uncertain provider evidence.

R11 safety

Raw mutation owner: `internal/adapters/hh/write`

Write retry: **NONE**

The implementation adds no transport retry, response retry, or reconciliation
query. Existing fresh preflight, test metadata comparison, task/option
identity, and one-executor-call behavior remain authoritative.

Tests

- Pure attempt-domain transition, blocking, replayability, and identity tests.
- JSON restart/reload, corruption fail-closed behavior, atomic concurrent
  reservation, accepted/rejected/uncertain outcomes, and replay policy.
- Automatic wrapper reservation-before-dispatch, preloaded `SENDING` and
  `ACCEPTED` residue blocking, ambiguity blocking, rejected replay, reservation
  failure, and final-save failure without retry.
- Existing `applicationprocessing`, `applicationsubmission`,
  `hhwritegateway`, HH adapter, manual application, controlled chat, scheduler,
  and runtime regression suites.
- PostgreSQL migration/SQL contract tests are present. Opt-in live PostgreSQL
  integration tests were not run because no `DATABASE_URL` was provided;
  Docker is not applicable to this repository stage.

# Risk after R14.1

Automatic application duplicate replay: **CRITICAL -> substantially
reduced locally**. Cross-run replay after an accepted result, uncertain result,
or crash residue is blocked by durable state in both storage modes.

Remaining unresolved-delivery risk: still present. An active attempt can be
stuck in `SENDING` or `DELIVERY_UNCERTAIN` until a future reconciliation or
operator-resolution stage. Absence of provider evidence does not clear it.

# Verification

gofmt: PASS

go test: PASS (`go test -count=1 ./...`)

race: PASS (`go test -race ./...`)

vet: PASS (`go vet ./...`)

build: PASS (`go build ./...`)

canonical build: PASS (`go build ./cmd/hh-ai-responder`)

diff: PASS (`git diff --check`)

node: PASS (`node --check web/app.js`)

Docker: NOT APPLICABLE

LIVE HH WRITES: **0**

# R14 status

R14.1: **COMPLETE**

Ready

R14.2 — Vacancy Response Reconciliation / Evidence Model

**READY**

Do NOT begin it.
