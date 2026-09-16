# P1.4 — HH Vacancy Data Enrichment & Import Completeness

## 1. Status

`PASS` for the read-only adapter/import implementation and fixture-based acceptance. The follow-up live acceptance is recorded in [P1.4.1](P1_4_1_FINGERPRINT_RUNTIME_ACCEPTANCE.md): one safe HH vacancy sync enriched 20 records and PostgreSQL migrations 1–13 were verified without HH writes.

## 2. Context

P1.1–P1.3 established freshness/fingerprint state, deterministic ranking, and the ranked review queue. P1.4 keeps those semantics and improves the provider read path before persistence. The [official HH API OpenAPI reference](https://api.hh.ru/openapi/redoc) lists structured `key_skills`, `professional_roles`, `work_format`, salary, experience, and timestamp fields.

## 3. Pre-Change Coverage

The current checkout uses the existing PostgreSQL baseline recorded on 2026-09-11. No live database was available for a second query in this run.

| Field | Known before | Unknown/empty before |
| --- | ---: | ---: |
| Description | 161 | 118 |
| Structured skills | 0 | 279 |
| Structured requirements | 0 | 279 |
| Work format | 0 | 279 |
| Area name | 98 | 181 |
| Work experience | 118 | 161 |
| Salary amount | 53 | 226 |
| Published timestamp | 0 | 279 |
| HH updated timestamp | 0 | 279 |

Queue baseline: 279 rows; 3 eligible, 248 review-required, 28 ineligible, 0 unavailable; 167 rankable and 98 application-linked.

## 4. HH Search vs Detail Contract

The adapter uses the existing HTML bootstrap JSON and normalizes both `vacancyView` and `redirectConfig`.

| Field | Search response | Detail response | Currently persisted |
| --- | --- | --- | --- |
| description | sometimes present/partial | `vacancyView.description` | yes, 118 empty before |
| key_skills | `keySkills`/`key_skills` aliases when present | same aliases | existing `skills`, 0 non-empty before |
| professional_roles | role aliases when present | role aliases / `professionalRoleIds` | new `professional_roles` JSONB |
| experience | `workExperience`/`experience` | `redirectConfig.workExperience` or aliases | `work_experience` |
| employment | `employment`/employment-form aliases | same aliases | `employment_type` |
| schedule | `@workSchedule`/`workSchedule`/`schedule` | same aliases | `work_schedule` |
| work format | `workFormat`/`workFormats` aliases | `workFormats`/work-format aliases | canonical `remote`/`office`/`hybrid` |
| area | `area.name` | `area.name` | `area_name` |
| address | not relied on | `address` object/string | `location` when supplied |
| salary | `salary`/`compensation` | `salary_range`/`salary`/`compensation` | salary fields |
| published_at | `publicationTime`/`publishedAt` aliases | same aliases | `published_at` |
| created_at | not used as publication time | `creationTime` retained as legacy text | local only; never substituted |
| hh_updated_at | `lastChangeTime`/`updatedAt` aliases | same aliases | `hh_updated_at` |
| alternate_url | `links.desktop` | `links.desktop`/`alternate`/`alternate_url` | `links` JSONB |
| employer | `company.name` | `company.name` | company fields |
| response/test/archived | provider flags when supplied | detail aliases | existing booleans |

`requirements []string` remains empty unless a trusted provider field supplies it. Snippets or description text are not promoted into structured requirements.

## 5. Current Import Pipeline

```text
HHReadSyncService → hhreadsync.ReadBatch → HH search GET
→ optional bounded vacancy detail GET → hhread.VacancyRecord
→ hhreadsync.MapVacancy → PostgreSQL ObserveVacancy / JSON import
```

`ReadVacanciesWithSearch` remains a search-only compatibility method. Automatic synchronization uses one enrichment/import path and does not add a second importer.

## 6. Field Loss Analysis

- Description: search items can omit it; the former profile path had no full detail mapping. Detail now reads `vacancyView.description`.
- Skills: the domain had `Skills`, but `keySkills`/`key_skills` were not mapped. Detail now extracts structured names.
- Area: area was collapsed into `Location`, while `MapVacancy` did not populate `Area.Name`. `AreaName` is now carried separately.
- Work format: no mapping existed. Only source-supported `remote`, `office`, or `hybrid` is emitted; schedule stays separate.
- Experience: HH categories are preserved without converting them to artificial months.
- Salary: structured `from`/`to`/currency is retained; missing salary stays unknown.
- Timestamps: publication and last-change fields are parsed separately. Local timestamps are never promoted to HH timestamps.
- Professional roles: names/IDs are retained in a minimal structured JSONB array and included in deterministic vacancy text.

## 7. Sample Detail Audit

One partial search item plus one rich detail response was tested with two GETs and no writes.

| Vacancy | Search desc | Detail desc | Skills gained | Area gained | Format gained | Exp gained | Timestamp gained |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| fixture 7 | missing | yes | 1 field | 1 field | 1 field | 1 field | 2 fields |

The fixture also proved salary and one professional-role mapping. Full descriptions are not emitted into logs or this report.

## 8. Chosen Enrichment Policy

Detail reads happen only for new records, records without a successful detail marker, records missing description/skills/work format/area/experience, or records whose provider update timestamp changed. The default per-sync detail budget is 50; an explicit smaller `ReadOptions` limit is supported, and zero means this conservative default, not unlimited.

Success stores `hh_detail_enrichment_version=1` and an enrichment time in provider metadata. Unchanged sufficiently enriched rows are skipped later. Detail reads are sequential through the existing bounded HH transport, whose GET 429 retry remains capped at two retries with `Retry-After` support.

## 9. Field Authority / Merge Rules

| Field | Search | Detail | Empty search overwrites detail? |
| --- | --- | --- | --- |
| title/company/link | provider | provider | no |
| description | partial | full detail | no |
| skills/roles | structured when present | structured detail | no |
| area/address | metadata | detail | no |
| format/schedule/experience | structured aliases | detail aliases | no |
| salary | structured compensation | structured detail compensation | no |
| provider timestamps | provider aliases | detail aliases | no |
| response/test flags | provider | detail | prior positive value preserved |

Empty normalized values are treated as omitted because the current DTO does not carry omitted-vs-null presence bits. Failed refreshes preserve rich fields. Explicit provider removal is not applied until a presence-aware contract proves removal semantics.

## 10. Mapping Changes

Added typed `ReadVacancyDetail`, structured alias decoding, canonical work-format mapping, separate area/address fields, professional-role mapping, and safe timestamp parsing. Search and detail feed the same normalized record and `MapVacancy` path.

## 11. Persistence Changes

Existing skills, area, salary, timestamp, compensation, and metadata columns are reused. Migration `000012_vacancy_provider_enrichment` adds only `professional_roles JSONB`; it is additive and preserves current rows. PostgreSQL scan/insert/update paths and JSON compatibility were updated together.

## 12. Fingerprint/Freshness Compatibility

Professional roles changed the provider/material fingerprint input contract. P1.4.1 therefore bumps the fingerprint algorithm from v1 to v2, records the version with review decisions/events, and treats a v1→v2 observation as a migration baseline rather than a provider update. The observer still merges the incoming partial search snapshot before fingerprinting, so omitted detail fields do not create a false source/material change. `first_seen_at`/`last_seen_at` still advance on successful search observation only; detail reads do not change them.

## 13. Failure / Retry Behavior

Search success plus detail failure remains a successful partial batch. The vacancy is imported with safe search fields, `detail_failed` is counted, and a privacy-safe warning is returned. Previous rich data is never cleared by a failed refresh. 404/429/5xx and malformed detail payloads remain read errors and authorize no HH write.

## 14. Legacy Enrichment Strategy

No automatic 279-row backfill was run. The first P1.4-aware sync can enrich at most 50 qualifying rows, including legacy rows without a marker. This is bounded and resumable without a parallel importer or startup-wide detail fetch.

## 15. Tests

Focused coverage includes search/detail mapping, skills/roles/work-format/area/experience/salary/description/timestamps, bounded success/failure, rich-then-partial preservation for JSON and PostgreSQL observer logic, migration pairing, and fingerprint/ranking compatibility.

## 16. Runtime Sync Validation

The live run used:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
```

Counters are `detail_requested`, `detail_succeeded`, `detail_skipped`, `detail_failed`, and `detail_fields_enriched`; no HH write path is reachable from this sync.

## 17. Post-Enrichment Coverage

The canonical after-counts and the limitations of the pre-run snapshot are recorded in P1.4.1. The live run created 15 rows, updated 5, fetched 20, succeeded on all 20 detail reads, and enriched 60 fields. Skills and provider timestamps remained unknown in the live sample; fixture coverage still demonstrates their mapping paths.

## 18. Ranking Impact

P1.2 score weights and P1.3 section semantics were not changed. The live after-ranking distribution and the pre-run baseline limitations are recorded in P1.4.1; any distribution change must come only from persisted provider evidence.

## 19. Performance / HH Read Cost

The live sync took 25.87 seconds for 20 detail requests. The policy adds at most 50 sequential detail GETs per sync by default and skips unchanged enriched rows. Existing request interval/concurrency and bounded 429 behavior are retained. Database writes occur after network reads; no transaction is held during HH I/O.

## 20. Remaining Gaps

- The resumed run did not capture a per-vacancy pre-detail snapshot, so some before/after field deltas remain explicitly unavailable; no values are inferred.
- Omitted-vs-explicit-null/removal semantics are not represented in the normalized DTO.
- `requirements` remain unknown because no trusted separate requirements array was observed.
- `ReadVacanciesWithSearch` remains search-only compatibility behavior.
- Discovery salary-filter and keyword expansion were intentionally not changed.

## 21. Verification

Focused commands:

```bash
go test -count=1 ./internal/adapters/hh/read
go test -count=1 ./internal/usecase/hhreadsync
go test -count=1 ./internal/vacancy
go test -count=1 ./internal/adapters/storage/postgres
go test -count=1 ./internal/vacancyranking
go test -count=1 ./internal/runtime
```

Release commands:

```bash
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

## 22. Safety

```text
HH_DRY_RUN=true                    (required for future live read-only run)
HH_WRITE_ENABLED=false             (required for future live read-only run)
Live HH writes observed: 0
New AI calls: 0
Candidate truth changed: no
Candidate Knowledge changed: no
P1.2 ranking weights changed: no
P1.3 queue semantics changed: no
Review-state semantics changed: no
Application policy changed: no
HH write safety changed: no
PostgreSQL source-of-truth changed: no
Docker used: no
```

## 23. Recommended Next Stage

`P1.4.1` is the current acceptance record. Do not begin P1.5 until its remaining baseline limitation is resolved or explicitly accepted with a comparable pre-sync snapshot.
