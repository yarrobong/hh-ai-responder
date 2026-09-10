# R6.2a — Candidate JSON Storage Boundary

## Before

Candidate JSON files were located beside the configured profile path:

- `candidate_profile.json` — legacy `CandidateProfile` aggregate;
- `candidate_skills.json`;
- `candidate_projects.json`;
- `candidate_achievements.json`;
- `candidate_unknowns.json`;
- `candidate_proposals.json`;
- `candidate_events.json`;
- `candidate_stories.json` — separate story persistence;
- `candidate_clarifications.json` — acquisition workflow persistence.

Knowledge collection persistence was embedded in root `CandidateKnowledgeBase`.
It loaded all collections fail-closed, validated values, encoded version-1
objects, staged all six files, then renamed each staged file in order. It was
deliberately not a multi-file transaction. The old implementation did not hold
the generic store lock around knowledge Save; callers controlled the broader
critical section where required.

Clarification persistence was embedded in `ai_reply_orchestrator.go`. It used
version-1 JSON, strict decoding, duplicate-ID validation, private atomic save,
and the local/process store lock. Acquisition policy and reconciliation were
root orchestration.

Candidate profile persistence remains in `candidate_profile.go`, together with
profile validation, merge/import, backups, bootstrap, and CLI. Candidate stories
remain root-owned and were not changed in this stage.

`JSONCandidateRepository` remains root-owned. It assembles `CandidateProfile`,
knowledge, and stories through `CanonicalCandidateInput` and the root canonical
mapper, so it is intentionally deferred to R6.2b.

## Classification

| Symbol | Storage responsibility | Decision |
|---|---|---|
| `CandidateKnowledgeBase.Load` / `Save` | Compatibility aggregate orchestration around profile plus knowledge collections | Kept as a thin root facade delegating collection I/O |
| `loadKnowledgeCollection` | Candidate collection file reads, strict JSON decode, validation | Moved to `jsonstorage.CandidateKnowledgeStore` |
| `encodeKnowledgeCollection` | Version-1 collection JSON serialization | Moved to `jsonstorage.CandidateKnowledgeStore` |
| six-file stage/rename sequence | Candidate Knowledge save mechanics | Moved unchanged to the adapter |
| `CandidateKnowledgeBase.Add*` / `MigrateLegacyProfile` | Candidate knowledge mutation and migration semantics | Kept in root compatibility orchestration |
| `CandidateClarificationStore` | Clarification JSON load/save, validation, cloning, ID creation, locking | Moved to `jsonstorage.CandidateClarificationStore` |
| `ReconcileCandidateClarifications` | Conversation/context reconciliation policy | Kept as a root function using adapter accessors |
| `CandidateClarificationRequest` | Acquisition value and JSON shape | Remains owned by `candidateacquisition`; no copy created |
| `LoadCandidateProfile` / `SaveCandidateProfile` / `ImportCandidateProfile` | Legacy profile serialization and profile workflow | Deferred; remains root-owned |
| `JSONCandidateRepository` | Canonical profile/knowledge/story assembly | Deferred to R6.2b |
| `CandidateWriter` / `CandidateMutationWriter` | Canonical persistence capabilities | Not implemented by JSON |

## New adapter

Package: `internal/adapters/storage/json`, package name `jsonstorage`.

Production files:

- `candidate_knowledge.go` — six collection load/save, validation boundary,
  JSON encoding, staged save, IDs, and storage cloning;
- `candidate_acquisition.go` — clarification JSON persistence, strict decode,
  duplicate validation, evidence/lifecycle value storage, and private locking.

The adapter imports only the Candidate domain, platform helpers, ports, and
acquisition value package. It does not import `package main`, PostgreSQL/pgx,
HH, AI, dashboard, vacancy/application/conversation, or candidatemutation.
The acquisition package has transitional transitive dependencies documented by
R6.1; no new cycle was introduced.

## Knowledge storage

| File | Domain value | Load/save owner after R6.2a | Special semantics |
|---|---|---|---|
| `candidate_skills.json` | `candidate.CandidateSkillDetailed` | `jsonstorage.CandidateKnowledgeStore` | version 1, strict fields, unique IDs |
| `candidate_projects.json` | `candidate.CandidateProject` | `jsonstorage.CandidateKnowledgeStore` | version 1, strict fields, unique IDs |
| `candidate_achievements.json` | `candidate.CandidateAchievement` | `jsonstorage.CandidateKnowledgeStore` | version 1, strict fields, unique IDs |
| `candidate_unknowns.json` | `candidate.CandidateUnknown` | `jsonstorage.CandidateKnowledgeStore` | lifecycle/provenance validation preserved |
| `candidate_proposals.json` | `candidate.KnowledgeProposal` | Adapter persistence plus root compatibility policy validation | no schema change; mutation-owned proposal payload rules remain root-owned |
| `candidate_events.json` | `candidate.CandidateKnowledgeEvent` | `jsonstorage.CandidateKnowledgeStore` | raw JSON snapshots and provenance preserved |

Missing files remain empty collections. Existing malformed files fail closed;
they are never silently reset. The profile file is loaded separately by the
root facade and is never rewritten by knowledge Save.

Paths, filenames, version, field names, `omitempty`, enum strings, nil/empty
behavior, ordering, timestamps, provenance, proposal values, and event values
are unchanged.

Save still performs:

1. validate and encode all six collections;
2. create private same-directory temporary files;
3. write and sync every temporary file;
4. rename staged files in collection order.

This preserves the existing guarantee that validation/staging failures occur
before replacement, while preserving the existing limitation that an OS failure
during the rename sequence may leave a partial multi-file save. No ACID claim
is made.

Directories created by the adapter use `0700`; temporary/replaced files use
`0600`. Existing directory mode behavior is unchanged when the directory
already exists.

## Acquisition storage

Implemented port: `ports.CandidateAcquisitionStore` by
`jsonstorage.CandidateClarificationStore`.

The existing contract remains unchanged: `Get`, `List`, `Create`,
`RecordAnswerEvidence`, `SetProposalIDs`, and `MarkResolved`. Existing
compatibility methods (`Save`, `Load`, `SetStatus`, `RecordAnswer`) remain on
the adapter. Root constructors are aliases/delegations to the adapter.

Clarification identity, existing request reuse, answer evidence retention,
proposal linkage, resolution lifecycle, strict status validation, and duplicate
IDs are preserved. Deduplication policy remains in the acquisition workflow;
the adapter rejects duplicate persisted IDs and duplicate explicit IDs.

The adapter stores acquisition values only. It does not interpret AI output,
promote truth status, or decide confirmation.

## Candidate profile

DEFERRED.

`CandidateProfile` persistence is still coupled to profile validation, source
trust rules, merge/import and backups, interactive bootstrap, and profile CLI.
Moving only a serializer now would either duplicate the profile type or drag
workflow/CLI into the adapter. It remains scheduled for a later boundary.

## Canonical CandidateReader

DEFERRED TO R6.2b.

`JSONCandidateRepository` still depends on root-owned `CandidateProfile`,
`CandidateKnowledgeBase`, `CandidateStory`, `CanonicalCandidateInput`, and the
canonical mapper. Moving it now would require moving the assembler and legacy
compatibility model together. No callback or `any` workaround was introduced.

## Root compatibility

- `NewCandidateKnowledgeBase` and its public methods remain compatible;
  `Load`/`Save` delegate to `jsonstorage.CandidateKnowledgeStore`.
- `NewCandidateClarificationStore` and `NewClarificationStore` delegate to the
  adapter through compatibility aliases.
- Root orchestration reads clarification snapshots through `List` and uses
  `Path` for diagnostics/reload planning.
- Root reconciliation remains business orchestration and uses adapter
  `Replace`; it is not part of the storage adapter.
- The shared process/file lock helper is centralized in `internal/platform`;
  existing root stores continue to use the same lock semantics.

Duplicate storage implementation: NONE for knowledge collection or
clarification load/save. Root code retains only compatibility delegation and
business/mutation orchestration. Profile and story storage are separate,
intentionally deferred contracts.

## Port compliance

| Port | JSON status |
|---|---|
| `CandidateReader` | Remains implemented by root `JSONCandidateRepository`; deferred as a complete move |
| `CandidateAcquisitionStore` | Implemented by `jsonstorage.CandidateClarificationStore` |
| `CandidateWriter` | NOT IMPLEMENTED BY JSON |
| `CandidateMutationWriter` | NOT IMPLEMENTED BY JSON |

The compile-time acquisition assertion now lives in the adapter package. The
root assertions for PostgreSQL and the compatibility repository remain.

## Atomicity / concurrency

Knowledge save staging and rename order are unchanged and intentionally not
transactional. Clarification Save retains the process mutex plus filesystem
lock and private atomic-file replacement. The generic lock implementation is
now shared by `internal/platform.WithPrivateFileLock`; lock scope for existing
root stores is unchanged.

No new backend fallback, dual-write, or PostgreSQL behavior was added.

## JSON compatibility

Paths: UNCHANGED.

Schemas: UNCHANGED.

Permissions: UNCHANGED (`0700` directories where created, `0600` private
files).

Existing files readable: PASS in root contract tests and adapter round-trip
tests.

## Tests

Adapter-local tests in `internal/adapters/storage/json` cover:

- all six collection round trips;
- private directory/file permissions;
- corrupt JSON fail-closed behavior;
- validation failure preserving existing files and cleaning staged files;
- rename failure preserving the established partial-save semantics and cleanup;
- clarification round trip, duplicate IDs, answer evidence, and permissions.

Existing root Candidate storage, profile, acquisition, mutation, context, and
backend tests remain in place. No real private candidate files or HH writes are
used by the tests.

## Size

At the R6.2a audit point, the low-level knowledge store was 453 root lines and
the clarification persistence block was approximately 247 root lines. After
the extraction, the adapter owns 521 production lines (254 knowledge, 267
acquisition). The root knowledge file remains 324 lines because it still owns
legacy aggregate compatibility, IDs, cloning used by broader orchestration,
mutation/event append behavior, migration, and profile joining. Profile and
canonical repository LOC remain root-owned by design.

The metric is ownership, not maximum line movement: file I/O, JSON collection
encoding/decoding, clarification persistence, and private locking now have an
adapter owner.

## Behavior

Candidate: UNCHANGED.

Truth/provenance: UNCHANGED.

CandidateContext: UNCHANGED.

CandidateMutation: UNCHANGED.

CandidateAcquisition: UNCHANGED.

Profile CLI: UNCHANGED.

JSON paths/schemas: UNCHANGED.

Permissions and staged multi-file behavior: UNCHANGED.

Backend selection: UNCHANGED.

PostgreSQL: UNCHANGED.

HH writes: 0.

Dashboard, conversation, vacancy/application, AI, and semantic behavior:
UNCHANGED.

## Verification

The final status below is filled from the R6.2a verification run.

- `gofmt`: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- focused Candidate/adapter/ports/domain/use-case tests: PASS
- `go list ./...`: PASS
- dependency graph audit: PASS
- Docker: SKIPPED (Docker CLI present, daemon unavailable)
- LIVE HH WRITES: 0

## Architectural debt

- R6.2b must extract the canonical JSON repository/assembler without moving
  profile or workflow policy into storage.
- Proposal payload validation is still a root compatibility check because the
  mutation policy owns its detailed identity/provenance rules. The adapter
  persists proposal values but does not duplicate that policy.
- Candidate acquisition values remain use-case-owned as permitted by R6.1;
  no dependency inversion was attempted in this stage.
- Knowledge Save remains a staged multi-file operation rather than a true
  transaction, matching the established JSON contract.

## Ready for R6.2b

READY.

R6.2b — Candidate JSON Repository / Canonical Adapter can proceed from the
documented blockers above. R6.2a does not begin that work or any other storage
boundary extraction.
