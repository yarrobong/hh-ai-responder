# Untyped dependency constructor audit

Audit scope: production constructors with `...any` or `interface{}` dependency
parameters, plus constructors whose accepted dependency is stored as `any`.
`map[string]any` used for JSON payloads/schemas is not dependency injection and
is listed only as a boundary concern.

| Symbol | File | Actual accepted dependency types | Call sites | Future typed signature | Target stage |
| --- | --- | --- | --- | --- | --- |
| `NewAIReplyOrchestrator` | `ai_reply_orchestrator.go` | `*ConversationContextBuilder`, `*ApplicationContextBuilder`, `*ConversationStore`, `*CandidateContextResolver`, `*ApplicationStore`, `*CandidateClarificationStore`, `*AIDraftStore`, `*CandidateKnowledgeUpdater`, `*CandidateMutationService`, `[]CandidateStory`, `string`, `CandidateSemanticRetriever`, `*CandidateKnowledgeAcquisitionService`; unknown values are ignored | dashboard composition, `hh_sync_cli.go`, integration and orchestrator tests | `NewAIReplyOrchestrator(ai StructuredAIClient, deps AIReplyDependencies)` where `AIReplyDependencies` has typed fields and optional acquisition/retriever | R2 typed composition, R3 use-case boundary |
| `NewHHReadSyncService` | `hh_read_sync.go` | `*VacancyStore`, `*ApplicationStore`, `*ConversationStore`, analyzer interface `{ Analyze(Vacancy, any) MatchResult }`, `*CandidateClarificationStore`, `*AIDraftStore`, `HHStatusMapper`, non-empty `string`, and one fallback candidate value | `hh_sync_cli.go`, `dashboard_command.go`, tests, performance compatibility path | `NewHHReadSyncService(client HHReadClient, deps HHReadDependencies)`; separate JSON/repository constructors should be explicit | R2 platform/read composition |
| `NewHHReadSyncServiceWithRepositories` | `hh_read_sync.go` | `CareerRepositories` plus the same variadic compatibility dependencies | currently typed PostgreSQL composition and compatibility callers | `NewHHReadSyncServiceWithRepositories(client, career, deps HHReadDependencies)` or a fully typed constructor | R2 |
| `NewCareerDataReconciler` | `reconciliation.go` | analyzer interface `{ Analyze(Vacancy, any) MatchResult }`; first other dependency becomes `Candidate any` | CLI monitor/reconcile, performance and reconciliation tests | `NewCareerDataReconciler(vacancies, applications, conversations, deps CareerReconcilerDependencies)` with typed `Analyzer` and `Candidate` | R2/R3 |
| `NewCareerDataReconcilerWithRepositories` | `reconciliation.go` | typed repositories, typed analyzer interface, but `candidate any` | PostgreSQL career composition and tests | replace `candidate any` with the canonical candidate/repository-facing type after candidate boundary exists | R3 |
| `NewHHWriteGateway` | `hh_write_gateway.go` | `HHWriteGatewayOptions`; unsupported values are ignored by the compatibility switch | dashboard/CLI composition and gateway tests | `NewHHWriteGateway(options HHWriteGatewayOptions)`; retain a compatibility wrapper only until callers migrate | R2, after write-safety tests |
| `NewApplicationStore` | `job_application_store.go` | `*ConversationStore`, `*CandidateContextResolver`; unsupported values are ignored | CLI, dashboard, workflow, and storage tests | `NewApplicationStore(path string, deps ApplicationStoreDependencies)` or explicit constructors | R2 |

No production constructor currently accepts `map[string]any` as a dependency.
The remaining `map[string]any` uses are JSON responses, AI schemas, or dynamic
HH payloads and should be typed only when their external compatibility contract
is characterized.

The audit is documentation only. No constructor signature changes are part of
R1.
