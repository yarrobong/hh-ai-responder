# Persistence landscape before

R6.1 started with persistence implementations and compatibility repositories in
the root package. The previous stages had already extracted the domain values
and pure use cases, but the persistence contracts were still owned by
`package main`.

Candidate JSON remains a legacy, multi-file store. `CandidateKnowledgeBase`
loads the profile and separate knowledge collections and saves them through the
existing staged collection replacement behavior. It is a persistence/workflow
aggregate, not a domain value and is not exposed by the new ports.

Candidate PostgreSQL consists of `PostgresCandidateRepository` for the
canonical aggregate and `PostgresCandidateStore`/`CandidateTx` for the
candidate-specific transaction. PostgreSQL has canonical stories, claims,
row-version checks, and transaction-backed aggregate replacement; JSON does not
share all of those capabilities.

Vacancy persistence is provided by `VacancyStore` through the existing JSON
adapter and by `PostgresVacancyRepository`. Both persist the normalized
`internal/vacancy.Vacancy` value.

Application persistence is provided by `ApplicationStore` through
`JSONApplicationRepository` and by `PostgresApplicationRepository`. Application
events remain append-only and are part of the application storage contract.

Conversation persistence is provided by `ConversationStore` through
`JSONConversationRepository` and by `PostgresConversationRepository`. Message
history, external message identity, state, summary, and candidate claims remain
owned by the existing stores/repositories.

# Caller capability matrix

| Caller | Operation | Concrete dependency before R6.1 | Port decision |
|---|---|---|---|
| `CandidateMutationService` | Read canonical candidate before post-commit work | `CandidateRepository` | `ports.CandidateReader` |
| PostgreSQL candidate mutation transaction | Persist a version-checked canonical mutation | `PostgresCandidateRepository` through `CandidateTx` | `ports.CandidateMutationWriter`; pgx transaction mechanics remain root infrastructure |
| Candidate migration/import | Persist a complete canonical candidate | `PostgresCandidateRepository` | `ports.CandidateWriter` |
| `CandidateKnowledgeAcquisitionService` | Find and create clarifications; retain answer evidence; attach proposals; resolve clarification | `*CandidateClarificationStore` | `ports.CandidateAcquisitionStore` |
| HH read synchronization | Read/create/update normalized vacancies | `VacancyRepository` | `ports.VacancyReader` + `ports.VacancyWriter`, composed as `ports.VacancyStore` where the flow needs both |
| HH read synchronization | Read/import/update applications and relations | `ApplicationRepository` | `ports.ApplicationReader` + `ports.ApplicationWriter`, composed as `ports.ApplicationStore` |
| HH read synchronization | Read/upsert conversations and append messages/state | `ConversationRepository` | `ports.ConversationReader` + `ports.ConversationWriter`, composed as `ports.ConversationStore` |
| `CareerDataReconciler` | Read, update, and explicitly flush vacancies/applications | Root repositories | Vacancy/application reader-writer ports, including their existing `Save(context.Context)` capability |
| Dashboard, career reports, migration-only enumeration | Rich projections, event enumeration, storage helpers | Root stores and concrete repositories | Deferred; not promoted into core ports |

The root names remain aliases for compatibility. The actual contracts now live
under `internal/ports`.

# New ports

| Interface | Consumer | Methods | Domain/use-case values | Current implementations |
|---|---|---|---|---|
| `CandidateReader` | Candidate mutation orchestration and candidate migration verification | `CurrentCandidate` | `candidate.Candidate` | JSON and PostgreSQL canonical readers |
| `CandidateWriter` | Canonical import/migration | `PersistCandidate` | `candidate.Candidate` | PostgreSQL repository; JSON is not forced to emulate canonical aggregate writes |
| `CandidateMutationWriter` | Version-checked candidate mutation persistence | `PersistCandidateIfVersion` | `candidate.Candidate` | PostgreSQL repository bound to the existing candidate transaction |
| `CandidateAcquisitionReader` | Acquisition orchestration | `Get`, `List` | `candidateacquisition.CandidateClarificationRequest` | Existing clarification store |
| `CandidateAcquisitionWriter` | Acquisition orchestration | `Create`, `RecordAnswerEvidence`, `SetProposalIDs`, `MarkResolved` | `candidateacquisition` clarification/answer values | Existing clarification store |
| `CandidateAcquisitionStore` | Acquisition orchestration needing both sides | Composition of the two acquisition interfaces | `candidateacquisition` values | Existing clarification store |
| `VacancyReader` | Vacancy synchronization and reconciliation | `Get`, `GetByExternalID`, `List` | `vacancy.Vacancy`, `ports.VacancyQuery` | JSON and PostgreSQL adapters |
| `VacancyWriter` | Vacancy synchronization and reconciliation | `Create`, `Update`, `Save` | `vacancy.Vacancy` | JSON and PostgreSQL adapters |
| `VacancyStore` | Current career synchronization transaction | Reader + writer composition | `vacancy.Vacancy` | JSON and PostgreSQL adapters |
| `ApplicationReader` | Application synchronization and context assembly | `Get`, `GetByExternalID`, `List`, `Timeline` | `application.JobApplication`, `application.Event` | JSON and PostgreSQL adapters |
| `ApplicationWriter` | Application synchronization, reconciliation, and lifecycle updates | Create/update/import/relation/status/match/event/flush operations | `application`, `conversation`, and `vacancy` values | JSON and PostgreSQL adapters |
| `ApplicationStore` | Current career synchronization transaction | Reader + writer composition | Domain values only | JSON and PostgreSQL adapters |
| `ConversationReader` | Conversation synchronization and history reads | Identity lookups, `List`, `Timeline` | `conversation.EmployerConversation`, `conversation.Message` | JSON and PostgreSQL adapters |
| `ConversationWriter` | Conversation synchronization and policy-state persistence | Upsert, append message, state, summary, claim operations | `conversation` values | JSON and PostgreSQL adapters |
| `ConversationStore` | Current career synchronization transaction | Reader + writer composition | `conversation` values | JSON and PostgreSQL adapters |

No port owns a persistence row, JSON envelope, SQL record, or root-package
compatibility type.

# Candidate design

There is no single `CandidateRepository`. JSON and PostgreSQL are not
structurally identical: JSON retains detailed collections and staged legacy
files, while PostgreSQL owns the canonical aggregate, claims, stories, and
versioned transactions. A giant interface would either leak unsupported JSON
semantics or force PostgreSQL details into callers.

The candidate contracts therefore express separate capabilities:

- `CandidateReader` for the canonical snapshot;
- `CandidateWriter` for complete canonical import;
- `CandidateMutationWriter` for the version-checked mutation primitive.

`CandidateKnowledgeBase` remains root-owned compatibility persistence. It was
not moved into `internal/ports`, and no `ports.CandidateKnowledgeBase` or
storage DTO was introduced.

Candidate mutation policy remains in `internal/usecase/candidatemutation` and
the existing root mutation service. PostgreSQL still uses `CandidateTx` and
`PersistCandidateIfVersion` under a real pgx transaction. JSON still performs
its existing staged multi-file writes through `CandidateKnowledgeUpdater` and
`CandidateKnowledgeBase.Save`; no fake generic transaction API was added.

# Vacancy design

Vacancy read and write capabilities are split because reconciliation and
synchronization use different subsets. `VacancyStore` is only a named
composition for the existing flow that genuinely needs both. HH search remains
outside the port; the port accepts normalized `vacancy.Vacancy` values only.

# Application design

Application reads cover identity lookup, external negotiation lookup, list,
and event timeline. Writes cover normalized creation/update/import, relation
attachment, lifecycle/status changes, match persistence, event append, and the
existing explicit flush used by reconciliation. Rich event enumeration and
Dashboard-specific statistics remain outside the core port.

The application event value is the domain-owned
`application.Event`; the port does not define an application row or event DTO.

# Conversation design

Conversation persistence is separated from query/context helpers. The port
contains stable identity lookups, list/timeline reads, normalized HH upsert,
message append, state/summary persistence, and candidate claim append. History
immutability and duplicate external message handling remain implemented and
tested by the existing adapters. Context builders, policy classification,
Dashboard projections, and HH transport remain deferred.

# Acquisition persistence

The extracted acquisition values remain owned by
`internal/usecase/candidateacquisition`. The new clarification port exposes
only the operations the current acquisition orchestration uses. It does not
expose the root JSON collection, file path, load/save lifecycle, or
`CandidateKnowledgeBase`.

The current JSON clarification store satisfies the port directly. PostgreSQL
candidate mutation remains the existing adapter for canonical candidate facts;
R6.1 does not invent a PostgreSQL clarification implementation or change the
acquisition schema.

# Query vs command split

Vacancy, application, and conversation ports each have reader and writer
capabilities. The `*Store` names are narrow compositions required by the
current transactional synchronization flow, not generic CRUD repositories.
Dashboard query projections, operational stores, migration-only concrete
imports, semantic indexing, and career reporting are deliberately deferred.

# Current implementations

Compile-time assertions in `persistence_ports_assertions.go` prove that the
following current root adapters satisfy the extracted ports:

- `JSONCandidateRepository` and `PostgresCandidateRepository` satisfy
  `CandidateReader`;
- `PostgresCandidateRepository` satisfies `CandidateWriter` and
  `CandidateMutationWriter`;
- `JSONVacancyRepository` and `PostgresVacancyRepository` satisfy
  `VacancyStore`;
- `JSONApplicationRepository` and `PostgresApplicationRepository` satisfy
  `ApplicationStore`;
- `JSONConversationRepository` and `PostgresConversationRepository` satisfy
  `ConversationStore`;
- `CandidateClarificationStore` satisfies `CandidateAcquisitionStore`.

The JSON repository wrappers remain thin root adapters that add context checks
and translate store methods. The PostgreSQL repository implementations remain
in place. No implementation file, SQL, migration, or storage path moved in
R6.1.

# Deferred persistence

The following were intentionally not added to `internal/ports`:

- semantic document/index repositories;
- notifications, quality logs, pilot reports, daily summaries, performance
  snapshots, and HH write audit/draft stores;
- HH search, application submission, tests, chat sending, or reconciliation
  transport;
- AI client and prompt persistence;
- Dashboard projections and storage convenience queries.

# Contract test strategy

R6.1 keeps the existing adapter contract tests as the behavioral authority and
adds compile-time port assertions rather than weakening tests to fit the new
interfaces.

| Port area | Existing invariant to reuse for future adapters |
|---|---|
| Candidate reader/mutation | Canonical snapshot stability, provenance/truth preservation, explicit version conflict behavior, and atomic rollback |
| Candidate acquisition | Clarification identity, answer evidence retention, proposal linkage, and explicit resolution lifecycle |
| Vacancy | Restart round trip, stable local/external IDs, timestamps, evidence, duplicate external-ID rejection |
| Application | Restart round trip, match/provenance/status preservation, append-only event history, and relation integrity |
| Conversation | Restart round trip, immutable message history, duplicate external-message rejection, state, claims, and activity projections |

The relevant existing suites are `candidate_storage_contract_test.go`,
`vacancy_storage_contract_test.go`, `application_storage_contract_test.go`,
`conversation_storage_contract_test.go`, `repository_contract_test.go`, and
the opt-in PostgreSQL repository tests. R6.2 adapters should run the same
behavioral cases against their implementation; no abstract generic test
framework or fake backend was introduced prematurely.

# Dependency graph

The new package imports only domain/use-case value packages:

```text
internal/ports
  -> internal/candidate
  -> internal/vacancy
  -> internal/application
  -> internal/conversation
  -> internal/usecase/candidateacquisition
       -> internal/usecase/candidatecontext
       -> internal/usecase/candidatemutation
```

Pure use cases do not import `internal/ports` in R6.1, so there is no
`ports ↔ usecase` cycle. Forbidden imports:

```text
NONE
```

# Interface quality audit

Generic CRUD interfaces: **NONE**.

`any`/`interface{}` persistence APIs: **NONE**.

Infrastructure DTOs in ports: **NONE**.

Candidate knowledge aggregate exposed by ports: **NO**.

Unnecessary generic transaction abstraction: **NONE**.

Backend-specific error taxonomy added: **NONE**.

# Behavior

All runtime behavior: **UNCHANGED**.

Schemas: **UNCHANGED**.

Files and file paths: **UNCHANGED**.

Backend selection and no-fallback behavior: **UNCHANGED**.

Candidate, resolution, mutation, acquisition, conversation, application, and
vacancy semantics: **UNCHANGED**.

HH write safety and transport: **UNCHANGED**.

LIVE HH WRITES: **0**.

# Verification

| Check | Result |
|---|---|
| `gofmt -w .` | PASS |
| `go test -count=1 ./...` | PASS |
| Focused extracted packages | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `go list ./...` | PASS |
| `go list -deps ./internal/ports` | PASS; no root, platform, JSON, PostgreSQL, HH, AI, or dashboard dependency |
| Docker build | SKIPPED; Docker daemon unavailable |
| Existing JSON/PostgreSQL storage contracts | PASS; PostgreSQL integration cases remain opt-in when `POSTGRES_TEST_DATABASE_URL` is absent |

# Ready for R6.2

**READY** — the contracts are capability-based and narrow enough that JSON and
PostgreSQL do not need to pretend they support identical candidate semantics.
Candidate mutation atomicity remains expressed as a versioned persistence
primitive, while transaction mechanics and backend-specific capabilities remain
in their current adapters.

R6.2 may now establish the Candidate JSON adapter boundary. It should not move
PostgreSQL files or begin a broader career repository migration automatically.
