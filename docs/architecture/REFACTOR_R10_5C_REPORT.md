# Stage R10.5c — Residual AI Compatibility Cleanup + R10 Closure

## Before

The pre-change audit was run with `git status --short`, `git diff`, and
`git diff --cached`. The worktree already contained extensive user changes;
they were preserved. No pre-existing file was reverted or reformatted.

The repository-wide search found the following residual AI surfaces:

- `AIClient` and `NewAIClient` in `main.go`.
- Generic `Chat`, `ChatStructured`, and `ChatStructuredWithSchema` methods in
  `main.go`.
- `StructuredAIClient` in `ai_reply_orchestrator.go`.
- Root compatibility adapters for vacancy analysis, candidate
  interpretation, cover letters, employer replies, and test answers.
- A live legacy HH chat path in `AutoRespondChats`.
- A live follow-up generation path in `AIReplyOrchestrator.callDecision`.
- Root test-fixture wire types in `ai_completion_compat.go`.

The extracted five workflows remain structurally owned by their importable
use-case packages. However, the audit found two additional substantial
runtime workflows, so the requested cleanup cannot safely delete or reroute
their compatibility surfaces.

## Complete residual inventory

| Symbol/caller | Classification | Owner | Action |
|---|---|---|---|
| `NewAIClient` (`main.go:2025`) | PURE COMPLETION COMPOSITION | root composition | Retain while the current root composition remains in place. It constructs the OpenAI adapter and preserves configuration behavior. |
| `AIClient` (`main.go:1592`) | PURE COMPLETION COMPOSITION | root composition/compatibility | Retain; it stores provider configuration and implements the current compatibility seam. |
| `AIClient.Complete` (`employer_reply_compat.go:75`) | PURE COMPLETION COMPOSITION | `ports/llm.CompletionProvider` | Retain as a one-call delegation to the constructed provider. |
| `AIClient.Chat` (`main.go:2069`) | SUBSTANTIAL UNEXTRACTED WORKFLOW | legacy HH auto-chat path (`main.go:1089-1220`) | Keep. Removing it would break a live path that has not been extracted. R10 is blocked. |
| `AIClient.ChatStructured` (`main.go:2077`) | DEAD | none found | Not removed because the live generic compatibility surface is retained pending extraction of the additional workflows. Revisit with the blocker extraction. |
| `AIClient.ChatStructuredWithSchema` (`main.go:2084`) | SUBSTANTIAL UNEXTRACTED WORKFLOW / COMPATIBILITY | follow-up path and legacy adapter bridge | Keep. It is reached by `callDecision` and by `legacyCompletionProvider`. |
| `StructuredAIClient` (`ai_reply_orchestrator.go:25`) | RUNTIME COMPATIBILITY | root orchestrator compatibility | Keep. It is the current test/legacy input seam and is still used by follow-up generation and candidate-interpretation compatibility. |
| `legacyCompletionProvider` (`employer_reply_compat.go:69`) | EXTRACTED USECASE COMPATIBILITY | root compatibility | Keep until all callers can provide `CompletionProvider` directly. It performs mechanical request conversion only. |
| `AIClient.EvaluateVacancy` (`vacancy_match.go:244`) | EXTRACTED USECASE COMPATIBILITY | `vacancyanalysis` | Retain as a typed delegate. It constructs the use case, delegates analysis, and preserves legacy result conversion/structured-field merging. |
| `AIClient.GenerateLetter` (`cover_letter_compat.go:89`) | EXTRACTED USECASE COMPATIBILITY | `coverletter` | Retain as a typed delegate. |
| `AIClient.GenerateLetterWithEvaluation` (`vacancy_match.go:276`) | EXTRACTED USECASE COMPATIBILITY | `coverletter` | Retain as a typed delegate. |
| `AIClient.GenerateLetterWithEvaluationAndSemantic` (`vacancy_match.go:280`) | EXTRACTED USECASE COMPATIBILITY | `coverletter` | Retain as a typed delegate. |
| `AIClient.SolveTests` (`test_answer_compat.go:55`) | EXTRACTED USECASE COMPATIBILITY | `testanswer` | Retain as a typed adapter; validation remains in `testanswer` and at the HH write boundary. |
| `buildVacancyEvaluationPrompt` and schema/parser delegates | EXTRACTED USECASE COMPATIBILITY | `vacancyanalysis` | Retain for root tests/source compatibility; no implementation remains in root. |
| candidate interpretation prompt/schema/parser delegates | EXTRACTED USECASE COMPATIBILITY | `candidateinterpretation` | Retain for root compatibility; no implementation remains in root. |
| cover-letter prompt/validation delegates | EXTRACTED USECASE COMPATIBILITY | `coverletter` | Retain for root compatibility; no implementation remains in root. |
| test-answer prompt/schema/parser delegates | EXTRACTED USECASE COMPATIBILITY | `testanswer` | Retain for root compatibility; no implementation remains in root. |
| `ChatCompletion*` and `ChatJSONSchema` types (`ai_completion_compat.go`) | TEST ONLY | OpenAI adapter characterization tests | Retain as root test fixtures. No runtime root HTTP DTO use was found. |
| `AIClient.chat` (`main.go:2100`) | SUBSTANTIAL UNEXTRACTED WORKFLOW | generic surface used by legacy chat/follow-up compatibility | Keep pending extraction. It currently contains compatibility request construction plus generic semantic validation/retry. |
| `AIReplyOrchestrator.callDecision` (`ai_reply_orchestrator.go:634`) | SUBSTANTIAL UNEXTRACTED WORKFLOW | follow-up generation | Do not delete or route silently. It owns follow-up-specific task construction, completion, parsing, and output checks. |
| `AutoRespondChats` (`main.go:1089`) | SUBSTANTIAL UNEXTRACTED WORKFLOW | legacy HH auto-chat | Do not route silently. It owns HH inbox context, reply-option handling, chat-specific prompt rules, review classification, and send/leave behavior. |
| `candidateStoriesPrompt` (`candidate_stories.go:135`) | TEST ONLY / COMPATIBILITY | cover-letter prompt characterization | No runtime caller found outside tests; it is unrelated to transport ownership. |
| conversation summaries and candidate claims | NON-AI / FALSE POSITIVE | conversation/domain persistence | Repository code stores, validates, and derives these values from recorded messages; no runtime LLM summary/claim extraction path was found. |

## AI workflow ownership

| Workflow | Prompt | Schema | Parser | Business retry | Safety/policy | Root role |
|---|---|---|---|---|---|---|
| Employer reply | `internal/usecase/employerreply` | `internal/usecase/employerreply` | `internal/usecase/employerreply` | `internal/usecase/employerreply` | `employerreply` + canonical candidate context/conversation policy | Context adaptation, draft/clarification persistence, legacy translation. |
| Vacancy analysis | `internal/usecase/vacancyanalysis` | `internal/usecase/vacancyanalysis` | `internal/usecase/vacancyanalysis` | `internal/usecase/vacancyanalysis` | `vacancyanalysis` plus deterministic root application gate | Input/result adaptation and application orchestration. |
| Candidate interpretation | `internal/usecase/candidateinterpretation` | `internal/usecase/candidateinterpretation` | `internal/usecase/candidateinterpretation` | `internal/usecase/candidateinterpretation` | candidate acquisition/mutation boundary | Legacy input/output adaptation only. |
| Cover letter | `internal/usecase/coverletter` | none | plain-text result validation in `coverletter` | none beyond provider transport | `coverletter` plus root story safety check | Application-context adaptation and draft persistence. |
| HH test answering | `internal/usecase/testanswer` | `internal/usecase/testanswer` | `internal/usecase/testanswer` | `internal/usecase/testanswer` | `testanswer` plus HH write-side validation | Legacy task/result conversion only. |
| Legacy HH auto-chat | root `candidate_communication.go` and `AutoRespondChats` | none | plain-text response/review logic in root | generic `AIClient.chat` retry | root chat review classifier plus HH send/leave path | **Unextracted substantial workflow; R10 blocker.** |
| Follow-up generation | root `PrepareFollowUp`/`callDecision`, using employer-reply schema/prompt | employer-reply schema reused through root | root call path delegates parsing to employer-reply but owns the workflow call | root `AIClient.chat`/`ChatStructuredWithSchema` path | follow-up eligibility, draft fingerprint/staleness, high-risk and fact checks | **Unextracted substantial workflow; R10 blocker.** |

## AIClient

Remaining purpose: root composition and source compatibility around the single
`ports/llm.CompletionProvider`, plus still-live legacy workflow entry points.

Runtime business logic: **present in the two blocker paths described above**.
The generic `chat` helper also currently performs validator-driven semantic
retries. This cannot be moved or removed without first extracting those
workflows, because doing so would change call counts and error behavior.

Completion transport: R10.1, through
`internal/adapters/llm/openai` implementing
`internal/ports/llm.CompletionProvider`.

Methods:

- `Chat`: runtime legacy HH auto-chat caller; not removable yet.
- `ChatStructured`: no direct caller found; dead pending safe compatibility
  cleanup after the blocker extraction.
- `ChatStructuredWithSchema`: runtime follow-up caller and legacy bridge;
  not removable yet.
- `SolveTests`: compatibility-only typed delegate to `testanswer`.
- `GenerateLetter*`: compatibility-only typed delegates to `coverletter`.
- `EvaluateVacancy`: compatibility-only typed delegate to `vacancyanalysis`.
- `Complete`: thin provider delegation and the current composition seam.

## Generic methods

`Chat` is **RUNTIME BUSINESS BUG / SUBSTANTIAL UNEXTRACTED WORKFLOW** for
ownership purposes, not because its transport code is incorrect: the caller
supplies and depends on a complete legacy employer-chat algorithm.

`ChatStructured` is **DEAD** by repository-wide runtime search. It has no
direct runtime or test caller.

`ChatStructuredWithSchema` is **COMPATIBILITY ONLY plus a live follow-up
dependency**. The follow-up path calls it directly; `legacyCompletionProvider`
also adapts typed use-case calls to it.

`StructuredAIClient` is **RUNTIME COMPATIBILITY**. No importable use-case
depends on it, but root `AIReplyOrchestrator` and the candidate interpretation
compatibility adapter still accept it.

There is no remaining root alternative completion HTTP transport. The only
general transport is `ports/llm.CompletionProvider` with the OpenAI-compatible
adapter.

## Residual runtime callers

### Previous `AIClient.Chat` runtime caller

The caller at `main.go:1177` is the legacy `AutoRespondChats` path. It is not a
mechanical generic call: the surrounding code builds the candidate/employer
prompt, handles text-button options, loads the HH chat history, applies
chat-specific review rules, and then can call `SendChatMessage` or
`LeaveChat`.

Comparison with R10.2:

| Concern | Legacy auto-chat | R10.2 `employerreply` |
|---|---|---|
| Input | `ChatToReply` plus live HH `ChatDataResponse` | persisted `ConversationContext` built from local conversation state |
| Output | plain text immediately considered for HH send | typed decision/draft/clarification result |
| Options | exact text-button branch | no equivalent legacy button contract |
| Policy | prompt rules and `chatReplyReviewReasonForContext` | extracted conversation policy and service preflight |
| Persistence | direct chat result/write path | draft and clarification persistence in root orchestration |
| Write path | may send/leave HH chat | service itself has no HH write capability |

These semantics are not proven equivalent. It must be separately extracted or
explicitly retired in a future stage; it must not be silently routed to
`employerreply`.

### Follow-up caller

`PrepareFollowUp` performs deterministic eligibility and fresh-state checks,
constructs a follow-up-specific payload/task, calls `callDecision`, validates
high-risk content and candidate facts, rechecks eligibility, and persists a
fingerprinted draft. This is a distinct workflow even though it currently
reuses the employer-reply decision shape. It requires a follow-up owner before
the generic structured compatibility surface can be removed.

Behavior: unchanged. No HH write was executed during this audit.

## Employer reply

Owner: `internal/usecase/employerreply`.

Root: context adaptation, compatibility aliases, draft/clarification
persistence, and orchestration.

Duplicate business algorithm: none for the typed R10.2 path; the separate
legacy auto-chat and follow-up paths remain blockers and are listed above.

## Vacancy analysis

Owner: `internal/usecase/vacancyanalysis`.

Root: typed compatibility delegate and deterministic application orchestration.

Duplicate business algorithm: none found in runtime root code.

## Candidate interpretation

Owner: `internal/usecase/candidateinterpretation`.

Root: typed compatibility adapter and legacy value conversion.

Duplicate business algorithm: none found in runtime root code.

## Cover letter

Owner: `internal/usecase/coverletter`.

Root: typed compatibility delegates and application-context composition.

Duplicate business algorithm: none found in runtime root code.

## Test answer

Owner: `internal/usecase/testanswer`.

Root: typed task/result adapter; HH write-side validation remains outside the
AI generation service.

Duplicate business algorithm: none found in runtime root code.

## Other AI workflow audit

Conversation summary: no runtime LLM summary generation found. Conversation
summary values are persisted domain data and are treated as untrusted input.

Claim extraction: no runtime LLM claim-extraction workflow found. Candidate
claims are recorded/validated from conversation messages and canonical
candidate data.

Other prompts:

- The root `candidate_communication.go` prompt belongs to legacy HH auto-chat
  and is a substantial unextracted workflow.
- The follow-up task string in `follow_up_service.go:142` belongs to the
  separate follow-up generation workflow.
- Prompts for the five extracted workflows are owned by their use-case
  packages; root helpers only delegate to them.

Substantial unextracted workflows:

1. Legacy HH auto-chat response generation and send/leave orchestration.
2. Follow-up draft generation.

Therefore: **R10 NOT COMPLETE — additional extraction required**.

## Completion boundary

CompletionProvider: `internal/ports/llm.CompletionProvider`.

OpenAI adapter: `internal/adapters/llm/openai`.

Root HTTP completion: none found.

Provider DTO duplication: `ai_completion_compat.go` contains root structs used
by OpenAI adapter characterization tests only. No runtime root HTTP encoder or
decoder was found. They should be migrated with those tests when the remaining
generic compatibility surface is retired.

Embedding remains separate through `EmbeddingProvider`; no merge was made.

## Compatibility debt

`NewAIReplyOrchestrator(...any)` remains a composition-only compatibility
wrapper. It discovers root stores/builders and wires typed use-case services;
it is not changed in this stage because the task explicitly defers broad
composition migration.

Other untyped wrappers: root compatibility adapters accept legacy root values,
but the five importable use cases accept typed inputs and depend only on the
completion port or lower-level domain values.

Why allowed: deleting these seams now would either break live legacy behavior
or force an unrequested dashboard/CLI and HH-chat composition migration.

## Tests

Existing root compatibility and use-case tests passed before this report was
added. No test was changed and no live HH write was invoked.

## Size

Root AI compatibility before: existing root `AIClient`, structured seam,
compatibility adapters, and legacy runtime callers.

After: unchanged, because the ownership audit found live unextracted
workflows and the task requires stopping before deleting their compatibility
surface.

Dead code removed: none.

## Dependencies

The five extracted use cases depend on `internal/ports/llm.CompletionProvider`
and `internal/llm` values; none imports package `main` or the concrete OpenAI
adapter. `internal/adapters/llm/openai` depends on the neutral LLM values and
completion port only.

The remaining root dependency edges are composition/compatibility edges,
plus the two blocker workflows described above.

## Behavior

Completion transport: unchanged.

Employer reply: unchanged.

Vacancy analysis: unchanged.

Candidate interpretation and trust boundary: unchanged.

Cover letter: unchanged.

Test answering and write-side validation: unchanged.

Candidate truth, conversation policy, deterministic vacancy rules, RelevantKnowledge,
HH read/sync, HH write boundaries, storage, and dashboard behavior: unchanged.

LIVE HH WRITES: **0**.

## Verification

The following checks passed during this audit:

- `go test ./...`
- Focused R10 suites: `internal/llm`, `internal/ports/llm`,
  `internal/adapters/llm/openai`, and all five extracted use cases, plus
  candidate acquisition/mutation.
- `go list ./...`
- `go list -deps` for all five extracted use cases and the OpenAI adapter.
- `gofmt -l .` (no unformatted files reported).
- `git diff --check`
- `node --check web/app.js`

The following full checks also passed:

- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

Docker was available as a client, but the daemon was not running, so the
Docker build was **SKIPPED** (`Cannot connect to the Docker daemon`). No Go
source was changed in R10.5c, so `gofmt -w .` was intentionally not run over
the already-dirty worktree.

## R10 final status

R10.1 Completion transport: EXTRACTED

R10.2 Employer reply: EXTRACTED

R10.3 Vacancy analysis: EXTRACTED

R10.4 Candidate interpretation: EXTRACTED

R10.5a Cover letter: EXTRACTED

R10.5b Test answer: EXTRACTED

R10.5c Residual cleanup: **BLOCKED**

Substantial root AI workflows:

- Legacy HH auto-chat response generation and send/leave orchestration.
- Follow-up draft generation.

**R10: BLOCKED — additional extraction required.**
