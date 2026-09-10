# Problem

Before R11.1a, `internal/adapters/hh/write` classified every concrete non-2xx
response as `OutcomeRejected`. That included 5xx responses, even though the
repository has no endpoint contract proving that HH did not apply a mutation
before returning a server error.

That classification was unsafe for all five mutation capabilities:

- chat message;
- chat leave;
- vacancy response, including the atomic test submission;
- resume touch;
- job-search status.

The risk was a false claim that the mutation was definitely rejected and safe
to resend. The adapter already performed no transport retries; R11.1a fixes the
result semantics as well.

# Status classification

| Status/class | Outcome | Evidence/reason |
|---|---|---|
| 2xx with accepted response evidence | `ACCEPTED` | The endpoint returned a success status. Chat send additionally requires a provider message identifier; missing or malformed required JSON evidence is ambiguous. |
| 400 | `DEFINITELY_NOT_APPLIED` / `rejected` | Validation failure is a concrete provider rejection. |
| 401 | `DEFINITELY_NOT_APPLIED` / `rejected` | Authentication failure is a concrete provider rejection. |
| 403 | `DEFINITELY_NOT_APPLIED` / `rejected` | Permission failure is a concrete provider rejection. |
| 404 | `DEFINITELY_NOT_APPLIED` / `rejected` | The addressed resource/route was not accepted by HH. |
| 429 | `DEFINITELY_NOT_APPLIED` / `rejected` | HH explicitly rate-limited the mutation. No `Retry-After` resend is performed. |
| Other 4xx except 409 | `DEFINITELY_NOT_APPLIED` / `rejected` | The repository treats a concrete client-error response as a provider rejection; no retry is performed. |
| 409 | `AMBIGUOUS` | The repository does not establish whether conflict means rejection, duplicate, already-applied mutation, or another current provider state. |
| 500, 502, 503, 504 and other 5xx | `AMBIGUOUS` | No operation-specific evidence proves that HH did not receive or apply the mutation. |
| Pre-dispatch local validation/configuration/cancellation | `NOT_SENT` | The HTTP client is not invoked. |
| Network failure after dispatch, including after request-body consumption | `AMBIGUOUS` | The request may have reached HH and no response proves the result. |
| Malformed or incomplete required success evidence | `AMBIGUOUS` | HTTP success alone is insufficient when the operation requires decodable response evidence. |

The classification is shared by all five mutation operations. No endpoint
contract in the repository provides a narrower 5xx or 409 guarantee.

# 409 audit

Chat: `SendChatMessage` preserves the existing `chatId`, `text`, and
`idempotencyKey` fields. The repository proves only that the key is required
and preserved; it does not prove HH's duplicate handling semantics. A 409 is
therefore ambiguous.

Vacancy response: the repository has no 409 contract proving whether a conflict
means a rejected response or an already-existing/applied response. The atomic
test submission uses the same vacancy-response mutation and inherits the same
ambiguous classification.

Other operations: no repository evidence characterizes 409 as a definite
rejection for chat leave, resume touch, or job-search status. They therefore
also use the shared ambiguous classification.

Final classification: HTTP 409 is `OutcomeAmbiguous` with
`TransportError.Outcome == OutcomeAmbiguous`.

# 5xx

500: `OutcomeAmbiguous`.

502: `OutcomeAmbiguous`.

503: `OutcomeAmbiguous`.

504: `OutcomeAmbiguous`.

Automatic retry: **NO**.

# Ambiguity

`OutcomeAmbiguous` means that the mutation may have reached or been applied by
HH and callers must not infer that resending is safe. The adapter returns the
same ambiguous outcome in both `WriteResult` and `TransportError`, retaining
status and bounded diagnostic fields where available.

Safe to resend inferred: **NO**.

Reconciliation: **OUTSIDE / R11.4**. R11.1a does not implement reconciliation
or automatic retry.

# Compatibility

The legacy root compatibility layer now preserves `hhwrite.WriteResult.Outcome`
in `HHWriteTransportResult` and `hhwrite.TransportError.Outcome` in
`HHWriteTransportError`. The existing `DeliveryUncertain` flag remains set for
ambiguous outcomes, preserving gateway behavior and source compatibility.
`errors.Is`/`errors.As` continue to work through the existing `Unwrap` methods.

Static audit findings:

- The adapter has no retry, backoff, `Retry-After` handling, or second POST.
- The gateway blocks repeat sends after transport failure or uncertain delivery
  through durable action state and consumed nonces.
- Existing compatibility code maps ambiguous transport errors to
  `DeliveryUncertain`; it does not turn them into retryable failures.
- The legacy root mutation methods return the compatibility transport error and
  retain their existing approval, preflight, dry-run, and write-enabled gates.

Automatic resend found: **NO**.

# Tests

Added/adjusted adapter coverage for:

- chat send 500, 502, 503, 504, and 409: ambiguous, one dispatch;
- vacancy response 500: ambiguous, one dispatch;
- chat leave 500: ambiguous, one dispatch;
- resume touch 500: ambiguous, one dispatch;
- job-search status 500: ambiguous, one dispatch;
- 400 and 429: rejected, one dispatch;
- network failure after request-body consumption: ambiguous, one dispatch;
- malformed success evidence: ambiguous, one dispatch;
- pre-dispatch cancellation: not sent, zero dispatches;
- outcome consistency between `WriteResult` and `TransportError`, including
  `errors.Is` preservation.

The exact request-contract tests for all five operations remain in place.

# Verification

The following commands were run for this change:

- `gofmt -w .`
- `go test -count=1 ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
- `git diff --check`
- `node --check web/app.js`
- `go test ./internal/adapters/hh/write/...`
- `go test ./internal/ports/hhwrite/...`
- root write gateway/preflight/reconciliation tests
- Docker build, if Docker was available

Results:

- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- `go test ./internal/adapters/hh/write/...`: PASS
- `go test ./internal/ports/hhwrite/...`: PASS (package has no test files)
- Root write gateway/preflight/reconciliation coverage: PASS as part of the
  root package suite.
- Docker build: SKIPPED; Docker daemon unavailable.

One initial full-suite invocation was run concurrently with the race suite and
hit the repository's timing-sensitive `TestStage22ReadInterval` performance
assertion. The full suite passed when rerun alone, and the concurrent race
suite also passed.

LIVE HH WRITES: **0**.

# Ready for R11.2

**READY** only when verification is green and all of the following remain
true:

- 5xx cannot be misrepresented as definitely rejected without endpoint proof;
- 409 is explicitly characterized as ambiguous by the repository evidence;
- ambiguous results propagate upward;
- no automatic resend exists.

R11.2 is not started.
