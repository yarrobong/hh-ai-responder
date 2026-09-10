# Before

Completion transport:

The root `AIClient` in `main.go` combined OpenAI-compatible request/response
DTOs, base URL normalization, model and API-key handling, HTTP client
construction, `/v1/chat/completions` request execution, response validation,
provider-specific Mistral/Groq options, transport retries, and the public
chat methods used by the application. Structured-output validation was
invoked by that same root retry loop.

Consumers:

| Caller | Prompt/message shape | Expected output | Model/options | Provider method | Parsing/validation | Retry |
|---|---|---|---|---|---|---|
| Employer chat in `main.go` | system + user; history and chat policy are assembled in root | plain text reply | configured model; 512 tokens; temperature `0.5` or `0.1` for reply options | `Chat` | root review/safety policy after text generation | provider transport retry |
| Vacancy AI in `vacancy_match.go` | system + user vacancy/candidate context | vacancy evaluation JSON text | configured model; 1024 tokens; temperature `0.1`; JSON format/schema option | `ChatStructuredWithSchema` | root strict JSON decode and hard-requirement derivation | provider transport retry + root semantic retry |
| Cover letter in `main.go`/`vacancy_match.go` | system + user vacancy and trusted candidate context | plain text letter | configured model; 512 tokens; temperature `0.5` | `Chat` | root experience/fact checks | provider transport retry |
| Test answering in `main.go` | system + user serialized tasks | solutions JSON text | configured model; `512 + len(tasks)*64`; temperature `0.2`; JSON format/schema option | `ChatStructuredWithSchema` | root strict decode and option/task validation | provider transport retry + root semantic retry |
| `AIReplyOrchestrator` | root reply prompt + safe conversation context | `AIResponseDecision` JSON text | configured model; 900 tokens; temperature `0.2`; schema option | `ChatStructuredWithSchema` | root decision and fact-safety validation | provider transport retry + root semantic retry |
| Candidate knowledge interpretation | root acquisition prompt + gap/answer data | interpretation proposal JSON text | configured model; 700 tokens; temperature `0.1`; schema option | `ChatStructuredWithSchema` | root strict decode and candidate-acquisition validation | provider transport retry + root semantic retry |

Provider(s):

One current OpenAI-compatible chat-completion protocol is used for OpenAI,
OpenRouter/local-compatible endpoints, and the existing Mistral/Groq variants
selected by base URL/model. The endpoint is not migrated to the Responses API.

Request DTO:

The old root DTO carried `model`, ordered `messages`, `stream: false`,
`max_tokens`, `temperature`, `response_format`, and the existing Groq
reasoning fields. The runtime DTO now lives privately in the completion
adapter.

Response DTO:

The old root DTO decoded the first choice, message content, finish reason, and
usage. The adapter now decodes those fields privately and returns neutral
content, finish reason, usage, and reported model values.

Auth:

The composition root passes the configured API key to the adapter. The adapter
sends `Authorization: Bearer <key>` only when the key is non-empty and never
includes it in errors, diagnostics, or returned values.

Retries:

The old configured attempt count and delay are preserved for transport and
provider-response failures. Semantic retries caused by invalid business JSON
remain in the root compatibility facade and are not part of the provider
contract. Authentication failures (401/403) fail without another attempt.

Prompt/business coupling:

Prompts, candidate facts, vacancy policy, conversation state, answerability,
fact safety, and all domain JSON parsing were root-owned and remain root-owned.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `AIClient` | root | root compatibility/orchestration facade | retained; delegates completion to the port |
| `ChatCompletionRequest` and response DTOs | root runtime | private adapter DTOs; root fixture compatibility only | moved runtime encoding/decoding |
| `AIMessage`/roles | root | `internal/llm.Message`/`Role`; root fixture compatibility remains | neutralized |
| Base URL normalization | root AI client | completion adapter | moved |
| HTTP client construction and connect timeout | root constructor | root composition | retained outside adapter as dependency construction |
| Completion endpoint/request/auth | root AI client | `internal/adapters/llm/openai` | moved |
| Mistral JSON schema compatibility | root AI client | completion adapter | moved |
| Groq GPT-OSS reasoning options | root AI client | completion adapter | moved |
| Completion response validation | root AI client | completion adapter | moved; first choice and non-empty content preserved |
| Usage/finish/model metadata | root provider DTO | `internal/llm.CompletionResponse` | normalized |
| Semantic JSON validation/retry | root callers/facade | root callers/facade | unchanged; not moved |
| Employer reply policy | root | root | unchanged; R10.2 |
| Vacancy evaluation policy | root | root | unchanged; R10.3 |
| Candidate acquisition interpretation | root | root | unchanged; R10.4 |
| Cover-letter policy | root | root | unchanged |
| Embedding transport | `internal/adapters/embedding/openai` | same package | unchanged and separate |

# Completion values

Package:

`internal/llm`

Types:

`Role`, `Message`, `JSONSchema`, `ResponseFormat`, `CompletionRequest`,
`Usage`, and `CompletionResponse`.

Dependencies:

Standard library only (`encoding/json` is used for opaque schema bytes). No
HTTP, provider DTO, Candidate, Vacancy, Conversation, HH, repository, pgx, or
semantic dependency.

# Completion port

Package:

`internal/ports/llm`

Interface:

```go
type CompletionProvider interface {
    Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error)
}
```

Provider-specific leakage:

NONE. The port contains no endpoint, API key, HTTP request/response, raw JSON,
or provider error type.

# Completion adapter

Package:

`internal/adapters/llm/openai`

Implementation:

One non-streaming OpenAI-compatible `POST /v1/chat/completions` implementation.
It sends the supplied ordered messages/options without prompt rewriting,
decodes only the first choice, trims and rejects empty content, returns usage
and finish/model metadata, and keeps protocol DTOs private.

Constructor:

`openai.New(openai.Options{...})` is typed. Options contain only base URL, API
key, model, attempt count, reusable `*http.Client`, retry delay, and optional
caller-controlled safe observability hooks. The adapter reads no environment,
CLI flags, or application config.

Dependencies:

`internal/llm`, `internal/ports/llm`, and the standard library. It imports no
Candidate, Vacancy, Conversation, HH, storage, semantic, or dashboard package.

# HTTP contract

Endpoint:

Configured base URL plus `/v1/chat/completions`, preserving existing base-path
semantics such as a compatible `/openai` prefix.

Method:

`POST`

Auth:

Optional `Authorization: Bearer ...`; never logged or returned.

Headers:

`Content-Type: application/json`; no new provider headers.

Request:

`model`, ordered `messages`, `stream: false`, existing `max_tokens` and
`temperature` fields with their existing `omitempty` behavior, and optional
`response_format`, `reasoning_effort`, and `include_reasoning` fields. No
trimming or reordering is performed.

Response:

First choice only. Content, finish reason, usage, and reported model are
normalized into `llm.CompletionResponse`; business JSON remains text.

Timeout:

The reusable HTTP client and its existing overall/connect/TLS timeout setup
are created by root composition and injected into the adapter. Each request
uses the caller context.

Retries:

The adapter retries provider transport/response failures using configured
attempts and delay, with cancellation interrupting the wait. 429 and 5xx
retain retry behavior. 401 and 403 are not retried. Semantic regeneration
remains outside the adapter.

# Model/options

Model authority:

The root configured model is supplied on every request, as before. The
adapter’s configured model is the fallback for direct adapter use and is also
used for the existing provider-variant detection; no response model mutates
configured state.

Temperature:

The existing float value is passed unchanged; zero remains omitted by the
provider DTO exactly as before. Existing callers continue to use `0.1`, `0.2`,
`0.5`, or `0.1` for reply options.

Token limit:

The existing `max_tokens` field is preserved; it is not renamed or tuned.

Structured output transport options:

Callers may request `json_object` or supply the existing schema option. Mistral
receives the existing strict `json_schema`; unsupported compatible endpoints
receive the existing `json_object` fallback. Groq GPT-OSS receives the
existing `reasoning_effort=low` and `include_reasoning=false` fields.

# Error semantics

401:

Fail with status/model diagnostics; no retry and no credential exposure.

403:

Fail with status/model diagnostics; no retry and no credential exposure.

429:

Retry according to the existing configured attempt count/delay; response body
is not included in the error.

5xx:

Retry according to the existing configured attempt count/delay; response body
is not included in the error.

Malformed:

Return a bounded diagnostic without response body contents and retry as a
provider-response failure when attempts remain.

Network:

Return the underlying network error and retry according to the transport
attempt policy. No new ambiguous-request behavior or retry framework was
introduced.

Cancellation:

The caller context controls request creation, HTTP execution, response read
completion, and retry waiting. Cancellation is returned to the caller.

Secret exposure:

NONE. A synthetic `super-secret-test-key` regression confirms provider errors
do not contain the key or request/private response text.

# Consumers

AIReplyOrchestrator:

ROOT / unchanged except that its existing `StructuredAIClient` dependency
ultimately reaches the typed completion port through `AIClient`.

Vacancy AI:

ROOT / prompts, parsing, hard-requirement derivation, and decisions unchanged.

Candidate acquisition:

ROOT / interpretation remains an untrusted proposal and cannot confirm facts.

Cover letter:

ROOT / generation policy and experience validation unchanged.

Summary/claims:

ROOT / no claim semantics moved into the provider.

Business parsing:

OUTSIDE PROVIDER. Root callers own JSON decoding, schema validation, enum/task
validation, and fact-safety checks.

# Embedding separation

EmbeddingProvider:

UNCHANGED — `internal/adapters/embedding/openai` remains authoritative.

Shared implementation:

NONE. Completion and embedding adapters intentionally keep separate capability
implementations; neither adapter implements the other provider interface.

# Root compatibility

`NewAIClient` preserves the existing root constructor and business methods.
Its `*http.Client` remains injectable through the existing test-visible field,
while the actual completion HTTP request/response implementation is no longer
in root. The old wire-shaped types remain only for root tests/legacy fixtures;
runtime provider DTOs are private to the completion adapter.

Runtime completion HTTP in root:

NONE.

# Tests

Transport:

`internal/adapters/llm/openai/openai_test.go` uses `httptest.Server` and a
synthetic round-tripper for request path/method/auth, exact message
whitespace/order, model/options, response normalization, zero-option omission,
provider variants, malformed/empty/zero-choice responses, retry behavior, and
cancellation.

Secrets:

The adapter regression checks that API keys, private prompts, and provider
error bodies are absent from returned errors.

Root AI regressions:

Existing reply, vacancy, cover-letter, candidate-acquisition, test-answer,
conversation, and write-preflight safety tests pass unchanged.

Embedding:

`go test ./internal/adapters/embedding/openai/...` passes; no embedding code
was changed by R10.1.

# Size

Root completion transport LOC before:

Approximately 190 LOC in the pre-change worktree, counting the root AI wire
DTOs, constructor, chat methods, provider option detection, request execution,
and response decoding.

Root compatibility after:

Runtime completion HTTP: 0 LOC. The root retains a 126-line completion facade
plus a 74-line test/legacy wire compatibility file.

Completion adapter LOC:

326 implementation LOC and 210 characterization-test LOC.

# Dependencies

```text
internal/llm
    -> standard library

internal/ports/llm
    -> internal/llm
    -> standard library

internal/adapters/llm/openai
    -> internal/ports/llm
    -> internal/llm
    -> standard library/net/http
```

`go list -deps` confirms no domain, HH, storage, semantic, or dashboard
dependency for the new value/port/adapter packages. The root composition
continues to construct the adapter alongside the unchanged embedding adapter.

# Behavior

Completion:

UNCHANGED — same endpoint, method, model, message order, options, first-choice
selection, empty-content failure, and configured transport attempt behavior.

AI reply:

UNCHANGED

Vacancy AI:

UNCHANGED

Candidate acquisition:

UNCHANGED

Embedding:

UNCHANGED

HH:

UNCHANGED; no HH write path was called.

Storage/semantic/dashboard:

UNCHANGED; no schema, migration, persistence, semantic, or dashboard changes
were made for R10.1.

# Verification

gofmt:

PASS

go test:

PASS — `go test -count=1 ./...`

race:

PASS — `go test -race ./...`

vet:

PASS — `go vet ./...`

build:

PASS — `go build ./...`

diff:

PASS — `git diff --check`

node:

PASS — `node --check web/app.js`

Docker:

SKIPPED — Docker CLI is present, but the daemon is unavailable in this
environment.

Focused packages:

PASS — `internal/llm/...`, `internal/ports/llm/...`,
`internal/adapters/llm/openai/...`, and
`internal/adapters/embedding/openai/...`.

LIVE HH WRITES:

0

# Ready for R10.2

R10.2 — Employer Reply / Conversation AI Orchestration Boundary

READY

Completion HTTP has one runtime owner; the provider contains no business
prompt or policy; domain-specific JSON parsing remains outside the provider;
embedding remains separate; and existing AI consumers preserve their
behavior. R10.1 stops here.
