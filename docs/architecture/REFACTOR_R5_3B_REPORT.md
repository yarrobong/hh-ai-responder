# Before

Candidate acquisition was implemented in the root `main` package. The
historical file combined typed gap values, deterministic gap detection and
question templates with clarification persistence, AI extraction transport,
and JSON/PostgreSQL mutation adapters. Candidate resolution and mutation policy
were already delegated to `candidatecontext` and `candidatemutation`, but the
root workflow still selected answer branches and constructed backend-specific
mutation calls.

The flow remains:

```text
employer factual question
  -> CandidateContext resolution
  -> typed acquisition gap
  -> clarification persisted by root adapter
  -> explicit candidate answer
  -> optional root AI extraction adapter
  -> typed acquisition decision / mutation intent
  -> existing CandidateMutation adapter
  -> JSON or PostgreSQL persistence
```

Gap detection uses the prepared canonical Candidate snapshot. Vacancy data is
not loaded by acquisition. An unknown vacancy requirement alone does not create
a Candidate question.

The root clarification store remains the JSON persistence adapter. PostgreSQL
candidate persistence remains in the root repository and mutation service.
Migration `000005_candidate_acquisition` was not changed.

# Classification

| Symbol | Classification | Decision |
| --- | --- | --- |
| `CandidateKnowledgeGap` | DOMAIN VALUE | Moved to `candidateacquisition` with a root compatibility alias. |
| `CandidateClarificationRequest` and answer shape | ACQUISITION WORKFLOW VALUE | Moved to `candidateacquisition`; JSON store remains root. |
| deterministic gap key and deduplication | GAP DETECTION | Moved to `gaps.go`. |
| question templates | QUESTION POLICY | Moved to `gaps.go`. |
| answer classification | ANSWER INTERPRETATION | Moved to `answers.go`. |
| `CandidateKnowledgeInterpretation` | AI ADAPTER VALUE | Typed DTO is owned by acquisition; model transport remains root. |
| mutation intent | MUTATION POLICY OUTPUT | Returned by acquisition; execution remains the root adapter and `candidatemutation`. |
| unknown/proposal transition tables | MUTATION POLICY | Remain owned by `candidatemutation`. |
| clarification file I/O | JSON STORAGE | Remains root. |
| candidate SQL repository | POSTGRES STORAGE | Remains root. |
| embeddings and semantic indexing | SEMANTIC | Remains post-commit root behavior. |
| dashboard and employer conversation | DASHBOARD/API | Remain root. |

# New boundary

Package:

`internal/usecase/candidateacquisition`

Files:

- `doc.go` — package boundary and ownership;
- `types.go` — acquisition values, lifecycle states, evidence origins and typed mutation intents;
- `gaps.go` — deterministic gap keys, gap detection, deduplication and question generation;
- `answers.go` — answer validation/classification and AI interpretation trust checks;
- `reconcile.go` — pure acquisition-state reconciliation against a current Candidate snapshot;
- `workflow_test.go` — focused characterization and safety tests.

Dependencies:

```text
candidateacquisition
  -> internal/candidate
  -> internal/usecase/candidatecontext
  -> internal/usecase/candidatemutation
```

There are no storage, PostgreSQL, HH, AI HTTP, dashboard, conversation-policy,
filesystem, SQL, HTTP, or embedding dependencies.

# Acquisition workflow

Gap detection delegates answerability and fact status to `candidatecontext`.
`ANSWERABLE` facts are suppressed; `PARTIALLY_ANSWERABLE` and `UNKNOWN` facts
may become actionable gaps when the supplied employer question is factual.
Operational/high-risk topics such as salary, relocation, work mode and
availability stay outside this Candidate knowledge-gap workflow.

Deduplication uses stable typed inputs and the deterministic `gap-` key. A
resolved `CandidateUnknown` is not reopened by `FilterKnownGaps`; pending work
can be returned by the root adapter so an existing clarification is reused.

Question selection is deterministic and preserves the existing choice/free-text
answer shapes for skills, experience details and behavioral stories.

`ClassifyAnswer` distinguishes unknown, dismiss, structured choice and free
text. `DecideAnswer` converts structured explicit choices to a typed user
mutation intent, or converts a validated AI interpretation to an untrusted
proposal intent requiring user review.

`Reconcile` is pure. When current Candidate knowledge satisfies an active
clarification, it returns the existing `resolved_existing_knowledge` acquisition
status; it does not write storage or mutate Candidate data.

# Trust boundary

An explicit structured candidate choice is represented as a user-confirmed
mutation intent and may be sent to the existing mutation adapter. The raw free
text answer is retained by the root clarification store and is not replaced by
the model interpretation.

AI interpretation is represented as a typed proposal draft with
`OriginAIInterpretation`, actor `candidatemutation.ActorAI`, and
`RequiresUserReview: true`.

Can AI confirm:

**NO.**

Enforcement:

- acquisition rejects interpretation truth statuses other than `hypothesis`;
- AI interpretations produce proposal intents, never confirmed intents;
- the existing `candidatemutation` and root adapters retain final proposal and
  unknown transition validation;
- explicit confirmation remains a separate user action.

# CandidateContext integration

Acquisition calls `candidatecontext.NewResolver(candidate).Resolve` with a
prepared Candidate snapshot. It does not duplicate `ANSWERABLE`,
`PARTIALLY_ANSWERABLE`, `UNKNOWN`, or `RESTRICTED` semantics and does not infer
Kubernetes from Docker, Linux, or another unrelated fact.

# CandidateMutation integration

Acquisition returns intent metadata using `candidatemutation.Actor` and
`candidatemutation.UnknownResolution`. Existing root adapters execute those
intents through `CandidateMutationService` or `CandidateKnowledgeUpdater`.

Metadata construction, proposal construction, proposal transitions, unknown
transitions, event construction, stale/base comparison, and confirmation truth
promotion remain in `internal/usecase/candidatemutation` and the established
mutation adapters.

Duplicated mutation semantics:

NONE in the new package.

# JSON adapter

The root adapter continues to load and save the existing clarification and
Candidate Knowledge JSON files, preserving filenames, permissions, JSON field
names, enum strings, timestamps and deduplication keys. It converts acquisition
decisions to the existing JSON updater and explicitly saves after mutation.

# PostgreSQL adapter

The root adapter continues to load and persist canonical Candidate state using
the existing PostgreSQL repository and transaction boundaries. It converts
acquisition decisions to the existing `CandidateMutationService`; semantic
indexing remains an optional post-commit hook.

# Backend capabilities

JSON continues to support the existing skill, project, achievement and unknown
collections. Canonical story acquisition remains a PostgreSQL capability in
the existing adapter. The use case does not contain `if postgres` branches or
emulate unsupported JSON storage through another collection.

# Safety regressions

Unknown Kubernetes: PASS — no inference from Docker/Linux and an unknown gap
is produced only for a relevant factual question.

AI contradiction: PASS — an AI interpretation cannot produce a confirmed
intent and a claimed `confirmed` status is rejected.

Negative relocation: PASS — relocation remains in the existing preference /
constraint path; explicit negative skill choices remain valid confirmed facts.

11 months: PASS — acquisition does not normalize duration, and the focused
test verifies exact `11 месяцев` preservation through CandidateContext.

Duplicate gap: PASS — deterministic key deduplication and resolved-unknown
filtering are covered.

Duplicate answer: PASS — repeated identical input produces the same typed
decision; existing mutation stale/idempotency protections remain authoritative.

# Clarification flow

Dashboard/API: UNCHANGED.

Explicit confirmation: UNCHANGED.

Automatic confirmation: NONE.

# Dependencies

`go list ./...` includes the new package successfully.

`go list -deps ./internal/usecase/candidateacquisition` resolves only the
standard library plus:

- `internal/candidate`;
- `internal/usecase/candidatecontext`;
- `internal/usecase/candidatemutation`.

No forbidden root/storage/HH/dashboard dependency is present.

# Tests

Package:

`internal/usecase/candidateacquisition/workflow_test.go` covers partial and
unknown gaps, known-fact suppression, deduplication, resolved unknowns,
trusted explicit choices, ambiguity, AI non-confirmation, negative facts,
exact duration and reconciliation.

Root:

Existing Candidate Knowledge Acquisition, clarification, JSON updater,
dashboard, mutation, context, semantic and write-safety tests remain in place
and pass with the compatibility aliases/adapters.

Postgres:

No migration or SQL contract was changed. Existing environment-dependent
integration behavior remains unchanged.

Semantic:

No semantic indexing behavior or transaction boundary was changed.
