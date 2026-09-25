# Cookie/Web Application Transport Design

Date: 2026-09-25

Status: approved conversational design; implementation pending written-spec review

## Goal

Add an authenticated HH web/cookie transport for the controlled vacancy-application flow. A valid Netscape `cookies.txt` session must be sufficient for GET-only authentication, application preflight, and the dry-run application path. OAuth API transport remains available as an optional independent transport and is not required by the browser-cookie path.

The implementation is a clean-room behavioral implementation of the transport concept described in `s3rgeym/hh-ai-responder@2fe3dc0fd2b5f55e4944e1a6e23966dd1068d400`. It will not copy upstream source code or autonomous-write behavior.

## Constraints and invariants

The current controlled application safety model remains authoritative:

- preparation, preparation hash, content hash, resume identity, approval, and one-time nonce remain exact bindings;
- a fresh GET-only preflight runs immediately before a possible transport attempt;
- `HHWriteGateway` remains the sole HH mutation boundary;
- `HH_DRY_RUN=true` produces zero HH writes, zero nonce consumption, and zero application attempts;
- per-run and per-day write limits remain enforced by the shared gateway/audit;
- batch processing is sequential, reconciles each attempted item before continuing, and stops on uncertain delivery or reconciliation failure;
- PostgreSQL remains canonical where configured;
- unknown critical state produces a block/review result and never a blind write or retry;
- daily and scheduler flows remain read-only;
- no production HH POST, message, resume mutation, or job-search status mutation is executed by this phase.

The existing API transport and OAuth authentication are preserved. The existing CLI spelling `hh-api apply` and related operator workflows remain compatible; a broad CLI rename is out of scope.

## Upstream comparison

The upstream reference uses a browser-authenticated session exported to Netscape cookies, a persistent cookie jar, ordinary HH web requests, an XSRF cookie, and the web vacancy-response endpoint. It does not require OAuth, PKCE, access tokens, or an OAuth callback for that transport.

The previous project implementation had two separate pieces: Playwright loaded `cookies.txt` into a browser for read-oriented checks, while a runtime-local `MemoryPersistentJar` was used by legacy HTTP paths and a generic write adapter received a separately captured XSRF token. The runtime jar had permissive parsing and ignored persistence errors, and the application command was API-specific.

The new implementation introduces one explicit `CookieWebSession` owned by the web transport. It loads and validates the cookie file, supplies cookies through a persistent `http.CookieJar`, persists legitimate `Set-Cookie` updates atomically, and exposes only safe cookie metadata. The web application writer reads XSRF from that same session and is composed behind the existing typed write port and gateway.

## Architecture

### Components

1. `internal/browsersession` owns the cookie-file/session primitive.
   
   It will provide strict Netscape parsing, HH-domain filtering/validation, cookie matching through standard jar semantics, concurrent updates, safe metadata, and atomic `0600` persistence. A persistence failure must not replace or delete the last valid cookie file. The session must make persistence errors observable to write orchestration.

2. `internal/adapters/hh/read` remains the read adapter and receives an HTTP client backed by `CookieWebSession`. Existing HTML/state parsers are reused where their evidence is authoritative. New resume-page mapping helpers are added at the narrow read boundary rather than inferred from titles.

3. A dedicated cookie-web application adapter implements `hhwrite.VacancyResponseWriter`, named `CookieWebVacancyResponseWriter`.

   It accepts only a typed, already-approved application request. It supports the standard vacancy response only; a non-nil test submission is rejected before transport. It sends exactly one form POST to `/applicant/vacancy_response/popup`, with cookie-jar authentication and a session-derived XSRF token. It does not implement chat, leave, resume touch, or job-search status mutations.

4. The controlled application orchestration is made transport-neutral behind a small composition interface. The existing API implementation becomes one adapter of that interface; the cookie-web implementation supplies the cookie read source, resume mapping, web preflight, writer, and web reconciliation evidence reader. Both routes use the existing application submission, durable attempt, gateway, and reconciliation use cases.

5. `career-agent browser-doctor` becomes a cookie/web GET diagnostic for the configured HH web transport. The separate Playwright browser-session flow remains available for headed user interaction and cookie preparation, but it is not required for ordinary cookie-web reads or dry-run applications.

### Cookie session behavior

The load path is:

```text
cookies.txt
  -> strict Netscape parser
  -> in-memory persistent cookie jar
  -> authenticated GET/POST through CookieWebSession
  -> response Set-Cookie updates
  -> atomic private cookies.txt replacement
```

Rows with malformed identity, expiry, or unsafe domain state fail closed. Only `hh.ru` and subdomains of `hh.ru` are accepted. Domain, path, secure, and expiry semantics are respected. Session cookies are retained with expiry zero. Cookie values, `Cookie` headers, XSRF values, and session identifiers are never placed in reports, logs, or audit evidence.

The persistence operation writes a temporary file in the cookie file's directory with mode `0600`, flushes/closes it, and renames it atomically. A failed update leaves the previous file intact. Concurrent jar reads and updates are serialized; a write path treats an unobservable persistence error as non-success and preserves delivery uncertainty after a transport attempt.

### Browser doctor

The doctor uses the same cookie session as the application transport and performs only GETs:

1. HH home page;
2. `/applicant/my_resumes`;
3. one explicit vacancy URL or a bounded search-page vacancy link;
4. XSRF presence when write capability is being checked.

It classifies login redirects, stale sessions, captcha/challenge pages, malformed responses, and unknown states explicitly. A successful run prints safe cookie names/domains/counts and `Overall: AUTH_OK`. It never suggests OAuth when browser cookies are the configured transport; remediation is to replace or refresh `cookies.txt`.

### Resume identity

The authenticated `/applicant/my_resumes` HTML state is parsed into a mapping containing, where available:

- browser `resume_hash`;
- numeric HH resume ID;
- title;
- existing internal resume identity;
- API/provider ID when already present in trusted local data.

The approved identity is matched by exact stored identity/hash. Titles are descriptive metadata only and cannot select a replacement resume. If the approved browser hash is absent or cannot be matched to the approved logical resume, the flow returns `APPROVED_RESUME_NOT_AVAILABLE_IN_BROWSER_SESSION` and does not proceed.

### Web preflight

The web preflight is a typed, read-only operation for one vacancy and one approved browser resume hash. It uses the vacancy page and the response form page, with negotiations as vacancy-scoped evidence where necessary. It must prove:

- authenticated session;
- active, non-archived vacancy;
- no existing response for the vacancy/resume scope;
- application is currently allowed;
- approved resume is present in the response state/mapping;
- no test is required for this first writer;
- cover-letter requirement is known;
- standard HH response path is available.

The existing parser and evidence model are reused where possible. Structured provider state outranks text inference. An absent marker is not interpreted as a negative fact. Any critical field that remains unknown blocks the write.

### Exact application request

The writer builds only this approved payload:

```text
_xsrf=<session XSRF cookie>
vacancy_id=<approved vacancy ID>
resume_hash=<approved browser resume hash>
letter=<exact approved letter>
ignore_postponed=true
```

The request is an `application/x-www-form-urlencoded` POST to the resolved HH host's `/applicant/vacancy_response/popup`. It sends the configured stable browser user agent, `Accept: application/json`, `X-Requested-With: XMLHttpRequest`, `X-Xsrftoken`, and the exact vacancy `Referer`. Cookies are supplied only by the jar. No test fields are emitted; a test-bearing request is `BLOCKED_PRE_SEND`.

### Response classification

The provider body is bounded and sanitized before it reaches typed errors or audit. HTTP success is not treated as confirmed delivery by itself. The writer maps evidence into the existing port categories:

- `SUCCESS` when the provider response contract is recognized as accepted;
- `ALREADY_APPLIED` when explicit duplicate evidence is returned;
- validation/business/auth/rate-limit rejection for deterministic failures;
- `UNKNOWN_SEND_RESULT` / ambiguous delivery for timeout, connection loss after request start, malformed or unknown provider responses, server errors, challenge responses, or persistence state that cannot be safely verified.

No ambiguous result is retried automatically.

### Reconciliation

Every transport-attempted result that can represent delivery triggers fresh cookie-web reads before the next batch item. Confirmation requires vacancy-scoped provider evidence, such as a matching negotiation/application entry or an explicit vacancy response state. Local attempt existence is never confirmation. Existing application-attempt and reconciliation infrastructure records the canonical result and maps to:

```text
POST_SUCCESS_RECONCILED
ALREADY_APPLIED_RECONCILED
UNKNOWN_SEND_RECONCILED_SUCCESS
UNKNOWN_SEND_UNRESOLVED
```

The web evidence reader is read-only and cannot call the writer.

## Call graphs

### Read

```text
CLI/read workflow
  -> CookieWebSession
  -> hh/read client
  -> authenticated HH GET
  -> existing HTML/state parser
  -> read model / PostgreSQL projection
```

### Dry-run application

```text
apply command
  -> transport-neutral controlled application service
  -> approval/preparation/content validation
  -> browser resume mapping
  -> fresh cookie-web GET preflight
  -> exact request construction preview
  -> HHWriteGateway policy
  -> dry-run block
```

No nonce is consumed, no application attempt is created, and no POST is issued.

### Live controlled application

```text
apply/apply-batch
  -> transport-neutral controlled application service
  -> fresh cookie-web preflight
  -> durable nonce reservation
  -> application submission service
  -> HHWriteGateway
  -> CookieWebVacancyResponseWriter
  -> one HH web POST
  -> fresh cookie-web reconciliation
  -> canonical application attempt/reconciliation state
```

### Reconciliation

```text
transport result
  -> application attempt state
  -> cookie-web vacancy/negotiations GETs
  -> vacancy-scoped evidence reader
  -> existing reconciliation use case
  -> confirmed or unresolved canonical result
```

## Configuration and compatibility

`HH_TRANSPORT=browser` selects the cookie-web application/read composition and requires `cookies.txt`. It must not require an OAuth token file, OAuth client credentials, or redirect URI. `HH_TRANSPORT=api` retains the current API/OAuth behavior. Existing flags and environment variables remain valid; any new configuration is additive, validated, and documented in `README.md` and `example.env`.

The existing `hh-api apply` and `hh-api apply-batch` operator names remain supported during this phase to avoid an unrelated CLI migration. Their internals use a transport-neutral service and select the configured adapter.

## Tests

All provider interaction tests use `httptest` or deterministic fakes. Required coverage includes:

- strict Netscape loading, HH-only domains, domain/subdomain matching, secure and expired cookies;
- `Set-Cookie` replacement/addition, session-cookie persistence, atomic mode `0600`, concurrent access, and no value leakage;
- doctor success, login redirect, stale session, captcha, missing XSRF, and safe remediation;
- exact resume mapping and missing approved resume blocking;
- active/inactive, already-responded, test-required, letter-required, optional-letter, auth, and unknown web preflight states;
- exact vacancy ID, browser resume hash, approved letter, XSRF header/body, referer, and jar cookies in the writer request;
- no test payload support, deterministic response classification, timeout ambiguity, unknown response ambiguity, and no retry;
- dry-run with zero POST, zero nonce consumption, and zero application attempt;
- successful, duplicate, unknown, reconciled, and unresolved outcomes;
- sequential batch limits, duplicate approvals/vacancies, pre-send continuation, uncertainty/reconciliation stop, shared gateway, and one-time nonce concurrency;
- valid cookies with absent/invalid OAuth configuration proving the web dry-run never accesses OAuth.

The full repository gate is:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

No test uses production cookies or performs a real HH mutation.

## Scope exclusions

- no deletion of OAuth code or token refresh;
- no autonomous application behavior;
- no changes to employer chat, leave-chat, resume-touch, or job-search-status transport selection beyond preserving their existing safety behavior;
- no broad CLI rename;
- no production live POST validation;
- no unrelated architecture cleanup.

