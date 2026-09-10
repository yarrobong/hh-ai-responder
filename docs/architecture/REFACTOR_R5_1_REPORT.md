# Before

Candidate model locations:

- `candidate_profile.go` contained the legacy profile value vocabulary plus file I/O, merge/import, CLI, and bootstrap behavior.
- `candidate_knowledge.go` and `candidate_knowledge_proposal.go` contained knowledge values, truth/provenance metadata, and proposal validation.
- `candidate_canonical.go` and `candidate_canonical_mapper.go` contained the canonical aggregate and pure mapping/projection rules.
- `candidate_knowledge_projection.go` and `candidate_knowledge_view.go` contained employer-safe read projections alongside knowledge-store methods.
- `candidate_facts.go` mixed presence-aware resume values with HH parsing.

Canonical model:

The canonical aggregate already present in the repository was characterized and a dependency-free domain representation was established in `internal/candidate` with the existing JSON vocabulary. Root mapper/storage/resolver code remains compatibility code for this stage.

Knowledge model:

Knowledge entity values, metadata, source records, proposal values, and audit-event values are represented in `internal/candidate`. Storage collections and lifecycle methods remain in the root package.

Truth model:

The actual persisted truth vocabulary is `confirmed`, `verified`, `hypothesis`, and `unknown`. Candidate unknown records additionally use `needs_confirmation`, `confirmed`, `rejected`, `dismissed`, and `superseded` as lifecycle statuses. There was no separate persisted `restricted` or `pending` truth enum, so none was invented.

Provenance:

The profile source order is unchanged: `user_confirmed > hh_resume > github_verified > derived`; unknown/unranked sources do not promote a value. Knowledge source values retain their existing transport-independent strings.

Safe projection APIs:

`internal/candidate.CanonicalEmployerSafeProjection`, `CanExposeToEmployer`, and `CanExposeProfileFact` are pure domain APIs. Root `GetEmployerSafeKnowledge`, `GetEmployerSafeCandidateKnowledge`, and context resolver APIs remain compatibility/workflow APIs.

Storage mixing:

Filesystem, JSON stores, PostgreSQL repositories, mutation, acquisition, HH parsing, AI, semantic retrieval, CLI, and dashboard code remain outside `internal/candidate`.

# Classification

| Symbol | Classification | Decision |
| --- | --- | --- |
| `Candidate`, canonical value structs | CORE DOMAIN MODEL | Added to `internal/candidate`; root canonical implementation remains staged compatibility code. |
| `CandidateSource`, `SkillLevel`, profile fact/value structs | CORE DOMAIN MODEL / TRUTH/PROVENANCE | Added as dependency-free domain values; legacy aggregate and I/O remain root. |
| `KnowledgeSource`, `TruthStatus`, metadata/source records | TRUTH/PROVENANCE | Added with exact persisted strings and separate confidence. |
| `CandidateSkillDetailed`, `CandidateProject`, `CandidateAchievement`, `CandidateUnknown` | CORE DOMAIN MODEL | Added with intrinsic validation and JSON tags preserved. |
| `KnowledgeProposal` | SAFE PROJECTION VALUE / DEFER | Pure proposal value added; proposal creation/confirmation/rejection workflow remains root. |
| `CandidateKnowledgeEvent` | CORE DOMAIN MODEL | Pure audit value added; event writing remains root. |
| `CanonicalEmployerSafeProjection` | SAFE PROJECTION VALUE | Added as deterministic pure projection. |
| `CompareSourcePriority`, `CompareKnowledgeSourcePriority` | PURE NORMALIZATION | Explicit deterministic precedence; no I/O. |
| profile merge/skill normalization | PURE NORMALIZATION | Canonical skill/name normalization is delegated to the domain package; storage/import methods remain root. |
| `CandidateKnowledgeBase`, JSON stores | STORAGE | Deferred. |
| PostgreSQL repositories/backend selection | POSTGRES / DEFER | Deferred unchanged. |
| `CandidateContextResolver`, `candidate_fact_resolver.go` | RESOLUTION POLICY | Deferred unchanged. |
| `candidate_knowledge_updater.go`, `candidate_mutation.go` | MUTATION | Deferred unchanged. |
| `candidate_knowledge_acquisition.go` | ACQUISITION | Deferred unchanged. |
| `candidate_facts.go` HH parsing | HH | Deferred; only the presence-aware `ResumeFacts` value is represented in the domain package. |
| semantic providers/retrieval | SEMANTIC | Deferred unchanged. |
| vacancy/conversation/application consumers | DEFER | No domain dependency was introduced. |
| CLI, AI, dashboard | CLI / AI / DEFER | Deferred unchanged. |

# Extraction

Package: `internal/candidate`

Files:

- `doc.go`
- `profile.go`
- `knowledge.go`
- `proposal.go`
- `canonical.go`
- `projection.go`
- `model_test.go`

Types moved/established:

Candidate aggregate/value models, profile fact values, truth/provenance metadata, knowledge entities, proposal value, event value, and presence-aware `ResumeFacts`. Profile fact values and `ResumeFacts` are wired into root compatibility code; canonical/knowledge aggregate ownership remains staged for the later adapter boundary.

Enums/constants:

Profile source, skill level, knowledge source, truth status, project type, unknown status, proposal status, canonical claim and skill-use vocabularies.

Methods/rules:

Intrinsic validation, exposure predicates, explicit source ranking, canonical skill-name normalization, and detached employer-safe canonical projection.

# Canonical Candidate

New domain representation: `internal/candidate.Candidate` and canonical value types.

Compatibility: existing root mapper, repositories, and consumers remain operational for R5.1. No persisted data or backend selection was changed.

Compatibility debt: root canonical and knowledge definitions remain in `main` because current methods and stores are still coupled to deferred workflows. A follow-up should replace those definitions with aliases once the resolver/mutation adapter boundary is ready; no persisted schema is changed here.

# Truth and provenance

Truth states: `confirmed`, `verified`, `hypothesis`, `unknown`.

Source states: all existing profile and knowledge source strings.

Priority: `user_confirmed > hh_resume > github_verified > derived`; other sources rank zero.

Confidence semantics: nullable and bounded to `[0,1]`; confidence does not change truth status.

Tests: source-order, high-confidence-derived, confirmed-safe, unknown-vs-zero, and invalid/duplicate canonical cases are covered in `internal/candidate/model_test.go`.

# Employer-safe projection

Canonical pure API: `candidate.CanonicalEmployerSafeProjection`.

Legacy wrappers: root knowledge-base projection methods remain unchanged for current consumers.

Context-dependent behavior deferred: vacancy-specific answering, employer-question interpretation, conversation context, AI prompts, and resolver orchestration.

# Deferred Candidate subsystems

Resolver: root.

Mutation: root.

Acquisition: root.

Semantic: root.

HH resume: root.

Storage/Postgres/AI: root and existing adapters.

# Root compatibility

Aliases: profile fact/value types and `ResumeFacts` now alias `internal/candidate`; canonical and knowledge aliases are deferred with the root compatibility aggregate.

Wrappers: root canonical behavior and consumers remain available; root skill-name normalization delegates to `internal/candidate`.

Duplicate implementation: pure canonical/projection behavior remains in root for current consumers and is mirrored by the new standalone domain API; consolidation is explicitly deferred and recorded as debt.

# JSON compatibility

Profile: PASS (no profile file rewritten).

Knowledge: PASS (domain values preserve existing tags and enum strings).

Canonical: PASS (domain model retains current tags and nil/empty shapes).

Enum strings: PASS.

Existing storage: PASS (stores/backends untouched).

# Dependencies

`go list ./...`: includes `hh-ai-responder/internal/candidate`.

`go list -deps ./internal/candidate`: standard library only.

Project dependencies: NONE.

# Tests

Package: `go test ./internal/candidate/...` PASS.

Root boundary: `candidate_r51_boundary_test.go` PASS; synthetic profile → canonical → employer-safe projection, including 11 months, unknown Kubernetes, salary, and relocation semantics.

Existing root candidate, mutation, acquisition, backend, Postgres, semantic, conversation, application, vacancy, CLI, config, and platform tests remain the regression suite.

# Size

Root production LOC before: 39,940 (pre-R5.1 workspace audit).

Root production LOC after: 39,852.

`internal/candidate` production LOC: 1,075.

The root reduction is limited because compatibility methods and I/O are deliberately deferred; the new package is dependency-free and independently testable.

# Behavior

Candidate, truth, provenance, confidence, safe projection, unknown handling, acquisition, mutation, semantic retrieval, vacancy, application, conversation, AI, HH read/write, dashboard, JSON schema, PostgreSQL schema, CLI, and config: UNCHANGED.

LIVE HH WRITES: 0.

# Verification

`gofmt -w .`: PASS

`go test -count=1 ./...`: PASS

`go test -race ./...`: PASS

`go vet ./...`: PASS

`go build ./...`: PASS

`git diff --check`: PASS

`node --check web/app.js`: PASS

`go list ./...`: PASS

`go list -deps ./internal/candidate`: PASS; standard library only.

Docker build: SKIPPED; Docker CLI is installed but the daemon is unavailable.

# Architectural debt discovered

The root `main` package still owns compatibility aggregates and methods that combine pure values with I/O/HH-specific types. R5.2/R5.3 can replace these with aliases and adapters after resolver and mutation ports are defined. Projection API overlap remains intentionally preserved.

# Ready for R5.2

READY, subject to review of the staged root compatibility surface.

==================================================

STOP after R5.1.
