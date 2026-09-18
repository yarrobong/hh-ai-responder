# RESET-2 read-only reconciliation

Дата: 2026-09-19.

## Target

- Vacancy: `137244538`
- Existing local attempt: `d532cd79-7347-4540-91c2-752aedec03b2`
- Previous local state: `ACCEPTED`
- Previous provider HTTP status: `200`
- Nonce: `USED`
- Application POST attempts: `1`

No application POST, nonce generation, or retry was performed during this
reconciliation.

## Fresh GET-only evidence

Three bounded reconciliation passes read the vacancy page, the vacancy response
page, and the negotiations/application history through the existing browser
read path.

| Source | Present | Vacancy binding | Identity/type | Observation |
|---|---:|---:|---|---|
| POST response | no provider identity persisted | n/a | application/response id absent | HTTP 200 alone is not confirmation |
| redirect/final URL | no stable provider identity | vacancy URL was read | URL, not application identity | not authoritative confirmation |
| response page state | yes | `137244538` | vacancy response state | `alreadyApplied=false`, `responseImpossible=false` |
| negotiations page | yes | `137244538` | negotiation `5587518503` | `RESPONSE_BY_APPLICANT` |
| negotiations page | yes | `137244538` | conversation `5641842900` | separate typed chat identity |
| local attempt | yes | `137244538` | local attempt UUID | state remains blocking `ACCEPTED` |

The browser reader previously advertised a next cursor while re-reading the
same page. The classifier now deduplicates repeated typed identities. Different
identity types are not treated as conflicts automatically.

## Result

`UNKNOWN` / `CONFLICTING_EVIDENCE`

The conflict is between a vacancy-bound negotiation record saying a response
exists and a fresh vacancy response state saying the vacancy is still
applicable. The two provider IDs are different types and are not themselves a
conflict. There is no safe provider-side confirmation that this POST produced
the observed negotiation.

The local attempt remains permanently blocking for automatic application
dispatch. Further automatic application attempts for vacancy `137244538` are
forbidden; no retry is allowed.
