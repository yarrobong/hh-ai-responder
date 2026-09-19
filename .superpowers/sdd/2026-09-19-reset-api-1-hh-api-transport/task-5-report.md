# RESET-API-1 Task 5 report

## Scope

Implemented only runtime HH read-transport selection and composition:

- browser remains the default and preserves the existing BrowserHHClient,
  browser profile, cookies.txt, and legacy HTML reader paths;
- explicit API mode probes `/me`, fails closed without a token/authenticated
  API source, and never falls back to browser;
- auto mode probes API once, then falls back only when the bounded browser
  doctor/session callback reports `AUTH_OK`;
- safe transport metadata records the selected transport and bounded fallback
  classifications without provider response bodies or credentials;
- API resume summaries and selected resume detail populate the existing
  responder resume/profile inputs and `ResumeFacts` without API-aware domain or
  use-case changes;
- API mode has no writer construction and rejects live/write-enabled settings
  before runtime composition;
- API-selected read calls do not fall through to browser/HTML reads; unsupported
  application, conversation, and preflight capabilities remain typed failures.

No CLI `hh-api auth|doctor|logout`, parity report, planner/router/policy/AI
changes, career-agent behavior changes, or HH write logic changes were made.

## TDD evidence

RED was verified before production implementation:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./internal/runtime -run 'TestSelectHHTransport|TestAPITransportWriteGuard|TestBootstrapAPIResume'
FAIL: undefined selector, transport options, metadata, write guard, and bootstrap symbols
```

GREEN was then verified with the required selection, fallback, write-guard,
and resume-bootstrap tests.

## Verification

All commands passed with `HH_DRY_RUN=true HH_WRITE_ENABLED=false`:

```text
gofmt -w internal/runtime/hh_transport.go internal/runtime/hh_transport_test.go internal/runtime/runtime.go internal/runtime/hh_read_sync.go
go test -count=1 ./internal/runtime -run 'TestSelectHHTransport|Test.*Browser|Test.*HHRead|Test.*CareerAgent|TestAPITransportWriteGuard|TestBootstrapAPIResume'
go test -count=1 ./...
go vet ./...
go build ./...
git diff --check
```

The focused runtime suite and full repository suite passed. No real HH request,
application, chat message, test submission, resume mutation, job-search status
change, or other HH write was performed.

## Secret and zero-write scan

The corrected word-boundary literal scan over all Task 5 runtime files found no
private-key material, credential prefixes, hardcoded token values, historical
gist/Android credential references, or token values in metadata/errors. The
transport selector and tests contain no POST/PUT/PATCH/DELETE methods, writer
construction, or mutation calls. Configuration field pass-throughs remain
operator-supplied only.

## Changed files

- `internal/runtime/hh_transport.go`
- `internal/runtime/hh_transport_test.go`
- `internal/runtime/runtime.go`
- `internal/runtime/hh_read_sync.go`
- `.superpowers/sdd/2026-09-19-reset-api-1-hh-api-transport/task-5-report.md`
