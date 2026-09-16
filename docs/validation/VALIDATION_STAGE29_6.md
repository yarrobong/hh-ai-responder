# Validation Stage 29.6 — AI advisory calibration

Дата: 2026-09-16 (Asia/Yekaterinburg)

Оба запуска выполнялись в безопасном режиме. Authoritative run использовал
auto-generated multi-resume scope, потому что manual `HH_SEARCH_URL` из `.env`
давал только 43 unique vacancy.

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_VACANCIES_PER_RUN=100
STORAGE_BACKEND=json
```

Ниже нет cookies, токенов, private session data или chain-of-thought.

## 1. Decision contract

Контракт теперь разделён логически, даже если transport остаётся одним JSON:

| Layer | Что делает | Что не делает |
|---|---|---|
| Extraction | извлекает hard requirement candidates, category и короткое vacancy evidence | не выставляет status кандидата |
| Assessment | выдаёт bounded `score`, `strong_match`, `missing`, `recommendation` и `recommendation_reasons` | не разрешает и не запрещает отклик |
| Local policy | проверяет trusted candidate facts, hard status, threshold и preflight | не интерпретирует свободный текст как blocker |

`apply` сохранён в JSON для backward compatibility. В новом контракте он
advisory/legacy counter и не может один дать `REJECT`. Старый JSON без
`recommendation` принимается и нормализуется из `apply`, но false становится
только advisory `DO_NOT_APPLY`.

Local precedence:

```text
hard MISSING       -> REJECT / HARD_REQUIREMENT_MISSING
score < 65         -> REJECT / FIT_SCORE_BELOW_THRESHOLD
hard UNKNOWN       -> REVIEW_REQUIRED / HARD_REQUIREMENT_UNKNOWN
recommendation != APPLY -> REVIEW_REQUIRED / AI_ADVISORY_CONCERN
otherwise          -> MATCH / MATCH_CONFIRMED
```

`OTHER` не является terminal reject. Свободные `reasons` и `missing` не
управляют safety decision.

## 2. Stage 29.5 Apply=false audit

В Stage 29.5 все восемь `Apply=false` имели old final `REJECT`. В новом replay
использованы те же vacancy IDs; new AI output получен на том же auto-generated
scope. Где outcome всё ещё REJECT, это уже объясняется локальным blocker.

| Vacancy | Selected resume | Old score/apply | Old final | New score | New recommendation/reason | Hard MISSING | Hard UNKNOWN | New final | Why |
|---|---|---:|---|---:|---|---|---|---|---|
| [137428040 Python Backend Developer](https://ekaterinburg.hh.ru/vacancy/137428040) | Python/Django | 45 / false | REJECT | 65 | UNCERTAIN / SENIORITY_GAP, HARD_REQUIREMENT | — | — | REVIEW_REQUIRED | AI advisory concern; no structured hard blocker |
| [137393619 Стажер-разработчик (PHP)](https://ekaterinburg.hh.ru/vacancy/137393619) | Backend | 20 / false | REJECT | 78 | APPLY / SENIORITY_GAP, LOCATION_CONCERN, LOW_OVERALL_FIT | — | — | REJECT | already responded in read-only preflight |
| [136597178 Backend Python/FastAPI](https://ekaterinburg.hh.ru/vacancy/136597178) | Python/Django | 30 / false | REJECT | 55 | UNCERTAIN / STACK_MISMATCH, SENIORITY_GAP | — | FastAPI; SQLAlchemy; Redis Streams; JWT; PostgreSQL schema; Docker Compose | REJECT | score below 65 |
| [136364927 Senior Backend Developer Python](https://ekaterinburg.hh.ru/vacancy/136364927) | Python/Django | 30 / false | REJECT | 65 | UNCERTAIN / SENIORITY_GAP, HARD_REQUIREMENT | — | — | REVIEW_REQUIRED | advisory concern; no verified hard blocker |
| [136028565 Инженер по внедрению](https://ekaterinburg.hh.ru/vacancy/136028565) | Automation/integrations | 30 / false | REJECT | 30 | DO_NOT_APPLY / LOCATION_CONCERN, SENIORITY_GAP | — | Москва (офис) | REJECT | score below 65 |
| [137394574 Middle Backend с AI](https://ekaterinburg.hh.ru/vacancy/137394574) | Backend | 30 / false | REJECT | 65 | UNCERTAIN / SENIORITY_GAP, STACK_MISMATCH | 3 года коммерческого опыта web-разработки | — | REJECT | confirmed hard experience mismatch |
| [136958238 Backend-разработчик Python](https://ekaterinburg.hh.ru/vacancy/136958238) | Python/Django | 10 / false | REJECT | 68 | UNCERTAIN / SENIORITY_GAP, STACK_MISMATCH | от 2 лет | — | REJECT | confirmed generic duration mismatch |
| [137374216 Интегратор amoCRM](https://ekaterinburg.hh.ru/vacancy/137374216) | Automation/integrations | 30 / false | REJECT | 65 | UNCERTAIN / SENIORITY_GAP, HARD_REQUIREMENT | 2 года опыта с amoCRM | — | REJECT | confirmed role-specific experience mismatch |

### 137428040: what is and is not known

В Stage 29.5 output был `score=45`, `apply=false`, `hard_requirements=[]`,
`reasons=[]`, `missing=[]`. Поэтому постфактум установить, почему именно
модель выбрала 45/false, нельзя: structured evidence отсутствовала. Это
зафиксировано как unexplained legacy veto, а не заменено придуманным
объяснением.

В новом contract/replay модель вернула `65`, `UNCERTAIN` и bounded reasons
`SENIORITY_GAP`, `HARD_REQUIREMENT`; hard lists пусты. Local result —
`REVIEW_REQUIRED / AI_ADVISORY_CONCERN`, то есть старое бинарное вето убрано.

## 3. Calibration replay dataset

Dataset содержит 25 real read-only vacancies из authoritative Shadow run:
16 дошли до AI, 9 остановились раньше из-за deterministic ambiguous resume
route. Это один и тот же набор для old/new comparison; для 9 pre-AI rows
сравнивался route outcome, AI score для них корректно указан как `n/a`.

Resume IDs:

```text
automation = hh-resume-6aac4e53ff110b3a8e0039ed1f4f5a68684d41
backend    = hh-resume-9d9a7b3aff10b8e0070039ed1f756941615344
python     = hh-resume-a89be050ff10a4a4fc0039ed1f786946636470
technical  = hh-resume-b29ec17dff103a8bc60039ed1f356c62486c37
```

Bounded confirmed facts used by the replay are the selected registry resume
title/skills, exact total-experience-known flag/months, candidate location and
the selected resume's safe candidate projection. Contacts, names and storage
handles are excluded. Absence of a skill is never encoded as a negative fact.

| ID / title | Relevant vacancy evidence | Selected/best resume | AI score | Old local result | New local result |
|---|---|---|---:|---|---|
| 137436275 Middle backend Python | Python; web development; 3+ years; ClickHouse; ETL | python | 55 | REJECT | REJECT / HARD_REQUIREMENT_MISSING |
| 137428040 Python Backend Developer | Python/Django; PostgreSQL; REST API; remote | python | 65 | MATCH | REVIEW / AI_ADVISORY_CONCERN |
| 137418714 Backend Python junior | Python; FastAPI; Python 3.13; from 1 year | python | 78 | REVIEW | REVIEW / HARD_REQUIREMENT_UNKNOWN |
| 136422627 TypeScript backend | TypeScript; Nest JS; GraphQL | python | 65 | REVIEW | REVIEW / HARD_REQUIREMENT_UNKNOWN |
| 137394574 Middle Backend AI | 3 years commercial web development | backend | 65 | REJECT | REJECT / HARD_REQUIREMENT_MISSING |
| 137393619 PHP intern | PHP; internship; Екатеринбург | backend | 78 | MATCH | REJECT / ALREADY_RESPONDED |
| 137384867 Fullstack AI-assisted | fullstack; web; AI-assisted development | backend | 65 | MATCH | REVIEW / AI_ADVISORY_CONCERN |
| 136958238 Backend Python | from 2 years; SQLAlchemy; Alembic; pytest | python | 68 | REJECT | REJECT / HARD_REQUIREMENT_MISSING |
| 136597178 Backend Python FastAPI | FastAPI; SQLAlchemy; Redis Streams; JWT | python | 55 | REJECT | REJECT / FIT_SCORE_BELOW_THRESHOLD |
| 136577315 Backend Python DRF | Python/DRF; Москва; office format | python | 75 | REVIEW | REVIEW / HARD_REQUIREMENT_UNKNOWN |
| 136364927 Senior Backend Python | senior; FastAPI; Go; AI coding | python | 65 | MATCH | REVIEW / AI_ADVISORY_CONCERN |
| 136028565 Enterprise implementation | Москва; office; implementation | automation | 30 | REJECT | REJECT / FIT_SCORE_BELOW_THRESHOLD |
| 137374216 amoCRM analyst | amoCRM; 2 years role experience | automation | 65 | REJECT | REJECT / HARD_REQUIREMENT_MISSING |
| 137312748 Technical support analyst | technical support; analytics; location/schedule | technical | 30 | REJECT | REJECT / FIT_SCORE_BELOW_THRESHOLD |
| 137251207 Integrations and AI automation | integrations; AI automation; seniority concern | automation | 45 | REJECT | REJECT / FIT_SCORE_BELOW_THRESHOLD |
| 136236390 Lead technical support | higher technical/IT education; 2+ years IT | technical | 55 | REJECT | REJECT / FIT_SCORE_BELOW_THRESHOLD |
| 137402396 L2 support r_keeper/iiko | support; r_keeper; iiko | best automation (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 137380411 IT Cloud/DevOps/AI | Cloud; DevOps; AI; support/engineering | best automation (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 136563592 Prompt Engineer | AI; prompt engineering; neural networks | best automation (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 135644599 Customer Success L2 | second-line support; customer success | best technical (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 137252236 Data analyst | data analysis; BI/data signals | best automation (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 137184982 BI analyst Junior+Middle | BI; analytics; junior/middle | best automation (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 137430509 Fullstack developer | fullstack; backend/frontend overlap | best backend (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 134160545 Medical Imaging developer | medical imaging; developer | best backend (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |
| 134881609 Backend developer | backend; developer; mixed stack | best backend (not selected) | n/a | REVIEW | REVIEW / ROUTE_AMBIGUOUS |

For the 16 AI rows, “Old” is the pre-29.6 local contract replay over the same
AI response (`missing -> apply=false -> low score -> unknown -> match`), not a
second random vacancy sample. The separate Stage 29.5 eight-row table above
preserves the historical old output audit.

### Confusion table (16 AI rows)

| Old \ New | REJECT | REVIEW_REQUIRED | MATCH |
|---|---:|---:|---:|
| REJECT | 9 | 0 | 0 |
| REVIEW_REQUIRED | 0 | 3 | 0 |
| MATCH | 1 | 3 | 0 |

Changed cells have concrete causes:

- `MATCH -> REVIEW_REQUIRED`: advisory `UNCERTAIN` with no hard blocker
  (`137428040`, `137384867`, `136364927`).
- `MATCH -> REJECT`: fresh preflight `ALREADY_RESPONDED` (`137393619`).
- `REJECT -> REVIEW_REQUIRED` in the historical eight-row comparison:
  `137428040` and `136364927`; both old rejects were attributable only to the
  old/ambiguous AI veto, and neither has a local hard blocker in the replay.

No decision changed from `REJECT` to `MATCH`.

## 4. Location and experience semantics

Location handling now distinguishes vacancy area from mandatory workplace:

- area/city alone is not a hard requirement;
- explicit office/onsite in another city is `UNKNOWN` unless relocation is a
  trusted candidate fact;
- remote vacancy, including “hybrid / can work remotely”, creates no city
  blocker;
- hybrid without a remote path remains review when the city cannot be verified;
- explicit relocation/pereezd evidence remains a hard review signal.

Thus `Москва` in a remote-capable card is not converted to “candidate cannot
work there”. `Москва (офис)` and `Краснодар` in the observed dataset remain
reviewable location uncertainty, not automatic missing.

Experience remains separate by meaning:

- explicit generic `от 1 года` / `2 года` / `3 года` is evaluated against exact
  total months where trusted evidence supports it;
- `between1And3` from HH is only a factual/ranking signal;
- `2 года Python`, `3 года backend` and similar role-specific requirements do
  not become met from total experience alone; without role-specific facts they
  remain `UNKNOWN`;
- absent skill => `UNKNOWN`; only an explicit negative candidate fact can be
  `MISSING`.

Regression tests cover all of these cases, including remote location,
hybrid+remote, 11-month soft gap, generic HH bands and absent skill.

## 5. Score calibration

Scores for the 16 AI-evaluated rows:

| Statistic | Value |
|---|---:|
| n | 16 |
| min | 30 |
| p25 | 55 |
| median | 65 |
| p75 | 65 |
| max | 78 |

Observed groups:

| Group | Scores | Interpretation |
|---|---|---|
| obvious mismatch / low signal | 30, 30, 45, 55 | low score or hard mismatch; no reason to lower threshold |
| reasonable near-match | 55, 65, 65, 65, 65, 65, 68 | close to threshold; hard unknown/advisory frequently remains |
| strong-looking candidate | 75, 78, 78 | score alone still cannot bypass hard unknown or preflight |

The model scale is compressed around 65–78 and is not enough evidence for a
production normalization. `HH_MIN_MATCH_SCORE` stayed **65**; no normalization
was enabled and no threshold was lowered to manufacture MATCH.

## 6. Human-review shortlist (15)

This is a review set, not application approval. Later operator labels remain
`ACCEPT`, `REJECT`, `WRONG_RESUME`, `GOOD_MATCH`, `BAD_MATCH`.

| Vacancy / URL | Selected/best resume | Router score | AI score | Hard requirements | Old | New | Concise evidence |
|---|---|---:|---:|---|---|---|---|
| [137428040](https://ekaterinburg.hh.ru/vacancy/137428040) Python Backend | python | 57 | 65 | — | REJECT | REVIEW | Python/Django, PostgreSQL, REST; unexplained old veto |
| [137418714](https://ekaterinburg.hh.ru/vacancy/137418714) Backend Python junior | python | 54 | 78 | UNKNOWN: 1 year, FastAPI, Python 3.13 | REVIEW | REVIEW | strong score, unverified requirements |
| [136577315](https://ekaterinburg.hh.ru/vacancy/136577315) Backend Python DRF | python | 57 | 75 | UNKNOWN: Москва office | REVIEW | REVIEW | good stack, location requires human confirmation |
| [137384867](https://ekaterinburg.hh.ru/vacancy/137384867) Fullstack AI-assisted | backend | 38 | 65 | — | MATCH | REVIEW | advisory uncertainty; mixed stack |
| [136364927](https://ekaterinburg.hh.ru/vacancy/136364927) Senior Backend Python | python | 56 | 65 | — | MATCH | REVIEW | seniority gap is advisory, not proven missing |
| [137393619](https://ekaterinburg.hh.ru/vacancy/137393619) PHP intern | backend | 36 | 78 | preflight already responded | MATCH | REJECT | fresh read-only state blocks action |
| [136958238](https://ekaterinburg.hh.ru/vacancy/136958238) Backend Python | python | 48 | 68 | MISSING: from 2 years | REJECT | REJECT | explicit duration conflict |
| [136597178](https://ekaterinburg.hh.ru/vacancy/136597178) Python/FastAPI | python | 54 | 55 | UNKNOWN: FastAPI stack | REJECT | REJECT | low score plus unknown stack |
| [137374216](https://ekaterinburg.hh.ru/vacancy/137374216) amoCRM integrator | automation | 25 | 65 | MISSING: 2 years amoCRM | REJECT | REJECT | role-specific experience missing |
| [137436275](https://ekaterinburg.hh.ru/vacancy/137436275) Middle backend Python | python | 53 | 55 | MISSING: 3+ years; UNKNOWN ClickHouse/ETL | REJECT | REJECT | hard duration conflict |
| [137402396](https://ekaterinburg.hh.ru/vacancy/137402396) L2 r_keeper/iiko | best automation | 41 | n/a | route ambiguous | REVIEW | REVIEW | support role; top resume margin too close |
| [135644599](https://ekaterinburg.hh.ru/vacancy/135644599) Customer Success L2 | best technical | 42 | n/a | route ambiguous | REVIEW | REVIEW | strong support signal, resume unresolved |
| [137252236](https://ekaterinburg.hh.ru/vacancy/137252236) Data analyst | best automation | 33 | n/a | route ambiguous | REVIEW | REVIEW | domain mismatch/close route |
| [137430509](https://ekaterinburg.hh.ru/vacancy/137430509) Fullstack | best backend | 38 | n/a | route ambiguous | REVIEW | REVIEW | backend/frontend tie |
| [136563592](https://ekaterinburg.hh.ru/vacancy/136563592) Prompt Engineer | best automation | 25 | n/a | route ambiguous | REVIEW | REVIEW | AI role, no deterministic resume choice |

## 7. Final Shadow validation

Final authoritative auto-generated-scope run (after the metadata fix):

| Metric | Value |
|---|---:|
| Raw fetched | 198 |
| Unique after dedup | 152 |
| AI evaluated | 15 |
| Legacy AI apply=true / false | 11 / 4 |
| AI recommendation APPLY | 1 |
| AI recommendation DO_NOT_APPLY | 4 |
| AI recommendation UNCERTAIN | 10 |
| Hard missing | 5 |
| Hard unknown | 9 |
| Score below threshold | 8 |
| AI advisory-only concerns | 2 |
| AI MATCH / REJECT / REVIEW_REQUIRED | 0 / 10 / 5 |
| Final MATCH | 0 |
| Final REJECT | 11 |
| Final REVIEW_REQUIRED | 88 |
| Would apply | 0 |
| Shadow writes | 0 |
| Accounting | PASS |

`MATCH=0` is accepted. In the earlier calibration replay one AI MATCH was
stopped by read-only preflight (`ALREADY_RESPONDED`); the final validation run
had zero AI MATCH. No application preview/write was emitted in either run.

The 10 nearest candidates were: `137418714` (unknown hard requirements),
`136577315` (unknown office location), `137428040` (advisory concern),
`137384867` (advisory concern), `136597178` (unknown FastAPI stack),
`137374216` (missing 2 years), `137394574` (missing 3 years), `136422627`
(score below threshold), `136364927` (score below threshold), and `137251207`
(score below threshold). `137393619` is additionally blocked by fresh
`ALREADY_RESPONDED` preflight. These blockers explain why none is an
auto-apply candidate.

## 8. Tests and smoke

The following checks passed after the change:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
node --check web/app.js
dashboard/API smoke tests (GET /api/dashboard and read endpoints)
```

Regression coverage includes:

- `apply=false` alone cannot terminal-reject or match;
- advisory false/uncertain with no blocker becomes review;
- hard missing rejects regardless of recommendation;
- hard unknown reviews;
- low score rejects;
- high score plus no blockers and `APPLY` matches;
- absent skill is unknown, not missing;
- vacancy city alone and remote city do not create relocation blockers;
- exact selected-resume facts reach the assessment prompt;
- old JSON `apply` remains parse-compatible.

## Answers

1. `apply=false` was too strong because final policy treated a model judgment as
   a terminal safety fact before checking structured evidence; empty hard/reason
   fields could therefore become `REJECT`.
2. Of the eight historical false rows, `137428040` and `136364927` changed
   `REJECT -> REVIEW_REQUIRED`. The other six remain REJECT, respectively for
   already-responded state, low score, confirmed hard missing, or both.
3. Remaining rejects have a local reason code: `HARD_REQUIREMENT_MISSING`,
   `FIT_SCORE_BELOW_THRESHOLD`, or `ALREADY_RESPONDED`.
4. The two historical rows moved to review because only the old AI veto blocked
   them; new output has no verified hard blocker.
5. Final MATCH did not appear; MATCH=0 is correct for this dataset.
6. Most frequent blockers are route ambiguity, low score, unknown FastAPI/
   location/role-specific stack, and explicit experience mismatches.
7. Match threshold changed: **NO**. It remained 65.
8. HH writes: **0**; Shadow writes: **0**.

## Final handoff

```text
AI evaluated: 15
AI recommend APPLY: 1
AI recommend DO_NOT_APPLY: 4
AI recommend UNCERTAIN: 10
Final:
MATCH: 0
REJECT: 11
REVIEW_REQUIRED: 88
Would apply: 0
Changed decisions from old Apply=false:
137428040 REJECT -> REVIEW_REQUIRED (AI_ADVISORY_CONCERN)
136364927 REJECT -> REVIEW_REQUIRED (AI_ADVISORY_CONCERN)
Top candidates: none auto-eligible; see 10 nearest candidates above
Match threshold changed: NO
Real HH writes: 0
Shadow writes: 0
ACCOUNTING: PASS
```
