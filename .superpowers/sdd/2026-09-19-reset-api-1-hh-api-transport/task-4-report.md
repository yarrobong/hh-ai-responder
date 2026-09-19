# RESET-API-1 Task 4 report

## Scope

Implemented only the read-only HH API wire models, normalization, endpoint
methods, optional resume capability, and capability-error regressions. No
runtime selector, CLI handler, browser reader, write adapter, planner/router,
AI, or policy code was changed.

## RED

Added `httptest` and mapping tests before the endpoint implementation and ran:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/adapters/hh/api -run 'Test.*Me|Test.*Resume|Test.*Vacanc|Test.*Relation'
```

The expected compiler failure occurred because `CurrentUser`, resume/vacancy
read methods, `SearchParams`, `CapabilityError`, and `ResumeRecord` did not yet
exist.

## GREEN

Implemented private bounded wire decoding and provider-neutral normalization for:

- `GET /me`;
- `GET /resumes/mine`;
- `GET /resumes/{id}`;
- `GET /vacancies` with explicit API query translation and page cursors;
- `GET /vacancies/{id}`.

The normalized vacancy model preserves known bits for nullable archive, test,
letter, response-count, and applicant-relation fields. An absent or incomplete
relation remains unknown. API applications and conversations return typed
`CapabilityError` values without an HTTP request, empty-page success, or browser
fallback.

## Verification

All commands below passed:

```text
gofmt -w internal/adapters/hh/api internal/hhread internal/ports/hhread
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test -count=1 ./internal/adapters/hh/api -run 'Test.*Me|Test.*Resume|Test.*Vacanc|Test.*Relation'
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test -count=1 ./internal/adapters/hh/api ./internal/hhread ./internal/ports/hhread
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test -race -count=1 ./internal/adapters/hh/api
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./...
go vet ./...
go build ./...
git diff --check
```

The standalone normalization-file secret scan found no private-key material,
credential prefixes, gist/Android credential references, or token-like output
fields. Fixtures are synthetic and are not emitted by normalized values,
errors, reports, or logs. No real HH request or write was performed.

## Changed files

- `internal/adapters/hh/api/client.go`
- `internal/adapters/hh/api/client_test.go`
- `internal/adapters/hh/api/mapping.go`
- `internal/adapters/hh/api/mapping_test.go`
- `internal/adapters/hh/api/wire.go`
- `internal/hhread/types.go`
- `internal/ports/hhread/hh_read.go`

## Parity limitations

- API negotiation/application and employer-conversation semantics remain
  explicitly unsupported until applicant relation behavior is proven.
- Suitable-resume selection parity is not claimed; this task only exposes
  own-resume summaries/details.
- Browser mode remains unchanged and remains authoritative for its existing
  profile parser.
