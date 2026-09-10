# R10.6a — Follow-up Draft Generation Boundary

## Before

`AIReplyOrchestrator.PrepareFollowUp` loaded the application and conversation
snapshot, evaluated `FollowUpEngine`, constructed the follow-up payload, called
root `callDecision`, validated generated text, re-evaluated eligibility, and
persisted an `AIDraft`.

`callDecision` used the root `StructuredAIClient` and
`ChatStructuredWithSchema`, then reused `employerreply.ParseDecision` after the
legacy completion call. Its remaining non-follow-up caller is application
answer generation.

Eligibility was deterministic and owned by `FollowUpEngine` in `follow_up.go`.
Prompt task construction and follow-up payload construction were in the root
workflow. Candidate fact and high-risk checks were also applied by the root
workflow before draft persistence.

Fingerprinting was performed by the root `followUpFingerprint` function, and
concrete draft persistence was performed by the root `AIDraftStore` facade.

## Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| Follow-up eligibility | root `FollowUpEngine` | root `FollowUpEngine` | unchanged |
| Context build | root conversation context builder | root workflow | unchanged |
| Prompt/task | `PrepareFollowUp` + shared prompt | `followupdraft` | extracted |
| Structured schema/parser | shared `employerreply` values | shared `employerreply` values | reused exactly |
| LLM call | root `callDecision` / legacy structured client | `followupdraft.Service` + `CompletionProvider` | extracted |
| Business retry | root structured client path | `followupdraft.Service` | malformed structured output only |
| Candidate fact validation | root wrappers over shared validator | `followupdraft` using shared validator | extracted |
| High-risk generated-text validation | root classifier and facade | shared `conversationpolicy` classifier + `followupdraft` | centralized, behavior preserved |
| Fresh recheck | root workflow | root workflow | preserved and made genuinely fresh |
| Fingerprint | root | root | unchanged hash/content function |
| Draft persistence | root `AIDraftStore` | root `AIDraftStore` | unchanged boundary |
| HH write | root HH write gateway | root HH write gateway | untouched |

## Follow-up draft use case

Package: `internal/usecase/followupdraft`

Service: `followupdraft.Service`

Dependencies:

- `internal/llm`
- `internal/ports/llm.CompletionProvider`
- `internal/usecase/employerreply` for shared context, decision, schema,
  parser, and Candidate fact validation
- `internal/usecase/conversationpolicy` for the established high-risk classifier
- standard library

Input is a typed detached snapshot containing the shared safe conversation
context, deterministic eligibility result, and confirmed delivered follow-up
history. It contains no repository, HH DTO, configuration, or untyped map API.

Output is `followupdraft.Result`, containing a proposed shared structured
decision only. It contains no send, approval, preflight, persistence, nonce, or
delivery capability.

## Eligibility

Authority: root `FollowUpEngine`.

Pre-generation: `PrepareFollowUp` still evaluates eligibility before calling
`followupdraft.Service`; ineligible states make zero completion calls. The
service also gates non-`eligible` inputs defensively.

LLM can override: NO.

Terminal, candidate-action-required, candidate-already-replied, timing,
follow-up-limit, dismissed, incomplete-history, and pending-clarification
decisions remain deterministic policy decisions.

## Prompt

Owner: `followupdraft.Task`, `followupdraft.SystemPrompt`, and
`followupdraft.MarshalInput`.

The task text, system safety instructions, context field names, context order,
message ordering, history field, JSON requirement, model, temperature, and
token limit are preserved. The shared employer-reply system prompt is reused
as the common pure employer-safe instruction layer; the follow-up task remains
owned by `followupdraft`.

Employer-reply reuse: exact `employerreply.ResponseFormat`,
`employerreply.Decision`, and `employerreply.ParseDecision`; no second schema or
provider-level decision type was introduced.

## Structured decision

Shared type/schema: `employerreply.Decision` and `ResponseFormat`.

Follow-up result: `followupdraft.Result` wraps the shared decision to keep the
workflow meaning distinct from an ordinary employer reply.

Parsing and business validation are performed inside `followupdraft.Service`.
Malformed structured output retries up to the configured semantic attempt
count. A provider failure is returned immediately; transport retry remains
owned by the R10.1 provider boundary. Invalid generated text becomes manual
review and never becomes a persisted actionable draft.

## Retry

Business attempts: the configured `AIClient.attempts` value, with the existing
default of one.

Provider attempts: R10.1 provider only.

Provider failure: returned as a structured AI error and does not consume a
follow-up semantic retry.

Call-count coverage includes eligible success, ineligible input, malformed
then valid output, provider failure, and cancellation.

## Candidate safety

- Unknown Kubernetes remains unknown and a generated Kubernetes claim is
  rejected.
- Canonical total experience of exactly 11 months is not rounded to one year;
  conflicting generated durations are rejected.
- Salary claims are not invented; salary/high-risk output is routed to review.
- Explicit no-relocation remains incompatible with willingness-to-relocate.
- Restricted Candidate facts cannot enter a valid generated draft.

The existing `employerreply.ValidateUsedFacts` and `ValidateDraft` functions
remain the single Candidate fact-safety implementation.

## Conversation/follow-up safety

Terminal conversations, rejected/closed states, candidate-already-replied
states, and external-action/high-risk content cannot become actionable drafts.
The follow-up service never sends a message, approves a draft, executes a test,
or performs another external action.

## Freshness

After generation, the root workflow rereads the application, event list,
conversation list, and clarifications before evaluating eligibility again.
If a candidate reply or terminal state appears during generation, the generated
proposal is discarded and no draft is persisted. This is covered by
`TestFollowUpFreshRecheckDiscardsDraftAfterCandidateReply`.

## Fingerprint

Owner: root `followUpFingerprint` and the root draft workflow.

Behavior: UNCHANGED. The LLM never creates the fingerprint. The fresh eligible
snapshot supplies the fingerprint used for persistence.

## Draft boundary

Can persist directly: NO.

Can send: NO.

Can approve: NO.

Persistence owner: root `AIDraftStore` after fresh eligibility and fingerprint
checks.

## Root compatibility

`PrepareFollowUp` is now a workflow facade: it loads context, evaluates policy,
builds typed input, calls `followupdraft.Service`, performs a fresh recheck,
fingerprints, and persists.

Previously saved follow-up drafts are also revalidated through the pure
`followupdraft.ValidateSavedDraft` entry point; the root facade does not own
Candidate fact-validation logic.

`callDecision` remains only for the existing application-answer compatibility
path. Follow-up generation no longer depends on it or on root
`AIClient.ChatStructuredWithSchema` for semantic retry.

The root `legacyCompletionProvider` remains only as a compatibility adapter for
older `StructuredAIClient` callers and tests. The importable follow-up package
depends only on `CompletionProvider`.

## Residual AI after R10.6a

Legacy `HHAIResponder.AutoRespondChats` remains the next substantial AI
workflow and is intentionally untouched for R10.6b.

Other remaining structured callers are compatibility/root paths, employer-reply
R10.2, application-answer compatibility, and the provider implementation.

## Tests

Pure follow-up tests cover exact prompt/options/payload, typed-provider use,
eligibility call gating, malformed-output retry, provider failure,
cancellation, Kubernetes safety, salary/relocation/high-risk rejection, and
shared Candidate fact validation.

Freshness coverage verifies candidate reply during generation prevents
persistence. Existing follow-up eligibility, timing, draft, fingerprint, and
write-safety tests remain green. Employer reply and other AI use-case tests are
unchanged and green.

No test performs a live HH write.

## Dependencies

`go list -deps ./internal/usecase/followupdraft` contains only the intended
domain/use-case/LLM-port dependencies and standard library packages. It has no
dependency on `internal/adapters/llm/openai`, HH read/write adapters, storage,
Dashboard, or root `main`.

## Behavior

Follow-up: UNCHANGED.

Employer reply: UNCHANGED.

Candidate truth/safety: UNCHANGED.

HH writes: UNCHANGED; live HH writes during this stage: 0.

## Verification

`gofmt -w .`: PASS

`go test -count=1 ./...`: PASS

`go test -race ./...`: PASS

`go vet ./...`: PASS

`go build ./...`: PASS

`git diff --check`: PASS

`node --check web/app.js`: PASS

Docker was available as a CLI, but the daemon was not running, so
`docker build -t hh-ai-responder:r10-6a .` was SKIPPED after the daemon
connection check.

LIVE HH WRITES: 0.

## R10 status

R10.6a Follow-up draft: EXTRACTED.

Remaining substantial AI workflow:

- Legacy HH AutoRespondChats

R10: BLOCKED pending R10.6b.

## Ready for R10.6b

R10.6b — Legacy HH Auto-Chat AI Boundary: READY.

No R10.6b, R11, scheduler, dashboard, frontend, or composition work was
started.
