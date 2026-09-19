# CLI Contract

This document records the command grammar implemented by `internal/cli` and
the existing package-main handlers. CLI parsing is side-effect free; execution
and capability construction remain in package `main` during R3.2.

## Boundary

```text
os.Args[1:] → internal/cli.Parse → cli.Invocation → package-main handler
```

Application configuration flags before a top-level command are retained as
`Invocation.LeadingArgs` and are parsed by `internal/config`. Flags after a
command remain owned by that command's existing handler unless noted below.
The precedence contract remains CLI > `.env` > environment > default.

## Top-level grammar

| Command | Subcommand / args | Config flags | Capability | Handler owner |
| --- | --- | --- | --- | --- |
| *(empty)* | application run; `-h`/`--help` are supported | application flags | HH reads and configured runtime; writes remain gateway-controlled | `runDefault` / `NewHHAIResponder` |
| `hh` | `sync`, `inbox`, `workflow`, `draft ID`, `pilot-candidates`, `pilot-shortlist`, `pilot-show ID`, `write-status`, `eligible`, `eligibility-report`, `eligibility-summary`, `quality-report`, `action preflight\|request-preview ID` | leading application flags | HH read; action preview/preflight are local; no live write is performed by these commands | `runHHCommand` |
| `hh-api` | `auth`, `doctor`, `logout`, `preflight VACANCY_ID [--resume-id ID] [--all-resumes]` | environment/dotenv application config | authenticated HH API GET diagnostics; suitable-resume matrix is read-only and never routes or switches resumes | `runHHAPICommand` |
| `candidate` | `migrate-postgres`, `status`, `semantic status\|reindex\|search QUERY` | environment/dotenv application config; command flags after the command | local/PostgreSQL candidate reads; migration `--apply` is an explicit storage write | `runCandidateCommand` |
| `profile` | `show`, `questions`, `bootstrap`, `import FILE`, `stories`, `communication`, `knowledge proposals\|sync-profile\|confirm ID\|reject ID` | environment/dotenv application config; profile flags remain handler-owned | local candidate reads; import/bootstrap/knowledge confirmation can write local candidate state | `runProfileCommand` / `runKnowledgeCommandWithConfig` |
| `storage` | `migrate-postgres` with migration flags | environment/dotenv application config; migration flags remain handler-owned | PostgreSQL migration; `--apply` is an explicit storage write | `runStorageCommand` |
| `web` / `dashboard` | `--host LOOPBACK`, `--port PORT`, follow-up options | environment/dotenv application config; dashboard flags remain handler-owned | local dashboard; HH reads by default; approved replies can use the HH write gateway | `runDashboardCommand` |
| `reconcile` | optional application flags, including `--dry-run` | application flags after the command | local reconciliation with configured read repositories; no HH write | `runReconcileCommand` |
| `monitor` | optional application flags, including `--run-once` | application flags after the command | HH GET synchronization and local notifications; read-only | `runMonitorCommand` |
| `audit` | optional application flags | application flags after the command | local read-only audit | `runAuditCommand` |

Unknown top-level commands and known-command subcommand names are rejected by
`internal/cli` before a runtime handler is constructed. Invalid arguments for
a recognized command continue to be validated by its existing handler.

## HH subcommands

`hh sync` accepts `vacancies`, `applications`, `conversations`, or
`conversation ID`; with no target it uses the existing all-target behavior.
`hh action` accepts only `preflight ID` and `request-preview ID`.

All HH commands in this table are read-only or local preview/report flows.
Actual HH writes remain behind the existing `HHWriteGateway` and are not
reachable through this R3.2 parser boundary. Dashboard Send is the explicit
exception: it is an application-level approved flow and still must pass the
gateway's dry-run, capability, freshness, and audit checks.

## Exit behavior

`main` delegates to `run(os.Args[1:])`, which returns without calling
`os.Exit` internally. The current compatibility behavior is retained:

- success and help: `0`;
- configuration or CLI syntax error: `2`;
- recognized-command runtime/handler error: command-specific existing failure
  behavior, currently `1` except profile compatibility errors, which remain
  `2`.

Help text is still produced by the canonical configuration or command-specific
`flag.FlagSet`; R3.2 does not rewrite its wording.

## Deliberate non-goals

There is no `cmd/hh-ai-responder/main.go` in R3.2. Business handlers and
application dependencies remain in package `main` until their use cases are
extracted into importable packages.
