# R10.6b2 — Legacy HH Auto-Chat AI Boundary

## Before

`HHAIResponder.AutoRespondChats` owned the complete legacy workflow: it read
the HH chat list and live chat history, assembled the legacy prompt, called
`AIClient.Chat`, classified the returned plain text, and then called
`SendChatMessage` or `LeaveChat`.

HH reads remain in the root/read adapter path:

- `getChatsAwaitingReply` performs the existing resume, age, participant,
  vacancy, and discard filtering.
- `GetChatData` obtains the full bounded live history.
- `getChatsAwaitingReply` extracts the ordered text-button display values.
- `AutoRespondChats` retains the existing write-state and 20-message gates.

Input was `ChatToReply` plus raw-shaped `ChatDataResponse`. History was the
existing ordered `Chat.Messages.Items` sequence, rendered as timestamp,
participant display name, text, and `---`; service/system messages were not
reordered or summarized. The current HH button model contains `Text` and
`Size` only. There is no action, callback, or provider button ID in this
repository, so no identity was silently invented.

The authoritative legacy prompt was assembled by `buildChatSystemPrompt` and
the `AutoRespondChats` user-prompt additions. The communication profile is
still loaded exactly as before with `go:embed` in the root and injected into
the use case. The prompt text, history formatting, option ordering, model,
temperature, max tokens, and plain-text response contract are preserved.

The old auto-chat call used `AIClient.Chat`, which passed a nil validator to
`AIClient.chat`; therefore this workflow had one provider call and no semantic
business retry. Provider retry remains owned by the R10.1 provider adapter.
The extracted service preserves this one-call behavior.

Review previously used `chatReplyReviewReasonForContext` for high-risk incoming
conversation material, high-risk generated text, the 400-rune limit, chat
mode, and dry-run. Chat mode and dry-run are write-side decisions and remain in
the root. Generated-content review and candidate-fact review are now owned by
the use case. Existing discard handling is deterministic and occurs before AI
generation; the use case cannot leave a chat.

`SendChatMessage` and `LeaveChat` are unchanged and remain root/write-side
operations. No live HH write was performed.

## Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| `AutoRespondChats` | root | root facade + `autochatreply.Service` | normalize, delegate, gate, execute |
| `getChatsAwaitingReply` | root/read path | root/read path | unchanged HH read/filter behavior |
| `ChatToReply` | root | root compatibility/read shape | normalized at facade boundary |
| `ChatDataResponse` | root/provider-shaped read value | root/read path | never enters use case |
| `JoinChatMessages` | root | compatibility helper; equivalent renderer in use case | ordering/rendering preserved |
| `buildChatSystemPrompt` | root | `autochatreply.BuildPrompt`; root wrapper retained | one auto-chat prompt algorithm owner |
| `candidate_communication.md` | root embedded resource | root resource injected into use case | load/configuration behavior unchanged |
| button text/order | root | `autochatreply.Input.Buttons` | observed text only; no invented action ID |
| `AIClient.Chat` / `AIClient.chat` | root compatibility surface | no auto-chat runtime caller | retained for other compatibility callers; R10.6c audit item |
| semantic retry | `AIClient.chat` only when validator exists | none for auto-chat | exact legacy behavior: one call |
| candidate fact review | prompt-only before extraction | `autochatreply.Review` plus shared pure validator | deterministic and fail-closed |
| high-risk review | root classifier | use case for content; root for mode/dry-run | write defense remains in root |
| discard/leave decision | root `IsDiscard` branch | root | no policy change; use case can only propose leave |
| `SendChatMessage` | root | root | unchanged |
| `LeaveChat` | root | root | unchanged |

## Auto-chat use case

Package: `internal/usecase/autochatreply`

Service: `Service.Prepare(context.Context, Input) (ProposedResult, error)`.

Dependencies are limited to `internal/ports/llm.CompletionProvider`, the
neutral `internal/llm` request values, candidate-context values, the shared
pure candidate validator, the shared high-risk classifier, and the standard
library. The package has no root import, HH adapter, raw HH DTO, storage,
configuration, write gateway, or concrete LLM adapter dependency.

`Input` contains detached chat identity/context, employer message, candidate
legacy display fields plus canonical employer-safe context, ordered bounded
history, observed buttons, communication profile, optional configured contact
values, and deterministic state. `ProposedResult` contains only `Outcome`,
proposed plain text, and a review reason. It has no send, leave, approval,
preflight, persistence, nonce, or delivery state capability.

Outcomes are `reply`, `no_reply`, `manual_review`, and
`leave_recommended`. Terminal, ineligible, and candidate-already-replied state
returns `no_reply` without a provider call. Discarded state can produce only a
`leave_recommended` proposal; actual leave remains root-owned.

## Prompt

Owner: `internal/usecase/autochatreply/prompt.go`.

Source/file behavior: the root continues to embed the existing
`candidate_communication.md` and passes its exact text as
`CommunicationProfile`; no filesystem dependency was added to the use case.
The root `buildChatSystemPrompt` is now a compatibility wrapper over the
use-case prompt builder, so the auto-chat runtime has one prompt algorithm
owner.

Candidate projection, history, and buttons are serialized as data. Button
text is never treated as system authority; instruction-like text is reviewed
as untrusted conversation data. History is not summarized, reordered, or
token-optimized. Observed option order is retained in both the prompt and
deterministic selection.

Behavior: **UNCHANGED** for the legacy prompt and normal plain-text path.
Candidate-output validation is explicitly **SAFETY STRENGTHENED**: unsupported
or unknown candidate facts, unsupported technical context, restricted facts,
contradictory constraints, invented salary values, unsupported duration claims,
and unverified external-action claims route to manual review.

## Generation

Completion provider: `internal/ports/llm.CompletionProvider`.

The root supplies the already configured R10.1 provider through `AIClient`'s
typed `Complete` implementation. The use case does not call `AIClient.Chat` or
`AIClient.chat`.

Model: the configured legacy AI model. Temperature: `0.5`, or `0.1` when
observed text buttons are present. Max tokens: `512`. Messages remain exactly
one system message followed by one user message. Response format remains
plain text; no JSON schema was introduced.

Business attempts: one, because the old AutoRespondChats call supplied no
validator. Provider/system failures remain Go errors and are not converted to
no-reply or manual review. Empty output is an error and cannot reach the root
send path. Context cancellation is returned and stops before any later work.

## Candidate safety

- Known confirmed Django remains usable and is covered by a successful reply
  test.
- Unknown Kubernetes cannot be asserted positively; a generated Kubernetes
  claim is manual review.
- Unknown Kubernetes also cannot be converted into a safe negative answer;
  unknown is not false.
- Partial/basic Docker cannot be upgraded to production, commercial, expert,
  senior, or high-load usage without matching canonical evidence.
- Exact canonical `11 месяцев` rejects an unqualified `год` claim; duration is
  not rounded.
- Salary output is reviewed unless its numeric claim is present in the
  canonical allowed context. Incoming salary conversations remain high-risk
  under the existing classifier.
- Explicit no-relocation and no-business-trips constraints cannot be
  contradicted.
- Restricted facts and forbidden claims cannot appear in a safe proposal.

## Button safety

Input buttons are the observed ordered HH text buttons (`Text`, `Size`). Since
the current provider model has no action identity, selection is exact text
matching only. A generated value not equal to an observed button is rejected
to manual review. Text is data only, including instruction-like text.
Interview dates/times cannot be invented because the use case has no calendar
or scheduling capability and accepts only observed button text.

## Review

Owner: `autochatreply.Review` for generated-content and candidate-safety
classification; root remains owner of mode/dry-run and all write gates.

Safe output returns `OutcomeReply`. High-risk incoming material, high-risk
generated material, unsupported candidate claims, overlong free text,
unobserved button text, and unverified external-action claims return
`OutcomeManualReview`. Empty output is an error. Terminal/ineligible and
candidate-already-replied states return `OutcomeNoReply` without generation.

## Leave

Current decision source: deterministic `ChatToReply.IsDiscard`, derived from
the observed HH workflow transition before generation. AI output does not
influence this existing leave decision.

Use case can execute leave: **NO**.

Root execution: the existing root discard branch still applies the current
chat-mode and dry-run gates and calls the unchanged `LeaveChat` only when the
existing enabled conditions allow it.

## HH write boundary

Use case can send: **NO**.

`SendChatMessage`: ROOT. `LeaveChat`: ROOT. Freshness, write flags, dry-run,
transport ambiguity, retry-after-send behavior, and delivery reconciliation
remain unchanged and outside this extraction. Invalid, empty, or reviewed
generated text is never passed to `SendChatMessage`.

LIVE HH WRITES: **0**.

## Root AutoRespondChats

After extraction the root fetches the live chat state, preserves the existing
read and deterministic eligibility gates, normalizes the chat into
`autochatreply.Input`, calls `Service.Prepare`, maps manual/no-reply outcomes
to the existing preview/skip behavior, applies mode and dry-run gates, and
then uses the existing send/leave methods.

Remaining auto-chat AI algorithm in root: **NONE**. The root retains only
composition, normalization, logging/event mapping, and write-side execution.

## Generic AI audit

- `AIClient.Chat`: no AutoRespondChats runtime caller; retained compatibility
  surface for the existing root tests/legacy callers and a later R10.6c audit.
- `AIClient.chat`: compatibility implementation used by legacy Chat and
  structured compatibility methods; no auto-chat runtime ownership remains.
- `ChatStructured` / `ChatStructuredWithSchema`: retained compatibility APIs;
  not used by autochatreply.
- `StructuredAIClient`: retained for existing orchestrator compatibility.
- `legacyCompletionProvider`: retained only as the existing compatibility
  bridge for callers that provide `StructuredAIClient`.

## Residual AI audit

Employer reply, vacancy analysis, candidate interpretation, cover letter, test
answer, follow-up draft, and application answer remain in their already
extracted packages. Legacy auto-chat is extracted here. No other substantial
AI workflow was moved or redesigned in this stage. The remaining generic
compatibility surfaces above are intentionally left for R10.6c cleanup.

## Tests

`internal/usecase/autochatreply/service_test.go` is pure: fake completion
provider only; no HTTP, DB, filesystem, HH, or writes. It covers prompt order,
plain text, empty output, provider errors, cancellation, one-call legacy
retry behavior, known Django, unknown Kubernetes positive/negative claims,
partial Docker, exact 11 months, salary, relocation, restricted facts,
buttons, instruction-like button text, high-risk output, and terminal/
ineligible/candidate-replied no-call gates.

Existing root tests continue to cover chat review classifiers and all direct
`SendChatMessage`/`LeaveChat` dry-run and disabled-write gates. No real HH
credentials or write configuration were used. The root integration test
`TestAutoRespondChatsUsesProposalBoundaryAndKeepsWritesDisabled` exercises
fake HH reads and fake completion responses for a safe proposal and a
manual-review proposal, and asserts that no HH write endpoint is reached.

## Dependencies

The dependency direction is:

```text
root HH reads
    -> autochatreply.Input
    -> autochatreply.Service
    -> CompletionProvider
    -> autochatreply.ProposedResult
    -> root write-side gates
    -> SendChatMessage / LeaveChat
```

`go list -deps ./internal/usecase/autochatreply` contains no root package,
HH adapter, concrete LLM adapter, config/CLI, storage implementation, pgx,
or write gateway dependency.

## Behavior

Auto-chat prompt, history, buttons, completion options, plain-text contract,
provider behavior, send behavior, leave behavior, HH transport, application
answer, follow-up draft, employer reply, and other AI workflows are unchanged
except for the explicitly documented deterministic candidate-safety
strengthening.

## Verification

The focused extraction and full repository test suite passed after the code
change. Final required verification is recorded below after the complete
stage command set is run.

- `gofmt`: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- focused use-case regressions: PASS for auto-chat and existing extracted packages
- Docker: SKIPPED — Docker CLI is present but the daemon is unavailable
- LIVE HH WRITES: 0

## R10 status

R10.6b2 Auto-chat: **EXTRACTED**.

After the residual generic compatibility cleanup in R10.6c, R10 can proceed to
final closure. This stage does not begin R10.6c, R11 HH write work, storage or
migration changes, career/follow-up restructuring, dashboard/frontend work, or
CLI composition redesign.
