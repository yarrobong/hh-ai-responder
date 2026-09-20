# RESET-API-3 controlled API application POST validation

Date: 2026-09-20

## Safety result

No real HH mutation was performed. The controlled command remains explicit,
single-vacancy, single-resume, and requires `--approval-file`. Its live
gateway is capped at one application mutation per invocation; no permanent
one-per-day policy was added.

The attempt executor preserves both protection scopes:

- exact attempt: application + vacancy + provider resume;
- vacancy mutation lock: application + vacancy.

Tests cover release after a proven pre-dispatch failure and blocking a second
resume after a possible dispatch, including after reopening the persistent
store.

## Dry-run evidence

The injected `httptest.Server` scenario ran with:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_TRANSPORT=api
```

`TestHHAPIApplyDryRunValidatesReadsAndNeverPosts` passed. It validated the
approval artifact, exact vacancy/resume identity, freshness/content gates,
fresh vacancy preflight, suitability, duplicate scan, test state, and
application-path eligibility; it asserted `WOULD_APPLY` and failed the test
if any POST request was observed. The mutation adapter is not constructed on
this path.

Targeted reconciliation tests passed for `SUCCESS`, `ALREADY_APPLIED`, and
`UNKNOWN_SEND_RESULT`; the API evidence reader uses targeted vacancy,
suitable-resume, and negotiation GETs only.

## Fresh GET-only preflight for proposed vacancy

Command used:

```text
HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false
go run ./cmd/hh-ai-responder hh-api preflight 137112468 --resume-id <sanitized-provider-resume-id>
```

Fresh result, with identifiers sanitized:

```text
duplicate: NO
suitable: YES
active/archive state: ACTIVE / archived NO
vacancy type: open
response_url present: absent
apply_alternate_url present: present (diagnostic web-response URL)
has_test: NO
response_letter_required: NO
negotiations URL present: YES
suitable endpoint scan complete: YES
suitable resume IDs discovered: 4
selected resume suitable: YES
existing negotiation: NO
negotiation scan complete: YES
application availability: AVAILABLE
server acceptance: NOT_ATTEMPTED (POST not issued)
final duplicate state: NO
```

Proposed live vacancy for manual review: **137112468**. This is only a
proposal; no application was submitted. A future live attempt still requires
a newly reviewed, fresh explicit approval artifact and operator authorization.

## Automated verification

All required checks passed after the final code change:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

The repository-wide tests and race tests use local fixtures/`httptest`; no
test sends a real HH POST.
