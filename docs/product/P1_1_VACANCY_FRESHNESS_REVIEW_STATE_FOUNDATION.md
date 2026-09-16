# P1.1 — Vacancy Freshness & Review-State Foundation

## 1. Status

Implementation complete. The local PostgreSQL schema and runtime contract are
ready; the optional live PostgreSQL acceptance test is skipped when
`POSTGRES_TEST_DATABASE_URL` is not configured.

## 2. Context

The P1 baseline found 279 canonical vacancy rows, 98 application-linked
vacancies, no provider publication/update timestamps, and no vacancy-specific
review state. Existing `created_at` values are storage timestamps for some
rows and are not historical provider observation timestamps.

## 3. Product Semantics

Provider observation state and user review state are separate dimensions.
`unseen` is an effective state only: absence of a review-state row means
“never explicitly reviewed”. `seen`, `interesting`, `dismissed`, `prepared`,
and `applied` are valid explicit states. `skip` remains an application/match
compatibility value and is not a vacancy review state.

`seen` requires an explicit local action. A vacancy returned by a list or
opened in a future UI is not automatically marked seen. `interesting` and
`dismissed` are explicit user choices. `prepared` means entering the local
application-preparation flow, and `applied` is available for the local
application lifecycle.

## 4. Data Model

`vacancy_freshness` stores provider observations:

- nullable first/last observed timestamps and nanosecond companions;
- current and previous source fingerprints;
- current material fingerprint;
- fingerprint algorithm version;
- latest material-change timestamp.

`vacancy_review_states` is a sparse current-state table. Its absence is the
canonical `unseen`/never-reviewed representation. It stores the decision
fingerprints, decision time, reason, and update time.

`vacancy_review_events` is append-only and stores the action, time, source,
reason, and both fingerprints observed when the decision was made.

No review data is stored in the legacy JSON backend.

## 5. Freshness Semantics

`first_seen_at` is the first timestamp at which the P1-aware system observed
the vacancy through a successful provider discovery/import path. It is
immutable after that observation. `last_seen_at` is updated by every
successful provider observation and is not changed by detail reads, review
actions, ranking, or local application actions.

Provider `published_at` and `hh_updated_at` remain separate fields. Missing
provider timestamps remain NULL; no local timestamp is substituted.

## 6. Legacy Backfill Policy

Migration `000011` does not create freshness rows for existing vacancies.
Therefore the current 279 legacy rows retain unknown first/last observation
history until their first successful P1-aware observation. No `created_at`, ID
ordering, notification state, conversation state, or application timestamp is
reinterpreted as first-seen history.

The first P1-aware observation creates the freshness row with both timestamps
equal to that observation. Its fingerprints describe the current source
snapshot only and do not prove an earlier provider observation.

## 7. Fingerprint Design

`vacancy.Fingerprints` is the single Go implementation used by runtime import.
It hashes canonical JSON with SHA-256 and version `1`.

When the stored algorithm version differs, the next observation refreshes the
hash baseline without treating the hash difference as a material provider
change.

Normalization trims text and normalizes line endings, sorts requirements,
skills, and user-label arrays, sorts maps through JSON encoding, normalizes
provider times to UTC RFC3339Nano, and makes nil/empty collections equivalent.
URLs retain their path/query semantics while normalizing surrounding space and
scheme/host case. Local IDs, local timestamps, match results,
recommendations, review state, and reconciliation evidence are excluded.

## 8. Material Change Policy

The source fingerprint includes all currently mapped provider-owned vacancy
state, including response counters and provider response metadata. The
material fingerprint includes title/name, description, requirements, skills,
salary/currency, location, work format, employment, schedule, experience,
area/company, and archived state. A source-only change therefore need not
reopen a review decision.

If a current material fingerprint differs from the fingerprint saved with the
last explicit decision, `changed_since_review=true`. If either side is
unknown, the result is unknown, not false. A dismissed decision is never
overwritten or silently converted to unseen.

## 9. Review Lifecycle

The domain service exposes typed `MarkSeen`, `MarkInteresting`, `Dismiss`,
`MarkPrepared`, and `MarkApplied` operations, plus a generic typed `Record`.
Invalid state strings are rejected. Reconsideration from dismissed to
interesting is allowed. Applied is terminal except for an idempotent applied
action.

Repeated action + same vacancy + same current fingerprint is idempotent and
does not append a duplicate event. A changed fingerprint may produce a new
event while preserving the previous event history.

## 10. Application Integration

The effective projection performs one bulk application read and treats any
durable application row with the vacancy ID as `ApplicationLinked=true` and
effective state `applied`. This is local evidence that the system handled the
vacancy; it is not fresh HH `already_responded` evidence and does not change
the existing preflight contract.

The projection preserves any explicit stored review row and does not invent a
historical review event or applied timestamp from an application row.

## 11. Repository / Service Design

`ports.VacancyObserver` is an optional sync capability implemented by the
PostgreSQL vacancy repository. `ports.VacancyFreshnessReader` and
`vacancyreview.Store` expose bounded reads and typed review actions. The
root runtime constructor is `NewPostgresVacancyReviewRepository`.

The effective projection uses bulk freshness/state reads and one application
list read, so a later queue does not require one review-state query per
vacancy.

## 12. Import Integration

`hhreadsync.ImportBatch` chooses one UTC observation timestamp per batch (or
uses explicit `ImportOptions.ObservedAt` in tests). When the selected vacancy
repository implements `VacancyObserver`, vacancy persistence and freshness
update happen in one PostgreSQL transaction. Existing local match,
recommendation, and reconciliation values remain preserved.

Repeated observations retain first-seen, advance last-seen, and update
fingerprints only when source state changes. Exact duplicate IDs in one batch
resolve to one freshness row; profile aggregation continues to own its
run-level ID de-duplication.

## 13. API Surface

No HTTP review UI or automatic mark-seen endpoint was added. The reusable
domain API is deliberately available before UI work through
`internal/vacancyreview.Service`. P1.2 can consume `Effective`/
`ListEffective` and the repository bulk methods without knowing PostgreSQL
SQL or the JSON compatibility layer.

## 14. Concurrency / Transactions

PostgreSQL review actions lock the canonical vacancy and current review row,
read the current freshness row, append the event, update current state, and
commit as one transaction. Concurrent actions cannot leave event/current-state
divergence. Idempotency is evaluated while holding those locks.

Vacancy observation likewise uses a transaction for the provider vacancy and
freshness row. No process mutex is used as the source of truth.

## 15. Migration

Added `internal/runtime/migrations/000011_vacancy_freshness_review.up.sql`
and its explicit down migration. The up migration is additive, creates
restrictive foreign keys, preserves legacy vacancies, and adds only the
freshness/review indexes needed by the planned reads.

## 16. Tests

Focused tests cover deterministic normalization, array ordering, material vs
source-only changes, exclusion of local match state, review lifecycle,
idempotency, material change after dismissal, application projection, sync
observation timestamp propagation, migration structure, and the opt-in
PostgreSQL contract.

## 17. Current Dataset After Migration

Based on the P1 baseline snapshot (279 vacancies / 98 application-linked):

| Measure | After `000011` before new observations |
|---|---:|
| Vacancies | 279 |
| First-seen known / unknown | 0 / 279 |
| Last-seen known / unknown | 0 / 279 |
| Source/material fingerprints known | 0 / 279 |
| Explicit seen / interesting / dismissed / prepared / applied | 0 / 0 / 0 / 0 / 0 |
| Effective unseen (not application-linked) | 181 |
| Effective applied from durable applications | 98 |
| Changed since explicit review | unknown/not measurable for legacy rows |

The table intentionally does not call legacy vacancies “new”; it records that
their P1 observation history is unknown.

## 18. Verification

The focused packages pass locally. Standard full-project verification remains
the release gate: formatting, full tests, race tests, vet, builds, diff check,
and JavaScript syntax checks. The live PostgreSQL contract is opt-in through
`POSTGRES_TEST_DATABASE_URL`.

## 19. Safety

This stage adds no HH writes, no AI calls, no application submission, no test
submission, no chat mutation, no candidate mutation, and no ranking changes.
The existing PostgreSQL canonical backend and HH dry-run/write boundaries are
unchanged. Docker is not used.

## 20. Remaining Gaps

- Existing legacy rows need a future P1-aware discovery observation before
  local freshness can be measured.
- Provider publication/update timestamps and several structured vacancy fields
  remain unknown when HH does not supply them.
- No review UI or HTTP action surface exists yet by design.
- Fresh HH applicability still belongs to the existing read-only preflight.

## 21. Recommended Next Stage

`P1.2 — Deterministic Eligibility & Explainable Base Rank`

It should consume this foundation without changing review semantics, source
fingerprints, or application preflight/write safety.
