# P1.4.1 — Fingerprint Compatibility & Runtime Acceptance

## 1. Status

`FINGERPRINT_COMPATIBILITY_PASS`; runtime execution was available and completed in safe read-only mode. The live run is evidence-backed, but the exact per-vacancy pre-detail snapshot was not captured before this resumed run, so fields that require that snapshot are marked `not captured` rather than inferred. P1.5 is not started.

## 2. Fingerprint Compatibility Audit

P1.4 added `professional_roles` to both provider/source and material fingerprint projections while leaving `fingerprint_version=1`. This changed the serialized JSON projection and therefore the SHA-256 input contract.

Answers:

- A: no. An existing vacancy with a populated role list produces different v2 serialized/hash input solely because the new field is now included.
- B: yes. A stored v1 hash could be compared with a v2 hash and differ without provider change.
- C: yes in the old implementation, because review comparison used hash inequality without a version guard.

The dismissed-v1 scenario was therefore unsafe: `dismissed v1 → v2 observation` could have been interpreted as a material update when detail enrichment only populated `professional_roles`.

## 3. Algorithm Version Decision

`FingerprintVersion` is now `2`; `LegacyFingerprintVersion` remains `1` for the upgrade boundary. A focused test proves that v1 omits `professional_roles` byte-for-byte while v2 distinguishes the role list.

On a v1→v2 observation, the observer compares the current value under the v1 projection when possible. A difference in fields already understood by v1 remains a real provider change. A role-only difference cannot be distinguished from first-time enrichment because v1 never stored that input; it establishes the v2 baseline and is detected on subsequent same-v2 observations. This is the deterministic conservative policy.

## 4. Review Decision Compatibility

Migration `000013_vacancy_fingerprint_versions` adds:

- `vacancy_review_states.decision_fingerprint_version`;
- `vacancy_review_events.fingerprint_version`.

Existing rows default to v1. New decisions/events store v2. `changed_since_review` is `null` when the current freshness version and decision version differ; v1 and v2 hashes are never compared directly. A dismissed vacancy remains dismissed during algorithm migration and is not reopened solely by the version change. An explicit subsequent decision records the current version.

## 5. Tests

Added/updated coverage includes:

- v1/v2 projection compatibility and `professional_roles` hash inclusion;
- unknown fingerprint-version rejection;
- no fake material change across a version mismatch;
- real material change detection when versions match;
- PostgreSQL review state/event version persistence;
- migration pairing and rollback checks;
- existing rich→partial merge and detail-failure regression paths.

## 6. Runtime Environment

The repository `.env` is the project-standard configuration source and contains a configured `DATABASE_URL`. `cookies.txt` is present for HH reads. The executed command explicitly forced:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
```

No credentials or raw cookie values were printed. AI was not invoked by the sync.

## 7. Real HH Sample Audit

One bounded sync read 20 vacancies from HH and successfully fetched 20 detail responses. No full descriptions or raw provider bodies were logged. Because the pre-detail item-level snapshot was not persisted by the command, the `Missing before sample` column is intentionally `not captured`.

| Field | Missing before sample | Available from detail | Successfully mapped |
| --- | ---: | ---: | ---: |
| description | not captured | 20/20 post-sync records | 20/20 |
| skills | not captured | 0/20 non-empty in sample | 0/20 |
| professional roles | not captured | 20/20 | 20/20 |
| work format | not captured | 20/20 | 20/20 |
| area | not captured | 20/20 | 20/20 |
| experience | not captured | 20/20 | 20/20 |
| salary amount | not captured | 7/20 | 7/20 |
| published_at | not captured | 0/20 | 0/20 |
| hh_updated_at | not captured | 0/20 | 0/20 |

Run counters: `fetched=20`, `created=15`, `updated=5`, `detail_requested=20`, `detail_succeeded=20`, `detail_skipped=0`, `detail_failed=0`, `detail_fields_enriched=60`. The process reported no sync errors.

## 8. PostgreSQL Acceptance

The standard `ApplyPostgresMigrations` path was run against the configured database. Applied versions are `1..13`; migration 13 is the fingerprint-version compatibility migration. No reset, drop, truncate, or source-of-truth replacement was used.

Counts after the run:

| Relation/metric | After |
| --- | ---: |
| vacancies | 294 |
| vacancy_freshness | 20 |
| vacancy_review_states | 0 |
| vacancy_review_events | 0 |
| v2 freshness rows | 20 |

The opt-in PostgreSQL freshness/review contract passed, including v2 decision/event persistence and cleanup of only its temporary test vacancy.

## 9. Bounded Sync

The ordinary `hh sync vacancies` command was run once. Its detail budget is the existing default of 50; the observed request count was 20, therefore within budget. Duration was 25.87 seconds. The transport remained read-only and no HH write endpoint was called.

## 10. Coverage Before / After

The pre-run baseline in the P1.4 record had 279 rows. The current post-run database has 294 rows because this run created 15 and updated 5. Comparable aggregate coverage is:

| Field | Before (P1.4 baseline) | After | Delta |
| --- | ---: | ---: | ---: |
| description known | 161 | 176 | +15 |
| skills non-empty | 0 | 0 | 0 |
| professional roles non-empty | column absent | 20 | new field |
| work format known | 0 | 20 | +20 |
| area known | 98 | 118 | +20 |
| experience known | 118 | 133 | +15 |
| salary amount known (compensation from/to) | 53 | 53 | 0 |
| published_at known | 0 | 0 | 0 |
| hh_updated_at known | 0 | 0 | 0 |
| freshness rows | not captured | 20 | — |

The baseline did not include a per-detail sample snapshot, so no finer-grained “missing before sample” numbers are claimed.

## 11. Ranking Impact

P1.2 weights remain unchanged (`AlgorithmVersion=v1`) and P1.3 queue semantics remain unchanged. The post-enrichment deterministic PostgreSQL ranking was run with the existing candidate source and fixed diagnostic time.

| Metric | Before | After |
| --- | ---: | ---: |
| eligible | 3 (P1.4 baseline) | 4 |
| review_required | 248 (P1.4 baseline) | 260 |
| ineligible | 28 (P1.4 baseline) | 30 |
| confidence high | not captured | 14 |
| confidence medium | not captured | 208 |
| confidence low | not captured | 72 |
| average unknown_count | not captured | 4.04 |
| median unknown_count | not captured | 4 |
| to_review | not captured | 12 |
| stretch_manual_review | not captured | 168 |

The available comparison shows more evidence-backed fields, but a complete distribution delta requires a pre-sync ranking snapshot with the same database and timestamp. No claim is made that the missing before metrics improved.

## 12. HH Read Cost

Observed cost was 20 search-result records and 20 detail GETs over 25.87 seconds. Detail requests were sequential and bounded by 50. No AI calls were made. No full descriptions or private raw bodies were included in this report.

## 13. Verification

Focused and live checks passed:

```text
go test -count=1 ./internal/vacancy ./internal/vacancyreview ./internal/adapters/storage/postgres ./internal/runtime
POSTGRES_TEST_DATABASE_URL=<configured DATABASE_URL> go test -count=1 -v ./internal/runtime -run TestPostgresVacancyFreshnessAndReviewContract
P1_2_POSTGRES_DIAGNOSTIC=1 POSTGRES_TEST_DATABASE_URL=<configured DATABASE_URL> go test -count=1 -v ./internal/vacancyranking -run TestPostgresCurrentDatasetDiagnostic
```

The full release verification commands remain to be run after this documentation/test adjustment:

```text
gofmt -l .
git diff --check
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go build ./cmd/hh-ai-responder
node --check web/app.js
node --check internal/runtime/web/app.js
```

## 14. Safety

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
Live HH writes observed: 0
AI calls: 0
Candidate truth changed: no
Candidate Knowledge changed: no
P1.2 weights changed: no
P1.3 queue semantics changed: no
Application policy changed: no
PostgreSQL source-of-truth changed: no
Docker used: no
```

## 15. Recommended Next Stage

Do not start P1.5 in this task. Before P1.5, capture a comparable pre-sync ranking/coverage snapshot in the same runtime environment, then rerun only the required evaluation/calibration workflow.
