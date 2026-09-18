# Reset baseline

Дата проверки: 2026-09-19. Целевой `HEAD` и `origin/main` на старте:
`fe15ef0ff9386be5ef2b2a878996aad952d161ef`.

`HEAD == origin/main` подтверждено. Рабочее дерево уже содержало незакоммиченные
изменения browser-session; они сохранены и расширены. Существующий `out/` не
трогался и добавлен в `.gitignore`.

## Phase 0

| Проверка | Результат |
|---|---|
| `gofmt` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |

## Inventory

Статус `WORKING` здесь означает наличие покрытого deterministic/fake path; live
HH без актуальной сессии не объявляется рабочим.

| Subsystem | Exists | Tests | Real validated | Current transport | Status |
|---|---:|---:|---:|---|---|
| Cookie loading | yes | yes | parser only | Netscape → Playwright context | PARTIAL |
| Authentication doctor | yes | partial | expired session | Playwright navigation | PARTIAL |
| Resume registry | yes | yes | no | existing read path | PARTIAL |
| Search planner | yes | yes | no | BrowserHHClient for hh.ru | PARTIAL |
| HH vacancy discovery | yes | yes | blocked by auth | BrowserHHClient | PARTIAL |
| Dedup | yes | yes | no | business logic | WORKING |
| Vacancy detail | yes | yes | blocked by auth | BrowserHHClient | PARTIAL |
| Responded detection | yes | yes | no fresh controls | browser preflight parser | PARTIAL |
| Application preflight | yes | yes | blocked by auth | browser read, no writes | PARTIAL |
| Resume router | yes | yes | no | business logic | PARTIAL |
| AI assessment | yes | yes | no live candidate run | existing AI adapter | PARTIAL |
| Hard requirement evaluator | yes | yes | no live candidate run | deterministic Go | PARTIAL |
| Cover letter | yes | yes | no live candidate run | existing usecase | PARTIAL |
| Application preview | yes | yes | no live candidate run | dry-run path | PARTIAL |
| Write gateway | yes | yes | no live write | explicit gateway | PARTIAL |
| Application submission | yes | yes/fake | not executed | write gateway | NOT_VALIDATED |
| Reconciliation | yes | yes/fake | not executed | targeted readback | PARTIAL |
| Employer chat | yes | yes | no | existing Chatik read path | PARTIAL |
| Resume touch | yes | yes/fake | not executed | write gateway | NOT_VALIDATED |
| Job status sync | yes | yes/fake | not executed | write gateway | NOT_VALIDATED |
| Dashboard | yes | yes | no live session | local read model | PARTIAL |
| Airtable integration | no evidence found | no | no | — | NOT_VALIDATED |
| BrowserHHClient | yes | yes/fake | auth blocked | Playwright | PARTIAL |

The previous CDP implementation was replaced at the transport boundary. The
BrowserHH reader exposes only vacancy search/detail/application reads and fresh
preflight reads. It has no POST or form-submit method. Legacy HTTP remains for
local fixtures and unsupported Chatik paths; real `hh.ru` web reads select the
browser automatically when `cookies.txt` exists.
