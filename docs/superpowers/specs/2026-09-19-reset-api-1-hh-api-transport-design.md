# RESET-API-1 — HH OAuth/API transport design

Date: 2026-09-19
Starting SHA: `a85d144bd27fda297163b273a6b8256d8e499236`
`origin/main` SHA at design time: `a85d144bd27fda297163b273a6b8256d8e499236`

## Status and scope

This document is the approved design for RESET-API-1. It is intentionally
limited to a first-class, read-only HH API transport and its OAuth lifecycle.
It does not migrate application, chat, resume, job-search-status, or other HH
mutations.

The existing browser/web transport remains available and remains the fallback.
The API transport must be selected behind the existing read/use-case boundary;
career-agent, Search Planner, RESET-6 routing, Stage29.6 precedence, AI
thresholds/prompts, and RESET-8A requirement semantics must not learn which
transport supplied a record.

All implementation and validation runs for this stage use:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
```

No real HH write is permitted by this stage.

## Repository findings

The current repository is already modular around the read path:

```text
career-agent / runtime / read-sync use cases
              |
              v
internal/ports/hhread.HHReadSource
              |
       +------+------+
       |             |
BrowserHHReader   APIHHClient (new)
       |             |
       +------> internal/hhread normalized values
```

The exact existing read port is in `internal/ports/hhread/hh_read.go`:

```go
type HHReadSource interface {
    ReadVacancies(context.Context, string) (hhread.VacancyPage, error)
    ReadApplications(context.Context, string) (hhread.ApplicationPage, error)
    ReadConversations(context.Context, string) (hhread.ConversationPage, error)
}

type VacancyDetailSource interface {
    ReadVacancyDetail(context.Context, int) (hhread.VacancyRecord, error)
}

type BoundedConversationReadSource interface {
    ReadConversationsBounded(context.Context, string, int) (hhread.ConversationPage, error)
}

type HHConversationReadSource interface {
    HHReadSource
    ReadConversation(context.Context, string) (hhread.ConversationRecord, error)
}
```

`internal/hhread` is the provider-neutral normalized model consumed by read
sync and downstream policy. API JSON types must not be added there.

The current browser implementation is `internal/adapters/hh/read/browser.go`:

- `BrowserHHReader` reads rendered HH pages through `BrowserPageSource`.
- `BrowserHHClient` is the existing public transport-facing alias and must
  remain intact.
- `internal/adapters/hh/read/client.go` is the existing GET-only HTML/JSON
  compatibility reader used by the current responder read façade.
- `internal/runtime/hh_read_sync.go` exposes the compatibility alias
  `HHReadClient = hhreadports.HHReadSource` and adapts the responder's current
  read helpers without exposing write methods.

The write boundary is separate:

- `internal/ports/hhwrite/hh_write.go` contains mutation interfaces.
- `internal/adapters/hh/write/client.go` is the existing browser/web write
  adapter.
- `internal/runtime/hh_write_gateway*.go` owns approval, freshness, nonce,
  dry-run, write-enabled, and reconciliation gates.

RESET-API-1 will not make the API client implement any `hhwrite` interface and
will not route any existing write gateway operation to the API.

## Goals

1. Add explicit `HH_TRANSPORT=browser|api|auto` selection while preserving the
   existing browser behavior as the default.
2. Add operator-configured HH OAuth authorization-code authentication.
3. Store access/refresh tokens locally with private permissions and atomic
   replacement, without mixing them with `cookies.txt`.
4. Add a JSON API client with context-aware requests, HH User-Agent, Bearer
   authentication, safe refresh behavior, bounded 429 handling, and typed
   errors.
5. Normalize read-only API data into the existing `internal/hhread` records.
6. Provide `hh-api auth`, `hh-api doctor`, and `hh-api logout` commands that
   never print token values or authorization codes.
7. Establish a bounded browser/API shadow-parity procedure without claiming
   parity where query or response semantics differ.

## Non-goals

RESET-API-1 does not:

- copy client credentials from the historical gist or any HH Android app;
- add credentials or tokens to source, `.env.example`, test fixtures, reports,
  logs, or errors;
- replace or weaken `BrowserHHClient`;
- change Search Planner semantics or query generation;
- change RESET-6 router scoring or outcomes;
- change Stage29.6 precedence;
- change the AI threshold, prompts, or RESET-8A requirement semantics;
- submit applications or tests;
- send employer chat messages;
- hide/archive/reconcile negotiations through an API write;
- mutate, raise, touch, or update resumes;
- change job-search status;
- add browser fallback when `HH_TRANSPORT=api` is explicitly selected;
- silently convert unknown API state into a safe affirmative state.

## HH API reference facts

The current HH API documentation identifies:

- authorization URL: `https://hh.ru/oauth/authorize`;
- token and refresh URL: `https://api.hh.ru/token`;
- current-user read: `GET https://api.hh.ru/me`;
- own resumes read: `GET https://api.hh.ru/resumes/mine`;
- vacancy search: `GET https://api.hh.ru/vacancies`;
- vacancy detail: `GET https://api.hh.ru/vacancies/{vacancy_id}`;
- negotiations resources: `GET https://api.hh.ru/negotiations` and related
  collection/detail/message URLs.

The implementation must use operator-supplied endpoint overrides, so the
defaults are convenience values rather than a secret or an assumption that
all HH regional hosts have identical behavior. The official reference is
https://api.hh.ru/openapi/redoc.

## Configuration

Extend `internal/config.Config` and its existing defaults/env/flag loader with
these fields:

| Config field | Environment variable | Default | Secret? |
| --- | --- | --- | --- |
| `HHTransport` | `HH_TRANSPORT` | `browser` | No |
| `HHAPIBaseURL` | `HH_API_BASE_URL` | `https://api.hh.ru` | No |
| `HHOAuthAuthorizeURL` | `HH_OAUTH_AUTHORIZE_URL` | `https://hh.ru/oauth/authorize` | No |
| `HHOAuthTokenURL` | `HH_OAUTH_TOKEN_URL` | `https://api.hh.ru/token` | No |
| `HHOAuthClientID` | `HH_OAUTH_CLIENT_ID` | empty | Yes/config credential |
| `HHOAuthClientSecret` | `HH_OAUTH_CLIENT_SECRET` | empty | Yes |
| `HHOAuthRedirectURI` | `HH_OAUTH_REDIRECT_URI` | empty | No, but local auth config |
| `HHAPITokenFile` | `HH_API_TOKEN_FILE` | `.hh-api-token.json` | Contains secrets |
| `HHOAuthUserAgent` | `HH_OAUTH_USER_AGENT` | empty | No, required operator metadata |

The existing CLI-over-environment precedence remains authoritative. Empty
client credentials must remain empty and fail only when an API auth operation
requires them. No third-party application ID, secret, token, or device
identity is a default.

`example.env` documents names and safe placeholders only. `.gitignore` must
ignore `.hh-api-token.json` and common operator-supplied token-file variants
without broadening the ignore rule to unrelated source files.

`HH_TRANSPORT=browser` is the compatibility default. `HH_TRANSPORT=api` is
explicit and fail-closed. `HH_TRANSPORT=auto` prefers API only after a
read-only `/me` probe succeeds; otherwise it may use the existing browser
doctor/session path and must record a safe fallback reason.

## OAuth and token lifecycle

### Token model

The API adapter owns a minimal private token model, not `internal/hhread`:

```go
type OAuthTokens struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    TokenType    string    `json:"token_type"`
    ExpiresAt    time.Time `json:"expires_at"`
}
```

The model is never included in a normal report, event, error, or diagnostic.
Expiry checks use a small safety skew so a request does not begin with a token
that will expire immediately. Missing access tokens are `AUTH_REQUIRED`.

### Token store

The token store is a small interface owned by the OAuth/API adapter or a
narrow `internal/ports` package if composition needs to inject it:

```go
type TokenStore interface {
    Load(context.Context) (OAuthTokens, error)
    Save(context.Context, OAuthTokens) error
    Delete(context.Context) error
}
```

The file implementation must:

- create parent directories privately where needed;
- write through the existing `platform.WritePrivateFileAtomic` primitive or an
  equivalent same-directory atomic replacement;
- produce mode `0600` on Unix where supported;
- validate the JSON shape before use;
- preserve a newly returned refresh token when refresh rotates it;
- preserve the prior refresh token only when the provider omitted a replacement
  and the response is otherwise valid;
- never log serialized token contents;
- treat a failed refresh as terminal for this request and require explicit
  reauthorization rather than looping;
- delete only the configured API token file for `logout`.

Cookies remain owned by the existing browser session configuration and are not
read by the API token store.

### Authorization-code flow

`hh-api auth` performs this sequence:

1. Validate that client ID, client secret, redirect URI, and user-agent
   configuration are operator-supplied and non-empty.
2. Generate a cryptographically random state value.
3. Build the authorization URL with `response_type=code`, client ID, redirect
   URI, and state. The authorization code itself is never written to logs.
4. Prefer a configured localhost callback when the registered redirect URI
   permits it; otherwise print the URL and accept the configured redirect
   mechanism without pretending that an unconfigured callback is safe.
5. Verify returned state before exchanging the code. Reject missing,
   mismatched, or provider-error callbacks.
6. POST a form-encoded authorization-code exchange to the configured token
   URL using the operator credentials and redirect URI.
7. Parse `access_token`, `refresh_token`, `token_type`, and `expires_in`, then
   compute `ExpiresAt` locally and store the result privately.
8. Call `GET /me` using the newly stored token.
9. Print only safe metadata, for example:

   ```text
   HH API OAuth
   Authorized: YES
   User type: applicant
   Token stored: YES
   Access token logged: NO
   Refresh token logged: NO
   ```

No device identity spoofing, fingerprint spoofing, CAPTCHA bypass, stealth,
APK impersonation, or browser-cookie extraction is part of this flow.

### Refresh behavior

The API client performs at most one refresh attempt for a request when the
access token is expired locally or the API returns an authentication response
that is safe to interpret as token expiry. It saves the complete replacement
pair atomically before retrying the original read once. It never retries an
operation indefinitely. A revoked-token response becomes `TOKEN_REVOKED` and
requires explicit `hh-api auth` again.

## API client

Create a separate adapter package under `internal/adapters/hh/api` with
provider-specific wire structures and no dependencies on runtime use cases.
The central type is `APIHHClient`, which implements the existing read port:

```go
var _ hhreadport.HHReadSource = (*APIHHClient)(nil)
var _ hhreadport.VacancyDetailSource = (*APIHHClient)(nil)
```

The client receives all dependencies through typed options: parsed base/token
URLs, an `http.Client`, a token store or token provider, user-agent, timeout,
clock, and read-only search configuration. It does not read process
environment directly and it does not construct a write adapter.

Every API request:

- uses the caller's `context.Context` and a bounded client timeout;
- sends `Authorization: Bearer <access token>` and the required HH User-Agent;
- sends JSON-compatible Accept headers;
- limits diagnostic response data to safe status/content metadata;
- never includes an authorization header or token in an error string;
- maps HTTP status and safe provider error fields to a typed API error.

The safe error taxonomy is:

```text
AUTH_REQUIRED
TOKEN_EXPIRED
TOKEN_REVOKED
APPLICATION_NOT_FOUND
RATE_LIMITED
FORBIDDEN
REMOTE_ERROR
```

`401` maps to `TOKEN_EXPIRED` only when the provider evidence supports expiry;
otherwise it maps to `AUTH_REQUIRED`. A provider error indicating revocation
maps to `TOKEN_REVOKED`. `403` maps to `FORBIDDEN` unless the endpoint/error
shape specifically identifies an application or authorization problem.
`404` maps to `APPLICATION_NOT_FOUND` only for a missing requested resource;
other endpoint errors remain `REMOTE_ERROR`. `429` maps to `RATE_LIMITED` and
may honor a bounded `Retry-After`; no write-shaped retry exists.

## Read-only endpoint and domain normalization

API wire responses are decoded into private adapter types and normalized into
the same `internal/hhread` structures that the browser reader produces. The
normalizer must preserve “unknown” where a field is absent or semantically
different; it must not fabricate browser-only values.

| Domain operation | Current/API endpoint | RESET-API-1 status | Normalized output / safety note |
| --- | --- | --- | --- |
| Authenticated current user | `GET /me` | `API_SUPPORTED` | Safe user type/auth metadata for doctor and transport selection; private profile fields are not reported. |
| Own resumes | `GET /resumes/mine` | `API_SUPPORTED` | API resume summaries mapped to a dedicated read model used by resume selection; no resume mutation. |
| Vacancy search | `GET /vacancies` | `API_SUPPORTED` | Items map to `hhread.VacancyRecord`; API query translation is explicit and logged as semantic metadata only. |
| Vacancy detail | `GET /vacancies/{id}` | `API_SUPPORTED` | Description, salary, area, schedule, employment, work format, experience, skills, roles, archive and response-related fields map only when present. |
| Already-responded relation | API relation fields / resume negotiation history | `UNKNOWN_REQUIRES_PROBE` | Do not infer `already responded` from a missing or differently named API field. A failed or ambiguous relation read blocks automatic equivalence. |
| Suitable resumes | API resume list plus vacancy suitability behavior | `API_NOT_YET_IMPLEMENTED` | Listing own resumes is supported; determining the same suitable-resume set as the browser flow is not yet proven. |
| Negotiations list | `GET /negotiations` and collection URLs | `UNKNOWN_REQUIRES_PROBE` | API documentation exposes employer-oriented collection concepts and applicant behavior must be verified before import into application state. |
| Negotiation detail | `GET /negotiations/{id}` | `UNKNOWN_REQUIRES_PROBE` | Endpoint exists, but applicant visibility/status semantics must be verified with safe fixtures and real read-only doctor evidence. |
| Messages | `GET /negotiations/{id}/messages` | `API_NOT_YET_IMPLEMENTED` | The documented endpoint is deprecated for newer chat features; do not claim parity with Chatik conversation history. |

The initial API client may implement the `ReadApplications` method only when
its response can be normalized without guessing. Until the negotiations probe
is complete, it must return a typed capability error rather than silently use a
browser reader or report an empty application set.

The following fields are explicitly compared during shadow validation:

- external vacancy ID and URL;
- title and company;
- description availability;
- professional roles;
- work experience;
- schedule and employment;
- work format and location/area;
- salary and currency;
- archive/availability state;
- response-letter and test indicators when the provider supplies them;
- already-responded state only when its evidence is authoritative.

## Transport selection and bootstrap wiring

The composition root remains responsible for constructing concrete adapters.
The selection layer returns the existing `hhreadport.HHReadSource` capability to
runtime/read-sync code. It must not add a transport parameter to search,
matching, routing, or AI use-case APIs.

Planned wiring:

```text
internal/config.Config
        |
        v
internal/runtime/bootstrap selection
        |
        +-- browser session -> BrowserHHClient/current read façade
        |
        +-- API config + TokenStore -> APIHHClient
        |
        +-- auto: API /me probe, then browser doctor fallback
        v
hhreadport.HHReadSource
        |
        v
HHReadSyncService / career-agent / preflight readers
```

`HH_TRANSPORT=browser` preserves current construction and browser doctor
behavior. `HH_TRANSPORT=api` requires a readable token file and successful
read-only authentication; it fails with a safe classification when unavailable
and never falls back. `HH_TRANSPORT=auto` tries API `/me` once, then uses the
existing browser session only if the browser doctor reports `AUTH_OK`.

Safe telemetry may contain:

```text
transport_selected=API|BROWSER
transport_fallback_reason=<bounded classification>
api_auth_status=AUTH_OK|AUTH_REQUIRED|TOKEN_EXPIRED|TOKEN_REVOKED|FORBIDDEN|...
```

It must not contain URLs with authorization parameters, token values, client
secrets, cookies, or authenticated response bodies.

`hh-api` is a top-level command handler wired through `internal/runtime/handlers.go`
and the existing bootstrap/CLI invocation graph. It must not be hidden inside
the career-agent search command, because auth/doctor/logout are explicit
operator operations.

## Doctor behavior

`./hh-ai-responder hh-api doctor` is read-only and validates only:

1. token-file presence and JSON shape, without printing token material;
2. local expiry state;
3. `GET /me` and applicant user type;
4. own resume read;
5. one bounded vacancy read using an operator-supplied safe probe ID or the
   documented search endpoint, without applying or touching the result.

Expected safe output is equivalent to:

```text
HH API Doctor

Transport: API
Token file: present
Token expired: no
/me: AUTH_OK
Applicant resumes: OK
Vacancy read: OK

Overall: AUTH_OK
```

It must not perform application, message, negotiation mutation, resume, or
job-search-status requests.

## Complete operation capability matrix

The matrix distinguishes the API capability from what RESET-API-1 actually
enables. “API_SUPPORTED” means the official API documents a read/write surface
that is a candidate for implementation; it does not authorize migration. Any
operation not proven against the current normalized model remains probe- or
not-yet-implemented.

| Current HH operation | API capability in this stage | Existing authoritative path | RESET-API-1 action |
| --- | --- | --- | --- |
| Authenticated current user | `API_SUPPORTED` | Browser doctor/session | Implement API `/me` read |
| Own resumes | `API_SUPPORTED` | Browser resume read | Implement read-only API normalization |
| Vacancy search | `API_SUPPORTED` | Browser/HTML search | Implement API read with explicit query mapping |
| Vacancy detail | `API_SUPPORTED` | Browser vacancy detail | Implement API read and normalization |
| Already-responded relation | `UNKNOWN_REQUIRES_PROBE` | Browser preflight/reconciliation reads | Probe only; unknown blocks parity claims |
| Suitable resumes | `API_NOT_YET_IMPLEMENTED` | Browser resume suitability/selection | Keep browser path for this capability |
| Negotiations list | `UNKNOWN_REQUIRES_PROBE` | Browser negotiations/read synchronization | Probe applicant semantics before enabling import |
| Negotiation detail | `UNKNOWN_REQUIRES_PROBE` | Browser targeted negotiation read | Probe; no automatic substitution |
| Messages/employer conversation history | `API_NOT_YET_IMPLEMENTED` | Existing Chatik/browser read model | Keep browser-only; API endpoint is deprecated for new chat features |
| Application/respond to vacancy | `API_NOT_YET_IMPLEMENTED` | Existing `hhwrite.VacancyResponseWriter` + `HHWriteGateway` | Keep browser-only; API client has no write method |
| Cover letter/message with application | `API_NOT_YET_IMPLEMENTED` | Existing application write path | Keep browser-only; no API migration |
| Employer chat message | `API_NOT_YET_IMPLEMENTED` | Existing `hhwrite.ChatMessageWriter` + gateway | Keep browser-only; no API migration |
| Hide/reconcile negotiation | `BROWSER_ONLY` for current workflow | Existing browser write/reconciliation path | No API write or fallback change |
| Resume mutation/raise/update/touch | `BROWSER_ONLY` for current workflow | Existing `hhwrite.ResumeWriter` and related paths | No API write |
| Job-search status mutation | `BROWSER_ONLY` for current workflow | Existing `hhwrite.JobSearchStatusWriter` | No API write |
| Test/questionnaire submission | `API_NOT_YET_IMPLEMENTED` | Existing browser application request | No API write; validation remains unchanged |

No write operation may be relabeled `API_SUPPORTED` merely because an HTTP
endpoint appears in external documentation. A future write stage must first
specify request semantics, fresh preflight, idempotency/reconciliation, and
operator controls independently.

## Protected write paths and zero-write audit

The following paths remain protected and unchanged:

- `internal/ports/hhwrite` mutation interfaces;
- `internal/adapters/hh/write` browser/web transport;
- `internal/runtime/hh_write_gateway.go` and split gateway files;
- application response/test submission flow;
- employer chat send/leave flow;
- resume touch and job-search-status flow;
- application reconciliation and negotiation state changes;
- dashboard action dispatch through `HHWriteGateway`;
- dry-run and write-enabled gates in runtime composition/configuration.

The API adapter must expose only read methods. If a future interface needs a
placeholder for a write capability, it must return an explicit
`WRITE_DISABLED`/`NOT_IMPLEMENTED` error without constructing or sending an
HTTP request. Tests must assert the API shadow path emits zero non-GET HH
requests.

## Testing strategy

All tests use local fakes or `httptest.Server`; no live HH API call and no real
cookie/token is required.

### OAuth and storage tests

- authorization URL contains the expected encoded parameters and no secret;
- state generation is non-empty and state mismatch is rejected;
- provider error callback is rejected without token exchange;
- token exchange parses expiry and token type without exposing values;
- refresh rotates the refresh token and preserves it only when omitted by a
  valid provider response;
- expired tokens are detected with the configured clock/skew;
- atomic token storage leaves no temporary files and is `0600` on Unix;
- logout deletes only the configured token file;
- errors and logs do not contain access tokens, refresh tokens, client secrets,
  authorization codes, or cookies.

### API adapter tests

- Bearer and HH User-Agent headers are present;
- `/me` applicant response decodes to safe auth metadata;
- own resumes decode to the planned read model;
- vacancy search query/response maps to `hhread.VacancyPage`;
- vacancy detail maps to `hhread.VacancyRecord` without invented fields;
- relations/got-response unknown state remains unknown;
- negotiation and message capability errors are explicit;
- 401/403/404/429 and representative provider errors map to the taxonomy;
- only one safe refresh/retry occurs;
- request context cancellation stops work;
- API client has no write-shaped method and shadow fakes record zero writes.

### Configuration/selection/CLI tests

- browser remains the default;
- invalid transport values fail validation;
- CLI values override environment values;
- explicit API selection fails clearly when token/auth is unavailable;
- auto selects API after `/me` success;
- auto falls back only after API failure and browser `AUTH_OK`;
- auto reports safe fallback metadata without secrets;
- `hh-api doctor` performs reads only;
- `hh-api logout` performs local token deletion only.

### Shadow parity procedure

Run a bounded search with both transports under identical planner and safety
settings:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_SEARCH_PAGES_PER_PROFILE=3
HH_MAX_SEARCH_PAGES_PER_RUN=48
HH_MAX_VACANCIES_PER_RUN=100
STORAGE_BACKEND=json
```

Compare generated planner profiles, query intent, raw discoveries, distinct
vacancy IDs, normalized detail fields, professional roles, experience,
schedule, employment, work format, location, salary, response state, router
outcomes, and AI-evaluated rows. Record differences by cause:

- query parameter unavailable or differently named;
- API response field absent or differently defined;
- pagination/order/rate-limit difference;
- authenticated versus public visibility difference;
- browser parser limitation;
- unresolved applicant relation semantics.

The parity report must use “equal”, “different with explanation”, or
“unknown/unproven”; it must not convert missing API data into a browser value.

## Secrets and data-handling requirements

Never commit or log:

- client secret;
- access token;
- refresh token;
- authorization code;
- cookies or browser profiles;
- authenticated raw API response bodies containing private data.

Token files and test fixtures containing token-like values stay outside tracked
files or use clearly synthetic sentinel values that are asserted absent from
reports. Normal errors contain only bounded status/classification metadata.
Reports contain no token model and no private `/me` response.

## Definition of done for implementation

RESET-API-1 is complete only when:

- the spec and implementation plan are followed;
- browser transport behavior is preserved;
- API OAuth/token lifecycle and read adapter are implemented and unit-tested;
- config/CLI selection is explicit and validated;
- API writes are absent/disabled;
- no write request occurs in API shadow mode;
- `gofmt`, `go test ./...`, `go test -race ./...`, `go vet ./...`,
  `go build ./...`, and `git diff --check` pass;
- the validation report records starting/final/origin SHAs, endpoints proven,
  unavailable operations, parity findings, zero-write audit, and secrets audit;
- no unrelated working-tree files are modified.

The implementation must not be described as ready to become the default for
reads until applicant duplicate-state, negotiations, and the bounded shadow
parity procedure are proven. The next stage after this one is a separate,
operator-reviewed read reconciliation/duplicate-state probe, followed by a
separate design for any application/chat write migration.
