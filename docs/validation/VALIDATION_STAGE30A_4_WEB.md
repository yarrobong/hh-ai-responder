# Validation Stage 30A.4 — WEB-only preflight diagnosis

Date: 2026-09-16 (Asia/Yekaterinburg)

Scope: existing authenticated web client, existing `cookies.txt`, existing
HTTP client/cookie jar, GET-only reads. No `api.hh.ru`, HH OAuth, developer
application, API transport, or HH write path was used.

## Result

The current cookie session did not expose an authenticated vacancy page. Every
diagnostic request returned an HTTP `200 text/html` challenge page and ended at
`/account/captcha`. HTTP 200 was therefore not treated as a successful vacancy
or application response.

The old artificial state was caused by interpreting generic/ambiguous page
state as provider facts:

- `CanApply=false`: the old response parser recursively searched all embedded
  JSON for `canApply`, `canRespond`, `responseAllowed`, `isResponseAllowed`, or
  `applyAvailable`, and accepted a false value without proving it belonged to
  the requested vacancy. The HTML fallback also promoted a disabled response
  control to provider-level `CanApply=false`. Neither “no proof of YES” nor a
  disabled generic control is a strong NO.
- `TestRequired=true`: the same recursive path searched all embedded JSON for
  `userTestPresent`, `testPresent`, `hasTest`, or `testRequired`. The marker was
  read from the response/application page, but the old code did not require a
  response-state container and vacancy identity. It could therefore be a
  generic UI/component marker. The old detail/search fallback could also
  promote `Vacancy.UserTestPresent` into fresh preflight state.

The corrected parser reads only direct fields of recognised response-state
containers (`redirectConfig`, `vacancyResponse`, or `response`), requires a
matching vacancy identity when one is present, and never recursively searches
unrelated page state. A valid same-vacancy application form or active
same-vacancy apply action is positive apply evidence. Missing/disabled action,
generic test UI, unexpected page type, login, and challenge remain UNKNOWN.

## Safe diagnostic trace

The command added for reproducibility is:

```text
career-agent web-trace --known <id,id> --unknown <id,id,...>
```

It performs, for every supplied vacancy, these three GETs:

1. vacancy page;
2. the response/application page currently used by preflight;
3. the existing HTML negotiations/application-history page.

The trace prints only paths, status, content type, page class, bounded marker
booleans, form method, and bounded form-field presence. It never prints
cookies, credentials, hidden form values, raw HTML, tokens, or embedded JSON.

The sample used two locally confirmed responded vacancies (`136624185`,
`136974139`) and five of the original UNKNOWN set (`137448342`, `137444477`,
`137437860`, `137437509`, `136841654`). The same original 40 UNKNOWN IDs were
then rechecked; the 126 requests were all GETs.

| Vacancy | Known state | Request | Initial path | Final path | Status | Content type | Class | Login/challenge | Responded | Apply | Disabled | Form | Method | Test | Same-vacancy negotiation |
|---:|---|---|---|---|---:|---|---|---|---|---|---|---|---|---|---|---|
| 136624185 | local confirmed | vacancy | `/vacancy/136624185` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136624185 | local confirmed | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136624185 | local confirmed | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136974139 | local confirmed | vacancy | `/vacancy/136974139` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136974139 | local confirmed | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136974139 | local confirmed | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137448342 | UNKNOWN | vacancy | `/vacancy/137448342` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137448342 | UNKNOWN | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137448342 | UNKNOWN | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137444477 | UNKNOWN | vacancy | `/vacancy/137444477` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137444477 | UNKNOWN | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137444477 | UNKNOWN | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437860 | UNKNOWN | vacancy | `/vacancy/137437860` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437860 | UNKNOWN | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437860 | UNKNOWN | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437509 | UNKNOWN | vacancy | `/vacancy/137437509` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437509 | UNKNOWN | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 137437509 | UNKNOWN | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136841654 | UNKNOWN | vacancy | `/vacancy/136841654` | `/account/captcha` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136841654 | UNKNOWN | response/application | `/applicant/vacancy_response` | `/applicant/vacancy_response` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |
| 136841654 | UNKNOWN | negotiation/history | `/applicant/negotiations` | `/applicant/negotiations` | 200 | text/html | CHALLENGE | yes | no | no | no | no | unknown | no | no |

The remaining 35 original UNKNOWN vacancies had the same result in the
repeat: three GETs each, HTTP 200, final challenge classification, and no
vacancy/form/history marker.

## Comparative classification

| Vacancy | Known state | Vacancy page class | Response page class | Responded evidence | Apply evidence | Test evidence | Final classification |
|---:|---|---|---|---|---|---|---|
| 136624185 | local confirmed | CHALLENGE | CHALLENGE | none; fresh evidence unavailable | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 136974139 | local confirmed | CHALLENGE | CHALLENGE | none; fresh evidence unavailable | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 137448342 | UNKNOWN | CHALLENGE | CHALLENGE | none | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 137444477 | UNKNOWN | CHALLENGE | CHALLENGE | none | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 137437860 | UNKNOWN | CHALLENGE | CHALLENGE | none | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 137437509 | UNKNOWN | CHALLENGE | CHALLENGE | none | none | none | REVIEW_REQUIRED / AUTH_FAILURE |
| 136841654 | UNKNOWN | CHALLENGE | CHALLENGE | none | none | none | REVIEW_REQUIRED / AUTH_FAILURE |

No local confirmed state was overwritten by the challenge result. A local
confirmed response is not treated as fresh web proof for another vacancy, and
absence of a history entry is not treated as strong NO when the history page
itself is challenged.

## Tri-state repeat for the original 40

Before, from the supplied Stage 30A.3 run:

```text
AlreadyResponded: YES=0, NO=0, UNKNOWN=40
CanApply:         YES=0, NO=40, UNKNOWN=0
TestRequired:     YES=40, NO=0, UNKNOWN=0
```

After the parser fix and the fresh web repeat:

```text
AlreadyResponded: YES=0, NO=0, UNKNOWN=40
CanApply:         YES=0, NO=0, UNKNOWN=40
TestRequired:     YES=0, NO=0, UNKNOWN=40
Active:           YES=0, NO=0, UNKNOWN=40
```

Thus the artificial `40x CanApply=false` and `40x TestRequired=true` picture
is gone. Current live auth/challenge state prevents proving either YES or NO.

## Regression coverage

Added fixtures cover:

- known responded HTML → responded YES;
- active same-vacancy apply action → responded NO / can apply YES;
- valid application-form GET → can apply YES, matching vacancy ID, method,
  letter field and resume selector;
- missing button → can apply UNKNOWN;
- disabled/contradictory control → UNKNOWN, not NO;
- generic test UI → test UNKNOWN;
- explicit same-vacancy test marker → test YES;
- unknown/other-vacancy marker → relevant states UNKNOWN;
- login page and challenge page → UNKNOWN/auth failure;
- diagnostic transport rejects any non-GET request.

The form fixture is fetched as a page by GET. Its HTML form method is observed
only as metadata; no form is submitted and hidden values are not retained.

## Pilot and handoff

No vacancy reached proven `AlreadyResponded=NO` plus `CanApply=YES` on the
current authenticated session. Therefore detail reads and AI evaluations were
not started, and no pilot candidate was found.

```text
WEB ONLY: YES
HH API used: NO
OAuth used: NO
Root cause of CanApply=false x40: recursive generic embedded-state parser and disabled-control fallback; absence was treated as provider NO
Root cause of TestRequired=true x40: recursive generic embedded-state parser (testPresent/userTestPresent/hasTest/testRequired), with an unsafe detail fallback
Before:
  Responded YES/NO/UNKNOWN: 0/0/40
  CanApply YES/NO/UNKNOWN: 0/40/0
  Test YES/NO/UNKNOWN: 40/0/0
After:
  Responded YES/NO/UNKNOWN: 0/0/40
  CanApply YES/NO/UNKNOWN: 0/0/40
  Test YES/NO/UNKNOWN: 0/0/40
Detail reads: 0
AI evaluations: 0
Pilot: NONE
READY: NONE
Real HH writes: 0
Application POST: 0
Final commit: pending
origin/main: not pushed yet
```
