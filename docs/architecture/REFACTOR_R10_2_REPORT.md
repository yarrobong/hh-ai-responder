# Before

`AIReplyOrchestrator` in the root package combined conversation-context use,
prompt construction, completion invocation, structured parsing, semantic
retry, candidate-fact validation, conversation-policy gating, clarification
creation, and draft persistence. The same root type also retained cover-letter,
application-answer, and follow-up compatibility behavior.

Constructor:

`NewAIReplyOrchestrator(ai StructuredAIClient, dependencies ...any)` discovered
conversation builders, stores, semantic retrievers, and mutation services by
runtime type. This remains only as a root compatibility constructor; the new
use case has a typed constructor.

Context:

`ConversationContextBuilder` remains root-owned and continues to assemble the
bounded history, canonical CandidateContext, conversation summary, policy
requirement, consistency warnings, and already-safe semantic selections.

Prompt and schema:

The employer-reply system prompt and `ai_response_decision` schema were root
functions. Parsing and validation were also root functions.

LLM and retry:

The root structured client called `ChatStructuredWithSchema`. The R10.1
provider already owned transport retries; the root structured path also
performed business-output validation/retry.

Fact safety and policy:

Root `validateAIUsedFacts`, `ValidateAIDraft`, and reply gating protected the
CandidateContext and delegated conversation policy to
`internal/usecase/conversationpolicy`.

Draft persistence:

Root `AIDraftStore` persistence and clarification persistence were coupled to
the orchestration method and remain outside the new use case.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `AIResponseDecision` | root | `internal/usecase/employerreply.Decision`; root alias | moved |
| reply action enums | root | `employerreply.Action`; root aliases | moved |
| reply prompt | root | `employerreply.SystemPrompt` | moved |
| structured reply schema | root | `employerreply.ResponseFormat` | moved |
| structured business parse | root | `employerreply.ParseDecision` | moved |
| semantic regeneration | root structured path | `employerreply.Service` | moved |
| provider transport retry | R10.1 provider | R10.1 provider | unchanged |
| candidate safe projection | candidate/context use cases | candidate/context use cases | unchanged |
| conversation policy | root compatibility delegation | `conversationpolicy`; result supplied/rechecked | unchanged |
| semantic retrieval/freshness | conversation context/root integration | conversation context/root integration | unchanged |
| clarification persistence | root | root workflow | unchanged/outside |
| draft persistence | root | root workflow | unchanged/outside |
| HH read/write | root adapters/workflows | root adapters/workflows | unchanged |
| cover letter/vacancy/acquisition AI | root | root; later stages | unchanged |

# Employer reply use case

Package:

`internal/usecase/employerreply`

Service:

`employerreply.NewService(Dependencies, Options)` and
`(*Service).Prepare(context.Context, Input)`.

Dependencies:

Only `ports/llm.CompletionProvider` is injected. Options carry the configured
model, exact reply completion options, semantic attempt count, extra prompt,
and an injectable clock seam for external-action classification.

Input:

Typed `employerreply.Input` contains a detached `Context`: normalized
conversation snapshot, bounded ordered messages, vacancy projection,
`candidatecontext.CandidateContext`, conversation summary/guidance, policy
requirement, consistency warnings, and already-safe semantic hints/snapshot.
There are no dependency bags, storage readers, HH clients, or `...any` APIs.

Output:

`employerreply.Decision` is a draft/decision only. It can represent a draft,
candidate clarification, no reply, courtesy/no-reply policy, or manual review.
It has no send, approve, preflight, nonce, delivery, or persistence method.

# Context contract

Candidate truth comes through `candidatecontext.CandidateContext`. The use case
does not create or mutate Candidate facts and does not rebuild a second
confirmed/verified projection.

Conversation input is `internal/conversation.EmployerConversation` plus the
detached context fields. History order and the existing recent-message window
are preserved by `ConversationContextBuilder`.

ConversationPolicy remains authoritative for terminal, external-interview,
reply-required, optional, and no-reply states. A supplied `NO_REPLY_NEEDED` or
`REPLY_OPTIONAL` state gates completion before any provider call; when the
detached requirement is empty, the service evaluates the shared policy.

Semantic hints are relevance-only. The service receives no semantic repository
and never treats score, stored text, or a hint as Candidate evidence. Stale and
ineligible semantic rows are filtered before this boundary by the existing
semantic/context path.

# Prompt

Owner:

`internal/usecase/employerreply/prompt.go`

Messages:

Exactly one system message followed by one user message, preserving the prior
system safety instructions, task suffix, context labels, history ordering, and
safe Candidate projection fields.

Schema:

`ai_response_decision`, strict object, unchanged action/reply-requirement
enums and required fields.

Model/options:

Model authority remains composition-provided. Max tokens remain `900` and
temperature remains `0.2`.

Behavior changed:

NO. The extraction changes ownership and provider seam only.

# Generation

Completion port:

`internal/ports/llm.CompletionProvider`, with text read from
`llm.CompletionResponse.Content`.

Transport:

R10.1, unchanged. `AIClient.Complete` is a root composition compatibility
bridge to the already-created provider and performs one provider operation.

Business attempts:

The configured attempt count is preserved for malformed/invalid business JSON
only. Provider errors are returned immediately so business retry cannot
multiply transport retry.

Provider attempts:

R10.1 / unchanged.

Worst-case call count:

`business semantic attempts × provider transport attempts`; no additional
network retry is introduced by `employerreply`. Provider-error tests assert one
business-layer call.

# AI decision

Type:

`employerreply.Decision`, with root `AIResponseDecision` as a source-compatible
alias.

JSON:

Strict decoding rejects unknown fields, malformed JSON, trailing data, invalid
enums, missing reasons, invalid confidence, invalid draft/action combinations,
and incomplete missing-information objects.

Validation:

Deterministic validation runs after parsing. An invalid draft becomes a typed
manual-review decision; malformed structured output remains a system/provider
error after all semantic attempts are exhausted. No invalid text becomes an
actionable draft.

# Candidate fact safety

Known:

Only the supplied employer-safe CandidateContext and its allowed claims are
usable.

Partial:

Partially resolved claims remain distinct from confirmed facts and are not
promoted by generated text.

Unknown:

Unknown atomic facts route to `need_candidate_input` before completion. Unknown
is never treated as a negative fact.

Restricted:

Restricted facts and forbidden claims are rejected if present in generated
text, regardless of model confidence or semantic relevance.

11 months:

The validator compares explicit generated durations with an exact canonical
total-experience fact and rejects `1 year`/`12 months` when canonical evidence
is exactly `11 months`; unqualified durations remain fail-closed.

Salary and relocation:

They remain answerable only from canonical resolved facts. The validator blocks
relocation wording that contradicts an explicit canonical negative.

Hypothesis promotion:

NONE. The package has no Candidate writer dependency.

# Conversation safety

Reply required/optional/no reply:

The shared policy result gates completion. Optional courtesy messages do not
become ordinary reply drafts.

Terminal:

Terminal/rejected/closed policy yields no reply; AI output cannot reopen it.

Candidate already replied:

The latest delivered candidate message yields no duplicate actionable reply.

External action:

External interview/action messages produce manual review when the current
policy permits a response path. No external action is executed.

Instruction confirmation:

Candidate-context confirmation requirements are resolved before completion and
remain candidate/user clarification, not an AI claim of completion.

# Semantic retrieval

Authority:

RELEVANCE ONLY.

Canonical validation:

Existing `candidatesemanticsearch` freshness validation and the root safe
semantic projection remain upstream. The new package accepts only the
resulting typed hints/snapshot.

Stale result:

No direct semantic repository dependency; stale raw rows cannot enter this
package through the employer-reply input contract.

Truth promotion:

NONE.

# Failure behavior

Provider:

Returned as a wrapped completion failure; not semantically retried by this
package.

Malformed structured output:

Regenerated up to the configured business attempt count; final failure is
fail-closed.

Validation failure:

Known invalid generated drafts become manual review, never sendable drafts.

All semantic retries exhausted:

Return error; do not return the last unvalidated content.

Unknown Candidate fact:

Typed candidate clarification before completion.

Manual confirmation:

Typed manual-review/clarification result; no Candidate mutation.

# Draft boundary

Usecase sends HH: NO

Usecase approves: NO

Usecase preflights: NO

Draft persistence: ROOT. The thin root workflow persists a validated
`employerreply.Decision` into the existing `AIDraftStore` and preserves draft
IDs, status, hashes, and dashboard JSON shape.

# Root compatibility

`AIReplyOrchestrator` remains a root facade because it still owns draft and
clarification persistence plus other AI consumers. Its employer-reply method
builds root context, translates it into typed `employerreply.Input`, calls the
service, and persists only the returned draft/clarification.

`NewAIReplyOrchestrator(...any)` remains compatibility-only debt because many
existing root callers still pass heterogeneous legacy dependencies. It now
constructs the typed service and contains no employer-reply prompt, structured
schema, parsing, semantic retry, or fact-validation algorithm.

Root aliases:

`AIResponseDecision`, action constants, and missing-information values alias
the use-case definitions. Runtime employer-reply prompt text exists only in
`internal/usecase/employerreply`.

# Completion transport

R10.1: UNCHANGED. The OpenAI-compatible adapter and neutral completion values
were not changed by this stage.

# Other AI consumers

Vacancy AI: ROOT / R10.3.

Candidate acquisition: ROOT / R10.4.

Cover letter: ROOT.

Test answering: ROOT.

Summary/claims: ROOT conversation workflow; not promoted to Candidate truth.

# Tests

Pure employerreply:

Typed fake `CompletionProvider` tests cover first-attempt success, malformed
then valid output, all semantic attempts invalid, provider failure,
cancellation, exact request options/message roles, policy gating, unknown
Kubernetes, restricted facts, and exact 11-month duration safety.

Fact safety:

Existing root CandidateContext, candidate mutation, and conversation-consistency
regressions remain green.

Conversation policy:

Existing root tests cover salary, relocation, technical questions,
courtesy/status, rejection/terminal, external interview action, and candidate
already-replied behavior.

Semantic:

Existing semantic index/search and context tests remain green; no indexing or
search code changed.

Root compatibility:

Existing AI reply, dashboard preview, draft persistence, follow-up, and
conversation tests remain green.

Write safety:

Static package audit found no HH write transport, gateway, storage adapter,
approval, nonce, or delivery dependency in `internal/usecase/employerreply`.

# Size

Root `ai_reply_orchestrator.go` before this extraction: 1072 lines in the
pre-change worktree audit. After: 863 lines. The reply-specific importable
package is 603 production lines plus 162 test lines (765 total, including its
package doc, prompt, schema, types, service, and validation files).

# Dependencies

```text
internal/conversation
internal/candidatecontext
internal/usecase/conversationpolicy
internal/semantic       (typed relevance-hint values only)
internal/ports/llm
        ↓
internal/llm
```

`employerreply` imports no root package, concrete LLM adapter, HH read/write
package, PostgreSQL/JSON storage implementation, Dashboard, CLI, or config.

# Behavior

Employer reply: UNCHANGED

Candidate: UNCHANGED

Conversation: UNCHANGED

Semantic: UNCHANGED

Completion: transport UNCHANGED; business retry ownership extracted.

HH: UNCHANGED; LIVE HH WRITES = 0.

# Verification

gofmt: PASS (`gofmt -w .`)

Focused tests: PASS.

Full `go test -count=1 ./...`: PASS.

`go test -race ./...`: PASS.

`go vet ./...`: PASS.

`go build ./...`: PASS.

`git diff --check`: PASS.

`node --check web/app.js`: PASS.

Dependency and forbidden-import audits: PASS.

Docker: SKIPPED; Docker CLI is installed but the daemon is unavailable.

LIVE HH WRITES: 0.

# Ready for R10.3

READY, subject to the final repository-wide verification pass. Employer reply
has one importable owner, Candidate truth and answerability remain
deterministic, conversation policy gates AI, invalid output cannot become an
actionable draft, and the use case has no HH write/storage/provider
implementation dependency.
