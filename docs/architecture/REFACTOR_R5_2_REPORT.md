# Stage R5.2 — Candidate Resolution + Safe Context Boundary

## Before

The root package owned CandidateContext, CandidateContextResolver, deterministic CandidateFactResolver logic, employer-question classification, fact matching, and safe-context assembly. The resolver assembled a canonical candidate from compatibility inputs, while storage and profile/KB I/O remained in root.

| Symbol | Classification | Decision |
| --- | --- | --- |
| CandidateContext, ResolvedFact, status vocabulary | cross-domain resolution output | moved to internal/usecase/candidatecontext; root aliases retained |
| CandidateContextResolver | compatibility adapter plus policy | adapter retained; deterministic policy delegated |
| classifyEmployerMessage | candidate-question classification | moved; root wrapper delegates |
| resolveAtomicFacts and technology facts | candidate fact policy | moved; root wrapper delegates |
| CanonicalEmployerSafeProjection | candidate-domain safety boundary | retained in internal/candidate and reused |
| CandidateKnowledgeBase/profile stores | storage/assembly | not moved |
| semantic retrieval, AI, HH parsing, workflow, write gateway | infrastructure/workflow | not moved |
| terminal state and reply requirement | conversation workflow policy | retained in root |

## New boundary

Package: internal/usecase/candidatecontext

Files:

- doc.go — package boundary and policy scope;
- types.go — detached context/output types, statuses, and typed inputs;
- resolver.go — deterministic safe-context selection;
- facts.go — employer-question classification and atomic fact resolution;
- resolver_test.go — synthetic characterization tests.

Project dependencies: internal/candidate only. The extracted policy does not need internal/vacancy or internal/conversation.

## Input model

The use case receives a prepared candidate.Candidate and typed ResolveInput:

    type ResolveInput struct {
        Query           string
        History         []HistoryMessage
        EmployerMessage bool
    }

The root adapter may assemble the canonical candidate from legacy profile/knowledge values. The use case performs no file, JSON-store, database, HH, or network access.

## Output model

Resolution statuses are unchanged:

| Status | Meaning | Employer-facing use | Clarification/draft effect |
| --- | --- | --- | --- |
| ANSWERABLE | confirmed/verified evidence answers the fact | bounded explicit value may be used | usable for a bounded draft |
| PARTIALLY_ANSWERABLE | base evidence exists but a qualifier/detail is missing | only bounded claims may be used | clarification may remain warranted |
| UNKNOWN | no safe evidence establishes the fact | must not be presented as a fact | factual candidate input remains required |
| RESTRICTED | evidence or candidate policy forbids the claim | must not be claimed | blocks the restricted claim |

Unknown is not false. A negative relocation preference is still an answerable confirmed fact. Vacancy requirements do not create candidate clarifications through the vacancy-context path.

## Candidate dependency

Safe projection API reused: candidate.CanonicalEmployerSafeProjection.

The use case interprets the safe projection and does not redefine the candidate truth/provenance predicate. Duplicated truth filtering: NONE. Hypotheses, unknowns, proposals, events, and unconfirmed profile facts are not employer evidence.

## Vacancy integration

Dependency: NO.

The root adapter retains already-read vacancy text handling. The extracted package does not parse vacancies, search HH, score vacancies, or perform application preflight. Vacancy requirements remain analysis input rather than fabricated candidate facts.

## Conversation integration

Dependency: NO.

The package classifies whether employer text is a candidate-fact question and resolves safe candidate facts. Terminal state, reply requirement, follow-up, eligibility, conversation state, and employer workflow remain root policy. History is only a relevance hint and never evidence.

## Root compatibility

Adapters:

- CandidateContextResolver retains existing constructors and assembles the canonical candidate.
- ResolveForVacancy, ResolveForVacancyContext, ResolveForEmployerMessage, and GetEmployerSafeContext retain their signatures.

Aliases:

- root context DTOs, ResolvedFact, statuses, and employer intent values alias the use-case types.

Delegating wrappers:

- root classification, instruction confirmation, atomic fact resolution, and context matching helpers delegate to the new package.

Mirrored resolver logic: NONE. Remaining root conversation helpers inspect conversation aggregates and have different semantics.

## Resolution contracts

| Contract | Result |
| --- | --- |
| Confirmed Django / known skill | PASS |
| Docker with insufficient production depth | PASS — partial |
| Unknown Kubernetes | PASS — not inferred from Docker/Linux |
| 11 months | PASS — exactly 11 месяцев |
| 1–3 year requirement with 11 months | PASS — no fabricated one-year fact |
| Salary preference | PASS |
| Negative relocation preference | PASS |
| Hypothesis safety | PASS |
| Unknown versus false | PASS |
| Restricted/forbidden claim | PASS |
| Interview/status/instruction/courtesy messages | PASS — not candidate fact questions |

## Clarification regression

Generic false clarification: NOT REINTRODUCED.

Stage 15 behaviors: PASS. Existing root tests continue to cover profile/KB assembly and clarification lifecycle; the new package adds focused synthetic tests for answerability, partial evidence, unknowns, restrictions, duration, preferences, and non-question messages.

## Write safety

- Relevant knowledge snapshot: UNCHANGED.
- Approval stale semantics: UNCHANGED.
- Targeted read-only preflight: UNCHANGED.
- Forbidden-claim validation: UNCHANGED; contextual restrictions remain surfaced.
- Manual approval, nonce/idempotency, reconciliation, and HH transport: UNCHANGED.
- HH_WRITE_ENABLED=false and HH_DRY_RUN=true: preserved.
- LIVE HH WRITES: 0.

## Dependencies

go list ./...: PASS.

go list -deps ./internal/candidate: standard library plus the package itself; no project-package dependency.

go list -deps ./internal/usecase/candidatecontext: standard library, internal/candidate, and the package itself; no storage, HH, AI, dashboard, configuration, CLI, application, vacancy, or conversation dependency.

## Flaky test observation

Stage 22 timing failure repeated during R5.2: NO. No timing assertion was changed.

## Size

Before:

- root candidate_context.go: 144 LOC;
- root candidate_context_resolver.go: 582 LOC;
- root candidate_fact_resolver.go: 491 LOC;
- combined root resolution area: 1,217 LOC.

After:

- root compatibility files: 59 + 219 + 109 = 387 LOC;
- candidatecontext production files: 909 LOC;
- focused package tests: 154 LOC.

The larger use-case total reflects moving the implementation and making the boundary explicit; root no longer contains a second algorithm.

## Behavior

| Area | Result |
| --- | --- |
| Candidate canonical model | UNCHANGED |
| Truth/provenance vocabulary | UNCHANGED |
| Safe projection | UNCHANGED and reused |
| Candidate fact resolution | UNCHANGED semantics; new owner |
| 11-month, unknown, partial, restricted handling | UNCHANGED |
| Clarifications | UNCHANGED lifecycle |
| Mutation/acquisition | UNCHANGED |
| Semantic retrieval / AI | UNCHANGED |
| Vacancy / application | UNCHANGED |
| Conversation workflow | UNCHANGED |
| HH reads/writes | UNCHANGED; live writes 0 |
| Dashboard / JSON / PostgreSQL / CLI/config | UNCHANGED |

## Verification

- gofmt: PASS
- go test -count=1 ./...: PASS
- focused go test ./internal/usecase/candidatecontext: PASS
- go list ./...: PASS
- dependency-boundary checks: PASS
- Docker build: SKIPPED — Docker CLI is installed but the daemon is unavailable.
- live HH writes: 0

- go test -race ./...: PASS
- go vet ./...: PASS
- go build ./...: PASS
- git diff --check: PASS
- node --check web/app.js: PASS
- Docker build: SKIPPED because the Docker daemon is unavailable.

## Architectural debt

The root package still owns the compatibility assembler and root helper names used by semantic, conversation, and dashboard consumers. They are thin delegating adapters or workflow-specific policy and should be removed only as those consumers cross later boundaries. The storage-shaped compatibility constructor remains by design for this staged refactor.

## Ready for next stage

R4.3b — Conversation Policy Boundary: READY.
