# Before

Vacancy AI was root-owned. `vacancy_match.go` contained the vacancy prompt,
structured schema, strict parser, AI response model, semantic retry, and
AI-derived hard-requirement validation. `AIClient.EvaluateVacancy` called the
root completion helper directly. The higher workflow then merged trusted
preflight requirements, made the deterministic decision, ran preflight, and
continued to the existing application flow.

Prompt:

The established system/user messages and bounded employer-safe candidate
projection were assembled in the root package.

Schema:

The `vacancy_evaluation` strict JSON schema and field vocabulary were root
owned.

Candidate input:

The prompt received the existing `LegacyCandidateContext`, including exact
structured total experience and the canonical employer-safe
`CandidateContext` projection.

Deterministic matching:

Salary and vacancy-only filters were already owned by `internal/vacancy` and
the root workflow. Preflight, archived state, work mode, location, and
application controls remained outside the AI evaluator.

Hard requirements:

The model returned extraction candidates only. Status and candidate evidence
were derived locally, including the soft gap policy for a known 11-month
candidate total against a one-year minimum.

LLM:

The root `AIClient` owned transport adaptation and also contained the vacancy
semantic validation loop.

Business retry:

Invalid vacancy JSON/schema/business output consumed the configured semantic
attempt budget. Provider transport retry remained in the R10.1 provider.

Persistence:

The root workflow emitted existing JSON events and continued existing vacancy
and application orchestration. The evaluator had no repository dependency.

Application recommendation:

The model's `apply` field was advisory. Deterministic decision logic, live
preflight, preview/manual controls, and the write gateway remained higher-level
responsibilities.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| Vacancy facts and salary filtering | `internal/vacancy` / root compatibility | unchanged | retain deterministic owner |
| Candidate truth/context | `internal/candidate`, `candidatecontext`, root projection | unchanged | pass bounded typed input |
| Vacancy AI prompt | `vacancy_match.go` | `internal/usecase/vacancyanalysis/prompt.go` | move |
| Structured schema | `vacancy_match.go` | `internal/usecase/vacancyanalysis/schema.go` | move |
| Strict JSON parsing | `vacancy_match.go` | `internal/usecase/vacancyanalysis/validation.go` | move |
| Semantic retry | root `AIClient` vacancy path | `vacancyanalysis.Service` | move |
| AI hard-requirement extraction/verification | root | `vacancyanalysis` | move |
| Final deterministic decision | root workflow | root workflow | retain |
| Preflight/application writes | root workflow and HH write boundary | unchanged | retain |
| Employer reply/test/cover-letter AI | other root/use-case paths | unchanged | out of scope |

# Vacancy analysis use case

Package:

`internal/usecase/vacancyanalysis`

Service:

`vacancyanalysis.NewService(Dependencies{Completion: ...}, Options{...})`
returns a typed `Service`. `Service.Analyze(ctx, Input)` performs one
structured assessment and returns `Assessment`.

Dependencies:

Only `internal/ports/llm.CompletionProvider` is injected. The package imports
no root package, concrete LLM adapter, HH adapter, storage adapter, database,
dashboard, CLI/config, or write gateway.

Input:

`Input` contains `vacancy.Vacancy`, description/presentation fields, keywords,
and `CandidateFacts`. `CandidateFacts` is a bounded safe projection with the
canonical `CandidateProfile` and `candidatecontext.CandidateContext`; exact
experience duration has an explicit known flag.

Output:

`Assessment` preserves the existing score, apply flag, reasons, missing,
strong-match, and evidence-backed hard-requirement fields. It does not contain
an application command or write capability.

# Prompt

Owner:

`internal/usecase/vacancyanalysis/prompt.go`.

Messages:

The existing system prompt, user section order, wording, JSON instruction,
candidate fields, vacancy fields, and keyword fields were preserved.

Candidate projection:

The existing bounded projection and canonical employer-safe context are used;
contacts and unrelated private metadata are not newly added.

Vacancy projection:

The existing title, company, HH work-experience field, description, salary,
location, schedule, and keyword context are preserved.

Model/options:

The service preserves the configured model, `max_tokens=1024`,
`temperature=0.1`, ordered system/user messages, and strict structured JSON
response format.

Behavior changed:

NO intended prompt-quality or matching-policy change.

# Structured assessment

Type:

`vacancyanalysis.AIResponse` is the strict provider contract; `Assessment` is
the locally validated result.

JSON schema:

`vacancy_evaluation`, strict object, no unknown fields, existing required
fields/enums/ranges, and no model-owned status or candidate evidence.

Parsing:

Malformed JSON, trailing data, null/missing required fields, unknown fields,
invalid hard-requirement objects, unsupported enums, and out-of-range scores
fail closed.

Validation:

AI hard-requirement candidates must have vacancy evidence present in vacancy
text/structured evidence and optional or hallucinated requirements are
discarded. Candidate status/evidence is derived from trusted facts. Positive
claims naming unknown requirements are not accepted into `reasons` or
`strong_match`.

Score:

The existing integer range 0–100 and meaning are unchanged. Score never
overrides deterministic hard failures or unknown critical facts.

# Business retry

Attempts:

The configured vacancy semantic attempt count is preserved. Only parsing or
business validation failures consume this budget.

Provider retry:

R10.1 remains responsible for transport/provider retry. A provider error is
returned as an error and is not converted into a candidate mismatch.

Worst-case calls:

The use case makes at most the configured semantic-attempt count of provider
calls; the injected R10.1 provider may apply its own transport retry policy.

# Candidate fact safety

Known skill:

Known/confirmed Django remains a positive deterministic candidate fact.

Partial:

Partial/related skills remain limited to the supplied projection and cannot be
promoted by the model.

Unknown:

Unknown Kubernetes remains unknown even when the model claims a strong fit.

Hypothesis:

Provider output never mutates Candidate Knowledge, CandidateProfile, or any
repository.

11 months:

The exact known value is included as `11 months`; it is never rounded to one
year.

Education:

Only the supplied structured education is used.

English:

No level is inferred or promoted by the evaluator.

Seniority:

No senior/lead/middle claim is created from a skill list.

# Deterministic rules

Salary:

Currency-aware salary filtering remains in `internal/vacancy`/higher
workflow. The model cannot invent salary or currency conversion.

Location:

Existing candidate location and vacancy/preflight evidence rules remain
deterministic; the AI cannot infer relocation.

Relocation:

No-relocation and mandatory-relocation handling remain outside the evaluator.

Work format:

Office/remote/hybrid/unknown handling remains in existing deterministic
preflight and matching logic.

Avoided roles:

Explicit candidate constraints remain deterministic.

Archived:

Archived vacancy state is handled by preflight and cannot become actionable due
to an AI score.

Data completeness:

Partial vacancy data remains partial/unknown; the evaluator does not fill
missing provider fields.

# Experience requirement

Candidate exact experience:

11 months preserved.

Vacancy 1–3 years:

The established general minimum policy remains a soft/unknown gap for a known
11-month candidate when the minimum is at or below 12 months.

Hard block:

NO solely from the 11-month shortfall under the established policy.

AI override:

NO.

# Hard requirements

Source:

AI may suggest requirement text and category only.

AI identification:

The candidate must contain a strict requirement, category, and exact vacancy
evidence fragment.

Deterministic verification:

The vacancy evidence is checked against the relevant vacancy text/structured
field, optional requirements are discarded, and status/evidence are derived
from the safe candidate projection.

Hallucinated requirement accepted:

NO. A synthetic Kafka/Kubernetes requirement absent from the vacancy cannot
become a hard requirement.

# MatchResult

Owner:

`internal/vacancy` remains the owner of persisted `MatchResult` and
`ApplicationRecommendation` domain values.

Construction:

The current root workflow combines the validated assessment with deterministic
preflight requirements and existing decision/recommendation behavior.

Persistence:

ROOT / higher workflow, unchanged.

Existing result on AI failure:

The existing workflow behavior is preserved: failed analysis does not produce
a new actionable match event and does not grant application eligibility.

# Application recommendation

AI role:

Advisory score/reasoning and soft-fit assessment only.

Deterministic authority:

Root/higher workflow, trusted vacancy facts, candidate facts, preflight, and
configured safety gates.

Direct HH application:

NO.

# Failure behavior

Provider:

Returned as a system/completion error, not a candidate mismatch.

Malformed:

Business retry, then fail closed.

Validation:

Strict parse/shape/evidence checks reject invalid output.

AI unavailable:

The existing root flow handles the error and does not apply.

Partial Vacancy:

Missing critical evidence remains unknown/degraded; the model cannot invent it.

# Root compatibility

Vacancy AI type/function:

Root aliases and thin wrappers preserve existing tests/callers. `AIClient` now
adapts its existing provider/model/context into `vacancyanalysis.Service`.

Runtime prompt in root:

NONE; root prompt helper is compatibility-only and delegates to the package.

Runtime Vacancy AI parsing in root:

NONE; root parser/schema helpers delegate to the package.

Remaining compatibility:

The root `LegacyCandidateContext` is converted into typed `CandidateFacts` at
the composition boundary. Cover-letter generation remains on the root AI
client.

# Other AI

Employer reply:

R10.2 / UNCHANGED.

Candidate acquisition:

ROOT / R10.4.

Cover letter:

ROOT.

Test answering:

ROOT.

# HH

HH sync:

UNCHANGED / AI-free.

HH write:

UNCHANGED; vacancyanalysis has no HH write dependency.

LIVE HH WRITES:

0.

# Tests

Pure:

Strict parser, schema, prompt, and option characterization are covered with a
fake `CompletionProvider`.

Fact safety:

Unknown Kubernetes and unsupported Kafka claims are rejected; Django remains
grounded.

Experience:

11 months versus one-year minimum remains soft unknown.

Hard requirements:

Evidence and candidate-fact verification remain deterministic.

Root compatibility:

Existing root vacancy AI, hard-requirement, preflight, and application-safety
tests remain passing.

Write safety:

The new package has no write dependency or write call.

# Size

Root Vacancy AI/matching LOC before:

`vacancy_match.go`: 1,345 lines.

Root compatibility after:

`vacancy_match.go`: 331 lines plus `vacancy_analysis_compat.go`: 80 lines.

`vacancyanalysis` LOC:

1,315 lines including focused tests.

# Dependencies

`vacancyanalysis` depends on `internal/vacancy`,
`internal/candidate`, `internal/usecase/candidatecontext`,
`internal/ports/llm`, `internal/llm`, and the standard library only. It does
not depend on `internal/adapters/llm/openai`, HH adapters, storage, PostgreSQL,
dashboard, CLI/config, or `hhreadsync`.

# Behavior

Vacancy AI:

UNCHANGED in prompt/schema/options/retry contract, with ownership moved.

Deterministic match:

UNCHANGED and remains authoritative.

Candidate:

UNCHANGED; no mutation or fact promotion.

Application:

UNCHANGED; no auto-apply path added.

HH:

UNCHANGED.

Completion:

R10.1 transport remains unchanged; the use case consumes the existing port.

# Verification

`gofmt -w .`: PASS

`go test -count=1 ./...`: PASS

`go test -race ./...`: PASS

`go vet ./...`: PASS

`go build ./...`: PASS

`git diff --check`: PASS

`node --check web/app.js`: PASS

Focused vacancyanalysis, vacancy, candidate, candidatecontext, R10.1,
employerreply, and hhreadsync tests: PASS

Docker: SKIPPED — Docker CLI is installed, but the local Docker daemon is not
running.

LIVE HH WRITES:

0.

# Ready for R10.4

READY.

Vacancy AI has one importable owner, deterministic matching remains
authoritative, exact candidate facts are not modified or inferred, unsupported
hard requirements are rejected, and `vacancyanalysis` has no application/write
capability.
