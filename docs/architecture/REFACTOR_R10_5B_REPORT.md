# R10.5b — HH Test Answering AI Boundary

## Before

`AIClient.SolveTests` in `main.go` owned the complete test-answer algorithm:
prompt construction, JSON schema construction, structured completion, strict
parsing, semantic retry, task/option validation, and conversion to the legacy
`map[int]SolutionFields` result.

`HHAIResponder.ApplyVacancyWithTest` remained responsible for the HH test
readback, payload preparation, preview/result projection, and `SendResponse`.
That function was not moved or refactored.

The existing test model was:

- `Task.ID`, `Task.Description`, `Task.Multiple`, `Task.Open`;
- `Task.CandidateSolutions []Solution`;
- option identity in `Solution.ID` (a string parsed deterministically as an integer);
- choice answers represented by one `solution_id` integer;
- open answers represented by non-empty `text_solution`;
- all supplied tasks required by the established exact task-count contract.

The authoritative prompt, schema, parser, and validator were the functions
`buildTestSystemPrompt`, `testSolutionsJSONSchema`, `parseStrictJSON`, and
`validateTestSolutions`. Semantic attempts used the existing `AIClient`
attempt setting, with `512 + len(tasks)*64` tokens and temperature `0.2`.

## Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| HH vacancy-test readback | `HHAIResponder.GetVacancyTests` | root workflow | unchanged |
| `Task` / `Solution` HH DTOs | `main` | root compatibility/read workflow | unchanged |
| normalized task values | implicit root values | `internal/usecase/testanswer` | added narrow values |
| test prompt | `main.go` | `testanswer.BuildPrompt` | moved unchanged |
| LLM call | `AIClient.ChatStructuredWithSchema` | `testanswer.Service` → `CompletionProvider` | moved |
| structured schema | `main.go` | `testanswer.ResponseFormat` | moved unchanged |
| strict parsing | root `parseStrictJSON` path | `testanswer.ParseResponse` | moved unchanged |
| question/task validation | `main.go` | `testanswer.Validate` | moved and source-ordered |
| option validation | `main.go` | `testanswer.Validate` | moved unchanged |
| business retry | `AIClient.chat` around `SolveTests` | `testanswer.Service` | moved; invalid output only |
| preview | `ApplyVacancyWithTest` / existing result projection | root workflow | unchanged |
| approval | HH Write workflow | HH Write workflow | unchanged |
| fresh preflight | HH Write/application workflow | HH Write/application workflow | unchanged |
| HH submission | `SendResponse` / existing write path | existing root/write path | unchanged |

## Test-answer use case

Package: `internal/usecase/testanswer`

Service: `testanswer.Service.Generate(ctx, Input) (Result, error)`

Dependencies: only `internal/ports/llm.CompletionProvider` and the neutral
completion values in `internal/llm`.

Input: normalized `Task` and `Option` values plus the established contacts,
GitHub URL, and extra prompt values.

Output: `Result{Answers []ProposedAnswer}` in source task order. The result is
explicitly a proposed, structurally validated value; it has no submit,
approval, preflight, nonce, delivery, storage, or HTTP capability.

## Test model

Question identity is the supplied integer `Task.ID`. Option identity is the
supplied option ID, converted to an integer only for compatibility with the
existing HH submission field. Question text and option ordering are preserved.

The current model supports one choice ID for a task with options and one
non-empty free-text answer for an open task. `Multiple` and `Open` remain
transported metadata but are not given new semantics. No unsupported question
types or multiple-selection framework was introduced.

## Prompt

Owner: `testanswer.BuildPrompt`.

Serialization uses the same JSON task shape, field names, ordering, IDs, and
user prefix (`JSON с тестами: `). The system instructions, configured contact
and GitHub additions, extra prompt, answer field names, and strict JSON
requirements are unchanged. Question and option text remain provider data;
IDs remain the deterministic structural authority.

Behavior changed: no prompt-quality rewrite. Instruction-like question and
option text is still serialized as data and cannot authorize an invented ID.

## Structured result

Schema: `test_solutions`, strict JSON schema, root object with only `solutions`,
and solution items with only `task_id`, `solution_id`, and `text_solution`.
The schema shape and strictness are unchanged.

Parser: `testanswer.ParseResponse` uses `json.Decoder.DisallowUnknownFields`,
custom presence tracking for answer fields, and trailing-data rejection.
Empty content, Markdown/preamble/postamble, multiple JSON values, unknown
fields, missing `task_id`, and malformed JSON fail closed.

## Validation

- Unknown question/task IDs are rejected.
- Options are checked against the exact option set of the exact task; a
  cross-question option is rejected.
- Duplicate question answers are rejected.
- Choice tasks require exactly the established scalar `solution_id` shape.
- Open tasks reject `solution_id` and require non-empty `text_solution`.
- The answer count must equal the supplied task count and every source task
  must be present.
- Validated output is rebuilt in source task order and does not depend on AI
  answer order.
- No generalized multiple-choice or unsupported-type semantics were added.

## Retry

Business attempts remain the configured `AIClient` attempt count, with a
minimum of one. Only malformed or structurally invalid model output consumes
a business retry. Provider/system errors return immediately from the use case;
provider transport retry remains owned by R10.1. Context cancellation stops
further calls and is returned as the context error.

Worst-case completion calls are therefore:

`business attempts × provider transport attempts`

for invalid model output, and one use-case call (with provider-owned transport
attempts) for a provider error. No new retry multiplication was introduced.

## Result authority

AI result: **PROPOSED / STRUCTURALLY VALIDATED**

HH submitted: **NO**

Correctness guaranteed: **NO** — structural validation proves mapping to the
supplied options, not factual correctness of an objective technical answer.

Submission authority: the existing root/application and HH Write workflow.

## HH boundary

Reads remain in the root/existing test metadata workflow. Writes remain in the
existing root/HH Write path. `testanswer` cannot submit answers and contains no
HH transport, write gateway, storage implementation, approval, nonce, or
delivery dependency.

Fresh preflight and downstream HH-write validation are preserved as
defense-in-depth and remain authoritative for current test state/staleness.

## Root compatibility

`AIClient.SolveTests` is now a thin compatibility facade. It converts legacy
root task values to `testanswer.Task`, constructs the typed service, delegates,
and converts `Result.Answers` back to `map[int]SolutionFields`.

Root compatibility aliases/adapters preserve existing callers and tests, but
the runtime prompt, schema, parser, business retry, and validation algorithm
have one owner in `internal/usecase/testanswer`.

`ApplyVacancyWithTest` still performs test readback and calls `SendResponse`
through the existing path; no submission behavior was moved or changed.

## Other AI

Employer reply, vacancy analysis, candidate interpretation, and cover letter
use cases are unchanged.

Residual generic AI audit:

- `AIClient.Chat` has one runtime caller in the legacy/root reply path and
  existing tests.
- `ChatStructuredWithSchema` remains used by the employer-reply compatibility
  path and `AIReplyOrchestrator`; it is not deleted in R10.5b.
- `ChatStructured` has no runtime direct caller found by the audit.
- `StructuredAIClient` remains the compatibility interface used by existing
  employer-reply/candidate-interpretation composition.
- Residual generic compatibility and unextracted workflows are intentionally
  deferred to R10.5c.

## Tests

Added pure fake-provider coverage for:

- valid structured response and exact request options/schema;
- exact prompt/task/option serialization and instruction-like data;
- malformed/invalid output retry and exact call counts;
- provider error without business retry;
- cancellation and empty input without provider calls;
- unknown task, unknown option, cross-question option, duplicate task,
  conflicting answer fields, missing required task, source-order output, and
  strict malformed/trailing/unknown-field responses.

Existing root compatibility tests and the extracted AI use-case regression
tests pass. No HTTP client or HH write is used by the new tests.

## Size

The extracted root test-answer algorithm was approximately 158 lines before
the move. The root compatibility facade and adapters are 74 lines. The new
package is 385 production lines, including its narrow values, prompt/schema,
service, parser/validator, and documentation; its tests are 159 lines.

## Dependencies

`go list -deps ./internal/usecase/testanswer` shows only the new use case,
`internal/llm`, `internal/ports/llm`, and standard library packages from the
project. It does not depend on `main`, HH adapters, HH write code, storage, or
other use cases.

## Behavior

Test answering: unchanged in prompt, schema, model, temperature, token formula,
IDs, option mapping, strictness, and business-attempt semantics; validated
result order is now deterministic source order.

Test submission: unchanged.

HH Write: unchanged.

Other AI: unchanged.

## Verification

- gofmt: PASS
- focused use-case tests: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- LLM transport regression tests: PASS
- Docker: SKIPPED — Docker CLI is installed, but the daemon is unavailable
- live HH writes: 0

The Docker build was attempted and could not connect to
`unix:///var/run/docker.sock`; no repository or HH state was changed by that
check.

## Ready for R10.5c

**READY**

- `SolveTests` has one importable implementation owner;
- `testanswer` has no HH write capability;
- model output is structurally validated against the exact supplied test;
- downstream write-side validation/preflight remains intact;
- remaining generic AI callers are classified and deferred to R10.5c.
