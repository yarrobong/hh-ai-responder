# RESET-API-1 — HH OAuth/API transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a secure, read-only HH OAuth/API transport alongside the existing browser transport without changing current career-agent, planner, router, AI, or write behavior.

**Architecture:** Reuse the existing `internal/ports/hhread.HHReadSource` as the exact transport boundary. Keep `BrowserHHClient` and the current HTML/browser reader unchanged, add `APIHHClient` in a separate adapter package, and select one read source in the runtime composition root. OAuth and token storage are isolated from normalized HH read models and from `cookies.txt`.

**Tech Stack:** Go 1.25, standard-library `net/http`, `net/url`, `crypto/rand`, `httptest.Server`, existing `internal/config`, `internal/bootstrap`, `internal/cli`, `internal/platform.WritePrivateFileAtomic`, and existing normalized `internal/hhread` values.

**Spec:** `docs/superpowers/specs/2026-09-19-reset-api-1-hh-api-transport-design.md`

## Global Constraints

- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` for every RESET-API-1 validation command.
- `HH_TRANSPORT=browser` remains the compatibility default and preserves current browser behavior.
- `HH_TRANSPORT=api` fails closed when API authentication or a required read capability is unavailable; it never silently falls back to browser.
- `HH_TRANSPORT=auto` prefers API only after a read-only `/me` probe succeeds, then may use the existing browser doctor/session fallback.
- `BrowserHHClient` and the existing `internal/adapters/hh/read` implementation remain intact.
- API adapter code exposes read-only methods only; it does not implement `internal/ports/hhwrite`.
- Client IDs, client secrets, access tokens, refresh tokens, authorization codes, cookies, and authenticated raw response bodies must not be committed, logged, or placed in reports.
- Unknown applicant duplicate/reconciliation state remains unknown and blocks automatic parity claims.
- Do not change Search Planner semantics, RESET-6 routing, Stage29.6 precedence, AI threshold/prompts, RESET-8A semantics, cover-letter generation, application send logic, nonce/reconciliation semantics, or duplicate-application protections.
- Preserve CLI-over-environment precedence, existing flags, cookies support, JSON event compatibility, and unrelated working-tree files.

## Review Focus

- **Resume bootstrap parity:** API mode must populate the existing `HHAIResponder` resume/profile inputs without making career-agent logic API-aware; test this in Task 5.
- **Refresh rotation:** a valid refresh response replaces the stored pair atomically, while a failed/revoked refresh requires explicit reauthorization; test this in Tasks 2–3.
- **Applicant response state:** absent or ambiguous API relation data must not become `already responded=false`; test this in Task 4.
- **Explicit API mode safety:** API mode must never silently instantiate or use the browser write transport when writes are enabled accidentally; test this in Tasks 5–6.
- **CLI secrets:** auth/doctor/logout output and all typed errors must remain token-free; test this in Tasks 2 and 6.

## File and responsibility map

### New files

- `internal/adapters/hh/api/oauth.go` — OAuth URL/state, callback result, code exchange, and refresh requests.
- `internal/adapters/hh/api/tokens.go` — private token model, store interface, secure JSON file store, expiry and deletion.
- `internal/adapters/hh/api/errors.go` — API error codes, status mapping, bounded retry metadata, safe error strings.
- `internal/adapters/hh/api/client.go` — authenticated `APIHHClient` and read-only endpoint methods.
- `internal/adapters/hh/api/wire.go` — private HH API JSON wire structs and nullable/date helpers.
- `internal/adapters/hh/api/mapping.go` — wire-to-`internal/hhread` normalization.
- `internal/adapters/hh/api/*_test.go` — OAuth, token, request, mapping, error, and zero-write tests.
- `internal/runtime/hh_transport.go` — browser/API/auto selection and safe telemetry.
- `internal/runtime/hh_api_command.go` — `hh-api auth|doctor|logout` handlers.
- `internal/runtime/hh_transport_test.go`, `hh_api_command_test.go` — composition and CLI tests.
- `docs/validation/VALIDATION_RESET_API_1.md` — final report without private API data.

### Modified files

- `internal/config/config.go`, `defaults.go`, `validation.go`, `flags.go`, and tests — transport/OAuth settings and safe defaults.
- `example.env`, `.gitignore` — safe variable documentation and token-file ignores.
- `internal/cli/invocation.go`, `parse.go`, and tests — `hh-api` command classification.
- `internal/bootstrap/bootstrap.go` and tests — named `HHAPI` handler wiring.
- `internal/runtime/runtime.go`, `handlers.go`, `hh_read_sync.go` — legacy config pass-through, command dispatch, and selected read-source integration.
- `internal/ports/hhread/hh_read.go`, `internal/hhread/types.go` — add the separate optional `ResumeReadSource` capability and provider-neutral resume records; do not widen the mandatory read port with writes.

## Exact interfaces and data flow

The existing read port remains the primary transport interface:

```go
type HHReadSource interface {
    ReadVacancies(context.Context, string) (hhread.VacancyPage, error)
    ReadApplications(context.Context, string) (hhread.ApplicationPage, error)
    ReadConversations(context.Context, string) (hhread.ConversationPage, error)
}

type VacancyDetailSource interface {
    ReadVacancyDetail(context.Context, int) (hhread.VacancyRecord, error)
}
```

`APIHHClient` implements those interfaces. `BrowserHHReader`/`BrowserHHClient`
remain the existing browser implementation. API-specific read capabilities used
by doctor and resume bootstrap are narrow methods on `APIHHClient`:

```go
type UserMetadata struct { ID, AuthType string }

func (c *APIHHClient) CurrentUser(context.Context) (UserMetadata, error)
func (c *APIHHClient) ReadResumes(context.Context) ([]hhread.ResumeRecord, error)
func (c *APIHHClient) ReadResume(context.Context, string) (hhread.ResumeRecord, error)
```

Define a narrow optional `ResumeReadSource` in `internal/ports/hhread` and
provider-neutral resume records in `internal/hhread`; do not add them to
mandatory `HHReadSource`. The browser path keeps its existing
`LoadProfileData` parser and does not need to implement this API-only optional
capability in RESET-API-1.

The optional interface is:

```go
type ResumeReadSource interface {
    ReadResumes(context.Context) ([]hhread.ResumeRecord, error)
    ReadResume(context.Context, string) (hhread.ResumeRecord, error)
}
```

```text
config.Load
  -> runtime selection
  -> APIHHClient or existing BrowserHHClient/read facade
  -> HHReadSource / optional resume and detail capabilities
  -> internal/hhread normalized records
  -> HHReadSyncService / career-agent / preflight
  -> unchanged domain, planner, router, AI, and policy behavior
```

The write path remains separate: application/chat/dashboard actions continue to
enter the existing `HHWriteGateway`, `hhwrite` port, and browser/web adapter.
No API client is passed to that writer boundary.

## Task 1: Add transport/OAuth configuration and CLI classification

**Files:**
- Modify: `internal/config/config.go`, `defaults.go`, `validation.go`, `flags.go`
- Test: `internal/config/config_test.go`
- Modify: `internal/cli/invocation.go`, `parse.go`, `internal/bootstrap/bootstrap.go`
- Test: `internal/cli/parse_test.go`, `internal/bootstrap/bootstrap_test.go`
- Modify: `internal/runtime/runtime.go`, `example.env`, `.gitignore`

**Interfaces:**
- Consumes: existing `config.Load`, `Config`, CLI parsing, and bootstrap handler map.
- Produces: validated `HHTransport`, API/OAuth values, and `CommandHHAPI` intent for later runtime handlers.

- [ ] **Step 1: Write failing config tests.** Cover default `browser`, valid `browser|api|auto`, invalid transport, API URL overrides, token-file override, empty credential defaults, and CLI-over-env precedence.

```go
func TestLoadParsesHHAPIConfiguration(t *testing.T) {
    lookup := lookupFrom(map[string]string{
        "HH_TRANSPORT": "api", "HH_API_BASE_URL": "http://api.test",
        "HH_API_TOKEN_FILE": ".test-token.json",
    })
    cfg, err := Load(nil, lookup, t.TempDir())
    if err != nil { t.Fatal(err) }
    if cfg.HHTransport != "api" || cfg.HHAPIBaseURL != "http://api.test" || cfg.HHAPITokenFile != ".test-token.json" {
        t.Fatalf("unexpected HH API config: %+v", cfg)
    }
}
```

- [ ] **Step 2: Run the focused tests and verify the expected failure.**

Run: `go test ./internal/config ./internal/cli ./internal/bootstrap -run 'TestLoadParsesHHAPIConfiguration|Test.*HHAPI|Test.*Command'`

Expected: FAIL because the new fields and command kind do not exist.

- [ ] **Step 3: Add the smallest configuration implementation.** Add fields/defaults for `HH_TRANSPORT`, `HH_API_BASE_URL`, `HH_OAUTH_AUTHORIZE_URL`, `HH_OAUTH_TOKEN_URL`, client ID/secret, redirect URI, user-agent, and token file. Keep credentials empty by default. Add env parsing, validation, and an optional `-hh-transport` flag without changing `HH_BROWSER_TRANSPORT`.

- [ ] **Step 4: Add `hh-api` lexical CLI support.** Add `CommandHHAPI`, accept subcommands `auth`, `doctor`, and `logout`, and reject unknown positional subcommands before network access.

- [ ] **Step 5: Pass values through runtime config and safe docs.** Update `runtime.Config` and `legacyConfigFromPackage`; document names with empty placeholders in `example.env`; ignore only the configured token-file patterns in `.gitignore`.

- [ ] **Step 6: Run focused suites.**

Run: `go test ./internal/config ./internal/cli ./internal/bootstrap`

Expected: PASS, with existing parser behavior preserved.

- [ ] **Step 7: Commit.**

```bash
git add internal/config internal/cli internal/bootstrap internal/runtime/runtime.go example.env .gitignore
git commit -m "feat: add HH API transport configuration"
```

## Task 2: Implement OAuth primitives and private token storage

**Files:**
- Create: `internal/adapters/hh/api/oauth.go`, `tokens.go`
- Test: `internal/adapters/hh/api/oauth_test.go`, `tokens_test.go`
- Reuse: `internal/platform/atomic_file.go`

**Interfaces:**
- Consumes: operator-supplied OAuth config from Task 1 and `platform.WritePrivateFileAtomic`.
- Produces: `OAuthTokens`, `TokenStore`, `FileTokenStore`, authorization URL/state, code exchange, and refresh helpers.

- [ ] **Step 1: Write failing OAuth/storage tests.** Cover URL encoding, non-empty random state, mismatch/provider-error rejection, form-encoded exchange, `expires_in`, refresh rotation, expiry skew, malformed files, `0600` mode on Unix, atomic replacement, deletion, and secret-free errors.

```go
func TestBuildAuthorizationURLEscapesStateAndRedirect(t *testing.T) {
    got, err := BuildAuthorizationURL(OAuthConfig{
        AuthorizeURL: "https://hh.test/oauth/authorize",
        ClientID: "operator-client", RedirectURI: "http://127.0.0.1:9876/callback",
    }, "state value")
    if err != nil { t.Fatal(err) }
    parsed, err := url.Parse(got)
    if err != nil { t.Fatal(err) }
    if parsed.Query().Get("state") != "state value" { t.Fatalf("query=%v", parsed.Query()) }
}
```

- [ ] **Step 2: Run the tests and verify the expected failure.**

Run: `go test ./internal/adapters/hh/api -run 'TestBuildAuthorizationURL|Test.*Token|Test.*State'`

Expected: FAIL because the API package does not exist.

- [ ] **Step 3: Implement secret-free OAuth primitives.** Define `OAuthTokens` with access/refresh/type/expiry fields and:

```go
type TokenStore interface {
    Load(context.Context) (OAuthTokens, error)
    Save(context.Context, OAuthTokens) error
    Delete(context.Context) error
}
```

Use `crypto/rand`, `net/url`, strict state comparison, and form-encoded exchange/refresh requests. Never include form values or token fields in errors.

- [ ] **Step 4: Implement `FileTokenStore`.** Validate non-empty access/type, write with `WritePrivateFileAtomic`, produce mode `0600` where supported, preserve a refresh token only when a valid provider response omits a replacement, and delete only the configured file.

- [ ] **Step 5: Run package tests and race coverage.**

Run: `go test ./internal/adapters/hh/api && go test -race ./internal/adapters/hh/api`

Expected: PASS with no token values printed.

- [ ] **Step 6: Commit.**

```bash
git add internal/adapters/hh/api
git commit -m "feat: add HH OAuth token lifecycle"
```

## Task 3: Implement typed API request execution and error mapping

**Files:**
- Create: `internal/adapters/hh/api/errors.go`, `client.go`
- Test: `internal/adapters/hh/api/client_test.go`

**Interfaces:**
- Consumes: `TokenStore` and OAuth refresh helper from Task 2.
- Produces: `APIHHClient`, `APIError`, and codes `AUTH_REQUIRED`, `TOKEN_EXPIRED`, `TOKEN_REVOKED`, `APPLICATION_NOT_FOUND`, `RATE_LIMITED`, `FORBIDDEN`, and `REMOTE_ERROR`.

- [ ] **Step 1: Write failing request/error tests.** Use `httptest.Server` to assert Bearer and HH User-Agent headers, context cancellation, 401/403/404/429/500 mapping, bounded `Retry-After`, one refresh/retry, and absence of synthetic token/secret values in errors.

```go
func TestAPIClientSendsBearerAndHHUserAgent(t *testing.T) {
    var got http.Header
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        got = r.Header.Clone()
        w.Header().Set("Content-Type", "application/json")
        io.WriteString(w, `{"auth_type":"applicant","id":"1"}`)
    }))
    defer server.Close()
    // Construct with a synthetic in-memory token and assert the two headers.
}
```

- [ ] **Step 2: Run focused tests and verify the expected failure.**

Run: `go test ./internal/adapters/hh/api -run 'TestAPIClient|Test.*Status|Test.*Error'`

Expected: FAIL because `APIHHClient` and the error taxonomy do not exist.

- [ ] **Step 3: Add typed safe errors.** Store only classification, status, safe request path, request ID, retry delay, and wrapped cause. `Error()` must not serialize response bodies, headers, authorization values, or form fields.

- [ ] **Step 4: Add authenticated request execution.** Client options include base URL, `http.Client`, token store/provider, user-agent, clock, timeout, and bounded retry policy. Only GET requests are constructed in this stage. Guard refresh/retry with a boolean so it cannot recurse.

- [ ] **Step 5: Map provider evidence.** Decode only bounded safe fields such as `oauth_error`, `type`, `value`, `description`, and request ID. Distinguish revoked tokens, rate limits, forbidden access, missing resources, and remote failures.

- [ ] **Step 6: Run package tests.**

Run: `go test ./internal/adapters/hh/api && go test -race ./internal/adapters/hh/api`

Expected: PASS; the server observes only GET requests from the read client.

- [ ] **Step 7: Commit.**

```bash
git add internal/adapters/hh/api/errors.go internal/adapters/hh/api/client.go internal/adapters/hh/api/client_test.go
git commit -m "feat: add safe HH API request transport"
```

## Task 4: Add API wire models and read-only normalization

**Files:**
- Create: `internal/adapters/hh/api/wire.go`, `mapping.go`
- Modify: `internal/hhread/types.go`, `internal/ports/hhread/hh_read.go`
- Test: `internal/adapters/hh/api/client_test.go`, `mapping_test.go`

**Interfaces:**
- Consumes: authenticated GET execution from Task 3 and existing `hhread.VacancyRecord`, `VacancyPage`, `ApplicationRecord`, and `ApplicationPage`.
- Produces: `CurrentUser`, `ReadResumes`, `ReadResume`, `ReadVacancies`, `ReadVacancyDetail`, and explicit capability errors for unproven negotiations/conversations.

- [ ] **Step 1: Write failing endpoint decode tests.** Serve synthetic non-private JSON for `/me`, `/resumes/mine`, `/resumes/{id}`, `/vacancies`, `/vacancies/{id}`, and an incomplete relation shape. Assert paths, query parameters, pagination, normalized fields, and unknown-state preservation.

- [ ] **Step 2: Run endpoint tests and verify the expected failure.**

Run: `go test ./internal/adapters/hh/api -run 'Test.*Me|Test.*Resume|Test.*Vacanc|Test.*Relation'`

Expected: FAIL because the wire structs and methods do not exist.

- [ ] **Step 3: Define private wire structs.** Model only IDs, titles, descriptions, company, area, address, salary, skills, roles, experience, employment, schedule, work format/properties, archive/response/test indicators, timestamps, resume details, and negotiation evidence. Keep provider JSON types private.

- [ ] **Step 4: Implement user/resume reads.** Decode `/me`, `/resumes/mine`, and `/resumes/{id}` into safe read models. Preserve absent/null fields as empty/unknown rather than false claims.

- [ ] **Step 5: Implement vacancy search/detail.** Translate existing planner-generated intent into API query parameters in one function. Preserve page cursor semantics and map API fields into the same `hhread.VacancyRecord` consumed by search/detail/router/AI.

- [ ] **Step 6: Implement conservative relation behavior.** Do not synthesize `already responded=false`. If verified relation evidence exists, map it into application metadata; otherwise return an explicit probe-required/capability error for callers that require duplicate state. Keep negotiations/messages explicit not-yet-implemented until applicant semantics are proven.

- [ ] **Step 7: Add the optional resume capability without widening the mandatory read port.** Add `ResumeReadSource` and provider-neutral resume records, assert that `APIHHClient` implements it, and leave `BrowserHHClient` unchanged because its existing profile parser remains authoritative for browser mode.

- [ ] **Step 8: Run package tests and formatting.**

Run: `gofmt -w internal/adapters/hh/api internal/hhread internal/ports/hhread && go test ./internal/adapters/hh/api ./internal/hhread ./internal/ports/hhread`

Expected: PASS with no token-like fields or private raw responses in normalized output.

- [ ] **Step 9: Commit.**

```bash
git add internal/adapters/hh/api internal/hhread internal/ports/hhread
git commit -m "feat: normalize HH API read models"
```

## Task 5: Wire browser/API/auto selection without changing business logic

**Files:**
- Create: `internal/runtime/hh_transport.go`
- Test: `internal/runtime/hh_transport_test.go`
- Modify: `internal/runtime/runtime.go`, `internal/runtime/hh_read_sync.go`, and relevant runtime composition tests

**Interfaces:**
- Consumes: config, `APIHHClient`, existing browser source/reader, `HHReadSource`, and `HHAIResponder` compatibility seams.
- Produces: selected read source plus safe transport metadata; existing use cases keep receiving `HHReadClient` unchanged.

- [ ] **Step 1: Write failing selection tests.** Cover browser default, explicit API with missing token, explicit API with `/me` failure, auto API success, auto API failure plus browser `AUTH_OK`, auto failure with browser unavailable, and safe fallback metadata.

```go
func TestSelectHHTransportAutoPrefersAPI(t *testing.T) {
    api := fakeReadSource{user: UserMetadata{ID: "1", AuthType: "applicant"}}
    browser := fakeReadSource{}
    selected, meta, err := selectHHReadSource(context.Background(), TransportOptions{Mode: "auto", API: api, Browser: browser})
    if err != nil { t.Fatal(err) }
    if selected != api || meta.Selected != "API" { t.Fatalf("selected=%v meta=%+v", selected, meta) }
}
```

- [ ] **Step 2: Run selection tests and verify the expected failure.**

Run: `go test ./internal/runtime -run 'TestSelectHHTransport|Test.*Transport'`

Expected: FAIL because the selection seam does not exist.

- [ ] **Step 3: Add a narrow selector.** Use a typed options struct containing mode, API probe/source, browser source, and browser doctor callback. `browser` returns the existing source; `api` requires `/me`; `auto` attempts API once and invokes browser doctor only for fallback. Keep fallback reasons bounded classifications.

- [ ] **Step 4: Integrate into `NewHHAIResponder` carefully.** Preserve current browser construction and `LoadProfileData` behavior. For API selection, construct `APIHHClient`, read `/me`, own resumes, and selected resume detail, then populate the existing responder resume/profile fields used by `careeragent.NormalizeResumes`, Search Planner, detail, router, and AI. Do not add transport conditionals to business functions.

- [ ] **Step 5: Guard API mode from accidental live writes.** Do not construct an API writer. If API mode has `HH_DRY_RUN=false` or `HH_WRITE_ENABLED=true`, return `WRITE_DISABLED`/`NOT_IMPLEMENTED` before any writer or mutation request. Browser mode keeps the existing writer/gateway.

- [ ] **Step 6: Delegate through the existing responder read facade.** Make `HHAIResponderReadClient` use the selected source for vacancies/detail/safe application reads. Retain all old browser/HTML methods and tests.

- [ ] **Step 7: Run focused runtime tests.**

Run: `go test ./internal/runtime -run 'TestSelectHHTransport|Test.*Browser|Test.*HHRead|Test.*CareerAgent'`

Expected: PASS, with default/browser tests still selecting the browser implementation.

- [ ] **Step 8: Commit.**

```bash
git add internal/runtime/hh_transport.go internal/runtime/hh_transport_test.go internal/runtime/runtime.go internal/runtime/hh_read_sync.go
git commit -m "feat: select HH API and browser read transports"
```

## Task 6: Wire `hh-api auth|doctor|logout` and enforce safe output

**Files:**
- Create: `internal/runtime/hh_api_command.go`, `hh_api_command_test.go`
- Modify: `internal/runtime/handlers.go`, `internal/bootstrap/bootstrap.go`, `internal/cli/invocation.go`, `internal/cli/parse.go`

**Interfaces:**
- Consumes: validated config, OAuth/token store, `APIHHClient`, injectable browser opener/callback receiver, and bootstrap streams.
- Produces: operator commands with safe human output and no write-shaped HH calls.

- [ ] **Step 1: Write failing command tests.** Test auth with fake callback/token endpoint, doctor with `httptest.Server`, missing-token doctor failure, logout deleting the configured file, and output redaction using sentinel values.

- [ ] **Step 2: Run command tests and verify the expected failure.**

Run: `go test ./internal/runtime ./internal/bootstrap ./internal/cli -run 'TestHHAPI|Test.*Doctor|Test.*Logout'`

Expected: FAIL because the handler and command wiring do not exist.

- [ ] **Step 3: Add handler and dispatch.** Add `HHAPI` to `bootstrap.Handlers`, route `CommandHHAPI` in `handlerFor`, and dispatch it from `NewHandlers` to `runHHAPICommand`.

- [ ] **Step 4: Implement auth with injectable seams.** Generate state, build the configured URL, open or print it without printing codes, receive a local callback for a configured localhost redirect or an explicit operator callback otherwise, validate state, exchange code, save tokens, call `/me`, and print only safe metadata.

- [ ] **Step 5: Implement doctor and logout.** Doctor performs only token-file inspection, `/me`, resumes, and one bounded vacancy read. Logout deletes only the token file and reports presence/deletion, never token contents.

- [ ] **Step 6: Run command and redaction tests.**

Run: `go test ./internal/runtime ./internal/bootstrap ./internal/cli`

Expected: PASS; captured output contains none of the access-token, refresh-token, client-secret, authorization-code, or cookie sentinels.

- [ ] **Step 7: Commit.**

```bash
git add internal/runtime/hh_api_command.go internal/runtime/hh_api_command_test.go internal/runtime/handlers.go internal/bootstrap internal/cli
git commit -m "feat: add HH API auth doctor and logout commands"
```

## Task 7: Add zero-write API shadow and parity validation

**Files:**
- Create: `internal/runtime/hh_api_shadow_test.go` for the injected API source and method audit
- Create: `docs/validation/VALIDATION_RESET_API_1.md`
- Test: focused shadow/parity tests under `internal/runtime` or `internal/adapters/hh/api`

**Interfaces:**
- Consumes: API/browser selection, normalized records, current Search Planner/router/AI pipeline, and existing dry-run/write gates.
- Produces: repeatable bounded comparison evidence and a validation report with no private data.

- [ ] **Step 1: Write a failing zero-write shadow test.** Run API-selected reads against an `httptest.Server` that records methods and paths; assert every HH method is GET and no application/chat/resume/status endpoint is requested.

```go
if strings.Contains(strings.Join(serverMethods, ","), "POST") {
    t.Fatalf("API shadow issued a mutation method: %v", serverMethods)
}
```

- [ ] **Step 2: Run the shadow test and verify the expected failure.**

Run: `go test ./internal/runtime ./internal/adapters/hh/api -run 'Test.*Shadow|Test.*Write|Test.*Mutation'`

Expected: FAIL until the API-selected path and guard are connected.

- [ ] **Step 3: Implement the bounded harness.** Use `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, `HH_MAX_SEARCH_PAGES_PER_PROFILE=3`, `HH_MAX_SEARCH_PAGES_PER_RUN=48`, `HH_MAX_VACANCIES_PER_RUN=100`, and `STORAGE_BACKEND=json`. Capture only normalized IDs, bounded field comparisons, router outcomes, and safe classifications.

- [ ] **Step 4: Compare browser/API outputs.** Compare planner profiles, query intent, raw/distinct vacancy IDs, detail fields, professional roles, experience, schedule, employment, work format, location, salary, response state, router outcomes, and AI rows. Classify differences as equal, different with explanation, or unknown/unproven. Record unavailable/differently interpreted parameters explicitly.

- [ ] **Step 5: Run focused shadow/regression tests.**

Run: `go test ./internal/runtime ./internal/adapters/hh/api ./internal/careeragent`

Expected: PASS with zero mutation requests and no planner/router/AI snapshot changes.

- [ ] **Step 6: Write the validation report.** Include starting SHA, final SHA, origin/main SHA, OAuth flow, transport architecture, endpoints proven, browser-only/probe-required operations, parity findings, zero-write audit, tests, and secrets audit. Exclude tokens, credentials, auth codes, cookies, and raw authenticated responses.

- [ ] **Step 7: Commit.**

```bash
git add docs/validation/VALIDATION_RESET_API_1.md internal/runtime internal/adapters/hh/api
git commit -m "docs: validate RESET-API-1 HH API transport"
```

## Task 8: Full verification and handoff

**Files:**
- Modify only: `docs/validation/VALIDATION_RESET_API_1.md` with the exact command results and SHAs captured by this task.

- [ ] **Step 1: Run the required repository checks.**

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

Expected: every command exits 0. If an unrelated existing test fails, record its exact package/test/output and do not claim completion.

- [ ] **Step 2: Perform a tracked-files secrets audit.** Do not read or print `.env`, browser profiles, token files, or authenticated artifacts.

```bash
git grep -n -I -E 'CLIENT_SECRET|access_token|refresh_token|authorization_code|HH_OAUTH_CLIENT_SECRET' -- ':!example.env' || true
git diff --cached --check
git status --short
```

Expected: only intentional field names in code/tests/docs; no working values, cookies, or private API bodies.

- [ ] **Step 3: Verify write safety.** Confirm the API adapter has no `hhwrite` imports or mutation methods, API shadow observed zero non-GET requests, and browser write tests still pass with dry-run/write-disabled settings.

- [ ] **Step 4: Update the validation report with exact evidence.** Record command names, package results, implementation SHA, origin/main SHA, real HH writes (`0`), secrets committed (`0`), and whether API reads are ready to become default. If duplicate-state/negotiation parity remains unproven, state that API transport is not ready to become the default for all reads.

- [ ] **Step 5: Stop before write migration.** The next stage is a separately reviewed duplicate-state/negotiation read probe, followed by a separate design for application/chat writes. Do not add those changes to RESET-API-1.
