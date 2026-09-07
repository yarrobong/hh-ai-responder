# Repository / Storage Boundary

This stage introduces the smallest repository boundary needed before a future
PostgreSQL backend. The existing JSON stores remain the only writers and keep
their file names, JSON schemas, IDs, explicit `Save` behavior, and safety
semantics.

## Audit map

| Consumer | Previous persistence coupling | Boundary in this stage |
| --- | --- | --- |
| `CareerDataReconciler` | `VacancyStore`, `ApplicationStore`, `ConversationStore`, including direct application/event mutation | `VacancyRepository`, `ApplicationRepository`, `ConversationRepository` |
| Canonical candidate read path | `CandidateProfile`/legacy knowledge values assembled in the responder | `CandidateRepository` returning only canonical `Candidate` |
| HH read sync | Concrete stores and compatibility constructors | Deferred: its staged batch commit still requires concrete JSON stores |
| Dashboard/follow-up projections | Direct snapshot field reads | Store query methods; one copy-before-save follow-up compatibility path remains |
| AI drafts and clarifications | Concrete durable stores used by orchestration | Deferred; their current APIs are already narrow and changing them would expand this migration |
| Notifications, approved actions, HH write audit | Concrete safety stores | Deferred deliberately; no safety semantics are changed in this stage |

## Interfaces and JSON implementations

`repositories.go` defines typed `CandidateRepository`, `VacancyRepository`,
`ApplicationRepository`, and `ConversationRepository` interfaces. Repository
operations accept `context.Context`, use typed vacancy queries, and preserve
the existing domain structs rather than introducing duplicate persistence DTOs.

`JSONVacancyRepository`, `JSONApplicationRepository`, and
`JSONConversationRepository` are thin adapters over the existing stores.
`JSONCandidateRepository` loads the legacy profile, knowledge collections,
stories, and configured candidate inputs into one snapshot and calls
`BuildCanonicalCandidate`. It never returns `CandidateProfile` or
`CandidateKnowledgeBase` to business logic.

Application event appends remain explicit. Conversation writes continue to use
`Upsert` plus `AppendMessage`; duplicate external messages remain idempotent
and conflicting replays remain rejected.

## Direct store field access

All direct collection access introduced by the repository migration was removed
from reconciliation and read-only dashboard projections. The remaining
production accesses are intentional:

* JSON store implementations themselves maintain their private collections.
* `hh_sync_performance.go` clones and publishes concrete stores for the
  existing staged HH read batch. This is the current compatibility transaction
  mechanism and is not exposed as a repository API.
* `follow_up_service.go` makes a detached copy before saving a dismissal so a
  failed file write does not alter the live snapshot. This remains until the
  application repository gains an explicit atomic update contract.
* A few constructors still retain concrete stores for compatibility with the
  existing CLI and write gateway.

No `...any` repository dependency or generic `Find(map[string]any)` query was
added.

## Future PostgreSQL transaction boundaries

The JSON backend does not expose a fake transaction abstraction. PostgreSQL
transactions will be needed at these application boundaries:

1. Candidate knowledge mutation: candidate/profile entity, knowledge claim,
   knowledge event, and unknown/proposal state.
2. HH read import: vacancy, application, conversation, and relation updates
   from one sync batch.
3. HH confirmed send: approved action nonce/delivery state, write audit event,
   and local application/conversation state.

## PostgreSQL readiness

| Repository | Readiness | Remaining blocker |
| --- | --- | --- |
| `CandidateRepository` | ready | Candidate mutation/write boundary is intentionally deferred |
| `VacancyRepository` | ready | SQL uniqueness and transaction implementation remain |
| `ApplicationRepository` | minor changes needed | Preserve append-only events and add transaction-aware batch operations |
| `ConversationRepository` | minor changes needed | Preserve immutable message history and idempotent external IDs in SQL constraints |

