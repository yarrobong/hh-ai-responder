# R4.3a Conversation Model Boundary

## Result

`internal/conversation` now owns the persisted employer-conversation
aggregate, normalized message model, persisted summary/claim values, status
and lifecycle values, intrinsic validation, activity projection, and the
existing pure message identity comparison.

The package has no project-package dependencies. It imports only the Go
standard library.

Storage, PostgreSQL, HH import/sync, AI/context construction, candidate
knowledge, eligibility/state resolution, follow-up policy, reconciliation,
career workflow, and Dashboard consumers remain in the root package.

No HH write path was invoked. `HH_WRITE_ENABLED=false` and
`HH_DRY_RUN=true` remain unchanged.

## Pre-change audit and classification

The direct aggregate dependency graph was:

```text
EmployerConversation
  -> ConversationStatus
  -> ConversationMessage[]
       -> sender/source/direction values
       -> external identity, timestamp, text, service-event flags, metadata
  -> ConversationSummary
       -> CandidateConversationClaim[]
            -> ConversationExperienceClaim?
  -> application/vacancy relation IDs
  -> persisted status, next action, waiting/activity timestamps,
     follow-up marker, raw HH status, HH metadata
```

The application relationship is represented by `ApplicationID`; the vacancy
relationship remains `VacancyID`. No application or vacancy aggregate is
embedded in the conversation package.

| Symbol | Current file | Model owned | Message value | Claim value | Workflow/policy | Follow-up | Candidate-dependent | AI | Storage | HH | Reconciliation | Defer | Decision |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `EmployerConversation` | `employer_conversation.go` | yes | — | — | persisted fields only | persisted marker only | no | no | no | no | no | — | moved |
| `ConversationMessage` | `employer_conversation.go` | yes | yes | — | no | no | no | no | no | normalized destination only | no | — | moved |
| sender/source/direction enums | `employer_conversation.go` | yes | yes | — | no | no | no | no | no | HH mapping stays root | no | — | moved |
| `ConversationSummary` | `employer_conversation.go` | yes | — | contains claim values | no generation logic | no | untrusted annotations only | generation stays root | no | no | no | — | moved |
| `CandidateConversationClaim` | `employer_conversation.go` | yes | — | yes | no truth decision | no | no candidate import | no | no | no | consistency stays root | — | moved |
| `ConversationExperienceClaim` | `employer_conversation.go` | yes | — | yes | no | no | no candidate import | no | no | no | no | — | moved |
| `ConversationStatus` values | `employer_conversation.go` / `conversation_state_resolver.go` | persisted value only | — | — | derived classification stays root | no | no | no | no | no | no | `manual_review` remains derived root value | moved persisted values |
| `ConversationFollowUpState` | `employer_conversation.go` / `internal/application/model.go` | persisted shared value | — | — | no policy | policy stays root | no | no | no | no | no | later career ownership not needed for this value | moved to conversation; application aliases it |
| `ConversationState` | `employer_conversation.go` | yes as update value | — | — | application of state stays root | marker only | no | no | no | no | no | transition decisions | moved |
| `Validate` methods | `employer_conversation.go` | intrinsic invariants | message validation | claim linkage/shape | no eligibility | no | no candidate resolution | no | no | no | no | — | moved |
| `RefreshActivity` | `employer_conversation.go` | pure aggregate projection | reads messages | no | no workflow decision | no | no | no | no | no | no | — | moved |
| message identity comparison | `conversation_store.go` / PostgreSQL | pure value identity | yes | — | no merge policy | no | no | no | callers remain root | no | merge policy stays root | — | moved as `SameMessage`; root wrapper retained |
| `ConversationConsistencyWarning` | `conversation_consistency.go` | no | — | — | consistency policy | no | yes | no | no | no | yes | R4.3b/later | remains root |
| `ConversationStore` | `conversation_store.go` | no | — | — | no | no | no | no | yes | no | no | later storage boundary | remains root |
| context builder / AI orchestrator | `conversation_context.go`, `ai_reply_orchestrator.go` | no | consumes model | consumes annotations | policy/use case | no | yes | yes | no | no | no | later use-case boundary | remains root |
| state resolver / eligibility | `conversation_state_resolver.go`, `conversation_eligibility.go` | no | consumes model | consumes annotations | yes | no | yes | no | no | no | no | R4.3b | remains root |
| follow-up engine/service/analytics | `follow_up.go`, `follow_up_service.go`, `follow_up_analytics.go` | no | consumes model | consumes annotations | yes | yes | yes | may call AI | no | no | no | later career/follow-up stage | remains root |
| HH import and sender mapping | `hh_read_sync.go`, `hh_read_validation.go` | no | creates normalized messages | no | no | no | no | no | no | yes | no | future HH adapter | remains root |
| PostgreSQL repository | `postgres_application_conversation_repository.go` | no | persists model | persists claims | no | no | no | no | yes | no | no | later storage adapter | remains root |

## Follow-up state ownership decision

`FollowUpState` is persisted on both `JobApplication` and
`EmployerConversation`, and it is a plain lifecycle value rather than a
follow-up eligibility decision. It now lives in `internal/conversation`.

`internal/application` retains `FollowUpState` and its constants only as type
and constant aliases for source compatibility. The dependency is:

```text
internal/application -> internal/conversation
internal/conversation -> standard library only
```

There is no cycle. Follow-up policy, timing, dismissal behavior, suggestions,
analytics, and orchestration remain root-owned.

## Extraction inventory

Moved production types:

- `conversation.EmployerConversation`;
- `conversation.Message`;
- `conversation.Summary`;
- `conversation.Claim` and `conversation.ExperienceClaim`;
- `conversation.Status`, `Sender`, `Source`, `Direction`;
- `conversation.FollowUpState` and `conversation.State`.

Moved pure behavior:

- message intrinsic validation;
- aggregate intrinsic validation, including claim-to-message linkage;
- activity timestamp refresh;
- the existing message identity comparison semantics.

No policy, repository, SQL, migration, HH DTO, sender parser, AI prompt, or
candidate resolver moved.

## Compatibility and JSON contract

`employer_conversation.go` now contains type aliases and constant aliases for
the old root names. The only root function wrappers are:

- `sameConversationMessage` → `conversation.SameMessage`;
- `sameConversationTime` → `conversation.SameTime`.

There are no copied structs, duplicated enum definitions, or copied business
policy. JSON field names, tags, enum strings, timestamps, nil/empty behavior,
message ordering, claims, summary, status, external IDs, relation IDs, and HH
metadata remain unchanged. No schema version, filename, migration, or local
JSON rewrite was introduced.

Characterization coverage is in `internal/conversation/model_test.go` and
covers full aggregate/message round trips, the zero value, external and
relation IDs, status/raw status, messages, claims, warning metadata,
timestamps, service/system events, unknown/content-unavailable messages, and
existing identity semantics.

The root `conversation_storage_contract_test.go` remains in place and still
covers synthetic store save/reload and immutable message history. The added
`conversation_boundary_test.go` covers application/conversation relation IDs
through the existing root stores after reload.

## Package and dependency verification

Expected packages are now present:

```text
hh-ai-responder
hh-ai-responder/internal/application
hh-ai-responder/internal/cli
hh-ai-responder/internal/config
hh-ai-responder/internal/conversation
hh-ai-responder/internal/platform
hh-ai-responder/internal/vacancy
```

`go list -deps ./internal/conversation` reports only:

```text
hh-ai-responder/internal/conversation
```

No root, application, vacancy, platform, configuration, CLI, HH, AI,
Dashboard, storage, or PostgreSQL package is imported by the model package.

## Size snapshot

| Measure | Before | After |
| --- | ---: | ---: |
| Root production LOC | 40,138 | 39,945 |
| `internal/conversation` production LOC | 0 | 318 |
| `EmployerConversation` fields | 21 | 21 |

The before root count is the R4.2 audit snapshot; the after count excludes
tests. The package test file adds 132 test LOC and is excluded from the
production count.

Moved value/model types: 11. Moved aggregate/model behaviors: 4 public
operations (`Message.Validate`, `EmployerConversation.Validate`,
`EmployerConversation.RefreshActivity`, `SameMessage`) plus private helpers.
Moved persisted constants: 28 across status, sender, source, direction, and
follow-up values (8 status + 4 sender + 4 source + 3 direction + 5
follow-up). Root compatibility aliases/wrappers are listed above.

## Business-change verification

The following remain unchanged in behavior and ownership:

- conversation persisted model and message model;
- message deduplication and immutable-history checks;
- conversation classification and reply eligibility;
- follow-up policy, timing, suggestions, dismissals, and analytics;
- candidate, application, and vacancy behavior;
- AI, HH read/import, Dashboard, storage, PostgreSQL, CLI, and config;
- HH write safety and dry-run gating.

PostgreSQL conversation repository tests were retained and run. No schema
changes or migrations were added.
