# Validation stage 20.2

Date: 2026-09-07

## Fresh Sber2B sync

- conversation: `conversation-aaeeeed46754bc9d34221479bc415eeb`
- company: Сбер2B
- chat.id: `5599440665`
- fresh sync result: `unchanged=1`
- last employer message: `hh-message-15361510747`
- reply requirement: `REPLY_REQUIRED`
- eligibility: `SAFE_FOR_MANUAL_REPLY`
- salary: `ANSWERABLE`
- terminal state: no

## New approved action

- ID: `hh-action-1c8886de9bb7fdff0807b863a148bf0e`
- SendNonce persisted: yes
- SendNonce length: 40
- content hash: `f12ff1a551ede6b7ff093a0eea6cd195a2179cf1ecbebea20bb1567cf6d9266c`
- RelevantKnowledgeHash: `4b8786c9278153d388ee9362e48a0197cb46c001bb55fd05dc0776d3f72b6574`
- last employer message ID: `hh-message-15361510747`
- old action `hh-action-75f9dcf7fdff4df4848d10a606a23567`: retained as failed with consumed nonce

## Request and parity

- Method: `POST`
- Endpoint: `https://chatik.hh.ru/chatik/api/send`
- Content-Type: `application/json`
- Payload fields: `chatId=5599440665`, exact approved text, `idempotencyKey=<redacted>`
- idempotencyKey source: `SendNonce`
- idempotencyKey length: 40
- request preview: `VALID`
- preview/live request parity: `PASS`
- preview and live use the single `buildHHWriteRequest` builder
- boundary validation: 29 invalid, 30 valid, 40 valid, 41 invalid

## Dashboard dry-run

- Safety: `READY_TO_SEND`
- Request validation: `VALID`
- Write capability: `BLOCKED_BY_DRY_RUN`
- lifecycle: `preflight_passed → send_ui_clicked → send_api_received → gateway_invoked → capability_blocked`
- HHWriteClient called: false
- transport_attempted: false
- nonce consumed: false

Metrics before and after are unchanged:

- `write_attempts_total=2`
- `failed_writes=2`
- `successful_writes=0`

No real HH POST was executed. `HH_WRITE_ENABLED` was not enabled and `HH_DRY_RUN` was not disabled.
