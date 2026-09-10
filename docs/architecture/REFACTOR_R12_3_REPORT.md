# Before

`CandidateKnowledgeAcquisitionService` in the root package was the effective
owner of the Candidate-learning sequence. It detected gaps, deduplicated
clarifications, accepted answers, selected the AI interpretation path, built
skill/story proposal values, routed JSON/PostgreSQL mutation calls with type
assertions, and resolved proposal/clarification lifecycle state.

The important pre-change call graph was:

```text
employer reply / follow-up / application answer
  -> root CandidateKnowledgeAcquisitionService.CreateClarification
  -> DetectCandidateKnowledgeGaps / FilterKnownGaps
  -> root mutation UpdateUnknown
  -> clarification store Create

dashboard / AI reply answer adapter
  -> root CandidateKnowledgeAcquisitionService.SubmitAnswer
  -> clarification store Get / ValidateAnswer / ClassifyAnswer
  -> clarification store RecordAnswerEvidence
  -> candidateinterpretation-compatible extractor (free text only)
  -> root applyProposal -> CandidateKnowledgeUpdater or CandidateMutationService

dashboard / CLI proposal action
  -> CandidateMutationService or CandidateKnowledgeUpdater
  -> candidatemutation policy + canonical persistence
```

Candidate truth was never supposed to be changed by AI interpretation. Free-text
answers were staged as hypothesis proposals. Structured choice answers were an
explicit candidate action and could resolve the corresponding unknown directly.

Clarifications use the persisted statuses `pending`, `answered`,
`dismissed`, and `resolved_existing_knowledge`. Pending clarification identity
is based on the deterministic gap key. A dismissed record can be recreated for
the same unresolved gap; a non-dismissed record is reused.

Candidate truth mutation remains backend-specific at the composition boundary:
the JSON updater performs its existing multi-file save sequence, while the
PostgreSQL mutation service performs its existing version-checked transaction.
The existing PostgreSQL mutation service may retain its pre-existing optional
post-commit semantic-index hook; R12.3 adds no indexing call.

# Lifecycle matrix

| Operation | Current owner after R12.3 | AI | Candidate truth mutation | Confirmation |
|---|---|---:|---:|---:|
| Gap -> clarification | `candidatelearningorchestration.Service` + `candidateacquisition` | No | Unknown record only | No |
| Answer validation/classification | `candidatelearningorchestration.Service` + `candidateacquisition` | No | No for free text; explicit choice uses mutation port | Choice is explicit |
| Free-text interpretation | `candidateinterpretation.Service` through `Interpreter` | Yes, typed leaf only | No | No |
| Interpretation -> proposal | Orchestration + mutation port | Interpretation already complete | No canonical truth | Yes, later |
| Proposal confirm | Orchestration + `candidatemutation`-backed mutation port | No | Yes | Explicit user action |
| Proposal reject | Orchestration + `candidatemutation`-backed mutation port | No | No | Explicit user action |
| Clarification dismiss | Orchestration + mutation port | No | Unknown lifecycle only | Explicit user action |
| Direct profile/resume mutation | Existing root/profile adapters | No | Yes where explicitly requested/imported | Existing policy |

# Characterization

| Case | Existing behavior | Preserved |
|---|---|---:|
| Known fact / no gap | No unnecessary clarification | Yes |
| Unknown required fact | One deterministic gap, unknown, and clarification | Yes |
| Repeated gap | Existing non-dismissed clarification reused | Yes |
| Valid free-text answer | Answer evidence recorded; typed interpretation may create pending proposal | Yes |
| Ambiguous/insufficient answer | Remains unknown and does not mutate Candidate truth | Yes |
| Interpretation/provider error | Error returned; no proposal confirmation | Yes |
| Malformed interpretation | Rejected by interpretation/acquisition validation | Yes |
| Structured choice | Explicit candidate choice mutates through mutation authority | Yes |
| Proposal creation | Pending hypothesis proposal only | Yes |
| Proposal confirmation | Canonical mutation then linked clarification resolution | Yes |
| Proposal rejection | Proposal lifecycle only; Candidate truth unchanged | Yes |
| Clarification dismissal | Unknown becomes dismissed/unknown; no negative skill is invented | Yes |
| Stale proposal | Existing base-value/version checks remain in mutation adapters | Yes |
| Double confirmation | Existing pending-status/atomic mutation policy rejects a resolved proposal | Yes |
| Mutation/persistence failure | Existing adapter error and crash-window behavior preserved | Yes |
| Context cancellation | Checked between orchestration phases and propagated to leaves | Yes |
| Semantic reindex | No orchestration call was added | Yes |

Existing regression coverage remains authoritative in
`candidate_knowledge_acquisition_test.go`, `candidate_knowledge_updater_test.go`,
`candidate_mutation_test.go`, dashboard tests, and the R12.2 workflow tests.
New pure tests in `internal/usecase/candidatelearningorchestration/service_test.go`
cover proposal separation, ambiguous answers, explicit confirmation/rejection,
and repeated-gap deduplication.

# Candidate learning orchestration

Package: `internal/usecase/candidatelearningorchestration`

Service: `candidatelearningorchestration.Service`

Methods:

- `CreateClarification`
- `SubmitAnswer`
- `ConfirmProposal`
- `RejectProposal`
- `DismissClarification`

Dependencies are narrow typed ports: a read-only Candidate snapshot reader,
clarification store, mutation authority, typed interpretation leaf, and an ID
generator. There are no concrete JSON/PostgreSQL imports, HH imports, semantic
embedding imports, prompt construction, raw completion calls, goroutines, or
generic operation/payload methods.

The root `CandidateKnowledgeAcquisitionService` is now a compatibility façade.
It translates the legacy root API to the importable service and adapts the
existing JSON/PostgreSQL mutation implementations.

# Gap / clarification

Policy owner: `internal/usecase/candidateacquisition`.

Identity: the deterministic `GapKey` produced from candidate, subject identity,
and field; persisted clarification records retain topic/question, conversation,
application, vacancy, and employer-message provenance where supplied.

Deduplication: list existing records by gap key; reuse every non-dismissed
record, including pending and answered/resolved records, and permit recreation
only after dismissal. Resolved CandidateUnknown values are filtered by the
acquisition policy.

The employer-reply clarification adapter continues to use the root façade,
which delegates into this service. Legacy records without typed gap metadata
continue through the existing root compatibility answer path.

# Interpretation

Owner: `internal/usecase/candidateinterpretation`.

Direct AI in orchestration: **NO**.

Truth mutation: **NO**. The interpreter returns the existing untrusted typed
interpretation. `candidateacquisition.DecideAnswer` validates that AI output is
proposal-only and retains `hypothesis` provenance.

# Proposal

Shape: the existing typed `CandidateKnowledgeProposalDraft` and persisted
`KnowledgeProposal` shape are unchanged: entity type/identity, proposed value,
reason, source, confidence, base value, source clarification, provenance, and
pending/confirmed/rejected status.

Persistence: proposal creation is delegated to the existing mutation adapter;
the orchestration service never writes a canonical Candidate value directly.

Status lifecycle: `pending` -> `confirmed` or `rejected`, enforced by the
existing `candidatemutation` policy.

Staleness: existing base-value and candidate-version checks remain in the JSON
updater and PostgreSQL mutation transaction. No new version or migration was
introduced.

# Confirmation

Explicit: **YES**.

Mutation owner: `candidatemutation` policy through the existing
`CandidateMutationService`/`CandidateKnowledgeUpdater` adapters.

Double-confirm: the existing proposal pending-state transition and atomic
candidate mutation reject a second confirmation; the orchestration service does
not auto-confirm based on confidence.

Failure behavior: proposal load, policy, stale-state, or mutation errors stop
before later lifecycle writes. Existing post-mutation persistence windows are
not widened or hidden by a new transaction abstraction.

# Candidate truth

Canonical authority: the existing Candidate canonical repository and mutation
services.

Provenance: source clarification, proposal, conversation/message references,
raw answer evidence, explicit user actor, and timestamps continue through the
existing mutation commands and events.

Unknown behavior: uncertainty remains unknown/needs confirmation. AI inference
is never recorded as user-confirmed truth.

# Semantic index

Automatic reindex after mutation: **NO new orchestration path**.

Existing explicit path: semantic indexing remains a separate
`CandidateSemanticIndexService.Reindex` capability. The orchestration package
does not import semantic ports or call it after confirmation. Any pre-existing
optional mutation-boundary hook is outside this extraction and unchanged.

Known debt: if Candidate mutation and semantic documents become out of date,
the repository still needs a future explicit reindex contract. R12.3 does not
choose between manual, event-driven, or outbox approaches.

# Persistence / atomicity

Clarification: existing clarification store methods remain the persistence
boundary for answer evidence, proposal IDs, and lifecycle state.

Proposal: existing updater/canonical mutation implementations retain proposal
validation, deduplication behavior, base-value checks, and event persistence.

Mutation: JSON and PostgreSQL composition remains outside the usecase package;
the orchestration layer does not branch on backend.

Post-mutation failure window: existing behavior is retained. In particular, a
canonical mutation can already commit before a subsequent clarification-store
update fails; no generic unit-of-work or migration was invented in this stage.

# Root compatibility

`CandidateKnowledgeAcquisitionService`: thin legacy façade and adapter.

Dashboard: proposal confirm/reject routes use a trusted-user façade; HTTP paths,
payloads, status codes, and error text remain unchanged.

CLI: JSON proposal confirmation/rejection uses the façade while preserving
output. PostgreSQL proposal commands remain direct explicit candidate mutation
adapter operations because their canonical proposal store is not the local
clarification store.

Conversation clarification callers: employer reply/follow-up continue to use
the root adapter, which delegates typed gap creation and deduplication.

# Direct candidatemutation callers

| Caller | Classification | Legitimate |
|---|---|---:|
| Candidate learning confirmation façade | Candidate learning confirmation | Yes |
| Dashboard/CLI explicit proposal actions | Explicit profile-management adapter | Yes |
| HH resume import | Trusted external import | Yes |
| Vacancy hard-requirement unknown creation | Other explicit acquisition/import path | Yes |
| Orchestration package direct canonical writes | None | N/A |

# R12.4 inventory

ApplyVacancies: not started.

Entry points: not extracted.

Selection: unchanged.

AI analysis: unchanged.

Cover letter: unchanged.

Test handling: unchanged.

Submission: unchanged.

Limits: unchanged.

Retry: unchanged.

# Tests

Pure orchestration fakes cover clarification deduplication, answer evidence,
interpretation, proposal creation without mutation, ambiguous input, explicit
confirm/reject, and lifecycle updates. Existing acquisition, interpretation,
mutation, context, dashboard, CLI, employer-reply, follow-up, inbox-refresh,
and legacy auto-chat coverage was retained.

# Dependencies

`go list -deps ./internal/usecase/candidatelearningorchestration` contains only
the domain, acquisition, interpretation, and foundational LLM value contracts
needed by the typed leaf. It does not depend on root, dashboard, CLI, concrete
storage, HH write, or semantic-index packages.

# Behavior

Gap detection: unchanged, delegated to `candidateacquisition`.

Clarification identity/dedupe: unchanged.

Interpretation: unchanged, delegated to `candidateinterpretation`.

Unknown handling: unchanged.

Proposal shape: unchanged.

Proposal confirmation: explicit.

Proposal rejection: unchanged.

Candidate mutation policy: unchanged, delegated to existing mutation adapters.

Candidate provenance: unchanged.

AI auto-mutation: none.

Semantic auto-reindex: not added.

Dashboard/CLI/storage shape: unchanged.

HH writes: none from this package or this stage.

# Verification

The focused and full `go test` suites pass after extraction. Final sequential
verification is recorded by the delivery turn; no live HH write was performed.

gofmt: PASS

go test: PASS

race: PASS (`go test -race ./...`)

vet: PASS (`go vet ./...`)

build: PASS (`go build ./...`)

diff: PASS (`git diff --check`)

node: PASS (`node --check web/app.js`)

Docker: SKIPPED; Docker CLI is present but the daemon is unavailable

LIVE HH WRITES: 0

# R12 status

R12.3: EXTRACTED

# Ready

R12.4 — Vacancy / Application Processing Orchestration READY after final
verification of this stage.

Remaining scope is intentionally deferred. Do not begin R12.4, R12.5, or R12.6
as part of this change.
