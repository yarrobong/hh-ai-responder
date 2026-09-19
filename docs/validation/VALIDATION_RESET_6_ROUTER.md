# RESET-6 router calibration validation

Validation date: 2026-09-19. All live validation runs were read-only with
`HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and `STORAGE_BACKEND=json`.

## Baselines and scope

- Source baseline: `5dc0e102534a2a270c0f0392af01df0827f96b1f`.
- Implementation start SHA: `7c5c69d39de31ee0a1c6beb39398575fde4ea1eb`.
- Old report: `/tmp/reset6-old.json`.
- Task 6 post-fix report: `/tmp/reset6-new2.json`.
- Final post-verification report: `/tmp/reset6-final.json`.
- Exact pre-flight metadata: `/tmp/reset6-baseline-meta.json`.
- Enabled resumes: 4. The preserved old baseline has 46 vacancy IDs.

The second live run discovered 47 unique IDs. Forty-four of the 46 preserved
IDs were present in that run; `137433931` and `137435884` were absent. Three
new IDs were excluded from old-vs-new transitions. The missing-ID limitation
is reported explicitly; no different vacancy IDs were substituted into the
transition matrix.

## Enabled resume role families

| Resume | Primary family | Secondary families |
|---|---|---|
| Специалист по автоматизации и интеграциям / инженер внедрения | `AUTOMATION_INTEGRATIONS` | `TECH_SUPPORT` |
| Backend-разработчик | `PYTHON_BACKEND` | `WEB_BACKEND`, `AUTOMATION_INTEGRATIONS`, `TECH_SUPPORT` |
| Backend-разработчик (Python/Django) / автоматизация и интеграции | `PYTHON_BACKEND` | `WEB_BACKEND`, `AUTOMATION_INTEGRATIONS` |
| Технический специалист | `TECH_SUPPORT` | none |

Role-family detection for the vacancy runs independently of this inventory.
Adjacent skills such as `Администрирование сайтов`, `Linux`, `React`, or
`1С-Битрикс` do not by themselves turn a resume into a bounded unsupported
role family.

## Before / after

The legacy report had one catch-all post-detail route category, so its
separate no-suitable, out-of-scope, and low-evidence counts were not
observable.

| Metric | Old baseline | Post-fix run |
|---|---:|---:|
| Unique processed | 46 | 47 |
| Fresh route set | 42 | 41 |
| `ROUTE_SELECTED` | 2 | 1 |
| `ROUTE_AMBIGUOUS` | 40 | 1 |
| `NO_SUITABLE_RESUME` | not separately reported | 2 |
| `ROLE_OUT_OF_SCOPE` | not separately reported | 16 |
| `ROUTE_LOW_EVIDENCE` | not separately reported | 21 |
| AI evaluated | 2 | 1 |
| MATCH | 0 | 0 |
| REVIEW_REQUIRED | 40 | 40 |
| REJECT | 2 | 1 |
| Shadow writes | 0 | 0 |

The final post-verification fresh-route accounting is exact:
`1 + 1 + 2 + 16 + 21 = 41`. The final rerun preserved the same route
categories, selected vacancy, AI count, and zero-write result as the Task 6
post-fix report.

## Old-to-new transition matrix

The matrix uses the preserved baseline IDs only. Old
`ROUTE_AMBIGUOUS_AFTER_DETAIL` is normalized to canonical
`ROUTE_AMBIGUOUS`.

| Old outcome | New outcome | Count |
|---|---|---:|
| `ALREADY_RESPONDED` | `ALREADY_RESPONDED` | 4 |
| `ROUTE_AMBIGUOUS` | `ROUTE_SELECTED` | 1 |
| `ROUTE_AMBIGUOUS` | `ROUTE_AMBIGUOUS` | 1 |
| `ROUTE_AMBIGUOUS` | `NO_SUITABLE_RESUME` | 1 |
| `ROUTE_AMBIGUOUS` | `ROLE_OUT_OF_SCOPE` | 15 |
| `ROUTE_AMBIGUOUS` | `ROUTE_LOW_EVIDENCE` | 20 |
| `ROUTE_AMBIGUOUS` | not present in new run | 2 |
| `ROUTE_SELECTED` | `NO_SUITABLE_RESUME` | 1 |
| `ROUTE_SELECTED` | `ROUTE_LOW_EVIDENCE` | 1 |

There were no `SELECTED -> same resume` or `SELECTED -> changed resume`
transitions. The only new selection was vacancy `137532422`, previously
ambiguous. Its selected support resume scored 53 versus 48 for the runner-up,
had strong `TECH_SUPPORT` evidence, four specific evidence items, and a 0.25
generic-evidence ratio. The runner-up had only one specific support match and
0.40 generic ratio. AI later rejected the vacancy for a hard requirement;
`would_apply` remained false.

The complete row-level analysis is in `/tmp/reset6-transition.tsv` and was
not committed because it is generated validation data.

## Old ambiguity categorization

These are analytical labels for the old catch-all outcomes, not new routing
rules.

| Category | Representative old vacancies |
|---|---|
| `TRUE_AMBIGUITY` | `137512891` Специалист технической поддержки |
| `SCORE_COMPRESSION` | `137532422` Специалист технической поддержки (офис) |
| `GENERIC_SIGNAL_COLLISION` | `137444477` Frontend-разработчик; `136977543` поддержка по маркировке и 1С |
| `MISSING_ROLE_ANCHOR` | `137532126` Data Engineer; `135732605` Тестировщик; `136901455` QA-инженер; `137454400` SQL разработчик |
| `WRONG_COMPETITOR` | `134141984` Системный администратор; `137157724` системный аналитик; `137283560` Аналитик 1С; `137518989` Программист 1С |
| `LOW_EVIDENCE` | `135846721` АСУ ТП; `136585470` инженер-программист АСУ ТП; `136841654` информационная безопасность; `137318231` сопровождение кибербезопасности; `137336588` IT Project Manager; `137437860` AI-агенты; `137474682` сопровождение финансовых рынков; `137484102` инженер-технолог; `137486025` преподаватель; `137528324` сетевой инженер; `137271831` QA Engineer |

## Safety sample

The live run produced only one selected route, so the requested ten-selected
sample was not available. All one selected route and the one ambiguous route
were reviewed. The blocked sample covered more than five vacancies:

| Vacancy | Route | Evidence reviewed |
|---|---|---|
| `137532422` Специалист технической поддержки (офис) | selected | strong `TECH_SUPPORT`; four specific matches; top 53 vs 48; generic ratio 0.25; AI rejected hard requirement; no application |
| `137512891` Специалист технической поддержки | ambiguous | competing support resumes; no deterministic selection |
| `137191871` Системный администратор Linux-серверов | out of scope | strong `SYSTEM_ADMIN`; no enabled resume family supports it |
| `137157724` Ведущий системный аналитик | out of scope | strong `SYSTEM_ANALYST`; no enabled resume family supports it |
| `137518989` Программист 1С / разработчик УНФ | out of scope | strong `ONE_C`; no enabled resume family supports it |
| `137444477` Frontend-разработчик Vue/Nuxt | no suitable | frontend evidence remained incompatible with enabled resume evidence floor |
| `137532126` Data Engineer | low evidence | no bounded strong family; review required |
| `135846721` Инженер-испытатель АСУ ТП | low evidence | no bounded strong family; review required |

The earlier safety review found and blocked a false sysadmin selection caused
by the adjacent skill `Администрирование сайтов`. A failing regression test
was added before the policy fix. The post-fix run produced no sysadmin or 1C
false selections; those vacancies became `ROLE_OUT_OF_SCOPE`.

## Safety and write audit

- Real HH writes: **0**.
- Application POST: **0**.
- `shadow_write_count`: `0`.
- `accounting_pass`: `true`.
- AI was entered only for the one `ROUTE_SELECTED` vacancy.
- No cover letter, test submission, chat reply, resume touch, or application
  was executed.

## Verification evidence

Task 8 complete checks passed: full tests, race, vet, build, formatting/diff
checks, JavaScript syntax check, and the final read-only run. The final source
commit is reported in the RESET-6 handoff because it includes this validation
report.
