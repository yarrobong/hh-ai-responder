# Stage R5.3a — Candidate Mutation Semantics Boundary

## Before

The JSON path loaded `CandidateKnowledgeBase` and let
`CandidateKnowledgeUpdater` validate provenance, construct metadata, create
proposals, resolve proposals/unknowns, and append events before saving the
collections.

The PostgreSQL path loaded the canonical `candidate.Candidate` inside
`PostgresCandidateStore.WithTx` and let `CandidateMutationService` perform a
parallel set of metadata, proposal, unknown, event, and stale-base decisions
before `PersistCandidateIfVersion` and commit. Semantic indexing remained a
post-commit operation.

The duplicated business rules were:

- truth/provenance metadata construction;
- proposal construction and pending/confirmed/rejected lifecycle;
- proposal base-value stale comparison;
- explicit confirmation metadata;
- unknown confirmation/dismissal lifecycle;
- logical event construction and validation.

The implementations were intended to be equivalent, but the duplication was
an architectural risk. Storage-specific differences were retained: JSON
collection/file atomic writes and legacy detailed values versus PostgreSQL
transactions, canonical claims, row versioning, and post-commit semantic
indexing.

## Mutation matrix

| Operation | JSON | PostgreSQL | Shared rule after R5.3a |
| --- | --- | --- | --- |
| Create/update skill | `CandidateKnowledgeUpdater.UpdateSkill` | `mutateCanonicalSkill` | `BuildMetadata`; proposal/truth decision remains adapter orchestration over the same policy |
| Create/update project | `UpdateProject` | `mutateCanonicalProject` | same provenance/truth and proposal construction |
| Create/update achievement | `UpdateAchievement` | `mutateCanonicalAchievement` | same provenance/truth and proposal construction |
| Create/update story | not supported by legacy JSON path | `mutateCanonicalStory` | same metadata/proposal/event policy; canonical story storage remains PG-specific |
| Create unknown | `UpdateUnknown` / unknown-source update | `mutateCanonicalUnknown` | unknown remains non-confirmed; shared metadata policy |
| Create proposal | updater staging | `createCanonicalProposal` | `BuildProposal` |
| Confirm proposal | `ConfirmKnowledge` | `resolveCanonicalProposal` | `ApplyProposalResolution`, `BuildConfirmedMetadata`, `CheckBaseValue` |
| Reject proposal | `RejectKnowledge` | `resolveCanonicalProposal` | `ApplyProposalResolution` |
| Confirm unknown | clarification/updater and service paths | `ResolveUnknown` / linked proposal path | `ResolveUnknown` |
| Dismiss unknown | updater and service paths | service path | `ResolveUnknown` |
| Canonical claim mutation | no canonical claim collection | `appendCanonicalClaim` | intentionally canonical-storage-specific; JSON retains its existing detailed collection semantics |
| Resume import | no live mutation through this stage | `ImportHHResumeFacts` | remains root import orchestration; no acquisition extraction moved |
| Candidate version write | legacy JSON has no canonical row version | PG adapter advances version once per transaction and repository checks it | `CheckExpectedVersion` is available for use-case preconditions; transaction conflict remains adapter-owned |
| Event append | JSON collection event | candidate event collection in canonical aggregate | `BuildEvent`; ID generation and durable append remain adapters |

## New boundary

Package: `internal/usecase/candidatemutation`

Files:

- `doc.go`
- `policy.go`
- `policy_test.go`

Dependency boundary:

- `internal/usecase/candidatemutation` imports only `internal/candidate` and the Go standard library;
- it does not import `main`, config, platform, application, conversation, vacancy, candidatecontext, PostgreSQL, pgx, HH, AI, or storage;
- no `internal/ports` package was created;
- no Candidate Acquisition, resolver, conversation-policy, JSON-storage, or PostgreSQL-repository extraction was started.

## Mutation results/plans

The policy returns domain values or logical decisions, not persistence
handles. `BuildProposal` returns a validated domain proposal,
`ProposalMetadata` removes confirmation markers before staging,
`ApplyProposalResolution` changes only a proposal lifecycle value,
`ResolveUnknown` returns the resolved domain unknown, and `BuildEvent` returns
an unpersisted domain event. IDs and timestamps are explicit inputs at the
boundary; random ID generation and durable writes remain root adapter work.

## Proposal lifecycle

Allowed transitions:

- `pending -> confirmed` only through `ConfirmProposal`;
- `pending -> rejected` only through `RejectProposal`;
- any already resolved proposal is denied;
- unsupported operations are denied.

Implementation owner: `candidatemutation.ApplyProposalResolution`.

Root duplicate: none for the transition decision. Root code still performs
entity-specific materialization and persistence orchestration.

## Unknown lifecycle

`candidatemutation.ResolveUnknown` owns the transition from
`needs_confirmation` to `confirmed`, `rejected`, `dismissed`, or
`superseded`. Resolved unknowns cannot be resolved again. Confirmation requires
an explicit non-empty answer and produces `TruthStatusConfirmed`; dismissal,
rejection, and superseding remain `TruthStatusUnknown` with no `ConfirmedAt`.

## Truth/provenance

`BuildMetadata` is the shared truth boundary. AI/derived/project-analysis
input remains hypothesis; confidence never promotes truth. Only trusted user
actor input can create user-confirmed knowledge. HH and GitHub evidence remain
verified under their existing checks. Unknown entities always remain unknown
until the explicit resolution policy is invoked. Existing
`internal/candidate` enums and validation remain unchanged.

## Version/stale semantics

`CheckBaseValue` owns deterministic proposal snapshot comparison for both
adapters. `CheckExpectedVersion` distinguishes use-case stale preconditions
from PostgreSQL transaction/row conflicts. PostgreSQL still retains its
transactional `PersistCandidateIfVersion` check; JSON still retains its caller
serialization and staged multi-file save behavior.

## Events

The logical mapping is unchanged:

`mutation action + entity + before/after snapshots + provenance + actor`
`-> CandidateKnowledgeEvent`.

`BuildEvent` validates this value without writing it. JSON and PostgreSQL root
adapters supply the existing event IDs, timestamps, references, and durable
append mechanisms. Failed policy operations do not create successful events.

## JSON adapter

Responsibilities after R5.3a:

- lock/load the existing JSON knowledge base;
- prepare compatibility inputs and generated IDs;
- call shared metadata, proposal, stale, unknown, and event policy;
- retain existing in-memory transaction clone and staged multi-file save;
- return compatibility results.

Remaining root code is detailed-value identity matching and JSON collection
orchestration, not an independent transition/truth algorithm.

## PostgreSQL adapter

Responsibilities after R5.3a:

- own the existing transaction and canonical aggregate load;
- prepare canonical entity values and generated IDs;
- call shared metadata, proposal, stale, unknown, and event policy;
- persist canonical claims and aggregate state using the existing repository;
- commit before triggering semantic indexing.

Canonical claim supersede/dispute bookkeeping remains storage-model-specific
because the legacy JSON path has no equivalent canonical claim collection.

## Semantic indexing

Trigger behavior is unchanged: PostgreSQL mutation commits first, then the
optional semantic reindex runs. An indexing/read-back failure leaves the
mutation committed and reports a semantic-index warning. No embeddings or
semantic repository code entered `candidatemutation`.

## Backend parity

Shared semantics now cover provenance/truth, proposal creation and lifecycle,
proposal confirmation metadata, proposal stale comparison, unknown
resolution, and event value validation.

Remaining intentional storage-specific differences:

- JSON detailed collections and staged multi-file atomic replacement;
- PostgreSQL canonical aggregate, claims, transaction, row-version check, and
  post-commit semantic indexing;
- canonical stories and resume import exist only in the current PostgreSQL
  mutation path.

No silent fallback or dual write was introduced.

## Relevant knowledge regression

`RelevantKnowledgeSnapshot`, approval hashes, preflight, and HH write code
were not changed. Existing relevant-knowledge and write-safety tests remain
the regression authority. No live HH write was performed.

## Clarification flow regression

Clarification answers still flow through the existing updater/service,
proposal confirmation remains explicit, dismissal does not create a negative
fact, and canonical reconciliation/Dashboard behavior remains in root code.
Acquisition extraction and orchestration were not moved.

## Tests

- Pure policy: proposal transitions, unknown transitions, truth safety,
  confidence independence, stale/base checks, event validation, fixed time,
  and explicit confirmation tests in `internal/usecase/candidatemutation`.
- JSON: existing updater, acquisition, clarification, storage, and lifecycle
  tests.
- PostgreSQL: existing repository/canonical mutation tests; integration tests
  retain their existing environment-dependent skip behavior.
- Acquisition and HH write safety: existing root regression suites.

## Verification

- `gofmt -w .`: PASS for changed Go files;
- `go test -count=1 -p 1 ./...`: PASS;
- `go test -race -p 1 ./...`: PASS;
- `go vet ./...`: PASS;
- `go build ./...`: PASS;
- `git diff --check`: PASS;
- `node --check web/app.js`: PASS;
- Docker: SKIPPED — Docker daemon unavailable;
- `go list -deps ./internal/usecase/candidatemutation`: project dependency is only `internal/candidate`;
- `go list -deps ./internal/candidate`: project dependency is stdlib-only;
- LIVE HH WRITES: `0`.

## Behavior

Candidate model, truth, provenance, proposal semantics, unknown semantics,
versioning, events, acquisition, resolution, conversation policy, vacancy,
application, AI, HH read/write safety, Dashboard, CLI, and configuration
behavior remain unchanged. The change consolidates policy ownership and adds
boundary coverage.

## Architectural debt

- Entity-specific mutation orchestration remains in root `main` while the
  later storage/ports stage is intentionally deferred.
- Canonical claim mutation has no JSON-equivalent shared adapter yet.
- Existing JSON multi-file save is staged/atomic per file, not a true
  multi-file transaction; this stage deliberately preserves that behavior.
- Candidate Acquisition remains root-owned for R5.3b.

## Ready for R5.3b

READY.

- proposal transition policy has one implementation;
- unknown transition policy has one implementation;
- truth/provenance mutation semantics have one implementation;
- JSON and PostgreSQL no longer contain independent copies of those rules.

R5.3a stops here. Candidate Acquisition extraction, semantic extraction,
ports/repository migration, and other later boundaries are not started.
