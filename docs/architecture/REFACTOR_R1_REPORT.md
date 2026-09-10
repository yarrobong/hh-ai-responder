# Stage R1 report — stabilization / repository hygiene / refactor preconditions

Date: 2026-09-08 (Asia/Yekaterinburg)

## WORKTREE BEFORE

The initial `git status --short`, `git diff`, and `git diff --cached` showed the
following dirty paths. All were classified before editing as
`ACQUISITION_WORK`:

```text
README.md
ai_reply_orchestrator.go
candidate_canonical.go
candidate_canonical_mapper.go
candidate_fact_resolver.go
candidate_knowledge.go
candidate_knowledge_proposal.go
candidate_knowledge_updater.go
candidate_mutation.go
candidate_storage_command.go
dashboard_server.go
postgres_candidate_repository.go
relevant_knowledge_snapshot.go
web/app.js
candidate_knowledge_acquisition.go
candidate_knowledge_acquisition_test.go
migrations/000005_candidate_acquisition.up.sql
```

Other dirty files: none observed.

`GENERATED_ARTIFACT`: none initially classified.

`ARCHITECTURE_AUDIT_OUTPUT`: none initially classified.

No reset, checkout, stash, discard, or user-change rewrite was performed.

## STABILIZATION

Acquisition status: **READY**.

- Production code compiles.
- JSON acquisition path is covered by typed gap creation/deduplication,
  clarification provenance, free-text proposal creation, explicit proposal
  confirmation, unknown resolution, structured choice, dismissal, and AI
  confirmation rejection tests.
- PostgreSQL acquisition wiring compiles through the canonical mutation service
  and repository transaction path. PostgreSQL integration contracts are present
  and skip safely because `POSTGRES_TEST_DATABASE_URL` is not configured.
- `CandidateUnknown` lifecycle, proposal lifecycle, provenance, transaction
  semantics, and semantic post-mutation indexing hooks were audited without
  redesign. No completion blocker was found.
- No acquisition production code was changed in R1.

Migration `000005_candidate_acquisition`:

- adds provenance/gap columns to `candidate_unknowns`;
- adds proposal provenance columns to `candidate_knowledge_proposals`;
- adds event provenance columns to `candidate_knowledge_events`;
- broadens the unknown status check;
- broadens the skill-use parent check;
- adds `employer_conversation` to event/source checks.

The paired `migrations/000005_candidate_acquisition.down.sql` reverses only
those columns and restores the exact pre-000005 check definitions. It does not
drop tables, extensions, earlier-migration objects, or candidate data. A
rollback correctly fails if acquisition-only data values make the earlier
constraints impossible to restore. `migration_contract_test.go` checks the
pairing, narrow scope, and restored contracts.

Docker build: **SKIPPED** after the required command was attempted. Exact
reason:

```text
Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?
```

The Dockerfile was minimally fixed to copy `migrations/` and `web/`, covering
all current `go:embed` paths.

Tracked artifacts removed: `hh-ai-responder.test` (Mach-O Go test binary,
14,971,826 bytes). It is now ignored by `*.test`.

## CONTRACT COVERAGE

Existing:

- configuration/CLI precedence and major routing coverage across current CLI,
  workflow, and performance tests;
- JSON filename/field compatibility and private-store behavior in storage
  contract tests;
- candidate truth ordering, unknown-not-confirmed behavior, and employer-safe
  projection tests;
- HH write approval, fresh preflight, terminal conversation, dry-run,
  nonce/replay, uncertain transport, and no-automatic-retry tests;
- targeted read versus full synchronization coverage;
- acquisition lifecycle and semantic index behavior.

Added:

- `migration_contract_test.go` for the narrow 000005 rollback contract;
- `docs/architecture/REFACTOR_CONTRACTS.md` matrix identifying the existing
  characterization coverage and extraction gates.

Still missing:

- a live disposable-PostgreSQL execution of the 000005 up/down pair and the
  acquisition transaction path;
- a single consolidated config/subcommand characterization table;
- a few explicit golden tests for conflicting candidate source precedence and
  stale semantic index behavior before package moves.

These are follow-up characterization items, not R1 blockers.

## REPOSITORY HYGIENE

Docs moved with `git mv`:

- `PERFORMANCE_STAGE22*.md` → `docs/performance/`
- `VALIDATION_STAGE*.md` → `docs/validation/`
- `QUALITY_STAGE26.md` → `docs/quality/`
- `REPOSITORY_BOUNDARY_STAGE.md` → `docs/architecture/`
- `CANDIDATE_WRITERS_AUDIT.md` → `docs/architecture/`

Broken links: none found after the moves. README links to the moved validation
and performance reports were updated; `candidate_communication.md` remains at
its original path.

`.gitignore`: added `*.test`, `*.out`, `coverage.*`, and `coverage/`; existing
private-data ignore rules were preserved.

## ARCHITECTURE

Current packages: **1** production package, `main`.

Current inventory: 84 production Go files, 53 test Go files, approximately
41k production LOC, `main.go` at 5,183 LOC, with 108 functions and 100+ types.

Target package plan: `cmd/hh-ai-responder`; `internal/config`, `internal/cli`,
`internal/platform`; domain packages `internal/candidate`, `internal/vacancy`,
`internal/application`, `internal/conversation`, `internal/career`; shared
`internal/ports` and `internal/usecase`; and adapters under
`internal/adapters/hh`, `ai`, `storage/json`, `storage/postgres`, and
`web/dashboard`. The staged dependency rules are documented in
`TARGET_PACKAGES.md`.

Untyped constructors: the audit covers `NewAIReplyOrchestrator`,
`NewHHReadSyncService` and its repository variant, `NewCareerDataReconciler`
and its repository variant, `NewHHWriteGateway`, and `NewApplicationStore`.
Accepted runtime types, call sites, typed-signature recommendations, and target
stages are in `UNTYPED_DEPENDENCIES.md`.

No `internal/` production package was created. `main.go` architecture was not
modified.

## VERIFICATION

```text
go test ./...:            PASS
go test -race ./...:     PASS
go vet ./...:            PASS
go build ./...:          PASS
git diff --check:        PASS
node --check web/app.js: PASS
docker build:            SKIPPED (Docker daemon unavailable)
```

PostgreSQL integration tests reported `POSTGRES_TEST_DATABASE_URL is not
configured` and skipped; they did not fail or perform external writes.

The test suites were also rerun explicitly with
`HH_WRITE_ENABLED=false HH_DRY_RUN=true`; both passed.

LIVE HH WRITES: **0**.

Expected production behavior changes: **NONE**. The only R1 runtime-adjacent
changes are Docker build input correctness and the safe paired rollback
migration. HH transport, HH Write Gateway, AI prompts/behavior, dashboard
behavior, storage filenames, and JSON schemas were not changed by R1.

READY FOR R2 PLATFORM EXTRACTION: **YES**.

The only unverified item is the Docker build, blocked by the unavailable local
daemon; the Dockerfile input correction is complete and should be rerun when
Docker is available. R1 is complete and stops here.
