# RESET-API-3 — controlled HH API application POST

Date: 2026-09-20
Status: design approved in conversation; implementation pending written-spec review

## Purpose

Add the first narrowly scoped HH API application write path. One command
invocation may target exactly one vacancy and one explicitly selected provider
resume. Normal career-agent search, scheduling, and bulk operation remain
read-only. The existing browser writer remains available and unchanged.

## Safety contract

An API application POST is permitted only when all of these are proven in the
same invocation:

- `HH_TRANSPORT=api`, `HH_DRY_RUN=false`, and `HH_WRITE_ENABLED=true`;
- exactly one positive vacancy ID and one provider resume ID were supplied;
- the approved application artifact matches both IDs and confirms the
  existing business decision (`MATCH_CONFIRMED`/equivalent);
- fresh API preflight is `AVAILABLE`;
- duplicate is `NO`, never unknown;
- selected resume is suitable and the suitable-resume scan is complete;
- negotiation scan is complete;
- vacancy is active/open and uses the standard applicant path;
- no direct/external response URL is present;
- `has_test=false` is known;
- the existing write gateway authorizes the mutation;
- durable local attempt/nonce protection reserved this exact target before
  transport; and
- the per-run cap allows this one attempt.

Any failed or unknown critical condition blocks before POST. `MATCH` and
`AVAILABLE` are eligibility evidence, not server acceptance.

## Explicit command

Add:

```text
hh-api apply <vacancy-id> --resume-id <provider-resume-id> [--approval-file PATH]
```

Both IDs are mandatory. The command accepts no search expression, vacancy list,
bulk mode, or implicit resume fallback. The approval file defaults to the
existing configured career-agent pilot artifact path, with an explicit path
available for controlled tests and operation.

The artifact is the bridge to existing business logic. It must prove the exact
vacancy, selected provider resume, confirmed match, and already-produced cover
letter when required. The command does not generate or enrich a letter and
does not reinterpret candidate facts. ID mismatch blocks.

Before POST, print only a sanitized summary:

```text
Vacancy: <numeric provider ID>
Resume: present(<shortened provider ID>)
Application availability: AVAILABLE
Match approval: CONFIRMED
Cover letter: present|omitted
Write cap: 1
POST: about to attempt exactly one controlled application
```

Never print tokens, cookies, authorization headers, complete provider IDs, raw
response bodies, prompts, or unrelated candidate data.

## Components and data flow

### Narrow API adapter

Create `APIApplicationWriter` under `internal/adapters/hh/api`. It implements
the existing `hhwrite.VacancyResponseWriter` boundary but carries a distinct
`ProviderResumeID`; it must not confuse that ID with the browser resume hash.

The adapter owns only:

- form encoding for `POST /negotiations` with `vacancy_id`, `resume_id`, and
  optional `message`;
- API authorization/User-Agent headers;
- bounded response reading and safe provider error extraction;
- provider status and `Location`/negotiation identity extraction; and
- conversion to provider-neutral `hhwrite.WriteResult` and
  `hhwrite.TransportError` values.

`APIHHClient` must not gain a generic `Post`, `Do`, or arbitrary write method.
If auth refresh must be shared, extract only a private authenticated-request
primitive; do not expose a general write client.

### Controlled application service

The command composes this ordered workflow:

1. validate explicit IDs and load the approval artifact;
2. perform fresh API preflight;
3. validate all safety and letter gates;
4. obtain existing write-gateway authorization;
5. reserve the durable application attempt;
6. dispatch exactly one API mutation;
7. perform targeted GET reconciliation; and
8. persist and render the final outcome.

This service is not called by career-agent search or bulk loops in RESET-API-3.

Reuse the existing durable application-attempt store/executor. Reserve before
POST and retain blocking states across restart or interruption. The reservation
identity must include vacancy provider ID, provider resume ID, and application
operation semantics. The local attempt ID is correlation only and is not sent
as a provider idempotency key. A reservation or persistence failure must not
produce a retryable “not sent” result when delivery may be uncertain.

Configure the existing gateway for one write per run and one application per
run; do not raise or bypass global limits.

## Letter and test contract

The provider `message` field is sent only when the application endpoint
supports it and the approved artifact contains a non-empty validated letter.
If the fresh vacancy requires a letter and the artifact has no valid letter,
block before POST. If a letter is not required, empty or omitted message is
allowed. This stage does not build a cover-letter generation engine.

The existing cover-letter validator remains authoritative for empty,
placeholder, markdown/structured-output, and internal-automation noise. No
candidate fact may be inferred from vacancy text.

Tests/questionnaires are unsupported by this API path. Known `has_test=true`
and unknown test state both block; the existing browser atomic-test path is
not routed through this adapter.

## Transport classifications

Normalize at least:

| Class | Meaning | Retry |
| --- | --- | --- |
| `SUCCESS` | provider confirms application creation | never in this invocation |
| `ALREADY_APPLIED` | provider explicitly reports an existing response | never |
| `BUSINESS_REJECTED` | provider explicitly rejects the request | never automatically |
| `AUTH_REQUIRED` | token/session is absent, expired, revoked, or unauthorized | never automatically |
| `RATE_LIMITED` | provider rate limit or applicable limit response | stop; no loop |
| `UNKNOWN_SEND_RESULT` | request may have reached HH but result is uncertain | never automatically |

Timeout, connection reset, EOF after the body may have been sent, and
ambiguous 5xx become `UNKNOWN_SEND_RESULT`. No second POST is allowed.
Provider business classification is used only when safe error fields prove it.
Sensitive fields are excluded from logs.

## Targeted reconciliation

After any successful or ambiguous attempt, use existing GET-only evidence for
the exact vacancy and selected resume:

- `got_response` relation;
- matching negotiation/application record; and
- negotiation ID when available.

Expose these final outcomes:

- `POST_SUCCESS_RECONCILED`;
- `POST_SUCCESS_UNCONFIRMED`;
- `ALREADY_APPLIED_RECONCILED`;
- `UNKNOWN_SEND_RECONCILED_SUCCESS`; and
- `UNKNOWN_SEND_UNRESOLVED`.

Unconfirmed or unresolved states remain blocking and never authorize a retry.
Reconciliation may update local evidence but cannot authorize another POST.

## Dry-run and compatibility

`HH_DRY_RUN=true` blocks before the adapter. `HH_WRITE_ENABLED` must be true,
and this command requires exactly `HH_TRANSPORT=api`; `auto` and `browser` are
blocked. Existing browser protections are not weakened.

Blocked and dry-run paths may perform reads, artifact validation, local audit
writes, and GET reconciliation, but must issue zero state-changing HH
requests. No API application POST is made during implementation or automated
verification.

## Verification requirements

Add tests for command parsing, artifact binding, every gate and unknown state,
letter rules, form encoding, all response classes, ambiguous failures and
no-retry behavior, durable reservation/restart residue, reconciliation, the
one-run cap, dry-run/API-only enforcement, sanitized output, and unchanged
browser/bulk wiring. Test HTTP methods with `httptest.Server` and never use
real HH writes.

Before completion run:

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./...
git diff --check
```

Live validation, if later authorized, must use one explicitly approved
`AVAILABLE` vacancy and must not use known duplicate `137244538` or known test
vacancy `137493556`.

## Non-goals

- no bulk/autonomous API application mode;
- no integration into normal career-agent operation;
- no API test, chat, resume, status, or other mutation;
- no new cover-letter generation engine;
- no browser fallback from explicit API mode;
- no blind retry or retry-on-absence behavior.
