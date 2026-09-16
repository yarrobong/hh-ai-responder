# Stage 29 — real HH validation

Дата: 2026-09-16 (Asia/Yekaterinburg).

Validation выполнена после пересборки binary из `HEAD`:

```text
195f777353b50edff990d7145345180bb908ed86
```

`origin/main` совпадает с этим SHA. Существующий untracked `out` сохранён.

## Safety boundary

Оба real-run выполнены с:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false
```

Использованы только HH GET/read paths: doctor, search, vacancy detail,
profile/resume reads, preflight reads и AI evaluation. Applications, messages,
tests, resume touch и job-status writes не выполнялись.

`career-agent --shadow` сам принудительно удерживает `DryRun=true`,
`HHWriteEnabled=false`, `AutoChat=false`, `AutoTouch=false` и
`AutoJobStatus=false`.

Первый automatic запуск с текущим `.env` остановился до HH из-за локального
`STORAGE_BACKEND=postgres`: database `hh_ai_responder_s3` отсутствует. Это не
HH/auth failure. Для real validation был повторён тот же pipeline с явным
`-storage-backend json`; architecture, OAuth и transport layer не менялись.

## HH doctor

Пересобрано:

```text
go build -o ./hh-ai-responder ./cmd/hh-ai-responder
```

`./hh-ai-responder -h` показывает application flags; command help для
`hh-doctor` присутствует в исходном CLI dispatcher. Фактический запуск:

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false ./hh-ai-responder hh-doctor
```

Результат: `AUTHENTICATED_READ_OK`.

- Public HH read: `GET /search/vacancy` → HTTP 200.
- Authenticated profile read: `GET /applicant/my_resumes` → HTTP 200.
- Cookie file: found, Netscape format, parsed successfully.
- HH writes attempted: 0.

## Real Shadow commands

Automatic planner (generated profiles, explicit `-u ""`):

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false ./hh-ai-responder -u "" \
  -storage-backend json career-agent --shadow
```

Manual/legacy profiles (configured `HH_SEARCH_URL(S)`):

```text
HH_DRY_RUN=true HH_WRITE_ENABLED=false ./hh-ai-responder \
  -storage-backend json career-agent --shadow
```

Both runs completed successfully against real HH reads. `ACCOUNTING CHECK` is
`PASS` and `shadow_write_count` is `0` in both reports.

## Automatic planner — real counters

The current `.env` has `HH_MAX_VACANCIES_PER_RUN=50`. Discovery still accounts
for every unique vacancy; 93 unique vacancies received the explicit terminal
`VACANCY_LIMIT` after the run budget was reached.

| Counter | Value |
|---|---:|
| Raw | 199 |
| Duplicates | 46 |
| Unique | 153 |
| Already responded | 13 |
| Obvious deterministic reject | 0 |
| Preliminary `CLEAR_ROUTE` | 0 |
| Preliminary `NEEDS_DETAIL` | 50 |
| Detail requested | 50 |
| Detail succeeded | 50 |
| Detail failed | 0 |
| Final routed | 20 |
| Final `REVIEW_REQUIRED` | 30 |
| AI evaluated | 17 |
| AI rejected | 14 |
| AI matched | 0 |
| `MATCH` | 0 |
| `REJECT` | 17 |
| `REVIEW_REQUIRED` | 30 |
| Would apply | 0 |
| Shadow writes | 0 |
| ACCOUNTING CHECK | PASS |

Terminal outcomes: `AI_REJECT=14`, `ALREADY_RESPONDED=13`,
`DETAIL_NOT_REQUIRED=3`, `REVIEW_REQUIRED=30`, `VACANCY_LIMIT=93`; total
terminal records `153`, exactly equal to unique discovery IDs.

## Manual/legacy Shadow — real counters

| Counter | Value |
|---|---:|
| Raw | 79 |
| Duplicates | 36 |
| Unique | 43 |
| Already responded | 7 |
| Obvious deterministic reject | 0 |
| Preliminary `CLEAR_ROUTE` | 0 |
| Preliminary `NEEDS_DETAIL` | 40 |
| Detail requested | 40 |
| Detail succeeded | 40 |
| Detail failed | 0 |
| Final routed | 26 |
| Final `REVIEW_REQUIRED` | 15 |
| AI evaluated | 18 |
| AI rejected | 13 |
| AI matched | 0 |
| `MATCH` | 0 |
| `REJECT` | 21 |
| `REVIEW_REQUIRED` | 15 |
| Would apply | 0 |
| Shadow writes | 0 |
| ACCOUNTING CHECK | PASS |

## Stage 29 experiment

Automatic planner funnel:

```text
NEEDS_DETAIL 50
  → successful detail fetch 50
  → clear final resume route 20
  → genuine final REVIEW_REQUIRED 30
```

All 50 preliminary routes requiring detail received successful detail. Twenty
vacancies then got a deterministic selected resume route. Thirty remained
`REVIEW_REQUIRED` because the full detail still left the top resume scores too
close (`ROUTE_AMBIGUOUS_AFTER_DETAIL`). AI was called only after a selected
resume route and successful detail/preparation; 17 cases reached AI.

The 93 `VACANCY_LIMIT` records are not counted as final review: they were
explicitly terminalized by the configured run budget before final routing.

## Stage 28 comparison

Stage 28 baseline from `VALIDATION_STAGE28.md`:

| Metric | Stage 28 | Stage 29 automatic |
|---|---:|---:|
| Unique | 105 | 153 |
| Detail fetched | 14 | 50 succeeded / 50 requested |
| AI evaluated | 4 | 17 |
| Final `REVIEW_REQUIRED` | 82 | 30 |
| Review rate over unique | 82/105 = 78.1% | 30/153 = 19.6% |

Because the current Stage 29 run budget terminalized 93 unique vacancies as
`VACANCY_LIMIT`, the comparable post-limit final-routing denominator is 60:
`30/60 = 50.0%`. The headline `30/153` is the rate over all unique discovery
records; the post-limit rate is shown to avoid hiding the configured budget
effect.

## Ten real automatic routing examples

`detail evidence` lists structured fields present in the successful HH detail
read. Alternative scores are abbreviated as `resume title: score`.

| Vacancy | Title / company | Selected resume | Alternative scores | Preliminary | Detail evidence | Final route | Confidence | AI | Final |
|---:|---|---|---|---|---|---|---|---|---|
| [137402396](https://ekaterinburg.hh.ru/vacancy/137402396) | Инженер технической поддержки второй линии (r-keeper & iiko) / ООО КОМПАНИЯ РДМ | Технический специалист | Тех. 100; Авто 88; Backend 36; Py/Django 30 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_SELECTED | HIGH | yes, AI 20 | REJECT |
| [137380411](https://ekaterinburg.hh.ru/vacancy/137380411) | IT-специалист (Cloud, DevOps, AI) / ИП Purinvest | — | Авто 100; Py/Django 100; Тех. 100; Backend 90 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [136764415](https://ekaterinburg.hh.ru/vacancy/136764415) | Программист робототехнических комплексов / ООО ИнКрафт | Автоматизация и интеграции | Авто 100; Py/Django 72; Тех. 70; Backend 66 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_SELECTED | HIGH | yes, AI 10 | REJECT |
| [136563592](https://ekaterinburg.hh.ru/vacancy/136563592) | AI-специалист / Prompt Engineer / ИП Елисеев Максим Анатольевич | — | Авто 100; Тех. 100; Py/Django 78; Backend 54 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [135644599](https://ekaterinburg.hh.ru/vacancy/135644599) | Инженер второй линии технической поддержки / ООО Клеверенс Софт | — | Авто 100; Тех. 100; Backend 84; Py/Django 72 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [137252236](https://ekaterinburg.hh.ru/vacancy/137252236) | Аналитик данных / ООО Эво | — | Авто 100; Backend 100; Py/Django 100; Тех. 100 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [137184982](https://ekaterinburg.hh.ru/vacancy/137184982) | BI-аналитик Junior+\\Middle / ПАО «Газпром нефть» ИТ | Технический специалист | Тех. 100; Авто 88; Py/Django 60; Backend 36 | NEEDS_DETAIL | title, description, roles, experience, employment, work format, location | ROUTE_SELECTED | HIGH | yes, AI 20 | REJECT |
| [137430509](https://ekaterinburg.hh.ru/vacancy/137430509) | Fullstack-разработчик / ООО Трианон | — | Авто 100; Backend 100; Py/Django 100; Тех. 100 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [134160545](https://ekaterinburg.hh.ru/vacancy/134160545) | Разработчик (Medical Imaging) / ООО ГравиЛинк | — | Backend 64; Py/Django 54; Тех. 40; Авто 30 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |
| [137428040](https://ekaterinburg.hh.ru/vacancy/137428040) | Python Backend Developer / Miles&Miles | — | Авто 100; Backend 100; Py/Django 100; Тех. 100 | NEEDS_DETAIL | title, description, roles, experience, schedule, employment, work format, location, salary | ROUTE_AMBIGUOUS_AFTER_DETAIL | LOW | no | REVIEW_REQUIRED |

## Pilot candidates

No `MATCH` and no `would_apply` candidates were produced in either real Shadow
run. Therefore there are no top-five pilot candidates and no cover-letter
previews to claim or send.

## Verification status

The full requested local suite was run after this report update:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
node --check web/app.js
```

Final results are reported in the handoff. No architecture, OAuth or transport
layer changes were made.

Real HH writes: **0**. Shadow writes: **0**.
