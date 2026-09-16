# Stage 29.1 — HH authenticated read diagnosis

Дата: 2026-09-16 (Asia/Yekaterinburg).

Режим всех HH-проверок: `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`.
Applications, chat messages, tests, resume touch и job-search-status writes не
выполнялись.

## Итог

- 403 endpoint: `GET https://perm.hh.ru/applicant/my_resumes`.
- 403 classification: `AUTH_SESSION_EXPIRED`.
- Root cause: HH не принимает текущую authenticated session; ответ — HTML
  `ForbiddenPage` с безопасно распознанной ссылкой `/account/login`.
- Cookies не являются доказательством валидной auth-сессии, но доказано, что
  auth-related cookies реально прикрепляются к outgoing request.
- Это не выглядит как Stage 29 HTTP regression: Stage 29 не менял HTTP client,
  cookie loading/jar, headers, base URL, redirects, scheduler или config
  parsing.
- Код изменён для диагностики, безопасной классификации, fail-fast и удаления
  побочного переписывания cookie-файла при GET; 403 не «чинялся» подделкой
  login/session.
- Authenticated HH read now works: **NO**.
- Remaining blocker: требуется легитимно обновить HH authenticated
  session/cookies интерактивно. CAPTCHA/anti-bot обход не реализован.

## Exact request evidence

`hh-doctor` использует тот же configured base host, что и приложение: первый URL
из текущего `HH_SEARCH_URLS` разрешает `perm.hh.ru`.

| Probe | Method | Endpoint | Redirect chain | Final URL | Status | Content-Type | Response size |
|---|---|---|---|---|---:|---|---:|
| public vacancy read | GET | `/search/vacancy` (query keys: `items_on_page,order_by,search_period`) | `200 perm.hh.ru/search/vacancy` | `https://perm.hh.ru/search/vacancy` | 200 | `text/html; charset=utf-8` | 856664 bytes |
| authenticated profile read | GET | `/applicant/my_resumes` | `403 perm.hh.ru/applicant/my_resumes` | `https://perm.hh.ru/applicant/my_resumes` | 403 | `text/html; charset=utf-8` | 416637 bytes |

Safe final response headers included `Server=ddos-guard`, `Date`,
`Content-Type`, and `X-Request-ID`; no secret headers were logged. The body was
not saved. Sanitized body signals were `ForbiddenPage` and
`account_login_link`. There were no explicit Cloudflare/CAPTCHA challenge
markers in the authenticated response.

As a separate canonical-host control, `GET https://hh.ru/applicant/my_resumes`
returned `302` to `https://salekhard.hh.ru/applicant/my_resumes`, followed by
the same kind of `403 ForbiddenPage` response. The production-configured
client's first authenticated request is the direct `perm.hh.ru` request above,
so this redirect is not the cause of the observed current failure.

## Cookie/session evidence

The file is Netscape HTTP Cookie format and is readable. The latest doctor run
parsed 41 cookies, all for `hh.ru` or its subdomains, and reported no currently
expired cookies. Auth-related names present were `_xsrf`, `crypted_hhuid`,
`crypted_id`, `hhrole`, `hhtoken`, `hhuid`, and `hhul`. Netscape format does not
carry `httpOnly` or `sameSite`; those attributes are therefore reported as
unavailable rather than guessed.

The authenticated profile request carried 36 cookie names, including `hhtoken`,
`hhuid`, `hhul`, `crypted_hhuid`, `hhrole`, and `_xsrf`. Values were never
printed. The jar's domain matching covers `.hh.ru`, `hh.ru`, `perm.hh.ru`, and
other `*.hh.ru` domains; the relevant cookies use path `/`, matching
`/applicant/my_resumes`.

A fixture regression test covers:

`cookie file → Netscape parser → MemoryPersistentJar → http.Client → outgoing Cookie header`.

The provider still returned 403 after those cookies were attached. Therefore
the evidence supports a server-rejected/expired session, not a parser or
cookie-domain failure. A cookie's local expiry is not treated as proof of
server-side validity.

## Stage 28 → Stage 29 HTTP-layer comparison

The diff `b42c091...e381310...` changes only vacancy detail mapping, Career
Agent routing/accounting, event fields, and related tests. It does not change:

- cookie file loading or cookie jar construction;
- HTTP client/transport creation or redirect handling;
- `User-Agent`, `Accept`, `Accept-Language`, `Sec-CH-UA` or fetch headers;
- base URL resolution or search URL parsing;
- request scheduler, read concurrency, rate limiter or authentication
  initialization;
- dotenv parsing or the `HH_DRY_RUN`/`HH_WRITE_ENABLED` precedence.

The first authenticated request in both stages remains `GET /applicant/my_resumes`
inside `NewHHAIResponder` before search discovery. Stage 29's new vacancy
detail request (`GET /vacancy/<id>`) is never reached in this run.

## Failure classification

### CODE FAILURE

Not supported by evidence. The client constructs the expected GET, follows the
provider redirect policy, and attaches the loaded cookies. New code adds safe
hop/response diagnostics and prevents jar reads from rewriting `cookies.txt`.

### CONFIG FAILURE

The current `.env` selects `STORAGE_BACKEND=postgres`, but the configured local
PostgreSQL database `hh_ai_responder_s3` is unavailable. This was observed as a
separate pre-HH startup failure. Passing the explicit read-only-compatible
`-storage-backend json` flag allowed the request-layer diagnosis to proceed;
this database issue is not the cause of HH's 403.

### AUTH/SESSION FAILURE

Supported by the response evidence: authenticated cookies were attached, but
HH returned 403 `ForbiddenPage` with an account-login link. The safe operational
classification is `AUTH_SESSION_EXPIRED`; the provider does not expose whether
the session was expired, revoked, or otherwise rejected. No login impersonation
or session fabrication was attempted.

### PROVIDER FAILURE

The public vacancy GET succeeds while the authenticated profile GET is rejected
by HH. The provider response includes `Server=ddos-guard`, but the body has a
login/ForbiddenPage shape and no explicit challenge marker. This is not enough
evidence to call it an anti-bot challenge, so it is not classified as
`ANTI_BOT_OR_CHALLENGE`.

## Doctor and Career Agent behavior

New command:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false ./hh-ai-responder hh-doctor
```

It runs one bounded public GET and one bounded authenticated profile GET. It
does not start vacancy discovery after the authenticated check fails and it
prints only safe response metadata, cookie names and sanitized body signals.

`career-agent --shadow` now fails before discovery with:

```text
HH read access unavailable: classification=AUTH_SESSION_EXPIRED action=refresh HH authenticated session/cookies ... writes_attempted=0
```

## Stage 29 Shadow validation

The doctor did not pass, so the required real automatic and manual/legacy Stage
29 Shadow runs were correctly **not advanced to discovery**. No honest provider
dataset exists for this pass:

| Metric | Automatic | Manual/legacy |
|---|---:|---:|
| Raw / Duplicates / Unique | N/A | N/A |
| Already responded | N/A | N/A |
| Obvious reject | N/A | N/A |
| Preliminary clear / needs detail | N/A | N/A |
| Detail requested / succeeded / failed | N/A | N/A |
| Final routed / final REVIEW_REQUIRED | N/A | N/A |
| AI evaluated / MATCH / REJECT / REVIEW_REQUIRED | N/A | N/A |
| Would apply | N/A | N/A |
| Shadow writes | 0 | 0 |
| ACCOUNTING | N/A — discovery not started | N/A — discovery not started |

Therefore the key experiment remains pending:

| Metric | Stage 28 | Stage 29.1 |
|---|---:|---:|
| Unique | 105 | N/A — authenticated read blocked |
| Needs detail | not separately measured in Stage 28 | N/A |
| Detail fetched | 14 | N/A |
| Final `REVIEW_REQUIRED` | 82 | N/A |

No MATCH/high-confidence Stage 29 candidates were produced; consequently no
top-five candidate list or cover-letter preview is claimed.

## Safety and verification

- HH writes: **0**.
- Shadow writes: **0**.
- No application, chat, test, resume-touch, or job-search-status request was
  sent.
- Tests cover expired-cookie classification, login redirect, 403/challenge
  classification, valid authenticated GET, actual cookie attachment, no body
  or secret logging, doctor GET-only behavior, doctor no-write behavior, and
  Career Agent fail-fast.
