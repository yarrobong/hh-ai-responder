# RESET-8 AI/local-policy audit design

Date: 2026-09-19
Repository: `yarrobong/hh-ai-responder`
Baseline commit: `78fc45f9f4fa2bc4ad7fae1555c0ff507ca13209`

## Status and scope

This document is the RESET-8 design-stage audit. It does not change production
implementation, decision thresholds, router behavior, HH transport, or any HH
write path. The audit is intended to make the existing `12 ROUTE_SELECTED ->
12 AI/local-policy evaluations -> 0 MATCH` funnel explainable before any fix is
considered.

The only repository artifact created in this stage is this specification. The
protected paths remain untouched: Search Planner, RESET-6 router scoring,
`BrowserHHClient`/auth, cookies/session handling, application send, nonce,
reconciliation, write gateway, cover-letter generation, and AI threshold.

## Reproducible baseline

The baseline was run from the stable commit above with the production caps and
read-only guards:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_SEARCH_PAGES_PER_PROFILE=3
HH_MAX_SEARCH_PAGES_PER_RUN=48
HH_MAX_VACANCIES_PER_RUN=100
mode=career-agent --shadow
run_timestamp=2026-09-19T11:43:40.550453Z
```

Sanitized audit artifacts were written outside the repository:

```text
/tmp/reset8-old.json
/tmp/reset8-selected.json
/tmp/reset8-baseline-meta.json
```

They contain the selected IDs, resume/router evidence, AI output, local result,
and safety counters. They do not contain cookies or private raw authenticated
HTML. The run reported `shadow_write_count=0`, `would_apply=0`,
`applied=0`, `real_hh_writes=0`, and `application_post=0`.

## Current architecture and precedence

The current flow is:

1. Search Planner discovers and deduplicates vacancies.
2. RESET-6 routing chooses a resume or routes the vacancy to an early
   terminal outcome.
3. Only `ROUTE_SELECTED` vacancies receive detail/AI evaluation.
4. The AI returns score, advisory recommendation, reasons, and candidate-free
   hard-requirement candidates containing `requirement`, `category`, and exact
   `vacancy_evidence`.
5. Local code derives `met`, `missing`, or `unknown` from trusted candidate
   facts. AI `status` and `candidate_evidence` are not accepted as authority.
6. `vacancyDecisionWithReason` applies the local policy.

The implementation at the baseline preserves the historical Stage 29.6
precedence exactly:

| Order | Condition | Decision | Reason code |
|---:|---|---|---|
| 1 | non-optional hard requirement is `MISSING` | `REJECT` | `HARD_REQUIREMENT_MISSING` |
| 2 | score `< 65` | `REJECT` | `FIT_SCORE_BELOW_THRESHOLD` |
| 3 | non-soft, non-optional hard requirement is `UNKNOWN` | `REVIEW_REQUIRED` | `HARD_REQUIREMENT_UNKNOWN` |
| 4 | advisory recommendation is not `APPLY` | `REVIEW_REQUIRED` | `AI_ADVISORY_CONCERN` |
| 5 | all preceding gates pass and recommendation is `APPLY` | `MATCH` | `MATCH_CONFIRMED` |

No precedence drift was observed. In particular, legacy `apply=false` is not a
terminal rejection by itself, and a high score cannot bypass a hard
requirement gate.

## Funnel and row-level evidence

The complete discovery funnel is:

```text
raw hits                         502
distinct discovered              211
processed by router               50
  ROUTE_SELECTED                  12
  ROUTE_AMBIGUOUS                 12
  ROUTE_LOW_EVIDENCE              17
  NO_SUITABLE_RESUME               6
  ROLE_OUT_OF_SCOPE                3
AI/local evaluations              12
  recommendation APPLY              0
  recommendation DO_NOT_APPLY      3
  recommendation UNCERTAIN         9
final MATCH                        0
final REVIEW_REQUIRED              3
final REJECT                       9
```

The `AI apply` boolean is legacy/advisory telemetry and was `true/false =
9/3`; it is intentionally not used as a terminal policy gate. All 12 selected
rows were evaluated. Counts below count vacancies containing at least one
requirement in the relevant status, not individual requirement objects.

| Vacancy | Title | Family | Selected resume | Router / runner-up | Router confidence | AI score | Recommendation | Hard missing | Hard unknown | Local final | Exact reason |
|---:|---|---|---|---:|---:|---:|---|---|---|---|---|
| 137546982 | Python Engineer | `PYTHON_BACKEND` | Backend Python/Django | 57 / 51 | MEDIUM | 55 | UNCERTAIN | — | 3y commercial Python | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |
| 137531969 | AI/AI-agent developer (Python) | `PYTHON_BACKEND` | Backend Python/Django | 56 / 50 | MEDIUM | 25 | DO_NOT_APPLY | 2y AI/ML/NLP | 4y Python developer | REJECT | `HARD_REQUIREMENT_MISSING` |
| 136958238 | Backend-разработчик (Python) | `PYTHON_BACKEND` | Backend Python/Django | 57 / 52 | MEDIUM | 30 | DO_NOT_APPLY | 2–5y commercial | FastAPI, SQLAlchemy, Alembic, pytest, Kubernetes | REJECT | `HARD_REQUIREMENT_MISSING` |
| 136597178 | Backend-разработчик (Python / FastAPI) | `PYTHON_BACKEND` | Backend Python/Django | 61 / 57 | HIGH | 65 | UNCERTAIN | — | FastAPI, PostgreSQL design | REVIEW_REQUIRED | `HARD_REQUIREMENT_UNKNOWN` |
| 137149433 | Python-разработчик | `PYTHON_BACKEND` | Backend Python/Django | 59 / 53 | MEDIUM | 55 | UNCERTAIN | — | 5y Python/3y Django+DRF, 3y Django | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |
| 137444629 | Fullstack-разработчик | `WEB_BACKEND` | Backend-разработчик | 48 / 42 | MEDIUM | 45 | UNCERTAIN | — | Flutter, RabbitMQ, Prometheus/Grafana/ELK, CI/CD | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |
| 137436275 | Middle backend developer / Python специалист | `PYTHON_BACKEND` | Backend Python/Django | 59 / 55 | MEDIUM | 45 | UNCERTAIN | 3y commercial | ClickHouse, ETL | REJECT | `HARD_REQUIREMENT_MISSING` |
| 137418714 | Backend Developer Python (junior) | `PYTHON_BACKEND` | Backend Python/Django | 61 / 56 | HIGH | 70 | UNCERTAIN | — | 1y backend, FastAPI, Apache Kafka | REVIEW_REQUIRED | `HARD_REQUIREMENT_UNKNOWN` |
| 137364064 | Python-разработчик | `PYTHON_BACKEND` | Backend Python/Django | 52 / 44 | MEDIUM | 45 | UNCERTAIN | — | 3y Python developer | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |
| 137516002 | Senior Backend Developer (Python / Django) | `PYTHON_BACKEND` | Backend Python/Django | 66 / 60 | HIGH | 45 | UNCERTAIN | — | — | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |
| 136577315 | Backend разработчик (Python DRF) | `PYTHON_BACKEND` | Backend Python/Django | 63 / 60 | HIGH | 65 | UNCERTAIN | — | Москва (офис) | REVIEW_REQUIRED | `HARD_REQUIREMENT_UNKNOWN` |
| 137500466 | Python Backend Developer | `PYTHON_BACKEND` | Backend Python/Django | 60 / 56 | HIGH | 30 | DO_NOT_APPLY | — | — | REJECT | `FIT_SCORE_BELOW_THRESHOLD` |

## Final reason taxonomy

| Reason | Count | Vacancies | Family breakdown |
|---|---:|---|---|
| `HARD_REQUIREMENT_MISSING` | 3 | 137531969, 136958238, 137436275 | Python 3 |
| `FIT_SCORE_BELOW_THRESHOLD` | 6 | 137546982, 137149433, 137444629, 137364064, 137516002, 137500466 | Python 5, Web 1 |
| `HARD_REQUIREMENT_UNKNOWN` | 3 | 136597178, 137418714, 136577315 | Python 3 |
| `AI_ADVISORY_CONCERN` | 0 | — | — |
| `MATCH_CONFIRMED` | 0 | — | — |

The reason accounting is complete: `3 + 6 + 3 = 12`. There were no
additional terminal reason codes in the selected set.

## Score and recommendation audit

AI scores, sorted, are:

```text
25, 30, 30, 45, 45, 45, 45, 55, 55, 65, 65, 70
```

```text
minimum: 25
maximum: 70
median: 50
<50: 7
50–64: 2
65–79: 3
80+: 0
```

The below-threshold explanations are consistent with the row evidence at a
high level: 137546982 has a 3-year seniority gap and stack mismatch;
137149433 has a 5-year Python/3-year Django seniority gap; 137444629 is an
adjacent fullstack profile with unverified mobile/messaging/observability
stack; 137364064 has an unverified 3-year Python requirement; 137516002 has a
low overall fit despite no extracted hard requirements; and 137500466 has a
low overall fit with `DO_NOT_APPLY`. The AI text is advisory evidence, not a
trusted negative candidate fact.

Recommendation is independent from score in the observed data:

| Combination | Count | Vacancies |
|---|---:|---|
| score `>=65` + `DO_NOT_APPLY` | 0 | — |
| score `>=65` + `UNCERTAIN` | 3 | 136597178, 137418714, 136577315 |
| score `<65` + `APPLY` | 0 | — |
| score `<65` + `DO_NOT_APPLY` | 3 | 137531969, 136958238, 137500466 |
| score `<65` + `UNCERTAIN` | 6 | remaining six below-threshold rows |

There was no `APPLY` recommendation, so no row reached `MATCH`. The three
high-score rows were not rejected for recommendation; all three were held for
unknown hard requirements, as required by the safety policy.

## Role-family breakdown

Counts of hard statuses are row-level counts: a row is counted once if it has
at least one missing or unknown hard requirement.

| Family | Selected | Avg score | Median score | Hard missing | Hard unknown | APPLY | REVIEW | REJECT | MATCH |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `PYTHON_BACKEND` | 11 | 48.18 | 45 | 3 | 9 | 0 | 3 | 8 | 0 |
| `WEB_BACKEND` | 1 | 45.00 | 45 | 0 | 1 | 0 | 0 | 1 | 0 |
| `AUTOMATION_INTEGRATIONS` | 0 | — | — | 0 | 0 | 0 | 0 | 0 | 0 |
| `TECH_SUPPORT` | 0 | — | — | 0 | 0 | 0 | 0 | 0 | 0 |

Overall, hard missing occurs in 3/12 rows and hard unknown in 10/12 rows.
The unknown count is high because the safe local policy treats absent evidence
for an extracted mandatory skill as unknown rather than inventing a negative
fact. This is conservative, but it also makes extraction quality critical.

## Hard-requirement extraction audit

The following classification is an audit label only. It does not change the
current production semantics. `EXPLICIT_HARD` means the evidence contains a
clear minimum, seniority, or explicit mandatory cue. `AMBIGUOUS` means the
provider fragment may be a core stack item but lacks enough surrounding source
context to establish that it is mandatory. `NOT_A_REQUIREMENT` means the
fragment contradicts a hard interpretation.

| Vacancy | Provider/local requirement | Current status | Audit classification and finding |
|---:|---|---|---|
| 137546982 | 3y commercial Python | UNKNOWN | `EXPLICIT_HARD`, role-specific duration; total experience must not satisfy it. |
| 137531969 | 4y Python developer | UNKNOWN | `EXPLICIT_HARD`, role-specific duration; correctly unknown without role evidence. |
| 137531969 | 2y AI/ML/NLP | MISSING | `EXPLICIT_HARD` if the stated minimum is mandatory, but role/technology-specific; current MISSING is not justified by total 11-month experience. |
| 136958238 | 2–5y commercial development | MISSING | `EXPLICIT_HARD`, generic minimum; current missing is evidence-backed for 11 months. |
| 136958238 | FastAPI, SQLAlchemy, Alembic, pytest, Kubernetes | UNKNOWN | `AMBIGUOUS`; bare stack fragments do not show an explicit mandatory cue in telemetry. Unknown is safe, but hard extraction may be too broad. |
| 136597178 | FastAPI; PostgreSQL schema design; Redis | UNKNOWN/UNKNOWN/MET | `AMBIGUOUS`; Redis is grounded by trusted candidate skill evidence, while the two qualified stack requirements are not fully evidenced. |
| 137149433 | 5y Python and 3y Django/DRF | UNKNOWN | `EXPLICIT_HARD`, seniority requirements; current unknown is conservative and does not infer from total duration. |
| 137444629 | Flutter; RabbitMQ; Prometheus/Grafana/ELK; CI/CD | UNKNOWN | `AMBIGUOUS`; stack list lacks mandatory context in the captured fragment. |
| 137436275 | 3y commercial development | MISSING | `EXPLICIT_HARD`, generic minimum; current missing is evidence-backed for 11 months. |
| 137436275 | ClickHouse; ETL | UNKNOWN | `EXPLICIT_HARD`; source explicitly says `Обязателен опыт`, so unknown is correct without candidate evidence. |
| 137418714 | 1y backend development | UNKNOWN | `EXPLICIT_HARD` role-specific duration; unknown is safer than converting total experience into a role-specific fact. |
| 137418714 | FastAPI; Apache Kafka | UNKNOWN | `AMBIGUOUS`; stack fragments lack mandatory context in the telemetry. |
| 137364064 | 3y Python developer | UNKNOWN | `EXPLICIT_HARD`, role-specific duration; correctly unknown without role-specific evidence. |
| 136577315 | Москва (офис) | UNKNOWN | `NOT_A_REQUIREMENT` / likely false hard extraction: evidence says an office exists if the candidate does not like remote work, not that office attendance is required. |
| 137516002 | none | — | `NOT_A_REQUIREMENT`; no hard requirement was extracted. |
| 137500466 | none | — | `NOT_A_REQUIREMENT`; no hard requirement was extracted. |

The baseline prompt names optional markers including `желательно`, `будет
плюсом`, and `будет преимуществом`, and the local filter also recognizes
additional variants. No selected row exposed a directly captured optional
marker as a hard requirement. However, optional-marker handling is split across
prompt text, candidate-side filtering, and final evaluation; the marker sets
are not one canonical classifier. For example, `приветствуется` and
`не обязательно` are handled by different layers. This is a maintainability
and regression risk even though it did not produce a selected-row mismatch in
this run.

## MISSING versus UNKNOWN and evidence provenance

The current local meanings are materially different:

- `MET` requires grounded candidate evidence.
- `MISSING` requires a trusted explicit negative fact, or a supported generic
  total-duration minimum that the trusted structured total does not meet.
- `UNKNOWN` is required when the candidate data is absent, role-specific
  evidence is unavailable, location/relocation is unresolved, or the
  requirement cannot be evaluated from trusted structure.

Observed sources:

| Verdict/source shape | Baseline examples | Audit result |
|---|---|---|
| Trusted structured total experience | 11 months for generic 2–5y and 3y requirements | Supports `MISSING` for generic minimums. |
| Trusted selected-resume/profile skill evidence | Redis on 136597178 | Supports `MET`; no inference to PostgreSQL schema design. |
| No trusted candidate evidence | FastAPI, Kafka, Kubernetes, ClickHouse, ETL, etc. | Correctly remains `UNKNOWN`, not fabricated `MISSING` or `MET`. |
| Role-specific duration with only total duration available | 3y Python, 4y Python, 3y Django, 2y AI/ML/NLP | Must remain `UNKNOWN`; the AI/ML/NLP row is the exception caused by a classifier gap. |
| Location/office evidence | 136577315 | Candidate relocation/office willingness is not inferred; the problem is upstream false-hard extraction. |

### Confirmed or likely false-rejection patterns

1. **Confirmed local classification defect for AI/ML/NLP.** The requirement
   `2 года опыта в AI/ML/NLP` was assigned `MISSING` with candidate evidence
   `Candidate total experience: 11 months`. The role-specific marker list
   recognizes Python/Django/DevOps/etc. but not `AI`, `ML`, or `NLP`, so the
   generic-duration path was used. This triggered
   `HARD_REQUIREMENT_MISSING` for vacancy 137531969. The safe expected result
   is `UNKNOWN` unless trusted AI/ML/NLP-specific evidence exists; this is a
   likely false rejection and must receive a regression fixture.
2. **Likely false hard extraction for the office phrase.** For vacancy
   136577315, `Москва (офис)` was emitted from evidence equivalent to “if you
   do not like remote work, there is an office in Moscow”. That is an optional
   work-mode alternative, not an office-only requirement. It caused a
   `HARD_REQUIREMENT_UNKNOWN` review. This is a likely false review and needs a
   location/work-mode fixture.
3. **No false negative was found for absent stack evidence.** The current
   `UNKNOWN` results for FastAPI, Kafka, Kubernetes, ClickHouse, ETL, and
   similar skills do not claim that the candidate lacks them. Whether those
   skills should have entered `hard_requirements` at all remains ambiguous
   because the report stores a short evidence fragment rather than the source
   sentence/section and mandatory cue.

## Router versus AI disagreement

Router and AI scores are different scales and answer different questions, so
they must not be treated as a single calibrated score. The strongest observed
disagreements are:

| Vacancy | Router | AI | Hard/final result | Interpretation |
|---:|---|---:|---|---|
| 137516002 | HIGH, 66 vs 60 | 45 | no hard requirements; score reject | Router found a strong resume-role route, while AI judged overall vacancy fit low. This is a genuine layer disagreement requiring vacancy-evidence review, not an automatic router or AI fault. |
| 137500466 | HIGH, 60 vs 56 | 30 | no hard requirements; score reject | High resume-route confidence does not imply seniority/stack fit; AI reasons are negative but not independently attributable to score components. |
| 137418714 | HIGH, 61 vs 56 | 70 | unknown hard requirements; review | Resume route and AI fit are aligned positively, but safe policy holds on unknown backend/stack evidence. |
| 136577315 | HIGH, 63 vs 60 | 65 | office unknown; review | Positive route/score conflict with a likely false location hard extraction. |
| 136597178 | HIGH, 61 vs 57 | 65 | FastAPI/PostgreSQL unknown; review | Positive route/threshold score is blocked by evidence sufficiency, not by AI recommendation. |

The router is therefore not demonstrably “too optimistic” from this sample.
The main evidence gap is that AI score reasons are free text and are not
linked to individual requirements or weighted components.

## Telemetry completeness and design requirements

The baseline JSON is sufficient to reconstruct all 12 final decisions, the
precedence gate, selected resume, router confidence, score, recommendation,
hard statuses, and safety counters. It is not sufficient to fully audit
extraction quality without returning to the source vacancy because it lacks:

- the source sentence/field and section for each `vacancy_evidence` fragment;
- a provider extraction label such as `EXPLICIT_HARD`, `PREFERENCE`, or
  `AMBIGUOUS`;
- candidate-evidence provenance as a structured source (`HH resume`, trusted
  profile, project, education, explicit constraint), rather than a free-text
  explanation;
- per-requirement normalization/classification diagnostics, including which
  experience marker made a requirement generic or role-specific;
- attributable score components connecting a score gap to evidence;
- a distinct field for `local_policy_gate` before the final reason code.

Future observational telemetry may add these fields in a backward-compatible,
read-only form. It must not persist cookies, raw authenticated HTML, complete
private prompts, or secrets. Evidence should be bounded source spans or
privacy-safe references sufficient to reproduce classification.

## Proposed design direction (not implementation)

The next implementation stage should proceed only after review of this audit.
The design should:

1. Centralize optional-marker detection and test it against all supported
   Russian and English variants.
2. Make experience classification explicitly distinguish generic total
   duration from role/technology-specific duration, including AI/ML/NLP and
   equivalent compound domains.
3. Treat office availability as non-blocking unless the source explicitly
   requires office attendance, relocation, or a named location.
4. Preserve the Stage 29.6 precedence and threshold of 65 unchanged.
5. Preserve `UNKNOWN` for absent candidate evidence and never turn it into
   `MISSING` merely to reduce review volume.
6. Keep AI recommendation advisory. `DO_NOT_APPLY` at a passing score must
   remain `REVIEW_REQUIRED / AI_ADVISORY_CONCERN` when no stronger local gate
   fires.
7. Add observational extraction/provenance telemetry before changing
   decision semantics.

No proposed change is authorized by this document. In particular, increasing
`MATCH` count is not a success criterion.

## Required regression fixtures

If the findings are approved for a later implementation, the minimum fixture
set is:

- `желательно`, `будет плюсом`, `будет преимуществом`, `приветствуется`,
  `предпочтительно`, and `не обязательно` do not create hard blockers;
- explicit mandatory degree and explicit mandatory years-of-experience remain
  hard requirements;
- `2 года AI/ML/NLP` with only trusted total experience remains `UNKNOWN`, not
  `MISSING`;
- role-specific `3 года Python`, `2 года DevOps`, and equivalent technology
  durations do not use generic total duration;
- absent Kubernetes/FastAPI/Kafka evidence is `UNKNOWN`, not fabricated
  `MET` or `MISSING`;
- generic `3 года` with trusted 11-month total experience remains
  `MISSING` under the existing policy;
- “if you do not like remote work, there is an office in Moscow” does not
  create an office-only hard requirement;
- score `>=65` plus `DO_NOT_APPLY` is advisory review, not hard rejection;
- score `>=65` plus all hard requirements satisfied plus `APPLY` is
  `MATCH`;
- hard `UNKNOWN` is `REVIEW_REQUIRED`;
- hard `MISSING` is `REJECT`;
- dry-run still prevents every HH write, application POST, chat write, resume
  mutation, and job-search status mutation.

## Definition of done for RESET-8 audit

The design-stage audit is complete when every selected vacancy has a
row-level, evidence-backed final decision; the exact historical precedence is
verified; hard extraction, MISSING/UNKNOWN semantics, candidate provenance,
score/recommendation independence, family behavior, router/AI disagreement,
and telemetry gaps are documented; likely false rejection patterns have
regression fixtures; and no production implementation or HH write was made.

For this baseline, `MATCH=0` is acceptable only as an audit result. The two
likely false patterns above must be resolved or explicitly accepted in a
separate reviewed implementation step before they can be considered stable.
