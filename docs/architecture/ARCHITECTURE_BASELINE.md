# Architecture baseline (R1)

Date: 2026-09-08. This is a read-only description of the repository before
package extraction. The current implementation remains in `package main`.

## Inventory

- 84 production Go files
- 53 test Go files
- 1 production package: `main`
- 41,041 production Go LOC (about 41k)
- `main.go`: 5,183 LOC
- 108 functions and 100+ types in `main.go`

The inventory intentionally describes the current baseline, not a proposed
file move. Candidate Knowledge Acquisition changes present in the working tree
are included as current behavior and are not extracted in R1.

## Current runtime flows

### Startup and command routing

`main.go` loads configuration from defaults, environment, and CLI arguments,
then routes the command to the command-specific functions. The composition
root selects JSON or PostgreSQL persistence and wires read services, AI
services, candidate knowledge services, dashboard services, and the HH write
gateway. Existing flags, environment variables, `cookies.txt`, and JSON file
names remain compatibility surfaces.

### Vacancy search and application preparation

Vacancy search reads HH vacancy data, local vacancy/application state, and
candidate context. Deterministic matching evaluates trusted structured facts;
AI may enrich scoring and requirement extraction. A vacancy preflight is a
separate read-only check of availability, response state, tests, cover-letter
requirements, and relevant constraints. Preparation produces a draft or a
review result; it does not itself send an application.

### HH read synchronization

`HHReadClient` exposes only vacancy, application, and conversation reads.
`HHReadSyncService` imports and reconciles those records into the selected
local backend. Targeted conversation reads are available for preflight and
display refresh; targeted reads do not invoke full conversation synchronization.
The synchronization layer has no write-shaped HH method.

### Candidate knowledge and acquisition

The candidate profile and knowledge collections can be loaded from compatible
JSON stores or from the canonical PostgreSQL repository. Context resolution
projects only employer-safe confirmed knowledge. Acquisition detects typed
knowledge gaps from employer questions, deduplicates by deterministic gap key,
stores provenance in clarifications/unknowns, and records candidate answers.
Structured choices may apply a confirmed mutation; free text creates a
proposal that remains unconfirmed until an explicit candidate confirmation.
Unknown, proposal, event, and canonical mutation updates are kept within the
existing backend transaction boundaries. Confirmed mutations can invalidate or
refresh semantic projections according to the existing semantic service.

### Employer conversation and AI drafts

Conversation/application context builders assemble trusted local state and
safe candidate knowledge. `AIReplyOrchestrator` asks the structured AI client
for a decision or draft, persists drafts/clarifications, and routes uncertain
candidate facts to manual review or acquisition. Employer text is untrusted
input and is not treated as instructions.

### HH write safety flow

An approved local action enters `HHWriteGateway`. The gateway verifies action
approval, freshness and targeted read-only preflight, terminal state, nonce and
request validity, then enforces capability settings. With
`HH_WRITE_ENABLED=false` and `HH_DRY_RUN=true`, transport is blocked before an
HH write. Uncertain transport is terminal for automatic retry and is recorded
for review. The gateway is the external HH write boundary.

### Dashboard and semantic search

The dashboard serves embedded `web/` assets and local API views over vacancy,
conversation, candidate knowledge, drafts, and safety state. Dashboard write
actions call application services or the HH write gateway; they do not call HH
transport directly. Semantic documents are a rebuildable, truth-filtered
projection of canonical candidate knowledge, stored in memory or PostgreSQL
and used for relevant examples/knowledge retrieval.

## Logical domains in the current package

- configuration and CLI routing;
- HH transport, read synchronization, vacancy search, preflight, and writes;
- vacancy matching and application lifecycle;
- employer conversations, follow-ups, notifications, and drafts;
- candidate profile, canonical candidate knowledge, acquisition, provenance,
  and semantic indexing;
- JSON compatibility stores and PostgreSQL repositories/migrations;
- dashboard HTTP handlers and embedded web assets;
- performance, validation, audit, and operational logging.

These are logical domains only. They are not Go packages today.

## File responsibility map

| Area | Current files / responsibility |
| --- | --- |
| Composition and legacy HH client | `main.go`, `config` definitions in `main.go`, HH API models and transport helpers |
| CLI and workflows | `*_command.go`, `daily_workflow.go`, `hh_sync_cli.go`, `storage_command.go` |
| Vacancy/application | `vacancy_*.go`, `job_application*.go`, `application_storage_contract_test.go` |
| HH read | `hh_read_sync.go`, `hh_incremental_read.go`, `hh_read_*.go`, `hh_sync_*.go` |
| HH write | `hh_write_gateway.go`, `hh_write_transport.go`, `hh_write_actions.json` compatibility store, `hh_reply_pilot.go` |
| Conversations and AI | `conversation_*.go`, `employer_conversation.go`, `ai_reply_orchestrator.go`, `candidate_communication.go` |
| Candidate knowledge | `candidate_*.go`, `relevant_knowledge_snapshot.go`, `candidate_fact_resolver.go` |
| Acquisition | `candidate_knowledge_acquisition.go`, acquisition sections in canonical/mutation/updater/repository files, clarification/dashboard integration |
| Persistence | `repositories.go`, `*_store.go`, `postgres*.go`, `migrations/` |
| Semantic indexing | `candidate_semantic*.go`, `semantic_retrieval.go`, semantic PostgreSQL repository |
| Dashboard | `dashboard_*.go`, `web/index.html`, `web/app.js`, `web/styles.css` |
| Operational reporting | `performance*.go`, `quality_log.go`, `career_audit.go`, historical reports under `docs/` |

## Findings

### HIGH

1. The entire production surface is one Go package. Domain, use-case,
   transport, persistence, and HTTP code can access one another without an
   enforced dependency boundary.
2. Three central composition constructors accept variadic untyped dependencies
   (`NewAIReplyOrchestrator`, `NewHHReadSyncService`, and
   `NewCareerDataReconciler`). This makes wiring errors runtime-discoverable
   rather than compile-time errors.
3. `main.go` is both a legacy HH client/model surface and part of the command
   composition root. Changes there have unusually broad blast radius.

### MEDIUM

1. JSON compatibility stores and PostgreSQL repositories coexist in the same
   package, with backend selection and transaction policy distributed across
   composition and service code.
2. Canonical candidate mutation, provenance events, and semantic projection
   invalidation cross several files and require characterization before moves.
3. Embedded assets are a build input. Docker must copy all `go:embed` paths,
   not only Go sources.
4. Historical operational reports were at repository root, making current
   source and audit output harder to distinguish.

### LOW

1. Several HTTP/JSON response shapes use `map[string]any` at boundaries. These
   are useful for compatibility but reduce discoverability.
2. Legacy constructor compatibility paths retain concrete store acceptance even
   where typed repository constructors already exist.
3. Repeated test fixture setup spans many files; consolidation should wait
   until package boundaries are established.

## Safety invariants

- `HH_DRY_RUN=true` blocks every state-changing HH request.
- `HH_WRITE_ENABLED=false` disables live HH writes.
- A `MATCH` is not application authorization; fresh read-only preflight is
  required.
- Unknown critical state becomes `REVIEW_REQUIRED`, never an automatic write.
- Candidate facts are employer-safe only when their provenance and truth status
  permit the claim; unknown is not confirmation.
- AI can extract proposals but cannot confirm candidate knowledge.
- Candidate confirmation, unknown resolution, event provenance, and canonical
  mutation remain atomic within the selected backend transaction.
- HH read services do not gain HH write capability.
- Dashboard external writes go through `HHWriteGateway`.
- Tests use fixtures/mocks and must not require real HH cookies or perform HH
  writes.

## Dependency direction

The current package has no compiler-enforced direction. The staged target is
documented in `TARGET_PACKAGES.md`; the contract matrix is in
`REFACTOR_CONTRACTS.md`. The intended direction is domain → ports/use cases →
adapters, with the composition root wiring implementations. This document
does not claim that target has been implemented.

## Refactor roadmap

1. R1: preserve the acquisition work, restore repository hygiene, verify
   embedded assets, document contracts, and do not move production files.
2. R2: extract platform/configuration and typed composition seams while keeping
   behavior and package `main` compatibility under characterization tests.
3. R3: extract candidate/vacancy/application/conversation domain and ports in
   small slices; keep adapters behind interfaces.
4. R4: move HH/AI/storage/dashboard adapters and reduce compatibility
   constructors only after call sites are migrated and safety tests pass.
5. R5: remove obsolete compatibility paths only with an explicit breaking
   change decision.

The roadmap is staged work, not a speculative rewrite plan. R1 stops here.
