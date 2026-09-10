# R12.2a — Controlled Conversation Draft Orchestration Boundary

## Before

### `PrepareEmployerReply`

The root `AIReplyOrchestrator` performed the complete controlled preparation
sequence:

```text
ConversationContextBuilder.BuildForReply
  -> ConversationStore.GetConversation/GetConversationTimeline
  -> CandidateContextResolver employer-safe projection
  -> conversation consistency and clarification projection
  -> relevance-only semantic retrieval and RelevantKnowledge snapshot
  -> employerDraftMetadata/message + knowledge fingerprint
  -> scan generated AIDraftStore records for exact conversation fingerprint
  -> employerreply.Service.Prepare
  -> Candidate clarification persistence OR AIDraftStore.Create
```

The leaf service remained the owner of deterministic reply eligibility,
high-risk handling, candidate-input gating, AI calls, parsing, retries and
draft validation. Reuse matched a generated `AIDraftEmployerReply` by
conversation ID and the existing cache key. There was no post-generation
employer-reply reread in the audited root method; the initial local snapshot
was the freshness boundary.

### `PrepareFollowUp`

The root method performed:

```text
ApplicationStore.GetApplication
  -> application/event/conversation/clarification snapshot reads
  -> CareerSnapshot.input
  -> ConversationContextBuilder.BuildForReply
  -> FollowUpEngine.Evaluate
  -> exact followUpFingerprint reuse check + saved-draft validation
  -> followupdraft.Service.Prepare
  -> clarification persistence OR fresh application/conversation/context reread
  -> fresh FollowUpEngine.Evaluate
  -> AIDraftStore.Create
```

The fresh reread was the race boundary: a candidate reply, terminal state,
changed eligibility, or unavailable fresh state prevented draft persistence.
There was no second AI generation.

### Stores and context

`ConversationStore`, `ApplicationStore`, `AIDraftStore`, and
`CandidateClarificationStore` were root compatibility façades over the staged
storage adapters. Candidate truth came from `CandidateContextResolver`; the
semantic retriever supplied relevance-only examples and snapshot metadata.
Clarifications used the existing typed candidate-acquisition path first, with
the legacy exact pending `(conversation, application, topic, question)`
deduplication fallback. Drafts were local generated records only.

### Other callers

Dashboard generate endpoints, dashboard sync, and the HH sync CLI called the
root preparation façade. `executeSync`, `AutoRespondChats`, application-answer
preparation, approval, preflight, gateway, and reconcile paths were not part of
this extraction.

## Characterization — employer reply

| Case | Existing behavior | Preserved |
|---|---|---|
| Normal confirmed reply | One typed leaf call; generated local draft | Yes |
| No reply / terminal / external action | No provider call; no draft | Yes |
| Unknown fact or confirmation-required instruction | Typed candidate clarification; no provider call | Yes |
| Same conversation and current input | Existing generated draft reused | Yes |
| Semantic context | Relevance-only selections and existing knowledge hash metadata | Yes |
| Provider error | Returned; no orchestration retry | Yes |
| Store failure | Returned after the same preparation side-effect point | Yes |

## Characterization — follow-up

| Case | Existing behavior | Preserved |
|---|---|---|
| Ineligible / terminal / candidate action required | Manual-review result; zero provider calls | Yes |
| Eligible new draft | One typed leaf call, fresh recheck, local draft | Yes |
| Matching generated fingerprint | Reuse; zero provider calls | Yes |
| Candidate reply during generation | Fresh evaluation invalidates proposal; no draft | Yes |
| Pending clarification / invalid state | Manual review; no provider call | Yes |
| Provider or store failure | Returned; no hidden regeneration | Yes |

## Employer reply workflow

Package: `internal/usecase/employerreplyworkflow`

The service accepts one conversation ID and a task. Its loader owns loading the
detached conversation/application-related context, candidate projection,
semantic relevance and exact draft metadata. The service then checks the
existing reusable generated draft, calls only `employerreply.Service`, and
persists only a typed clarification or generated local draft.

The root composition adapter maps the existing stores and builder into narrow
workflow ports. It preserves the candidate-acquisition clarification path,
draft fields, model metadata, cache key, and deduplication behavior.

Result outcomes are workflow-local (`DRAFT_READY`,
`NEEDS_CANDIDATE_INPUT`, `NO_REPLY`, `MANUAL_REVIEW`, `REUSED`). None is send
authorization.

## Follow-up orchestration

Package: `internal/usecase/followuporchestration`

The service accepts one application ID and evaluation time. Its loader returns
a detached snapshot; an injected evaluator adapts the existing deterministic
`FollowUpEngine`; matching generated drafts are checked before generation;
`followupdraft.Service` is called only when eligible; and generated proposals
are always followed by `Reload` plus a second eligibility evaluation before
`Save`.

The root adapter preserves the exact `followUpFingerprint`, saved-draft
validation, eligibility warnings, model metadata, and candidate-reply/terminal
invalidations. Invalid reusable drafts produce manual review rather than an
error or a regenerated proposal.

## Candidate safety

`CandidateContextResolver` remains the root composition adapter to the staged
Candidate truth projection. The new packages do not mutate Candidate truth.
Semantic selections remain relevance-only and are not promoted to facts.

## Clarifications

Only typed leaf `need_candidate_input` outcomes are handed to the local
clarification writer. Existing candidate-acquisition gap deduplication remains
the first path; the exact legacy pending-record fallback remains available.
Candidate answer handling and learning remain outside R12.2a.

## Freshness

Employer reply preserves the audited behavior: initial context is loaded before
generation and the existing root method had no post-generation reread.
Follow-up preserves the mandatory post-generation fresh reread and evaluation;
changed eligibility, candidate reply, terminal state, or fresh-state failure
prevents persistence without a second provider call.

## Capability containment

The two new packages import neither root package code, dashboard/CLI code,
concrete storage adapters, HH-read/write adapters, nor approval/preflight/
reconcile packages. They call only typed leaf services and local persistence
ports. They contain:

```text
HH writes: 0
Approvals/nonces: 0
Candidate truth mutations: 0
Direct CompletionProvider calls: 0
Auto-chat changes: 0
```

## Root compatibility

`AIReplyOrchestrator.PrepareEmployerReply` is now a thin delegate to
`employerreplyworkflow.Service`. `PrepareFollowUp` constructs the workflow
with the caller's existing policy and delegates to it. Root `AnalyzeConversation`
is a separate non-persisting compatibility path. Application-answer
preparation remains root-owned and is a residual candidate for a later stage;
it was not silently included in R12.2a.

Dashboard and CLI API shapes are unchanged. `executeSync` and
`AutoRespondChats` were not refactored.

## Tests

Added pure orchestration tests with fake loaders, evaluators, leaf services,
draft stores, and clarification writers. They cover context propagation,
normal persistence, reuse without provider calls, ineligible gating, fresh
invalidation, and fresh eligible persistence. Existing root tests cover the
broader employer-reply, follow-up, dashboard, R10, R11, and local-only
behavioral regressions.

## Verification

The required verification completed as follows:

```text
gofmt -w .                                                        PASS
go test -count=1 ./...                                             PASS
go test -race ./...                                                PASS
go vet ./...                                                       PASS
go build ./...                                                     PASS
git diff --check                                                   PASS
node --check web/app.js                                             PASS
go test ./internal/usecase/employerreplyworkflow/...                PASS
go test ./internal/usecase/followuporchestration/...                PASS
go test ./internal/usecase/employerreply/...                        PASS
go test ./internal/usecase/followupdraft/...                        PASS
go test ./internal/usecase/conversationpolicy/...                    PASS
dependency audit for HH write/approval/root imports                 PASS
docker build -t hh-ai-responder:r12-2a .                            SKIPPED (Docker daemon unavailable)
```

No HH write request was made during tests or verification.

## R12 status

R12.2a: EXTRACTED

Ready for R12.2b inventory only after the full verification below. R12.2b,
R12.2c, R12.3, R12.4, R12.5, and R12.6 are not started by this change.
