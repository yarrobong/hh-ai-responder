# S3.Fix-1 — Candidate Migration Semantic Parity

## Executive summary

S3.Fix-1: **COMPLETE**

Candidate migration state: **candidate-local COMMITTED; Career NOT IMPORTED;
semantic index NOT BOOTSTRAPPED**.

The old verifier compared marshaled Candidate JSON byte-for-byte. The new
dedicated `internal/candidateparity` comparator verifies semantic Candidate
content and is used by migration planning, post-apply verification, and
PostgreSQL import idempotency. It does not modify Candidate data, source JSON,
schema, Career data, or the semantic index.

## Root cause

The previous comparison path was `reflectCandidateEqual` in
`internal/runtime/candidate_migration.go`, with the same strict JSON behavior
duplicated as `candidateEqual` in the PostgreSQL adapter. Both compared
`json.Marshal(Candidate)` strings.

The committed PostgreSQL read model differed only in representation:

- story rows were read with `ORDER BY id`, while the source Candidate retained
  the JSON list order;
- PostgreSQL returned an equivalent instant in UTC `Z` form instead of the
  source `+05:00` offset form.

The bounded diagnostic found no unexplained Candidate field-content
difference.

## Previous verifier semantics

Previous behavior was strict serialized JSON equality. It therefore treated
timezone formatting, Go/JSON representation details, and repository iteration
order as Candidate conflicts. It did not produce field-level semantic
differences and could not distinguish an unordered identity collection from an
ordered historical sequence.

## New semantic parity contract

The comparator is `candidateparity.CompareCandidateSemantic(source,
persisted)`. It returns `Equivalent` and value-free structured differences
with paths such as `candidate.stories[story-id].summary`.

- Scalar semantic values compare exactly, including empty/unknown/status
  values.
- Typed timestamps representing instants compare by exact nanosecond instant;
  timezone location/format is not significant.
- Human date/period fields stored as strings are not parsed or normalized.
- Identity collections compare by identity key plus complete entity content.
  Missing, extra, duplicate, or changed entities fail parity.
- JSON objects/maps compare by key/value semantics; JSON array order remains
  strict unless the array is an explicitly audited identity collection.
- Provenance and evidence remain strict content checks. Source and evidence
  list order is preserved.
- Knowledge events remain a strict ordered history sequence.

The comparator uses no candidate values in diagnostics, so parity reports do
not copy private candidate data.

## Collection ordering audit

| Collection | Ordered semantically | Comparison rule | Evidence |
|---|---:|---|---|
| `contacts` | No | Stable `id` identity map; compare full entity | No position column; canonical read contract is identity-based |
| `external_references` | No | Stable `id` identity map; compare full entity | No position column; repository reads by `id` |
| `education` | No | Stable `id` identity map; compare full entity | No product ordering contract; repository reads by `id` |
| `languages` | No | Stable `id` identity map; compare full entity | No product ordering contract; repository reads by `id` |
| `experience` | No | Stable `id` identity map; compare full entity | Entities have stable IDs; repository reads by `id` |
| `experience.skills_used` | No | Stable use `id` identity map | Relational skill-use rows have stable IDs |
| `experience.story_ids` | No | Identity-reference set | Story references are relational links, not a position list |
| `skills` | No | Stable skill `id` identity map; compare full entity | Canonical skills have stable IDs; repository reads by `id` |
| `skills.source_ids` / `claim_ids` | No | Identity-reference sets | References identify sources/claims; no position contract |
| `skills.capabilities` | No | Stable capability `id` identity map | Relational capability primary key is `(skill_id, capability_id)` |
| `skills.uses` | No | Stable use `id` identity map | Relational skill-use rows have stable IDs |
| `skills.source_assertions` | No | Stable assertion `id` identity map | Assertions are independently identified source values |
| `skills.source_assertions.projects` | No | Identity-reference set | Project references identify related entities |
| `skills.source_assertions.uses` | No | Stable use `id` identity map | Uses are identified relations |
| `projects` | No | Stable project `id` identity map; compare full entity | Canonical projects have stable IDs; repository reads by `id` |
| `projects.source_ids` / `claim_ids` / `related_skills` / `achievement_ids` / `story_ids` | No | Identity-reference sets | Relational/reference semantics; no position column |
| `projects.skill_uses` | No | Stable use `id` identity map | Relational skill-use rows have stable IDs |
| `achievements` | No | Stable achievement `id` identity map; compare full entity | No position column; repository reads by `id` |
| `preferences` | No | Stable preference `id` identity map; compare full entity | No position column; repository reads by `id` |
| `constraints` | No | Stable constraint `id` identity map; compare full entity | No position column; repository reads by `id` |
| `stories` | No | Stable story `id` identity map; compare full entity | Stories are reusable narrative context, not ordered facts; schema has no position column |
| `stories` content arrays | Yes where represented as arrays | Strict array order and content | PostgreSQL JSONB payload preserves array order; no verifier-wide sorting |
| `stories.profile_refs` | No | Identity-reference set | References identify related profile entities |
| `claims` | No | Stable claim `id` identity map; compare full entity | Claims are identified facts/state records |
| `unknowns` | No | Stable unknown `id` identity map; compare full entity | Unknown state is content; ID is identity |
| `proposals` | No | Stable proposal `id` identity map; compare full entity | Proposal state/content is strict; collection has no user order |
| `events` / knowledge history | **Yes** | Strict logical sequence, timestamps, IDs, payloads and provenance | Append-only history; causal/time sequence must not be normalized away |
| `knowledge sources` | **Yes within metadata** | Strict source record order/content; observed instants semantic | `source_index` is persisted and source records are provenance |
| `evidence` | **Yes within metadata** | Strict evidence order/content | `evidence_index` is persisted and evidence is safety-relevant |
| `profile_snapshot` list values (`preferred_roles`, `always_emphasize`, etc.) | **Yes** | Strict array order/content | Profile JSONB preserves these arrays; no position-independent contract was established |
| `resume_facts` | N/A | Typed scalar/object content; JSON object keys semantic, arrays strict | Read-model JSON value; no collection-order normalization |

Narrative/content string lists such as experience responsibilities and
achievements, skill capabilities and claim restrictions, and project
technologies/tasks/results are also strict. They are content arrays, not
generic sets. No global slice sorting was introduced.

## Timestamp semantics

All typed `time.Time` fields are normalized to UTC for comparison while
retaining exact nanosecond precision. Thus
`2026-09-10T12:00:00+05:00` equals `2026-09-10T07:00:00Z`, but an actual
instant difference, zero/invalid domain timestamp, or lost nanosecond is a
parity difference.

The comparator does not parse arbitrary strings that look like dates. For
example, experience `start_date` and `end_date` remain scalar strings and
different timezone spellings there are not silently treated as equal. Invalid
timestamp text is rejected at the typed source/model boundary rather than
being guessed by parity verification.

## Story order decision

**NON-SEMANTIC**

Reason: the Candidate story model describes stories as reusable communication
context and explicitly not as independent evidence. The source JSON list has
no documented priority/position contract, the CLI displays the stored list
without assigning positional meaning, and PostgreSQL has no story position
column. Cover-letter selection uses relevance score and a stable source-index
tie-break only; that incidental tie-break is not a persisted Candidate truth
or user-controlled ordering guarantee. The comparator therefore compares
stories by stable logical identity and complete content.

The decision is intentionally limited to the top-level story collection. It
does not sort story content arrays, event history, provenance, or evidence.

## Strictness / negative cases

Focused regression tests cover:

- equal and different typed instants, including nanosecond precision and a
  zero timestamp versus an instant;
- equivalent PostgreSQL round-trip with reordered stories and UTC timestamp;
- changed, missing, and duplicate story identity/content;
- strict reordered event history;
- changed provenance and evidence;
- unchanged human date strings remaining strict;
- changed Candidate identity/content remaining a conflict.

Differences remain conflicts in migration planning. Equivalent destinations
are classified as `already_present` and are never rewritten.

## Existing PostgreSQL Candidate verification

CandidateID: `candidate-local`

Semantic parity: **PASS**

Remaining differences: **none**

Verification was read-only against the already committed Candidate. Candidate
was not re-imported.

## Candidate migration dry-run after fix

Command used the existing source directory and PostgreSQL database in dry-run
mode only. Result:

`Status: already_present`

`Safe to apply: true`

It did not report a conflict for the timezone representation or story
iteration order and did not propose a destructive rewrite.

## Database state

Candidate: **1** (`candidate-local`, version `1`)

Candidate child/read-model counts: external references `1`, education `1`,
languages `1`, experience `1`, skills `22`, projects `3`, preferences `13`,
constraints `4`, claims `58`, stories `5`, unknowns `7`, knowledge events `15`,
knowledge sources `145`, evidence `145`; contacts, achievements, and proposals
are `0`.

Career: **0** vacancies, applications, application events, conversations, and
conversation messages.

Semantic: **0** documents.

Automatic application attempts: **0**.

Legacy auto-chat attempts: **0**.

No database data was changed by S3.Fix-1.

Schema migration: **NONE**. No `000011` or other migration was added.

## Source integrity

Original Candidate JSON remains **UNCHANGED**. SHA-256 values still match the
source manifest, including `candidate_profile.json`, `candidate_stories.json`,
`candidate_skills.json`, `candidate_projects.json`, `candidate_achievements.json`,
`candidate_unknowns.json`, `candidate_proposals.json`, and
`candidate_events.json`.

## R14 / S1 / S2 regression

R14 controlled actions: **13 and unchanged**. Application attempt semantics,
autochat attempt semantics, controlled actions, reconciliation, notifications,
HH write gateway, and retry behavior were not changed.

S1 persistence source-of-truth: **unchanged**.

S2 embedding contract: **unchanged** (`openai-compatible`, `mistral-embed`,
`1024` dimensions). No embedding provider was called.

## Verification

tests: **PASS**

race: **PASS**

vet: **PASS**

build: **PASS**

canonical: **PASS**

diff: **PASS**

gofmt: **PASS**

node: **PASS**

Docker: **NOT APPLICABLE**

LIVE HH WRITES: **0**

## Decision

The existing Candidate passes semantic parity and the Candidate migration dry
run recognizes it as equivalent/already migrated.

`S3.Apply.Resume: READY`

Do **not** start it in this stage. Candidate APPLY, Career import, semantic
reindex, V2, and HH writes remain stopped.
