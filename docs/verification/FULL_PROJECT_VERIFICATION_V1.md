# Executive summary

Overall: PARTIAL

Core project usable: YES for local, dry-run, fixture, and fake-provider workflows. Live HH, live AI, and live PostgreSQL integrations were not run because the required credentials/test DSN were not configured.

LIVE HH WRITES: 0

Highest-severity issue: P3 — nested CLI `--help` handling is inconsistent: some valid nested help invocations exit 1 or 2 instead of 0. No P0/P1/P2 functional defect was reproduced.

The current refactored project builds, passes the complete non-race and race Go suites, passes vet and formatting checks, starts the loopback dashboard on isolated state, persists/reloads local candidate state, fails closed on representative corrupt stores, and has strong fake-transport coverage for application, controlled-chat, auto-chat, leave, reconciliation, notification, and restart safety.

The result is PARTIAL rather than PASS because the CLI help behavior is a reproducible user-facing defect and because external integrations could only be verified at their local typed/mock boundaries. This is not a refactor recommendation; the smallest follow-up is a focused CLI help/exit-code fix and separately environment-enabled integration verification.

# Environment

| Item | Result |
|---|---|
| OS / architecture | macOS / arm64 |
| Go | `go1.25.0 darwin/arm64` |
| Node | `v24.6.0` |
| Canonical source target | `cmd/hh-ai-responder` is a Go package directory |
| Built executable | `go build ./cmd/hh-ai-responder` produces the ignored root binary `./hh-ai-responder` |
| Default storage | JSON |
| `DATABASE_URL` | NOT CONFIGURED |
| Safe PostgreSQL test DSN | NOT CONFIGURED |
| HH cookies/token | NOT CONFIGURED |
| AI provider key | NOT CONFIGURED |
| Embedding provider key | NOT CONFIGURED |
| `psql` / `pg_isready` | Available, but no test DSN was present |
| Docker | Not used; NOT SUPPORTED / NOT APPLICABLE for this stage |

No secret values were printed. One preliminary default invocation reached a read-only HH endpoint and received HTTP 403 before the credential inventory was complete. No HH mutation endpoint was called, and this was not counted as a valid live-read smoke test.

# Worktree

The worktree was already substantially modified before verification. The initial checks recorded:

- latest commit: `121eb62 push`;
- 243 short-status entries at the final pre-report check;
- extensive pre-existing refactor moves/deletions, including root legacy Go files, Docker files, and documentation moves;
- staged documentation renames and unstaged source/web changes.

No reset, clean, checkout, restore, destructive migration, or user-state deletion was performed. All runtime experiments used temporary directories except the preliminary read-only invocation noted above. The requested report is the only verification artifact added by this stage.

# Build / static verification

All required static checks passed:

| Check | Result |
|---|---|
| `gofmt -l .` | PASS; no files listed |
| `git diff --check` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |

The production inventory is organized under `cmd/hh-ai-responder` and `internal/{adapters,application,applicationattempt,autochatattempt,bootstrap,candidate,cli,config,conversation,hhread,llm,platform,ports,runtime,semantic,usecase,vacancy}`. The CLI parser also covers the `run`, `hh`, `candidate`, `profile`, `storage`, `web`, `reconcile`, `monitor`, and `audit` command families.

# Full tests

| Suite | Result |
|---|---|
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |

The full non-race suite passed across command, config, HH read/write adapters, LLM/embedding adapters, JSON/PostgreSQL repositories, bootstrap, candidate, conversation, scheduler, runtime, and typed use-case packages. The race suite also passed. Targeted verbose runs confirmed application-attempt, application-submission, reconciliation, HH-write gateway, auto-chat orchestration, runtime dashboard, persistence, security, and scheduler cases.

PostgreSQL tests that require `POSTGRES_TEST_DATABASE_URL` correctly skipped in this environment. This is environment-limited NOT RUN, not a test failure.

# Startup

Result: PARTIAL.

The built canonical binary started the loopback dashboard from clean temporary state and printed its read-only startup URL. Startup/shutdown and empty-state behavior are covered by runtime tests. A complete normal agent startup that performs HH read synchronization was not run because HH credentials were unavailable; the preliminary unconfigured read returned 403. No panic or fatal startup defect was found in the local/dashboard path.

# JSON backend

Result: PASS.

Default JSON configuration and CLI-over-environment precedence are covered by config tests. Isolated runtime checks verified:

- profile bootstrap creates a valid versioned profile with unknown fields represented explicitly;
- a second process reloads the persisted profile;
- empty local stores produce valid empty projections;
- representative corrupt application-attempt and auto-chat stores return errors, are not treated as empty state, and remain unchanged;
- JSON storage contracts and restart behavior pass in the full suite.

# PostgreSQL backend

Result: NOT RUN for live integration; PASS for static/mock repository coverage.

`DATABASE_URL` and `POSTGRES_TEST_DATABASE_URL` were absent. PostgreSQL repository, transaction, constraint, and migration logic has unit/mock coverage and passed. No production database was contacted, and no destructive database operation was attempted.

# Candidate knowledge

Result: PASS locally; live external enrichment NOT RUN.

Storage, reload, confirmed facts, provenance, missing facts, corrections, proposals, explicit confirmation/rejection, event/projection behavior, canonical mapping, and semantic context resolution are covered and passed. Consumer tests verify candidate context reaches vacancy analysis, cover letters, application answers, employer replies, follow-up drafts, candidate interpretation, and auto-chat decisions.

Unknown facts remain unknown. The tested mutation path requires an explicit confirmation step; no hidden candidate fact creation was observed.

# Semantic / AI

Result: PARTIAL.

Fake-provider and deterministic tests pass for semantic indexing/search, empty indexes, provider failures, malformed results, typed AI parsing, validation, and candidate-context constraints. The typed workflow inventory includes vacancy analysis, cover letter, application answer, test answer, employer reply, follow-up draft, candidate interpretation, and auto-chat reply.

Live AI and live embedding calls were NOT RUN because provider credentials were not configured. Provider-boundary behavior is covered with fakes, including malformed responses and timeout/error handling. Provider-only retry behavior was not confused with HH mutation retry.

# HH reads

Result: PARTIAL.

Read adapters and fixtures cover vacancy search/pagination, vacancy detail parsing, salary/location/experience/description metadata, application and conversation ingestion, status mapping, sync state, rate limiting, expired-auth errors, timeouts, and read-side 429 handling. No live authenticated HH read smoke was run because valid credentials were absent.

No HH write fallback was observed for read failures.

# Vacancies

Result: PASS at fixture/local boundary; live discovery NOT RUN.

Duplicate IDs across pages and queries, deterministic location/format/salary/experience/role filters, vacancy ID parsing, structured metadata precedence, vacancy processing, typed analysis, malformed AI responses, unknown candidate facts, and manual-review/skip semantics are tested. Live HH vacancy discovery was not run.

# Application preparation

Result: PASS.

Fake-provider tests verified vacancy context, candidate context, cover-letter generation, typed parsing, provider errors, boolean/free-text/salary/experience/technology/location answer mapping, unknown-fact handling, and stable question ordering. Literary quality was not used as a pass criterion.

# Automatic applications

Result: PASS for dry-run and fake-writer workflows; live external submission NOT RUN by design.

The tested path composes vacancy filtering, candidate context, analysis, cover letter/questions, fresh read-only preflight, and the write boundary. With `HH_WRITE_ENABLED=false` and `HH_DRY_RUN=true`, mutation calls are zero and preview/blocked behavior is returned. Fake-writer tests cover accepted, rejected, 429, 409, 5xx, network ambiguity, malformed success, preflight failure, reservation failure, persistence failure, restart residue, concurrent reservation, and replay safety.

R14 guarantees were rechecked: reservation before dispatch, early gates, restart blocking, positive-only reconciliation, new attempt IDs for permitted replay of rejected history, dispatch budgets, and no automatic HH write retry.

# Application tracking

Result: PASS locally.

Fixture tests cover application ingestion, provider negotiation ID mapping, status and timestamp updates, conversation relations, repeated sync, duplicate handling, dashboard projections, reliability projections, and CLI inspection. Live HH application sync was NOT RUN.

# Conversations

Result: PASS locally; live sync NOT RUN.

Fixtures verify conversation IDs, stable message IDs, sender/direction, ordering, timestamps, latest employer/candidate messages, and awaiting-reply detection. Provider read failures remain visible rather than authorizing a send.

# Controlled chat

Result: PASS with fake transport/dry-run; live external send NOT RUN.

Draft, edit where supported, approve, nonce, fresh preflight, accepted send, ambiguity, persistence uncertainty, and read-only reconciliation were exercised through the internal fake writer. Controlled-action state persists and remains blocked after restart; a persisted action does not silently become sendable again.

# Auto-chat

Result: PASS with fake transport/dry-run; live external send NOT RUN.

Review mode reads history, evaluates eligibility, invokes the AI boundary, and produces a proposal without a durable live mutation. Fake-writer tests cover new employer triggers, reserve-before-write, accepted/rejected/ambiguous outcomes, persistence failure, restart, same-trigger blocking, independent new trigger M2, concurrency, and missing `TriggerMessageID` fail-closed behavior. Text fallback is not used.

Reconciliation covers exact outgoing IDs, target-response evidence, manual candidate replies, wrong IDs, candidate-before-trigger, new triggers, absence, read failure, and persistence failure. LEAVE remains intentionally unsupported for reconciliation and is represented safely.

# Chat leave

Result: PASS with fake transport; live external leave NOT RUN.

Eligible leave, reservation, accepted/ambiguous outcomes, restart state, and same-trigger replay blocking passed. No real leave request was made.

# Follow-ups

Result: PASS locally with fake AI/dry-run send boundary.

Tests cover eligible application selection, cooldown/date rules, conversation state, employer-response exclusion, candidate context, AI draft generation, persistence/output, and the send boundary. Persisted completed/ineligible work remains excluded after reload according to current semantics.

# Career loop

Result: PASS locally; live HH/AI enrichment NOT RUN.

Fixture/fake-provider iteration tests cover recommendations, questions, candidate-learning gaps, notifications, analytics/events, migration behavior, and clean iteration termination. The career monitor default interval is 15 minutes.

# Notifications

Result: PASS.

Notification storage/projections cover ordinary notifications, application uncertainty/confirmation/conflict, auto-chat uncertainty/confirmation, unsupported leave, unavailable stores, deduplication, dismiss/resolve, links, bounded reads, and restart persistence.

The authority regression passed: dismissing/resolving a notification does not mutate application or auto-chat attempts, controlled actions, provider evidence, or retry authorization, and does not call HH.

# Reliability

Result: PASS locally; live reconciliation NOT RUN.

Application and auto-chat attempt stores, evidence/state labels, inspection, reconciliation, and persistence passed. Positive evidence can confirm delivery; absence, read failure, conflicts, wrong IDs, and persistence failure do not release uncertainty. The reliability dashboard and CLI expose SENDING, DELIVERY_UNCERTAIN, TARGET_RESPONSE_CONFIRMED/confirmed, and unsupported LEAVE states without falsely claiming delivery.

`hh reliability applications` and `hh reliability autochat` were exercised with JSON output, empty state, limits, invalid filters, and corrupt-store fixtures. Ordinary inspection makes no HH call.

# Dashboard

Result: PASS for isolated local startup and routes.

The dashboard was started on loopback from temporary state. The HTML shell, JavaScript, CSS, and 36 meaningful GET/static routes returned 2xx or expected 4xx responses, including home/today, vacancies, applications, conversations/inbox, career-related views, knowledge, analytics, notifications, reliability, sync, health, deep health, diagnostics, and detail-route handling. API responses were valid JSON in empty state; populated-state and field-shape tests passed.

The server logged an explicit HH read-only URL, shut down on SIGTERM without a panic, and dashboard tests passed for security, empty startup, analytics, reliability, action boundaries, and frontend/backend field compatibility. Both `web/app.js` and the embedded runtime asset `internal/runtime/web/app.js` passed syntax checks; the runtime dashboard serves the embedded asset.

# CLI

Result: PARTIAL.

The canonical CLI help, invalid command, invalid argument, missing configuration, local profile, audit, reconcile, reliability JSON, and storage/configuration paths were exercised. Invalid commands return nonzero without panic; local read-only outputs are valid and meaningful; write-capable paths honor disabled/dry-run gates.

Reproducible defect: `hh reliability applications --help` exits 1 with `flag: help requested`; `profile --help`, `monitor --help`, `audit --help`, and `reconcile --help` also do not consistently return a successful help exit. This is P3 and does not bypass HH write safety.

The local `candidate status` and PostgreSQL migration commands correctly report the missing database configuration; this is environment-limited, not a wiring failure.

# Scheduler

Result: PASS locally.

Repository truth shows these registrations:

| Job | Interval |
|---|---:|
| Automatic applications | 12h |
| Auto-chat | 15m |
| Resume touch | 4h |
| Job-search status | 24h |
| Career monitor | 15m default |

Scheduler tests cover registration, completion-based loop behavior, cancellation/shutdown, interval hooks, error handling, and no overlapping runaway loops. No real-hour wait and no HH mutation were used. One simulated iteration was covered by the runtime/use-case test boundaries; external mutating calls remained zero.

# Restart / persistence

Result: PASS locally.

Isolated profile bootstrap/reload verified process-to-process persistence. Contract tests cover candidate knowledge, application read models, conversations, notifications, application attempts, auto-chat attempts, controlled actions, sync state, and restart residue. Restarted uncertain/approved actions remain blocked as required; corrupt stores fail closed and are not silently replaced.

# Failure handling

Result: PASS for tested local/fake boundaries.

Covered failure classes include malformed AI output, provider errors/timeouts, HH read 401/403/429/timeout, HH write 400/409/429/5xx/ambiguity, network ambiguity, reservation failure, persistence uncertainty, PostgreSQL unavailability at configuration/repository boundaries, notification-store failure, embedding failure, invalid CLI input, and corrupt JSON stores.

The observed behavior is fail-closed for safety-sensitive uncertainty. A live PostgreSQL outage and live provider outage were not independently exercised because those external services were not configured.

# Shutdown / concurrency

Result: PASS.

Race tests passed. Context cancellation, HTTP server shutdown, scheduler cancellation, repository/store lifecycle, reservation concurrency, and single-writer behavior have passing tests. No leaked-goroutine failure was reported by the suite.

Production panic/fatal audit found only process exit handling in `cmd/hh-ai-responder/main.go`, schema-construction panics for programmer invariants, and an unsupported internal knowledge-type panic. No `log.Fatal` or user-data-driven runtime panic was found.

# Security smoke

Result: PASS for the requested smoke scope.

The dashboard binds to loopback for accepted local hosts and rejects `0.0.0.0`. GET inspection paths are read-only. Reconciliation is POST-only and security-header/action tests pass. HH writes require the write gateway, explicit enablement, dry-run exclusion, approval/nonce/preflight as applicable, and dispatch limits. Reliability API/CLI output sanitizes secrets and does not expose authorization/cookie fields.

The tracked-file and working-tree high-risk pattern scan found no obvious committed bearer tokens, cookies, private keys, API keys, or credentialed `DATABASE_URL` values. Test fixtures containing placeholder secret-like words were not treated as credentials.

# Verification matrix

| Area | Static | Unit | Integration | Smoke/E2E | Result | Notes |
|---|---|---|---|---|---|---|
| Startup | PASS | PASS | NOT RUN | PARTIAL | PARTIAL | Dashboard/local startup pass; full HH-sync startup not run without auth |
| Config | PASS | PASS | NOT RUN | PASS | PASS | Defaults, precedence, validation, secret redaction |
| JSON | PASS | PASS | PASS | PASS | PASS | Restart, empty, corrupt-store fail-closed |
| PostgreSQL | PASS | PASS | NOT RUN | NOT RUN | NOT RUN | No safe DSN; SQL/repository tests pass |
| Candidate Knowledge | PASS | PASS | PASS | PASS | PASS | Provenance, confirmation, consumers, corrections |
| Semantic | PASS | PASS | NOT RUN | PASS | PASS | Fakes/local semantic paths pass; live embedding/pgvector not run |
| AI | PASS | PASS | NOT RUN | PASS | PARTIAL | Typed fake-provider paths pass; live provider not run |
| HH Reads | PASS | PASS | NOT RUN | PARTIAL | PARTIAL | Fixtures/adapters pass; live authenticated read not run |
| Vacancy Discovery | PASS | PASS | NOT RUN | PASS | PARTIAL | Search/dedupe/filter fixtures pass; live discovery not run |
| Vacancy Analysis | PASS | PASS | PASS | PASS | PASS | Candidate/vacancy context and malformed output handling |
| Cover Letter | PASS | PASS | PASS | PASS | PASS | Fake AI and unsupported-claim validation |
| Application Answers | PASS | PASS | PASS | PASS | PASS | Types, ordering, unknown facts |
| Auto Apply | PASS | PASS | PASS | PASS | PASS | Dry-run and fake writer; live writes prohibited |
| Application Tracking | PASS | PASS | PASS | PASS | PASS | Ingestion/read models/repeated sync fixtures |
| Conversation Sync | PASS | PASS | PASS | PASS | PASS | Stable IDs, ordering, awaiting reply |
| Employer Reply AI | PASS | PASS | PASS | PASS | PASS | Representative categories with fake AI |
| Controlled Chat | PASS | PASS | PASS | PASS | PASS | Approval/nonce/preflight/ambiguity/restart |
| Auto Chat | PASS | PASS | PASS | PASS | PASS | Review and fake-writer live logic |
| Chat Leave | PASS | PASS | PASS | PASS | PASS | Fake transport; unsupported reconciliation safe |
| Follow-up | PASS | PASS | PASS | PASS | PASS | Eligibility, cooldown, draft, boundary |
| Career Loop | PASS | PASS | PASS | PASS | PASS | One fixture iteration terminates cleanly |
| Notifications | PASS | PASS | PASS | PASS | PASS | Dedupe, lifecycle, restart, authority |
| Reliability | PASS | PASS | PASS | PASS | PASS | Attempts, evidence, reconciliation projections |
| Dashboard | PASS | PASS | PASS | PASS | PASS | 36 isolated GET/static routes and populated tests |
| CLI | PASS | PASS | NOT RUN | PARTIAL | PARTIAL | Nested help exit-code defect |
| Scheduler | PASS | PASS | PASS | PASS | PASS | Hooks/clocks; no real-hour wait |
| Shutdown | PASS | PASS | PASS | PASS | PASS | Cancellation, race, server/scheduler lifecycle |
| Security Smoke | PASS | PASS | NOT RUN | PASS | PASS | Local gates and sanitization; no external writes |

# Product feature matrix

| Product capability | Exists | Works end-to-end | Live external dependency tested | Status |
|---|---:|---:|---:|---|
| Application startup | Yes | Local/dashboard path | No | PARTIAL |
| JSON persistence | Yes | Yes, isolated | N/A | PASS |
| PostgreSQL persistence | Yes | Mock/repository boundary | No | NOT RUN |
| Candidate knowledge | Yes | Yes with local fixtures/fakes | No | PASS |
| Semantic search | Yes | Yes with deterministic/fake provider | No | PASS |
| AI workflow suite | Yes | Yes at typed fake-provider boundary | No | PARTIAL |
| HH vacancy reads | Yes | Fixture/read adapter path | No | PARTIAL |
| Vacancy processing/filtering | Yes | Yes with fixtures | No | PASS |
| Vacancy analysis | Yes | Yes with fake AI | No | PASS |
| Cover-letter preparation | Yes | Yes with fake AI | No | PASS |
| Application question answers | Yes | Yes with fixtures/fake AI | No | PASS |
| Automatic application dry-run | Yes | Yes | No, intentionally | PASS |
| Automatic application safety | Yes | Yes with fake writer | No, intentionally | PASS |
| Application tracking | Yes | Yes with fixtures | No | PASS |
| Conversation ingestion | Yes | Yes with fixtures | No | PASS |
| Employer reply generation | Yes | Yes with fake AI | No | PASS |
| Controlled chat | Yes | Yes with fake transport | No, intentionally | PASS |
| Auto-chat review mode | Yes | Yes | No, intentionally | PASS |
| Auto-chat fake-writer logic | Yes | Yes | No, intentionally | PASS |
| Chat leave | Yes | Yes with fake transport | No, intentionally | PASS |
| Follow-ups | Yes | Yes with local/fake boundary | No, intentionally | PASS |
| Career loop | Yes | Yes with fixtures/fakes | No | PASS |
| Notifications | Yes | Yes | N/A | PASS |
| Reliability inspection | Yes | Yes | No HH call | PASS |
| Reliability reconciliation | Yes | Yes with fake/read-only reader | No | PASS |
| Dashboard | Yes | Yes on loopback temporary state | No | PASS |
| CLI | Yes | Mostly; help defect | No | PARTIAL |
| Schedulers | Yes | Yes with test clocks/hooks | No, intentionally | PASS |
| Restart persistence | Yes | Yes in local contracts/fixtures | No | PASS |

# Failed / partial scenarios

## V1-P3-CLI-HELP

Severity: P3

Workflow: Nested CLI help discovery.

Expected: Every valid `--help` invocation prints command-specific help and exits 0.

Actual: `hh reliability applications --help` exits 1 with `flag: help requested`; `profile --help`, `monitor --help`, `audit --help`, and `reconcile --help` also return nonzero or fall back to global help. The main `-h` and `web --help` paths return 0.

Reproduction: Build `./hh-ai-responder`, then run the commands above from an isolated directory.

Root cause: CLI parsing/help handling is inconsistent across command families.

Suggested smallest fix: V1.Fix-CLI — normalize nested help interception and successful exit status without changing command behavior.

## V1-PARTIAL-EXTERNAL-BOUNDARIES

Severity: P2 for release confidence, not a confirmed product defect.

Workflow: Live HH read/sync, live AI/embedding, and live PostgreSQL integration.

Expected: Execute bounded authenticated/read-only or safe-test-database smoke operations.

Actual: Required credentials/test DSN were not configured, so only local, mocked, and typed boundaries were verified. No live write was attempted.

Reproduction: Environment inventory shows all relevant secret/DSN variables NOT CONFIGURED.

Root cause: Environment limitation.

Suggested smallest fix: V1.Fix-ENV — run the bounded read-only/safe-DB integration pack in a controlled environment. Do not use a production database or enable HH writes.

# Environment-limited NOT RUN checks

- Live HH vacancy/application/conversation read synchronization with valid auth.
- Any HH mutation, by design; all mutation tests used dry-run or fake transport.
- Live AI generation and live embedding provider calls.
- Live PostgreSQL migrations, transactions, uniqueness constraints, and repository integration.
- pgvector-backed semantic integration.
- Long-duration scheduler intervals; test clocks/hooks were used instead.
- Stage-specific performance checks requiring private local datasets or explicit live-read opt-in.
- Production-scale data-volume/performance comparison.

These are NOT RUN because credentials, DSNs, or private datasets were unavailable or because running them would violate the zero-live-write constraint. They are not marked FAIL solely for that reason.

# Regression assessment

Pre-refactor/product capabilities exercised locally remain available at their current boundaries: JSON state, candidate knowledge, vacancy processing and analysis, application preparation and safety gates, application tracking projections, conversation mapping, employer-reply and follow-up drafting, controlled/auto-chat safety logic, leave handling, career iteration, notifications, reliability inspection/reconciliation, dashboard pages/APIs, CLI inspection, scheduler registration, and restart persistence.

The verification found no evidence that R10–R14 safety guarantees were removed. In particular, unknown facts remain non-authoritative, notification lifecycle is not an action authority, absence does not confirm delivery, and write attempts do not retry automatically.

# HH write safety

Raw mutation owner: `internal/adapters/hh/write/client.go`; provider mutation HTTP construction remains isolated there. Runtime code uses write interfaces/gateways and does not introduce a direct HH mutation bypass.

HH WRITE RETRY: NONE. Retry-like matches in the write gateway are metadata, tests, or explicit no-retry safeguards. No automatic resend/backoff/requeue loop was found for HH mutation. Read/provider retry behavior is separate.

HH_DRY_RUN: enforced in tested write paths.

HH_WRITE_ENABLED=false: enforced in tested write paths.

LIVE HH WRITES: 0.

# Final decision

PROJECT FUNCTIONAL: PARTIAL

Reason: All required local builds, full tests, race checks, JSON persistence checks, fake-provider/fake-writer safety workflows, dashboard smoke routes, scheduler checks, restart behavior, and security smoke checks passed. Core local product behavior is usable. The result is not a clean PASS because nested CLI help has a reproducible P3 defect and live HH/AI/PostgreSQL integration could not be executed in this environment.

# Recommended next action

Do not start R15 or a broad refactor from this report. If a product fix is desired, run the smallest focused V1.Fix-CLI stage for nested help/exit-code normalization. Separately, run the environment-limited integration pack with valid read-only HH credentials, a disposable PostgreSQL database, and AI credentials if end-to-end provider confirmation is required. Keep HH writes disabled throughout.
