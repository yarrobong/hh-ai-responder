# Executive summary

R14.3: **COMPLETE**

Safety improvement: automatic vacancy application now performs a durable,
vacancy-wide attempt gate before `applicationprocessing`. Existing blocking
attempts consume zero AI calls and zero HH mutation calls. Unresolved attempts
may receive one bounded, read-only R14.2 reconciliation, but reconciliation
never authorizes the current run to apply.

The per-run application safety limit now counts possible provider dispatches,
including accepted, rejected, ambiguous, 409/429/5xx, and network-ambiguous
writer invocations. Successful applications remain a separate count.

Provider exactly-once guarantee: **NOT ESTABLISHED**. `AttemptID` remains a
local correlation identifier and is never claimed to be provider causality.

# Before

Attempt gating position: the legacy `already_responded` vacancy-ID set was
checked before policy filtering, but the durable attempt was discovered only
inside the final R14.1 executor, after description reads, candidate context,
vacancy-analysis AI, cover-letter AI, and test-answer AI. A blocking attempt
therefore prevented the POST but did not prevent expensive preparation.

The effective old sequence was:

```text
search result -> already_responded -> deterministic filters
-> applicationprocessing / description / candidate context
-> vacancy analysis -> cover letter/test AI
-> fresh submission preflight -> R14.1 Reserve -> one HH mutation
```

Counter semantics: `applicationsInRun` was used for both preview and live
success behavior and was incremented for accepted results only in live mode.
Ambiguous or rejected transport attempts could therefore fail to consume
`maxApplicationsPerRun` even though a provider request may have been sent.

# State policy

| State | Automatic action | Reconcile | Replayable |
|---|---|---|---|
| `SENDING` | Skip; remain blocking | One read-only reconciliation when encountered | No |
| `ACCEPTED` | Skip; remain blocking | Not required; may be reconciled only by an explicit reconciliation path | No |
| `DELIVERY_UNCERTAIN` | Skip; remain blocking | One read-only reconciliation when encountered | No |
| `TARGET_RESPONSE_CONFIRMED` | Skip deterministically | No routine read | No |
| `REJECTED` | Allow a later normal application iteration | No | Yes, as a new logical attempt |
| `NOT_SENT` | Allow a later normal application iteration when execution proved no provider dispatch | No | Yes, as a new logical attempt |
| no blocking attempt | Run normal automatic pipeline | No | N/A |

`ACCEPTED` is blocking without successful reconciliation. Positive evidence
can strengthen it to `TARGET_RESPONSE_CONFIRMED`; failed or unavailable
reconciliation never releases it. There is no TTL or age-based release.

# Early gating sequence

```text
vacancy discovered
    -> durable vacancy-wide blocking-attempt gate
       -> store error: fail closed; no AI and no HH mutation
       -> blocking confirmed: skip; AI=0; HH mutation=0
       -> blocking unresolved: one read-only reconciliation, then skip
       -> no blocking attempt: continue
    -> legacy already_responded compatibility observation
    -> deterministic vacancy policy
    -> maxVacanciesPerRun / dispatch-budget check
    -> normal applicationprocessing preparation
    -> fresh vacancy/test proof in applicationsubmission
    -> R14.1 atomic Reserve immediately before possible dispatch
    -> at most one existing HH mutation executor invocation
```

# Authority

Attempt store: `applicationattemptpolicy.Gate` reads the narrow
`applicationattempt.BlockingReader` capability. The policy owns the
state-to-classification mapping. JSON and PostgreSQL adapters only query and
persist records; they do not decide replay safety. The final R14.1 atomic
reservation remains mandatory for TOCTOU and concurrent-run protection.

Already-responded state: the legacy vacancy-ID JSON set is preserved as a
provider/read observation compatibility cache. It is checked only after the
durable gate and cannot override a durable blocking attempt. Durable local
dispatch history remains authoritative for automatic replay protection.

Provider preflight: `applicationsubmission` still performs the fresh,
read-only vacancy and test proof immediately before a new write. The early
gate does not move or replace that proof.

# Reconciliation integration

`SENDING` and `DELIVERY_UNCERTAIN` are reconciled at most once when an early
gate finds them. The runtime calls the existing R14.2 service by attempt ID;
the provider reader is read-only. Positive evidence persists
`TARGET_RESPONSE_CONFIRMED`, but the current vacancy is still skipped. Weak,
absent, conflicting, unavailable, or persistence-error evidence leaves the
target blocking. `ACCEPTED` and `TARGET_RESPONSE_CONFIRMED` are skipped
without routine reconciliation, avoiding unnecessary read churn.

Reconciliation does not consume the provider mutation budget or successful
application count. A confirmed reconciliation is diagnostic evidence, not a
new application submitted in the current run.

# Replayable attempts

REJECTED: a later independent automatic run may enter the normal pipeline if
vacancy selection and all fresh deterministic/provider checks pass. There is
no immediate same-run retry.

NOT_SENT: the same policy applies only when the execution metadata proves the
underlying mutation executor was not invoked. Reservation alone is not a
dispatch and does not consume dispatch budget, but its durable `SENDING`
residue still blocks replay until the existing lifecycle resolves it.

New AttemptID: every later reservation calls `applicationattempt.New`, which
generates a new local ID. The old `REJECTED`/`NOT_SENT` record is never reused.
The vacancy-wide blocking query wins over older replayable history when both
exist. No manual force resend or automatic retry loop was added.

# Fail-closed storage behavior

An attempt-store initialization error aborts the automatic batch before
vacancy preparation. A per-vacancy blocking-read error skips that vacancy with
an explicit typed gate reason and continues unrelated vacancies; it never
becomes “no attempt”. A live compatibility responder without authoritative
attempt storage also fails closed. Dry-run-only compatibility fixtures retain
their preview behavior because no provider mutation is reachable.

# Counter model

Vacancies examined: `VacanciesProcessed` counts discovered vacancies examined
by the loop. Blocking attempts and store-error skips are examined vacancies;
they do not count as successful or dispatching applications. `maxVacanciesPerRun`
continues to limit `eligibleVacancies` after cheap durable/legacy/deterministic
skips, preserving the existing vacancy-limit behavior.

Provider dispatch attempts: `dispatch_attempts` increments only when the
R14.1-reserved underlying application executor reports `TransportTried=true`.
This includes accepted, rejected, ambiguous, 409, 429, 5xx, network ambiguity,
and post-dispatch persistence uncertainty. It excludes reservation failure,
fresh preflight/test failure, write-disabled/dry-run short circuit, and a
conclusive pre-executor `NOT_SENT` result.

Successful applications: existing `Applied` retains its successful/submitted
meaning and increments only for `StatusSubmitted` without a submission error.
It is intentionally separate from `DispatchAttempts`.

Ambiguous: consumes one dispatch-budget slot and does not increment
`Applied`. The durable state remains `DELIVERY_UNCERTAIN`.

Rejected: consumes one dispatch-budget slot when the mutation executor was
invoked and does not increment `Applied`. A later run may create a new attempt
only under the replayable policy.

# Limit semantics

`maxVacanciesPerRun`: unchanged examination/eligibility limit. A blocked or
reconciled vacancy is processed and recorded as a blocked observation, but it
does not become a prepared or applied vacancy and does not consume the
provider dispatch budget.

`maxApplicationsPerRun`: in live mode this is a per-run cap on possible
provider dispatches, not accepted responses. The next vacancy is stopped
before expensive preparation once `dispatch_attempts` reaches the limit. In
dry-run mode the existing preview-oriented behavior is preserved using the
planned-preview count; no durable attempt or provider mutation is created.

# Concurrent runs

Two concurrent paths may both pass the early read gate and prepare the same
vacancy. The final R14.1 atomic `Reserve` remains immediately before dispatch
and allows at most one blocking reservation/provider executor path. The early
gate is an optimization and policy check, never a replacement for that race
authority.

# JSON / PostgreSQL parity

Both adapters preserve the vacancy-wide active unique constraint and expose
the same blocking read. Both use the same domain states and R14.2 evidence
transitions. No schema migration was required for R14.3. A PostgreSQL
misconfiguration is no longer reported as `ErrAttemptNotFound`; it is a store
failure and therefore fail-closed.

# Automatic application sequence

New targets keep the R12/R14.1 sequence: deterministic selection, normal AI
preparation, fresh vacancy/test proof, durable `SENDING` reservation, one
existing HH vacancy-response mutation, and durable outcome recording. A
blocking cross-run record exits before all AI preparation. The raw HH mutation
owner remains `internal/adapters/hh/write`.

# Manual application compatibility

Manual application paths do not call the automatic attempt policy and remain
unchanged. Controlled chat, legacy auto-chat, touch, status, leave, and the
12-hour auto-apply scheduler cadence are unchanged.

# Retry safety

HH WRITE RETRY: **NONE**

No automatic transport retry, same-run retry, reconciliation scheduler, TTL
release, force resend, or reset UI was added. Existing AI validation retries
are unrelated to HH mutation retry and remain within their existing usecases.

# Tests

Focused coverage includes:

- authoritative state classification for all six attempt states;
- missing attempt versus attempt-store failure distinction;
- pre-existing `ACCEPTED` blocking `ApplyVacancies` before description/AI and
  with zero HH mutation calls;
- reconciliation package coverage for positive, insufficient, conflicting,
  unavailable, persistence-error, and accepted evidence paths;
- new AttemptID behavior and replayability of `REJECTED` in the R14.1 executor;
- atomic concurrent reservation behavior for JSON storage;
- dispatch budget behavior where two ambiguous dispatches exhaust limit two,
  while a pre-dispatch `NOT_SENT` does not consume it;
- applicationsubmission one-executor-call, fresh-proof, resume, and test
  metadata regressions;
- dry-run, legacy already-responded, manual application, and write-gateway
  regressions.

# Risk after R14.3

Cross-run automatic replay: blocking states are gated before AI and remain
blocked across runs; `REJECTED`/`NOT_SENT` intentionally permit a fresh future
attempt under normal checks.

Unresolved attempts: remain fail-closed forever until positive explicit
reconciliation evidence or a separately authorized future stage changes that
policy. Absence of evidence is not treated as not sent.

Budget overrun via ambiguous writes: closed for one automatic run because
possible transport dispatches, not only accepted responses, consume the
configured limit.

# Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

canonical build: PASS

diff: PASS

node: PASS

Docker: NOT APPLICABLE

Postgres live integration: NOT RUN (no live database configured)

LIVE HH WRITES: **0**

# R14 status

R14.3: **COMPLETE**

# Ready

R14.4 — Legacy Direct-Write Reliability / Auto-Chat Replay Protection
READY

Do NOT begin it as part of R14.3.
