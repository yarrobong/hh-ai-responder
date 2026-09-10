# Stage R4.3b — Conversation Policy Boundary

## Before

The deterministic conversation policy was split across root files:

- `conversation_state_resolver.go`: state resolution, terminal precedence,
  waiting/action-required classification, and consistency warnings.
- `candidate_fact_resolver.go`: terminal-message detection, reply requirement,
  and external interview-action detection alongside candidate-context adapters.
- `conversation_eligibility.go`: operational destination/history checks mixed
  with semantic reply policy decisions.
- `conversation_consistency.go`: read-only comparison of conversation claims
  against employer-safe candidate knowledge.

The root consumers included AI context, daily workflow, dashboard, pilot
eligibility, notification, follow-up, and HH write preflight paths. HH status
mapping, stores, transport, preflight, and write capability remain root
concerns.

## Classification

| Symbol | Classification | Decision |
|---|---|---|
| `ConversationStatus`, `Message`, `EmployerConversation` | CONVERSATION MODEL / PERSISTED MODEL | Remain in `internal/conversation`; root aliases preserved. |
| `ConversationStateResolver` | STATE POLICY | Root adapter delegates to `conversationpolicy.ResolveState`. |
| `ConversationResolution` | DERIVED POLICY RESULT | Owned by `conversationpolicy`; root type alias preserved. |
| `ReplyRequired`, `ReplyOptional`, `NoReplyNeeded` | REPLY POLICY | Single enum in `conversationpolicy`; root aliases preserved. |
| `conversationReplyRequirement` | REPLY POLICY | Compatibility wrapper only. |
| `TerminalConversationState` | STATE POLICY | Moved to `conversationpolicy`; HH raw mapping remains an adapter. |
| `ExternalInterviewAction` | EXTERNAL ACTION / REPLY POLICY | Moved as deterministic normalized policy. |
| `USER_CONFIRMATION_REQUIRED` | CANDIDATE DEPENDENCY / INSTRUCTION | Classification remains in `candidatecontext`; policy exposes the same typed helper. |
| `ConversationConsistencyWarning` | CONSISTENCY POLICY | Moved to `conversationpolicy`; root wrapper prepares inputs. |
| `ConversationStore`, `ApplicationStore` | STORAGE | Remain root. |
| `HH_WRITE_ENABLED`, `HH_DRY_RUN`, nonce, approval, preflight | WRITE SAFETY | Remain root/HH gateway. |
| `follow_up.go`, `follow_up_service.go`, `daily_workflow.go` | FOLLOW-UP / CAREER WORKFLOW | Deliberately deferred. |
| `conversation_context.go`, `ai_reply_orchestrator.go` | AI | Deliberately deferred; consumers use compatibility adapters. |

## New boundary

Package: `internal/usecase/conversationpolicy`

Files:

- `policy.go` — normalized state resolution and meaningful-message access.
- `reply.go` — reply requirements, terminal policy, and external interview
  action classification.
- `consistency.go` — read-only claim consistency policy.
- `policy_test.go` — synthetic characterization and regression tests.

Dependencies:

```text
conversationpolicy
    -> internal/conversation
    -> internal/usecase/candidatecontext
         -> internal/candidate
```

There is no root-package, storage, filesystem, HH, AI, PostgreSQL, or config
dependency in `conversationpolicy`.

## State policy

States reuse the persisted `internal/conversation.Status` values rather than
duplicating the conversation model enum. Derived terminal, waiting,
candidate-action-required, interview, offer, rejected, closed, and manual
review decisions are produced by `ResolveState`.

Terminal application/conversation evidence wins over stale message text.
Normalized HH status evidence is supplied by the root adapter; raw HH parsing
is not in the package. Unknown required status evidence produces review rather
than a guessed state.

Interview invitations, external actions, instructions, courtesy/status
messages, and employer factual questions retain their previous classifications.

## Reply policy

Requirements remain distinct:

- `REPLY_REQUIRED`
- `REPLY_OPTIONAL`
- `NO_REPLY_NEEDED`

`EvaluateReplyPolicy` answers the semantic question “does this conversation
require a candidate reply?” It does not answer whether HH transport may send.
Transport capability, approval, fresh targeted preflight, dry-run enforcement,
nonce/idempotency, and delivery reconciliation remain outside the package.

## Candidate integration

`candidatecontext` remains the authoritative resolver for:

- `ANSWERABLE`;
- `PARTIALLY_ANSWERABLE`;
- `UNKNOWN`;
- `RESTRICTED`;
- employer-message intent;
- user-confirmation instruction detection.

Truth filtering is duplicated: **NO**. Conversation claims remain separate
from candidate truth. Unknown or partial evidence is not upgraded into a safe
claim, and the policy never mutates Candidate Knowledge.

## Consistency

`CheckConsistency` compares normalized conversation claims with a supplied
employer-safe candidate projection and optional trusted evidence. It reports
supported/unsupported/conflict/unknown outcomes through typed warnings. It is
read-only: **YES**. Candidate mutation: **NONE**.

## Deliberately deferred

- Follow-up timing and orchestration.
- Daily Career workflow, dashboard projections, and notifications.
- AI context and reply orchestration.
- HH import/sync, status parsing, transport, and writes.
- Stores, repositories, PostgreSQL, ports, and adapters.
- Candidate mutation and acquisition.

## Root compatibility

Root aliases and delegating wrappers preserve the existing API used by daily
workflow, dashboard, AI, pilot, follow-up, and HH gateway code. There is one
state implementation and one reply-policy implementation; root switches only
consume their results or add operational HH safety findings.

## Historical regressions

| Case | Result |
|---|---|
| Salary question | PASS — `REPLY_REQUIRED`; candidate answerability remains in `candidatecontext`. |
| Relocation | PASS — `REPLY_REQUIRED`; confirmed negative relocation remains answerable and is not generically blocked by this policy. |
| Courtesy/status | PASS — `REPLY_OPTIONAL`, not candidate action required. |
| Rejection | PASS — terminal / `NO_REPLY_NEEDED`. |
| Interview | PASS — existing interview intent preserved. |
| External action | PASS — `EXTERNAL_ACTION_REQUIRED`; no ordinary AI answer. |
| Instruction confirmation | PASS — `USER_CONFIRMATION_REQUIRED` remains candidate-controlled. |
| Candidate already replied | PASS — latest candidate message is not an employer reply candidate. |
| Service/system event | PASS — excluded from meaningful human timeline. |

## Write safety

Terminal send block: PASS.

No-reply send block: PASS.

Approval, targeted preflight, dry-run, transport, nonce/idempotency, stale
knowledge, and reconciliation: UNCHANGED.

LIVE HH WRITES: **0**.

## Dependencies and size

`go list -deps ./internal/conversation` reports only standard-library and
`internal/conversation` packages.

Project dependencies of `conversationpolicy` are:

```text
hh-ai-responder/internal/candidate
hh-ai-responder/internal/conversation
hh-ai-responder/internal/usecase/candidatecontext
hh-ai-responder/internal/usecase/conversationpolicy
```

The new policy implementation is 520 non-test LOC and the focused tests are
98 LOC. The three root compatibility adapters are 153 LOC combined.

## Tests

Focused package tests cover state, reply requirements, terminal behavior,
courtesy/status, rejection, interview, external action, confirmation
instructions, candidate replies, service events, consistency, and the
relocation/salary characterization cases.

Root regression coverage remains in the root package for stores, PostgreSQL,
HH sync, AI, dashboard, daily workflow, follow-up, and write gateway.

The earlier Stage 22 timing observation did not reproduce in this extraction.

## Behavior

Conversation model: UNCHANGED in persisted semantics.

Conversation state semantics: UNCHANGED.

Reply policy: UNCHANGED.

Terminal, interview, external-action, instruction-confirmation, candidate
resolution, candidate truth, consistency, follow-up, daily workflow, AI, HH
read/write, dashboard, JSON, PostgreSQL, CLI, and config behavior: UNCHANGED.

## Verification

The following checks pass:

- `gofmt -w .` on changed Go files;
- `go test -count=1 ./...`;
- `go test -race ./...`;
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`;
- `node --check web/app.js`.

Docker build: SKIPPED — the Docker CLI is present, but the daemon is not
running (`Cannot connect to the Docker daemon`).

LIVE HH WRITES: **0**.

## Architectural audit

- [x] conversation model remains standard-library-only;
- [x] `conversationpolicy` imports no root package;
- [x] `conversationpolicy` performs no I/O;
- [x] `conversationpolicy` imports no HH, AI, PostgreSQL, or storage;
- [x] state policy has one implementation;
- [x] reply policy has one implementation;
- [x] candidate truth filtering is not duplicated;
- [x] consistency is read-only;
- [x] no Candidate mutation;
- [x] follow-up and daily workflow remain deferred;
- [x] no `any`/`...any` dependency injection was introduced;
- [x] no HH writes occurred.

## Architectural debt

Root eligibility still combines semantic findings with operational HH
destination/history checks for compatibility. A later Career/HH boundary can
split those projections further, but doing so is outside R4.3b. The root
compatibility adapters should remain until all consumers migrate.

## Ready for next stage

R5.3 — Candidate Mutation + Acquisition Boundary: **READY**.

R4.3b stops here. No follow-up, Career, candidate mutation/acquisition,
semantic extraction, repository/port, JSON/PostgreSQL, HH, AI, dashboard, or
`cmd/hh-ai-responder` extraction was started.
