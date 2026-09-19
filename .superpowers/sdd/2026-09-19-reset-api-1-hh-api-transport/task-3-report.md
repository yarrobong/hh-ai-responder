# RESET-API-1 Task 3 report

## Scope

Implemented only the typed, read-only HH API request transport and safe error
mapping:

- `APIHHClient` with operator-supplied base URL, HTTP client, token store,
  OAuth refresh configuration, User-Agent, clock, timeout, and bounded
  `Retry-After` policy;
- authenticated GET execution with Bearer and JSON Accept headers;
- one explicit refresh-and-retry path guarded by a boolean, consuming the
  Task 2 `TokenStore` and `RefreshTokens` helper;
- `APIError` and the required classifications:
  `AUTH_REQUIRED`, `TOKEN_EXPIRED`, `TOKEN_REVOKED`,
  `APPLICATION_NOT_FOUND`, `RATE_LIMITED`, `FORBIDDEN`, and `REMOTE_ERROR`;
- bounded provider evidence decoding and safe request ID/retry metadata;
- `httptest` coverage for headers, GET-only API requests, cancellation,
  401/403/404/429/500 mapping, bounded `Retry-After`, refresh/retry, and
  secret-free errors.

No endpoint wire models, normalization, runtime selector, CLI handlers,
browser changes, API write methods, or HH write requests were added. API
resource requests construct GET only; OAuth refresh uses the existing Task 2
refresh helper as its separate token lifecycle operation.

## Reviewer fix

The focused fix keeps refresh-helper failures classified as `REMOTE_ERROR`.
Only explicit revocation evidence in an API authentication response maps to
`TOKEN_REVOKED`; generic refresh-helper errors do not expose enough provider
evidence to claim revocation. Regression coverage includes malformed refresh
responses, transient provider failures, invalid refresh configuration, network
failure, and refresh cancellation with context preservation.

Test diagnostics were also redacted so failed assertions do not print bearer
values, refreshed token structs, or the secret values used by redaction tests.

The reviewer regression command first reproduced the bug with a malformed
refresh response classified as `TOKEN_REVOKED`. After the fix, the focused
regressions passed:

```text
go test -count=1 -timeout=30s ./internal/adapters/hh/api -run 'TestAPIClientRefreshFailuresRemainRemoteErrors|TestAPIClientRefreshCancellationPreservesContextError'  # PASS
go test -count=1 ./internal/adapters/hh/api                                                                                                  # PASS
go test -race -count=1 ./internal/adapters/hh/api                                                                                             # PASS
```

## RED evidence

After writing `client_test.go` before production code, the required focused
command failed because the Task 3 symbols did not exist:

```text
go test ./internal/adapters/hh/api -run 'TestAPIClient|Test.*Status|Test.*Error'
```

The compiler reported undefined `APIHHClient`, `NewAPIHHClient`,
`APIClientOptions`, `APIErrorCode`, and the error classification constants.

## GREEN evidence

The focused transport/error tests passed after implementation. The package
and race suites also passed:

```text
go test ./internal/adapters/hh/api -run 'TestAPIClient|Test.*Status|Test.*Error'  # PASS
go test ./internal/adapters/hh/api                                                     # PASS
go test -race ./internal/adapters/hh/api                                                 # PASS
```

## Verification

The repository checks required by `AGENTS.md` passed:

```text
gofmt -w internal/adapters/hh/api/errors.go internal/adapters/hh/api/client.go internal/adapters/hh/api/client_test.go  # PASS
go test ./...                                                                                                             # PASS
go vet ./...                                                                                                              # PASS
go build ./...                                                                                                            # PASS
git diff --check                                                                                                          # PASS
```

The tests use only in-memory synthetic token fixtures and `httptest.Server`.
No real HH request, mutation, application, chat message, test submission,
resume change, or job-search-status change was performed.

## Secret scan

Targeted scans of the three Task 3 source/test files found no private-key
material, common credential prefixes, or forbidden secret artifact references.
The only credential-shaped strings are intentionally labeled synthetic test
fixtures used to assert header behavior and error redaction. Production typed
errors retain only classification, status, safe request path, request ID,
bounded retry delay, and a sanitized cause; they do not retain bodies,
headers, authorization values, token fields, form values, or authorization
codes.

## Changed files

- `internal/adapters/hh/api/errors.go`
- `internal/adapters/hh/api/client.go`
- `internal/adapters/hh/api/client_test.go`
- `.superpowers/sdd/2026-09-19-reset-api-1-hh-api-transport/task-3-report.md`
