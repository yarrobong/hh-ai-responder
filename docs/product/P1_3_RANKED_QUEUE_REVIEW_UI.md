# P1.3 — Ranked Queue Projection & Review UI

Статус: implementation complete; verification recorded below.

Дата: 2026-09-11

## 1. Цель

P1.3 добавляет рабочую очередь вакансий поверх P1.2. Очередь помогает выбрать следующий просмотр, но не принимает решение об отклике и не выполняет HH write.

Границы:

- PostgreSQL остаётся source of truth для вакансий, eligibility, ranking, freshness и review state.
- Одна загрузка очереди вычисляет P1.2 read model один раз, затем выполняет классификацию секций, фильтрацию, подсчёт и pagination над одним snapshot.
- `MATCH`/`REVIEW_REQUIRED`/`REJECT` не превращаются в автоматическую подачу отклика.
- GET очереди не помечает вакансию просмотренной и не запускает AI.
- локальные review-действия не обращаются к HH и не меняют application state.

## 2. Модель секций

Секции — это workflow projection поверх канонического P1.2 comparator:

| Секция | Смысл |
| --- | --- |
| `to_review` | rankable-вакансии без закрывающего состояния, которые ещё стоит проверить |
| `worth_another_look` | просмотренные/интересные вакансии либо dismissed-вакансии, изменившиеся после решения |
| `stretch_manual_review` | stretch/unlikely или требующие ручной проверки fit/eligibility |
| `closed_excluded` | application-linked, не rankable, ineligible или unavailable |

Приоритет секции фиксирован: закрытое/исключённое состояние проверяется раньше review state. Поэтому уже связанные с application вакансии не возвращаются в «Вернуться позже» только из-за старого review state.

## 3. Projection и bounded DTO

`internal/vacancyranking.QueueItem` содержит только данные карточки:

- id, external id, title, company, salary/location/work format;
- eligibility, fit band, base rank score, confidence, analysis state;
- bounded `positive_reasons` (до 3), `concerns` (до 2), `unknowns` (до 3), `hard_reasons` (до 3);
- `unknown_count`, review/application state, freshness, changed-since-review и section.

Полное описание вакансии не входит в список. Detail endpoint дополнительно возвращает полный набор explainability reasons и legacy evidence. Неизвестные поля не скрываются и не трактуются как отсутствие факта.

## 4. API

### Ranked queue

`GET /api/vacancies/ranked`

Поддерживает:

- `section=to_review|worth_another_look|stretch_manual_review|closed_excluded`;
- `eligibility=eligible|review_required|ineligible|unavailable`;
- `fit_band=compatible|stretch|unlikely|hard_incompatible`;
- `review_state=unseen|seen|interesting|dismissed|prepared|applied`;
- `analysis_state=complete|partial|insufficient_evidence`;
- `query` или совместимый alias `search`;
- `limit` (по умолчанию 25, максимум 100) и `offset`.

Ответ содержит `items`, filtered `total`, `counts` по полному snapshot, `has_more`, `next_offset`, `limit`, `offset` и `algorithm_version`.

### Detail

`GET /api/vacancies/:id/ranking` возвращает vacancy detail, ranking detail и review event history. Legacy `GET /api/vacancies` и `GET /api/vacancies/:id` сохранены.

### Local review actions

`POST /api/vacancies/:id/review/seen`

`POST /api/vacancies/:id/review/interesting`

`POST /api/vacancies/:id/review/dismiss`

Для dismiss можно передать JSON `{ "reason": "..." }`, ограниченный 500 символами. Действия идемпотентны на уровне PostgreSQL review repository, не создают HH write и после записи возвращают актуальную карточку.

## 5. Application boundary

Очередь не добавляет новый путь отправки отклика. Existing application preparation/preflight/approve/send flow остаётся отдельным. Открытие карточки и review-действия не устанавливают `prepared`, не создают application attempt и не отправляют отклик. Detail view явно сообщает эту границу и даёт перейти к существующим vacancy/application экранам для контролируемой ручной работы.

## 6. UI

Страница `/vacancies` по умолчанию показывает ranked queue. Legacy full list доступен по вкладке `All vacancies` и остаётся диагностическим fallback, если ranked read model недоступен.

Карточка показывает:

- раздел, eligibility, fit band, confidence и analysis state;
- secondary base-rank score;
- bounded explainability: «Почему посмотреть», ограничения и неизвестные поля;
- freshness label (`first seen today`, `first seen Nd ago` или `freshness unknown`);
- действия «Отметить просмотренной», «Интересна», «Скрыть».

Новая карточка не помечается просмотренной автоматически. В UI нет ложной метки «новая сегодня» при неизвестной freshness.

## 7. Изменённые компоненты

- `internal/vacancyranking/queue.go` — canonical queue projection, sections, counts, filters, pagination и bounded DTO.
- `internal/runtime/ranked_queue.go` — HTTP parsing, detail и local review actions.
- `internal/runtime/dashboard_server.go` — routes и dependency wiring.
- `internal/runtime/dashboard_command.go` — PostgreSQL production wiring; JSON fallback сохранён.
- `internal/runtime/postgres_vacancy_review_repository.go` и `internal/adapters/storage/postgres/vacancy_freshness_review.go` — persisted review/freshness source.
- `web/app.js`, `internal/runtime/web/app.js` — ranked queue, filters, detail explainability и review actions.
- `web/styles.css`, `internal/runtime/web/styles.css` — compact queue card styles.

## 8. Тесты

Добавлены:

- projection tests: canonical ordering, pagination без дублей, section precedence, application-linked closed state, unknown freshness и bounded reasons;
- API test: ranked list, detail-compatible review projection, local interesting action и invalid route;
- optional PostgreSQL queue diagnostic;
- optional PostgreSQL endpoint benchmark.

Действия review не используют HH API. Benchmark и diagnostic активируются только явными env-флагами и используют read-only PostgreSQL запросы.

## 9. PostgreSQL snapshot

На текущем dataset queue diagnostic:

```text
total=279
first_page=25
to_review=11
worth_another_look=0
stretch_manual_review=156
closed_excluded=112
rankable=167
eligible=3
review_required=248
ineligible=28
unavailable=0
application_linked=98
```

Нулевой `worth_another_look` соответствует текущему состоянию review rows: в dataset нет просмотренных/интересных вакансий, которые можно было бы вернуть в эту секцию.

## 10. Performance smoke

Для 10 запросов `GET /api/vacancies/ranked?limit=25` через local dashboard httptest:

```text
payload_bytes=42338
warm median=43.791µs
warm p90=106.25µs
cold max=561.993875ms
```

Warm timings используют существующий 15-second dashboard read cache; cold max включает построение PostgreSQL-backed read model. Payload ограничен 25 bounded карточками и не содержит полных описаний.

## 11. Browser validation

Local dashboard был запущен с `HH_DRY_RUN=true` и `HH_WRITE_ENABLED=false`.

- 5 последовательных загрузок `/vacancies` отдали по 25 queue cards;
- карточка показала section, eligibility, fit, confidence, analysis, score и freshness unknown;
- detail view показал deterministic result, positive reasons, concerns/unknowns, review actions и application boundary;
- браузерный smoke-test не нажимал state-changing review buttons.

## 12. Verification commands

Обязательный финальный набор:

```bash
gofmt -w .
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go build ./cmd/hh-ai-responder
git diff --check
node --check web/app.js
node --check internal/runtime/web/app.js
```

Дополнительные queue checks:

```bash
go test -count=1 ./internal/vacancyranking
go test -count=1 ./internal/vacancyreview
go test -count=1 ./internal/runtime -run 'TestRankedQueue'
```

## 13. Safety closeout

- dry-run protection не менялась и остаётся на HH write gateway;
- queue GET и local review POST не вызывают HH write path;
- нет auto-apply, auto-message или auto-test на queue load;
- неизвестные critical state остаются `review_required`/`REVIEW_REQUIRED` и не применяются автоматически;
- secrets, cookies и private raw AI bodies не добавлялись;
- existing legacy vacancy API и JSON storage fallback сохранены.

## 14. Remaining bounded gaps

- P1.3 не меняет отсутствие freshness/review history в старых строках; эти данные появляются только после соответствующих read sync и local review actions.
- Прямой application preparation остаётся в существующем flow, потому что добавление нового prepare handler без утверждённого preflight/approval contract было бы небезопасным.
- `All vacancies` намеренно сохраняется как legacy diagnostic view; ranked queue является default рабочим представлением.
