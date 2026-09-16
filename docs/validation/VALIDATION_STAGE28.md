# Stage 28 — Career Agent pipeline validation

Дата проверки: 2026-09-16 (Asia/Yekaterinburg).

Цель прохода — доказать полноту discovery accounting, корректный порядок
cheap-filter → detail → evaluation, реальный resume routing и отсутствие HH
write-операций в Shadow Mode.

## Git

- Before SHA: `692af6cd5be868ac6169a97b85dfc533e77dd11d`
- After SHA (implementation commit): `e88015c`.
- Validation report commit: `d3aa8925147ce75b7db02963ad717b552e692222`.
- `origin/main` verified after validation push: `d3aa8925147ce75b7db02963ad717b552e692222`.
- A final report-metadata follow-up commit may advance both `HEAD` and
  `origin/main`; the final exact values are also printed in the handoff.
- Force push не использовался.
- Существующий untracked `out` не добавлялся и не изменялся.

## Discovery Accounting

В runtime добавлен terminal record на каждую unique vacancy. Record содержит
компактные structured reasons, а не chain-of-thought. Проверяется точное
соответствие множества unique vacancy IDs множеству terminal records и
валидность terminal outcome.

### Automatic planner, real HH read, Shadow Mode

Источник: `/tmp/stage28_auto_final3.json`, создано с
`HH_DRY_RUN=true HH_WRITE_ENABLED=false`.

| Stage | Count |
|---|---:|
| Raw | 108 |
| Duplicates | 3 |
| Unique | 105 |
| Processed | 105 |
| Terminal total | 105 |
| AI evaluated | 4 |
| Resume routed | 14 |
| MATCH | 0 |
| REJECT | 13 |
| REVIEW_REQUIRED | 82 |
| Would apply | 0 |
| Shadow writes | 0 |

Terminal outcomes:

| Outcome | Count |
|---|---:|
| `ALREADY_RESPONDED` | 9 |
| `DETERMINISTIC_REJECT` | 0 |
| `DETAIL_FETCH_FAILED` | 1 |
| `DETAIL_NOT_REQUIRED` | 9 |
| `AI_REJECT` | 4 |
| `AI_MATCH` | 0 |
| `REVIEW_REQUIRED` | 82 |
| `VACANCY_LIMIT` | 0 |
| `APPLICATION_LIMIT` | 0 |
| `ERROR` | 0 |
| `ATTEMPT_BLOCKED` | 0 |
| **TOTAL** | **105** |

**ACCOUNTING CHECK: PASS** — `9 + 1 + 9 + 4 + 82 = 105 unique`.

Legacy/manual run также прошёл accounting:

| Raw | Duplicates | Unique | Terminal | AI | MATCH | REJECT | REVIEW | Writes |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 82 | 38 | 44 | 44 | 4 | 0 | 6 | 33 | 0 |

## Stage accounting and reason

Automatic run:

| Stage | Entered | Exited | Reasons |
|---|---:|---:|---|
| attempt gate | 105 | 105 | clear: 105 |
| cheap filters | 105 | 105 | already responded: 9; passed: 96 |
| resume routing | 96 | 96 | selected: 14; close scores → review: 82 |
| detail fetch | 14 | 14 | OK: 13; failed: 1 |
| AI evaluation | 4 | 4 | completed: 4 |

Последовательность исправлена так, чтобы отсутствие поля в search-card не
сразу превращало вакансию в REVIEW_REQUIRED: после cheap safe reject
выполняется detail read, затем deterministic enrichment и AI evaluation.
Очевидные rejects и уже отвеченные вакансии не требуют detail read.

Причина старого `AI evaluated: 1`: прежняя оркестрация имела только coarse
счётчики и могла выйти из цикла на vacancy/application limit; вакансии после
границы не получали отдельного результата. Среди обработанных только одна
вакансия прошла старую цепочку до AI, остальные останавливались раньше или
были скрыты пределом. Теперь каждая такая остановка видна как terminal outcome.

## Search Planner

В automatic run было 4 enabled resume и 6 generated profiles. Профили не
добавлялись искусственно сверх лимита; planner теперь round-robin-ит очереди
enabled resume, чтобы малый budget не принадлежал только первому resume.

| Resume | Search query/profile | Reason | Enabled | Results | Unique contribution |
|---|---|---|---|---:|---:|
| Automation / integrations | `Специалист по автоматизации и интеграциям инженер внедрения` | resume role/title | yes | 7 | 5 exclusive |
| Backend | `Backend-разработчик` | resume role/title | yes | 20 | 19 exclusive |
| Python/Django | `Backend-разработчик (Python Django) автоматизация и интеграции` | resume role/title | yes | 1 | 0 exclusive |
| Technical specialist | `Технический специалист` | resume role/title | yes | 80 | 78 exclusive |
| Automation / integrations | `Full-stack Developer Automation Engineer` | candidate role signal | yes | 0 | 0 |
| Backend | `Full-stack Developer Automation Engineer` | candidate role signal | yes | 0 | 0 |

Вклад не суммируется как raw: 3 вакансии встретились в нескольких profiles,
поэтому `7 + 20 + 1 + 80 = 108 raw`, а после dedup осталось 105.

Manual `HH_SEARCH_URL(S)` сохранён. В manual run фактически использованы три
explicit profiles, каждый с reason `explicit HH_SEARCH_URL or HH_SEARCH_URLS
profile`; их queries были Python/Django/backend, automation/integrations/API и
technical/application support. Generated и manual profiles проходят общий
vacancy-ID dedup.

## Resume Router

Automatic real dataset показал выбор двух разных enabled resume:

- `hh-resume-6aac4e53...` — Automation / integrations: 9 selected;
- `hh-resume-b29ec17d...` — Technical specialist: 5 selected.

Примеры сравнения:

| Vacancy | Selected resume | Selected score | Alternative evidence | Confidence | Decision |
|---:|---|---:|---|---|---|
| 137402396 | Automation / integrations | 12 | Backend 0; Python/Django 0; Technical 0 | MEDIUM | selected, later AI_REJECT |
| 137252236 | Technical specialist | 18 | Automation 0; Backend 0; Python/Django 0 | MEDIUM | selected, then deterministic reject |
| 136811568 | Technical specialist | 24 | Automation 12; Backend 0; Python/Django 0 | MEDIUM | selected, then deterministic reject |

Для остальных 82 вакансий top scores были слишком близкими, поэтому router
вернул `REVIEW_REQUIRED`; высокий semantic score не используется как override
для hard incompatibility. Explicit `ExcludeKeywords` дают hard blockers и
исключают resume из выбора. Disabled resume не является candidate. Отдельный
тест подтверждает, что location в resume не интерпретируется как доказательство
отказа от relocation.

## Router → application preparation

`internal/runtime/career_agent_pipeline_test.go` — integration regression test:

`Vacancy → router selects Resume B → Prepare receives hash-b → cover letter
receives Resume B projection → preflight is read → submission request contains
hash-b`.

Тест падает, если application path снова возьмёт старый global
`HH_RESUME`. В real Shadow report `resume_candidates`, `selected_resume` и
`resume_confidence` сохраняются для каждой vacancy.

## Shadow Safety

Shadow Mode принудительно выставляет `DryRun=true`, `HHWriteEnabled=false`,
отключает chat/touch/job-status и удерживает это на orchestration boundary,
даже если входные flags конфликтуют (`HH_WRITE_ENABLED=true`,
`HH_AUTO_APPLY=true`).

Evidence:

- fake HTTP transport test падает на любом non-GET HH request;
- real automatic run: `Shadow writes: 0`;
- real manual run: `Shadow writes: 0`;
- `application_preview` допустим как preview event и не является HH write.

## Canary Simulation

Реальный Canary не запускался. Проверялась только fake/integration логика:

- `configureCareerAgentMode` caps: максимум 1 write/run, 3 writes/day и 1
  application/run;
- chat, resume touch и job-search status в Canary отключены;
- общий pipeline до write decision тот же, что в Shadow;
- `applicationsubmission` tests подтверждают single executor call,
  preflight-before-submit, блокировку duplicate/already-responded, stale
  resume/test и отсутствие retry для delivery-uncertain/unknown outcome;
- reconciliation остаётся обязательным для unresolved attempt.

Ни одного реального Canary application не выполнялось.

## Shadow report

JSON report v2 содержит для каждой из 105/44 unique vacancies поля:

`vacancy_id`, `title`, `company`, `url`, `found_by_profiles`,
`cheap_filter_result`, `cheap_filter_reasons`, `detail_fetch_status`,
`ai_evaluated`, `ai_score`, `ai_reasons`, `resume_candidates`,
`selected_resume`, `resume_confidence`, `final_decision`, `would_apply`,
`blocked_reason`, `cover_letter_generated`, `terminal_outcome`,
`processed_at`.

Рядом с JSON создаётся человекочитаемый `.md` report с aggregate summary,
terminal breakdown, planner table и vacancy outcome table. AI reasons
сохраняются компактно; chain-of-thought не сохраняется.

## Feedback

`store_test.go` проверяет end-to-end round-trip для `GOOD_MATCH`, `BAD_MATCH`,
`WRONG_RESUME`, `ACCEPT`, `REJECT`: feedback сохраняется, читается обратно,
содержит vacancy ID и resume ID, имеет стабильный уникальный ID и idempotent
повторную запись. Feedback не меняет Candidate Knowledge и пока не обучает
ranking автоматически.

## HH validation boundary

- Real HH reads: один automatic planner Shadow run и один legacy/manual Shadow
  run; search cards, vacancy detail/preflight, resume/profile state и AI
  evaluation были read-only.
- Fixture/mock: router/application propagation, shadow non-GET guard,
  accounting edge cases, canary caps, submission/reconciliation safety и
  feedback tests.
- Real HH writes: **0** — не выполнялись applications, chats, tests, resume
  touch или job-search-status writes.
- `./start.sh web smoke` не объявлялся passed; direct smoke выполнялся через
  `go run ... web` и `GET /api/dashboard` с JSON storage backend. JSON response
  успешно распарсился. Запуск с текущим `.env` отдельно остановился из-за
  отсутствующей локальной PostgreSQL database `hh_ai_responder_s3`.

## Verification

| Command | Result |
|---|---|
| `gofmt -w .` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| direct dashboard/API smoke (`GET /api/dashboard`) | PASS with JSON storage |

No `.env`, `cookies.txt`, tokens, credentials, session data or `out` were
added to the commit.
