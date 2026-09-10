# Target package plan

This is a staged target, not an R1 implementation. R1 creates no `internal/`
production packages and moves no Go files.

```text
cmd/hh-ai-responder

internal/config
internal/cli
internal/platform

internal/candidate
internal/vacancy
internal/application
internal/conversation
internal/career

internal/ports
internal/usecase

internal/adapters/hh
internal/adapters/ai
internal/adapters/storage/json
internal/adapters/storage/postgres
internal/adapters/web/dashboard
```

## Responsibilities

- `cmd/hh-ai-responder`: thin process entry point and composition invocation.
- `internal/config`: defaults, env parsing, CLI-over-env precedence, and
  validated runtime configuration.
- `internal/cli`: command routing and human/JSON output.
- `internal/platform`: locks, clocks, filesystem primitives, logging, and
  process-level helpers.
- `internal/candidate`: candidate facts, provenance, truth status, canonical
  mutations, unknowns, proposals, and employer-safe projections.
- `internal/vacancy`: vacancy models, deterministic matching, and preflight
  state decisions.
- `internal/application`: applications, approvals, drafts, and application
  state transitions.
- `internal/conversation`: conversation state, clarifications, eligibility,
  and message-domain rules.
- `internal/career`: reconciliation, monitoring, follow-up, and cross-domain
  career workflows.
- `internal/ports`: narrow interfaces for HH reads/writes, AI, storage,
  semantic indexing, clock, and external side effects.
- `internal/usecase`: orchestration of domain operations through ports.
- `internal/adapters/*`: concrete HH, AI, JSON, PostgreSQL, and dashboard
  implementations.

## Allowed dependency directions

```text
cmd -> config, cli, platform, usecase, adapters
cli -> config, usecase, domain read models
usecase -> domain packages, ports
career -> candidate, vacancy, application, conversation, ports
candidate/vacancy/application/conversation -> platform primitives only when
  explicitly approved; no external adapters
adapters -> ports, domain DTOs/domain services
platform -> standard library and approved low-level libraries
```

The composition root may depend on all implementations needed for wiring.
Adapters must not become hidden composition roots.

## Explicit prohibitions

Candidate, vacancy, application, and conversation domain code must not import:

- PostgreSQL;
- HH transport/client code;
- AI transport;
- HTTP dashboard code;
- filesystem storage adapters.

Use cases may depend on domain packages and ports. Adapters may depend on ports
and domain packages. HH read ports must not expose HH write capability, and the
dashboard must not bypass `HHWriteGateway` for external writes.

## Extraction order

1. Establish typed config/platform seams and keep compatibility wrappers.
2. Extract ports and characterization-tested use cases.
3. Move candidate and vacancy domain slices with no behavior change.
4. Move application, conversation, and career slices.
5. Move HH/AI/storage/dashboard adapters and simplify composition.

Every stage must retain dry-run protection, JSON compatibility, PostgreSQL
transaction semantics, and the no-uncertainty-to-action rule.
