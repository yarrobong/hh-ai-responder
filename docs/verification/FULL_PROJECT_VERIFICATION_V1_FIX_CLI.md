# Executive summary

V1.Fix-CLI: COMPLETE

The nested CLI help defect is fixed. All required root and nested `--help` /
`-h` invocations exit successfully with command-specific help, without entering
normal business logic. No live HH write occurred.

# Root cause

`internal/cli.Parse` only classified help before a top-level command. Help
flags after a command therefore reached different parser implementations.

`bootstrap.Run` treated `config.ErrHelp` as successful only for the synthetic
root `CommandHelp` invocation. Nested configuration parsers consequently
reported the typed `flag.ErrHelp` sentinel as a normal configuration error.
Runtime command handlers also passed `flag.ErrHelp` through `commandResult`,
which printed `flag: help requested` and returned a failure code. Reliability
flag sets discarded their help output as well.

# Previous behavior

| Command | Previous exit |
| --- | ---: |
| root `--help` / `-h` | 0 |
| `profile --help` / `-h` | 2 |
| `monitor --help` / `-h` | 2 |
| `audit --help` / `-h` | 2 |
| `reconcile --help` / `-h` | 2 |
| `hh --help` / `-h` | 1 |
| `hh reliability --help` / `-h` | 0 |
| `hh reliability applications --help` / `-h` | 1 |
| `hh reliability autochat --help` / `-h` | 1 |

# New behavior

| Command | Exit |
| --- | ---: |
| root `--help` / `-h` | 0 |
| `profile --help` / `-h` | 0 |
| `monitor --help` / `-h` | 0 |
| `audit --help` / `-h` | 0 |
| `reconcile --help` / `-h` | 0 |
| `hh --help` / `-h` | 0 |
| `hh reliability --help` / `-h` | 0 |
| `hh reliability applications --help` / `-h` | 0 |
| `hh reliability autochat --help` / `-h` | 0 |

Each smoke-tested invocation printed one usage block and zero occurrences of
`flag: help requested`.

# Implementation

- Added typed immediate command-help classification to `cli.Invocation`.
- Made `bootstrap.Run` skip configuration loading for that intent and accept
  the typed `config.ErrHelp` sentinel as success.
- Added one runtime help boundary for command-specific usage before handler
  construction and business logic.
- Made `commandResult` treat `flag.ErrHelp` as success, while preserving all
  other parser errors and exit codes.
- Routed command-owned `FlagSet` help to the command output stream so help is
  printed once instead of being discarded.

No HH readers, writers, AI calls, storage schemas, scheduler, dashboard,
application flow, chat flow, or reconciliation semantics were changed.

# Regression safety

Process-level tests cover the complete requested help matrix, command-specific
output, single help rendering, and absence of the help error message. They also
cover unknown root/HH/reliability commands, invalid profile/reliability flags,
and a missing reliability reconciliation attempt ID.

# Tests

- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS

# Verification

gofmt: PASS

tests: PASS

race: PASS

vet: PASS

build: PASS

canonical build: PASS

diff: PASS

node: PASS

Docker: NOT APPLICABLE

LIVE HH WRITES: 0

# V1 local status

LOCAL FUNCTIONAL VERIFICATION: PASS

Remaining environment-limited checks:

- live HH reads
- live AI
- live embeddings
- live PostgreSQL

These are NOT RUN, not product defects.

# Ready

V1.External Integration Verification: READY WHEN ENVIRONMENT IS CONFIGURED

Do NOT start it automatically.
