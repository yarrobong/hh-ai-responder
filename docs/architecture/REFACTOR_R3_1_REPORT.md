# Stage R3.1 report — configuration boundary

Date: 2026-09-08 (Asia/Yekaterinburg)

# Before

Packages:

```text
hh-ai-responder
hh-ai-responder/internal/platform
```

`main.go` LOC: **5183**.

Configuration was owned by `main.go` through one flat `Config` type, a global
`flag.CommandLine` parser, process-environment reads, dotenv loading, scalar
helpers, and validation. `FollowUpPolicy` was embedded in that type even
though its behavior belongs to the follow-up domain.

The pre-change audit and complete field matrix are in
[`CONFIG_CONTRACT.md`](CONFIG_CONTRACT.md). It covers all application flags,
environment inputs, defaults, validation, consumers, secret status, and
target ownership, including legacy and dashboard inputs.

Global parser state before R3.1:

- `os.Args` was read directly by configuration parsing.
- `flag.CommandLine` was mutated and parsed by `main.go`.
- `.env` was parsed by application code but its values were loaded into the
  process environment before lookup.

# Config ownership

New package:

```text
internal/config
```

Files:

- `config.go` — flat transitional config model, `LookupEnv`, and primitive
  `FollowUpConfig`.
- `defaults.go` — canonical defaults.
- `env.go` — dotenv parsing and environment helpers.
- `flags.go` — explicit `flag.FlagSet`, env/CLI precedence, and loading.
- `validation.go` — scalar parsing, normalization, and config validation.
- `config_test.go` — characterization and boundary tests.

Dependency imports:

`internal/config` imports only Go standard-library packages. It does not
import `main`, candidate/vacancy/application/conversation code, HH or AI
adapters, storage, dashboard, PostgreSQL, or `internal/platform`.

# Config model

Structure:

R3.1 intentionally uses a flat `internal/config.Config` shape. The rest of
the production code still uses keyed `Config{...}` literals in package main;
introducing nested subsystem structs here would create a broad compatibility
rewrite before the requested CLI extraction. R3.2 can introduce grouped
`HH`, `AI`, `Storage`, `Candidate`, `Search`, `Dashboard`, `Monitoring`, and
`Runtime` views at the composition boundary.

Subsystem ownership:

- defaults, dotenv, environment parsing, flags, precedence, and scalar
  validation: `internal/config`;
- legacy construction of the root package-main `Config`: one narrow bridge in
  `main.go`;
- dashboard host/port subcommand parsing: remains in package main until CLI
  extraction;
- business/domain validation: remains with its existing domain code.

Domain-typed fields encountered:

- `FollowUpPolicy`.

How handled:

`internal/config.FollowUpConfig` owns only four primitive values. The root
composition bridge converts those values into the existing `FollowUpPolicy`.
Follow-up eligibility and policy behavior were not moved or duplicated.

# Parsing

Dotenv:

Moved to `internal/config`. The default path remains `.env` in the working
directory. Missing files remain tolerated. Comments, `export`, quoting,
escaping, commas, equals signs, and `||` search separators remain data. No
shell is invoked. Existing same-name dotenv/process-environment behavior is
preserved and is explicitly characterized.

Environment:

`Load` accepts a `LookupEnv` function. Production uses `os.LookupEnv`; tests
use map-backed lookups. No environment service or reflection-based parser was
introduced.

Flags:

All previous application flags, short flags, long flags, defaults, usage text,
and validation remain registered in an explicit `flag.FlagSet`. Subcommand
routing and subcommand-specific parsers remain in package main.

Precedence:

```text
CLI flag > .env value > process environment value > default
```

This preserves the existing parser behavior, including the special
`HH_SEARCH_URLS` fallback to `HH_SEARCH_URL` and the legacy `HH_AUTO_CHAT`
fallback when explicit chat mode is absent.

Validation:

Storage backend, PostgreSQL requirement, booleans, durations, integer limits,
concurrency, salary, match score, chat mode, currency, quiet hours, monitor
intervals, AI timeouts, and follow-up primitives are validated in config.
Candidate truth, vacancy matching, conversation state, follow-up eligibility,
and write authorization remain outside config.

# Compatibility

Temporary aliases/wrappers:

- root `parseConfig` delegates to `internal/config.Load` and converts to the
  legacy root `Config`;
- root `getEnv`, `getEnvBool`, `parseDurationEnv`, and `loadDotEnv` delegate to
  package helpers;
- root scalar helpers for search URLs, storage backend, salary, score, chat
  mode, keyword lists, non-negative limits, currency, and quiet hours delegate
  to `internal/config`;
- root default constants used by existing HH/AI code alias canonical config
  defaults;
- `DefaultFollowUpPolicy` is a composition conversion from primitive config
  defaults and remains outside the pure follow-up implementation file.

Remaining config code in main:

- the legacy root `Config` type used by existing package-main consumers;
- the conversion bridge and compatibility delegates;
- CLI dispatch and dashboard-specific option parsing.

Reason:

R3.1 explicitly does not move CLI commands, domain packages, or the
composition root. These wrappers keep the diff mechanical and preserve all
existing keyed config fixtures until R3.2.

# Contract coverage

Safe defaults: **PASS**

Environment parsing: **PASS**

Flags: **PASS**

Environment vs flag precedence: **PASS**

Dotenv: **PASS**

Invalid values: **PASS**

Coverage is in `internal/config/config_test.go`; existing package-main tests
also remain green.

# main.go

Before: **5183 LOC**

After: **4801 LOC**

Moved responsibilities:

- default construction;
- application flag registration;
- env-to-config mapping;
- dotenv implementation;
- scalar parsing and normalization;
- config validation;
- `||` search URL list parsing.

Configuration-related declarations removed:

- the 466-line inline parser/default/env/validation block;
- the root implementations of the moved scalar helpers;
- duplicate default literals for canonical AI/runtime values.

Remaining responsibilities:

- legacy HH client/model surface;
- CLI dispatch and command composition;
- domain workflows and adapters;
- transitional root `Config` conversion.

# Packages

`go list ./...`:

```text
hh-ai-responder
hh-ai-responder/internal/config
hh-ai-responder/internal/platform
```

# Behavior

Candidate truth: **UNCHANGED**

Candidate Acquisition: **UNCHANGED**

Vacancy matching: **UNCHANGED**

Conversation classification: **UNCHANGED**

Follow-up rules: **UNCHANGED**

AI prompts/API: **UNCHANGED**

HH Read: **UNCHANGED**

HH Write: **UNCHANGED**; the safe defaults remain `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`.

Dashboard API: **UNCHANGED**

Storage: **UNCHANGED**

JSON schemas: **UNCHANGED**

PostgreSQL schema: **UNCHANGED**

Local store filenames: **UNCHANGED**

CLI syntax: **UNCHANGED**

Environment names: **UNCHANGED**

Flag names: **UNCHANGED**

# Verification

`gofmt -w .`: **PASS** (the changed Go files were formatted; no unrelated
files were rewritten).

`go test ./...`: **PASS**

`go test -race ./...`: **PASS**

`go vet ./...`: **PASS**

`go build ./...`: **PASS**

`git diff --check`: **PASS**

`node --check web/app.js`: **PASS**

Focused:

- `go test ./internal/config/...`: **PASS**
- `go test ./internal/platform/...`: **PASS**

Safe binary smoke tests using a temporary build and empty temporary working
directory:

- `-h`: **PASS**
- `hh write-status`: **PASS**, reported blocked-by-dry-run capability
- `hh workflow`: **PASS**
- `hh quality-report`: **PASS**

The smoke environment explicitly set `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`. **LIVE HH WRITES: 0.**

Docker: **SKIPPED**. `docker info` found the Docker CLI but reported:
`Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?`

# Remaining configuration debt

- The root legacy `Config` type remains until R3.2 composition/CLI extraction.
- Dashboard host/port and its local follow-up parser remain in package main.
- Candidate profile/stories contain legacy direct environment fallbacks used
  by existing profile-loading paths; the application parser is still the
  canonical source for startup configuration.
- The config model is flat in this stage; grouped subsystem structs are
  deferred to avoid an unrelated keyed-literal migration.
- Existing package-main domain code still consumes the converted root config.

# Ready for R3.2

**READY**

R3.1 is complete. No `internal/cli`, `cmd/hh-ai-responder`, thin-main work,
domain extraction, ports, adapter extraction, or other R3.2 work was started.
