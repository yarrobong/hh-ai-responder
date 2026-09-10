# Go Refactor

## Purpose

Use this skill when refactoring Go code in this repository.

Typical tasks include:

* splitting large files;
* extracting packages;
* reducing coupling;
* moving types/functions;
* simplifying orchestration;
* improving boundaries between domains;
* reducing duplicated logic;
* improving testability.

Refactoring must preserve existing behavior unless the task explicitly requires a behavior change.

---

## Core principle

Inspect before editing.

Do not perform a large architecture rewrite merely because a file is large.

Prefer the smallest coherent refactor that improves the requested area.

---

## Behavior preservation

A refactor should not silently change:

* HH application behavior;
* vacancy matching decisions;
* dry-run behavior;
* CLI flags;
* environment variables;
* JSON event formats;
* default configuration;
* persistence formats;
* employer-chat behavior;
* write safety gates.

If behavior must change, separate that change clearly from structural refactoring.

---

## Before changing code

Before editing a function/type/package:

1. locate its definition;
2. search all usages;
3. identify tests;
4. identify interfaces;
5. identify persistence/serialization impact;
6. identify HH read/write implications;
7. identify configuration dependencies.

Do not move code based only on filename intuition.

---

## Package boundaries

Create a new package only when there is a meaningful domain or dependency boundary.

Good candidates may include:

```text id="3jtw3x"
internal/hh
internal/application
internal/candidate
internal/matching
internal/chat
internal/ai
internal/config
internal/storage
```

Do not create packages solely to make the directory tree look cleaner.

A package should have a clear responsibility.

---

## Dependency direction

Prefer dependencies flowing from orchestration toward lower-level components.

Example:

```text id="zqtrha"
CLI / main
    ↓
orchestrator
    ↓
domain services
    ↓
HH / storage / AI adapters
```

Avoid circular dependency pressure.

Do not let low-level packages import the main orchestration layer.

---

## Keep main thin

When practical, `main` should primarily handle:

* configuration loading;
* dependency construction;
* CLI parsing;
* top-level lifecycle;
* application startup/shutdown.

Business logic should not accumulate in startup code.

However, do not migrate the entire project at once.

Extract incrementally.

---

## Extraction strategy

Prefer extracting one coherent responsibility at a time.

Example:

```text id="f1f0x9"
main.go

before:
- config
- HH client
- vacancy matching
- application orchestration
- chat orchestration
- dashboard
- CLI
```

A safe sequence may be:

```text id="8g2b3g"
1. extract pure types/helpers
2. extract read-only HH functions
3. extract write gateway
4. extract vacancy matching
5. extract orchestration
6. reduce main
```

Do not combine unrelated extractions into one large unreviewable diff.

---

## Function signatures

Before changing a function signature:

1. search every call site;
2. inspect tests;
3. inspect interface implementations;
4. inspect mocks/fakes;
5. inspect indirect function references.

Avoid introducing parameter explosions.

If a function requires many related parameters, consider a small explicit input struct.

Do not create generic context bags containing unrelated fields.

---

## Interfaces

Introduce interfaces only where they provide value.

Good reasons:

* external dependency abstraction;
* testing;
* multiple implementations;
* stable domain boundary.

Avoid interfaces created solely "for clean architecture".

Prefer small interfaces owned by the consumer.

Example:

```text id="8y4tlt"
type VacancyReader interface {
    GetVacancy(ctx context.Context, id string) (Vacancy, error)
}
```

instead of a massive interface containing every HH operation.

---

## HH read/write boundary

Refactoring must preserve explicit separation between HH reads and writes.

Do not accidentally merge:

```text id="6bwo8k"
HHReader
HHWriter
```

into a generic client if doing so makes write behavior less visible.

Write operations should remain easy to audit.

Dry-run enforcement must remain at or above the real write boundary.

---

## Pure logic

Prefer extracting deterministic pure logic where possible.

Examples:

* salary comparison;
* requirement classification;
* match decision derivation;
* validation;
* normalization;
* parsing.

Pure logic should not perform:

* network requests;
* disk writes;
* HH writes;
* environment reads;
* logging as a side effect.

This makes behavior easier to test.

---

## Context

For I/O functions, prefer `context.Context` where appropriate.

Do not add context mechanically to pure helpers.

Respect cancellation/timeouts when calling:

* HH;
* AI providers;
* databases;
* HTTP services.

Do not replace existing context handling with `context.Background()` inside lower-level functions unless there is a strong reason.

---

## Errors

Prefer explicit errors that preserve useful cause information.

Use wrapping:

```go id="x6wk2g"
fmt.Errorf("load vacancy %s: %w", id, err)
```

Do not replace meaningful errors with generic strings.

Where the project benefits from stable classification, use:

* sentinel errors;
* typed errors;
* explicit status enums.

Do not parse error strings for control flow if a structured alternative exists.

---

## Safety errors

Safety-related failures must remain distinguishable.

Examples:

```text id="fhbflh"
review required
dry-run blocked
preflight incomplete
already responded
unsafe test answer
write forbidden
```

Do not collapse safety decisions into generic `error != nil` paths if that would make behavior ambiguous.

---

## State mutation

Make mutation explicit.

Avoid helper functions that unexpectedly modify:

* global state;
* configuration;
* candidate knowledge;
* application state;
* persistent storage.

Prefer returned values or clearly named mutation methods.

---

## Global variables

Do not introduce new mutable global state unless strictly necessary.

Where existing globals block testability, prefer dependency injection during incremental refactoring.

Do not replace simple constants with unnecessary configuration objects.

---

## Concurrency

Do not add goroutines merely to improve apparent performance.

Before introducing concurrency, verify:

* work is independent;
* ordering is not required;
* HH rate limits are respected;
* cancellation works;
* shared state is safe;
* tests cover race conditions.

Run:

```bash id="bhnm58"
go test -race ./...
```

for meaningful concurrency changes.

---

## Persistence

When moving persistence-related code:

* preserve serialization;
* preserve keys;
* preserve database schema;
* preserve migration compatibility;
* preserve existing records.

A package move must not silently invalidate persisted data.

---

## Configuration

Refactoring configuration code must preserve:

```text id="nt8t9g"
CLI > environment > defaults
```

where that is the existing precedence.

Do not rename environment variables without explicit requirement.

Do not move personal candidate data into source code.

---

## Tests during refactor

Prefer characterization tests before risky structural changes.

If behavior is unclear, first write a test proving current expected behavior.

Useful targets include:

* vacancy decision;
* application preflight;
* dry-run behavior;
* HH write gateway;
* test validation;
* chat safety;
* candidate fact handling.

---

## File splitting

Do not split files randomly by line count.

Split by responsibility.

Bad:

```text id="2wpu7m"
main_part1.go
main_part2.go
main_part3.go
```

Better:

```text id="8wlb7l"
config.go
application_orchestrator.go
vacancy_match.go
chat_orchestrator.go
hh_read.go
hh_write.go
```

If everything remains in the same package initially, that is acceptable.

Package extraction can happen later.

---

## Same-package extraction

For a large monolithic `package main`, a useful intermediate step is:

```text id="hugmi2"
package main
├── main.go
├── config.go
├── vacancy_match.go
├── application_orchestrator.go
├── hh_read_gateway.go
├── hh_write_gateway.go
└── chat_orchestrator.go
```

This can reduce risk before introducing package boundaries.

Do not force package extraction in the same change unless clearly beneficial.

---

## Comments

Do not preserve obsolete comments after moving behavior.

Comments should explain:

* invariants;
* safety reasoning;
* non-obvious external behavior.

Avoid comments that simply restate code.

---

## Naming

Prefer domain-specific names.

Bad:

```text id="g4d1dl"
Manager
Processor
Handler
Utils
Service2
```

Better:

```text id="a70a58"
ApplicationPreflight
VacancyMatcher
HHWriteGateway
EmployerReplyOrchestrator
CandidateFactRepository
```

---

## Refactor workflow

Use this sequence:

```text id="klm2jn"
inspect
↓
define boundary
↓
search usages
↓
add/confirm tests
↓
move smallest coherent unit
↓
build/test
↓
continue if needed
```

Do not wait until the end of a huge refactor to run tests.

---

## Verification

After Go changes run:

```bash id="2xmkue"
gofmt -w .

go test ./...

go test -race ./...

go vet ./...

go build ./...

git diff --check
```

If race tests are prohibitively slow for a trivial non-concurrent change, follow the project-level workflow, but any concurrency-sensitive refactor must include them.

---

## Diff review

Before completion inspect:

```bash id="ro6qcs"
git diff --stat
git diff
```

Look specifically for:

* accidental behavior changes;
* removed safety checks;
* config default changes;
* write-path movement;
* duplicated logic;
* stale imports;
* dead code.

---

## Completion checklist

A Go refactor is complete only when:

* requested structure improved;
* behavior is preserved unless explicitly changed;
* all usages of changed APIs were updated;
* HH read/write separation remains clear;
* dry-run remains enforced;
* persistent formats remain compatible;
* no unrelated cleanup was bundled unnecessarily;
* relevant tests pass;
* race-sensitive code passes race tests;
* build/vet/format checks pass;
* global AGENTS.md rules remain satisfied.
