# Post-Canary Validation and Controlled Batch Applications

## Goal

Validate the first reconciled real HH application against the canonical PostgreSQL projections, suppress historical preparation records once a confirmed application exists, and add an explicit operator-only batch command for at most three separately approved applications. No autonomous approval or live batch send is part of this change.

## Fixed invariants

- The canonical production storage for this workflow is PostgreSQL, selected by the current production configuration.
- The existing preparation fingerprint serialization fix is preserved in its own commit before batch work.
- `HHWriteGateway` remains the only HH mutation boundary.
- Daily Career Agent and scheduler paths remain read-only and never receive batch-write capability.
- A batch accepts only explicit approval file paths, from one through three items.
- Every item is freshly preflighted immediately before its possible POST and is reconciled before the next item.
- A pre-send block may skip to the next explicitly approved item; transport, delivery, or persistence uncertainty stops the run without retry.
- `HH_DRY_RUN=true` performs validation and produces an execution plan with zero HH writes.
- This implementation performs no live batch POST.

## Phase A: post-canary validation

Use the existing PostgreSQL-backed attempt, application, conversation, workflow, and attention stores. The validation workflow must prove the canary identity, one provider dispatch attempt, successful reconciliation, and the current duplicate/negotiation evidence without exposing secrets.

Run the existing HH application read-sync against the canonical stores twice. The first pass may create or update the single canonical `JobApplication`; the second pass must be unchanged and must not create another application for the same HH response. Conversation sync remains read-only with respect to HH. If HH has no conversation, retain a valid applied application with no fabricated conversation.

Refresh the Communication Agent using current provider reads. Employer messages remain untrusted. A newly applied vacancy with no employer message produces no reply, interview, test, or offer work item. If a message exists, it is imported and classified for review only; no response is sent.

The attention queue remains a derived read model. A confirmed application or response suppresses preparation-derived `application_ready` and vacancy-review attention for the same vacancy. Historical preparation rows are not mutated or deleted. A ready preparation without confirmed application remains actionable. The suppression should be implemented by a small pure helper or typed reason, not a new lifecycle.

Repeat-send validation is offline/read-only: fresh HH duplicate state is checked, consumed approval nonce reuse is rejected before transport, and a new preparation cannot treat an already applied vacancy as an unresponded send target. No second POST is issued.

Restart/reload checks use the same PostgreSQL stores and must preserve attempts, reconciliation evidence, applications, exact conversation relations when present, consumed approval state, and suppressed actionability.

## Phase B: batch architecture

Extract or introduce one controlled application execution service around the existing single-application flow. Both `hh-api apply` and the new batch command use the same approval validation, preparation binding, fresh preflight, write gateway, attempt persistence, and reconciliation logic. The batch executor is orchestration/telemetry only and is not a second application source of truth.

The proposed command surface is:

```text
hh-api apply-batch --approval-file approval-a.json [--approval-file approval-b.json] [--approval-file approval-c.json]
```

The parser rejects zero, more than three, duplicate approval paths, duplicate vacancies, implicit directory/glob discovery, and approvals with invalid or mismatched identity. The executor uses one shared gateway instance configured with `MaxWritesPerRun=3` and the existing `HH_MAX_WRITES_PER_DAY` counter. It never silently truncates input.

For each item, in input order:

```text
load exact approval
→ validate approval/preparation/nonce
→ fresh GET-only preflight
→ if eligible, submit through HHWriteGateway
→ reconcile the exact attempt
→ suppress corresponding preparation actionability after confirmed success
→ continue only when the outcome is confirmed or the item was blocked before transport
```

`ALREADY_APPLIED_PRE_SEND`, stale approval, closed vacancy, unsuitable resume, and other pre-transport blocks become item results and may allow later approved items to proceed. `UNKNOWN`, `DELIVERY_UNCERTAIN`, `PERSISTENCE_UNCERTAIN`, or unconfirmed post-transport outcomes produce `STOPPED_UNCERTAIN` and prevent all later items. No retry is implicit.

The batch result is a typed in-memory execution report with run status `COMPLETED`, `PARTIAL`, `STOPPED_UNCERTAIN`, or `FAILED_BEFORE_SEND`. Existing durable attempts and applications remain the authoritative records; no new application state machine or schema migration is introduced unless implementation evidence proves one is required.

## Tests

Phase A tests cover: reconciled attempt reload, one-application read-sync, idempotent second sync, exact application/conversation relation, absent conversation validity, applied-preparation attention suppression, unapplied preparation visibility, consumed approval rejection, and confirmed duplicate blocking.

Batch tests use fakes or `httptest.Server` only and cover: one and three sequential successes; four approvals rejected before write; duplicate approval/vacancy inputs; stale approval; preflight block on item two with item three allowed; uncertain item two stopping item three; shared per-run and daily limits; reconciliation before the next POST; nonce consumed once; replay/restart without resend; concurrent duplicate execution protection; and dry-run with zero writes.

## Verification and delivery

Run `gofmt -w .`, `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`. No live batch application is permitted. Keep logical commits separate, leave the original dirty checkout untouched, and do not merge or push without a separate operator instruction.
