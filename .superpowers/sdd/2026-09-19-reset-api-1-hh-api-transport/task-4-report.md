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

## Reviewer follow-up fixes

Added real API resume-field coverage and normalization for `skill_set`,
`salary.amount`, and `total_experience`. Free-text `skills` is retained only as
wire input and is not promoted into the structured normalized skill list.
Vacancy mapping now uses `salary_range` when the primary salary projection is
absent. Work-format normalization aggregates all values and returns `hybrid`
when remote and office/on-site evidence coexist.

Vacancy detail reads preserve a valid detail with unknown applicant relation
when relation evidence is absent or ambiguous. The optional
`VacancyDuplicateStateSource` / `ReadVacancyDetailRequiringRelation` path
requires explicit, non-conflicting evidence and returns typed
`CapabilityError{Capability: "duplicate-state"}` with an empty result when it
cannot prove it; it never becomes `AlreadyResponded=false`, an empty
successful page, or a browser fallback.

Follow-up focused endpoint, mapping, package, race, full-suite, vet, build,
diff-check, and normalization-file secret-scan verification passed. The fixes
were committed separately from the original Task 4 implementation.

## Re-review correction

Work-format wire decoding now accepts raw strings, singleton objects, and
object arrays while preferring stable IDs/codes and recognizing localized
display names such as `Из дома` and `На месте работодателя`; combined remote
and on-site evidence normalizes to `hybrid`. The endpoint fixture covers the
object forms.

`ReadVacancyDetail` now preserves provider-unknown applicant relation state in
the valid vacancy detail. Callers that require duplicate-state proof must opt
into `ReadVacancyDetailRequiringRelation`; missing or conflicting evidence
returns the typed capability error. Applications and conversations remain
explicit capability errors, with no browser fallback behavior added.

Re-review verification:

- RED confirmed the object-form work-format and explicit relation-required
  tests failed before the correction.
- GREEN: focused API tests passed with object-form localized work formats,
  unknown detail relation preservation, and typed duplicate-state capability
  errors.
- Package tests, API race tests, `go test ./...`, `go vet ./...`,
  `go build ./...`, and `git diff --check` passed.
- Targeted token/credential-reference secret scan passed. No real HH request or
  write was performed; unrelated untracked files were not staged.
