# RESET-API-2D — documented applicant application availability

Date: 2026-09-20

Scope: authenticated HH API reads only. The live checks used
`HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false`. No application, chat, resume,
job-search-status, browser, router, Stage29.6, or AI operation was performed.

## `quick_responses_allowed` audit

The field is decoded by the private API wire model in
`internal/adapters/hh/api/wire.go` from the optional JSON member
`quick_responses_allowed` returned by the authenticated vacancy detail request:

```text
GET https://api.hh.ru/vacancies/{vacancy_id}
```

The normalized path was introduced by the original API-preflight change. The
field has no provider-neutral meaning elsewhere in the repository and no
applicant capability semantics are documented for it. The current live detail
response for vacancy `137112468` contains:

```json
{
  "type": {"id": "open"},
  "archived": false,
  "response_url": null,
  "apply_alternate_url": "present",
  "has_test": false,
  "response_letter_required": false,
  "relations": [],
  "quick_responses_allowed": false
}
```

HH's published vacancy contract documents `response_url` as the URL for direct
vacancies (`type.id=direct`) and `apply_alternate_url` as the applicant web
response URL. It does not document `quick_responses_allowed` as an applicant
response prerequisite. The field is therefore retained only for compatibility
and audit visibility; it is not used to derive API application availability.

## Capability matrix

| Evidence | Source | Meaning for preflight |
| --- | --- | --- |
| vacancy detail and `archived` | `GET /vacancies/{id}` | active/inactive evidence |
| `type.id` and `response_url` | vacancy detail | standard HH path versus direct/external path |
| `apply_alternate_url` | vacancy detail | web response URL; diagnostic only, not a POST-success guarantee |
| `has_test` | vacancy detail | `true` is API-unavailable because documented `test_required` says API response is unavailable; unknown stays `UNKNOWN` |
| `response_letter_required` | vacancy detail | reported and passed through; it does not by itself prove POST success or failure |
| `relations` / `got_response` | vacancy detail | positive duplicate evidence when present |
| negotiations URL and complete scan | provider URLs from vacancy detail plus GET pages | duplicate `NO` only after complete vacancy/resume reconciliation |
| `suitable_resumes_url` and complete scan | provider URL from vacancy detail plus GET pages | selected resume suitability |
| `POST /negotiations` response | write-time server result | authoritative `SERVER_ACCEPTANCE`; not predicted by GET preflight |

The official applicant API contract describes application creation through the
“apply to vacancy” operation and the published negotiation error table lists
`already_applied`, `test_required`, `invalid_vacancy`, `resume_not_found`,
`resume_visibility_conflict`, `application_denied`, and applicable limit errors.
Those are server-side business outcomes, not all preflight-predictable facts.

## Availability semantics

`AVAILABLE` means `APPLICATION_ATTEMPT_ELIGIBLE`: the read-only evidence proves
duplicate `NO`, selected resume suitable, complete suitable-resume and
negotiation scans, an active/open standard HH path, and `has_test=false`.

`UNAVAILABLE` means the provider proves that this API path cannot be attempted,
including duplicate `YES`, unsuitable selected resume, an explicit direct or
external response URL, an explicitly closed path, or `has_test=true`.

`UNKNOWN` means a critical prerequisite is absent or ambiguous. It never permits
an automatic application. `AVAILABLE` is not a guarantee: only a future POST
response can establish `SERVER_ACCEPTANCE`.

## Live GET-only matrix

The rows below are the observed account-resume matrix. Resume IDs are shown only
as sanitized presence markers by the command; no raw IDs are recorded here.

| Vacancy | Duplicate | Suitable resume | Active/archive | Type | `response_url` | `apply_alternate_url` | `has_test` | `response_letter_required` | Availability | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 137112468 | NO | a89 and all 4 YES | ACTIVE / archived NO | open | absent | present | NO | NO | AVAILABLE | eligible to attempt standard POST; server acceptance is not guaranteed |
| 137493494 | NO | all 4 YES | ACTIVE / archived NO | open | absent | present | NO | NO | AVAILABLE | eligible to attempt standard POST; server acceptance is not guaranteed |
| 137244538 | YES | a89 NO; other rows YES | ACTIVE / archived NO | open | absent | present | NO | NO | UNAVAILABLE | duplicate response is proven |
| 137493556 | NO | all 4 YES | ACTIVE / archived NO | open | absent | present | YES | NO | UNAVAILABLE | `test_required` is documented as unavailable through applicant API |
| direct/external example | not discovered safely | — | — | — | — | — | — | — | not run | authenticated read-only search slices did not safely surface a direct/external vacancy; no guessed ID was used |

For `137112468`, the selected `a89…` resume is suitable and the complete
provider scan contains all four account resumes. The vacancy is active, open,
has no direct `response_url`, has no test, and has no duplicate. It is therefore
`AVAILABLE` for an application attempt even though
`quick_responses_allowed=false`.

## Verification

Initial disk check: only 826 MiB free on a 228 GiB filesystem at 100%
capacity. This is below a comfortable margin for the full Go verification
suite; the check was recorded before running commands.

Focused API/runtime tests passed after the change. Live command output reported
`server acceptance: NOT_ATTEMPTED (POST not issued)` for every row.
