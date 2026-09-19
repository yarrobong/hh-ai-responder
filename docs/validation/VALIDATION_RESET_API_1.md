# RESET-API-1 HH API transport validation

Date: 2026-09-19
Scope: Task 7 only — zero-write API shadow and bounded parity validation.

## Git and execution boundary

- Starting SHA for Task 7: `4d49a11a80850d1503e182230eb239cced7a77b9`.
- `origin/main` SHA at validation: `d417a304a42cf332dea61a5c96af8ec3dc9373c7`.
- Final Task 7 SHA: recorded in `task-7-report.md` after the task commit; the
  report is part of the committed tree, so this document does not embed a
  self-referential commit hash.
- No unrelated working-tree files were staged or changed.
- All validation sources use injected `httptest.Server` endpoints. No real
  authenticated HH request was made.

## Transport architecture proven

The shadow selects the existing explicit `HH_TRANSPORT=api` source through the
runtime selector and exercises the existing normalized read boundary. The
browser source is an injected sentinel. An API probe failure in explicit API
mode returns a typed transport error and does not invoke the browser doctor or
browser read source.

The proven synthetic read endpoints are:

| Endpoint | Method | Safe purpose |
| --- | --- | --- |
| `/me` | GET | API selection/current-user metadata |
| `/resumes/mine` | GET | own-resume summaries |
| `/resumes/{id}` | GET | selected-resume read |
| `/vacancies` | GET | bounded vacancy search |
| `/vacancies/{id}` | GET | vacancy detail read |

The method/path audit allowlists only those paths. It records method and path
only; it does not capture query values, headers, tokens, cookies, or response
bodies.

## Browser-only and probe-required operations

- Applicant applications and employer conversations are unsupported by the API
  transport and return typed `CapabilityError` values without an HTTP request,
  empty success, or browser fallback.
- Duplicate/application state is only safe when an unambiguous relation is
  present. A missing relation returns the typed `duplicate-state` capability
  error from the relation-required read path.
- Application submission, test submission, chat messages, resume mutation,
  job-search status mutation, and other HH writes remain browser/write-gateway
  concerns and were not exercised or migrated.
- AI output parity was not invoked or claimed; it is explicitly
  `unknown/unproven` in the shadow result.

## Bounded parity findings

The test comparator uses only bounded normalized IDs, vacancy fields, safe
response-state classifications, planner profile/query metadata, router outcome
metadata, and an explicit AI-unproven row. It excludes descriptions, complete
prompts, raw provider responses, credentials, and private exports.

- Synthetic API/browser normalized vacancy IDs: **equal**.
- Synthetic API/browser title, company, professional roles, experience,
  schedule, employment, work format, location, and salary fields: **equal**
  for the matching fixture.
- Synthetic API/browser response-state classification: **equal** when both
  sides provide the same explicit relation.
- Missing or differently interpreted optional fields: **unknown/unproven**;
  the comparator never converts absence into a negative or positive claim.
- The regression comparator separately demonstrates a salary difference as
  **different**, with an explanation, and missing response relation as
  **unknown/unproven**. This is a synthetic classifier test, not a live HH
  parity claim.
- Search Planner profiles/query intent and RESET-6 router outcome are
  **equal** for identical normalized inputs. No planner, router, AI threshold,
  prompt, policy, or RESET-8A production behavior was modified.
- AI rows are **unknown/unproven** because the read-only shadow does not call
  an AI provider.

## Zero-write audit

Every shadow command sets:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_SEARCH_PAGES_PER_PROFILE=3
HH_MAX_SEARCH_PAGES_PER_RUN=48
HH_MAX_VACANCIES_PER_RUN=100
STORAGE_BACKEND=json
```

The injected API server observed zero non-GET requests and zero
application/chat/resume/status mutation paths. The API client used a synthetic
in-memory token fixture only to authenticate the local test server; no token
value is printed, logged, or included in this report. Unsupported capabilities
made zero HTTP requests. Explicit API failure made zero browser fallback calls.

Real HH writes: **0**.
Real authenticated HH calls: **0**.

## Tests and verification

RED was observed before the harness existed:

```text
go test ./internal/runtime ./internal/adapters/hh/api -run 'Test.*Shadow|Test.*Write|Test.*Mutation'
internal/runtime: undefined: runAPIShadowFixture
```

Focused shadow/regression tests passed:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test -count=1 ./internal/runtime ./internal/adapters/hh/api -run 'Test.*Shadow|Test.*Write|Test.*Mutation'

HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test -count=1 ./internal/runtime ./internal/adapters/hh/api ./internal/careeragent
```

The first command passed the five `TestAPIShadow...` tests. The second command
passed all three relevant packages. The race-enabled relevant-package command
also passed. In addition, this task ran `gofmt -w .`, full `go test ./...`,
`go vet ./...`, `go build ./...`, `git diff --check`, and the staged-diff
secret scan; all passed. Task 7 itself made no production changes.

## Secrets and private-data audit

The report contains no client IDs, client secrets, access or refresh tokens,
authorization codes, cookies, authorization headers, raw authenticated
responses, complete AI prompts, private chat exports, or private resume
exports. The test uses synthetic fixture labels only. The tracked Task 7
changes consist of the test-only shadow harness and this documentation report.
