# Validation Stage 30A.3 — responded preflight truth set

Date: 2026-09-16 (Asia/Yekaterinburg)

The starting pilot result was re-read from the local, ignored pilot artifact
only to identify the same 44 discovered vacancies. The provider checks below
were fresh GET-only reads. No HTML, cookies, tokens, prompts, or private raw
responses were stored in this report.

## Audit result

The original result was:

Original fresh `AlreadyResponded=YES`: `40`

```text
Scanned: 44
Known responded: 4
Fresh ALREADY_RESPONDED: 40
Detail reads: 0
AI evaluations: 0
Writes: 0
```

The source audit found that the previous parser recursively searched the
whole embedded JSON for `alreadyResponded`, `responseAlreadySent`,
`hasResponse`, `responseExists`, and `responded`. It also accepted broad page
text. The latter generic aliases were not proven to be vacancy-scoped
response state. The parser did not derive `YES` from `CanApply=false`, a
missing/disabled button, test state, response URL, login page, unknown active
state, or generic negotiation wording directly; however, those signals were
not represented by a bounded evidence contract and could coexist with an
unrelated positive alias.

The classifier now uses this contract:

| Value | Allowed evidence | Provider signal |
|---|---|---|
| `YES` | `EXPLICIT_RESPONDED_MARKER` | vacancy-scoped explicit `alreadyResponded=true` / `responseAlreadySent=true`, or an explicit page marker such as “Вы уже откликались” / “Отклик уже отправлен” |
| `YES` | `NEGOTIATION_ID_FOUND` | independent application/negotiation record has a non-empty provider ID and the same `vacancy_id` |
| `NO` | `EXPLICIT_NOT_RESPONDED` | vacancy-scoped explicit `alreadyResponded=false` / `responseAlreadySent=false` |
| `NO` | `APPLY_ACTION_AVAILABLE` | provider exposes an available response action, with no contradictory disabled-action observation |
| `UNKNOWN` | `AMBIGUOUS_PAGE` | no positive vacancy-scoped response evidence; `CanApply=false`, test, archive, URL, generic markup, or contradictory action state is insufficient |
| `UNKNOWN` | `AUTH_FAILURE` | login, forbidden, CAPTCHA/challenge, or HTTP 401/403 |

`AlreadyRespondedKnown` and the legacy bool are now projections of this
evidence value. A positive bool without evidence is treated as `UNKNOWN`.
`UNKNOWN` is counted separately by pilot search and cannot reach
`READY_FOR_EXPLICIT_SEND`.

## All 40 original fresh YES rows

For each row, `response identifier` means only that the preflight target URL
was constructed/present. It is not response evidence. `negotiation identifier`
is whether the preflight projection exposed a provider negotiation/response
ID; none did.

| Vacancy | Title | Already responded | Evidence | Can apply | Test required | Active | Response identifier | Negotiation identifier |
|---:|---|---|---|---|---|---|---|---|
| 137448342 | Разработчик PaaS-продуктов | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137444477 | Frontend-разработчик (Vue 2/3, Nuxt) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137437860 | Ведущий специалист по созданию AI-агентов | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137437509 | Ведущий администратор информационных систем | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136841654 | Специалист по информационной безопасности | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137435884 | Инженер по разработке | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137433931 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 135815250 | Инженер АСУ ТП | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137428012 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136337829 | Специалист отдела прикладных систем Департамента информационных технологий | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137191871 | Системный администратор Linux-серверов | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137406956 | AQA - инженер (Python) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137384621 | Специалист по тестированию (ручное тестирование) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137157724 | Ведущий системный аналитик | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136398208 | Аналитик данных | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137359058 | Администратор баз данных (SQL DBA) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 117319140 | Администратор баз данных PostgreSQL | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137348279 | Программист ТУРБО ERP | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 135732605 | Тестировщик | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136316916 | Технический специалист / системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137436050 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137203563 | Специалист технической поддержки | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136639686 | Наладчик - программист станков с ЧПУ | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137385088 | Flutter-разработчик | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137380353 | Специалист технической поддержки | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136585470 | Инженер-программист АСУ ТП | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136476946 | Администратор баз данных (SberInfra) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 134439967 | Support Manager / Менеджер поддержки (Games) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136989983 | Инженер технической поддержки | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136335111 | Инженер поддержки | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137194249 | IT Support specialist/Специалист отдела поддержки | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137192135 | Специалист технической поддержки (1 линия) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 136164561 | Ведущий инженер-программист | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137247881 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137244538 | Python-разработчик (в офис) | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137242831 | Специалист бизнес-поддержки Process Mining | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137238398 | Системный аналитик, работа в офисе г. Екатеринбург | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137229291 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137199114 | Помощник системного администратора | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |
| 137187408 | Системный администратор | UNKNOWN | AMBIGUOUS_PAGE | false | true | UNKNOWN | yes | no |

Evidence distribution for the 40 fresh rows: `AMBIGUOUS_PAGE: 40`.

## Independent cross-check sample

The sample used the provider's application/negotiation list directly, not the
vacancy response-page parser. Four provider pages were read until the cursor
chain ended. A `NOT_FOUND` result means no matching provider record with an ID
was observed in that bounded history read; it does not prove that no response
exists if the provider history is incomplete or subject to another visibility
boundary.

| Vacancy | Original preflight | Independent evidence |
|---:|---|---|
| 137448342 Разработчик PaaS-продуктов | YES | NOT_FOUND |
| 137444477 Frontend-разработчик (Vue 2/3, Nuxt) | YES | NOT_FOUND |
| 137437860 Ведущий специалист по созданию AI-агентов | YES | NOT_FOUND |
| 137437509 Ведущий администратор информационных систем | YES | NOT_FOUND |
| 136841654 Специалист по информационной безопасности | YES | NOT_FOUND |
| 137435884 Инженер по разработке | YES | NOT_FOUND |
| 137433931 Системный администратор | YES | NOT_FOUND |
| 135815250 Инженер АСУ ТП | YES | NOT_FOUND |
| 137428012 Системный администратор | YES | NOT_FOUND |
| 136337829 Специалист отдела прикладных систем Департамента информационных технологий | YES | NOT_FOUND |

Confirmed responded: `0`

Not independently found: `10`

Unknown: `0`
False positives found: `YES` — the original 40 YES values were not
evidence-backed by the new scoped classifier; 10/10 sample rows were also not
found in the independent bounded history read. The history caveat prevents
claiming that all 40 definitely lack a response.

## Required regression fixtures

Added tests cover:

- explicit responded marker → `YES`;
- same-vacancy negotiation ID → `YES`;
- available apply action without response marker → `NO`;
- `CanApply=false` without response evidence → `UNKNOWN`;
- test required without response evidence → `UNKNOWN`;
- inactive vacancy without response evidence → `UNKNOWN`;
- login/challenge page → `UNKNOWN/AUTH_FAILURE`;
- generic response wording → `UNKNOWN`;
- response belonging to another vacancy → not `YES`;
- disabled action conflicting with an apply signal → `UNKNOWN`.

Classifier changed: `YES` (ambiguous states fail closed). Router and AI were
not changed. Scan budget was not expanded.

## After fix / revalidation

```text
Scanned: 44
Known responded: 4
Fresh confirmed responded: 0
Preflight unknown: 40
Fresh unresponded: 0
Detail reads: 0
AI evaluations: 0
Pilot candidate: NONE
READY / NONE: NONE
Real HH writes: 0
Application POST: 0
```

Command was run with `--max-scan 100 --max-candidates 20`,
`HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and `STORAGE_BACKEND=json` (the
equivalent storage/dry-run CLI flags were also supplied because this checkout's
`.env` historically takes precedence over process environment values). No HH
POST/PUT/PATCH/DELETE was attempted.
