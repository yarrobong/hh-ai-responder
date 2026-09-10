# Before

The root package contained the HH synchronization read records, the
`HHAIResponderReadClient` façade, HH request construction, and the read-side
chat/vacancy/negotiation decoding helpers. `HHRequester` was a mixed requester
used by both the old responder surface and the write gateway; its read
scheduler enforced the configured request interval and bounded read
concurrency.

The read capabilities used by synchronization were:

- vacancy search: `GET /search/vacancy`, with configured search parameters and
  sequential `page` pagination; missing card descriptions trigger
  `GET /vacancy/{id}?hhtmFrom=negotiation_list`;
- negotiations: `GET /applicant/negotiations?page={page}`;
- conversation list: `GET /chatik/api/chats` with the fixed filters and an
  optional `from` cursor;
- conversation detail: `GET /chatik/api/chat_data?chatId={id}&applicantId={user}&do_not_track_session_events=true`.

The root write surface remains separate and includes application submission,
chat send/leave, test submission, resume touch, and job-search status changes.
No write endpoint was moved into the read adapter.

Other existing read-only responder capabilities audited but intentionally left
outside the sync-facing R9.1 adapter are profile bootstrap
(`GET /applicant/my_resumes`), resume facts (`GET /resume/{hash}`), vacancy
preflight (`GET /applicant/vacancy_response?...`), and vacancy-test metadata
readback (`GET /applicant/vacancy_response?...` with embedded
`vacancyTests`). They remain single-owner root compatibility reads; extracting
their candidate/startup/write-preflight-specific value models is a separate
follow-up and is not part of the R9.2 synchronization boundary.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `HHVacancyRecord`, `HHApplicationRecord`, `HHConversationRecord`, `HHMessageRecord` | `hh_read_sync.go` | `internal/hhread` | moved as neutral value types; root aliases retained |
| `HHReadClient` | root synchronization layer | `ports.HHReadSource` | narrowed port; no write methods |
| HH vacancy search transport | `HHAIResponder` / root search helper | `internal/adapters/hh/read.Client` | moved; search behavior and query encoding retained |
| HH vacancy description transport | `HHAIResponder` | `hhread.Client.ReadVacancyDescription` | moved; typed read method |
| HH negotiation transport and parsing | root read client | `hhread.Client` | moved |
| HH chat list/detail transport and parsing | root read client / responder | `hhread.Client` | moved for sync and legacy `GetChats`/`GetChatData` façade |
| profile bootstrap read | `HHAIResponder.LoadProfileData` | root responder | retained for startup compatibility; not a sync capability |
| resume facts read | `HHAIResponder.GetResumeFacts` | root responder | retained for candidate-specific parsing; not a sync capability |
| vacancy preflight read | `HHAIResponder.GetVacancyPreflight` | root responder | retained for write-preflight compatibility; no write method exposed by adapter |
| vacancy-test metadata read | `HHAIResponder.GetVacancyTests` | root responder | retained for test workflow compatibility; no test submission method exposed by adapter |
| response-count knowledge | root/domain decoding | adapter value + `internal/vacancy.Vacancy` decode | preserved: absent/null, zero, and positive remain distinct |
| sync/reconciliation | root `HHReadSyncService` | root | deliberately unchanged; R9.2 |
| candidate, AI, semantic, repositories | root/use cases/adapters | existing owners | unchanged |
| HH writes | root gateway/responder | root | unchanged |

# Read adapter

Package: `internal/adapters/hh/read`.

Type: `hhread.Client`.

Constructor: `NewClient(hhread.Options)`.

Dependencies: typed `*http.Client`, typed base/chat URLs, copied search
parameters, XSRF token value, user ID, request interval, and read concurrency.
The adapter reads no environment/config files, creates no repositories, and
does not persist authentication state.

The root `HHAIResponderReadClient` remains a compatibility façade and lazily
constructs one adapter for its responder. Existing fakes implementing the
root-compatible read methods remain source-compatible through type aliases.

# Read capability surface

| Operation | Method | Endpoint | Return type |
|---|---|---|---|
| `ReadVacancies` | GET | `/search/vacancy` (+ optional vacancy detail GET) | `hhread.VacancyPage` |
| `ReadApplications` | GET | `/applicant/negotiations` | `hhread.ApplicationPage` |
| `ReadConversations` | GET | `/chatik/api/chats` + sequential chat detail GETs | `hhread.ConversationPage` |
| `ReadConversation` | GET | `/chatik/api/chat_data` | `hhread.ConversationRecord` |
| `ReadVacanciesWithSearch` | GET | `/search/vacancy` | `hhread.VacancyPage` |
| `ReadVacancyDescription` | GET | `/vacancy/{id}` | `string` |
| `ReadChatList` | GET | `/chatik/api/chats` | `hhread.ChatListPage` |
| `ReadChatHistory` | GET | `/chatik/api/chat_data` | `hhread.ChatHistory` |

Explicit write capabilities: NONE.

There is no exported `Do`, `Request`, or `Raw` escape hatch. Reflection tests
also assert that exported client methods contain no send/apply/write/answer
capability.

# Protocol values

Raw DTO owner: unexported DTOs in `internal/adapters/hh/read`.

Neutral values: `internal/hhread`; they contain normalized identity, status,
timestamps, message sender/direction, service-event and unavailable-content
markers, metadata, and read-only action/button descriptions.

Domain values: `internal/vacancy.Vacancy` is used as the existing normalized
vacancy value during provider decoding; synchronization still owns local
repository mapping and policy.

Port: `internal/ports/hhread.HHReadSource`, plus the optional targeted
`hhreadport.HHConversationReadSource`. It is isolated from the aggregate
`internal/ports` package so the adapter does not inherit unrelated Candidate,
Application, or Conversation dependencies.

# Authentication

The caller supplies the XSRF token and the HTTP client (including the existing
cookie jar). The adapter adds the same XSRF/request headers used by the prior
read calls and never logs or returns the token. It does not implement refresh,
environment loading, `.env` loading, or token persistence.

# Headers

The existing User-Agent, Accept, Accept-Language, Sec-CH-UA, Sec-Fetch-*,
X-Requested-With (Chatik reads), and X-Xsrftoken behavior is retained. Query
parameters are built with `url.Values`; the adapter does not concatenate search
text or cursor values into an endpoint.

# Rate limiting

Interval: supplied from the existing configuration; production default is
`1200ms` (`internal/config.DefaultRequestInterval`).

Ownership: the new adapter owns its read-only scheduler. The scheduler keeps
the existing bounded read-concurrency default/cap (default 4, cap 8), waits
between request starts, removes canceled waiters, and passes caller context
through the wait and HTTP call. Requests are not parallelized beyond the
existing configured read concurrency and pagination remains sequential. The
root compatibility façade retains the existing metadata-reuse decision before
asking the adapter for a detail read; that is sync/read policy, not transport.

429: GET retries retain the existing maximum of three attempts and
`Retry-After` seconds/date handling. A canceled context interrupts the retry
wait. Non-GET operations are not exposed by this adapter.

Cancellation: a canceled context before or during throttle wait prevents the
HTTP request. Tests use zero/short intervals and do not wait for production
1.2-second intervals.

# Pagination

Vacancies: caller cursor is the numeric page; empty/non-empty behavior and
sequential page progression are retained. Multi-profile search continues to
control pages in root orchestration.

Negotiations: numeric `page` query and observed HH `paging.next` page are
retained. Explicit normalized-list compatibility uses the prior non-recursive
next-page behavior.

Conversations/messages: `from` cursor is retained for Chatik list reads;
detail reads are sequential in the existing bounded scheduler. The root
compatibility façade preserves the prior metadata-reuse counters; no prefetch
was added.

Owner after R9.1: transport and protocol pagination are the adapter; sync
completion/reconciliation and local repository writes remain root for R9.2.

# Vacancy mapping

The adapter preserves HH ID/external identity, title/name, description,
requirements, skills, salary/currency, area/location, work format, schedule,
experience, employment, employer/company, links, publication/update times,
archive/test/response-letter flags, response URL, metadata, and total response
count knowledge. The root `mapHHVacancy` now carries count-known, archive,
test, response-letter, and response URL fields into the domain value.

No candidate matching, salary policy, AI evaluation, or application
recommendation is performed by the adapter.

# Negotiation mapping

Observed negotiation identity, vacancy relation, company/title, raw HH status,
creation/update timestamps, conversation ID, and warning/delivery metadata are
normalized into `hhread.ApplicationRecord`. Unknown status remains raw and
unknown; local application status transitions and reconciliation stay in the
root sync service.

# Conversation/message mapping

Conversation identity, vacancy/topic relation, raw status, timestamps, message
IDs, sender classification, direction, text, content-unavailable markers,
system/service events, metadata, and text-button descriptions are retained.
Empty human content is not silently discarded. No AI interpretation,
candidate claim, reply-required classification, or button execution occurs in
the adapter.

# Error semantics

401/403/404/400/409: returned as a typed `HTTPStatusError` with status and
standard text; response bodies are not copied into errors.

429: existing read retry behavior and `Retry-After` handling are preserved.

5xx: returned without a new retry policy beyond the existing read 429 path.

Malformed: JSON and identity/required-container failures return an error and no
partially accepted page.

Network: returned from the supplied `http.Client` and remains context-aware.

The adapter never includes authenticated headers, tokens, message bodies, or
large private response payloads in errors.

# Read / write separation

Read adapter can apply: NO.

Read adapter can send message: NO.

Read adapter can answer HH test: NO.

Read adapter can change negotiation state: NO.

Generic raw request escape hatch: NO.

The root write gateway and write transport were not changed by this stage.
Existing root write methods continue to use their existing safety, dry-run,
approval, nonce, and delivery behavior.

# Root compatibility

`HHReadClient` remains available as a root alias to `ports.HHReadSource`.
`HHAIResponderReadClient` remains available and delegates its public read
capabilities to `hhread.Client`. Root raw chat DTOs remain only as compatibility
types for the legacy auto-chat orchestration; the new adapter does not expose
them through the synchronization port.

Legacy profile/resume metadata, vacancy preflight, and vacancy-test readbacks
remain on the broader responder surface because their existing parsing and
composition are coupled to startup/write-preflight compatibility. They were
not pulled into a speculative second adapter in R9.1. Their extraction is an
explicit follow-up boundary item; they do not become capabilities of the
sync-facing `ports.HHReadSource`.

# HHAIResponder remaining responsibilities

The root responder still owns configuration composition, profile/resume
startup, AI, matching, application orchestration, preflight policy, legacy
presentation DTOs, write compatibility, and dashboard/CLI composition. R9.1
only routes synchronization/search/chat read transport through the new adapter.

# HH sync

Owner: ROOT / DEFERRED R9.2.

Behavior: UNCHANGED. Repository writes, reconciliation, local state, status
policy, candidate context, and AI analysis remain outside the adapter.

# HH Write

Owner: ROOT.

Material changes: NONE in the R9.1 extraction. The pre-existing worktree
changes in `hh_write_gateway.go` and `hh_write_transport.go` were preserved and
not broadened.

LIVE HH WRITES: 0.

# Tests

Transport: new `httptest.Server` coverage for method/path/query construction,
headers, authentication-header presence, status errors, malformed payloads,
network-safe failure, and cancellation.

Mapping: vacancy, negotiation, conversation, service-message,
content-unavailable, action/button, and total-response-count absent/zero/
positive fixtures.

Rate limiting: interval scheduling, 429 cancellation, and canceled-before-HTTP
regression coverage; production interval remains configuration-owned at
1200ms.

Root sync regressions: existing vacancy, application, conversation,
incremental, targeted, reconciliation, CLI, and run-once tests pass.

Write safety: existing dry-run, preflight, approval, chat, salary/relocation,
terminal-state, and write-gateway tests pass; no write request is made by the
new adapter.

# Size

Root HH read transport/normalization before: approximately 400 lines across
`hh_read_sync.go`, `hh_read_validation.go`, `hh_read_performance.go`, and the
responder search/chat methods.

Root compatibility after: approximately 1000 lines remain in
`hh_read_sync.go` plus legacy responder DTOs; the synchronization facade is
delegation-only for its adapter-backed operations.

Read adapter LOC: 1629 implementation/test lines across the cohesive client,
mapping, error, and server-rendered vacancy search files.

# Dependencies

Conceptual graph:

```text
internal/hhread / domain vacancy values
            ↑
internal/ports/hhread.HHReadSource
            ↑
internal/adapters/hh/read.Client
            ↓
          net/http

root HHReadSyncService → HHReadClient compatibility port → read.Client
root write gateway     → existing root write transport
```

`go list -deps ./internal/adapters/hh/read` confirms no root package,
repository implementation, Candidate, AI, or HH write gateway dependency; the
read port is isolated in `internal/ports/hhread` rather than the aggregate
ports package.

# Behavior

HH Read: UNCHANGED for the adapter-backed sync/search/chat operations.

HH Sync: UNCHANGED / ROOT.

HH Write: UNCHANGED.

Other domains: UNCHANGED.

# Verification

gofmt: PASS

Focused adapter tests: PASS.

`go test ./...`: PASS.

`go test -race ./...`: PASS.

`go vet ./...`: PASS.

`go build ./...`: PASS.

`git diff --check`: PASS.

`node --check web/app.js`: PASS.

Docker build: SKIPPED because the Docker daemon was unavailable on this host.

LIVE HH WRITES: 0.

# Ready for R9.2

R9.2 — HH Read Synchronization Use Case Boundary

READY for the extracted sync-facing read source. The strict “all historical
responder GETs have moved” criterion is intentionally deferred for the legacy
profile/resume/preflight/test-read surface documented above; R9.2 must not move
those reads into synchronization policy or repositories. No read adapter
mutation capability is exposed, and the current HH write gateway remains
untouched.
