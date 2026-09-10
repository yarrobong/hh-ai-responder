# Executive summary

R14.5d: COMPLETE

Operator improvement: reliability events from automatic applications and legacy auto-chat now produce bounded, durable, advisory notifications in the existing \`NotificationStore\`. Notifications include local incident identity and a relative read-only reliability detail path. Reconciliation can evolve an uncertainty notification into truthful confirmation or add a conflict/persistence warning without changing replay authority.

The implementation uses a narrow typed projection boundary:

\`\`\`text
typed attempt/reconciliation outcome
    ↓
reliability notification projector
    ↓
existing NotificationStore
\`\`\`

No HH write capability, AI capability, retry, resend, reset, or new attempt operator state was added.

# Why R14.5c was skipped

R14.5c remains intentionally skipped. Local acknowledge/review annotations would add mutable operator state without adding a demonstrated safety property. Notification lifecycle remains separate from attempt/action authority.

# Notification authority

NOTIFICATION IS NOT SAFETY AUTHORITY.

Application attempt stores remain authoritative for application replay blocking. Auto-chat attempt stores remain authoritative for legacy REPLY and LEAVE replay blocking. Controlled-chat ActionStore/write state remains authoritative for controlled chat.

Notification creation, update, dismissal, snooze, or resolution only changes notification persistence and lifecycle. It cannot mark an attempt \`NOT_SENT\`, confirm an attempt, release a block, create an attempt, reconcile HH state, or send an HH request.

# Application notifications

Uncertain: \`SENDING\`, \`DELIVERY_UNCERTAIN\`, and outcome-persistence uncertainty use one stable incident key per attempt. The operator sees \`AttemptID\`, \`VacancyID\`, technical detail, and a link to \`/reliability/application-attempts/<AttemptID>\`. The wording is “Возможно отправлено; повторная отправка запрещена.” For \`SENDING\`/persistence uncertainty it also says the local state may remain \`SENDING\` and never claims “not sent”.

Confirmed: \`CONFIRMED_RESPONSE_EXISTS\` updates the same uncertainty incident to “Отклик подтверждён на HH.” The wording does not claim that the exact local attempt caused the provider response.

Conflict: conflicting reconciliation evidence creates one high-priority incident keyed by attempt and conflict category. It says that HH evidence conflicts, manual inspection is required, and automatic re-send remains blocked.

Store failure: reservation/attempt-authority failure projects one bounded \`application_store_unavailable\` notification when the notification store is available. If notification persistence is also unavailable, the workflow still fails closed and no notification recursion occurs.

# Auto-chat notifications

Reply: \`SENDING\`, \`DELIVERY_UNCERTAIN\`, and persistence uncertainty create or update one incident keyed by \`AttemptID\`, with \`ConversationID\`, \`TriggerMessageID\`, \`ActionType=REPLY\`, and a link to the auto-chat reliability detail. The message is “Ответ мог быть отправлен. Повторный автоматический ответ на это сообщение заблокирован.”

Leave: uncertain LEAVE attempts are visible with \`ActionType=LEAVE\` and the same per-attempt dedupe identity. Unsupported reconciliation updates that incident to explain: “HH не предоставляет достаточно сильных данных для безопасного подтверждения выхода из диалога.” It does not confirm or release the leave attempt.

Confirmed: exact outgoing-message evidence is described as an exact outgoing message found. Target-response evidence is described as “Ответ после сообщения работодателя подтверждён на HH; причинность конкретной авто-попытки не установлена.” It never says “Автоответ успешно доставлен” for target-only evidence.

Unsupported: repeated unsupported LEAVE reconciliation updates one existing notification and does not create scheduler-period duplicates.

# Controlled chat

Existing controlled-chat delivery-uncertain notification coverage remains in the legacy audit projection. The R14.5d changes do not alter approval, nonce, preflight, send, write-gateway, or controlled reconciliation behavior.

# Dedupe model

Reliability notification fingerprints use workflow + attempt/target identity + meaningful category, for example:

\`\`\`text
application/<AttemptID>/delivery-uncertain
autochat/<AttemptID>/delivery-uncertain
autochat/<AttemptID>/reconciliation-conflict
application/store-unavailable/<VacancyID>
autochat/store-unavailable/<ConversationID-or-global>
\`\`\`

The implementation does not deduplicate only by human text. Existing notifications are updated by fingerprint, so repeated 12-hour application gates, 15-minute auto-chat gates, dashboard refreshes, and reconciliation reads remain bounded. A new auto-chat trigger creates a separate incident because it has a separate attempt identity.

# Notification lifecycle

Dismiss: changes only the notification lifecycle to dismissed.

Resolve: changes only the notification lifecycle to resolved. Reliability confirmation projection may also resolve the corresponding notification as a projection update; it does not reconcile implicitly.

Relationship to attempt state: NONE

The dashboard explicitly distinguishes “Уведомление закрыто” from durable attempt state and explains that closing a notification does not remove the send block.

# Reconciliation integration

Application and auto-chat reconciliation services emit typed projection events after local evidence persistence. \`CONFIRMED\` updates the uncertainty notification; \`CONFLICTING\` and \`PERSISTENCE_ERROR\` create high-value notifications; \`INSUFFICIENT\` and ordinary \`UNAVAILABLE\` results do not create duplicate noise. Unsupported LEAVE reconciliation receives one stable advisory projection.

Projection is best effort. It does not invoke reconciliation and it receives no HH reader or writer capability.

# Store failure behavior

Application and auto-chat attempt-authority failures fail closed. Where the existing notification store is usable, a bounded high-priority store-health notification says: “Автоматическая отправка остановлена: хранилище состояния недоступно.” When notification persistence is unavailable too, the original safety error remains the workflow result; no fallback notification write loop is attempted.

# Notification failure behavior

Notification persistence failures are ignored at the workflow projection boundary after the durable attempt/action safety operation has determined its result. They do not change the attempt state, create a retry, repeat an HH write, or call another notification projection.

# Vocabulary

- \`SENDING\`: “Исход отправки неизвестен”
- \`DELIVERY_UNCERTAIN\`: “Возможно отправлено; повторная отправка запрещена”
- application target confirmation: “Отклик подтверждён на HH”
- auto-chat target confirmation: “Ответ после сообщения работодателя подтверждён”
- unresolved LEAVE: “Исход выхода из диалога неизвестен”
- unavailable store: “Автоматическая отправка остановлена: хранилище состояния недоступно”

Unresolved post-dispatch states never use “Не отправлено”, “Повторить”, or a retry instruction. No retry control was added.

# Security / secret exposure

Notification records expose only local attempt/target identifiers, action type, technical state through the detail link, and masked/allowlisted provider evidence already present in the read model. They do not include request keys, cookies, authorization headers, tokens, database URLs, raw provider bodies, or AI prompts/history. The auto-chat operator API continues to omit its durable request key.

# No-write proof

HH write capability: NONE in the notification projector.

AI capability: NONE in the notification projector.

Notification dismissal HH writes: 0

The projector package depends only on typed local event values and the narrow notification persistence interface. It cannot reserve attempts, reconcile provider state, send HH requests, or call an LLM.

# Regression safety

R14.1: PASS — durable application reservation/outcome behavior remains in the application attempt executor; projection is best effort after that boundary.

R14.2: PASS — application reconciliation still persists only its existing read-only evidence transition and retains blocking state on uncertainty.

R14.3: PASS — existing application early gates remain authoritative and now project at most one advisory incident for a blocking attempt.

R14.4: PASS — auto-chat trigger identity, reservation, request key, write count, and replay block are unchanged.

R14.5a: PASS — reliability read-only routes remain GET-only for inspection; notification links point to those detail routes.

R14.5b: PASS — operator reconciliation remains bounded, operator-triggered, read-only to HH, and locally evidence-persisting. Notification projection is a side effect only.

# Tests

Focused coverage includes:

- application uncertainty, dedupe, confirmation evolution, conflict, and persistence uncertainty;
- auto-chat uncertain REPLY, new-trigger separation, causality-safe confirmation, LEAVE unsupported reconciliation, and store failure;
- notification persistence failure without safety-state mutation;
- notification dismissal preserving incident identity and lifecycle-only semantics;
- existing controlled-chat, application, auto-chat, reliability inspection, dashboard, and scheduler regression suites.

# Verification

gofmt: PASS

go test: PASS (\`go test -count=1 ./...\`)

race: PASS (\`go test -race ./...\`)

vet: PASS (\`go vet ./...\`)

build: PASS (\`go build ./...\`)

canonical build: PASS (\`go build ./cmd/hh-ai-responder\`)

diff: PASS (\`git diff --check\`)

node: PASS (\`node --check web/app.js\`)

Docker: NOT APPLICABLE

Postgres live integration: NOT RUN — \`DATABASE_URL\` is absent.

LIVE HH WRITES: 0

# R14 status

R14.5d: COMPLETE

# Ready

R14.Final — Reliability / Delivery Semantics Closure Audit
READY

STOP after R14.5d. R14.Final was not started.
