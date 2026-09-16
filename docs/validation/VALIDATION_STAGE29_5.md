# Validation Stage 29.5 — Resume Router and Shadow shortlist

Дата проверки: 2026-09-16 (Asia/Yekaterinburg)

Проверка выполнена в режиме:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_VACANCIES_PER_RUN=100
```

Реальные HH writes не выполнялись. Были разрешены только HH reads, AI evaluation и локальная запись Shadow report. `out` в этот commit не входит.

## 1. Resume Registry audit

В Registry ровно четыре enabled resume. Нормализация не свела их к одному набору: различия в title и skills сохранены. `Search hints`, `Include keywords` и `Exclude keywords` во всех четырёх фактических HH-профилях сейчас пусты.

| Resume ID | Title / DesiredRole | Skills count | Skills | Search hints | Include keywords | Exclude keywords |
|---|---|---:|---|---|---|---|
| `hh-resume-6aac4e53ff110b3a8e0039ed1f4f5a68684d41` | Специалист по автоматизации и интеграциям / инженер внедрения | 23 | Python; Django Framework; PostgreSQL; SQL; REST API; GitHub; Git; Linux; Docker; Redis; Nginx; TypeScript; JavaScript; React; Node.js; Express; Автоматизация процессов; API-интеграции; Техническая документация; Webhooks; Bitrix24; CRM; Техническая поддержка | — | — | — |
| `hh-resume-9d9a7b3aff10b8e0070039ed1f756941615344` | Backend-разработчик | 29 | Информационные технологии; Техническое обслуживание; Техническая поддержка; Разработка инструкций; Английский язык; Linux; Деловая переписка; Администрирование сайтов; Автоматизация процессов; GitHub; PostgreSQL; Битрикс24; SQL; VueJS; Laravel; 1С-Битрикс; jQuery; Docker; Git; SOAP; Django Framework; Python; React; PHP; HTML; CSS; Tailwind CSS; Redis; Nginx | — | — | — |
| `hh-resume-a89be050ff10a4a4fc0039ed1f786946636470` | Backend-разработчик (Python/Django) / автоматизация и интеграции | 19 | Python; Django Framework; PostgreSQL; SQL; REST API; GitHub; Git; Linux; Docker; Redis; Nginx; TypeScript; JavaScript; React; Node.js; Express; Автоматизация процессов; API-интеграции; Техническая документация | — | — | — |
| `hh-resume-b29ec17dff103a8bc60039ed1f356c62486c37` | Технический специалист | 30 | Точность и внимательность к деталям; Работа с базами данных; Работа с большим объемом информации; Обучение и развитие; Деловое общение; Деловая переписка; Умение работать в коллективе; Электронная почта; Работа с документами; Организаторские навыки; Работа с оргтехникой; Сбор и анализ информации; Оперативный поиск информации в сети Интернет; оформление документации; оформление заказов; Техническая поддержка; Телефонные переговоры; Администрирование сайтов; Техническая поддержка пользователей; Диагностика неисправностей; Установка и настройка ПО; Настройка компьютерного оборудования; Работа с VR-оборудованием; CRM; Git; GitHub; SQL; PostgreSQL; HTML; CSS | — | — | — |

Из этих данных Router выводит identities, не добавляя новые факты:

| Resume | Primary roles | Strong skills / domain signals | Supporting skills | Negative signals |
|---|---|---|---|---|
| Automation / implementation | automation, engineer, implementation, integration, specialist | Python, Django Framework, PostgreSQL, SQL, REST API, Linux, Docker, Redis, Nginx, TypeScript, JavaScript, React, Express, Автоматизация процессов, API-интеграции, Webhooks, Bitrix24, CRM, Техническая поддержка; domains: automation, crm, implementation, integration, support | GitHub, Git, Node.js, Техническая документация | — |
| Backend | backend, developer | Техническая поддержка, Linux, Автоматизация процессов, PostgreSQL, SQL, VueJS, Laravel, Docker, SOAP, Django Framework, Python, React, PHP, HTML, CSS, Tailwind CSS, Redis, Nginx; domains: automation, backend, support | Информационные технологии, Техническое обслуживание, Разработка инструкций, Английский язык, Деловая переписка, Администрирование сайтов, GitHub, Битрикс24, 1С-Битрикс, jQuery, Git | — |
| Python/Django backend | automation, backend, developer, django, integration, python | Python, Django Framework, PostgreSQL, SQL, REST API, Linux, Docker, Redis, Nginx, TypeScript, JavaScript, React, Express, Автоматизация процессов, API-интеграции; domains: automation, backend, integration | GitHub, Git, Node.js, Техническая документация | — |
| Technical specialist | specialist, technical | Техническая поддержка, Техническая поддержка пользователей, CRM, SQL, PostgreSQL, HTML, CSS; domains: crm, support | Остальные подтверждённые HH skills профиля, включая диагностику, установку/настройку ПО, документацию, работу с оборудованием | — |

## 2. Что изменено в scoring

Старый Router складывал пересечения токенов (`skill +18`, role/title token `+12`) и преждевременно делал `min(total, 100)`. На реальных описаниях широкие слова и повторяющийся стек быстро заполняли этот cap, поэтому ranking исчезал.

Новая структура `ResumeScore` содержит `RoleScore`, `SkillScore`, `DomainScore`, `ExperienceScore`, `ProvenanceScore`, bounded `GenericEvidenceScore`, `RawFit`, `NormalizedScore`, `Reasons` и `HardBlockers`. `RawFit` не capped и используется для сортировки и margin; UI score остаётся 0–100 через монотонное сжатие `100 * raw / (raw + 60)`.

Сигналы разделены так:

- role-specific overlap: `+12` за подтверждённый role token; generic role token — `+2` и общий generic signal ограничен `5`;
- exact normalized skill phrase в структурированных полях — `+20`;
- multi-token substantial overlap — `+12`;
- single specific token — `+7`;
- description-only варианты слабее: `+5`, `+4`, `+2` соответственно;
- domain signal — `+8` за canonical domain token;
- seniority/experience fit — отдельный компонент `+8`, только при явном совпадении уровня;
- search provenance — bounded soft signal `+5`, не способный один разрешить близкую неоднозначность;
- exclude keyword — hard blocker.

Generic vocabulary намеренно небольшой и reviewable: `developer`, `specialist`, `technical`, `engineer`, `api`, `backend`. Specific vocabulary содержит подтверждаемые технологии и доменные термины: например `python`, `django`, `postgresql`, `sql`, `rest_api`, `docker`, `crm`, `support`, `automation`, `integration`, `fullstack`, `kafka`.

Matching различает exact phrase, multi-token substantial overlap, single specific token и single generic token. Поэтому `REST API` не становится полноценным exact match от одного `API`, а `Technical Support` не получает полный skill score от одного `technical`.

## 3. Saturation и margins

Сравнение выполнено на том же discovery set через exact old replay и новый Shadow run:

| Диагностика | Stage 29 old | Stage 29.5 new |
|---|---:|---:|
| Vacancies с >=2 resume ровно `score=100` | 24 | 0 |
| Vacancies со всеми четырьмя resume `score=100` | 8 | 0 |

New Router хранит и выводит `top_raw_score`, `second_raw_score`, `top_normalized_score`, `second_normalized_score`, `absolute_margin`, `relative_margin`. `REVIEW_REQUIRED` для близкого выбора проверяется по uncapped fit margin (`fit margin < 10` или relative fit margin `< 12%`), а не через `100 - 100`.

Распределение выбранных resume в финальном Shadow:

| Категория | Resume | N |
|---|---|---:|
| Automation/integrations | Специалист по автоматизации и интеграциям / инженер внедрения | 2 |
| Backend | Backend-разработчик | 3 |
| Python/Django | Backend-разработчик (Python/Django) / автоматизация и интеграции | 7 |
| Technical specialist | Технический специалист | 0 |
| Ambiguous | — | 38 |

## 4. Stage 29 examples: old vs new

В старой колонке приведены top scores после cap. В новой — `top raw / top normalized` и `second raw / second normalized`; выбор не форсировался.

| Vacancy | Old top scores | Old decision | New best / selected | New scores | New decision |
|---|---|---|---|---|---|
| IT-специалист (Cloud, DevOps, AI), `137380411` | Automation 100; Python/Django 100; Technical 100; Backend 90 | REVIEW_REQUIRED | Automation / no deterministic selection | 25/29 vs 15/20 | REVIEW_REQUIRED |
| AI-специалист / Prompt Engineer, `136563592` | Automation 100; Technical 100; Python/Django 78; Backend 54 | REVIEW_REQUIRED | Automation / no deterministic selection | 20/25 vs 7/10 | REVIEW_REQUIRED |
| Инженер второй линии техподдержки / Customer Success L2, `135644599` | Automation 100; Technical 100; Backend 84; Python/Django 72 | REVIEW_REQUIRED | Technical / no deterministic selection | 43/42 vs 41/41 | REVIEW_REQUIRED |
| Аналитик данных, `137252236` | All four 100 | REVIEW_REQUIRED | Automation / no deterministic selection | 29/33 vs 19/24 | REVIEW_REQUIRED |
| BI-аналитик Junior+Middle, `137184982` | Technical 100; Automation 88; Python/Django 60; Backend 36 | REJECT | Automation / no deterministic selection | 9/13 vs 4/6 | REVIEW_REQUIRED |
| Fullstack-разработчик, `137430509` | All four 100 | REVIEW_REQUIRED | Backend / no deterministic selection | 37/38 vs 36/38 | REVIEW_REQUIRED |
| Python Backend Developer, `137428040` | All four 100 | REVIEW_REQUIRED | Python/Django | 79/57 vs 65/52 | REJECT: AI_APPLY_FALSE, AI 45 |
| Инженер технической поддержки второй линии, `137402396` | Technical 100; Automation 88; Backend 36; Python/Django 30 | REJECT | Automation / no deterministic selection | 41/41 vs 38/39 | REVIEW_REQUIRED |

Python Backend Developer теперь получает устойчиво различимый Python/Django top raw score и не становится `REVIEW_REQUIRED` из-за artificial `100/100/100/100`. При этом AI отказал ему с кодом `AI_APPLY_FALSE`, поэтому safety не ослаблялась.

## 5. Automatic real Shadow

| Metric | Value |
|---|---:|
| Raw fetched | 199 |
| Duplicates | 45 |
| Unique | 154 |
| Processed | 154 |
| VACANCY_LIMIT | 100 |
| Entered before per-run limit | 62 |
| Limit-skipped after budget | 92 |
| NEEDS_DETAIL | 50 |
| Detail succeeded | 50 |
| Final routed | 12 |
| Final ambiguous | 38 |
| AI evaluated | 12 |
| AI Apply=true | 4 |
| AI Apply=false | 8 |
| Hard missing | 3 |
| Hard unknown | 3 |
| AI score below threshold | 1 |
| AI MATCH | 0 |
| AI REJECT | 9 |
| AI REVIEW_REQUIRED | 3 |
| Final MATCH | 0 |
| Final REJECT | 9 |
| Final REVIEW_REQUIRED | 41 |
| Would apply | 0 |
| Writes | 0 |
| ACCOUNTING | PASS |

AI-level primary reason aggregation:

```text
AI_REJECT due Apply=false: 5
AI_REJECT due score<threshold: 1
AI_REJECT due hard requirement missing: 3
REVIEW due unknown hard requirement: 3
MATCH: 0
```

`AI Apply=false` как общий input counter равен 8; три из этих случаев имеют более конкретный fail-closed primary code `HARD_REQUIREMENT_MISSING`, поэтому primary reason counts суммируются ровно в 12 evaluations.

### AI_REJECT / AI audit

Для каждого AI evaluation в `career_agent_vacancy` trace сохранены selected resume, `ai_apply`, score, полный `ai_hard_requirements` с локально рассчитанными `MET/MISSING/UNKNOWN`, decision и reason code. Ниже — компактная audit-выборка без chain-of-thought; пустая колонка hard requirements означает, что обязательные требования не были зарегистрированы.

| Vacancy | Selected resume | AI score | Apply | Hard requirements (status: requirement) | Decision | Reason code | Final |
|---|---|---:|---|---|---|---|---|
| `137428040` Python Backend Developer | Python/Django | 45 | false | — | REJECT | AI_APPLY_FALSE | REJECT |
| `137418714` Backend Developer Python (junior) | Python/Django | 75 | true | UNKNOWN: от 1 года; UNKNOWN: FastAPI; UNKNOWN: Python 3.13; MET: PostgreSQL; MET: Docker | REVIEW_REQUIRED | HARD_REQUIREMENT_UNKNOWN | REVIEW_REQUIRED |
| `136422627` TypeScript-разработчик (backend) | Python/Django | 40 | true | MET: TypeScript; UNKNOWN: Nest JS; UNKNOWN: GraphQL; UNKNOWN: CockroachDB; UNKNOWN: Prisma | REJECT | AI_SCORE_BELOW_THRESHOLD | REJECT |
| `137394574` Middle Backend-разработчик с AI-инструментами | Backend | 30 | false | MISSING: 3 года коммерческого опыта web-разработки | REJECT | HARD_REQUIREMENT_MISSING | REJECT |
| `137393619` Стажер-разработчик (PHP) | Backend | 20 | false | UNKNOWN: PHP 8.x; UNKNOWN: ООП; UNKNOWN: SQL | REJECT | AI_APPLY_FALSE | REJECT |
| `137384867` Fullstack-разработчик Web / AI-assisted | Backend | 65 | true | UNKNOWN: Краснодар | REVIEW_REQUIRED | HARD_REQUIREMENT_UNKNOWN | REVIEW_REQUIRED |
| `136958238` Backend-разработчик (Python) | Python/Django | 10 | false | MISSING: от 2 до 5 лет; UNKNOWN: SQLAlchemy; UNKNOWN: Alembic; UNKNOWN: pytest; UNKNOWN: Москва | REJECT | HARD_REQUIREMENT_MISSING | REJECT |
| `136597178` Backend-разработчик (Python / FastAPI) | Python/Django | 30 | false | UNKNOWN: FastAPI; UNKNOWN: SQLAlchemy; UNKNOWN: Redis Streams; UNKNOWN: async; UNKNOWN: JWT/bcrypt | REJECT | AI_APPLY_FALSE | REJECT |
| `136577315` Backend разработчик (Python DRF) | Python/Django | 70 | true | UNKNOWN: Москва (офис) | REVIEW_REQUIRED | HARD_REQUIREMENT_UNKNOWN | REVIEW_REQUIRED |
| `136364927` Senior Backend Developer Python | Python/Django | 30 | false | UNKNOWN: FastAPI; UNKNOWN: Go; UNKNOWN: AI-кодинг; UNKNOWN: Воронеж или remote office | REJECT | AI_APPLY_FALSE | REJECT |
| `136028565` Инженер по внедрению (IT, enterprise) | Automation/integrations | 30 | false | UNKNOWN: Москва или remote office schedule | REJECT | AI_APPLY_FALSE | REJECT |
| `137374216` Интегратор amoCRM / Аналитик | Automation/integrations | 30 | false | MISSING: 2 года опыта работы с amoCRM | REJECT | HARD_REQUIREMENT_MISSING | REJECT |

В этом fresh run AI MATCH не было. В более раннем v2 Shadow на том же кодовом изменении два AI MATCH были, но финальный read-only preflight уже отвечал `ALREADY_RESPONDED`; итоговый final MATCH и `Would apply` всё равно были нулевыми. Для текущего отчёта authoritative является fresh final run выше.

## 6. Unknown ≠ Missing и AI input

Hard-requirement evaluator оставляет отсутствие подтверждённого факта как `UNKNOWN`. `MISSING` появляется только при подтверждённом конфликте; в текущем audit missing — явные требования к длительности опыта (`3 года`, `от 2 до 5 лет`, `2 года amoCRM`). Ошибочных преобразований `UNKNOWN → MISSING` в regression tests и fresh audit не обнаружено.

Добавлен regression test, который строит selected-resume projection и затем фактический vacancy AI prompt: Resume B (`Resume B`, `API, SQL`, `B-specific experience`) присутствует, а Resume A (`Python, Django`, `A-specific experience`) отсутствует. Prompt содержит vacancy description, salary/location/schedule fields, structured requirements/skills и confirmed candidate context в bounded projection.

## 7. Top Shadow shortlist

Это ручной shortlist, не разрешение на отклик. Для stable feedback используются HH `vacancy_id` и Registry `resume_id`; feedback позже можно записать как `GOOD_MATCH`, `BAD_MATCH`, `WRONG_RESUME`, `ACCEPT` или `REJECT`. Feedback пока не подключён обратно к scoring.

`components` записаны как `raw / normalized (role, skill, domain, experience, provenance, generic)`. Для ambiguous строки `best` — лучший кандидат Router, но selected resume не назначен.

| Category | Vacancy / company / stable URL | Selected or best resume | Components; AI | Decision; reason | would_apply |
|---|---|---|---|---|---|
| near-match | `136577315` Backend разработчик (Python DRF), ИдаПроджект, [HH](https://ekaterinburg.hh.ru/vacancy/136577315) | Python/Django | `80/57 (16,49,8,0,5,2)`; AI 70, Apply=true; UNKNOWN Москва (офис) | REVIEW_REQUIRED; HARD_REQUIREMENT_UNKNOWN | false |
| near-match | `137418714` Backend Developer Python (junior), Онлайн-школа Тетрика, [HH](https://ekaterinburg.hh.ru/vacancy/137418714) | Python/Django | `71/54 (16,39,8,0,5,3)`; AI 75, Apply=true; UNKNOWN от 1 года/FastAPI/Python 3.13 | REVIEW_REQUIRED; HARD_REQUIREMENT_UNKNOWN | false |
| near-match | `137428040` Python Backend Developer, Miles&Miles, [HH](https://ekaterinburg.hh.ru/vacancy/137428040) | Python/Django | `79/57 (16,47,8,0,5,3)`; AI 45, Apply=false | REJECT; AI_APPLY_FALSE | false |
| interesting REVIEW | `137420269` Специалист технической поддержки (2 линия), Чиббис, [HH](https://ekaterinburg.hh.ru/vacancy/137420269) | best Technical specialist; ambiguous | `56/48 (4,37,8,0,5,2)`; AI not evaluated | REVIEW_REQUIRED; raw margin 9 | false |
| interesting REVIEW | `137433434` Специалист технической поддержки (Helpdesk), Finstar Financial Group, [HH](https://ekaterinburg.hh.ru/vacancy/137433434) | best Technical specialist; ambiguous | `51/46 (4,32,8,0,5,2)`; AI not evaluated | REVIEW_REQUIRED; raw margin 14, fit remains close | false |
| interesting REVIEW | `135644599` Инженер второй линии техподдержки / Customer Success L2, ООО Клеверенс Софт, [HH](https://ekaterinburg.hh.ru/vacancy/135644599) | best Technical specialist; ambiguous | `43/42 (2,27,8,0,5,1)`; AI not evaluated | REVIEW_REQUIRED; raw margin 2 | false |
| interesting REVIEW | `137402396` Инженер технической поддержки второй линии, ООО КОМПАНИЯ РДМ, [HH](https://ekaterinburg.hh.ru/vacancy/137402396) | best Automation/integrations; ambiguous | `41/41 (2,24,8,0,5,2)`; AI not evaluated | REVIEW_REQUIRED; raw margin 3 | false |
| interesting REVIEW | `137430509` Fullstack-разработчик, ООО Трианон, [HH](https://ekaterinburg.hh.ru/vacancy/137430509) | best Backend; ambiguous | `37/38 (2,27,0,0,5,3)`; AI not evaluated | REVIEW_REQUIRED; raw margin 1 | false |
| interesting REVIEW | `137252236` Аналитик данных, ООО Эво, [HH](https://ekaterinburg.hh.ru/vacancy/137252236) | best Automation/integrations; ambiguous | `29/33 (0,24,0,0,5,0)`; AI not evaluated | REVIEW_REQUIRED; raw margin 10 | false |
| interesting REVIEW | `137384867` Fullstack-разработчик Web / AI-assisted, MODULDOM ЮГ, [HH](https://ekaterinburg.hh.ru/vacancy/137384867) | Backend | `36/38 (2,27,0,0,5,2)`; AI 65, Apply=true; UNKNOWN Краснодар | REVIEW_REQUIRED; HARD_REQUIREMENT_UNKNOWN | false |

В списке нет MATCH-категории, потому что в fresh run не было AI MATCH и final MATCH. Это результат данных и safety gate, а не искусственное понижение порога.

## 8. Tests and smoke

PASS:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
node --check web/app.js
go test ./internal/runtime -run 'TestDashboard' -count=1
```

Dashboard/API smoke tests прошли, включая GET `/api/dashboard` и основные `/api/*` read endpoints. Тесты не выполняли реальные HH writes и не требовали реальных HH cookies.

## Ответы на вопросы прохода

1. Старый Router выдавал много `score=100`, потому что capped сумма широких token overlaps уничтожала порядок после нескольких совпадений.
2. Scoring разделён на role, specific skill, domain, experience, provenance, bounded generic и blockers; ranking использует uncapped `RawFit`.
3. `>=2 score=100`: 24 → 0; all-four `100`: 8 → 0.
4. В новом run выбираются Python/Django: 7, Backend: 3, Automation/integrations: 2; technical specialist: 0; 38 остаются ambiguous.
5. AI отклонял из-за `Apply=false` (primary 5), score below threshold (1) и подтверждённых hard missing (3); ещё 3 ушли в review из-за unknown hard requirements.
6. Ошибочных `UNKNOWN→MISSING` не обнаружено.
7. В fresh run MATCH не появились; `MATCH=0` принят как корректный результат.
8. Лучшие кандидаты перечислены в top-10 выше; наиболее перспективные для ручной проверки — Python DRF, junior Python backend и support/L2 вакансии с близкими resume margins.
9. HH writes: 0.
