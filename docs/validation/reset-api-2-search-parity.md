# RESET-API-2 search parity audit

This is a report-only audit of the capped live API shadow run on 2026-09-20.
It does not tune or redesign search.

Command limits were `HH_MAX_SEARCH_PAGES_PER_PROFILE=3`,
`HH_MAX_SEARCH_PAGES_PER_RUN=48`, and `HH_MAX_VACANCIES_PER_RUN=100`, with
`HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and explicit API transport.

Run totals:

- raw hits: 2054
- distinct discovered: 1214
- processed by router: 50
- search pages fetched: 47
- detail requested/succeeded: 50/47
- AI evaluated: 10
- applied: 0
- shadow writes: 0
- accounting pass: true

Per-profile evidence:

| Query | Pages | Items returned | HH found | First ID | Last ID |
|---|---:|---:|---:|---:|---:|
| Специалист по автоматизации и интеграциям инженер внедрения | 3 | 76 | 76 | 137010566 | 134512547 |
| Backend-разработчик | 3 | 150 | 521 | 136422627 | 130351650 |
| Backend-разработчик (Python Django) автоматизация и интеграции | 2 | 3 | 3 | 137516002 | 137434460 |
| Python-разработчик | 3 | 150 | 702 | 134626812 | 136958238 |
| веб-разработчик | 3 | 150 | 790 | 137549579 | 136806013 |
| backend-разработчик | 3 | 150 | 521 | 136422627 | 130351650 |
| инженер внедрения | 3 | 150 | 3138 | 136474654 | 135615679 |
| Backend-разработчик | 3 | 150 | 521 | 136422627 | 130351650 |
| Backend developer | 3 | 150 | 306 | 137010566 | 136556462 |
| специалист по интеграциям | 3 | 150 | 3163 | 137011007 | 136998850 |
| Python backend | 3 | 150 | 305 | 136862604 | 136889169 |
| PHP backend | 3 | 119 | 119 | 136657473 | 135244435 |
| automation engineer | 3 | 150 | 3277 | 136474654 | 136998729 |
| Python developer | 3 | 150 | 1864 | 134626812 | 135691121 |
| Laravel developer | 3 | 90 | 90 | 136657729 | 135244435 |
| integration specialist | 3 | 116 | 116 | 137548671 | 136261454 |

For every profile, the API requests sent these query parameters:

- `text=<profile query>`
- `page=0,1,...` for the pages fetched
- `per_page=50`
- `period=7`
- `order_by=publication_time`

No `area`, salary, employment, schedule, experience, professional-role, or
work-format filter was present in the API request set. The planner-only
parameters present in the profile definitions but deliberately not sent to
the API were `career_agent_include` and `career_agent_exclude`. They remain
local planner filters; this task makes no parity claim about their effect.

The large difference between raw hits and distinct discovered vacancies is
therefore recorded as evidence for a later RESET-7 investigation, not used to
change search behavior in RESET-API-2.
