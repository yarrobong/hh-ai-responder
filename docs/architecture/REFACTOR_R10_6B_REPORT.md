# Stage R10.6b — Legacy HH Auto-Chat AI Boundary

## Status

**R10.6b BLOCKED.**

The required application-answer precondition audit found a substantial
independent AI workflow. The auto-chat extraction was not started, so the
legacy `HHAIResponder.AutoRespondChats` behavior and all HH write paths remain
unchanged.

Additional substantial workflow:

`AIReplyOrchestrator.PrepareApplicationAnswer` and its
`prepareApplicationDecision` / `callDecision` path.

The workflow must be extracted or given an explicit boundary before the
remaining generic AI compatibility surface can be safely reduced and before
R10.6b can proceed.

## Precondition audit — application answer

### Entry point

`AIReplyOrchestrator.PrepareApplicationAnswer(applicationID, question)` in
`ai_reply_orchestrator.go`.

The method builds an application context, resolves a vacancy-specific
CandidateContext, and delegates to `prepareApplicationDecision` with the
application-answer task. Repository-wide search found the method definition;
no separate active runtime caller was found in the current tree, but the
implemented path is a complete callable workflow and cannot be treated as
dead compatibility code without an explicit removal decision.

### Classification

**SUBSTANTIAL independent workflow (C).**

It is not an existing compatibility path into R10.2 `employerreply.Service`:
it does not call `employerreply.Service.Prepare`, and it supplies a different
application context and task. It is also not a thin adapter around an already
extracted application-answer use case; no such use case exists.

### Input

- application ID;
- application question;
- application aggregate and match result loaded by `ApplicationContextBuilder`;
- vacancy description and company/title context;
- vacancy-scoped `CandidateContext` resolved by
  `CandidateContextResolver.ResolveForVacancy`;
- configured Candidate stories and relevant knowledge already held by the
  orchestrator.

### Prompt owner

The root `callDecision` path owns the application-specific prompt assembly:

- `employerreply.SystemPrompt("ответ на вопрос вакансии", extraPrompt)`
  provides the shared system safety prompt;
- `marshalApplicationContext` in `ai_reply_orchestrator.go` owns the
  application-answer payload and field selection;
- `AIReplyOrchestrator` owns the task and additional prompt composition.

This is not the R10.2 employer-conversation input contract.

### Schema owner

`employerReplySchema()` / the employer-reply response schema is reused by the
root compatibility path. Reuse of the schema does not make the workflow
semantically equivalent to employer reply because the task, input payload,
validation, persistence, and application semantics differ.

### Parser and retry owner

- Parser: `employerreply.ParseDecision`, invoked by root `callDecision`.
- Semantic/business retry: root `AIClient.chat`, reached through
  `ChatStructuredWithSchema`; the retry count follows `AIClient.attempts`.
- Transport retry: the configured `CompletionProvider` / R10.1 adapter.

The root path therefore still has a substantive validator-driven completion
caller that cannot be removed or rerouted as part of auto-chat extraction
without changing application-answer behavior and call counts.

### Candidate safety owner

The application-answer workflow has its own post-generation safety sequence:

1. `validateAIUsedFacts` checks `used_facts` against the resolved
   `CandidateContext`.
2. `ValidateAIDraft` checks forbidden claims, restricted facts, technologies,
   exact experience duration, relocation contradictions, and invented salary.
3. `validateStoryClaims` checks story numbers, technologies, outcomes, and
   details against the safe Candidate context.
4. Missing Candidate information becomes a persisted clarification instead of
   a draft.

These checks are application-answer business logic in the root workflow, not
merely an adapter around R10.2.

### Output

`AIResponseDecision`, with actions including draft reply, need candidate input,
manual review, and no-reply equivalents. A draft is produced only after the
structured response has been parsed and the deterministic safety checks pass.

### Persistence

- application-answer drafts are persisted through `AIDraftStore` as
  `AIDraftApplicationAnswer`;
- missing information is persisted through `CandidateClarificationStore`;
- draft metadata includes the application ID, decision reason, used facts, and
  relevant-knowledge hash.

### Write capability

The application-answer path has no direct `SendChatMessage`, `LeaveChat`, or
`HHWriteGateway` capability. Its material side effects are local draft and
clarification persistence. HH application submission is outside this path.

## Why R10.6b stops

The task explicitly requires stopping when the application-answer caller is a
substantial independent workflow. Extracting `AutoRespondChats` now would
leave a second substantial root AI generation/retry path hidden inside
`callDecision`, producing a false residual-AI closure and making the generic
compatibility boundary misleading.

No auto-chat package, prompt migration, retry migration, review migration, or
root `AutoRespondChats` write-path change was made.

## Auto-chat audit snapshot

| Area | Current owner | R10.6b decision |
| --- | --- | --- |
| HH chat list/history reads | `HHAIResponder` and HH read compatibility/read adapter | unchanged; not moved |
| Detached chat input normalization | root `ChatToReply` / `ChatDataResponse` path | not started |
| Legacy auto-chat prompt | `buildChatSystemPrompt`, `AutoRespondChats`, `candidate_communication.md` | not moved |
| Button/text-button handling | `getChatsAwaitingReply`, `AutoRespondChats` | not moved |
| Plain-text completion | `AIClient.Chat` → `AIClient.chat` | not moved |
| AI semantic retry | `AIClient.chat` | not moved |
| Chat review classifier | `chatReplyReviewReasonForContext` / `conversationpolicy` high-risk classifier | not moved |
| HH send | `HHAIResponder.SendChatMessage` | unchanged |
| HH leave | `HHAIResponder.LeaveChat` | unchanged |

## Existing extracted workflows

The precondition does not invalidate prior boundaries:

- follow-up drafts remain owned by `internal/usecase/followupdraft`;
- employer replies remain owned by `internal/usecase/employerreply`;
- vacancy analysis, candidate interpretation, cover letters, and test
  answering remain in their existing use-case packages.

The application-answer path is the additional blocker that must be resolved
before R10.6b can claim a single importable owner for the remaining legacy
auto-chat AI surface and before R10.6c cleanup can begin.

## Safety and verification

- Auto-chat implementation changes: **none**.
- HH write transport changes: **none**.
- Live HH writes during this stage: **0**.
- `HH_WRITE_ENABLED=false` / `HH_DRY_RUN=true`: preserved for any future
  testing of the live workflow.
- Full verification suite: not run because the mandatory precondition requires
  stopping before implementation.

## R10 status

**R10: BLOCKED.**

Next required stage: extract or explicitly retire the application-answer
workflow, documenting its own typed boundary and safety ownership. Only then
may R10.6b resume for legacy HH auto-chat.

**READY: BLOCKED.**
