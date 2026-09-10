# Before

Cover-letter entry points:

- `AIClient.GenerateLetter` in `main.go`.
- `AIClient.GenerateLetterWithEvaluation` and
  `AIClient.GenerateLetterWithEvaluationAndSemantic` in `vacancy_match.go`.
- The application-preview `AIReplyOrchestrator.PrepareCoverLetter` path,
  which previously reused the employer-reply decision service.

Prompt:

The plain-text cover-letter prompt, candidate projection formatting, story
selection/formatting, vacancy/evaluation context, semantic-example section,
and communication profile were root-owned or duplicated across those paths.

Candidate context:

The existing `LegacyCandidateContext` and canonical employer-safe projection
remain the sources. The extracted service receives a detached typed projection
and does not read or mutate Candidate state.

Vacancy context:

The existing `vacancy.Vacancy` value and separately fetched vacancy description
are passed through unchanged. No HH read is performed by the use case.

Vacancy assessment:

The evaluated path passes the validated `vacancyanalysis.Assessment` as
auxiliary prompt context. Cover-letter generation does not rerun analysis or
make an application recommendation.

Semantic/relevant knowledge:

Already-safe semantic selections are converted to relevance-only
`coverletter.SemanticHint` values. Similarity is not Candidate evidence and
the use case does not query a semantic repository. `RelevantKnowledgeHash` is
still calculated and persisted by the existing higher workflow only.

Completion:

The new service calls only `internal/ports/llm.CompletionProvider` once with
the established plain-text request: ordered system/user messages,
`max_tokens=512`, `temperature=0.5`, configured model, and no structured
response format.

Validation:

The existing exact-month validator moved into the package. The package also
fails closed for empty output and deterministic cover-letter claims that would
promote unknown/partial Kubernetes, Kafka, Celery, or Docker production facts,
unsupported seniority/education/English, or contradictory relocation and
business-trip preferences.

Retry:

The current cover-letter paths had no cover-letter-specific semantic retry:
validation happened after the single `Chat` call. That call count remains one;
provider transport retry remains owned by R10.1.

Application integration:

The daily vacancy workflow still performs vacancy analysis, deterministic
decision, fresh preflight, optional letter generation, and then the existing
application flow in the same order. `PrepareCoverLetter` now calls the
cover-letter service and adapts its validated text back to the existing draft
decision/persistence shape. No application write moved into the use case.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| Cover-letter prompt | root `main.go` | `coverletter/prompt.go` | move |
| Candidate prompt projection | root compatibility context | `coverletter.CandidateFacts` plus canonical safe values | typed input |
| Story selection and formatting | root `candidate_stories.go` / prompt | `coverletter/prompt.go` | move for runtime letter generation; root story helper retained for other communication flows |
| Vacancy fields/description | root workflow | `coverletter.Input` | pass through |
| Validated vacancy assessment | `vacancyanalysis` result | `coverletter.Input.Assessment` | consume; do not rerun |
| Semantic selections | root retrieval/context | `coverletter.SemanticHint` | relevance-only projection |
| LLM call | root `AIClient.Chat` | `CompletionProvider` | move |
| Plain-text output | root string return | `coverletter.Result` | typed result |
| Empty-output handling | implicit root string behavior | `ErrEmptyLetter` | fail closed |
| Experience fact validation | root `validateGeneratedLetterExperience` | `coverletter.ValidateLetter` | move |
| Other cover-letter fact checks | previously mostly prompt-only | `coverletter.ValidateLetter` | deterministic fail-closed boundary |
| Business retry | none for cover letters | none | unchanged |
| Provider retry | R10.1 | R10.1 | unchanged |
| Root compatibility methods | root algorithm | `cover_letter_compat.go` adapters | retain source compatibility |
| Application draft persistence | root orchestrator | root orchestrator | retain |
| Application preflight/write | root HH workflow/gateway | unchanged | retain outside use case |

# Cover-letter use case

Package:

`internal/usecase/coverletter`

Service:

`coverletter.NewService(coverletter.Dependencies{Completion: provider},
coverletter.Options{Model: model, MaxTokens: 512, Temperature: 0.5})`.

Dependencies:

`internal/ports/llm.CompletionProvider` only at runtime, plus neutral value
owners `internal/candidate`, `internal/usecase/candidatecontext`,
`internal/usecase/vacancyanalysis`, and `internal/vacancy`.

Input:

Typed `coverletter.Input` contains bounded `CandidateFacts`, selected stories,
`vacancy.Vacancy`, description, optional validated vacancy assessment, optional
application match context, relevance-only semantic hints, and the existing
extra prompt. It contains no map-based API, repository, HH DTO, writer, or
application gateway.

Output:

`coverletter.Result{Letter string}` is validated draft text only. It has no
approval, persistence, preflight, send, apply, or mutation method.

# Prompt

Owner:

`internal/usecase/coverletter/prompt.go` and its embedded copy of the existing
communication profile.

Candidate projection:

The existing full name, resume title, salary, skills, location, education,
exact structured experience, trusted profile communication rules, optional
configured contacts, canonical employer-safe context, and selected story
fields are preserved. No new contact or private-conversation field is added.

Vacancy projection:

The existing vacancy title, company, and description are preserved. The
evaluated path preserves the existing strong-match, missing, and reason
context; the application-preview path preserves saved matched skills/projects
and missing skills.

Assessment/context:

Assessment and semantic values are explicitly labeled auxiliary/data-only
context. They cannot establish Candidate truth.

Model/options:

Configured model, 512 max tokens, 0.5 temperature, two ordered messages, and
plain text response mode are unchanged.

Behavior changed:

No intended prompt-quality or workflow-order change. Empty/unsafe generated
text now fails closed before a typed result is returned.

# Fact safety

Known:

Confirmed employer-safe Candidate facts remain usable; the Django regression
passes.

Partial:

Partial Docker capability cannot be promoted to production/commercial Docker
experience without a confirmed commercial use context.

Unknown:

Unknown Kubernetes, Kafka, and Celery are not promoted from backend, Docker,
REST, or semantic-example context. A positive Kubernetes production claim is
rejected.

11 months:

Exact 11-month structured experience cannot become “one year”; the existing
rounding error contract is preserved.

Seniority:

Senior/Lead/Middle+ wording is rejected unless the canonical resume/profile
role context supports it.

Education:

Bachelor/master/university wording is rejected when no confirmed education is
available.

English:

B2 cannot be promoted to fluent/native/C1 without a matching safe language
fact.

Relocation:

Positive relocation wording is rejected when the canonical preference says no.
The same contradiction check applies to business trips.

# Semantic/relevant knowledge

Role:

Relevance hints help select or frame examples only.

Authority:

Canonical Candidate facts and the employer-safe projection remain the sole
authority for factual claims. Semantic score, selected text, and example
similarity are not evidence.

Hash behavior:

UNCHANGED. Cover-letter generation does not modify `RelevantKnowledgeHash` or
Candidate state.

# Generation

Completion provider:

`internal/ports/llm.CompletionProvider`.

Business attempts:

Exactly one for success, invalid facts, empty output, and provider errors,
matching the pre-extraction cover-letter behavior. Cancellation stops before a
provider call when the caller context is already canceled.

Provider attempts:

R10.1.

Call counts:

Pure fake-provider tests cover success, empty output, provider failure,
cancellation, and all deterministic safety regressions. The application
preview compatibility test continues to use one provider call.

# Failure behavior

Provider:

Returned as a distinct wrapped completion error; it is not converted into an
empty successful letter.

Empty:

`ErrEmptyLetter`; no usable result reaches preview/application callers.

Invalid fact:

`ErrUnsupportedCandidateFact` or the established exact-month validation error;
the result is withheld.

Retries exhausted:

Not applicable to the cover-letter business layer; there is no semantic retry.

Cancellation:

Caller context cancellation is returned unchanged and prevents generation when
already canceled.

# Application boundary

Can create application:

NO.

Can submit HH:

NO.

Can approve:

NO.

Can preflight:

NO.

Result consumer:

The daily root workflow consumes the legacy string adapter before existing
preflight/write logic. The application-preview orchestrator consumes the typed
result through a root decision adapter and retains existing draft persistence.

# Root compatibility

`AIClient.GenerateLetter`:

Thin adapter in `cover_letter_compat.go` that constructs typed input and
delegates to `coverletter.Service`.

`AIClient.GenerateLetterWithEvaluation` and
`GenerateLetterWithEvaluationAndSemantic`:

Thin adapters that pass the validated assessment and safe semantic hints to the
same service.

Runtime prompt in root:

NONE. `buildLetterSystemPrompt` remains only as a compatibility wrapper for
legacy tests/callers and delegates to `coverletter.SystemPromptWithStories`.

Runtime cover-letter validation in root:

NONE.

# Other AI

Employer reply:

UNCHANGED; it remains owned by `internal/usecase/employerreply`.

Vacancy analysis:

UNCHANGED; it remains owned by `internal/usecase/vacancyanalysis` and its
validated result is only consumed by coverletter.

Candidate interpretation:

UNCHANGED.

Test answering:

ROOT / R10.5b; `SolveTests` and its structured schema were not moved.

Generic Chat direct callers:

- `main.go`: employer-chat generation still calls `AIClient.Chat`.
- `main.go`: `SolveTests` still calls `ChatStructuredWithSchema`.
- `ai_reply_orchestrator.go`: employer-reply structured decisions still use
  `ChatStructuredWithSchema` through the compatibility provider.
- `employer_reply_compat.go`: legacy structured compatibility bridge remains.
- `main_test.go`: transport/compatibility tests call the generic methods.

There is no remaining runtime cover-letter call to root `Chat` or the employer
reply service.

# Tests

Pure:

`internal/usecase/coverletter/service_test.go` uses a typed fake provider with
no HTTP and verifies message order, options, prompt sections, empty/provider
errors, and cancellation.

Fact safety:

Regressions cover known Django, unknown Kubernetes, partial Docker production
overclaim, exact 11 months versus one year, unsupported seniority, relocation
contradiction, and semantic hints remaining non-authoritative.

Application/preflight:

Existing vacancy matching, response-letter-required, application preview,
fresh-preflight, dry-run, manual-control, and HH write-gateway tests remain
outside the use case and pass. No live HH write was performed.

# Size

Root cover-letter algorithm before:

Approximately 117 LOC across `main.go` and `vacancy_match.go`, excluding tests.

Root compatibility after:

91 LOC in `cover_letter_compat.go`; it contains adapters only.

Coverletter production code:

674 LOC across typed input/result, prompt, service, validation, and package
documentation; 140 LOC are pure tests.

# Dependencies

`go list -deps ./internal/usecase/coverletter` confirms no dependency on the
root package, HH read/write adapters, storage implementations, PostgreSQL,
dashboard, CLI/config, or concrete LLM adapters. The package has no Candidate
writer and no application writer.

# Behavior

Cover letter:

UNCHANGED in prompt structure, model/options, plain-text response shape,
workflow order, semantic-hint authority, and provider call count; invalid and
empty results now fail closed at the typed boundary.

Candidate:

UNCHANGED; no truth acquisition, confirmation, mutation, persistence, or hash
change occurs.

Vacancy:

UNCHANGED; assessment and deterministic recommendation remain outside.

Application:

UNCHANGED; preview/persistence adapters retain their existing JSON/draft shape.

HH:

UNCHANGED; live HH writes during this task: `0`.

# Verification

`gofmt -w .`: PASS

`go test -count=1 ./...`: PASS

`go test -race ./...`: PASS

`go vet ./...`: PASS

`go build ./...`: PASS

`git diff --check`: PASS

`node --check web/app.js`: PASS

Focused coverletter, vacancyanalysis, employerreply, candidateinterpretation,
candidatecontext, candidate, llm, ports/llm, and openai adapter tests: PASS.

Docker:

SKIPPED. Docker CLI is installed, but the daemon is unavailable
(`Cannot connect to the Docker daemon at unix:///var/run/docker.sock`).

LIVE HH WRITES:

0.

# Ready for R10.5b

READY.

- Cover-letter generation has one importable runtime owner.
- Candidate fact safety is deterministic and fail-closed.
- `coverletter` has no application, HH, storage, or mutation capability.
- Generic `Chat` residual callers are identified above.

# Stop

R10.5a complete. HH test answering, generic AI compatibility cleanup, HH write
extraction, career/follow-up, dashboard, frontend, and cmd composition work
were not started.
