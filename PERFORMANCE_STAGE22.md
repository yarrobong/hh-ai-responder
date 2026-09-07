# Stage 22 — Performance Hardening / Fast Career Agent

## Условия измерений

Apple M1, macOS arm64, Go 1.25.0. До изменения поведения были выполнены
baseline-тесты, CPU/heap profiles и benchmarks. Все запуски:
`HH_WRITE_ENABLED=false`, `HH_DRY_RUN=true`.

Локальные замеры используют **временную приватную копию** настоящего dataset.
Baseline и after используют исходные 261 conversations / 791 messages /
98 applications / 118 vacancies / 13 write actions / 50 write audit events.
Исходные 19 JSON-файлов проверены SHA-256 до и после работы. Их содержимое
не публикуется. HH contract проверен настоящими GET-запросами; cookies также
копировались во временный файл. HH writes: **0**.

HTTP timings ниже — server-side `httptest` handler, включая JSON response,
без времени браузера и DOM. Это отдельные воспроизводимые замеры, не p95/SLA.
Before benchmark: 1 iteration; after: 3 iterations, `-benchmem`.
Время HH зависит от сети и throttling; live и synthetic данные разделены.

## BEFORE → AFTER: локальная часть

| Операция | Before | After | Повторный GET |
|---|---:|---:|---:|
| Load Dashboard / stores | 39.94 ms | 43.38 ms | — |
| GET /, HTML shell | 0.167 ms | 0.071 ms | 0.006 ms |
| GET /inbox, HTML shell | 0.007 ms | 0.005 ms | 0.004 ms |
| GET /api/dashboard | 1,386.82 ms | 462.46 ms | 0.112 ms |
| GET /api/inbox | 1,451.34 ms | 138.97 ms | 0.227 ms |
| GET conversation detail API | 2,669.13 ms | 11.88 ms | 11.43 ms |
| GET /api/applications | 21.00 ms | 21.80 ms | 0.098 ms |
| GET /api/vacancies | 26.80 ms | 26.64 ms | 0.142 ms |
| GET /api/knowledge | 0.169 ms | 0.219 ms | 0.061 ms |
| GET /api/health | 2,662.71 ms | 418.16 ms | 416.95 ms |

Порядок API-запросов одинаков: Overview, Inbox, Applications, Vacancies,
Knowledge, Health, conversation. Поэтому Inbox after уже использует consistency
projection, построенную Overview. Первый Overview измеряет холодное построение.
HTML shell всегда был быстрым; заметное ожидание находилось в API.
Conversation/action cards и Health не используют response cache.

Ускорение server-side conversation: **~225×**, Inbox: **~10.4×**, Health:
**~6.4×**. Startup, Applications и Vacancies уже были быстрыми; значимого
ускорения их первого чтения не заявляется.

Synthetic safety fixture after, без HH/LLM:

- edit draft, с сохранением и повторной проверкой: **10.82 ms**;
- approval: **18.10 ms**;
- local preflight с durable action reload и сохранением: **14.66 ms**.

Для этих трёх операций отдельного before-замера нет. Это не live network latency
и не утверждение об ускорении в указанное число раз.

## BEFORE → AFTER: HH reads

| Операция | Before | After |
|---|---|---|
| Targeted `ReadConversation`, synthetic 261 chats, 1 ms handler delay | 435.34 ms, 262 requests | ~1.6 ms, **1 request**, 0 list requests |
| Fresh `ReadConversationState`, тот же fixture | 387.08 ms, 262 requests | ~1.5–1.8 ms, **1 request** |
| Full fixture page, 261 chats | 385.87 ms | ~98–106 ms, 4 workers |
| Live full 261-conversation sync | ранее наблюдалось пользователем ~5m30s | **5m31.03s**, **275 requests** |
| Live incremental Inbox refresh сразу после full sync | отсутствовал | **1m52.64s**, **94 requests** |
| Live targeted preflight read | отдельного live before нет | **1.18–1.20s**, 1 detail GET |
| Live reader initialization | отдельного before нет | **1.71–1.74s**, profile/resume reads |

**Полный sync не ускорился заметно на настоящем HH.** Сохраняется общий
интервал старта запросов 1.2 s. 14 list pages + 261 detail requests = 275 requests;
нижняя граница между первым и последним стартом — 328.8 s. Измеренные 331.03 s
соответствуют этому ограничению. Уменьшать интервал ради результата нельзя.

Новый metadata refresh сделал 14 list requests + 80 detail requests. История
**181** чата переиспользована. Это **~2.94× быстрее** full sync и **на 65.8% меньше
HTTP requests**. У остальных чатов не прошли консервативные условия повторного
использования; их история была запрошена снова. Совпадение list metadata не
считается доказательством безопасности Send.

Реальный contract: `chats.items`, `nextFrom`, `id`, `lastMessage`,
`lastActivityTime`; detail содержит собственный `chat.id`, `resources`, topic и
vacancy references, participant identity, `messages.items`, `hasMore`. В живом
ответе list page содержала 20 чатов. `updated_since`/delta API не выдумывался.

Раньше targeted read вызывал `ReadConversations` и загружал историю всех чатов
просматриваемых страниц. Preflight смотрел только первую страницу, поэтому
старый чат мог вообще не находиться. Теперь используется прямой `chat_data`
по известному numeric chat ID, с проверкой совпадения response identity.

Dashboard fresh preflight сохраняет два независимых targeted reads:
обновление локального conversation, затем проверку gateway. Это консервативное
сохранение прежнего порядка safety checks. Post-Send reconciliation использует
один targeted read; реальный Send не выполнялся, поэтому live end-to-end
`Send → DELIVERY_CONFIRMED` не измерялся. Проверены mock transport и
существующие delivery/nonce/idempotency regression tests.

## Найденные bottlenecks и изменения

1. **Rate limiter и последовательные HH detail reads.** Добавлены 1–8 bounded
   workers, общий лимит одновременных GETs, cancellation и общий 429 cooldown.
   Default — 4. Writes не получили retry или параллельное выполнение.
2. **Targeted чтение фактически было full-page чтением.** Прямой detail GET
   используется sync, preflight и reconciliation. Dashboard и gateway разделяют
   один lazy reader/request limiter.
3. **Conversation detail строил pilot report по всем 261 conversations** и
   consistency snapshot всего Inbox. Теперь расчёт ограничен одним conversation.
4. **Повторная токенизация candidate context.** В baseline CPU profile
   `BuildForReply` занимал ~75% cumulative CPU, `contextTokens` ~49%,
   `contextMatchingNames` ~34%. Нормализация query выполняется один раз для
   списка имён; pure canonical normalization имеет bounded memoization.
   Тест сравнивает прежние правила aliases, Unicode и punctuation.
5. **Повторный display analysis.** Consistency cache keyed by complete
   conversation content + candidate knowledge. GET response cache ограничен
   15 s и 64 entries; consistency cache — 512 conversations. Gateway не читает
   эти caches. Важные action cards пересчитываются каждый раз.
6. **Лишний Inbox lookup для каждой строки.** Timeline берётся из уже прочитанной
   conversation; добавлены индексы conversation ID / HH chat ID, пересоздаваемые
   при публикации snapshot. Маленькие action/application scans не заменялись
   большой новой архитектурой без необходимости.
7. **Сериализация Dashboard на всё время sync.** Fetch идёт вне Dashboard/data
   locks; validated durable merge — отдельно. UI получает 202 и progress,
   может открывать страницы во время сетевого ожидания. Одинаковые jobs
   coalesce; отдельная operation lease подавляет дубли другого процесса.
8. **Persistence.** Исходный sync уже сохранял conversations/applications
   в конце, а не после каждого item: O(N²) disk writes не обнаружены.
   Новый batch reload/merge сохраняет внешние изменения и пишет каждый
   изменившийся store максимум один раз. Unchanged import не обновляет
   conversation timestamp; исправлено сравнение nil/empty metadata map.
   Notifications больше не сохраняются при отсутствии изменений.
9. **Cross-process display freshness.** Перед API read проверяются file identity,
   size, mode, mtime. Atomic replacement с прежним mtime тоже обнаруживается.
   Изменённые knowledge files вызывают один KB reload. Corrupt file блокирует
   ответ, а не возвращает старый cached success. Preflight и Send явно
   перечитывают durable action store.
10. **Observability / UI.** `/api/performance`, SyncResult.performance,
    `/api/sync/status`, progress/requests/sec и `/api/generation`.
    Timer проверяет локальную generation, HH по таймеру не вызывается.
    Отдельные кнопки targeted / metadata / full sync.

## Worker benchmark

261 chats; локальный HTTP fixture с 8 ms delay, без HH rate limiter.
Это throughput experiment, не обещание такой скорости на HH.

| Workers | Duration |
|---:|---:|
| 1 | 2,670 ms |
| 2 | 1,350 ms |
| 4 | 687 ms |
| 8 | 351 ms |

4 workers дают ~3.9× против 1 и ограничивают pressure. 8 ускоряют только
fixture; настоящий HH уже ограничен 1.2 s interval, поэтому 8 не выбраны.
429 tests проверяют Retry-After, cancellation, отсутствие retry для writes и
соблюдение общего интервала параллельными readers.

## Local benchmarks / allocations

| Benchmark | Before ms/op | After ms/op | Before B/op → After B/op | Allocations before → after |
|---|---:|---:|---:|---:|
| Load ConversationStore | 9.92 | 9.95 | 4,187,632 → 4,241,792 | 10,154 → 10,183 |
| Build Inbox | 1,443.33 | 130.98 | 226,971,096 → 20,521,872 | 4,106,471 → 79,666 |
| Build conversation view | 2,661.75 | 10.83 | 423,401,088 → 2,870,034 | 8,140,077 → 15,099 |
| Eligibility / 261 | 1,325.84 | 389.24 | 210,934,640 → 84,015,805 | 4,040,051 → 421,395 |
| Local write-status report | 0.247 | 0.131 | 119,104 → 108,720 | 273 → 216 |
| Select current action / 262 fixture actions | — | 0.449 | 329,234 | 10 |
| Idempotent JSON batch merge / 261 fixture chats | — | 22.07 | 7,112,093 | 41,401 |
| Reconciliation / real local copy, dry-run | — | 40.00 | 10,323,858 | 83,479 |

Local write-status в таблице — построение отчёта. Отдельно проверен настоящий
CLI `hh write-status`: первый запуск после build **1,420.69 ms**, следующие —
**41.35 / 40.75 ms**, exit code 0. До изменения кода OS-process baseline не
снимался; причину первого медленного запуска отдельно не профилировали.
Первый Inbox API after: 0 store reloads, 0 extra timeline store lookups; before
выполнял 261 дополнительных timeline lookups с JSON clone на строку. Conversation
API after сохраняет 2 намеренных durable reloads: actions и audit. Health — также
2. Полный JSON parsing не являлся главным baseline bottleneck: store load ~10 ms.
Количество всех внутренних JSON clones/сканов отдельно не инструментировалось;
allocations и CPU profiles дают измеренное сравнение вместо выдуманных counters.

## Correctness / validation

- Dataset parity: **PASS**, byte-for-byte SHA-256 исходных 19 JSON files.
- Counts: **261 / 791 / 98 / 118 / 13 / 50**, без изменений исходного dataset.
- Existing matching, relevant knowledge, stale, terminal, nonce, duplicate,
  delivery and dry-run tests: **PASS**.
- External message + local annotations survive batch merge; repeat import is
  idempotent and produces zero conversation writes: **PASS**.
- Targeted detail/preflight: 1 request, no chat listing: **PASS**.
- Same full/targeted operation coalescing; Dashboard reads during sync: **PASS**.
- External atomic replacement invalidates display cache; corruption fails closed:
  **PASS**.
- Analysis runs outside data file locks: **PASS**.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`,
  `git diff --check`, `node --check web/app.js`: **PASS**.
- Real HH applications/tests/messages/resume/status changes: **0**.

## Оставшиеся ограничения

- Первый запуск CLI после build занял 1.42 s; следующие ~41 ms. Поэтому
  target <300 ms подтверждён для повторных запусков, но не для первого.
- Настоящий full sync остаётся ~5m31s при текущем HH rate limit. Для повседневной
  работы используйте targeted read; для Inbox — metadata refresh.
- Last-message metadata не доказывает отсутствие правок старой истории.
  Metadata cache предназначен только для display, ограничен 15 минутами и
  обходится при полном sync/preflight/reconciliation. Чаты с неполной/неизвестной
  историей перечитываются; полноценный delta/history protocol не добавлялся.
- Vacancy list snippets не являются надёжной revision для полного description.
  Дополнительные vacancy detail reads не пропускаются на основе догадки.
  VacancyAnalyzer остаётся deterministic и запускается для new/changed input.
- GET Health / eligibility всё ещё вычислительно тяжелее простых страниц:
  примерно 0.4 s. Следующий шаг требует отдельного профиля, не новой СУБД.
- Gateway serialized safety section сохранён, включая независимый fresh read.
  Отдельный preflight может кратковременно занять Dashboard; многоминутный
  full sync больше его не занимает. LLM draft generation также остаётся
  явной отдельной операцией, без speculative background generation.
- JSON stores по-прежнему имеют atomicity на файл, не на весь набор. Partial
  multi-file failure возвращает ошибку и требует reconciliation. SQLite/
  PostgreSQL migration не проводилась; профиль не обосновывает её сейчас.
- Background HH monitor дополнительно не включался. Никакого постоянного
  сетевого polling/автогенерации drafts этим stage не добавлено.
