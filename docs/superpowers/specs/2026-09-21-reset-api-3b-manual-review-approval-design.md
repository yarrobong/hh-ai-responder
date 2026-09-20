# RESET-API-3B — explicit operator approval for MANUAL_REVIEW_BEFORE_SEND

Date: 2026-09-21
Status: approved in conversation; amended written spec approved

## Purpose

Allow an operator to explicitly approve a safe `MANUAL_REVIEW_BEFORE_SEND`
pilot artifact for the controlled HH API application path without changing
automatic `MATCH` behavior and without making `REVIEW_REQUIRED` automatically
sendable.

The feature is an operator-controlled artifact transformation only. It does
not create a second HH mutation path and does not bypass fresh provider state.

## Explicit command

Add:

```text
hh-api approval review --pilot <pilot.json> --out <approval.json> [--letter-file <file>]
```

The command requires an explicit operator invocation. It performs local file
reads and validation only; it must issue zero HH requests and zero HH POSTs.

The existing `hh-api approval export` command remains the automatic path for
`READY_FOR_EXPLICIT_SEND + MATCH`. No generic force, ignore-review, or
skip-safety option is added.

## Manual approval eligibility

The source `PilotArtifact` is accepted only when all of the following are
proven:

- `status == MANUAL_REVIEW_BEFORE_SEND`;
- `final_decision == REVIEW_REQUIRED`;
- `hard_missing` is empty;
- `hard_unknown` is empty;
- the selected provider resume identity is known and unambiguous;
- the vacancy ID is positive and known;
- `preview_fresh_at` is present and within the existing 30-minute freshness
  window at approval time;
- the pilot contains a fresh safe provider preflight snapshot;
- the vacancy is active;
- duplicate state is known and `NO`;
- `can_apply` is known and `true`;
- test state is known and `false`; and
- the AI assessment is populated (`ai_score` is present and
  `ai_recommendation` is non-empty) and `ai_recommendation == UNCERTAIN`; and
- the source pilot nonce is empty and `nonce_used_at` is absent.

The converter rejects `BLOCKED`, `REJECT`, `MATCH`, hard missing, hard
unknown, AI `DO_NOT_APPLY`, required/unknown tests, duplicate/unknown
duplicate state, inactive vacancies, missing identity, malformed or stale
artifacts, unexpected source nonces, and any conflicting provider resume
identities. Provider-specific selected-resume suitability and API application
availability are intentionally not required here because the pilot snapshot
does not bind those API facts and approval review must issue zero HH requests.

## Approval artifact

Extend `APIApplicationApproval` with fields needed to preserve provenance and
manual approval:

- `approval_basis`, set to `OPERATOR_MANUAL_REVIEW` for manual approval;
- `operator_approved`, set to `true`;
- `operator_approval_timestamp`;
- original AI score;
- original AI recommendation;
- original AI recommendation reasons;
- original final decision (`REVIEW_REQUIRED`);
- original pilot hash/identity;
- selected resume and vacancy identity; and
- exact approved cover letter and its content hash.

The manual artifact retains `final_decision == REVIEW_REQUIRED`. It is not
rewritten to `MATCH`.

After all validation and letter review succeed, the command creates a new
cryptographically random one-time nonce. The source manual pilot has no nonce
contract: an empty source nonce is expected, `nonce_used_at` must be absent,
and an unexpected non-empty source nonce is malformed. The generated nonce is
never copied from or reused from a pilot. The approval artifact is written
atomically with private permissions.

## Cover-letter binding

If `--letter-file` is supplied, read its bytes exactly, use that exact content
as the approved letter, recompute its content hash, and run the existing
cover-letter safety validation. The approval command never invokes AI and
never rewrites the file content.

If `--letter-file` is omitted, preserve the pilot letter exactly and validate
it under the same existing cover-letter rules. No claim may be added during
approval. Any post-approval letter mutation makes the content hash invalid at
apply time.

## Apply validation

The controlled `hh-api apply` command accepts exactly two approval bases:

1. automatic: `status == READY_FOR_EXPLICIT_SEND`,
   `final_decision == MATCH`;
2. manual: `final_decision == REVIEW_REQUIRED`,
   `approval_basis == OPERATOR_MANUAL_REVIEW`, and
   `operator_approved == true`.

Both paths continue through every existing safety gate: API-only transport,
explicit vacancy/resume identity, approval freshness, exact letter hash and
cover-letter validation, fresh GET-only provider preflight immediately before
POST, duplicate `NO`, selected resume suitability, `AVAILABLE` application
state, active standard vacancy path, `can_apply`, known no-test state, one
mutation per invocation, durable attempt reservation and vacancy lock,
configured run/day limits, redirect-free transport, no retry after ambiguous
delivery, targeted reconciliation, and CAPTCHA/manual-challenge handling.

For the manual basis, approval review does not substitute for provider API
eligibility. `hh-api apply` must authoritatively re-check, from fresh GET-only
state, duplicate `NO`, selected-resume suitability, `AVAILABLE` application
state, the active standard path, `can_apply`, and known no-test state before
any POST. The existing automatic MATCH validation path remains backward
compatible and unchanged.

Manual approval never overrides fresh provider state. A changed vacancy,
duplicate, test, inactive vacancy, unsuitable resume, unknown eligibility,
stale approval, or any other failed/unknown critical gate blocks before POST.

With `HH_DRY_RUN=true` and writes disabled, apply performs the same artifact
and GET-only preflight validation, prints `WOULD_APPLY` when eligible, and
issues zero HH POSTs. A real POST remains gated by the existing write-enabled
configuration and all existing durable protections.

## Audit and privacy

Manual approval records explicit audit fields:

- `approval_basis=OPERATOR_MANUAL_REVIEW`;
- original AI score;
- original AI recommendation;
- original final decision; and
- operator approval timestamp.

Logs and command output remain sanitized. They must not include candidate
private context, complete cover letters, prompts, cookies, tokens, headers, or
raw provider response bodies.

## Tests

Use unit tests and local `httptest.Server` fixtures only. Required coverage:

- eligible manual review with no hard blockers is approved;
- automatic MATCH export/apply remains unchanged;
- REJECT, hard missing, hard unknown, test-required, duplicate, inactive,
  unknown provider eligibility, and stale pilot are rejected;
- `--letter-file` is byte-for-byte bound by a recomputed hash;
- changing the letter after approval fails apply validation;
- approval issues a fresh nonce and cannot reuse it;
- fresh apply preflight can still block a manual artifact;
- manual approval creation performs zero HH POSTs; and
- manual dry-run emits `WOULD_APPLY` with zero HH POSTs.

No implementation or verification step may perform a real HH POST. Final
validation includes the required Go checks and a GET/dry-run-only validation
using vacancy `137532422`.
