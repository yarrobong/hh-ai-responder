# Executive summary

S3.Apply: **PARTIAL**

S3: **BLOCKED**

Runtime canonical storage remains **not accepted for cutover**. The approved
Candidate PostgreSQL import transaction committed one Candidate aggregate, but
the existing post-apply verifier reported a semantic difference. Per the
approved stop policy, career import and semantic bootstrap were not started.

The failure is diagnosable: the PostgreSQL read model orders two stories by ID
while the source preserves their original order, and PostgreSQL returns the
same Candidate timestamp normalized from `+05:00` to `Z`. The current verifier
uses strict JSON equality and therefore rejects this round-trip. No migration
code was changed to bypass the failure.

## Safety

Original JSON: **UNCHANGED**. Original source hashes match
`docs/storage/STORAGE_S3_SOURCE_MANIFEST.md`, including the excluded
conversations.

HH writes: **0**

Effective safety configuration remained `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`. S3.Apply made no HH requests.

## Apply source

Path: `/tmp/hh-ai-responder-s3apply.s3xFP5`

Frozen: **YES**

Created: `2026-09-10T11:40:48Z`

Verified manifest: `/tmp/hh-ai-responder-s3apply.s3xFP5-manifest-verified.txt`

The frozen source contains 11 JSON input files. Their sizes and SHA-256
digests were recorded before database mutation and rechecked afterwards; the
snapshot is unchanged.

Counts:

| Entity | Frozen source |
|---|---:|
| Vacancies | 279 |
| Applications | 98 |
| Application events | 308 |
| Conversations | 259 |
| Messages | 791 |

## PostgreSQL destination

Database: `hh_ai_responder_s3`

Identity: **PASS** — PostgreSQL 14.19, role `Yaroslav`

Schema: `000001–000010`

pgvector: `0.8.6`

Destination was empty before the Candidate import. After the stopped stage,
career tables remain empty; one Candidate and its canonical child rows are
present.

## Candidate migration

Dry-run: **PASS** — status `new`, safe to apply `true`, conflicts `0`.

Apply: **COMMITTED, then verification FAILED**. The report is
`/tmp/hh-ai-responder-s3apply.s3xFP5-candidate-apply.json`.

Canonical CandidateID: `candidate-local`

Committed Candidate state: one canonical Candidate; child counts include
education `1`, language `1`, experience `1`, skills `22`, projects `3`, claims
`58`, stories `5`, unknowns `7`, knowledge events `15`, sources `145`, and
evidence `145`.

Parity: **BLOCKED** by the existing strict verifier. The logical data is
present, but stories `ekb-metro` and `crm-integrations` are returned in ID
order instead of source order, and the timestamp representation is normalized
to UTC. No unexplained field-content difference was found by the bounded
read-only diagnostic.

## Career migration

Dry-run: **NOT RUN after Candidate APPLY**. The pre-apply frozen-source
dry-run passed with 279 vacancies, 98 applications, 308 events, 259
conversations, 791 messages, zero relation errors, zero conflicts, and
`safe_to_apply=true`.

Apply: **NOT STARTED**.

## Count parity

| Entity | S3 source | PostgreSQL after stop | Difference |
|---|---:|---:|---:|
| Candidate | 1 | 1 | 0 |
| Vacancies | 279 | 0 | not imported |
| Applications | 98 | 0 | not imported |
| Application events | 308 | 0 | not imported |
| Conversations | 259 | 0 | not imported |
| Messages | 791 | 0 | not imported |

## Identity parity

Vacancy IDs: **NOT RUN** — career import did not start.

Application IDs: **NOT RUN** — career import did not start.

Conversation IDs: **NOT RUN** — career import did not start.

Message IDs: **NOT RUN** — career import did not start.

## Relations

Career relation checks: **NOT RUN after apply**. No career rows exist in the
destination, so no imported relation or orphan was created.

## Historical partial vacancies

Expected: `19`

Imported: **NOT RUN** — career import did not start.

Integrity: The frozen source contains all approved IDs as exact non-zero
`data_completeness=partial` records. No PostgreSQL round-trip was performed.

## Explicit legacy exclusions

Conversations excluded: `2`

Messages excluded: `2`

Imported accidentally: `0` — career import did not start.

Original JSON preserved: **YES**. Both excluded conversations remain in the
original `employer_conversations.json`; neither is in the frozen source.

## R14 authority

Application attempts: source absent/`0`; PostgreSQL `0`.

Auto-chat attempts: source absent/`0`; PostgreSQL `0`.

Controlled actions: `13`; `hh_write_actions.json` remains available and its
source-manifest hash is unchanged. No attempt history was derived from
Candidate or career data.

## Embedding contract

Provider: `openai-compatible`

Model: `mistral-embed`

Dimensions: `1024`

SpaceID: `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9`

## Semantic status before reindex

Read-only status after Candidate import: **REINDEX_REQUIRED**. The active
contract is the expected provider/model/dimension/SpaceID; three project
documents are eligible and stale, and zero semantic documents are indexed.

## Semantic reindex dry-run

**NOT STARTED**. The stage stopped before semantic work.

## Semantic reindex apply

**NOT STARTED**.

## Semantic status after reindex

**NOT APPLICABLE**. Required `READY` status was not reached.

## Semantic document integrity

No semantic documents were written. Active 1024-dimensional vector integrity
and legacy-row checks were not applicable.

## Semantic retrieval smoke

**NOT RUN**. No provider request or semantic retrieval was made during the
stopped S3.Apply.

## Post-migration idempotency preview

Candidate post-import dry-run reports `status=conflict` and
`safe_to_apply=false` because the same strict round-trip verifier difference
remains. Career idempotency preview was not run.

## Source integrity

Original JSON hashes: **PASS**

Frozen migration source: **PASS**

## Source-of-truth

Candidate: **PostgreSQL**, partially imported and not yet accepted for runtime
cutover.

Vacancies: PostgreSQL, not imported.

Applications: PostgreSQL, not imported.

Conversations: PostgreSQL, not imported.

Semantic: PostgreSQL/pgvector, not bootstrapped.

Operational local JSON: preserved intentionally.

## Regression

S1: **PASS in S3.Resolve baseline; not rerun after stop**

S2: **PASS in S3.Resolve baseline; not rerun after stop**

R14: **PASS in S3.Resolve baseline; not rerun after stop**

## Verification

Tests: **NOT RERUN after stop**

Race: **NOT RERUN after stop**

Vet: **NOT RERUN after stop**

Build: **NOT RERUN after stop**

Canonical: **BLOCKED** by post-apply Candidate verification

Diff: **PASS for pre-existing worktree state; no production code change made**

Gofmt: **NOT RERUN after stop**

Node: **NOT RERUN after stop**

Docker: **NOT APPLICABLE**

## HH safety

HH_DRY_RUN: `true`

HH_WRITE_ENABLED: `false`

Vacancy response writes: `0`

Application/test writes: `0`

Chat sends: `0`

Chat leaves: `0`

Resume touches: `0`

Job-status writes: `0`

LIVE HH WRITES: `0`

HH WRITE RETRY: **NONE**

## Final decision

S3: **BLOCKED**

Reason: Candidate data committed, but the existing post-apply parity verifier
rejected deterministic ordering/timezone normalization. Career import and
semantic bootstrap were intentionally not started. The next step requires a
separate S3.Fix decision for the verifier/migration contract, followed by an
explicit recovery plan for the committed Candidate row; do not delete or
truncate it automatically.

## Ready

V2 — PostgreSQL + pgvector Runtime Acceptance: **NOT READY**.

Do not start V2, semantic reindex, R15, product development, or any HH write
until the partial state is reviewed and the migration issue is explicitly
resolved.
