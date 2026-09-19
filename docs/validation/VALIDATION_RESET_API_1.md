# RESET-API-1 HH API transport validation

Date: 2026-09-19
Scope: Task 8 final verification and handoff only.

## Git and execution boundary

- Task 8 starting SHA: `b4f5d54704577478ec477bd70850f2ef4d9dc729`.
- Final implementation SHA before the Task 8 report commit:
  `b4f5d54704577478ec477bd70850f2ef4d9dc729`.
- `origin/main` SHA at validation: `d417a304a42cf332dea61a5c96af8ec3dc9373c7`.
- The required checks produced no Go-file or other unrelated working-tree
  changes. Pre-existing untracked user files were not staged, read, or
  modified.
- All network evidence uses injected `httptest.Server` fixtures. No real
  authenticated HH request or real HH write was made.

## Required repository verification

Every command below ran with:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
```

Exact command results:

```text
gofmt -w .
exit=0; no output

go test ./...
ok   hh-ai-responder/cmd/hh-ai-responder
ok   hh-ai-responder/internal/adapters/embedding/openai
ok   hh-ai-responder/internal/adapters/hh/api
ok   hh-ai-responder/internal/adapters/hh/read
ok   hh-ai-responder/internal/adapters/hh/write
ok   hh-ai-responder/internal/adapters/llm/openai
ok   hh-ai-responder/internal/adapters/storage/json
ok   hh-ai-responder/internal/adapters/storage/postgres
ok   hh-ai-responder/internal/application
ok   hh-ai-responder/internal/applicationattempt
ok   hh-ai-responder/internal/autochatattempt
ok   hh-ai-responder/internal/bootstrap
ok   hh-ai-responder/internal/browsersession
ok   hh-ai-responder/internal/candidate
ok   hh-ai-responder/internal/candidateparity
ok   hh-ai-responder/internal/careeragent
ok   hh-ai-responder/internal/cli
ok   hh-ai-responder/internal/config
ok   hh-ai-responder/internal/conversation
?    hh-ai-responder/internal/hhread [no test files]
?    hh-ai-responder/internal/llm [no test files]
ok   hh-ai-responder/internal/platform
ok   hh-ai-responder/internal/platform/scheduler
?    hh-ai-responder/internal/ports [no test files]
?    hh-ai-responder/internal/ports/applicationattempt [no test files]
?    hh-ai-responder/internal/ports/autochatattempt [no test files]
?    hh-ai-responder/internal/ports/hhread [no test files]
?    hh-ai-responder/internal/ports/hhwrite [no test files]
?    hh-ai-responder/internal/ports/llm [no test files]
ok   hh-ai-responder/internal/runtime
ok   hh-ai-responder/internal/semantic
ok   hh-ai-responder/internal/usecase/applicationanswer
ok   hh-ai-responder/internal/usecase/applicationattempt
ok   hh-ai-responder/internal/usecase/applicationattemptpolicy
ok   hh-ai-responder/internal/usecase/applicationpilot
ok   hh-ai-responder/internal/usecase/applicationprocessing
ok   hh-ai-responder/internal/usecase/applicationreconciliation
ok   hh-ai-responder/internal/usecase/applicationsubmission
ok   hh-ai-responder/internal/usecase/autochatorchestration
ok   hh-ai-responder/internal/usecase/autochatreconciliation
ok   hh-ai-responder/internal/usecase/autochatreply
ok   hh-ai-responder/internal/usecase/candidateacquisition
ok   hh-ai-responder/internal/usecase/candidatecontext
ok   hh-ai-responder/internal/usecase/candidateinterpretation
ok   hh-ai-responder/internal/usecase/candidatelearningorchestration
ok   hh-ai-responder/internal/usecase/candidatemutation
ok   hh-ai-responder/internal/usecase/candidatesemanticindex
ok   hh-ai-responder/internal/usecase/candidatesemanticsearch
ok   hh-ai-responder/internal/usecase/careeriteration
ok   hh-ai-responder/internal/usecase/conversationpolicy
ok   hh-ai-responder/internal/usecase/coverletter
ok   hh-ai-responder/internal/usecase/employerreply
ok   hh-ai-responder/internal/usecase/employerreplyworkflow
ok   hh-ai-responder/internal/usecase/followupdraft
ok   hh-ai-responder/internal/usecase/followuporchestration
ok   hh-ai-responder/internal/usecase/hhreadsync
ok   hh-ai-responder/internal/usecase/hhwritegateway
ok   hh-ai-responder/internal/usecase/hhwritepreflight
ok   hh-ai-responder/internal/usecase/hhwritereconcile
ok   hh-ai-responder/internal/usecase/inboxrefresh
ok   hh-ai-responder/internal/usecase/reliabilityinspection
ok   hh-ai-responder/internal/usecase/reliabilitynotifications
ok   hh-ai-responder/internal/usecase/testanswer
ok   hh-ai-responder/internal/usecase/vacancyanalysis
ok   hh-ai-responder/internal/usecase/writeapproval
ok   hh-ai-responder/internal/vacancy
ok   hh-ai-responder/internal/vacancyranking
ok   hh-ai-responder/internal/vacancyreview
exit=0

go test -race ./...
all the same packages as go test ./... returned ok (or [no test files])
exit=0

go vet ./...
exit=0; no output

go build ./...
exit=0; no output

git diff --check
exit=0; no output
```

The race run completed without race reports. No host temporary-space cleanup
was needed during Task 8.

## API read/write safety evidence

The following focused command passed:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false \
HH_MAX_SEARCH_PAGES_PER_PROFILE=3 HH_MAX_SEARCH_PAGES_PER_RUN=48 \
HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test -count=1 ./internal/runtime ./internal/adapters/hh/api \
  -run 'Test.*Shadow|Test.*Write|Test.*Mutation'
ok   hh-ai-responder/internal/runtime
ok   hh-ai-responder/internal/adapters/hh/api [no tests to run]
exit=0
```

The shadow assertions recorded:

- API non-GET requests: **0**.
- API mutation paths: **0**.
- Unexpected API paths: **0**.
- Browser fallback calls in explicit API mode: **0**.
- Real HH writes: **0**.
- Real authenticated HH calls: **0**.

The API adapter has no `hhwrite` import or reference. Its resource-read
methods construct GET requests only; the only POST in the adapter is the
separate OAuth authorization-code/refresh exchange against the configured
token endpoint, not an HH resource mutation. `ReadApplications` and
`ReadConversations` return typed capability errors without making requests.
`BrowserHHClient` remains the existing alias and implementation in
`internal/adapters/hh/read`; its focused tests passed.

Additional focused safety commands passed:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go test -count=1 ./internal/adapters/hh/write ./internal/runtime \
  ./internal/usecase/hhwritegateway ./internal/usecase/hhwritepreflight \
  ./internal/usecase/hhwritereconcile
ok   all five packages
exit=0

HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go test -count=1 ./internal/adapters/hh/read -run 'TestBrowserHHClient'
ok   hh-ai-responder/internal/adapters/hh/read
exit=0
```

## OAuth callback safety evidence

The focused PKCE/callback/doctor command passed:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go test -count=1 ./internal/adapters/hh/api ./internal/runtime \
  -run 'TestAuthorizationSessionUsesPKCES256|TestHHAPIAuthManualReadsOnlyFullRedirectURLFromStdin|TestHHAPIAuthRejectsAuthorizationCodeArgAndDoesNotReadEnvironment|TestHHAPIDoctor|TestAPIClientConstructsOnlyGETRequests'
ok   hh-ai-responder/internal/adapters/hh/api
ok   hh-ai-responder/internal/runtime
exit=0
```

OAuth PKCE uses S256. The verifier and authorization code are ephemeral:
they remain in the in-memory authorization flow, are read from stdin only for
manual full-redirect callbacks, and are never logged or stored. Code-like CLI
arguments and the authorization-code environment path are rejected/ignored.

## Tracked-files secrets audit

The required audit ran without reading or printing `.env`, cookies, browser
profiles, token files, or authenticated artifacts:

```text
git grep -n -I -E 'CLIENT_SECRET|access_token|refresh_token|authorization_code|HH_OAUTH_CLIENT_SECRET' -- ':!example.env' || true
```

Matches were limited to intentional configuration/schema field names,
documentation/specification text, redaction assertions, and clearly synthetic
test fixtures. No working credential, access token, refresh token,
authorization code, cookie, authorization header, authenticated raw response,
private resume export, or private chat export is committed.

```text
git diff --cached --check
exit=0; no output

tracked sensitive-artifact filename audit
no tracked sensitive-artifact filenames matched
```

Secrets committed: **0**. Synthetic fixture values are test-only and are
labeled/asserted as synthetic; they are not reported, logged, or used for real
HH access.

## Parity and remaining unsupported/unproven behavior

The bounded synthetic shadow found equal normalized vacancy IDs and the
compared title, company, role, experience, schedule, employment, work format,
location, salary, and explicit response-state fields. Planner/query metadata
and router outcomes remained equal for identical normalized inputs. AI parity
is **unknown/unproven** because no AI provider was called.

The following remain unsupported or unproven and stay browser-authoritative:

- duplicate/already-responded state when the API relation is absent or
  ambiguous;
- negotiation/application-state parity and suitable-resume selection;
- negotiation detail and employer conversation/message history;
- application, test, chat, resume, reconciliation, and job-search-status
  writes.

Because duplicate-state and negotiation/application/conversation parity remain
unproven, the API transport is **not ready to become the default for all
reads**. `HH_TRANSPORT=browser` remains the compatibility default. The next
stage is a separately reviewed duplicate-state/negotiation read probe,
followed by a separately designed application/chat write migration; neither
was added to RESET-API-1.
