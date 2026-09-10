# R10.6c — Final Residual AI Cleanup + R10 Closure

## Before

The pre-change audit found all eight expected substantial AI workflows already
extracted into importable usecases. The remaining root AI surface consisted of
the composition-owned `AIClient`, typed-usecase compatibility facades, and the
legacy `StructuredAIClient` bridge used by older tests/callers.

The residual issue was the legacy root `AIClient.Chat*` implementation: its
private `chat` helper combined provider delegation with validator-driven
semantic retry. No current live workflow used that retry path; typed usecases
already owned their own parsing, validation, and semantic retry budgets.

Root OpenAI-shaped request/response fixtures were also production Go types even
though they were used only by root-package tests. Runtime protocol DTOs were
already implemented privately by `internal/adapters/llm/openai`.

## Complete inventory

| Symbol | Runtime callers | Test callers | Classification | Owner | Action |
|---|---|---|---|---|---|
| `AIClient` | composition; root workflow facades | broad root regression tests | EXTRACTED USECASE COMPATIBILITY | typed usecases plus root composition | retained as facade; no business algorithm |
| `NewAIClient` | CLI/composition roots | provider regression tests | PURE COMPLETION COMPOSITION | `ports/llm.CompletionProvider`, OpenAI adapter | retained; constructs the sole completion provider |
| `AIClient.Complete` | typed services through provider assertion | facade/provider tests | PURE COMPLETION COMPOSITION | `ports/llm.CompletionProvider` | one-call delegation with caller context |
| `AIClient.Chat` | none in live workflows | root diagnostics test | COMPATIBILITY ONLY | `AIClient.legacyCompletion` | retained as one-call facade |
| `AIClient.chat` | none | none | DEAD REMOVED | typed usecases | removed |
| `AIClient.ChatStructured` | none in live workflows | legacy structured callers | COMPATIBILITY ONLY | typed usecase | retained as one-call `json_object` facade |
| `AIClient.ChatStructuredWithSchema` | legacy fallback only | root provider test and fakes | COMPATIBILITY ONLY | typed usecase | retained as one-call schema translation |
| `AIClient.chatStructured` | none | none | DEAD REMOVED | typed usecases | removed |
| `StructuredAIClient` | root composition compatibility field | orchestrator fakes | COMPATIBILITY ONLY | `CompletionProvider` | retained for source compatibility |
| `legacyCompletionProvider` | compatibility fallback construction | legacy fake-provider tests | COMPATIBILITY ONLY | `CompletionProvider` | retained as mechanical bridge |
| `NewAIReplyOrchestrator(...any)` | root workflow composition | orchestration tests | ROOT WORKFLOW GLUE | typed usecases and stores | retained; no AI algorithm |
| root `ChatCompletion*` DTOs | none | root HTTP fixtures | TEST ONLY | OpenAI adapter DTOs at runtime | moved to `ai_completion_fixture_test.go` |
| root prompt/schema/parser helpers | no independent runtime owner | root compatibility tests | EXTRACTED USECASE COMPATIBILITY | corresponding typed usecase | retained only as delegates/aliases |

There is no `RUNTIME BUSINESS BUG` classification in the final inventory.

## Workflow ownership

| Workflow | Prompt | Schema/parser | Retry | Safety | Completion | Root |
|---|---|---|---|---|---|---|
| Employer reply | `internal/usecase/employerreply` | `employerreply` | `employerreply.Service` | `employerreply` plus write defense | `CompletionProvider` | context/store/write glue |
| Vacancy analysis | `internal/usecase/vacancyanalysis` | `vacancyanalysis` | `vacancyanalysis.Service` | deterministic rules/usecase validation | `CompletionProvider` | workflow facade/glue |
| Candidate interpretation | `internal/usecase/candidateinterpretation` | `candidateinterpretation` | `candidateinterpretation.Service` | interpretation validation and mutation boundary | `CompletionProvider` | compatibility extractor |
| Cover letter | `internal/usecase/coverletter` | `coverletter` | none beyond provider transport | `coverletter` plus write defense | `CompletionProvider` | input mapping/persistence glue |
| HH test answering | `internal/usecase/testanswer` | `testanswer` | `testanswer.Service` | strict validation; submit outside | `CompletionProvider` | task mapping/submission glue |
| Follow-up draft | `internal/usecase/followupdraft` | usecase parser | `followupdraft.Service` | eligibility and draft validation | `CompletionProvider` | freshness/persistence glue |
| Application answer | `internal/usecase/applicationanswer` | application service/parser | `applicationanswer.Service` | fact/story validation | `CompletionProvider` | persistence/workflow glue |
| Legacy auto-chat | `internal/usecase/autochatreply` | plain-text policy review | one business attempt; transport retry only | autochat/policy; send/leave outside | `CompletionProvider` | HH capability glue |

No workflow row has a root prompt owner, root business retry owner, or root
authoritative AI-safety algorithm.

## AIClient final state

Purpose: compose the configured OpenAI-compatible adapter and preserve the
pre-R10 root API while typed usecases own AI semantics.

Runtime business logic: NONE in the generic completion surface. Facade methods
delegate to the corresponding typed usecase.

Methods:

- `Complete`: PURE COMPOSITION; one call to the configured provider with the
  supplied context.
- `Chat`: COMPATIBILITY ONLY; legacy request conversion and one delegation.
- `ChatStructured`: COMPATIBILITY ONLY; `json_object` request and at most one
  caller callback, without retry.
- `ChatStructuredWithSchema`: COMPATIBILITY ONLY; schema/format conversion and
  one delegation, without retry or business parsing.
- `EvaluateVacancy`, `GenerateLetter*`, and `SolveTests`: EXTRACTED USECASE
  COMPATIBILITY facades.

The former generic `chat` and `chatStructured` retry implementation is gone.

## Generic compatibility

`Chat` and `ChatStructured*` remain only for source compatibility. Their
implementation is a mechanical request conversion plus one provider call. A
structured callback is invoked at most once; semantic retry is not part of the
compatibility layer.

`StructuredAIClient` is a compatibility constructor/fake seam. Normal runtime
composition uses `CompletionProvider` directly because `*AIClient` implements
that port. `legacyCompletionProvider` is the mechanical fallback for older
implementations that expose only the legacy structured method.

## Final generic method table

| Method/symbol | Final status |
|---|---|
| `AIClient` | EXTRACTED USECASE COMPATIBILITY |
| `NewAIClient` | PURE COMPOSITION |
| `AIClient.Complete` | PURE COMPOSITION |
| `AIClient.Chat` | COMPATIBILITY ONLY |
| `AIClient.chat` | REMOVED |
| `AIClient.ChatStructured` | COMPATIBILITY ONLY |
| `AIClient.ChatStructuredWithSchema` | COMPATIBILITY ONLY |
| `StructuredAIClient` | COMPATIBILITY ONLY |
| `legacyCompletionProvider` | COMPATIBILITY ONLY |
| `NewAIReplyOrchestrator(...any)` | COMPATIBILITY ONLY composition debt |

Required final status: zero runtime business bugs in generic methods.

## Completion boundary

`internal/ports/llm.CompletionProvider` is the sole general completion
capability. `internal/adapters/llm/openai.Provider` is the sole current
completion transport implementation and owns provider/transport retry.

Root HTTP transport: NONE. Alternative completion abstractions: NONE in
runtime usecases. Embedding remains separate behind its existing embedding
port and adapter.

## Root prompt/schema/parser/retry audit

Runtime root prompt construction: NONE. Root helper names such as
`buildVacancyEvaluationPrompt`, `buildLetterSystemPrompt`,
`buildTestSystemPrompt`, and `aiReplySystemPrompt` delegate to the matching
usecase builders and are retained only for compatibility tests/callers. The
shared `candidate_communication.md` is a composition-owned embedded profile
asset passed into the auto-chat usecase; it is not a root prompt algorithm.

Runtime root schemas: NONE. Root schema helpers only convert typed usecase
formats to legacy `ChatJSONSchema` values.

Runtime root AI parsers: NONE. Root parsing helpers delegate to typed usecase
parsers/validators. Non-LLM HH/storage JSON parsing is outside this audit.

Runtime root semantic/business retries: NONE. Provider transport retry remains
in the OpenAI adapter; business retries remain in the eight typed usecases.

## Candidate safety

Root authoritative AI safety algorithm: NONE.

Usecase validators own employer-reply facts/drafts, application-answer stories,
cover-letter claims, test answers, candidate interpretation, and vacancy hard
requirements. Root `ValidateAIDraft` and `validateStoryClaims` are delegates.
`HHWriteGateway.validateDraftText` remains write-side defense and was not
removed or weakened.

## Other workflow audit

Conversation summaries and candidate claims are persisted/read domain data. No
runtime LLM-generated summary or claim-extraction workflow was found: this is
NON-AI / FALSE POSITIVE for the residual AI audit.

Other direct model callers: none. Every runtime `.Complete` call is in one of
the eight typed usecases; adapter tests call the provider only to verify
transport behavior.

Substantial unextracted workflow: NONE.

## Compatibility debt

`NewAIReplyOrchestrator` performs dependency discovery, store/context wiring,
typed usecase construction, and compatibility fallback selection. It contains
no prompt, schema, parser, semantic retry, candidate-fact safety algorithm, or
AI decision algorithm.

This is explicitly **COMPATIBILITY-ONLY COMPOSITION DEBT** reserved for later
composition cleanup. R10.6c does not migrate the full command/composition root.

## Dead code removed

- Removed the root private `AIClient.chat` generic semantic retry engine.
- Removed the root private `AIClient.chatStructured` helper.
- Moved OpenAI-shaped request/response fixtures to
  `ai_completion_fixture_test.go`.

## Tests

Focused suites cover `autochatreply`, `applicationanswer`, `followupdraft`,
`employerreply`, `vacancyanalysis`, `candidateinterpretation`, `coverletter`,
`testanswer`, the LLM values/port, and the OpenAI adapter. Root workflow,
dry-run, write-gateway, preflight, reconciliation, and compatibility tests
remain included in the full suite. No live HH write was performed.

## Dependencies

All eight AI usecases depend on the infrastructure-neutral completion port or
lower pure/domain packages. They do not import the concrete OpenAI adapter,
root package, HH write capability, or concrete storage. The OpenAI adapter
keeps its protocol DTOs private.

## Behavior

Typed runtime completion behavior, provider retry behavior, model/options,
temperature, token limits, and caller-context propagation are unchanged. The
only generic-path change is removal of an unused hidden semantic retry. All
business retry ownership remains in the typed usecases; transport retry
remains in the OpenAI adapter.

HH reads, HH writes, approval, preflight, reconciliation, storage, and
dashboard behavior were not refactored.

## Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED — Docker CLI present, daemon unavailable

LIVE HH WRITES: 0

## R10 final status

R10.1 Completion transport: EXTRACTED

R10.2 Employer reply: EXTRACTED

R10.3 Vacancy analysis: EXTRACTED

R10.4 Candidate interpretation: EXTRACTED

R10.5a Cover letter: EXTRACTED

R10.5b Test answer: EXTRACTED

R10.6a Follow-up draft: EXTRACTED

R10.6b1 Application answer: EXTRACTED

R10.6b2 Legacy auto-chat: EXTRACTED

R10.6c Final cleanup: COMPLETE

Substantial runtime AI workflows in root: NONE

R10: COMPLETE

## Ready for next stage

R11.1 — HH Write Transport / Capability Boundary: READY

R10.6c stopped here. No R11, HH write, storage migration, dashboard,
frontend, or full composition migration work was started.
