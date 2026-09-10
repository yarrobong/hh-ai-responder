# R11.1 — HH Write Transport / Capability Boundary

## Before

The repository had five live-capable HH mutation operations:

| Operation | Method/path | Before owner | Request shape | Response/evidence | Retry / ambiguity |
|---|---|---|---|---|---|
| Chat message | `POST /chatik/api/send` | `HHAIResponder.SendChatMessage` and the gateway's legacy responder writer | JSON `chatId`, `text`, `idempotencyKey` | JSON message identifier | No write retry; a transport error was treated as uncertain by the controlled writer, but raw request construction lived in root code |
| Chat leave/discard | `POST /chatik/api/leave` | `HHAIResponder.LeaveChat` | JSON `chatId` | JSON response | No write retry; response loss was a plain transport error |
| Vacancy response | `POST /applicant/vacancy_response/popup` | `HHAIResponder.SendResponse` / `ApplyVacancy` | URL-encoded application fields, resume, letter, and provider headers | JSON response map | No write retry; response loss was a plain transport error |
| Vacancy test submission | Atomic with vacancy response above | `HHAIResponder.ApplyVacancyWithTest` | Same form plus HH test metadata and `task_<id>` / `task_<id>_text` fields | Same response | Not a separate HH mutation; no separate port existed |
| Resume touch | `POST /applicant/resumes/touch` | `HHAIResponder.TouchResume` | Multipart `resume`, `undirectable=true` | HTTP 200 | No write retry |
| Job-search status | `POST /profile/shards/user_statuses/job_search_status?status=looking_for_offers` | `HHAIResponder.SetActiveJobSearchStatus` | No body; provider status headers | HTTP 200 | No write retry |

Authentication remained cookie-based: the root responder read `_xsrf` from its configured cookie jar and added it as `X-Xsrftoken`; the shared root request builder supplied browser-like headers. The root `HHRequester` retries only GET/HEAD 429 reads, so it did not retry mutations, but mutation calls were coupled to that read/requester implementation.

The write gateway continued to own approval, action state, nonce consumption, write-enabled and dry-run gates, fresh preflight, audit, and reconciliation. AI and read-sync paths had no mutation dependency.

## Mutation inventory

| Operation | Method/path | Before owner | After owner | Capability |
|---|---|---|---|---|
| Chat message | `POST /chatik/api/send` | root responder / legacy gateway writer | `internal/adapters/hh/write` | `hhwrite.ChatMessageWriter` |
| Chat leave/discard | `POST /chatik/api/leave` | root responder | `internal/adapters/hh/write` | `hhwrite.ChatLeaveWriter` |
| Vacancy response | `POST /applicant/vacancy_response/popup` | root responder | `internal/adapters/hh/write` | `hhwrite.VacancyResponseWriter` |
| Vacancy test submission | Atomic in vacancy response | root responder | `internal/adapters/hh/write` | Included in `VacancyResponseRequest.Test`; no separate capability |
| Resume touch | `POST /applicant/resumes/touch` | root responder | `internal/adapters/hh/write` | `hhwrite.ResumeWriter` |
| Job-search status | `POST /profile/shards/user_statuses/job_search_status` | root responder | `internal/adapters/hh/write` | `hhwrite.JobSearchStatusWriter` |

No separate negotiation, test, or resume mutation endpoint was invented. Read-only vacancy preflight, negotiations, chats, and resume reads remain reads.

## HH write ports

Package: `internal/ports/hhwrite`.

Capabilities:

- `VacancyResponseWriter`
- `ChatMessageWriter`
- `ChatLeaveWriter`
- `ResumeWriter`
- `JobSearchStatusWriter`

Each interface has a semantic operation name, accepts `context.Context`, and returns neutral `WriteResult` evidence. There is no generic `Do`, `Request`, `Execute`, or combined read/write client. The port package depends only on the standard library.

## HH write adapter

Package: `internal/adapters/hh/write`.

Constructor: `NewClient(Options)`.

Dependencies: injected `*http.Client`, resolved base URLs, and the already-supplied XSRF token. Browser headers and endpoint-specific headers are transport options/constants; the adapter reads no environment, `.env`, CLI flags, or global application configuration. The HTTP client is used directly, without the read scheduler.

Operations are implemented by one concrete client, which satisfies all five narrow ports. Composition decides which capability a consumer receives. The adapter owns provider DTOs, serialization, response decoding, bounded/sanitized diagnostics, and transport classification.

## Request contracts

- Vacancy response: URL-encoded fields preserve `vacancy_id`, `resume_hash`, `letter`, `ignore_postponed`, the exact application headers, and the existing referer. Atomic test answers preserve `uidPk`, `guid`, `startTime`, `testRequired`, `incomplete`, `lux`, `withoutTest`, `mark_applicant_visible_in_vacancy_country`, `country_ids`, and task answer field names.
- Test submission: not a separate mutation; represented by the optional `VacancyTestSubmission` in the vacancy response request.
- Chat message: JSON `chatId`, `idempotencyKey`, `text`; existing Chatik referer, XSRF, XHR, and browser headers are preserved.
- Chat leave: JSON `chatId`; existing Chatik referer, source, label, XSRF, XHR, and browser headers are preserved.
- Resume touch: multipart `resume` and `undirectable=true`; existing resume-list headers and referer are preserved.
- Job-search status: empty-body POST with the existing `status=looking_for_offers` query and resume-list source headers.

## Response/evidence

- Success returns `WriteResult{Outcome: accepted, ProviderStatus, ProviderID when HH supplies one, Timestamp, Metadata}`.
- A concrete non-2xx HH response returns `Outcome: rejected` and a typed `TransportError`, preserving status and bounded/sanitized diagnostic fields. 400, 401/403, 404, 409, 429, and 5xx are not retried.
- Local validation, invalid configuration, missing authentication, or canceled context before dispatch returns `Outcome: not_sent`.
- A network failure after the transport call, response-body failure, or malformed required success JSON returns `Outcome: ambiguous` and `TransportError`. It does not claim that HH rejected the mutation.

## Retry

Transport retry: **NONE**.

Business retry: outside the adapter; the existing gateway remains responsible for its no-resend safety policy.

429: concrete provider rejection/rate-limit result; no `Retry-After` scheduling or automatic resend.

5xx: concrete provider response/rejection; no automatic resend.

Timeout/reset/EOF: ambiguous when dispatch may have occurred; no retry.

Automatic resend after ambiguity: **NO**.

## Ambiguity

Ambiguous means the client cannot prove whether HH applied the mutation. The adapter marks it explicitly in `WriteResult.Outcome` and `TransportError.Outcome`. Higher-layer reconciliation remains unchanged and outside R11.1.

## Capability containment

- `hhreadsync`: no write dependency.
- AI usecases: no write dependency.
- Candidate, vacancy, application, and conversation domains: no concrete write adapter dependency.
- Root compatibility wrappers and the existing HH write gateway remain the composition boundary.

The adapter package imports only `internal/ports/hhwrite` plus standard library packages. The port package imports no adapter, root, domain, AI, or storage package.

## Root compatibility

- `SendChatMessage` retains its legacy dry-run, chat-mode, and controlled-gateway checks, then delegates transport.
- `LeaveChat` retains its legacy gates and delegates transport.
- `SendResponse`, `ApplyVacancy`, and `ApplyVacancyWithTest` retain application gates and fresh preflight; only the final mutation transport delegates.
- `TouchResume` and `SetActiveJobSearchStatus` retain their existing feature flags, dry-run behavior, and controlled-write gate; only the final mutation transport delegates.
- Existing chat write gateway and dashboard writer behavior remain source-compatible.

## Write safety

`HH_WRITE_ENABLED`: outside the adapter / unchanged.

`HH_DRY_RUN`: outside the adapter / unchanged. Root and gateway tests continue to block adapter invocation before transport.

Approval: outside.

Preflight: outside and unchanged.

Staleness: outside and unchanged.

Reconciliation: outside and unchanged.

## Tests

`internal/adapters/hh/write/client_test.go` verifies exact method/path/query/body/header contracts for all five operations, successful evidence, concrete provider rejection without retry, pre-dispatch cancellation with zero dispatches, malformed success response ambiguity, and connection failure after request-body consumption with exactly one dispatch for chat and vacancy response.

Existing root tests verify write-disabled, dry-run, approval, preflight, staleness, and reconciliation behavior. Existing auto-chat, test-answering, read-adapter, and read-sync tests remain green.

## Dependencies

```text
internal/ports/hhwrite
            ▲
            │
internal/adapters/hh/write
            ▲
            │
root compatibility / HH write gateway
```

The read adapter and `hhreadsync` graph remains independent. No AI or domain package imports either write package.

## Behavior

HH endpoint methods, paths, query parameters, form/JSON encoding, provider identifiers, authentication headers, feature gates, preflight, approval, draft state, staleness, audit ownership, and reconciliation policy are preserved. The concrete mutation transport now has one owner and one synchronous dispatch per explicit method call.

## Verification

The focused and repository-wide standard test suites pass after the extraction. `gofmt`, `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, `git diff --check`, and `node --check web/app.js` pass. Docker build was skipped because the Docker daemon was unavailable. No live HH writes were made.

## Ready for R11.2

**READY** — raw mutation transport has one adapter owner, narrow write capabilities exist, AI/read paths cannot acquire write capability, transport performs no automatic retry, ambiguous delivery is explicit, and approval/preflight/reconciliation remain higher-level.

R11.1 stops here. R11.2 gateway extraction, R11.3 preflight/approval isolation, and R11.4 reconciliation redesign are not started.
