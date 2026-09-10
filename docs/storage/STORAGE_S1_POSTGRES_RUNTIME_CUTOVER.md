# Executive summary

S1: COMPLETE

Before: PostgreSQL repositories and a PostgreSQL career bundle existed, but
several normal runtime paths still constructed or read legacy JSON vacancy,
application, and conversation stores. Dashboard, monitor, inbox, workflow,
and some sync paths could therefore observe stale or empty JSON data while
PostgreSQL was configured.

After: backend selection is performed in runtime composition. In PostgreSQL
mode, the configured vacancy, application, and conversation repositories are
wrapped by the existing runtime facades and passed to dashboard, Today/home,
inbox, monitor, workflow, draft/eligible, pilot, sync, and projection paths.
JSON mode keeps the existing JSON behavior. No schema or HH write behavior was
changed.

## Source-of-truth rule

`STORAGE_BACKEND=json` selects the JSON repositories as the authoritative
career source.

`STORAGE_BACKEND=postgres` selects PostgreSQL repositories as the authoritative
career source wherever an implementation exists. The JSON files are not loaded
for normal career reads in that mode, and PostgreSQL failures are returned
instead of falling back to JSON.

## Previous mixed wiring

The storage audit found that `CareerRepositories` was already available, but
`loadDashboard`, `runMonitorCommand`, `runHHCommand`, and sync composition
still created local JSON stores or wrapped them directly. The application sync
builder also ignored the already-selected career bundle. Candidate PostgreSQL
composition was present, but normal candidate and career consumers did not
share one composition boundary.

## Runtime repository composition

`CareerRepositories` in `internal/runtime/postgres.go` is the narrow typed
composition result for vacancies, applications, and conversations. The
backend-aware loader in `internal/runtime/hh_sync_cli.go` creates JSON stores
only in JSON mode; in PostgreSQL mode it builds the PostgreSQL bundle and
creates compatibility facades backed by those repository ports.

The production construction/reference audit is:

| Package/file | Construction or consumer | Command/workflow | Backend respected | Role | PostgreSQL implementation |
|---|---|---|---|---|---|
| `internal/runtime/postgres.go` | `BuildCareerRepositories`, JSON adapters, PG bundle | shared runtime composition; `hh sync`, dashboard, monitor, reconcile | yes | canonical career composition | yes |
| `internal/runtime/hh_sync_cli.go` | `loadHHLocalStoresForBackend` | dashboard, `hh inbox`, `hh workflow`, `hh draft`, `hh eligible`, pilot/action paths | yes | injected runtime facades | yes |
| `internal/runtime/dashboard_command.go` | backend-aware local store load | `hh dashboard` / Today/home | yes | dashboard read model dependencies | yes |
| `internal/runtime/hh_read_sync.go` | typed usecase repositories or JSON fallback | `hh sync` | yes | sync read/write ports | yes |
| `internal/runtime/hh_sync_performance.go` | JSON staging adapters; PG batch commit | `hh sync` | yes | JSON-only staging vs PG canonical commit | yes |
| `internal/runtime/hh_sync_cli.go` | JSON stores in the JSON branch | `hh reconcile` | yes | JSON backend composition | yes |
| `internal/runtime/career_migration.go` | JSON career stores | explicit career migration/import | explicit migration only | migration source/compatibility | destination is selectable |
| `internal/runtime/runtime.go` | PG career bundle in the normal responder | application execution/projection | yes | canonical application projection | yes |
| `internal/runtime/repositories.go` | `NewJSON*Repository` adapters | adapter definitions and JSON composition | caller-selected | JSON adapter layer | paired PG adapters exist |
| `internal/runtime/candidate_backend.go` | JSON or PG candidate repository | candidate startup/reconcile/storage | yes | canonical candidate source | yes |

Remaining direct JSON career references are limited to JSON backend branches,
explicit migration/import code, JSON sync staging, compatibility adapters, and
tests. None is a production PostgreSQL-mode career consumer.

## Dashboard

`hh dashboard` now receives the backend-selected vacancy, application, and
conversation facades. PostgreSQL mode therefore reads canonical PostgreSQL
data for dashboard list/detail/status views. Reliability read models retain
their existing backend selection and R14 authority.

## Today

The existing Today/home page uses the same dashboard dependencies. In
PostgreSQL mode vacancies, applications, conversations, and the canonical
candidate are selected from PostgreSQL composition; the page no longer mixes
PostgreSQL candidate data with JSON career data.

## Vacancies

Vacancy list/detail and existing sorting/filtering continue through the same
facade APIs. PostgreSQL mode uses `PostgresVacancyRepository`; JSON mode uses
the existing JSON store and adapter.

## Applications

Application list/detail/status paths use the selected application repository.
Business status semantics were preserved. Successful normal application
execution in PostgreSQL mode now persists the accepted application and its
`Applied` event through `ApplicationRepository`; the existing write/audit
event stream remains operational telemetry, not a second authoritative career
store.

## Conversations / inbox

Conversation and inbox/history paths receive the selected conversation
repository through the existing runtime facades. PostgreSQL mode reads the
canonical conversation and message projections, preserving provider
conversation and message IDs.

## Monitor

`hh monitor` now builds its reconciler from the configured career repository
bundle. PostgreSQL mode no longer monitors stale JSON career repositories.
JSON mode retains the previous JSON composition.

## Workflow / draft / eligible

`hh inbox`, `hh workflow`, `hh draft`, and `hh eligible` now use the
backend-aware loader for career source data. Intentional local draft data
remains JSON. Draft/eligible decisions therefore use PostgreSQL career data in
PostgreSQL mode without migrating local draft persistence.

Pilot paths that are production-reachable through these commands receive the
same selected facades. Debug-only and legacy compatibility paths were not
rewritten.

## Candidate canonical source

Candidate composition remains explicit:

- JSON mode: the JSON Candidate Knowledge source is canonical.
- PostgreSQL mode: `BuildCandidatePersistence` and `CurrentCandidate` are
  canonical; detached snapshots are read views only.
- Legacy JSON in PostgreSQL mode is limited to explicit migration/import or
  comparison use and cannot silently override PostgreSQL candidate truth.

Missing PostgreSQL candidate state continues to produce the explicit
startup/preflight error; it does not trigger an implicit JSON import.

## Application projection

The normal accepted-application path projects local application state to the
configured `ApplicationRepository` in PostgreSQL mode. Existing HH dispatch,
dry-run, write gateway, attempt authority, reconciliation, notification, and
retry behavior was not changed. JSON mode retains its existing event/store
behavior.

## HH sync

`hh sync` remains the reference backend-selection behavior. Its PostgreSQL
batch commit writes the selected vacancy/application/conversation repositories
directly, and the sync service now passes that same bundle to its usecases.
JSON staging and JSON commit remain active only for JSON mode.

## Remaining JSON-only state

| Data | Why JSON remains |
|---|---|
| `candidate_clarifications.json` | Local clarification queue; operational state, not career canonical data |
| `ai_drafts.json` | Intentional local draft persistence |
| `notification_events.json` | Notification lifecycle/audit state; R14 read model |
| `hh_write_actions.json` | Approval/action lifecycle state |
| `hh_write_events.json` | HH write audit/event log; not career authority |
| `hh_pilot_observations.json` | Pilot observation telemetry |
| `quality_log.json` | Quality/diagnostic telemetry |
| `hh_sync_state.json` | Sync cursor/health state |
| `career_monitor_state.json` and related local state | Monitor scheduling/operational state |
| already-responded compatibility state | Legacy compatibility/preflight marker; current HH state and canonical applications remain authoritative |

These are intentionally backend-independent operational or compatibility
files. No S1 migration of local operational files was performed.

## Legacy JSON references

The remaining production references to JSON career constructors are classified
as: JSON backend composition; explicit career migration/import; JSON-only sync
staging; compatibility adapter definitions; or tests. Candidate JSON
construction similarly remains in JSON mode and explicit migration paths.

No remaining reference is classified as a PostgreSQL-mode production bug.

## No-fallback behavior

Selecting PostgreSQL requires a database URL, opens and pings PostgreSQL, and
propagates connection, migration, schema, and repository errors. A missing
URL, unavailable database, missing required schema, or missing canonical
candidate fails visibly. No normal workflow falls back to JSON, and normal
career correctness does not require a PostgreSQL-plus-JSON dual write.

## JSON / PostgreSQL matrix

| Workflow | JSON mode | PostgreSQL mode | Canonical source |
|---|---|---|---|
| Candidate | JSON Candidate Knowledge | PostgreSQL Candidate repository | selected backend |
| Vacancies | JSON store/repository | PostgreSQL repository | selected backend |
| Applications | JSON store/repository | PostgreSQL repository | selected backend |
| Conversations | JSON store/repository | PostgreSQL repository | selected backend |
| Dashboard | JSON-backed facades | PostgreSQL-backed facades | selected backend |
| Today | JSON-backed dashboard dependencies | PostgreSQL-backed dashboard dependencies | selected backend |
| Inbox | JSON conversation/application data | PostgreSQL conversation/application data | selected backend |
| Monitor | JSON reconciler repositories | PostgreSQL reconciler repositories | selected backend |
| Workflow | JSON-backed career ports | PostgreSQL-backed career ports | selected backend |
| HH sync | JSON repositories and staging | PostgreSQL repositories and PG batch commit | selected backend |
| Application projection | existing JSON behavior | `ApplicationRepository` plus applied event | selected backend |

## Semantic configuration audit

Chat provider: the existing OpenAI-compatible chat client uses
`HH_AI_BASE_URL`, `HH_AI_API_KEY`, and `HH_AI_MODEL`; the configured Mistral
setup uses the same client path.

Embedding provider: the configured embedding adapter is OpenAI-compatible and
currently uses `HH_AI_BASE_URL` and `HH_AI_API_KEY`, with `EMBEDDING_PROVIDER`
and `EMBEDDING_MODEL` selecting the adapter/model.

Can separate chat and embedding endpoints today: NO

Current DB vector dimension: `vector(1536)`.

Current embedding dimension contract: runtime semantic embeddings are fixed at
1536 dimensions (`CandidateSemanticEmbeddingDimensions`), matching the
database vector column.

S2 required: YES. S2 must define separate semantic provider endpoint/key and
dimension configuration if chat and embedding providers are to differ. No S2
changes were made here.

## Migrations

NONE. S1 changes runtime wiring only.

## R14 regression

Application attempt authority, auto-chat attempt authority, write gateway,
reconciliation, notifications, and retry semantics remain unchanged. The
existing JSON/PostgreSQL attempt-store composition is preserved.

## Tests

Added S1 composition tests covering repository-backed vacancy/application/
conversation facades, dashboard snapshots, inbox reads, and sync composition.
The tests use fakes and do not require a live database. Existing repository
contract/mock tests remain green.

The empty/stale JSON trap is covered by the fake-backed PostgreSQL composition
tests: the selected repository data is returned without loading or consulting
legacy JSON career files. JSON mode remains independently composable without a
PostgreSQL connection. PostgreSQL live integration was NOT RUN because
`POSTGRES_TEST_DATABASE_URL` was not configured.

## Verification

tests: PASS

race: PASS

vet: PASS

build: PASS

canonical: PASS

diff: PASS

node: PASS

Docker: NOT APPLICABLE

PostgreSQL live integration: NOT RUN

LIVE HH WRITES: 0

## Ready

S2 — Semantic Provider / Vector Dimension Contract
READY

S1 is complete. S2 was not started.
