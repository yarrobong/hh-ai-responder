# RESET-API-1 Task 7 report

## Scope

Implemented only the zero-write API shadow/parity validation requested by Task
7. Changes are limited to the test-only
`internal/runtime/hh_api_shadow_test.go` harness and
`docs/validation/VALIDATION_RESET_API_1.md`. No production transport, domain,
planner, router, AI, policy, CLI, browser, or HH write path was modified.

## RED

The failing test was written first and run before the harness existed:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test ./internal/runtime ./internal/adapters/hh/api -run 'Test.*Shadow|Test.*Write|Test.*Mutation'

internal/runtime/hh_api_shadow_test.go: undefined: runAPIShadowFixture
FAIL
```

This was the expected RED failure for the missing test-only bounded harness.

## GREEN and verification

The harness now proves, using only injected `httptest.Server` sources:

- API-selected reads issue only GET to the allowlisted read endpoints;
- zero application, chat, resume-mutation, or job-search-status mutation paths;
- explicit API failure does not call browser fallback;
- applications and conversations fail with typed capability errors without
  requests or empty success pages;
- missing duplicate-state relation fails with a typed capability error;
- page/result limits and required dry-run/write-disabled/json-storage
  environment are asserted;
- bounded normalized IDs/fields, planner/query intent, and router outcomes are
  compared without raw responses or complete prompts;
- AI parity remains explicitly unknown/unproven.

Passed focused commands:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test -count=1 ./internal/runtime ./internal/adapters/hh/api -run 'Test.*Shadow|Test.*Write|Test.*Mutation'
ok   hh-ai-responder/internal/runtime
ok   hh-ai-responder/internal/adapters/hh/api [no tests to run]

HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 HH_MAX_VACANCIES_PER_RUN=100 STORAGE_BACKEND=json \
go test -count=1 ./internal/runtime ./internal/adapters/hh/api ./internal/careeragent
ok   hh-ai-responder/internal/runtime
ok   hh-ai-responder/internal/adapters/hh/api
ok   hh-ai-responder/internal/careeragent
```

`gofmt` was run on the touched Go test file. The validation report includes
the starting SHA and `origin/main` SHA; the final commit SHA is the Task 7
commit recorded below after commit creation.

Repository checks also passed with the required dry-run/write-disabled
environment: `gofmt -w .`, `go test -count=1 ./...`, `go vet ./...`,
`go build ./...`, `git diff --check`, and the race-enabled relevant-package
suite (`./internal/runtime ./internal/adapters/hh/api ./internal/careeragent`).

## Report contents

`docs/validation/VALIDATION_RESET_API_1.md` records the proven architecture and
allowlisted endpoints, browser-only/probe-required operations, equal/different/
unknown parity rules, zero-write audit, synthetic test commands, and the
no-private-data boundary. It explicitly records zero real HH writes and zero
real authenticated HH calls.

## Secret-scan evidence

The staged Task 7 diff was scanned with a high-risk pattern audit for private
keys, API-key prefixes, JWTs, bearer tokens, cookie files, and `.env` artifacts;
it reported no matches. The source and report were also reviewed for
authorization codes, raw authenticated response bodies, complete prompts, and
private exports. No such data is present. The only credential-shaped test value
is an in-memory synthetic fixture label and is not emitted by the harness or
report. Unrelated pre-existing untracked files were not staged.

## Commit

Commit message: `docs: validate RESET-API-1 HH API transport`
Commit SHA: recorded after the Task 7 commit is created.
