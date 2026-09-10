# Before

PrepareApplicationAnswer was a root AIReplyOrchestrator workflow. It loaded an
ApplicationContext, resolved vacancy-scoped CandidateContext, serialized the
application/match/vacancy/question/stories/knowledge payload, called root
callDecision, validated the result, and persisted either a draft or
clarification.

prepareApplicationDecision owned the application-answer missing-information
branch, AI call, used-facts validation, draft validation, story validation, and
draft/clarification persistence. callDecision built the application task, used
AIClient.ChatStructuredWithSchema, parsed the shared employer-reply decision,
and rejected a draft containing missing information.

ApplicationContextBuilder supplied the snapshot. CandidateContextResolver.ResolveForVacancy
remained the candidate truth and answerability authority. Relevant knowledge
and semantic selections were prepared by the root workflow; the root retained
RelevantKnowledgeHash, CandidateClarificationStore, and AIDraftStore
persistence.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| application load | root ApplicationContextBuilder | root facade | retained |
| application/vacancy snapshot | root ApplicationContext | applicationanswer.Input | detached typed projection |
| candidate context | CandidateContextResolver | resolver plus typed input | resolver remains authority |
| question | root method argument | Input.Question | preserved verbatim |
| application payload | root marshalApplicationContext | applicationanswer.MarshalInput | moved |
| application task | root callDecision | applicationanswer.BuildPrompt | moved; wording unchanged |
| schema and parser | shared employerreply | shared employerreply | reused |
| LLM call | root AIClient | ports/llm.CompletionProvider | typed boundary |
| business retry | root AIClient.chat path | applicationanswer.Service | moved |
| used-facts/draft validation | shared employerreply via root | applicationanswer | reused at new owner |
| Docker production validation | implicit root behavior | applicationanswer | explicit partial-fact guard |
| story validation | root validateStoryClaims | applicationanswer.ValidateStoryClaims | moved; root wrapper only |
| relevant knowledge/hash | root workflow | root workflow | unchanged |
| clarification persistence | root store | root facade | outside use case |
| draft persistence | root store | root facade | outside use case |
| HH write | none in application answer | none | unchanged; zero writes |
| callDecision | root runtime caller | no substantive runtime caller | removed |

# Application-answer use case

Package: internal/usecase/applicationanswer.

Service: Service.Prepare(context.Context, Input) (Result, error). Generate is
a descriptive alias.

Dependencies are CompletionProvider, application and vacancy value types,
detached candidate context/stories/semantic values, the shared employerreply
decision/schema/parser/validators, and the standard library. There is no root,
adapter, HH, storage, dashboard, CLI, Candidate writer, or concrete store
dependency.

Input is a detached typed snapshot containing application.JobApplication,
vacancy.MatchResult, vacancy description, candidatecontext.CandidateContext,
the exact question, safe stories, safe semantic selections, and a detached
relevant-knowledge snapshot. Result contains the shared Decision and a typed
outcome: draft, need_candidate_input, manual_review, or no-reply outcomes.
It is a proposed local result only.

# Shared employerreply values

The service reuses employerreply.SystemPrompt with the exact application task,
Decision and action values, ResponseFormat, ParseDecision,
ValidateUsedFacts, and ValidateDraft. The values are reused because the
provider-level structured decision contract is identical.

applicationanswer does not call employerreply.Service. Employer reply handles
a Conversation; applicationanswer answers a Vacancy/Application question.

# Distinct application semantics

The supplied question remains question data and is not reinterpreted as an
employer message, HH test, or candidate clarification. MarshalInput retains
the existing field order and names: application, match_result,
vacancy_description, candidate_context, question, stories,
verified_candidate_facts, relevant_real_examples, and
relevant_knowledge_snapshot.

The root facade now loads the snapshot, resolves CandidateContext, calls the
service, and persists only the returned draft or clarification. The service
cannot persist, submit, approve, preflight, send, or mutate Candidate data.

# Generation

The service preserves the existing application-answer values: configured
model, 900 max tokens, temperature 0.2, structured response format, and the
configured semantic attempt count. One business attempt is one
CompletionProvider.Complete call. Malformed/schema-invalid/invalid decision
output is retried according to attempts; provider errors are returned
immediately and are not converted into clarification. Provider transport retry
remains the R10.1 boundary.

The service receives caller context and has no context.Background, goroutines,
cache, batching, or concurrency. The legacy root method has no context
parameter, so the facade passes the existing AIClient context when available.

# Candidate safety

CandidateContextResolver.ResolveForVacancy remains the answerability authority.
Unknown information is not converted into a negative fact. Missing candidate
information returns need_candidate_input before completion and is persisted
only by the root workflow.

The shared deterministic validator rejects unsupported technology claims,
restricted facts, unsupported durations, invented salary values, and
contradictory relocation claims. Application-answer validation additionally
rejects production/commercial Docker claims unless the resolved Docker fact is
answerable; partial/basic Docker knowledge is insufficient.

Regressions cover unknown Kubernetes, generated negative claims for unknown
Kubernetes, partial Docker, exact 11 months without rounding, salary,
relocation, and restricted facts.

# Story safety

applicationanswer.ValidateStoryClaims owns the former story-claim algorithm.
It selects at most the same two relevant stories and checks story numbers,
technologies, outcomes, and details against the already-safe CandidateContext.
Stories alone never establish Candidate truth.

Regression coverage includes a supported story, invented story number,
technology, result/metric, and unsupported detail. The root function remains
only as a compatibility wrapper for existing root tests/callers.

# Missing information and draft boundary

Missing information produces a typed ActionNeedCandidate result. The service
does not mutate Candidate knowledge or persist clarification. The root facade
retains clarification creation and preserves application ID, topic, question,
reason, pending state, and existing multi-gap behavior.

The service does not persist or approve a draft. The root facade retains
AIDraftApplicationAnswer persistence, including draft ID, type, application
relation, decision reason, used facts, relevant-knowledge hash, status, and
timestamps. No application submission or HH test answer is performed.

# Root compatibility and residual AI

PrepareApplicationAnswer is now a thin root facade. prepareApplicationDecision
and the substantive root callDecision implementation were removed. The root
StructuredAIClient, AIClient.ChatStructuredWithSchema, and
legacyCompletionProvider remain for compatibility and other legacy paths; the
application-answer runtime no longer depends on them directly.

Follow-up, employer-reply, cover-letter, vacancy-analysis,
candidate-interpretation, test-answer, R10.1 completion, AutoRespondChats, and
HH write/preflight architecture were not redesigned.

# Tests and verification

Pure tests are in internal/usecase/applicationanswer/service_test.go and use a
typed fake CompletionProvider with no HTTP, filesystem, database, or HH access.
Root integration tests verify draft and clarification persistence outside the
use case. Live HH writes: 0.

Verification results:

- gofmt: PASS
- go test -count=1 ./...: PASS
- go test -race ./...: PASS
- go vet ./...: PASS
- go build ./...: PASS
- git diff --check: PASS
- node --check web/app.js: PASS
- focused AI/application packages: PASS
- go list dependency audit: PASS; no storage, HH, adapter, dashboard, or root dependency in applicationanswer
- Docker build: SKIPPED; Docker daemon unavailable
- live HH writes: 0

# R10 status

R10.6b1 Application Answer: EXTRACTED

Remaining substantial AI workflow: legacy AutoRespondChats.

R10: BLOCKED pending R10.6b2.

# Ready for R10.6b2

R10.6b2 — Legacy HH Auto-Chat AI Boundary: READY

This stage stops here. It does not begin AutoRespondChats extraction, R10.6c
cleanup, R11 HH writes, career/follow-up architecture, dashboard, frontend,
or cmd composition work.
