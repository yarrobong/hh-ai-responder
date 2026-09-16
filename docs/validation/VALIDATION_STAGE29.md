# Stage 29 — Career Agent pipeline validation

Дата: 2026-09-16 (Asia/Yekaterinburg).

Stage 29 исправляет порядок routing: неполная search card теперь даёт
internal `NEEDS_DETAIL`, а не terminal `REVIEW_REQUIRED`. После read-only
detail выполняется новый final route. Search-profile provenance добавляется
как bounded soft signal (+4 максимум) и не может разрешить близкий fit.

## Git and safety

- Starting `HEAD`: `b42c09158ac2a6557afa2f2d71dedc3376fdc451`.
- Starting `origin/main`: `b42c09158ac2a6557afa2f2d71dedc3376fdc451`.
- Existing untracked `out` сохранён и в commit не включается.
- Shadow mode принудительно устанавливает `HH_DRY_RUN=true` и
  `HH_WRITE_ENABLED=false`, а также отключает chat/touch/job-status.
- HH writes выполнены: **NO**.

## Что было причиной Stage 28

В Stage 28 orchestration вызывала `RouteResume` на search-card до чтения
description/detail. Для 96 вакансий, прошедших cheap filters, неполные поля
делали scores близкими; 82 записи сразу становились terminal
`REVIEW_REQUIRED`. Только 14 выбранных resume доходили до detail, из них 13
до AI. Таким образом, provider evidence, уже доступный на detail endpoint,
не участвовал в выборе resume для 82 вакансий.

Stage 29 разделяет:

`search card → cheap filters → preliminary route → detail (только для
неполных/потенциально релевантных cards) → final route → AI`.

`REVIEW_REQUIRED` теперь означает final ambiguity, unknown critical state или
другой safety reason после доступного detail. Для каждой vacancy trace
содержит `preliminary_route`, `preliminary_reason_code`,
`detail_fetch_status`, `detail_evidence`, `final_route_reason_code` и
`ai_call_reason`.

## Deterministic router validation

Это structured regression evidence, без chain-of-thought:

| Category | Search-card evidence | Detail evidence | Before detail | After detail | Final result |
|---|---|---|---|---|---|
| Python/backend | `Backend specialist` | `Python backend service development`, key skill `Python` | `NEEDS_DETAIL` | Python resume wins with role + skill evidence | selected |
| automation/integration | partial integration title | `API`, `SQL`, `integration` | `NEEDS_DETAIL` | integration resume wins on normalized terms | selected |
| technical support | sparse support card | support title/description and structured fields | `NEEDS_DETAIL` | support resume is evaluated with full evidence | selected/review by fit |
| clearly irrelevant | unrelated title, no source provenance | not requested | `OBVIOUS_REJECT` | no final route | reject, zero detail reads |
| ambiguous | same Python/backend evidence for two enabled resumes | same full detail | `NEEDS_DETAIL` | equal fit remains equal | `REVIEW_REQUIRED` |

Covered regressions include RU/EN canonical groups (`автоматизация` /
`automation`, `интеграция` / `integration`, `поддержка` / `support`,
`внедрение` / `implementation`, `разработчик` / `developer`, `бэкенд` /
`backend`), punctuation/hyphen tokenization, title/description/key-skills
evidence, disabled resume exclusion, hard-blocker precedence, and soft-only
search-profile provenance.

## Tests and smoke checks

PASS:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
- `git diff --check`
- `node --check web/app.js`
- local dashboard/API smoke: `GET /api/dashboard` → HTTP 200.

The test suite verifies exact one-terminal-outcome accounting and the new
detail-first behavior, including cached detail reuse so the subsequent
application preparation does not issue a duplicate description read.

## Real Shadow validation

Both requested read-only commands were attempted with
`HH_DRY_RUN=true HH_WRITE_ENABLED=false`:

1. automatic planner (`career-agent --shadow` with generated profiles);
2. manual/legacy profiles (`career-agent --shadow` with configured explicit
   search profiles).

Both runs stopped before search discovery because the first authenticated HH
GET returned `403 Forbidden`. No valid Stage 29 provider dataset was produced;
therefore the following values are intentionally **N/A**, not zero:

| Metric | Automatic | Manual/legacy |
|---|---:|---:|
| Raw / Duplicates / Unique | N/A | N/A |
| Already responded | N/A | N/A |
| Obvious deterministic rejects | N/A | N/A |
| Preliminary clear route | N/A | N/A |
| Preliminary needs detail | N/A | N/A |
| Detail requested / succeeded / failed | N/A | N/A |
| Final selected resume | N/A | N/A |
| Final ambiguous | N/A | N/A |
| AI evaluated / rejected / matched | N/A | N/A |
| MATCH / REVIEW_REQUIRED / REJECT | N/A | N/A |
| Would apply | N/A | N/A |
| Shadow writes | 0 | 0 |
| ACCOUNTING CHECK | N/A: no discovery records | N/A: no discovery records |

Real HH read attempts: 2; successful Stage 29 provider reads: 0; provider
response: `403 Forbidden`.

## Stage 28 vs Stage 29

The Stage 28 column is the verified automatic run from
`VALIDATION_STAGE28.md`. Stage 29 has no comparable real-run numbers because
HH rejected the first read, so claiming an improved REVIEW percentage would
be misleading.

| Metric | Stage 28 | Stage 29 |
|---|---:|---:|
| Unique | 105 | N/A — provider blocked |
| Detail fetches | 14 | N/A — provider blocked |
| Resume selected | 14 | N/A — provider blocked |
| Final `REVIEW_REQUIRED` | 82 | N/A — provider blocked |
| AI evaluated | 4 | N/A — provider blocked |
| MATCH | 0 | N/A — provider blocked |
| REJECT | 13 | N/A — provider blocked |
| Errors | 0 | 2 run-start failures (`403`) |
| Writes | 0 | 0 |

The implementation-level evidence does demonstrate the intended change:
review is counted only after final routing, `review_before_detail` is exposed
separately, and a selected full-detail vacancy reaches AI unless a distinct
deterministic preparation or safety reason stops it. The real percentage
change requires rerunning after HH read access is restored.

## Best controlled-pilot candidates

No Stage 29 provider run reached AI, so there are **0 verified Stage 29
`MATCH`/high-confidence candidates** and no truthful cover-letter previews to
save. No application was submitted. After read access is restored, the JSON
report's `would_apply`, selected resume, score, confidence, reasons,
`ai_call_reason`, and application preview fields are the source for the
controlled top-five pilot list.

## Answers to the required questions

1. Stage 28 produced 82 reviews because routing happened before detail and
   treated sparse card evidence as terminal ambiguity.
2. Stage 29 cases resolved after detail: not measurable in this run because
   HH returned `403` before discovery; regression fixtures prove the
   ambiguous-card → detail → clear-route path.
3. Genuine ambiguity: regression fixture confirms equal full-detail fits stay
   `REVIEW_REQUIRED`; real count is pending provider access.
4. Resume choices: deterministic fixtures cover Python/backend,
   integration/automation, and support routes; real Stage 29 choices are
   pending provider access.
5. AI reached all successfully selected full-detail fixtures; real run count
   is pending provider access.
6. Best five: none verified in Stage 29 because no vacancy reached AI.
7. HH write performed: **NO**. Shadow writes: **0**.
