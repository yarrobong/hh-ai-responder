# R10.4 — Candidate Acquisition AI Interpretation Boundary

## Before

Candidate clarification acquisition was already split between the root
workflow and `internal/usecase/candidateacquisition`. The root
`candidate_knowledge_acquisition.go` still owned the candidate-answer AI
adapter, prompt, structured schema, strict parser, and model-output
validation. `StructuredCandidateKnowledgeExtractor` called the root
`StructuredAIClient` directly.

The deterministic acquisition package already owned gap identity, answer
classification, mutation-intent construction, and the rule that an AI-origin
answer can only create a proposal requiring explicit review. Candidate
mutation and persistence remained root/backend responsibilities.

## Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| Gap detection and deduplication | `candidateacquisition` | unchanged | retain |
| Clarification lifecycle | root/storage + `candidateacquisition` values | unchanged | retain |
| Untrusted proposal DTO | `candidateacquisition` | unchanged | retain to avoid a package cycle |
| Candidate interpretation prompt | root | `candidateinterpretation` | move |
| Structured interpretation schema | root | `candidateinterpretation` | move |
| Strict JSON parsing | root | `candidateinterpretation` | move |
| Gap/target/reference validation | root + acquisition | `candidateinterpretation` delegates to acquisition policy | move AI boundary only |
| Semantic/business retry | root `AIClient` path | `candidateinterpretation.Service` | move |
| Completion transport retry | root/provider | R10.1 provider | unchanged |
| Answer-to-intent conversion | `candidateacquisition` | unchanged | retain deterministic owner |
| Proposal creation, confirmation, rejection | mutation workflow | unchanged | retain |
| Candidate persistence and semantic indexing | root/adapters | unchanged | retain |

## Interpretation use case

Package: `internal/usecase/candidateinterpretation`

Constructor:

```go
candidateinterpretation.NewService(
    candidateinterpretation.Dependencies{Completion: provider},
    candidateinterpretation.Options{Model: model, Attempts: attempts},
)
```

Input is a typed `Input` containing the current acquisition gap, the raw user
answer, and a minimal known-context identifier set for validating story
references. Known context is not copied into the prompt.

Output is `candidateinterpretation.Interpretation`, an alias of the neutral
untrusted proposal DTO owned by `candidateacquisition`. The service has no
Candidate writer, mutation method, lifecycle method, persistence dependency,
or timestamp authority.

Dependencies are limited to `internal/ports/llm.CompletionProvider`,
`internal/llm`, `candidateacquisition`, and the standard library. The package
does not import the root package, HH, storage, PostgreSQL, JSON adapters,
dashboard, config/CLI, semantic repositories, or a concrete LLM adapter.

## Prompt

Owner: `candidateinterpretation/prompt.go`.

The existing system prompt and user format were preserved exactly:

- one system message;
- one user message in `gap=<JSON>` then `answer=<raw answer>` order;
- the existing evidence-only, hypothesis-only, non-authority instructions;
- employer question and gap remain data, not instructions.

No unrelated Candidate profile or private contact data is added to the
prompt.

Model options remain `max_tokens=700` and `temperature=0.1`. The configured
model and caller context are passed through unchanged.

## Structured interpretation

Owner: `candidateinterpretation/schema.go` and `validation.go`.

The existing `candidate_knowledge_interpretation` strict JSON schema is
preserved, including the proposal fields, enums, required shape, and
`additionalProperties: false`.

Parsing preserves strict unknown-field rejection, trailing-data rejection,
proposal-count validation, target-gap validation, usage-context validation,
and story reference validation. Model output targeting an unrelated field
such as salary for a Kubernetes gap is rejected.

The package does not add unsupported numeric, salary, relocation, education,
language, seniority, or confidence fields. Those concepts remain governed by
their existing deterministic Candidate workflows; this extraction does not
broaden the interpretation surface.

## Trust boundary

AI authoritative for Candidate truth: **NO**.

AI can set `confirmed`: **NO**.

AI can set `verified`: **NO**.

Confidence can promote truth: **NO**. The current interpretation contract has
no confidence field, and the service does not add one.

The validator rejects a model `truth_status` other than the existing
hypothesis value, including `confirmed`. The result remains an untrusted
interpretation. `candidateacquisition.DecideAnswer` is still the boundary that
turns AI-origin output into an AI-actor proposal requiring user review; it does
not confirm Candidate knowledge. Actual confirmation and verification remain
owned by deterministic acquisition/mutation policy and trusted source
semantics.

## User evidence and exact values

The raw answer is passed unchanged to the interpreter and remains the evidence
stored by the existing clarification workflow. The interpreter's normalized
proposal is not substituted for raw provenance.

Explicit negative values such as `explicitly_not_used` remain representable in
the existing skill proposal contract. Structured negative confirmations still
go through the existing user-origin acquisition path; AI output alone cannot
make them confirmed.

The current AI interpretation schema only supports skill-usage and behavioral
story proposals. Exact 11-month experience, salary currency/period,
relocation, travel, availability, education, language, and numeric skill-level
semantics are not reinterpreted or broadened by R10.4. Existing deterministic
Candidate tests and workflows remain authoritative for those values.

## Unknown, ambiguity, and lifecycle

The interpreter handles one supplied answer only. It does not detect gaps,
rank or deduplicate questions, resolve unknowns, mark clarifications answered,
create proposal IDs, reconcile Candidate state, or choose a different gap.

An insufficient answer remains an acquisition outcome requiring clarification;
the interpreter cannot convert absence into false, zero, confirmed, or
verified knowledge. Proposal and unknown lifecycle transitions remain in
`candidateacquisition` and `candidatemutation`.

## Retry and failure behavior

Business attempts are owned by `candidateinterpretation.Service`. Malformed
JSON, trailing data, unsupported fields/enums, target mismatches, illegal
authority claims, and invalid references consume the configured semantic
attempt budget. A valid first response uses one business call.

Provider errors are returned as provider/system errors and do not create an
interpretation or multiply business attempts. Context cancellation returns
the caller's cancellation and stops further semantic attempts. Transport
retry remains exclusively owned by the R10.1 completion provider.

No error path mutates Candidate state.

## Root compatibility

`candidate_interpretation_compat.go` retains the old root
`StructuredCandidateKnowledgeExtractor` shape and test helper names as thin
delegates. It adapts legacy `StructuredAIClient` callers to
`CompletionProvider`; it contains no prompt, schema, parser, or interpretation
algorithm.

The contextual adapter is used by the acquisition service when available, so
the caller context reaches the completion provider. The old no-context method
exists only for source compatibility with legacy callers.

## Mutation and persistence

Direct Candidate mutation from interpretation: **NO**.

The existing flow remains:

```text
interpretation result
  -> candidateacquisition deterministic validation/conversion
  -> typed mutation intent / untrusted proposal
  -> existing Candidate mutation service
  -> existing JSON or PostgreSQL persistence
  -> optional existing post-commit semantic indexing
```

No storage files, migrations, SQL, JSON persistence semantics, optimistic
versioning, write preflight, or semantic post-commit behavior changed.

## Other AI consumers

R10.2 employer reply, R10.3 vacancy analysis, and R10.1 completion transport
remain unchanged by this stage.

Remaining root AI consumers for a later stage include:

- `AIClient.GenerateLetter` and the evaluated/semantic cover-letter path;
- `AIClient.SolveTests` and test-answer structured parsing;
- generic root `Chat`/`ChatStructured` compatibility methods and their
  remaining direct callers;
- legacy root employer-reply/vacancy compatibility composition where retained.

Cover-letter and test-answer extraction were deliberately not started.

## Tests

Added `internal/usecase/candidateinterpretation/service_test.go` covering:

- prompt ordering and exact completion options;
- valid first response;
- invalid-then-valid and all-invalid retry counts;
- provider failure without semantic retry multiplication;
- cancellation;
- strict trailing-data rejection;
- unrelated target-field rejection;
- confirmed truth rejection;
- story reference validation;
- preservation of explicit negative proposal values.

Existing candidate acquisition, mutation, domain, persistence, employer-reply,
vacancy-analysis, and completion tests remain passing.

## Verification

- `gofmt -w .`: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- focused candidate interpretation/acquisition/mutation/domain tests: PASS
- employer reply, vacancy analysis, and completion adapter regressions: PASS
- Docker build: SKIPPED — Docker CLI is installed but the daemon is not running
- live HH writes: **0**

## R10 status

R10.1 Completion transport: **EXTRACTED**

R10.2 Employer reply: **EXTRACTED**

R10.3 Vacancy analysis: **EXTRACTED**

R10.4 Candidate interpretation: **EXTRACTED**

R10: **NEEDS R10.5**. Substantial root AI workflows remain for cover letters,
test answering, and generic/legacy AI composition. R10.4 does not justify
marking the broader R10 series complete.

Ready for next stage: **R10.5 — Remaining AI Workflows**, not R11.
