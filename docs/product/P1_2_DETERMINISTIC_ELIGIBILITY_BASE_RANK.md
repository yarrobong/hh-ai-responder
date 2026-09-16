# P1.2 — Deterministic Eligibility & Explainable Base Rank

## 1. Status

Implementation is complete and the deterministic read-model tests pass. The
PostgreSQL runtime diagnostic evaluated all 279 canonical vacancies. The
dashboard queue/UI was intentionally not changed; that belongs to P1.3.

## 2. Context

P1.1 established separate vacancy freshness and review-state projections. P1.2
consumes those projections together with canonical Candidate Knowledge,
vacancy data, and application linkage. It does not authorize applications.

## 3. Migration Acceptance

The project-standard embedded runner is `runtime.ApplyPostgresMigrations`.
`POSTGRES_TEST_DATABASE_URL` is not exported in the environment, so the
configured local `.env` `DATABASE_URL` was used for the opt-in PostgreSQL
contract and diagnostic tests. The runner confirmed migrations `1..11`,
including `000011_vacancy_freshness_review`.

The resulting read-only state was:

| Measure | Value |
|---|---:|
| Vacancies | 279 |
| Applications | 98 |
| Freshness rows | 0 |
| Review-state rows | 0 |
| Review events | 0 |

No canonical rows, review events, or historical freshness observations were
invented. The contract exposed and fixed one P1.1 PostgreSQL review-action bug:
missing current review state is represented by `ErrReviewStateNotFound` and is
now correctly treated as an absent row inside the transaction. The contract
also now checks the documented changed-fingerprint event semantics.

## 4. Current Inputs

The ranking read-model bulk-loads:

1. all vacancies through `ports.VacancyReader.List`;
2. one canonical candidate snapshot through `ports.CandidateReader`;
3. one application list;
4. one freshness list and one review-state list when the review store exists.

Candidate data enters through `CanonicalEmployerSafeProjection`; only
confirmed/verified facts are available to ranking. Unknown candidate facts stay
unknown.

## 5. Ranking Domain Model

The `internal/vacancyranking` package exposes:

- `Eligibility`: `eligible`, `review_required`, `ineligible`, `unavailable`;
- `FitBand`: `compatible`, `stretch`, `unlikely`, `hard_incompatible`;
- `Confidence`: `high`, `medium`, `low`;
- `AnalysisState`: `complete`, `partial`, `insufficient_evidence`;
- typed `Reason` values with code, outcome, source, concise evidence, and
  confidence;
- `Result` with score components, rankability, review/application state,
  freshness, and legacy evidence.

The algorithm version is `v1`. The result is computed on read; no rank
migration or stale rank cache was introduced.

## 6. Eligibility Model

Eligibility is a safety classification, not a fit score. A hard conflict wins
over every positive signal. Unknown critical evidence produces
`review_required`; it never produces an application.

Archived vacancies are `unavailable`. Explicit office conflicts, mandatory
relocation/business travel conflicts, and explicit 1C-only, cold-sales-only,
QA-only, or management-only roles are `ineligible` when evidence is sufficient.
Ambiguous mixed roles remain reviewable.

## 7. Hard Rules

Structured provider area/location and work format have stronger provenance than
description text. An explicit remote format does not conflict with the
candidate's office-city constraint. An office vacancy in a different known
city is a hard conflict. Missing city/format, missing candidate location, and
missing relocation/travel preference remain unknown/reviewable.

The candidate's exact total experience is read in months. The current confirmed
value remains exactly 11 months. `between1And3` is a stretch signal, not a
blanket rejection. Explicit specialist requirements starting at 36 months are
a known incompatibility with the current 11-month candidate truth and are
hard-gated; generic `between1And3` remains a stretch band.

## 8. Soft Signals

The bounded score is 0–100:

| Component | Range | Unknown behavior |
|---|---:|---|
| Role fit | 0–25 | neutral/uncertain; no title-only hard reject |
| Skill fit | 0–25 | unknown requirements receive neutral partial credit |
| Experience fit | 0–20 | neutral when requirement or candidate months are unknown |
| Location/work format | 0–15 | neutral when not known; hard conflicts are separate |
| Salary fit | 0–10 | neutral when absent/foreign currency; no FX conversion |
| Freshness | 0–5 | neutral when P1 observation timestamps are absent |

Missing information primarily reduces confidence and can require manual review;
it is not treated as a negative candidate fact. The score is deliberately
coarse and is not presented as hiring probability.

## 9. Unknown Handling

Field-level unknowns are emitted for salary, location, work format, experience,
skills, role evidence, and freshness. `data_completeness` is used only as an
analysis-state hint, not as truth. A vacancy with no meaningful content is
`insufficient_evidence`, low-confidence, review-required, and capped at 55.

## 10. Fit Bands

`hard_incompatible` is used only with `ineligible` or `unavailable`. A known
near-threshold experience requirement is `stretch`; a strong specialist gap,
explicitly unavailable required skill, or clearly unrelated role evidence is
`unlikely`. Unknowns alone do not turn a potentially compatible vacancy into a
negative fit band.

## 11. Confidence

Confidence describes completeness and reliability of evidence, not probability
of receiving an offer. Rich detail with few unknowns is high confidence;
partial/detail-light evidence is medium; minimal or absent evidence is low.

## 12. Score / Comparator

The comparator orders rankable results before excluded results, then eligibility,
fit band, descending base score, confidence, review preference, known freshness,
known publication time, and ascending vacancy ID. Known timestamps are compared
only when present. Missing timestamps never receive fabricated recency.

## 13. Review/Application Integration

`applied` or any durable application relation sets `rankable=false` with
`already_application_linked`. `prepared` is excluded from active discovery.
Dismissed vacancies remain excluded unless P1.1 reports a known material change;
then they are reconsideration candidates without changing the stored review
state. `interesting` is retained as positive context and never bypasses a hard
eligibility gate.

Ranking does not replace draft/edit/approve, fresh read-only preflight, explicit
Send, or reconciliation.

## 14. Explainability / Reason Codes

Every result has typed positive reasons, concerns, unknowns, and hard reasons.
Sources are `structured_provider`, `description`, `candidate_truth`,
`review_state`, `application_state`, `legacy_match`, or `derived`. Evidence is
short and does not duplicate raw descriptions.

## 15. Legacy Match Integration

Persisted `match_result` is exposed as optional `legacy_match` evidence,
including its score, confidence, matched/unknown/missing skills, roles, and
risks. Its score is never assigned to `base_rank_score` and its free-text risks
are not hard gates. Unanalyzed vacancies receive the same deterministic result
shape from current vacancy fields and canonical candidate truth.

## 16. Read Model / Performance

`ReadModel.ListRankedVacancies` computes all results in memory after bounded
loads. The structural read groups are fixed at five for PostgreSQL (vacancies,
candidate, applications, freshness, review states), not one query per vacancy.

The current 279-vacancy PostgreSQL diagnostic completed in approximately
0.81 seconds locally. No synchronous AI call was made.

## 17. Tests

Focused tests cover office conflict, remote, unknown location, relocation and
business-travel semantics, role policy, mixed support/sales, exact 11-month
experience behavior, salary/currency unknowns, React.js/Postgres/REST
normalization, review/application exclusions, material-change reconsideration,
determinism, and bounded repository groups. An opt-in diagnostic test prints
privacy-safe counts and top-20 metadata for the current dataset.

## 18. Current Dataset Distribution

PostgreSQL diagnostic on 2026-09-11:

| Measure | Count |
|---|---:|
| Evaluated | 279 |
| Rankable | 167 |
| Application-linked excluded | 98 |
| Application-linked workflow exclusions | 84 |
| Eligible | 3 |
| Review required | 248 |
| Ineligible | 28 |
| Unavailable | 0 |
| Compatible | 24 |
| Stretch | 227 |
| Unlikely | 0 |
| Hard incompatible | 28 |
| Confidence high / medium / low | 0 / 207 / 72 |
| Analysis complete / partial | 161 / 118 |

These are diagnostic distributions, not validated ranking-quality metrics.

## 19. Diagnostic Top/Bottom Review

The top diagnostic rows contain internship/technical-support/developer
signals, but several ambiguous or clearly unrelated rows remain review-required
because current structured fields and persisted skill evidence are sparse. The
sanity check confirmed that application-linked rows are not active top results
and that legacy match scores do not control the new score. The current dataset
still has no freshness rows, so freshness remains explicitly unknown.

## 20. Known Limitations

- P1-aware synchronization has not yet populated freshness for legacy rows.
- HH structured skills, requirements, work format, and provider timestamps are
  sparse in the current dataset.
- The search salary filter remains unchanged at its existing configured value;
  P1.2 does not alter discovery configuration.
- No manually labeled gold set exists yet, so no precision@5/@10 claim is made.
- Final queue sections, review actions, pagination, and Today integration are
  intentionally deferred to P1.3.

## 21. Verification

The relevant commands are:

```bash
go test ./internal/vacancyranking -count=1
P1_2_POSTGRES_DIAGNOSTIC=1 POSTGRES_TEST_DATABASE_URL="$DATABASE_URL" \
  go test ./internal/vacancyranking -run TestPostgresCurrentDatasetDiagnostic -count=1 -v
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go build ./cmd/hh-ai-responder
node --check web/app.js
node --check internal/runtime/web/app.js
git diff --check
```

## 22. Safety

```text
HH_DRY_RUN=true                 (local configured environment)
HH_WRITE_ENABLED=false          (local configured environment)
Live HH writes observed: 0
New AI calls: 0
Candidate truth changed: no
Candidate Knowledge changed: no
Existing match weights changed: no
Application policy changed: no
HH write safety changed: no
Review-state/fingerprint semantics: preserved
PostgreSQL source-of-truth: preserved
Docker used: no
```

## 23. Recommended Next Stage

`P1.3 — Ranked Queue Projection & Review UI`

P1.3 can consume this read-model and define queue sections/actions while keeping
ranking separate from application authorization.
