# Validation stage 21.4 — final third pilot draft

Date: 2026-09-07

Real HH writes: **DISABLED** (`HH_WRITE_ENABLED=false`, `HH_DRY_RUN=true`).

## Target

- Company: ИТЛ Консалтинг
- Vacancy: Разработчик БД (PostgreSQL, Middle)
- Conversation: `conversation-5dc9b17e2db65f5637dba17dfffa35b9`
- HH chat ID: `5599452149`
- Current employer message ID: `hh-message-15361539401`
- Employer message: `Напишите,пожалуйста,уровень дохода вы рассматриваете?`

Confirmed salary fact remains unchanged:

`Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно`

Saved exact draft:

`Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить.`

The previous approval was not reused. Editing the draft changed the old action to `stale` with `draft edited after approval`.

## Fresh action

- Action: `hh-action-a31ba56bedf1664d3cf2983d9e84448f`
- Draft: `ai_draft-e45bc3de1d2af526084cfbe3adcfa6f1`
- Status: `approved`
- Content hash: `f12ff1a551ede6b7ff093a0eea6cd195a2179cf1ecbebea20bb1567cf6d9266c`
- SendNonce: persisted, fresh, length 40, unused
- RelevantKnowledgeHash: `4b8786c9278153d388ee9362e48a0197cb46c001bb55fd05dc0776d3f72b6574`
- `source_message_id == last_message_id`: yes

## Read-only verification

- Targeted fresh sync: `fetched=1`, `unchanged=1`, `errors=0`.
- Fresh preflight: `READY_TO_SEND`.
- `REPLY_REQUIRED`: yes.
- `SAFE_FOR_MANUAL_REPLY`: yes.
- `ANSWERABLE`: yes.
- Latest employer message unchanged: yes.
- Candidate reply after latest employer message: absent.
- Request validation: `VALID`.
- Preview/live parity: `PASS` (shared canonical request builder).
- Write request performed: `false`.
- HHWriteClient called: `false`.
- `transport_attempted`: `false`.
- nonce consumed: `false`.
- Write capability: `BLOCKED_BY_DRY_RUN`.

The new action audit contains only `approved` and read-only `preflight_passed`; no `send_started` or transport event was created.

## Semantic safety fixes

### Generic Apps

The instruction asking whether the vacancy conditions were read is now:

- `candidate_context_status`: `USER_CONFIRMATION_REQUIRED`;
- blocker: `USER_CONFIRMATION_REQUIRED`;
- no automatic claim or text draft;
- candidate prompt: `Ты ознакомился с условиями вакансии? Если да, можно ответить ровно «Да».`

### Солюшен / GetProfi

The external invitation is now classified separately:

- `MANUAL_REVIEW`;
- `EXTERNAL_ACTION_REQUIRED / INTERVIEW_INVITATION`;
- destination: `https://interview.getprofi.me/...`;
- duration: `30–40 минут`;
- required action: proceed to the external link and complete the voice interview;
- aging: `aging`;
- no ordinary text-reply candidate and no automatic HH text response.

## Checks

- `gofmt -w .`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS

LIVE WRITES: **DISABLED**.
