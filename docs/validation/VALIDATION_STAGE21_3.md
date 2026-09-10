# Validation stage 21.3 — third controlled HH pilot

Date: 2026-09-07

Real HH writes: **DISABLED** (`HH_WRITE_ENABLED=false`, `HH_DRY_RUN=true`).

## Fresh read-only sync and ranking

- Fresh `hh sync conversations`: `fetched=261`, `updated=53`, `unchanged=208`, `errors=0`.
- `hh pilot-shortlist`: 2 candidates.
- Safe manual-reply eligibility: 3; the third safe conversation is an aging interview invitation and is not a pilot candidate.
- Status/courtesy messages are classified as `REPLY_OPTIONAL` and excluded from actionable status; 10 such records were excluded by the eligibility report.

### Rank 1 — selected fallback

- Company: ИТЛ Консалтинг
- Vacancy: Разработчик БД (PostgreSQL, Middle)
- Conversation: `conversation-5dc9b17e2db65f5637dba17dfffa35b9`
- HH chat.id: `5599452149`
- Exact employer message: `Напишите,пожалуйста,уровень дохода вы рассматриваете?`
- Question type: salary expectation / factual question; high-risk topic requiring manual review.
- Confirmed facts available: `Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно`; confirmed salary preferences.
- Context status: `ANSWERABLE`.
- Freshness: `fresh`.
- Eligibility: `SAFE_FOR_MANUAL_REPLY`.
- Vacancy/state: non-terminal; latest human message is from employer; complete history; no candidate reply after it.
- Draft: `Рассматриваю предложения: Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно.`
- Warning: `HIGH_RISK_TOPIC_MANUAL_REVIEW`.
- Why safe for this controlled dry-run: direct employer question, exact confirmed fact only, short plain-text draft, verified destination, fresh read-only state, and no complex interview/test flow. Salary is used only as fallback because no safe new-fact candidate is available.

### Rank 2 — not selected

- Company: Generic Apps
- Vacancy: Партнер / Амбассадор IT-решений (DevOps, QA, разработка)
- Conversation: `conversation-e1f1446204a77f8ce792df65520b37a9`
- Exact employer message: `Вы внимательно ознакомились с вакансией и ее условиями? Если ознакомились - напишите именно "Да" , иначе не дойдет ответ`
- Question type: instruction / acknowledgement gate, not a direct factual question.
- Confirmed facts available: none.
- Context status: `ANSWERABLE`.
- Draft preview: `Да, я ознакомился с вакансией и её условиями.`
- Warning: `STATUS_OR_COURTESY_MESSAGE`.
- Decision: not suitable for the third pilot; no new candidate fact and no direct factual question.

### Rank 3 — not selected

- Company: Солюшен
- Vacancy: Программист/Fullstack-разработчик Python
- Conversation: `conversation-f8a3f2bbb56e6ca39b5b4442fa887d9a`
- Exact employer message: `Здравствуйте, Ярослав! Мы внимательно рассмотрели ваш отклик и приглашаем вас на следующий этап отбора. Первые два этапа мы проводим онлайн через платформу GetProfi — это удобный формат интервью, который можно пройти в комфортное время и в своём темпе. Без видеосвязи, без ожидания в Zoom — просто отвечаете голосом на вопросы. Это экономит ваше время и позволяет нам быстрее принять решение. 👉 Для продолжения перейдите по ссылке: https://interview.getprofi.me/#/i/538e5041b81bf4c6105f735d3f6aac3bee1ed1c5d2fa98f2fa1ab887ea905cdf Интервью занимает около 30-40 минут. Решение принимает руководитель после просмотра анализа ваших ответов. Ответ по результатам вы получите в личном кабинете HH в течение 7–10 дней. Будем рады познакомиться ближе. 📱 Если ссылка не открылась или кажется неактивной: зажмите её → «Скопировать» → вставьте в браузере и откройте там.`
- Question type: external interview invitation.
- Confirmed facts available: Python/Django are confirmed in candidate knowledge, but the employer did not ask a factual question.
- Context status: `ANSWERABLE`; eligibility `SAFE_FOR_MANUAL_REPLY`.
- Freshness: `aging`.
- Warning: manual coordination required; 30–40 minute external interview flow.
- Decision: not suitable for controlled send rehearsal.

## THIRD PILOT CANDIDATE

Company: ИТЛ Консалтинг  
Vacancy: Разработчик БД (PostgreSQL, Middle)  
Conversation: `conversation-5dc9b17e2db65f5637dba17dfffa35b9`  
Question type: salary expectation  
Employer message: `Напишите,пожалуйста,уровень дохода вы рассматриваете?`

Confirmed facts:

- Salary preference: `Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно`.
- No unconfirmed experience, seniority, Kubernetes production, Kafka, Celery production, ML Engineer, or highload claims used.

Draft:

`Рассматриваю предложения: Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно.`

Safety: `READY_TO_SEND` (rehearsal only; capability blocked)  
Request validation: `VALID`  
Write capability: `BLOCKED_BY_DRY_RUN`  
HHWriteClient called: `false`

Action: `hh-action-25167e2f92860111e1e8541036ac857f`  
Draft: `ai_draft-e45bc3de1d2af526084cfbe3adcfa6f1`  
Current employer message ID: `hh-message-15361539401`  
`source_message_id == last_message_id`: yes  
Content hash: `6ec10bcad62c2c9f2f327a158ca61f1afc6cd44ad2799e70d2544a85076d2499`  
RelevantKnowledgeHash: `4b8786c9278153d388ee9362e48a0197cb46c001bb55fd05dc0776d3f72b6574`  
SendNonce: present, length 40; persisted and unused.

## Fresh validation and full dry-run

- Targeted fresh conversation sync: `fetched=1`, `unchanged=1`, `errors=0`.
- Latest employer message unchanged: yes (`hh-message-15361539401`).
- `REPLY_REQUIRED`: yes.
- `SAFE_FOR_MANUAL_REPLY`: yes.
- `ANSWERABLE`: yes.
- Complete history: yes.
- Reply already sent: no.
- Destination `chat.id`: exists (`5599452149`).
- Preflight: `READY_TO_SEND`.
- Request validation: `VALID`.
- Request preview/live parity: `PASS`.
- Request method: `POST`; endpoint shape: HH Chatik send endpoint; idempotency source: `SendNonce`; key length: 40.
- Dry-run lifecycle: `send_ui_clicked → send_api_received → gateway_invoked → capability_blocked → send_failed`.
- `transport_attempted=false`.
- nonce consumed: `false`.

## LIVE PILOT HISTORY

- Salary: `DELIVERY_CONFIRMED` (`hh-action-1c8886de9bb7fdff0807b863a148bf0e`).
- Relocation: `DELIVERY_CONFIRMED` (`hh-action-c275669ca15a874ce7f7449ceddd1844`).
- Both remain terminal and are blocked by fresh-process preflight with `DELIVERY_ALREADY_CONFIRMED`.
- Neither prior successful pilot is in the current pilot shortlist for the same employer message.
- Metrics unchanged by this rehearsal: `write_attempts_total=4`, `successful_writes=2`, `delivery_confirmed=2`, `delivery_uncertain=0`, `failed_writes=2`.

## NEXT ALTERNATIVES

2. Generic Apps — acknowledgement/instruction gate; no confirmed candidate fact; not a direct factual question.

3. Солюшен — Python interview invitation; aging and complex external interview flow; manual review required.

## Checks

- `gofmt -d .`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS

LIVE WRITES: **DISABLED**.
