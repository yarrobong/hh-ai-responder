# Executive summary

V2.Fix-1: **COMPLETE / CLOSED**

The HH conversation read adapter now accepts the observed current provider
shape for `resources.vacancies.<id>.archived` while retaining the historical
boolean, missing, and null shapes. The current object is retained as
transport metadata only; it is not converted into normalized archived truth.

# Reproduced provider shape

Previous expected type: `bool`

Current observed type: `object`

Relevant sanitized keys: `@hidden` (boolean)

The bounded authenticated inspection found the current shape to be an object
with exactly one provider metadata key, `@hidden`. No private message bodies,
cookies, authorization values, API keys, or database credentials were placed
in tests or this report.

# Root cause

The HH chat detail transport DTO declared `rawChatVacancy.Archived` as `bool`.
Current HH chat detail responses can encode the same provider field as an
object, so `encoding/json` rejected the entire typed response before mapping.

The old bool field meant a scalar archived flag in the transport assumption.
Tracing consumers showed that the raw chat vacancy archived field was not used
by conversation mapping, sync mapping, auto-chat context, or Career context.
Conversation state is derived from exact conversation/vacancy references,
topics, and messages instead.

# Archived semantics

Provider meaning: the current response carries an object-shaped provider
resource value with an `@hidden` boolean metadata key. The inspected contract
does not establish that this object is a normalized vacancy archive status.

Domain requirement: conversation ingestion needs the exact vacancy relation and
message/topic data; it does not require this archived field.

Mapping decision: use a strict transport union for bool, null, missing, and the
exact current `{"@hidden": bool}` object. Preserve object metadata in the raw
DTO only, and do not manufacture `archived=true` or `archived=false` from the
object.

# Implementation

- Added a dedicated `rawChatArchived` JSON transport type.
- Accepted only the known bool, null, missing, and current object shapes.
- Rejected strings, numbers, arrays, malformed objects, unknown object keys,
  and non-boolean `@hidden` values with a field-specific provider decode error.
- Added a minimal sanitized regression fixture at
  `internal/adapters/hh/read/testdata/chat_detail_archived_object.json`.
- Kept the change inside the HH read adapter; no write capability, domain
  semantics, schema, or storage behavior changed.

# Compatibility

bool true: **PASS**

bool false: **PASS**

missing: **PASS**; retains the prior zero/missing transport state.

null: **PASS**; retains the prior null transport state.

current object: **PASS**; accepted without creating archived domain truth.

unsupported shapes: **PASS**; explicit provider field decode error.

# Identity regression

ConversationID: **PASS**

VacancyID: **PASS**; exact `VACANCY` resource reference remains linked to the
provider vacancy ID.

MessageID: **PASS**

sender/direction/order: **PASS**

The fixture exercises the normal adapter mapping and `hhreadsync.MapMessages`.

# Live targeted HH read

Auth: **PASS**

HTTP method: `GET`

Typed decode: **PASS**

One bounded authenticated conversation read carrying the object-shaped field
completed successfully and produced one fetched/updated conversation. No draft
was persisted or sent.

HH mutations: **0**

# Auto-chat / Career regression

**PASS.** The full non-race and race suites passed, including existing
auto-chat orchestration/replay and conversation-policy tests, HH read/sync
tests, Career iteration tests, and conversation context/input regressions.

# V2.Fix-2

Status: **UNTOUCHED / still pending**

No automatic application-attempt authority, `FindBlocking`, applicationattempt
repository, early-gate, or related V2.Fix-2 code was changed.

# Verification

tests: **PASS** — `go test -count=1 ./...`

race: **PASS** — `go test -race ./...`

vet: **PASS** — `go vet ./...`

build: **PASS** — `go build ./...` and `go build ./cmd/hh-ai-responder`

canonical: **PASS** — focused adapter, read-sync, auto-chat, Career, and
conversation regressions included in the full suite

gofmt: **PASS** — `gofmt -l .` returned no files

diff: **PASS** — `git diff --check`

node: **PASS** — `node --check web/app.js`

Docker: **NOT APPLICABLE**

PostgreSQL schema: **UNCHANGED**

LIVE HH WRITES: **0**

# Decision

V2.Fix-1: **CLOSED**

V2.Fix-2 — Empty PostgreSQL Application Attempt Store: **READY**

V2.Fix-2 was not started automatically.
