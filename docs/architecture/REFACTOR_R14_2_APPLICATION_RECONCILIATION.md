# Executive summary

R14.2: **COMPLETE**

Safety improvement: unresolved durable automatic application attempts can now
be checked with bounded, fresh, read-only HH evidence. Sufficient positive
evidence moves the local attempt to `TARGET_RESPONSE_CONFIRMED`, which remains
blocking. Missing, stale, unavailable, or conflicting evidence never becomes
`NOT_SENT` and never authorizes a new POST.

Provider exactly-once guarantee: **NOT ESTABLISHED**.
`AttemptID` remains a local correlation identifier, not a provider idempotency
key. Reconciliation proves a target response exists; it does not prove that
the exact local attempt caused it.

# Before

Unresolved attempt states were `SENDING` and `DELIVERY_UNCERTAIN`; `ACCEPTED`
was also blocking because it represented transport acceptance without a
provider confirmation. R14.1 persisted these states before and after the one
existing vacancy-response dispatch.

Current replay protection remains authoritative: `SENDING`, `ACCEPTED`, and
`DELIVERY_UNCERTAIN` block a new automatic reservation across restarts. R14.2
adds evidence and confirmation without releasing any blocking record.

# Provider evidence inventory

| Source | Evidence | Strength | Freshness |
|---|---|---|---|
| Fresh vacancy response preflight (`getVacancyPreflightContext` / `ReadApplicability`) | `AlreadyRespondedKnown=true` and `AlreadyResponded=true` | Strong positive target evidence | Fresh targeted HH read |
| Fresh `/applicant/negotiations` read (`HHReadSource.ReadApplications`) | matching `vacancyId`, stable `ExternalID`, and parser marker `metadata.delivery_confirmed=true` from `initialTopicType=RESPONSE_BY_APPLICANT` | Strong positive target evidence; provider negotiation ID is preserved | Fresh provider list read, bounded by pagination guard |
| Fresh preflight says applicable / no matching negotiation record | No positive proof | Insufficient only; never strong negative proof | Fresh but non-conclusive |
| Local `ApplicationStore` / HH sync import | Imported negotiation/application IDs and metadata | Cached/imported evidence only; not reconciliation authority | Depends on prior sync and may be stale |
| Resume identity | Not exposed by the current HH negotiation parser | Not available | No resume-specific inference is made |

The provider parser does not invent an application ID distinct from the HH
negotiation topic ID. The stable observed `ExternalID` is stored as
`ProviderNegotiationID`.

# Evidence semantics

Can prove target responded: yes, when fresh preflight explicitly reports an
existing response, or when a matching fresh negotiation has a response-by-
applicant marker and a stable provider ID.

Can prove exact `AttemptID` delivered: no. The provider does not expose the
local attempt ID and current reads do not expose resume identity.

Can prove not sent: no. A missing negotiation, applicable vacancy page,
temporarily unavailable read, or stale list is never treated as proof of
non-delivery.

Precedence is explicit: matching provider negotiation/application identity
with the response-by-applicant marker > explicit fresh already-responded
marker > applicability/absence. A fresh explicit applicable marker combined
with a strong positive application identity is classified as
`CONFLICTING_EVIDENCE`; it is not silently resolved by incidental branch
order. Evidence for another vacancy cannot confirm the target.

# Reconciliation model

Domain state added: `TARGET_RESPONSE_CONFIRMED`. It is blocking and not
replayable.

Allowed transitions:

```text
SENDING              -> TARGET_RESPONSE_CONFIRMED
DELIVERY_UNCERTAIN   -> TARGET_RESPONSE_CONFIRMED
ACCEPTED             -> TARGET_RESPONSE_CONFIRMED
```

`TARGET_RESPONSE_CONFIRMED` is idempotent for repeated positive reads. No
transition exists from a confirmed or unresolved attempt to `NOT_SENT`,
`REJECTED`, or a retryable state due to reconciliation.

The durable record retains `AttemptID`, vacancy/resume identity, and creation
time. Reconciliation adds only evidence metadata, observed time, provider IDs,
provider response time when supplied, and the blocking resolution state.

# Read-only capability

HH write capability: **NONE**.

`internal/usecase/applicationreconciliation` depends on the narrow
`EvidenceReader` and an attempt persistence capability only. It does not
import the HH writer, `VacancyResponseWriter`, generic requester, or concrete
JSON/Postgres adapters. Runtime composition adapts fresh HH reads to that
interface. The existing `applicationattempt.Executor` invokes reconciliation
only after a blocking reservation conflict and still returns the blocking
error; it never dispatches after reconciliation.

# Reconciliation sequence

```text
automatic submission encounters existing blocking attempt
        -> load that durable attempt
        -> fresh vacancy preflight read
        -> bounded fresh negotiation/application reads
        -> explicit evidence classification
        -> sufficient positive evidence?
             yes: persist TARGET_RESPONSE_CONFIRMED + evidence
             no: persist observation only, keep blocking state unchanged
        -> return typed result
        -> no HH mutation branch
```

## Attempt integration

`applicationattempt.Store` remains the R14.1 write-safety contract. JSON and
Postgres repositories additionally implement reconciliation methods used by
the narrow reconciliation port: `FindBlocking` and
`RecordReconciliation`. They perform an atomic read/validate/update under the
existing private file lock or a PostgreSQL row lock.

## Vacancy preflight evidence

The targeted vacancy page is read through the existing preflight parser.
Only an explicit positive already-responded marker is sufficient. `false`,
unknown, applicable, archived, and a parse/read failure do not prove that a
previous ambiguous POST did not happen.

## Application / negotiation evidence

The fresh negotiation list is read through the existing `HHReadSource` and
parser. A target `vacancyId`, a stable `ExternalID`, and
`delivery_confirmed=true` are required for the strong list evidence. The
reader has a bounded pagination loop and rejects repeated cursors; it does not
poll or retry a provider mutation.

## Identity limitations

Vacancy: vacancy-wide. This matches the R14.1 active conflict key, which is
`VacancyID`; a second resume cannot create a second blocking automatic
attempt.

Resume: not provider-confirmed. HH reads currently do not expose a resume ID
for the negotiation record, so no resume mismatch is inferred.

Attempt: exact local causality is not established. The result is
`TARGET_RESPONSE_CONFIRMED`, not “this exact attempt delivered”.

Provider IDs: stable parsed HH `ExternalID` values are preserved as
`ProviderNegotiationID`. A later positive observation with a different strong
provider ID is rejected as an invalid identity change; the old strong evidence
is not silently overwritten.

## Absence of evidence

No matching negotiation, a still applicable vacancy, unknown state, stale
history, and partial read failure leave the attempt blocking. They are
classified as insufficient or unavailable evidence and cannot cause a new
automatic application.

## JSON persistence

`application_attempts.json` remains version 1. New reconciliation fields are
optional/additive, so existing R14.1 records load unchanged. The repository
uses the existing private file lock and atomic file replacement. Restarted
repositories preserve the confirmed blocking state and provider evidence.

## PostgreSQL persistence

Migration `000007_application_attempt_reconciliation` appends nullable/empty
evidence columns, extends the state check, and includes
`TARGET_RESPONSE_CONFIRMED` in the active-vacancy unique index. Migration
`000006` is not edited. PostgreSQL reconciliation uses `SELECT ... FOR UPDATE`
and updates the state/evidence in one transaction.

## Concurrent reconciliation

Two processes may read HH concurrently. Local persistence is serialized. A
weaker later observation cannot downgrade a confirmed attempt; a changed
strong provider identity fails closed. No process can transition a confirmed
attempt back to unresolved or replayable.

## Persistence failure

If fresh evidence is positive but local reconciliation persistence fails, the
caller receives `ErrPersistenceUncertain`. The existing blocking state and
record remain; no HH write, release, deletion, or retry occurs.

# Automatic application integration

New attempt: unchanged. The path remains fresh preflight -> fresh test proof
-> R14.1 reservation -> exactly one existing HH vacancy-response write.
R14.2 does not reconcile a completely new target before its first dispatch.

Existing unresolved attempt: reservation conflict invokes only the explicit
read-only reconciliation hook. Positive evidence confirms and keeps the
target blocked. Insufficient, unavailable, conflicting, or persistence-error
results also keep it blocked. The current `applicationsInRun` and
`maxApplicationsPerRun` counters are not redesigned; a blocked/reconciled
vacancy is still not counted as a successful application.

Manual application semantics are unchanged. Controlled chat reconciliation
is unchanged and still uses exact provider message IDs and targeted history.
Legacy auto-chat, polling, manual release, retry UI, budgets, touch/status/
leave, and scheduler ownership are out of scope.

# Provider semantics

Exactly-once: **NO**.

Provider idempotency: **NOT ESTABLISHED**.

Local reconciliation is evidence-based only. It improves observability and
can positively resolve some blocked attempts; it does not establish provider
delivery causality or add a write retry.

# Tests

Focused coverage includes:

- positive preflight evidence with zero provider writes;
- negotiation/application evidence and stable provider ID persistence;
- absence of evidence preserving `SENDING` / `DELIVERY_UNCERTAIN`;
- read failure preserving a blocking state;
- conflicting evidence and a different vacancy not confirming the attempt;
- positive evidence with persistence failure remaining fail-closed;
- `ACCEPTED` upgrade and confirmed-state idempotence;
- JSON restart persistence and blocking after confirmation;
- R14.1 reservation/dispatch regression coverage.

# Risk after R14.2

Automatic duplicate replay remains locally blocked by R14.1 and the new
confirmed state. Some previously unresolved attempts can now become positively
confirmed. Weak, absent, conflicting, stale, or unavailable evidence remains
blocking. Automatic retry: **NONE**. Manual release: **NOT IMPLEMENTED**.

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

Postgres live integration: NOT RUN (no `DATABASE_URL` in the environment)

LIVE HH WRITES: **0**

# R14 status

R14.2: **COMPLETE**

Ready

R14.3 — Cross-run Application Gating / Attempt Resolution Policy

READY

Do not begin R14.3 in this change.
