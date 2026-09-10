# Stage V2.Fix-3a — Auto-Chat Loader HTTP 400 Acceptance

Date: 2026-09-10

# Executive summary

V2.Fix-3a: COMPLETE and CLOSED. The first auto-chat list request sent `from=0`; HH rejects that first-page form. The narrow fix omits `from` for page zero while retaining the existing GET-only read adapter. The corrected live request returned HTTP 200, the bounded review exited 0 with no proposal events, and HH writes remained 0.

No full V2 rerun was performed. Fix-3b Career and Fix-3c application preparation were not started.

# Reproduction

Path: `AutoRespondChats` → `getChatsAwaitingReply` → `getChatsThroughReadAdapterContext` → `ReadChatList` → `GET /chatik/api/chats`.

Pre-fix first request: `GET /chatik/api/chats?do_not_track_session_events=true&filterHasTextMessage=false&filterUnread=false&from=0` with an authenticated HH session. Result: HTTP 400.

# Provider error

Sanitized body: `{"code":400,"errors":[]}`.

# Root cause

The runtime represented the first page as integer page `0` and passed it as HH's `from` cursor. HH's current chat-list contract uses an opaque cursor: the first request omits `from`, and later requests use the returned `nextFrom` value. The adapter already omits `from` for an empty cursor; the compatibility bridge supplied the invalid `0`.

# Previous request contract

The first request included `from=0`, which the provider rejected.

# Correct request contract

The first request contains exactly `do_not_track_session_events=true`, `filterHasTextMessage=false`, and `filterUnread=false`; `from` is absent. A later `from` value must be the opaque provider `nextFrom` cursor.

# Implementation

In `internal/runtime/hh_read_sync.go`, `getChatsThroughReadAdapterContext` now maps page `0` to an empty cursor before calling the existing `ReadChatList` GET-only adapter. No writer, schema, orchestration policy, Career path, or application-preparation path was changed.

# Request regression tests

`internal/runtime/main_test.go:TestAutoRespondChatsUsesProposalBoundaryAndKeepsWritesDisabled` asserts GET, the exact three query parameters, absence of `from` on the first request, required `X-Requested-With` and `Accept` headers, no Authorization header, and no send/leave calls. The focused adapter/runtime tests passed.

# Live bounded loader

A direct authenticated read-only request using the corrected contract returned HTTP 200, 20 items, `found=260`, and a present opaque `nextFrom`; vacancy, resume, participant, and negotiation-topic resource maps decoded successfully.

Canonical bounded run: `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, `--run-once --auto-apply=false --auto-chat=true --auto-touch=false --auto-job-status=false --chat-mode=review`. Exit 0; no loader error; NDJSON event output had zero lines.

# Auto-chat review

The review reached the safety boundary after loading. There were no eligible proposals/events in the bounded run. No AI proposal event, send, leave, resume touch, job-search status update, application, or test submission occurred. PostgreSQL remained the configured persistence source; no JSON fallback or local conversation mutation occurred.

# Identity / R14 regression

Focused auto-chat orchestration, reply-safety, reconciliation, and identity/replay suites passed. Existing dry-run and write-boundary assertions remained green; identity mapping and trigger replay behavior were unchanged.

# Fix-1 regression

PASS. Archived conversation-detail cases for true, false, null, missing, and object-shaped values, plus detail identity validation, remained green.

# Fix-2 regression

PASS. The real PostgreSQL empty `automatic_application_attempts` table integration test passed. Empty-table authority remains `NO_ATTEMPT`; typed `sql.ErrNoRows` normalization and application-attempt policy/reconciliation suites passed.

# PostgreSQL / semantic integrity

Post-fix counts matched the pre-fix baseline:

| Entity | Count |
|---|---:|
| Candidates | 1 |
| Vacancies | 279 |
| Applications | 98 |
| Application events | 308 |
| Conversations | 259 |
| Conversation messages | 791 |
| Automatic application attempts | 0 |
| Legacy auto-chat attempts | 0 |
| Candidate semantic documents | 3 |

Read-only integrity checks returned zero orphan applications, application events, conversations, and messages; zero zero-VacancyID canonical relations; and zero duplicate stable external IDs.

Semantic status: provider `openai-compatible`; model `mistral-embed`; dimensions `1024`; unchanged active space `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9`; project eligible/indexed/stale `3/3/0`; status `READY`.

# Verification

| Check | Result |
|---|---|
| Exact pre-fix HTTP 400 reproduced | PASS |
| Corrected live bounded loader | PASS |
| Auto-chat review / no eligible action | PASS |
| HH writes | 0 |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` and command build | PASS |
| `gofmt -l .` and `git diff --check` | PASS |
| JavaScript syntax checks | PASS |
| PostgreSQL and semantic integrity | PASS |

# Decision

V2.Fix-3a is CLOSED. The HTTP 400 is fixed with a narrow first-page cursor correction, covered by a sanitized request regression test, and verified by a bounded authenticated read-only run.

V2.Fix-3b — Bounded Career One-Run is READY to start separately. V2.Fix-3c — Application Preparation remains pending. Neither stage was started or changed by this task.
