# R4.2 Application Domain Boundary

## Result

`internal/application` now owns the job-application aggregate, its persisted
status/source/event values, event validation, and the intrinsic status-to-event
mapping. The package has one internal dependency: `internal/vacancy`, for the
existing R4.1 matching and reconciliation value types stored on an application.

Persistence, PostgreSQL, HH sync, reconciliation orchestration, conversations,
follow-up policy, AI, Dashboard, and application context construction remain in
the root package.

## Pre-change audit

The direct aggregate dependency graph before extraction was:

`JobApplication` → `ConversationFollowUpState`, `ApplicationStatus`,
`ApplicationSource`, `ApplicationEvent`, `MatchResult`, `DataCompleteness`,
`ReconciliationEvidence`, `time.Time`, and persisted primitive fields.

`ApplicationContext` was intentionally excluded: it embeds Conversation,
CandidateContext, semantic selections, and timeline data and is a read model
assembled by `ApplicationStore`. `ApplicationStats` was also excluded because
it is a store-derived projection. `ApplicationStore` and all repository
methods were excluded as storage/infrastructure.

| Type / function | Previous location | Ownership | Decision |
| --- | --- | --- | --- |
| `JobApplication` | `job_application.go` | APPLICATION_OWNED | Moved |
| `ApplicationStatus` values | `job_application.go` | APPLICATION_OWNED | Moved as `application.Status` |
| `ApplicationSource` values | `job_application.go` | APPLICATION_OWNED | Moved as `application.Source` |
| `ApplicationEvent` / event values | `job_application.go` | APPLICATION_OWNED | Moved as `application.Event` / `EventType` |
| `ApplicationEventTypeForStatus` rule | `job_application_store.go` | APPLICATION_OWNED | Moved as pure `EventTypeForStatus` |
| application validation | `job_application.go` | APPLICATION_OWNED | Moved as `Validate` methods |
| `ConversationFollowUpState` value | `employer_conversation.go` | DEFERRED SHARED VALUE | Moved only as a plain persisted value to remove root conversation dependency; follow-up policy remains root |
| `NextAction` constants | `job_application.go` | WORKFLOW VALUE | Moved as a string-compatible value; workflow orchestration remains root |
| `MatchResult` | `internal/vacancy` after R4.1 | VACANCY/CROSS-DOMAIN VALUE | Kept in vacancy |
| `DataCompleteness` / `ReconciliationEvidence` | `internal/vacancy` after R4.1 | CAREER/RECONCILIATION DEBT | Kept temporarily in vacancy |
| `ApplicationStore` | `job_application_store.go` | STORAGE | Deferred |
| `ApplicationContext` | `job_application.go` / store | INFRASTRUCTURE/READ MODEL | Deferred |
| `ApplicationStats` | `job_application.go` / store | STORAGE PROJECTION | Deferred |
| follow-up policy/service/engine | `follow_up*.go` | CONVERSATION/WORKFLOW | Deferred |
| reconciliation decisions/orchestration | `reconciliation.go` | CAREER/RECONCILIATION | Deferred |

## Extraction inventory

Moved production types:

- `JobApplication`;
- `application.Status`, `application.Source`, `application.FollowUpState`,
  `application.NextAction`;
- `application.Event` and `application.EventType`.

Moved pure behavior:

- application and event intrinsic validation;
- valid status/source/event-type checks;
- status-to-event-type mapping.

Not moved:

- event ID generation, JSON file persistence, locking, Save/Load, indexes,
  cloning, repository methods, SQL, HH mapping, candidate resolution,
  conversation lookup, follow-up policy, or reconciliation.

## Compatibility

`application_compat.go` contains aliases for the old root names and narrow
wrappers for application validation, status validity, and status event mapping.
There are no duplicate application structs, enums, or business-rule
implementations. `ConversationFollowUpState` and its old constants are aliases
to the plain application value so existing conversation JSON and APIs remain
source-compatible.

## JSON and behavior contract

The aggregate fields and tags are unchanged, including `omitempty`, timestamp
representation, external/vacancy/conversation IDs, matching values,
follow-up/reconciliation metadata, and enum strings. Existing application JSON
files remain readable without migration. Existing root storage, PostgreSQL,
HH-read, conversation, follow-up, reconciliation, AI, Dashboard, CLI, config,
and HH-write behavior was not redesigned. No HH write path was invoked.

Characterization coverage is in `internal/application/model_test.go`; storage
and repository contracts remain in root. The root boundary regression
`TestApplicationBoundary_VacancyValueStoreRoundTrip` exercises a synthetic
vacancy relation, application creation, JSON store save/reload, and matching
value preservation.

## Dependency verification

Expected packages:

```text
hh-ai-responder
hh-ai-responder/internal/application
hh-ai-responder/internal/cli
hh-ai-responder/internal/config
hh-ai-responder/internal/platform
hh-ai-responder/internal/vacancy
```

`go list -deps ./internal/application` contains the standard library and
`hh-ai-responder/internal/vacancy`; it does not import the root package,
configuration, CLI, platform, PostgreSQL, HH, AI, or Dashboard packages.

## Size snapshot

Before extraction, `JobApplication` was defined in `job_application.go` as a
20-field aggregate with root-owned validation and application enums/events.
After extraction it is defined in `internal/application/model.go:64-85` with
the same 20 persisted fields and tags. The old root declaration is gone.

Current production LOC snapshot: root package `40,138` lines; application
production package `201` lines (`doc.go` + `model.go`), excluding tests. LOC is
reported as an audit aid, not as the success criterion; ownership and behavior
compatibility are the criteria.
