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

The canonical `application_preparations` table contains four historical ready preparations for this vacancy. They all point to the canary provider resume identity; no preparation row was mutated during validation.

Browser authentication was restored with the operator-supplied cookie file. The normal headed browser doctor reported `AUTH_OK` for Home, My resumes, and the target vacancy. Cookie values were not logged.

The canonical PostgreSQL application projection now contains exactly one HH row for this vacancy:

- ApplicationID: `application-57ddaa3e233210edcb704c844f63181f`.
- HH negotiation/application external ID: `5603136427`.
- Status/raw status: `applied` / `RESPONSE`.
- Projection: `partial=true`, `data_completeness=partial`.
- Authoritative metadata: `delivery_confirmed=true`.
- Application↔conversation relation: `conversation-f3a01bf880297d699544560c2744a605`.
- Count invariant: applications `1`, distinct non-empty HH external IDs `1`.

The partial projection is intentionally not treated as sufficient by itself. The preparation is suppressed only because the same HH application also has provider identity, a confirming response status, and authoritative `delivery_confirmed=true` evidence. The regression case for partial-without-authoritative-evidence remains actionable.

### Read synchronization and communication refresh

- `career-agent browser-doctor --headed` with the supplied cookies: `Overall: AUTH_OK`.
- First canonical `hh sync applications`: `fetched=40, created=9, updated=11, unchanged=20, skipped=0`.
- Second canonical `hh sync applications`: `fetched=40, created=0, updated=0, unchanged=40, skipped=0`; the repeat was idempotent.
- Full `hh sync conversations` was run read-only. Its observed report was `requests=285`, `cache_hits=271`, `fetched=271`, `created=4`, `updated=0`, `unchanged=0`, `skipped=1`; two unrelated `vacancy not found` errors were reported for conversations whose local vacancy rows were absent.
- Targeted canary conversation sync for HH conversation `5658569933`: `fetched=1, created=1, updated=0, unchanged=0, skipped=0`.
- Fresh PostgreSQL relation after targeted sync: conversation status `waiting_employer`, no employer message timestamp, one stored message, employer/inbound messages `0`.
- A subsequent `career-agent daily --json` invocation was an idempotent replay of an already-running same-day daily run and returned `FAILED`/no communication items; it was not used as evidence for suppression. The independent fresh dashboard reload and `GET /api/career/attention` read model loaded from PostgreSQL and returned `count=38`, with zero attention items for vacancy `137609053`.

The fresh dashboard process was started after the sync work and then stopped cleanly. This provides the restart/reload boundary for the attention read model. The canary preparation is absent from the reloaded target-vacancy attention queue, while unrelated attention remains visible.

### Fresh API preflight and repeat-send protection

Fresh read-only API preflight, using the configured API token file without exposing its contents, reported:

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
requested resume present: NO
application availability: UNAVAILABLE (duplicate response is proven)
server acceptance: NOT_ATTEMPTED
```

This is the expected post-canary fail-closed repeat-send state. No approval or nonce was created or consumed during this validation.

## Phase B — implementation and regression evidence

Implemented and verified:

- Preparation attention is suppressed only by authoritative HH evidence: non-partial HH application, provider identity, applied-or-later status, and provider delivery confirmation or HH reconciliation evidence. A partial/local `JobApplication` row alone remains actionable.
- Single controlled API apply now uses `controlledAPIApplicationService`.
- `apply-batch` accepts one through three explicit approval files, rejects normalized duplicate paths and duplicate vacancy IDs, and uses one shared API client, attempt store, reconciliation reader, audit sink, application service, and `HHWriteGateway`.
- Fresh vacancy preflight is executed per item immediately before nonce reservation and possible transport. Pre-send blocks continue to later explicitly approved items; transport/delivery/persistence uncertainty stops the batch.
- Dry-run batch emits a plan and consumes no approval nonce or HH write capability.
- A concurrent same-approval regression proves exactly one provider boundary call; the competing runner receives deterministic nonce failure without deadlock or ambiguous duplicate execution.
- Gateway batch cap is three; the fourth mutation is blocked before provider transport.

Verification passed before the final attention-evidence patch, and the final full verification is rerun after the patch below:

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
- Working-tree patch — authoritative `delivery_confirmed` evidence suppresses a partial application projection; partial-only projections remain actionable.

Branch remains unmerged and unpushed.

## Phase A verdict

`POST_CANARY_FIXED_AND_PASS`

Reason: browser auth was restored, canonical application sync is durable and idempotent, the canary conversation is linked in PostgreSQL, a fresh dashboard reload suppresses only the authoritative applied evidence, and fresh API preflight keeps repeat-send unavailable because the response is already duplicated. The daily orchestration replay remains a same-day running-run artifact, but it did not change the canonical application/conversation projection and was not used to bypass attention safety.

## Safety counters

```text
new applications sent: 0
HH POST: 0
employer messages sent: 0
resume mutations: 0
production approvals created: 0
production approvals consumed: 0
HH writes: 0
```

The original dirty checkout at `/Users/Yaroslav/Documents/dev/hh-ai-responder` remained unchanged. The isolated branch remains unmerged and unpushed.

## Final verdict

`CONTROLLED_BATCH_READY`
