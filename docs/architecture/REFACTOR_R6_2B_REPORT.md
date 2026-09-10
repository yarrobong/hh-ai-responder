# Before

`JSONCandidateRepository` was implemented in package `main`. It loaded the
root `CandidateKnowledgeBase`, root `CandidateProfile`, root stories, and
called the root canonical mapper. The JSON knowledge collections had already
been extracted in R6.2a, but the repository could not be imported without a
package-main dependency.

`CandidateProfile` combined the legacy persisted value with JSON file I/O,
profile import/merge, bootstrap, interactive editing, and HH resume
orchestration. `CandidateStory` and story JSON decoding were also root-owned.
The canonical mapper was deterministic, but root-owned and therefore not
available to an importable JSON reader.

Root blockers were the structural profile/story values, profile/story storage,
`CanonicalCandidateInput`, the canonical mapper, and the repository itself.
Profile merge/import, bootstrap, HH resume import, CLI commands, and backend
selection were intentionally retained in root.

# Classification

| Symbol | Before owner | After owner | Reason |
|---|---|---|---|
| `CandidateProfile` | `main` | `internal/candidate.CandidateProfile` | Legacy persisted value is a Candidate-domain value; root is an alias. |
| Profile validation | `main` | `internal/candidate` + JSON adapter | Intrinsic validation is domain-owned; schema and file checks are persistence-owned. |
| Profile load/save | `main` | `jsonstorage.CandidateProfileStore` | Low-level JSON persistence is now importable. |
| Profile import/merge | `main` | `main` + domain merge helper | External workflow and backup decisions remain root; merge algorithm is domain-owned. |
| `CandidateStory` | `main` | `internal/candidate.CandidateStory` | Pure narrative value; stories are not evidence. |
| Story load/decode | `main` | `jsonstorage.CandidateStoryStore` | Path, missing-file, envelope, array, and malformed-file behavior are storage concerns. |
| Story selection/rendering | `main` | `main` | Vacancy/context-dependent workflow and prompt policy remain root. |
| Canonical assembly input | `main` | `internal/candidate.CanonicalCandidateInput` plus root compatibility input | Domain readers need an importable typed input; old root call sites remain source-compatible. |
| Canonical mapper | `main` | `internal/candidate.BuildCanonicalCandidate` | One deterministic mapping and source-priority implementation. |
| `JSONCandidateRepository` | `main` | `jsonstorage.CandidateRepository` | Implements `ports.CandidateReader` without package-main dependencies. |
| `CandidateKnowledgeBase` | `main` | `main` compatibility facade | Contains `ProfilePath` and mutation/workflow methods; repository does not import it. |
| Backend selection | `main` | `main` | Composition is unchanged and still selects JSON or PostgreSQL. |

# Legacy profile ownership

Structural type:

`internal/candidate.CandidateProfile` is the only structural definition.
Root declares `type CandidateProfile = candidate.CandidateProfile`.
`ProfileSnapshot` remains only as an internal/candidate compatibility alias.

Domain validation:

`candidate.ValidateCandidateProfile` owns intrinsic profile fact, skill level,
and pending-question validation. The JSON adapter performs required fields,
schema version, strict unknown-field decoding, secret scanning, and file I/O.

File persistence:

`jsonstorage.CandidateProfileStore` owns profile read/write, private directory
and file modes, strict decoding, and the existing `path + ".tmp"` replacement
mechanics. Profile backups remain in root because backup-before-import is part
of the import workflow, not ordinary profile save semantics.

Root workflows retained:

Interactive CLI editing, bootstrap, HH resume import, import/merge orchestration,
backup creation, and commands remain in package `main`. Root load/save names are
thin compatibility wrappers over the JSON store.

# Story ownership

Value:

`internal/candidate.CandidateStory` is the sole story value definition. Root
keeps an alias.

Persistence:

`jsonstorage.CandidateStoryStore` owns `candidate_stories.json` decoding. It
preserves missing-file-as-empty behavior, empty-file failure, array and versioned
envelope forms, secret rejection, IDs, fields, and story validation.

Deferred workflow:

Relevance scoring, story selection, prompt rendering, and profile CLI output
remain root-owned.

# Canonical assembly

Owner:

`internal/candidate.BuildCanonicalCandidate`.

Input:

`candidate.CanonicalCandidateInput` combines the domain profile, typed
`candidate.KnowledgeSnapshot`, stories, optional HH-derived `ResumeFacts`, and
runtime identity/config values. The root `CanonicalCandidateInput` remains a
compatibility facade and performs only a typed conversion.

Output:

`candidate.Candidate`.

Source priority:

Reused from `internal/candidate`; no mapper-local truth/provenance priority was
added. The employer-safe projection remains `internal/candidate` and is only
called through a root compatibility wrapper where legacy callers require it.

Duplicate mapper:

NONE. The root mapper is now a compatibility converter with no mapping loops.

# JSON CandidateRepository

Owner:

`internal/adapters/storage/json.CandidateRepository`.

Port:

`ports.CandidateReader`.

Dependencies:

The repository depends on `internal/candidate` and `internal/ports`; the JSON
adapter package also contains the existing knowledge and acquisition stores,
which depend on `internal/platform` and `candidateacquisition` as before.
There is no package-main, HH, AI, Dashboard, or PostgreSQL dependency.

Root implementation:

REMOVED. Root retains an alias and thin constructors for compatibility. The
legacy constructor converts the root compatibility facade once into a domain
snapshot; it does not provide a second repository or assembly implementation.

# Candidate capabilities

CandidateReader:

JSON ADAPTER

CandidateAcquisitionStore:

JSON ADAPTER

CandidateWriter:

NOT IMPLEMENTED

CandidateMutationWriter:

NOT IMPLEMENTED

# Snapshot semantics

Each `CurrentCandidate` call loads profile, all six knowledge collections, and
stories, then assembles a fresh canonical candidate. Missing profile and story
files preserve their existing semantics; malformed existing files fail closed.
The returned candidate is detached: source slices and nested values are copied
by the assembly path and a regression test verifies mutation of one result does
not affect a later read.

The files are not protected by one cross-file transaction. Knowledge save keeps
its historical validate/stage/rename behavior, so a multi-file read is not
claimed to be atomic. Parallel reader calls are covered and pass the race
suite; no stronger cross-file consistency guarantee was introduced.

# JSON compatibility

Profile:

PASS — path, schema version, strict decoding, missing-file behavior, private
permissions, and replacement mechanics are preserved.

Knowledge:

PASS — existing R6.2a store is reused through its typed domain snapshot.

Stories:

PASS — path, fields, IDs, array/envelope forms, validation, and missing-file
behavior are preserved.

Paths:

UNCHANGED

Permissions:

UNCHANGED (`0700` directories and `0600` private files).

# Root compatibility

Profile CLI:

Retained in root; load/save calls delegate to `CandidateProfileStore`.

Import:

Retained in root, including backup-before-replacement and merge orchestration.

Bootstrap:

Retained in root.

Backend composition:

Retained in root; JSON selection constructs the extracted reader and PostgreSQL
selection is unchanged.

# Duplicate audit

CandidateProfile struct:

One structural definition in `internal/candidate`; root alias only.

CandidateStory:

One narrative value definition in `internal/candidate`; root alias only. The
JSON envelope is owned by the JSON adapter.

Canonical mapper:

One implementation in `internal/candidate`; root converter only.

JSON CandidateReader:

One implementation in `internal/adapters/storage/json`; root alias only.

Expected duplicate implementations:

NONE.

# Dependencies

`internal/adapters/storage/json` → `internal/candidate`, `internal/ports`,
`internal/platform`, and `internal/usecase/candidateacquisition` for the
existing acquisition store. `internal/candidate` remains free of root,
storage, HH, AI, and Dashboard dependencies. No PostgreSQL files, SQL, or
migrations were changed for this stage.

# Tests

Adapter coverage includes profile round trip/strict failure/private
permissions, story round trip/malformed/missing behavior, JSON repository
assembly, source-priority/safety values, detached results, and parallel reads.
Existing root profile, canonical, relevant-knowledge, backend, mutation, and
acquisition tests remain in place.

# Size

Root Candidate persistence LOC before:

Approximately 2,388 lines in the pre-R6.2b audit grouping of profile, stories,
canonical input/mapper, and repository sections (the repository file also
contains unrelated repositories).

After:

1,012 lines across the root profile, stories, canonical compatibility, and
mapper files; the remaining root repository section is compatibility-only.

JSON adapter LOC:

525 lines across profile, story, repository, and existing knowledge storage
files. Domain-owned profile/story/assembly values and policy are outside this
adapter.

# Behavior

Candidate:

UNCHANGED

Reader:

UNCHANGED result; implementation owner changed.

Mutation:

UNCHANGED

Acquisition:

UNCHANGED

Postgres:

UNCHANGED

HH:

UNCHANGED; LIVE HH WRITES = 0.

Dashboard:

UNCHANGED

# Verification

gofmt:

PASS

go test:

PASS (`go test -count=1 ./...`)

race:

PASS (`go test -race ./...`)

vet:

PASS (`go vet ./...`)

build:

PASS (`go build ./...`)

diff:

PASS (`git diff --check`)

node:

PASS (`node --check web/app.js`)

Docker:

SKIPPED — Docker CLI was present but the daemon was unavailable.

LIVE HH WRITES:

0

# Candidate JSON boundary status

Knowledge storage:

EXTRACTED (R6.2a, reused)

Acquisition storage:

EXTRACTED (R6.2a, reused)

Profile storage:

EXTRACTED

Story storage:

EXTRACTED

Canonical CandidateReader:

EXTRACTED

Root persistence implementation:

COMPATIBILITY ONLY

# Ready for R6.3

R6.3 — Vacancy/Application/Conversation JSON Adapter Boundary

READY
