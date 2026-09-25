# Post-canary controlled batch validation — 2026-09-25

## Scope and safety

Validated in isolated worktree `/Users/Yaroslav/.codex/worktrees/canary-merge-main`, branch `codex/post-canary-batch`.

- Canonical storage: PostgreSQL (`STORAGE_BACKEND=postgres`).
- Original dirty checkout was not modified.
- No approval was created or consumed for a live batch.
- No `apply-batch` live run was started.
- No batch provider POST was issued.
- Operational validation used `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false`.

## Phase A — canary and canonical PostgreSQL evidence

The read-only reliability inspection returned exactly one automatic attempt for vacancy `137609053`:

- AttemptID: `e39ead92-eb43-4ac4-b9fb-9da957d9b980`.
- Provider resume identity: `b29ec17dff103a8bc60039ed1f356c62486c37`.
- State: `TARGET_RESPONSE_CONFIRMED`.
- Classification: `CONFIRMED`.
- Provider status: `201`.
- Provider negotiation identity: `5603136427`.
- Evidence: strong HH response/negotiation evidence.
- Reliability store: PostgreSQL, available.
- Duplicate dispatch attempts observed: one.

Fresh read-only API preflight for the canary resume reported:

```text
active/archive: ACTIVE / archived NO
duplicate: YES
got_response: YES
negotiation: YES
negotiation scan: complete
has_test: NO
response_letter_required: NO
selected resume suitable: NO
suitable resume scan: complete
application availability: UNAVAILABLE (duplicate response is proven)
server acceptance: NOT_ATTEMPTED
```

The canonical `application_preparations` table contains four historical ready preparations for this vacancy. They all point to the canary provider resume identity; no preparation row was mutated during validation. The canonical `applications` projection currently has no row for this vacancy because read synchronization could not be completed with the available HH browser session.

### Read synchronization attempts

- `career-agent browser-doctor --headed`: `SESSION_EXPIRED`; browser reported 8 HH cookies but required a session refresh.
- `hh sync applications` with browser transport: blocked before sync with `HH browser session is not authenticated`.
- `hh sync applications` with API transport: failed closed with `applications (applicant negotiation semantics are unproven)`; no application sync was fabricated.
- `hh sync conversations` with browser transport: blocked by the same unauthenticated session.
- `career-agent daily --json`: blocked by the same browser authentication requirement.

These failed closed and did not produce HH writes. Because the canonical application projection and a successful read-only daily refresh could not be established, Phase A attention suppression cannot be declared operationally validated.

## Phase B — implementation and regression evidence

Implemented and verified:

- Preparation attention is suppressed only by authoritative HH evidence: non-partial HH application, provider identity, applied-or-later status, and provider delivery confirmation or HH reconciliation evidence. A partial/local `JobApplication` row alone remains actionable.
- Single controlled API apply now uses `controlledAPIApplicationService`.
- `apply-batch` accepts one through three explicit approval files, rejects normalized duplicate paths and duplicate vacancy IDs, and uses one shared API client, attempt store, reconciliation reader, audit sink, application service, and `HHWriteGateway`.
- Fresh vacancy preflight is executed per item immediately before nonce reservation and possible transport. Pre-send blocks continue to later explicitly approved items; transport/delivery/persistence uncertainty stops the batch.
- Dry-run batch emits a plan and consumes no approval nonce or HH write capability.
- A concurrent same-approval regression proves exactly one provider boundary call; the competing runner receives deterministic nonce failure without deadlock or ambiguous duplicate execution.
- Gateway batch cap is three; the fourth mutation is blocked before provider transport.

Verification passed:

```text
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

No production batch POST was used by these tests.

## Change set

- `70829a8` — stabilize preparation fingerprints.
- `f9ffc28` — controlled-batch design specification.
- `4fca760` — implementation plan.
- `d005b90` — authoritative preparation attention suppression.
- `fddbd02` — shared controlled application execution service.
- `e5c0c74` — bounded controlled application batch and CLI integration.
- `20ff02c` — validate single-apply identity before executor execution.
- `68e3cf0` — batch safety and post-canary regression coverage.

Branch remains unmerged and unpushed.

## Verdict

`POST_CANARY_BLOCKER_REMAINS`

Reason: implementation safety gates and tests pass, but canonical Phase A operational refresh remains blocked by expired HH browser authentication and API application-semantics limitations. No send is authorized by this validation.
