# Validation Stage 27 — Career Agent foundation

Дата: 2026-09-16 11:54 Asia/Yekaterinburg.

Изменения добавляют Resume Registry, deterministic Search Planner, multi-search
discovery/routing, Shadow/Canary CLI, feedback storage и сохраняют существующий
write gateway/application-attempt/reconciliation pipeline.

## Реальный read-only Shadow Mode

Запуск выполнялся с локальными `.env` и `cookies.txt`, но с явным override
`--storage-backend json` и принудительными `HH_DRY_RUN=true` /
`HH_WRITE_ENABLED=false`. HH write transport не вызывался.

Legacy explicit search profiles:

| Metric | Measured |
| --- | ---: |
| normalized resumes | 4 |
| search profiles | 3 |
| raw results | 87 |
| unique vacancies | 47 |
| duplicates skipped | 40 |
| previously responded skipped | 3 |
| AI evaluated | 0 |
| routing review | 44 |
| would apply | 0 |
| actually applied | 0 |
| errors | 0 |

Automatic planner run used an explicit empty `-u`, `--max-search-profiles 6`
and `--max-vacancies-per-run 8`:

| Metric | Measured |
| --- | ---: |
| normalized resumes | 4 |
| generated search profiles | 6 |
| raw results | 49 |
| unique vacancies | 39 |
| duplicates skipped | 10 |
| processed in bounded run | 9 |
| AI evaluated | 1 |
| MATCH | 0 |
| REVIEW_REQUIRED | 5 |
| would apply | 0 |
| actually applied | 0 |
| errors | 0 |

The last extra processed item is the existing limit-boundary event; it does not
dispatch an application. The local AI evaluation returned a score below the
configured match threshold, so no cover-letter preview qualified as
`would_apply`.

## Regression and safety checks

```text
go test ./...                 PASS
go test -race ./...           PASS
go vet ./...                  PASS
go build ./...                PASS
git diff --check              PASS
node --check web/app.js       PASS
```

Focused CLI checks passed for `career-agent --help`, resume listing and feedback
append/read using a temporary JSON store. Direct dashboard startup on loopback
port 18081 and `GET /api/dashboard` returned valid JSON, then the process was
stopped.

The historical command `./start.sh web smoke` was also attempted. It returns
`usage: web [--host 127.0.0.1] [--port 8080]` because this HEAD has no `smoke`
subcommand in `parseDashboardOptions`; it is not counted as a passing smoke
test. The direct startup/API smoke above is the measured replacement.

## Write boundary

No real HH application, chat reply, test submission, resume touch or job-search
status update was executed. Existing gateway and reconciliation regression
tests remain green, including dry-run/disabled capability, nonce reuse,
staleness, knowledge-hash changes, duplicate prevention, ambiguous transport,
serialized writes and write limits.
