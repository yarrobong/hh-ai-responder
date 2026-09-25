# hh-ai-responder

Инструмент для автоматических откликов на HH.RU и общением с работодателями с помощью AI.

Что умеет:

- поднимать резюме;
- откликаться на вакансии;
- писать сопроводительные;
- решать тесты;
- готовить ответы работодателям и отправлять только вручную подтверждённый reply/follow-up через Write Gateway;
- готовить правдивые сопроводительные и ответы на основе данных резюме;
- чистит чаты от отказов.

## Запуск и компиляция

Компиляции:

```sh
go build ./cmd/hh-ai-responder
```

В [релизах](https://github.com/s3rgeym/hh-ai-responder/releases/latest) можно скачать готовую версию под все целевые платформы: Windows, Linux, Darwin (Mac) и Android (для запуска через Termux).

Для работы с HH нужен только актуальный экспорт авторизованной web-сессии:
`cookies.txt` в строгом Netscape format. Cookie-web transport использует
единый GET-only/POST-only HTTP session: значения cookies не выводятся, а
безопасные `Set-Cookie` обновления сохраняются атомарно в private-файл с
правами `0600`.

Сначала запустите read-only browser doctor:

```sh
./hh-ai-responder career-agent browser-doctor --headed
```

Doctor проверяет HH home/account, `/applicant/my_resumes` и одну vacancy page
только через GET/navigation. При истёкшей или challenged-сессии он завершает
работу сообщением:

```sh
HH session requires refresh.
Replace cookies.txt with a fresh authenticated export and run again.
```

После `AUTH_OK` запускайте безопасный bounded pipeline:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent run
```

`browser-doctor` выполняет bounded GET-проверки через тот же cookie session,
который используется web application transport. Флаги `--headless` и
`--headed` сохранены для совместимости CLI, но cookie-web doctor не запускает
браузер и не выполняет обход challenge.

Для запуска приложения можно указать ссылку для поиска по вакансиям:

```sh
./hh-ai-responder -c ./cookies.txt -u "https://hh.ru/search/vacancy?..."
```

<img width="834" height="600" src="https://github.com/user-attachments/assets/9a23879b-44a6-446f-b6d3-f496d57bc234" />

Из данной ссылки берется тот же базовый адрес сайта, чтобы не было лишних редиректов и дополнительных запросов. Выбор различных фильтров в интерфейсе приводит к изменению параметров **Query String** ссылки — они будут применяться и при работе через программу, если ее скопировать и передать. Так же настоятельно советую почитать про [специальный язык запросов](https://hh.ru/article/1175) для более точного поиска.

### Поиск без ссылки `-u`

Если флаг `-u` не передан:

1. Базовый домен берётся из cookie `redirect_host` для `.hh.ru`.
2. Если такой cookie нет — используется `https://hh.ru`.
3. Если параметры поиска отсутствуют — из доступных резюме и Candidate Profile строится bounded набор explainable search profiles; legacy `resume=<id_выбранного_резюме>` остаётся только безопасным fallback при пустом/неполном профиле.

Явные `HH_SEARCH_URL`/`HH_SEARCH_URLS` имеют приоритет над auto-planner и
сохраняют `area`, `resume`, порядок, период и размер страницы; в отчёте такие
профили имеют `profile_type=MANUAL` и `reason=MANUAL_PROFILE`. Auto-planner
использует bounded детерминированные RU/EN role aliases только при trusted
evidence соответствующей role family. Discovery (`raw_hits`,
`distinct_discovered`, page caps) и population, вошедшая в RESET-6 router
(`processed_by_router`, route outcomes и yields), считаются раздельно.

Multi-phrase fallback через literal `OR` отключён, пока controlled read-only
проверка BrowserHHClient не докажет семантику альтернатив HH; URL encoding или
mock parser сами по себе такой семантикой не являются. Per-profile telemetry
включает raw hits, distinct vacancies, overlap и union contribution.

### Career Agent / Shadow Mode

Для всех доступных резюме можно автоматически построить ограниченный набор
объяснимых поисковых профилей, дедуплицировать результаты и получить JSON-отчёт
без единой записи в HH:

```sh
./hh-ai-responder career-agent --shadow
```

Отчёт сохраняется в `career_agent_latest.json` (или в `HH_CAREER_AGENT_RESULT`),
а компактная человекочитаемая версия — рядом с суффиксом `.md`. В JSON для
каждой unique vacancy есть ровно один `terminal_outcome`; summary содержит
`accounting_pass`, stage counters и `shadow_write_count`.
Список нормализованных резюме и локальное включение/отключение:

```sh
./hh-ai-responder career-agent resumes
./hh-ai-responder career-agent resume disable --id hh-resume-<hash>
./hh-ai-responder career-agent resume enable --id hh-resume-<hash>
```

Операторский feedback сохраняется отдельно и не меняет Candidate Knowledge:

```sh
./hh-ai-responder career-agent feedback --vacancy 123 --type GOOD_MATCH --resume-id hh-resume-<hash>
```

`career-agent --canary` существует как строго ограниченный live-путь, но требует
явных `HH_DRY_RUN=false` и `HH_WRITE_ENABLED=true`; по умолчанию команда всегда
работает в shadow mode.

#### Operational daily workflow

Phase 4 объединяет vacancy discovery и employer communication в один durable,
read-only application service:

```sh
./hh-ai-responder career-agent daily
./hh-ai-responder career-agent daily --json
```

Повторный запуск за UTC-день возвращает durable replay; параллельные процессы не
запускают одну и ту же daily работу одновременно. Результаты имеют стабильные
коды `SUCCESS`, `PARTIAL_SUCCESS` или `FAILED`. Dashboard (`./hh-ai-responder
dashboard`) содержит Career Agent Control Center, unified Attention Queue и
безопасную кнопку `Run daily`. Ни CLI, ни dashboard, ни scheduler не получают
`HHWriteGateway`: approval и отправка остаются отдельным ручным flow.

Периодический scheduler в dashboard default-off; для явного включения задайте
`HH_CAREER_AGENT_DAILY_ENABLED=true` и при необходимости
`HH_CAREER_AGENT_DAILY_INTERVAL=24h`.

Подробности и PostgreSQL parity описаны в
[`docs/OPERATIONAL_CAREER_AGENT.md`](docs/OPERATIONAL_CAREER_AGENT.md).

Для контролируемого application pilot используется read-only поиск. `--max-scan`
ограничивает число unique вакансий, проверенных дешёвыми стадиями, а
`--max-candidates` — число новых вакансий, допущенных до detail/router/AI:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent pilot --search --max-scan 100 --max-candidates 20
```

For manual review of one explicitly trusted enabled resume, use an exact resume
registry/provider/hash identity with a specific vacancy. This never lets the
router choose a different resume and never produces an automatic send state:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent pilot --vacancy <id> --resume-id <identity>
```

When fresh read-only preflight proves that the vacancy does not require a
cover letter, the targeted pilot may intentionally skip optional cover-letter
generation with `--omit-optional-cover-letter`. The flag is accepted only
with an explicit `--vacancy` and `--resume-id`; required or unknown letter
requirements fail closed.

Уже подтверждённые отклики пропускаются до detail и AI; при готовом результате
команда останавливается на `PILOT: READY_FOR_EXPLICIT_SEND` и не выполняет POST.

### Controlled application POST (API или cookie-web)

Для одного явно выбранного vacancy/resume доступен отдельный controlled путь.
Текущие CLI-команды `hh-api approval`, `hh-api apply` и `hh-api apply-batch`
сохраняются. `HH_TRANSPORT=api` использует существующий OAuth/API adapter;
`HH_TRANSPORT=browser` использует cookies, требует `cookies.txt` и не требует
OAuth-конфигурации или API token.

```sh
HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
  ./hh-ai-responder hh-api approval export \
  --pilot ./career-agent-pilot.json --out ./api-approval.json \
  [--letter-file ./reviewed-letter.txt]

HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
  ./hh-ai-responder hh-api approval review \
  --pilot ./career-agent-pilot.json --out ./api-manual-approval.json \
  [--letter-file ./reviewed-letter.txt]

HH_TRANSPORT=browser HH_DRY_RUN=true HH_WRITE_ENABLED=false \
  ./hh-ai-responder hh-api apply 137112468 \
  --resume-id <provider-resume-id> --approval-file ./api-approval.json
```

Сначала automatic `approval export` принимает только `PILOT:
READY_FOR_EXPLICIT_SEND` с решением `MATCH`, точно связывает vacancy и
выбранное provider resume,
нормализует только внутреннее представление
`hh-resume-provider-id-...` и атомарно создаёт private approval-файл. Он не
выполняет запросов к HH. Dry-run выполняет approval validation и свежий GET-only preflight и может
вывести `WOULD_APPLY`, но не создаёт mutation adapter и не отправляет POST.
При явном `--letter-file` automatic MATCH approval сохраняет точные байты
операторской версии письма и вычисляет новый `ContentHash`; vacancy, resume,
nonce, freshness, `READY_FOR_EXPLICIT_SEND` и `MATCH` остаются из уже
проверенного pilot. Без `--letter-file` письмо pilot сохраняется без изменений.
Команда `approval review` — отдельное явно вызванное локальное действие для
`MANUAL_REVIEW_BEFORE_SEND`: она принимает только `REVIEW_REQUIRED` с
AI-рекомендацией `UNCERTAIN`, без hard missing/unknown, выпускает новый nonce,
сохраняет исходную AI-позицию и не делает запросов к HH. Если указан
`--letter-file`, его содержимое связывается побайтно; без него сохраняется
письмо pilot. Это не превращает `REVIEW_REQUIRED` в автоматический `MATCH`.
`--approval-file` обязателен: default/stale artifact автоматически не
подбирается. Артефакт должен точно соответствовать vacancy и provider resume,
содержать `READY_FOR_EXPLICIT_SEND`, решение `MATCH`, nonce, content hash и
свежий `preview_fresh_at`. Письмо сохраняется побайтно; пустое/отсутствующее
письмо разрешено только когда свежий HH preflight достоверно сообщает
`response_letter_required=false`.

Единственный live-вызов имеет тот же точный синтаксис и требует одновременно
`HH_TRANSPORT=api` или `HH_TRANSPORT=browser`,
`HH_DRY_RUN=false HH_WRITE_ENABLED=true`. В browser mode approval должен
содержать точный `browser_resume_hash`, подтверждённый свежим `/applicant/my_resumes`
preflight; provider resume ID и browser resume hash не смешиваются. За один invocation
разрешена ровно одна application mutation; существующие глобальные и дневные
лимиты Write Gateway сохраняются, но этот путь не добавляет отдельную
постоянную политику «один отклик в день». Команда не подключена к поиску,
bulk career-agent или переключению резюме.

После `SUCCESS`, `ALREADY_APPLIED` или `UNKNOWN_SEND_RESULT` команда делает
обязательный targeted GET reconciliation. Итогом могут быть
`POST_SUCCESS_RECONCILED`, `POST_SUCCESS_UNCONFIRMED`,
`ALREADY_APPLIED_RECONCILED`, `UNKNOWN_SEND_RECONCILED_SUCCESS` или
`UNKNOWN_SEND_UNRESOLVED`. Timeout/reset/EOF и неопределённый 5xx не повторяются;
durable vacancy lock сохраняется до reconciliation. Валидация и тесты этого
пути не выполняют реальный HH POST.

Для нескольких заранее проверенных откликов существует только явный bounded
batch-путь:

```sh
HH_TRANSPORT=browser HH_DRY_RUN=true HH_WRITE_ENABLED=false \
  ./hh-ai-responder hh-api apply-batch \
  --approval-file ./api-approval-a.json \
  --approval-file ./api-approval-b.json \
  --approval-file ./api-approval-c.json
```

Принимается от одного до трёх explicit approval-файлов; пути и vacancy не
могут дублироваться. Batch использует один application execution service и
один `HHWriteGateway`, выполняет свежий GET-only preflight непосредственно
перед возможным transport для каждого item и reconciles его до перехода к
следующему. Pre-send block может пропустить item, а uncertain transport,
delivery или persistence останавливает batch без retry. `HH_DRY_RUN=true`
строит план с `WOULD_ATTEMPT`, не расходует nonce и не выполняет HH writes.
Live batch POST в validation workflow не выполняется.

Перед Shadow или любым другим HH read-path проверьте доступ без discovery:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false ./hh-ai-responder hh-doctor
```

`hh-doctor` — legacy HTTP diagnostic, выполняющий только два bounded GET:
публичный vacancy search и authenticated `GET /applicant/my_resumes`. Для
основного HH web transport используйте `career-agent browser-doctor`. `hh-doctor`
показывает безопасные metadata
запроса, redirect chain, status, content type, размер ответа, классификацию
403 и только имена cookies; значения cookies и секретные headers не выводятся.
При недоступном authenticated read `career-agent --shadow` останавливается до
discovery и не выполняет HH writes.

Для безопасной диагностики web preflight можно проверить две локально
подтверждённые вакансии и выбранные UNKNOWN-вакансии. Команда выполняет
только GET через существующий cookie jar и печатает только metadata страниц:

```sh
HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
  ./hh-ai-responder career-agent web-trace \
  --known 123,456 --unknown 789,790,791,792,793
```

Значения `--known` и `--unknown` нужно подставить явно; команда не угадывает
состояние отклика по отсутствию кнопки.

Используйте флаг `-h` для справки.

По умолчанию приложение использует модель `llama3:8b`, запущенную на `http://localhost:11434` (например через **Ollama**).

## Переменные окружения

Аргументы могут передаваться не только через командную строку, но и через переменные окружения.

Скопируйте пример файла, содержащего переменные окружения, и отредактируйте его:

```bash
cp example.env .env
```

Приложение автоматически грузит переменные окружения из `.env` в текущем рабочем каталоге. У аргументов, переданных через командную строку, более высокий приоритет чем у переменных окружения.

Поддерживаемые переменные приложения:

| Переменная             | Флаг                 | Назначение                                                             |
| ---------------------- | -------------------- | ---------------------------------------------------------------------- |
| `HH_SEARCH_URL`        | `-u`                 | URL для поиска вакансий.                                               |
| `HH_SEARCH_URLS`       | —                    | Несколько URL поиска через `||`; если пусто, используется `HH_SEARCH_URL`. Параметры `area` и `resume` каждого URL сохраняются; задаются также `order_by=publication_time`, `search_period=7`, `items_on_page=50`. |
| `HH_SEARCH_PERIOD_DAYS` | `--search-period-days` | Период свежести автоматически построенного и manual HH search profile; по умолчанию `7`. |
| `HH_MAX_SEARCH_PROFILES` | `--max-search-profiles` | Верхняя граница автоматически построенных search profiles; по умолчанию `16`. |
| `HH_MAX_SEARCH_PAGES_PER_PROFILE` | `--max-search-pages-per-profile` | Положительная read-only граница страниц HH на один search profile; по умолчанию `3`. При остановке отчёт помечается `MAX_SEARCH_PAGES_PER_PROFILE` и `discovery_complete=false`. |
| `HH_MAX_SEARCH_PAGES_PER_RUN` | `--max-search-pages-per-run` | Положительная глобальная read-only граница страниц HH за запуск; по умолчанию `48`. Незапущенные профили явно помечаются `MAX_SEARCH_PAGES_PER_RUN`. |
| `HH_BROWSER_PROFILE`    | `--browser-profile` | Persistent headed Chrome/Chromium profile; по умолчанию `.hh-browser-profile`, файл не коммитится. |
| `HH_BROWSER_TRACE_VACANCY` | `--browser-trace-vacancy` | Одна явно заданная HTTPS vacancy URL для безопасного Browser/Go HTTP trace. |
| `HH_BROWSER_TRANSPORT` | `--browser-transport` | `auto` (browser для реального hh.ru с cookies), `browser` или legacy `http`. |
| `HH_BROWSER_HEADLESS` | `--browser-headless` | Запуск Playwright browser headless; по умолчанию `false`. |
| `HH_TRANSPORT` | `--hh-transport` | Основной HH transport: `browser` (по умолчанию, cookies-only web reads/writes), `api` или `auto` для остальных read-сценариев. Для controlled `apply`/`apply-batch` допустимы `browser` и `api`; browser mode не требует OAuth. |
| `HH_API_BASE_URL` | — | Базовый URL HH API; по умолчанию `https://api.hh.ru`. |
| `HH_OAUTH_AUTHORIZE_URL` | — | URL OAuth authorize; по умолчанию `https://hh.ru/oauth/authorize`. |
| `HH_OAUTH_TOKEN_URL` | — | URL OAuth token; по умолчанию `https://api.hh.ru/token`. |
| `HH_OAUTH_CLIENT_ID` | — | ID OAuth-клиента оператора; по умолчанию пусто. Не добавляйте рабочее значение в Git. |
| `HH_OAUTH_CLIENT_SECRET` | — | Секрет OAuth-клиента оператора; по умолчанию пусто. Не логируется и не коммитится. |
| `HH_OAUTH_REDIRECT_URI` | — | Redirect URI OAuth-клиента; по умолчанию пусто. |
| `HH_OAUTH_USER_AGENT` | — | User-Agent для OAuth/API оператора; по умолчанию пусто. |
| `HH_API_TOKEN_FILE` | — | Локальный файл OAuth token; по умолчанию `.hh-api-token.json`, файл не коммитится. |
| `HH_AI_BASE_URL`       | `-ai-base-url`       | Базовый URL OpenAI-compatible API.                                     |
| `HH_AI_MODEL`          | `-ai-model`          | Модель AI.                                                             |
| `HH_AI_API_KEY`        | `-ai-api-key`        | API key для OpenAI-compatible API.                                     |
| `EMBEDDING_PROVIDER`   | —                    | Явно включает semantic retrieval: `openai` или `openai-compatible`. Настройки embeddings независимы от `HH_AI_*`. |
| `EMBEDDING_BASE_URL`   | —                    | Базовый URL embedding API; если пуст, используется `HH_AI_BASE_URL` для обратной совместимости. |
| `EMBEDDING_API_KEY`    | —                    | Ключ embedding API; если пуст, используется `HH_AI_API_KEY` для обратной совместимости. Не выводится в status/help. |
| `EMBEDDING_MODEL`      | —                    | Embedding model; по умолчанию `text-embedding-3-small`. |
| `EMBEDDING_DIMENSIONS` | —                    | Явный размер embedding-вектора; по умолчанию `1536`, допустимо `1..16000`. Ответ другого размера отклоняется без padding/truncation. |
| `HH_LETTER_PROMPT`     | `-letter-prompt`     | Дополнительные инструкции для сопроводительного письма.                |
| `HH_SOLUTION_PROMPT`   | `-solution-prompt`   | Дополнительные инструкции для решения тестов.                          |
| `HH_CHAT_REPLY_PROMPT` | `-chat-reply-prompt` | Дополнительные инструкции для ответов в чатах с работодателями.        |
| `HH_CONTACTS`          | `-contacts`          | Контакты (телефон, email и т.д.), которые будут добавлены в сообщение. |
| `HH_GITHUB_URL`        | `-github-url`        | Настроенная ссылка на GitHub кандидата; пустое значение не добавляет ссылку. |
| `STORAGE_BACKEND`      | `-storage-backend`   | Единый backend career data (`Vacancy`/`Application`/`Conversation`): `json` по умолчанию или явно включаемый `postgres`. |
| `DATABASE_URL`         | `-database-url`      | PostgreSQL DSN; обязателен только при `STORAGE_BACKEND=postgres`. Не логируется. |
| `HH_CANDIDATE_ID`      | `-candidate-id`      | Стабильный ID canonical Candidate для PostgreSQL; по умолчанию `candidate-local`. |
| `HH_DRY_RUN`           | `-dry-run`           | Безопасный режим без записей в HH; включён по умолчанию.               |
| `HH_WRITE_ENABLED`     | `-hh-write-enabled`  | Разрешить только ручную отправку подтверждённых ответов/follow-up через Write Gateway; по умолчанию выключено. `HH_DRY_RUN=true` всегда блокирует запись. |
| `HH_CHAT_URL`          | `-hh-chat-url`       | Базовый URL HH Chatik для offline request preview; по умолчанию `https://chatik.hh.ru`. Реальный adapter использует URL из HH config. |
| `HH_MAX_WRITES_PER_RUN` | `-hh-max-writes-per-run` | Лимит ручных HH-отправок за процесс; по умолчанию `1`, `0` отключает лимит. |
| `HH_MAX_WRITES_PER_DAY` | `-hh-max-writes-per-day` | Лимит ручных HH-отправок за UTC-день; по умолчанию `5`, `0` отключает лимит. |
| `HH_AUTO_APPLY`        | `-auto-apply`        | Разрешить отклики на вакансии.                                        |
| `HH_AUTO_CHAT`         | `-auto-chat`         | Разрешить отправку ответов в чаты.                                    |
| `HH_AUTO_TOUCH`        | `-auto-touch`        | Разрешить поднятие резюме.                                            |
| `HH_AUTO_JOB_STATUS`   | `-auto-job-status`   | Разрешить обновление статуса поиска работы.                           |
| `HH_CHAT_MODE`         | `-chat-mode`         | Режим чатов: `off`, `review` или `auto`; по умолчанию `review`.       |
| `HH_MIN_SALARY`        | `-min-salary`        | Отклонять вакансии, если известный верхний предел ниже суммы.         |
| `HH_MIN_SALARY_CURRENCY` | `-min-salary-currency` | Валюта минимальной зарплаты; по умолчанию `RUR`. Несовпадающие/пустые валюты не отклоняются hard-фильтром. |
| `HH_INCLUDE_KEYWORDS`  | `-include-keywords`  | Позитивный сигнал через запятую; отсутствие совпадения не отклоняет.  |
| `HH_EXCLUDE_KEYWORDS`  | `-exclude-keywords`  | Отклонять вакансии, содержащие любое слово/фразу из списка.           |
| `HH_MIN_MATCH_SCORE`   | `-min-match-score`   | Минимальный AI score от 0 до 100; по умолчанию `65`.                  |
| `HH_RUN_ONCE`          | `-run-once`          | Выполнить разрешённые задачи один раз и завершиться.                 |
| `HH_MAX_VACANCIES_PER_RUN` | `-max-vacancies-per-run` | Максимум вакансий после базовой eligibility-проверки за проход; `0` — без лимита, по умолчанию `20`. |
| `HH_MAX_APPLICATIONS_PER_RUN` | `-max-applications-per-run` | Максимум откликов/превью за проход; `0` — без лимита, по умолчанию `10`. |
| `HH_MAX_CONVERSATIONS_PER_RUN` | `-max-conversations-per-run` | Максимум чатов при явном `monitor --run-once`; `0` — полный scope. Ограничение применяется до чтения деталей чатов и не меняет scheduled Career monitor. |
| `HH_CANDIDATE_PROFILE` | `-candidate-profile` | Локальная база знаний кандидата; по умолчанию `candidate_profile.json`, файл не коммитится. |
| `HH_CANDIDATE_STORIES` | `-candidate-stories` | Необязательные примеры опыта; по умолчанию `candidate_stories.json`. Используются только в релевантных cover letters, максимум 2 кейса. |
| `HH_RESUME_REGISTRY` | `-resume-registry` | Локальные enabled/disabled overrides нормализованных HH-резюме; файл создаётся только явной CLI-командой. |
| `HH_CAREER_AGENT_RESULT` | `-career-agent-result` | Последний JSON shadow/canary report. |
| `HH_CAREER_AGENT_FEEDBACK` | `-career-agent-feedback` | Структурированный feedback `ACCEPT`, `REJECT`, `WRONG_RESUME`, `GOOD_MATCH`, `BAD_MATCH`. |
| `HH_CAREER_AGENT_WORKFLOW` | `-career-agent-workflow` | Durable Career Agent runs и application-preparation store; по умолчанию `career_agent_workflow.json`. |
| `HH_AUTO_APPLY_MODE` | `-auto-apply-mode` | Документирует `off`/`canary`; само значение не включает writes. Для live-пути нужен `career-agent --canary`. |
| `HH_SYNC_INTERVAL` | `-sync-interval` | Интервал локального Career Monitor; по умолчанию `15m`. Только read-only sync. |
| `HH_QUIET_HOURS` | `-quiet-hours` | Тихие часы уведомлений в формате `23:00-07:00`; HH sync не отключается. |
| `HH_NOTIFICATION_COOLDOWN` | `-notification-cooldown` | Минимальный интервал повторных локальных уведомлений; по умолчанию `15m`. |
| `HH_CONVERSATION_DISPLAY_TTL` | `-conversation-display-ttl` | TTL HH-проверки для неблокирующего display refresh открытого диалога; по умолчанию `60s`. Это не заменяет fresh safety preflight. |
| `HH_BACKGROUND_INBOX_REFRESH` | `-background-inbox-refresh` | Опциональный лёгкий metadata refresh Inbox с интервалом `HH_SYNC_INTERVAL`; не загружает полную историю неизменённых чатов. |
| `HH_ALREADY_RESPONDED_STATE` | `-already-responded-state` | Локальный JSON со списком vacancy ID, подтверждённых read-only preflight как уже откликнутые. |

Безопасный пример конфигурации HH API (учётные данные остаются пустыми до
явной настройки оператором):

```env
HH_TRANSPORT=browser
HH_API_BASE_URL=https://api.hh.ru
HH_OAUTH_AUTHORIZE_URL=https://hh.ru/oauth/authorize
HH_OAUTH_TOKEN_URL=https://api.hh.ru/token
HH_OAUTH_CLIENT_ID=
HH_OAUTH_CLIENT_SECRET=
HH_OAUTH_REDIRECT_URI=
HH_OAUTH_USER_AGENT=
HH_API_TOKEN_FILE=.hh-api-token.json
```

### Stage 26: локальный quality feedback

Dashboard сохраняет лёгкий `quality_log.json` рядом с остальными локальными store-файлами. В нём нет секретов и копий сообщений: только идентификаторы, состояния workflow, решение reply-policy, результат draft/notification и пользовательский feedback. В Inbox и Today доступны отметки классификации и «не требует внимания», а на странице draft — оценка ответа и причина исправления. Feedback не изменяет Candidate Knowledge и никогда не запускает отправку в HH.

Сводка после накопления реальных наблюдений:

```bash
./hh-ai-responder hh quality-report
```

`HH_DRY_RUN=true` и `HH_WRITE_ENABLED=false` остаются безопасными настройками Stage 26; quality logging не является HH write.

### Локальный профиль кандидата

Общий [Communication Profile](candidate_communication.md) задаёт позиционирование, тон, структуру ответов и правила достоверности. Он автоматически включается в AI-промпты сопроводительных писем и ответов рекрутерам в HH chats. Для подготовки к собеседованиям используйте этот же файл как инструкцию вместе с подтверждёнными данными кандидата; отдельного режима собеседований в приложении нет. Файл встроен в бинарник через `go:embed`: после изменения пересоберите приложение (`go build ./cmd/hh-ai-responder`). Посмотреть его можно командой `./hh-ai-responder profile communication`.

`candidate_stories.json` содержит примеры опыта, но не расширяет факты `candidate_profile.json` и не участвует в vacancy matching. В письмо попадают только кейсы с явным совпадением ключевых слов, ролей или технологий с текстом вакансии; добавляется не более 1–2 кейсов. Если подтверждение результата или достижения вызывает сомнение, кейс не используется. Посмотреть загруженные stories можно командой `./hh-ai-responder profile stories`.

Минимальный формат stories:

```json
{
  "version": 1,
  "stories": [
    {
      "id": "api-integration",
      "title": "Интеграция API",
      "keywords": ["API", "интеграции"],
      "technologies": ["Python"],
      "task": "Подтверждённая задача",
      "action": "Что сделал кандидат",
      "result": "Только подтверждённый результат"
    }
  ]
}
```

Заполняйте stories только реальными примерами. Поля `keywords`, `roles` и `technologies` используются для детерминированного отбора релевантных кейсов, а не доказывают наличие навыка.

Профиль расширяет данные HH resume и хранит сведения кандидата с источником и временем последнего подтверждения, включая структурированные факты из HH и верифицированные факты GitHub. AI не может записать факт или установить `confirmed=true`; производные сведения не используются как утверждения в сопроводительных письмах и чатах.

Без cookies и запросов к HH можно запустить начальный опрос:

```bash
./hh-ai-responder profile
./hh-ai-responder profile bootstrap
./hh-ai-responder profile import ready_candidate_profile.json
./hh-ai-responder profile show
./hh-ai-responder profile questions
./hh-ai-responder profile stories
./hh-ai-responder profile communication
```

Если важное требование вакансии неизвестно, automation run только добавляет дедуплицированный pending-вопрос в профиль и переводит вакансию в `REVIEW_REQUIRED`. Ответить на него можно при следующем запуске `profile`. Уровни навыка: `unknown`, `heard_of`, `basic`, `working`, `confident`, `advanced`. `Docker`, `Kubernetes`, `GitHub` и `Git` не считаются взаимозаменяемыми без явного deterministic alias.

`profile import <file>` валидирует готовый JSON, перед заменой сохраняет старый профиль в `candidate_profile.json.bak`, объединяет факты по приоритету источников и записывает результат с правами `0600`.

### Candidate Knowledge Base

Candidate Knowledge Base — это живой профиль кандидата, который развивается со временем. JSON служит форматом хранения; Go-слой моделей, валидации и истории изменений создаёт основу для пополнения через ответы пользователя, импорт резюме, интервью и анализ проектов. Реализованы локальное хранилище и единый Knowledge Update Pipeline с предложениями и ручным подтверждением. Автоматические адаптеры источников и подключение новых коллекций к AI будут добавляться отдельно; существующие `profile import/show/stories`, matching, письма и чаты продолжают использовать прежние данные.

| Файл | Назначение |
| --- | --- |
| `candidate_profile.json` | Основной профиль в прежнем формате; сохраняется существующими командами. |
| `candidate_skills.json` | `CandidateSkillDetailed`: уровень, категория, confidence, статус достоверности, источники, evidence, проекты, способности, ограничения и время последнего использования. |
| `candidate_projects.json` | `CandidateProject`: тип, роль, период, описание, технологии, задачи, результаты и связанные навыки. |
| `candidate_achievements.json` | `CandidateAchievement`: проблема, решение, действия, результат, технологии и ссылка на проект. |
| `candidate_unknowns.json` | `CandidateUnknown`: вопрос, связанная сущность, provenance employer conversation и статус `needs_confirmation`, `confirmed`, `rejected`, `dismissed` или `superseded`. |
| `candidate_proposals.json` | `KnowledgeProposal`: предлагаемое значение, причина, источник, confidence, состояние `pending` / `confirmed` / `rejected`, время создания и исходная запись для проверки устаревания. |
| `candidate_events.json` | `CandidateKnowledgeEvent`: время, действие, сущность, прежнее/новое значение, источник и автор записи. |
| `candidate_stories.json` | Прежние примеры опыта; миграция их не изменяет и не превращает в подтверждённые достижения. |

Новые коллекции находятся рядом с выбранным файлом профиля. Формат каждой — объект с `version: 1` и массивом соответствующего имени, например `{"version":1,"skills":[]}`. Старые `CandidateProfile`, `CandidateSkill`, `ProjectFact` и строковый `level` остаются совместимыми.

**Источник и достоверность.** `KnowledgeSource` поддерживает `user_confirmed`, `hh_resume`, `github_verified`, `candidate_interview`, `project_analysis`, `employer_conversation`, `derived`, `unknown`. В `sources` хранятся записи с `type`, собственным `evidence`, необязательными `reference` и `observed_at`. Все новые сущности знаний имеют общие метаданные: `confidence`, `truth_status`, `sources`, `evidence`, `created_at`, `updated_at` и необязательное `confirmed_at`.

- `confidence` — число от 0 до 1 либо `null`, если оценка неизвестна. Ноль отличается от неизвестной оценки.
- `truth_status: confirmed` требует источника `user_confirmed` с непустым evidence и времени подтверждения.
- `truth_status: verified` требует evidence от `hh_resume` или `github_verified`.
- `hypothesis` и `unknown` не являются подтверждёнными фактами. Confidence 0.9 или даже 1.0 не повышает их статус.
- `derived`, `candidate_interview` и `project_analysis` сами по себе не подтверждают опыт. Для подтверждения нужен отдельный подтверждающий источник; название интервью или наличие проекта не доказывает уровень навыка.
- Вопрос в статусе `confirmed` требует пользовательского подтверждения. Отклонение также требует пользовательского evidence; отклонённая гипотеза остаётся неподтверждённой. Запись ответа пока не создаёт навык автоматически.

Проверки выполняются при добавлении, загрузке и сохранении. Хранилище принимает данные от доверенного локального Go-кода: подлинность пользовательского действия и GitHub evidence обеспечивает доверенный адаптер ввода через `CandidateKnowledgeUpdater`. Передавать ответ AI напрямую как доверенные метаданные нельзя. Приоритет источников старого профиля не меняется; новые `Add…()` добавляют отдельные утверждения и не перезаписывают существующий ID.

**Go API и миграция.** `NewCandidateKnowledgeBase(profilePath)` создаёт хранилище. `Load()` только читает профиль и шесть новых коллекций: отсутствующая коллекция считается пустой, повреждённая или неподдерживаемая возвращает ошибку без замены состояния в памяти. `AddSkill()`, `AddProject()`, `AddAchievement()`, `AddUnknown()` валидируют запись, при необходимости назначают ID и timestamps и добавляют событие в память. `AddEvent()` позволяет записать отдельное событие. Эти методы остаются низкоуровневым API хранилища для совместимости; новые источники должны писать через `CandidateKnowledgeUpdater`. Прямое изменение экспортированных полей не создаёт историю автоматически.

Миграция вызывается явно из Go-кода, без HH-запросов:

```go
kb := NewCandidateKnowledgeBase("candidate_profile.json")
if err := kb.Load(); err != nil {
    return err
}
if err := kb.MigrateLegacyProfile(); err != nil {
    return err
}
if err := kb.Save(); err != nil {
    return err
}
```

`MigrateLegacyProfile()` переносит skills и projects из загруженного профиля в память, сохраняя источники, evidence, исходное время подтверждения, уровни и отрицательные факты. Подтверждённые HH/GitHub-записи получают `verified`, пользовательские — `confirmed`, derived — `hypothesis`; неподтверждённые сведения не повышаются до фактов. Отсутствующий confidence сохраняется как `null`; тип и период проекта, способности и связи навыков не додумываются. Описание проекта сохраняется отдельно, `business_impact` — в `results`.

ID миграции вычисляется по содержимому исходного утверждения. Повторный запуск с прежними данными не создаёт дублей или событий и не перезаписывает дополненные записи. Различающиеся исходные утверждения сохраняются отдельно: автоматического разрешения конфликтов на этом этапе нет. Миграция копирует данные; исходный профиль и stories остаются побайтно неизменными.

`Save()` сохраняет **только шесть новых коллекций**, не основной профиль и stories. Все коллекции сначала валидируются и записываются в уникальные временные файлы с правами `0600`, затем выполняются `sync`, закрытие файлов и `rename`. Замена атомарна для каждого файла; общей транзакции между файлами пока нет. Ошибка ОС во время замены может оставить частично сохранённую базу и возвращается вызывающему коду. Используйте одного писателя; параллельные процессы и прямые изменения полей требуют внешней синхронизации.

Файлы базы и временные снимки исключены из Git. История содержит значения фактов, поэтому она также является приватными данными кандидата. Хранилище проверяет запрещённые маркеры секретов по правилам существующего профиля. На этом этапе новые коллекции не участвуют в HH-действиях; будущая AI-интеграция должна отделять подтверждённые знания от гипотез и вопросов.

### Knowledge Update Pipeline

`NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: ...})` предоставляет `UpdateSkill`, `UpdateProject`, `UpdateAchievement`, `UpdateUnknown`, `ConfirmKnowledge` и `RejectKnowledge`. Update-методы принимают значение сущности и `KnowledgeUpdate` с источником (`KnowledgeSourceRecord`), причиной и необязательным confidence. Они возвращают ID сущности и, если созданы, ID proposal или вопроса.

Значение — **полная новая версия сущности**, не частичный patch. Передавайте содержательные поля без `KnowledgeMetadata`: статус достоверности, подтверждение, источники и timestamps назначает pipeline. При обновлении сохраняется `created_at`, а событие содержит прежнюю и новую версии. ID существующего навыка определяется также по каноническому имени (например, Go/golang); неоднозначность после миграции требует явного ID. Обновления атомарны в памяти, затем вызывающий код явно вызывает `kb.Save()`.

| Источник | Правило pipeline |
| --- | --- |
| `user_confirmed` | Только `Actor: KnowledgeActorUser` и непустое evidence; создаёт `confirmed`. |
| `hh_resume` | Доверенный импорт с evidence создаёт `verified`. AI не может заявлять HH-происхождение. Указание уровня выше прежнего (включая переход от неизвестного уровня) и замена пользовательского `confirmed` требуют proposal. |
| `github_verified` | Обязательны GitHub reference, evidence и успешный `VerifyGitHub`; создаётся pending proposal. После подтверждения пользователем факт получает `verified`. |
| `candidate_interview` | Pending proposal; после отдельного подтверждения пользователя — `confirmed`. |
| `project_analysis`, `derived` | Только гипотеза и pending proposal; существующий подтверждённый/проверенный факт и повышение существующего уровня остаются без изменений до подтверждения. Подтверждение добавляет источник `user_confirmed`. |
| `unknown` | Только вопрос в `candidate_unknowns.json`; факт и proposal не создаются. |

`Actor` и `VerifyGitHub` задаются доверенным Go-кодом, не полями ответа AI. Для AI используется `KnowledgeActorAI`, для импорта — `KnowledgeActorImporter`. Callback `VerifyGitHub(source, entityType, value)` обязан проверить реальное, относящееся к кандидату доказательство **всего утверждения**, включая заявленные уровень и роль. URL репозитория или наличие отдельного файла конфигурации сами по себе не доказывают уровень владения инструментом. Без callback или при ошибке обновление отклоняется. Сетевой GitHub/AI-адаптер на этом этапе не реализован; JSON proposals — доверенное локальное хранилище, не формат импорта ответов AI.

`ConfirmKnowledge(proposalID)` и `RejectKnowledge(proposalID)` разрешены только для `KnowledgeActorUser`. Confirm применяет ровно просмотренное значение, добавляет пользовательское подтверждение и события для факта и proposal. Если исходная запись изменилась, подтверждение завершается ошибкой: нужно новое предложение. Reject меняет только состояние proposal и добавляет событие, сохраняя профиль и факты. Повторное решение и неизвестный ID возвращают ошибку. Неподтверждённая гипотеза после reject остаётся в локальной базе и не становится фактом для работодателя.

`UpdateUnknown` создаёт или обновляет открытый вопрос; он не превращает вопрос в утверждение. Для ответа с конкретным значением используйте соответствующий `UpdateSkill` / `UpdateProject` / `UpdateAchievement` с пользовательским источником либо предложение на подтверждение.

```go
updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{
    Actor: KnowledgeActorAI,
})
result, err := updater.UpdateSkill(
    CandidateSkillDetailed{Name: "Docker", Level: SkillLevelWorking},
    KnowledgeUpdate{
        Source: KnowledgeSourceRecord{Type: KnowledgeSourceDerived},
        Reason: "Гипотеза для проверки пользователем",
    },
)
if err != nil {
    return err
}
// result.ProposalID нужен для отдельного пользовательского решения.
_ = result
return kb.Save()
```

`kb.GetEmployerSafeKnowledge()` возвращает отдельную копию skills, projects и achievements только со статусами `confirmed` или `verified`, повторно проверяя валидность. Гипотезы, неизвестные сведения, вопросы, proposals и история событий туда не попадают. Pending proposal не скрывает прежний подтверждённый факт и не подменяет его своим значением. Этот метод подготовлен для будущих matching, писем и HR-чата; текущие HH-пути записи не изменены.

Минимальный CLI:

```bash
./hh-ai-responder profile knowledge proposals
./hh-ai-responder profile knowledge confirm <proposal-id>
./hh-ai-responder profile knowledge reject <proposal-id>
# Другой профиль и соседние коллекции:
./hh-ai-responder profile knowledge proposals -candidate-profile /path/to/candidate_profile.json
```

`proposals` выводит JSON-массив pending proposals с полными предлагаемыми значениями; при отсутствии — `[]`, без записи. В `json` режиме `confirm` и `reject` сохраняют решение и аудит локально; в `postgres` режиме они используют canonical transaction и не изменяют legacy JSON. PostgreSQL storage должен быть заранее инициализирован `candidate migrate-postgres --apply`; команды не запускают миграцию автоматически и не требуют cookies.

### Candidate Context Resolver

`NewCandidateContextResolver(kb)` подготавливает отдельный `CandidateContext` для конкретного запроса. В `STORAGE_BACKEND=postgres` resolver получает detached snapshot напрямую из `PostgresCandidateRepository`; в `json` режиме сохраняется legacy mapper. Оба пути не вызывают HH/AI и не меняют candidate storage при чтении.

```go
resolver := NewCandidateContextResolver(kb)
context, err := resolver.ResolveForVacancy(Vacancy{Name: "Python Django Developer"})
// При наличии уже прочитанных описания/требований:
context, err = resolver.ResolveForVacancy(vacancy, description, requirements)
// История — []ChatMessage; сообщения расположены от старых к новым.
context, err = resolver.ResolveForEmployerMessage("Работали ли вы с Kubernetes?", history)
// Общий запрос, в том числе отдельный вопрос HR или текст требований:
context, err = resolver.GetEmployerSafeContext("Python Django")
```

Результат содержит шесть JSON-полей, всегда массивы: `allowed_facts`, `relevant_skills`, `relevant_projects`, `relevant_achievements`, `missing_information`, `forbidden_claims`. В `allowed_facts` входят формулировки из подтверждённых навыков (`CanDo`, с указанием навыка), их уровень и явные связи технологии с проектом. Проекты и достижения представлены отдельными DTO без источников, evidence, audit metadata, полного профиля и контактов. Текстовые описания, задачи и результаты выбранных проектов сохраняются; списки технологий ограничены запросом. Полученный контекст отделён от коллекций базы.

### Candidate Semantic Retrieval

Semantic layer доступен только в PostgreSQL Candidate backend после migration
`000004_candidate_semantic`. Canonical Candidate остаётся source of truth;
pgvector отвечает только за поиск релевантных уже существующих narrative
entities. В индекс попадают только eligible `stories`, `projects` и
`achievements`; skills, preferences, constraints, claims, unknowns, proposals,
events и employer conversations vector-индексом не заменяются.

```bash
hh-ai-responder candidate semantic status --database-url "$DATABASE_URL" --candidate-id candidate-local
hh-ai-responder candidate semantic reindex --dry-run --database-url "$DATABASE_URL" --candidate-id candidate-local
hh-ai-responder candidate semantic reindex --apply --database-url "$DATABASE_URL" --candidate-id candidate-local
hh-ai-responder candidate semantic search "опыт автоматизации" --database-url "$DATABASE_URL" --candidate-id candidate-local
```

`reindex` использует SHA-256 normalized searchable representation, batch
embeddings и перестраивает только новые/изменившиеся документы; stale и
ineligible документы удаляются из active index. `status` не выводит полный
private story text, а `search` показывает type, ID, score и title. Cosine
`score` однозначно означает `higher = more relevant`; exact scan выбран для
небольшого Candidate KB, без premature HNSW/IVFFlat.

Embedding provider, endpoint, key, model and dimensions are configured
separately from chat. Empty `EMBEDDING_BASE_URL`/`EMBEDDING_API_KEY` values
fall back to the corresponding `HH_AI_*` values for compatibility only.
Responses must match `EMBEDDING_DIMENSIONS` exactly. Changing the provider,
endpoint, model or dimensions changes the non-secret embedding space identity;
semantic search reports `REINDEX_REQUIRED` until the explicit reindex completes.

Eligibility проверяется до индексации и повторно после retrieval по canonical
Candidate. Hypothesis/unknown/disputed/unsupported references не попадают в
employer context; similarity никогда не становится доказательством навыка или
коммерческого опыта. Embedding failure не откатывает committed Candidate
mutation и возвращает explicit semantic-unavailable error. Интеграция
retrieved narrative selections в cover-letter/chat orchestrator оставлена
следующим PR; typed `RelevantKnowledgeSnapshot.SemanticSelections` уже
подготовлен для validated results.

Поиск детерминированный, по границам названий без учёта регистра; поддержаны алиасы Go/Golang, Kubernetes/K8s и PostgreSQL/Postgres. Используются названия подтверждённых знаний и небольшой словарь технологий для распознавания отсутствующего опыта. Проекты выбираются по явно указанной технологии, имени, `RelatedSkills` (ID или имя навыка) либо `skill.Projects` (ID или имя проекта); достижения — по технологии, заголовку или `ProjectID` выбранного проекта. Нет автоматического вывода Django из Python: запрос «Python Django» выбирает оба подтверждённых навыка и связанный проект, запрос только «Python» не добавляет соседние навыки React/Linux/Docker из стека проекта.

Источником утверждений служит `GetEmployerSafeKnowledge()`: только валидные `confirmed` / `verified`. Неподтверждённые derived/hypothesis/unknown, pending proposals, вопросы и события не читаются как контекст. Явное последующее подтверждение гипотезы через updater делает её подтверждённым знанием по правилам предыдущего этапа. История диалога служит лишь подсказкой темы для коротких продолжений вроде «А сколько лет?»; используется последнее непустое видимое сообщение. История никогда не подтверждает опыт, а явная тема нового сообщения имеет приоритет.

Отсутствие подтверждения создаёт `missing_information: [{"question":"Есть ли подтверждённый опыт «Kubernetes»?"}]`, а не утверждение об отсутствии навыка. Для нераспознанного запроса формируется общий вопрос. Сложный вопрос работодателя, production/коммерческий опыт, длительность, уровень или условия требуют отдельного подтверждения даже при наличии навыка. Это консервативный поиск релевантных фактов, а не полный анализ естественного языка или окончательное решение о соответствии требованиям. Пустой список `missing_information` не разрешает HH-действия и не доказывает выполнение всех требований вакансии.

`forbidden_claims` сохраняет все `CannotClaim` подтверждённых навыков независимо от релевантности и доверенные подтверждённые `AvoidClaiming` старого профиля (только запреты, не утвердительные факты). Явный `Negative` остаётся отрицательным фактом; противоречивые положительные/отрицательные записи, невалидные метаданные или запрещённые маркеры секретов возвращают ошибку и пустой контекст. `nil`/пустая база поддерживается. Вызывающий код обязан проверить `err`, соблюдать запреты и не превращать вопросы в ответы; автоматическое применение контекста будет отдельным этапом.

### Employer Conversation Memory

`ConversationStore` хранит разговоры отдельно от знаний кандидата, в `employer_conversations.json`. Формат — `{"version":1,"conversations":[...]}`. Конструктор принимает явный путь; новых CLI-флагов и env-переменных нет. Это локальный API: он не читает и не меняет реальные чаты HH, не запускает AI, автоответы, dashboard, таймеры или follow-up.

```go
store := NewConversationStore(filepath.Join(filepath.Dir(profilePath), EmployerConversationsFilename))
if err := store.Load(); err != nil { return err }
conversation, err := store.UpsertConversation(EmployerConversation{
    VacancyID: vacancy.ID,
    HHConversationID: hhConversationID,
    CompanyName: companyName,
    VacancyTitle: vacancy.Name,
    Status: ConversationApplied,
})
if err != nil { return err }
// Фиксирует уже полученное сообщение, не вызывает HH:
message, err := store.AppendMessage(conversation.ID, ConversationMessage{
    ExternalID: externalMessageID,
    Timestamp: originalTimestamp,
    Sender: ConversationSenderEmployer,
    Text: originalText,
    Source: ConversationSourceHH,
    Direction: ConversationIncoming,
})
if err != nil { return err }
_ = message
if err := store.Save(); err != nil { return err }

builder := NewConversationContextBuilder(store, NewCandidateContextResolver(kb))
replyContext, err := builder.BuildForReply(conversation.ID)
if err != nil { return err }
_ = replyContext // Подготовленные данные; отправка/генерация не выполняется.
```

**Хранение и оригиналы.** `Load()` заменяет состояние только после успешной проверки всего файла; отсутствующий файл означает пустое хранилище. Повреждённые файлы, неподдерживаемая версия, неизвестные JSON-поля и повторные идентификаторы возвращают ошибку. Изменения происходят в памяти, `Save()` вызывается явно: приватный temp-файл в той же директории, `Sync`, `Close`, `Rename`. Права файла — `0600`; при ошибке staging удаляется. Хранилище и временные снимки исключены из Git. Доступ вызывающий код сериализует, как для Knowledge Base; межпроцессной блокировки нет.

`UpsertConversation()` принимает полный снимок и возвращает сохранённую копию. При отсутствии локального ID разговор может быть найден по `hh_conversation_id`; иначе ID генерируется. Существующие сообщения должны остаться неизменным префиксом: нельзя стирать, переставлять, менять текст, автора или исходный timestamp. `AppendMessage()` сохраняет исходный текст, включая пробелы и переводы строк. Timestamp задаёт импортёр, он не угадывается. Повторный `(source, external_id)` в рамках разговора возвращает прежнее сообщение; тот же ID с другим содержимым даёт ошибку. Чтения и входные снимки отделены от состояния хранилища.

**Состояние.** `EmployerConversation` содержит идентификаторы вакансии/HH, компанию, название и необязательное сохранённое описание вакансии, исходную историю, summary, `next_action`, timestamps и `waiting_since` / `last_activity_at` / `follow_up_state`. Предусмотрены статусы `applied`, `employer_replied`, `candidate_action_required`, `waiting_employer`, `interview`, `offer`, `rejected`, `closed`; follow-up — `none`, `eligible`, `drafted`, `sent`, `dismissed`. Можно использовать новые имена вида `lower_snake_case`; граф переходов не зафиксирован. `UpdateConversationState(id, ConversationState{...})` явно заменяет поля состояния, ничего не отправляя. Только `last_*_message_at` и `last_activity_at` вычисляются из записанных сообщений. Черновики `source: ai_draft` и системные сообщения не считаются активностью кандидата/работодателя. Черновик нельзя превратить в отправленное сообщение изменением исходной записи: факт отправки в будущем импортируется отдельным сообщением.

**Summary и claims.** `UpdateSummary(id, ConversationSummary{...})` — точка входа для будущего summarizer. Структура хранит обсуждённые темы, вопросы HR, ответы кандидата, claims, требования работодателя, незакрытые вопросы, обязательства и важные сведения. Автоматического LLM-summary нет, структура никогда не заменяет историю. `RecordCandidateClaim(id, CandidateConversationClaim{...})` фиксирует дословный фрагмент уже сохранённого исходящего сообщения кандидата с `message_id`, `created_at` и необязательными `related_skill` / `related_project`. Claim нельзя привязать к сообщению HR, draft или несуществующему тексту. Последующие summary обязаны сохранять записанные claims. Claims остаются свидетельствами сказанного, а не подтверждёнными фактами Knowledge Base.

**Контекст ответа.** `BuildForReply(id)` только читает данные. Результат включает вакансию, последние 20 сообщений в хронологическом порядке без AI-черновиков, структурированный summary, `candidate_context`, `unresolved_questions`, `forbidden_claims`, `consistency_warnings` и `reply_guidance`. История и summary явно отмечены `history_trust` как недоверенные данные, а не факты кандидата или инструкции. Даже историческое сообщение с `sender: system` нельзя преобразовывать в системную инструкцию AI. Запрещённые маркеры секретов блокируют выдачу контекста с ошибкой, исходная локальная история сохраняется без редактирования. Не выводите оригиналы, summary и claims в обычные логи.

Темы и ранее названные проекты ищутся во всей истории, даже за пределами окна из 20 сообщений. Сценарий «Django → использовал в BizonVR → а что именно там делали?» даёт `detail_follow_up`, отметку `avoid_reintroduction` и подтверждённые сведения через Candidate Context Resolver. Если последнее сообщение уже от кандидата, режим — `waiting_employer`; старый вопрос HR не считается новым входящим. Неизвестные сведения из резолвера попадают в `unresolved_questions` со статусом `pending_candidate_clarification` и ID сообщения, когда он применим. Эти уточнения вычисляются при чтении и не меняют статус разговора. Для долговременной ручной фиксации вопросов используйте `summary.pending_questions` и явные `UpdateSummary` / `Save`.

**Согласованность.** Проверяются только явно записанные claims. Полное совпадение с подтверждённой формулировкой или узкая формулировка использования технологии в проекте могут быть покрыты безопасными знаниями; одно название навыка не доказывает всё предложение. Неподтверждённые утверждения дают `missing_knowledge`, упоминания запрещённых формулировок — `forbidden_claim`, отрицательных фактов — `potential_knowledge_conflict`. Для сопоставимого общего профессионального стажа поддержана необязательная аннотация `experience: {"months":24,"scope":"total_professional"}`: число должно явно присутствовать в тексте про общий профессиональный опыт. Оно сравнивается только с доверенным подтверждённым `Profile.TotalExperienceMonths`; несовпадение даёт `experience_mismatch`. Коммерческий, общий и технологический стаж не приравниваются: например, «Python 2 года» без точного подтверждения останется warning о недостающем знании. Проверка консервативная, не является полным семантическим анализом и ничего автоматически не исправляет.

**API для будущего dashboard.** `GetConversation(id)` и `GetByHHConversationID(hhID)` возвращают копию или `ErrConversationNotFound`; `GetByVacancyID(vacancyID)` возвращает список, поскольку одной вакансии могут соответствовать несколько разговоров. `ListConversations()` и `ListConversationsByStatus(status)` сохраняют порядок разговоров в хранилище. `GetConversationTimeline(id)` возвращает хронологическую копию сообщений, при равных timestamps — в порядке импорта; в самом хранилище сохраняется порядок добавления. `GetConversationStats()` считает `total`, `waiting_employer`, `candidate_action_required`, `interview`, `offer`, `rejected` по явно записанным статусам.

Например, для Chat-GPT нужно указать сл:

`.env`

```env
HH_AI_BASE_URL="https://api.openai.com"
HH_AI_MODEL="gpt-4o-mini"
HH_AI_API_KEY="ваш_api_ключ_от_openai"
```

### Безопасный запуск

### Career repositories и PostgreSQL

По умолчанию career data сохраняются в существующих JSON-файлах. PostgreSQL
можно включить явно через `STORAGE_BACKEND=postgres` и
`DATABASE_URL=postgres://...`; при старте применяются versioned SQL migrations.
В PostgreSQL-режиме `VacancyRepository`, `ApplicationRepository` и
`ConversationRepository` используют одну базу и поддерживают общую транзакцию
для read-sync/reconcile batch. При `STORAGE_BACKEND=postgres` Candidate читает
`PostgresCandidateRepository`, а knowledge mutations проходят через
transactional `CandidateMutationService`; legacy JSON остаётся frozen
compatibility snapshot и не является fallback. Startup делает только
read-only candidate preflight: schema и candidate row должны быть заранее
инициализированы явной миграцией. В режиме `json` поведение legacy writer'ов
остаётся прежним.

PostgreSQL хранит canonical aggregate и связанные таблицы, а не копию legacy
JSON-файлов. При недоступном или неинициализированном Candidate startup
завершается с actionable ошибкой и не переключается на JSON.

#### Явная миграция legacy JSON в PostgreSQL

Старые `vacancies.json`, `job_applications.json` и
`employer_conversations.json` не импортируются при запуске приложения. Сначала
постройте read-only план:

```bash
hh-ai-responder storage migrate-postgres --dry-run \
  --database-url "$DATABASE_URL" --report migration-report.json
```

Источник можно указать через `--source-dir`. План проверяет domain validation,
стабильные local/external IDs, конфликты, relations и append-only
events/messages. Повторный запуск идентичного набора idempotent. Запись требует
отдельного явного действия:

```bash
hh-ai-responder storage migrate-postgres --apply \
  --database-url "$DATABASE_URL" --report migration-report.json
```

Без `--apply` команда всегда остаётся dry-run, даже если передан
`--dry-run=false`. В apply-режиме career data импортируются в одной PostgreSQL
transaction; critical или неразрешённые conflicts блокируют запись, а ошибка
откатывает transaction. Existing rows не перезаписываются, destination не
очищается, JSON-файлы не изменяются и `STORAGE_BACKEND` автоматически не
переключается. После commit команда выполняет semantic read-back verification
IDs, external IDs, timestamps, statuses, metadata, relations и counts.
Candidate data, drafts, clarifications, notifications, HH write audit, cookies
и pgvector в эту миграцию не входят.

#### Явная миграция canonical Candidate

После миграции состояние backend можно проверить без вывода candidate facts:

```bash
hh-ai-responder candidate status --database-url "$DATABASE_URL" --candidate-id candidate-local
```

План строится через validated legacy loaders и `BuildCanonicalCandidate`.
Dry-run не меняет destination schema и данные:

```bash
hh-ai-responder candidate migrate-postgres --dry-run \
  --source-dir . --candidate-id candidate-local \
  --database-url "$DATABASE_URL" --report candidate-migration.json
```

Применение требует отдельного `--apply`. Critical mapper diagnostics и
отличающийся уже существующий Candidate блокируют overwrite; повторный apply
идентичного aggregate становится safe no-op. После commit выполняется
canonical read-back verification. Legacy candidate JSON не удаляются.

### Read-only HH sync

Read-only синхронизация импортирует вакансии, уже существующие отклики и историю HH-чатов в выбранный career-data backend. В режиме `json` это локальные JSON-хранилища; в режиме `postgres` batch vacancy/application/conversation сохраняется одной транзакцией. Она не вызывает отклик, отправку сообщений, тесты, изменение резюме или другие HH write-actions. Для состояния синхронизации используется `HH_SYNC_STATE` (по умолчанию `hh_sync_state.json`):

```bash
hh-ai-responder hh sync
hh-ai-responder hh sync vacancies
hh-ai-responder hh sync applications
hh-ai-responder hh sync conversations
hh-ai-responder hh sync conversation <conversation-id-or-chat-id>
hh-ai-responder hh inbox
hh-ai-responder hh workflow
hh-ai-responder hh draft <conversation-id>
hh-ai-responder hh pilot-candidates
hh-ai-responder hh pilot-shortlist
hh-ai-responder hh pilot-show <conversation-id>
hh-ai-responder hh write-status
hh-ai-responder hh eligible
hh-ai-responder hh eligibility-report --class manual-review --reason UNKNOWN_HH_STATUS
hh-ai-responder hh eligibility-summary
hh-ai-responder hh action preflight <action-id>
hh-ai-responder hh action request-preview <action-id>
```

`hh draft` только строит и сохраняет локальный AI-черновик либо clarification; отправка отсутствует в этом flow. Неизвестный статус HH импортируется как `unknown` с сохранённым `raw_status` и предупреждением. Ошибки и частично успешные результаты возвращаются в JSON `SyncResult`, а локальные store-файлы обновляются атомарно.

`hh workflow` — локальный read-only отчёт Stage 24: распределение conversations по пользовательским состояниям и до пяти representative examples каждого состояния.

`hh eligibility-report` — read-only диагностика разговоров без текста сообщений. Она разделяет `SAFE_FOR_MANUAL_REPLY`, `BLOCKED` и `MANUAL_REVIEW`, возвращает стабильные blocker codes и отдельно показывает HH destination, историю, state, candidate context, clarification, audit и обязательный write preflight. Для обычного reply application link не обязателен, если сохранён сильный HH conversation identifier; follow-up использует отдельную, более строгую eligibility.

`hh sync conversation <conversation-id-or-chat-id>` выполняет точечный fresh read-only sync одной HH conversation. `hh action request-preview <action-id>` отдельно строит и валидирует точный JSON request к HH Chatik без network request; idempotency key показывается только как `present`, с источником `SendNonce` и длиной, а заголовки — только по именам/наличию. Preview и live transport используют единый canonical request builder. Safety preflight запускается отдельной командой `hh action preflight <action-id>`; при `HH_DRY_RUN=true` write capability имеет значение `BLOCKED_BY_DRY_RUN`.

`hh pilot-candidates` строит read-only ranking только для `SAFE_FOR_MANUAL_REPLY`: `RECOMMENDED`, `POSSIBLE` и `NOT_RECOMMENDED`. Массовый отчёт не раскрывает тексты сообщений; полный локальный контекст одной conversation доступен через `hh pilot-show <conversation-id>`. Pilot не включает автоматическую отправку: пользователь сам проходит Generate/Edit/Approve → fresh preflight → Send в Dashboard.

`hh pilot-shortlist` строит максимум пять read-only previews для следующего ручного pilot: с последним сообщением работодателя, доступными подтверждёнными фактами и предложенным текстом. Команда не вызывает AI, не создаёт draft и не создаёт approval.

### Reconciliation и Career Monitor

Связи между откликами, вакансиями и чатами восстанавливаются только по HH ID и сохранённым metadata. Недостающие данные остаются informational, а evidence сохраняется локально. Monitor выполняет только HH GET-синхронизацию, reconciliation, audit и расчёт уведомлений:

```bash
hh-ai-responder reconcile --dry-run
hh-ai-responder reconcile
hh-ai-responder monitor --run-once
hh-ai-responder monitor
```

Уведомления хранятся в `notification_events.json`, состояние monitor — в `career_monitor_state.json`. Dashboard предлагает Generate/Edit/Approve, после чего показывает точный текст и отдельную кнопку Send. Доступны только conversation reply и follow-up; автоотклики, bulk-send, tests, resume/status writes и autonomous monitor sends не разрешены.

По умолчанию `HH_DRY_RUN=true`: приложение может читать данные HH, обращаться к AI и формировать превью, но не отправляет отклики, тесты или сообщения, не покидает чаты, не поднимает резюме и не обновляет статус поиска работы. Превью записываются в поток событий с типами `application_preview` и `chat_reply_preview`.

Для единственного разрешённого live write нужно одновременно установить `HH_DRY_RUN=false` и `HH_WRITE_ENABLED=true`. Отправка проходит только через `HHWriteGateway` после approval конкретного текста и свежего read-only preflight. Старые auto/apply/chat/resume/status paths принудительно заблокированы gateway-only policy. Не указывайте `HH_GITHUB_URL`, если ссылку не следует сообщать работодателю.

Перед письмом приложение загружает описание вакансии, применяет deterministic-фильтры и просит AI оценить соответствие кандидата. После AI-match выполняется обязательный read-only preflight authenticated response page. Live-отклик разрешается только при известном состоянии вакансии, отсутствии предыдущего отклика и теста, известном требовании письма и `can_apply=true`; неизвестное состояние получает `vacancy_review_required`. Preflight записывается событием `vacancy_preflight`, не выполняет POST/PUT/PATCH/DELETE и добавляет только структурированные hard-ограничения формата/локации. HH `WorkExperience` (`noExperience`, `between1And3`, `between3And6`, `moreThan6`) остаётся factual/ranking signal и не блокирует отклик сам по себе; явные требования к стажу из описания проверяются отдельно, причём общий стаж кандидата хранится точно в месяцах. Отклик продолжается только при локальном `MATCH`: score не ниже `HH_MIN_MATCH_SCORE`, отсутствуют hard `missing`/`unknown`, и AI advisory recommendation равен `APPLY`. Legacy-поле `apply` сохраняется для совместимости и само по себе не может дать `REJECT`; `DO_NOT_APPLY`/`UNCERTAIN` без другого blocker дают `REVIEW_REQUIRED`. `HH_EXCLUDE_KEYWORDS` и недостаточная зарплата в совпадающей известной валюте отбрасывают вакансию до AI; если валюта отсутствует или отличается от `HH_MIN_SALARY_CURRENCY`, hard reject по зарплате не выполняется и решение оставляется AI. Конвертация валют не выполняется. `HH_INCLUDE_KEYWORDS` является только дополнительным позитивным сигналом. Ключевые слова разделяются запятыми, сравнение нечувствительно к регистру, пустые элементы игнорируются.

`HH_RUN_ONCE=true` выполняет read-only поиск, анализ и подготовку превью, затем завершает программу. В сочетании с `HH_DRY_RUN=true` все HH write-запросы блокируются. Автоматические отклики, ответы, tests и другие legacy writes на этом этапе не выполняются; отправка разрешена только отдельным dashboard flow через gateway.

Если read-only vacancy preflight достоверно сообщает `already_responded_known=true` и `already_responded=true`, ID сохраняется в локальный state-файл и пропускается в следующих запусках до AI evaluation. В state не записываются AI-решения, `MATCH`/`REVIEW_REQUIRED`, неизвестные или ошибочные preflight-состояния; повреждённый state-файл игнорируется с fail-safe поведением.

AI в `hard_requirements` только извлекает кандидатов требований с полями `requirement`, `category` и точным `vacancy_evidence`; поля `status` и `candidate_evidence` в AI-контракте отсутствуют. Локальный код проверяет, что evidence действительно встречается в описании или разрешённом структурированном поле HH, отбрасывает неподтверждённые и optional-требования, а затем вычисляет `met`, `missing` или `unknown` по данным кандидата. Общий минимум из описания до 12 месяцев при фактическом стаже кандидата от 9 месяцев получает `unknown` как soft gap и не блокирует отклик; большой явный разрыв может быть `missing`. Стаж конкретной роли/технологии (`SRE`, `DevOps`, `Java` и т. п.) общий стаж не подтверждает и без role-specific evidence остаётся `unknown`. Образование и числовой стаж без структурированных данных кандидата, другая локация без известной релокации, отсутствие навыка, язык, лицензия и гражданство без подтверждающего факта остаются `unknown`; отсутствие факта не превращается в `missing`. Semantic discard не запускает retry и не ломает всю оценку. Для backward compatibility в событиях вычисляются `hard_requirements_missing` и `hard_requirements_unknown`, но источником истины остаётся структурированный внутренний массив. Фразы вакансии `Без опыта` и `Опыт не требуется` не являются hard-требованием опыта.

### Режимы чатов

`HH_CHAT_MODE=off` не читает и не обрабатывает чаты. `review` читает чаты и генерирует `chat_reply_preview`; live reply выполняется только через dashboard approval/send flow. Legacy `auto` не получает write capability. Зарплата, дата выхода, переезд, документы, банковские данные, договоры, интервью, ссылки, установка ПО и тестовые задания всегда переводятся в review.

Пример безопасной персональной конфигурации:

```env
HH_DRY_RUN=true
HH_AUTO_APPLY=true
HH_MIN_MATCH_SCORE=65
HH_EXCLUDE_KEYWORDS=1С,PHP Senior,DevOps Senior
HH_CHAT_MODE=review
```

## Скрипты автозапуска

В проекте добавлены вспомогательные скрипты для удобного запуска и добавления в автозагрузку:

### Linux / macOS

Файл: `start.sh`

Что делает:

- Переходит в директорию, где находится скрипт.
- Запускает приложение; приложение само загружает `.env` единым безопасным
  парсером, поэтому wrapper не shell-source-ит значения из этого файла.
- Проверяет наличие бинарника `hh-ai-responder`.
- Проверяет, что бинарник исполняемый.
- Запускает приложение, передавая аргументы.

Пример запуска:

```bash
./start.sh -u "https://hh.ru/search/vacancy?..."
```

### Windows (PowerShell)

Файл: `start.ps1`

Что делает:

- Переходит в директорию, где находится скрипт.
- Запускает приложение; приложение само загружает `.env` единым парсером.
- Проверяет наличие `hh-ai-responder.exe`.
- Запускает приложение с передачей аргументов.

Пример запуска:

```powershell
.\start.ps1 -u "https://hh.ru/search/vacancy?..."
```

Оба скрипта возвращают коды завершения приложения и подходят для использования в автозагрузке (systemd, cron `@reboot`, Windows Task Scheduler и т.д.).

## Лицензия

Проект распространяется по некоммерческой лицензии.

- Разрешено использовать код бесплатно в личных и некоммерческих целях.
- Запрещено использование проекта или его частей с целью получения коммерческой выгоды.
- Разрешается модификация и распространение кода только при обязательном указании ссылки на оригинальный репозиторий:

```
https://github.com/s3rgeym/hh-ai-responder
```

Полный текст лицензии см. в файле [LICENSE](LICENSE).

## Career Agent Dashboard

Локальный интерфейс поверх существующих Go stores, `HHReadSyncService`,
`CandidateKnowledgeUpdater` и `AIReplyOrchestrator`. Frontend — встроенные в
бинарник HTML/CSS и vanilla JS, без Node.js, npm и CDN при запуске.

```bash
go build ./cmd/hh-ai-responder
./hh-ai-responder web
```

Откройте **http://127.0.0.1:8080**. Есть эквивалентный режим
`./hh-ai-responder dashboard`. Остановить сервер можно через Ctrl+C.

```bash
./hh-ai-responder web --host 127.0.0.1 --port 8090
```

`HH_WEB_HOST` и `HH_WEB_PORT` задают адрес и порт через окружение или `.env`;
флаги `--host` / `--port` имеют приоритет. По умолчанию — `127.0.0.1:8080`.
Поскольку MVP не имеет аутентификации, допустимы только loopback-адреса
(`127.0.0.1`, `::1`, `localhost`). `0.0.0.0` и адреса локальной сети отклоняются.

### Страницы

| Страница | Содержимое |
| --- | --- |
| `/` | Состояние поиска, отклики, ответы, интервью, офферы, AI и очередь вопросов |
| `/inbox` | Секции «Нужно ответить», «Нужно сделать», «Ждём работодателя», «Интервью / тесты» и «Не требует действий» с важным счётчиком и human-readable next step |
| `/applications` | Фильтры по статусу, рекомендации, компании, поиску и ожиданию; сортировка по дате, score и активности |
| `/applications/:id` | Вакансия, match, timeline, переписка, AI и контекст знаний |
| `/conversations/:id` | История сообщений, summary, темы, pilot suitability, открытые вопросы и AI Assistant |
| `/vacancies` и `/vacancies/:id` | Реальные сохранённые вакансии, рекомендации и диапазон score |
| `/knowledge` | Skills, projects, achievements, truth status, confidence, источники |
| `/knowledge/questions` | Candidate clarifications, unknowns и просмотр/подтверждение/отклонение proposals |
| `/analytics` | Сегодня / 7 / 30 дней / всё время, метрики и график датированных событий |
| `/sync` | Состояние HH sync, ручной запуск и результат по каждому разделу |

### HH read-only и локальные действия

Dashboard не отправляет отклики, tests, resume/status writes или bulk actions.
Единственные state-changing маршруты — manual approval flow для reply/follow-up:
`/api/drafts/:id/edit`, `/api/drafts/:id/approve`, `/api/actions/:id/preflight`,
`/api/actions/:id/send`, `/api/actions/:id/reconcile` и
`/api/actions/:id/cancel`. Эти маршруты доступны локально и fail-closed при
выключенной capability; при `HH_DRY_RUN=true` Send возвращает
`BLOCKED_BY_DRY_RUN` до вызова `HHWriteClient`. Send повторно читает HH и fail-closed при любой
неоднозначности. Защита Host/Origin, same-origin POST с отдельным заголовком,
CSP и лимит JSON 16 KiB защищают локальный интерфейс от посторонних веб-страниц.
Тексты работодателей экранируются и не исполняются как HTML.

Для диагностики ручного Send Dashboard сохраняет локальный bounded lifecycle в
`/api/diagnostics/lifecycle`: `send_ui_clicked`, `send_api_received`,
`gateway_invoked`, `preflight_passed`, `send_started` и итоговый `send_failed`.
Только `send_started` является HH write attempt и попадает в
`write_attempts_total`; UI/API lifecycle не являются HH writes.

Для controlled production pilot сначала выполните `hh write-status`, затем
`hh pilot-candidates` и выберите только `RECOMMENDED`/`POSSIBLE` среди
`SAFE_FOR_MANUAL_REPLY`. В Dashboard последовательность
строго ручная: Inbox → Generate Draft → Edit при необходимости → Approve → Run fresh
preflight → Send. Лимиты по умолчанию — одна отправка за процесс и пять за UTC-день;
они не заменяют отдельный approval. После успешного HTTP-ответа доставка подтверждается
только read-only sync; при отсутствии подтверждения действие получает `sent_unconfirmed`
и автоматически не повторяется.

После ручной отправки gateway сохраняет техническую `PilotObservation` в
`hh_pilot_observations.json`: action, draft/preflight flags, delivery state,
reconciliation result, состояние conversation после отправки и issues — без
текста переписки и секретов.

Браузер получает данные только через `/api/*`. JSON stores загружаются через
существующие store API при старте; для просмотра не нужны действующие HH cookies
или доступный AI. Файлы используются в текущем рабочем каталоге, KB — рядом с
`HH_CANDIDATE_PROFILE`, состояние sync — по `HH_SYNC_STATE`. Запускайте
Dashboard из того же каталога, что и CLI. Работа с файлами сериализована внутри
сервера; **не запускайте одновременно другой процесс, изменяющий те же stores**.
После изменения данных внешним CLI перезапустите Dashboard.

Для обычной работы откройте Inbox и нажмите **Refresh Inbox**: это incremental
metadata flow с переиспользованием неизменённой истории. `/sync` — раздел
Maintenance / Advanced для **Full Sync Conversations** (глубокая проверка,
примерно 5–6 минут), **Full Sync Applications**, **Full Sync Vacancies** и
**Sync Everything**. Full Sync не требуется перед draft, fresh preflight или
Send.
Используются существующие настройки HH, включая `cookies.txt`. Результат
показывает `fetched`, `created`, `updated`, `unchanged`, `skipped`, `errors` и
предупреждения. Само открытие страниц не обращается к HH. Polling каждые 45 секунд
обновляет только локальные представления, не запускает sync и не стирает
редактируемый ответ. Local store operations безопасно выстраиваются в очередь;
`/api/sync/status` остаётся доступным.

Для `NEEDS_REPLY` Inbox после первого локального рендера лениво запускает
background `AIReplyOrchestrator.PrepareEmployerReply()`. Повторная генерация
не выполняется, если совпадают hash последнего employer message, relevant
knowledge hash и prompt version. Полностью answerable вопрос показывает draft;
при `PARTIALLY_ANSWERABLE`/`UNKNOWN` draft не создаётся, а появляется
конкретный clarification. Нажатие **Generate AI Draft** на странице диалога
остаётся доступным как явный локальный retry. Вызывается
`AIReplyOrchestrator.PrepareEmployerReply()`: результатом
может быть `draft_reply`, `need_candidate_input`, `manual_review` или
`no_reply_needed`. Доступны **Regenerate draft**, **Reject draft**, **Copy draft**.
Генерация использует существующие настройки AI и может обращаться к провайдеру;
отправки в HH нет. Drafts и clarifications сохраняются существующими stores.
Последнее решение AI дополнительно отображается до перезапуска сервера или
нового sync/изменения знаний; после перезапуска сохраняются сами drafts,
clarifications и вычисляемые предупреждения контекста.

Ответ на clarification проходит через
`AIReplyOrchestrator.ResolveCandidateClarification()` → typed Candidate Knowledge
Acquisition Loop. Вопрос работодателя создаёт детерминированный `gap_key`, один
`CandidateUnknown` и один clarification с provenance conversation/application/vacancy
и employer message. Choice-ответ применяется атомарно только после явного выбора
кандидата; free-text сохраняется без изменений и проходит строгий AI extraction в
`hypothesis` proposal. AI не может подтвердить знание. Отдельное подтверждение
кандидата через `ConfirmKnowledge`/canonical mutation закрывает unknown и помечает
draft к регенерации; отправка в HH не выполняется автоматически. Невалидный ответ,
неизвестный choice, неподдержанная ссылка или конфликт версии останавливают flow.
Для PostgreSQL примените `migrations/000005_candidate_acquisition.up.sql` после
базовых candidate migrations. `candidate status` показывает pending knowledge
questions и pending proposals.

### Значение метрик

* Вакансии и анализ — сохранённые записи `VacancyStore`; в выбранном периоде
  учитывается дата появления записи в локальном поиске.
* Отклик подтверждается событием `applied` или сохранённым структурированным
  статусом. Дата отклика берётся только из `applied` event или сохранённого
  `hh_metadata.applied_at`, а не из даты импорта. Если её нет, в таблице стоит «—»,
  а отклик участвует только в метриках «Всё время».
* Response rate — доля откликов выбранного периода с известным ответом;
  interview conversion — доля с зафиксированным интервью. Один отклик считается
  один раз. Отказ сам по себе не считается сообщением работодателя.
* График показывает датированные отклики и первые ответы работодателя.
  «Сегодня», «7 дней», «30 дней» — календарные дни в часовом поясе сервера.
* «Сообщения без ответа» — сообщения работодателя после последнего ответа
  кандидата, **не HH unread receipts**. Данные о прочтении текущий store не хранит.
* AI drafts и knowledge queues показывают текущее состояние, независимо от
  выбранного аналитического периода. Новая analytics database не создаётся.

### Career audit и Follow-up Engine (этап 10)

`./hh-ai-responder audit` читает локальные JSON stores и печатает `CareerAuditReport`.
Он не вызывает HH или AI, не исправляет записи и не выводит тексты сообщений.
Диагностический загрузчик позволяет обнаруживать дубликаты даже в store, который
обычный строгий загрузчик отказывается открывать. Повреждённый JSON отражается в
`store_errors`; отсутствие файла означает, что этот store ещё не создан.

В Dashboard доступны `/health` (System Health / Data Quality), `/api/health` и
`GET /api/follow-ups`. Health показывает размеры stores, sync, связи, неизвестные
HH статусы, несовпадения статуса с последним сообщением, stale drafts и уточнения.
Количество unknown statuses — число записей с отсутствующим или неизвестным HH
статусом, а не число уникальных строк статусов. Коды warnings не содержат переписку.

ConversationStateResolver определяет состояние по временной шкале доставленных
сообщений, HH raw status, application, уточнениям и warnings. AI drafts и системные
события не меняют очередь ответа. Отказ, offer, архив и интервью блокируют follow-up;
конфликт источников и неполная история требуют ручной проверки. `waiting_since`
берётся из последнего сообщения кандидата либо подтверждённой даты отправки отклика,
никогда из даты импорта. При одинаковом времени сообщений разных сторон порядок
считается неоднозначным.

FollowUpEngine — чистое детерминированное вычисление без HTTP-клиента, таймеров или
фоновых задач. Настройки политики задаются переменными окружения или CLI-флагами:

| Переменная | Флаг | По умолчанию |
| --- | --- | --- |
| `HH_FOLLOW_UP_AFTER_APPLICATION` | `--follow-up-after-application` | `120h` (5 дней) |
| `HH_FOLLOW_UP_AFTER_MESSAGE` | `--follow-up-after-message` | `72h` (3 дня) |
| `HH_FOLLOW_UP_MAX` | `--follow-up-max` | `2` |
| `HH_FOLLOW_UP_MINIMUM_INTERVAL` | `--follow-up-minimum-interval` | `72h` (3 дня) |

Длительности используют синтаксис Go (`120h`, `90m`; суффикс `d` не поддерживается).
Задержки должны быть положительными; max должен быть неотрицательным, `0` отключает
предложения. CLI имеет приоритет над env, включая невалидное перекрытое значение:

```bash
./hh-ai-responder web --follow-up-after-application 168h --follow-up-max 1
./hh-ai-responder audit --follow-up-after-application 168h
HH_DRY_RUN=true ./hh-ai-responder hh sync
```

Overview показывает Follow-up available; Inbox — Follow-up suggested; карточка
application — причину, waiting since, eligibility, число предыдущих follow-ups,
следующую допустимую дату и AI draft. `POST /api/follow-ups/{application-id}/draft`
повторно проверяет eligibility и вызывает `PrepareFollowUp()` только для eligible.
AI получает безопасный Candidate Context, вакансию/компанию, контекст диалога,
длительность ожидания и явную историю follow-up. Черновик проходит существующую
проверку фактов. Повторный запрос с теми же данными использует текущий черновик.
Ни Send, ни endpoint отправки нет.

`POST /api/follow-ups/{application-id}/dismiss` сохраняет только локальное
`follow_up_state=dismissed` в application. Оно переживает перезапуск и HH sync.
Оба локальных POST используют существующую защиту Origin/Host и заголовок
`X-Career-Agent: local`. Генерация и dismiss никогда не регистрируют отправку.

История follow-up учитывает только явные `ApplicationEvent{type: follow_up_sent}`
с известным временем. Обычные сообщения кандидата и AI drafts в неё не входят.
Старый marker `sent` без даты/счётчика требует проверки. Endpoint регистрации
отправки в этом этапе не добавлен.

Analytics показывает среднее время ответа работодателя, среднее текущее ожидание,
число eligible/drafted follow-ups и разговоров, требующих ответа кандидата.
Средние скрываются при менее чем трёх наблюдениях; неполные или конфликтующие
истории исключаются. Это описательные показатели, а не прогноз успеха.

Read-only sync разбирает контейнер HH `applicantNegotiations.topicList`, читает
пагинацию переговоров и `nextFrom` чатов, сохраняет raw status и отделяет события
участников от сообщений. Неизвестная страница считается ошибкой, а не пустым
успешным sync. Неполная история (`hasMore`) явно отмечается и не допускает follow-up.
Существующие сообщения объединяются по external ID; конфликт содержимого или
sender не перезаписывает исходную запись и остаётся warning. Для пустых записей с явным
`workflowTransition` и `hasContent=false` добавляется `hh_system_event=true`: это
идемпотентная отметка служебного события, исходные sender/text/timestamp сохраняются.
Такие события исключены из очереди ответов и метрик времени. Сводки и локальный
dismiss сохраняются. Клиент sync/dashboard принудительно использует dry-run и
дополнительно запрещает HTTP-методы, кроме GET/HEAD, на границе HHRequester.

Подробный отчёт проверки: [VALIDATION_STAGE10.md](docs/validation/VALIDATION_STAGE10.md).

## Stage 22: performance and read-only sync

Dashboard serves local data without blocking HH requests; Inbox draft preparation
is explicitly background/local-AI work. Conversation detail
calculates its own pilot report; display consistency projections are cached by
conversation content and candidate knowledge. Store file identity, size, mode,
and modification time are checked before API reads. Atomic replacements by a
CLI process invalidate display caches. Invalid changed files fail closed.
Canonical `Load`/`Reload` and write/preflight validation do not use display caches.

- Conversation: сначала мгновенно показывается local state; при истёкшем
  `HH_CONVERSATION_DISPLAY_TTL` (по умолчанию `60s`) в фоне выполняется
  targeted refresh numeric HH `chatId` напрямую. Кнопка называется **Refresh
  this conversation**.
- **Run fresh preflight** refreshes only that conversation and independently
  validates fresh HH state. No full Inbox sync is needed before Send.
- Inbox: **Refresh Inbox** compares observed list fields, including
  activity, last message, participant and topic/vacancy resources. Complete,
  trusted histories with matching metadata can be reused for display for up to
  15 minutes after the last detail read. Missing/changed/ambiguous metadata
  forces a detail read. This is not a safety freshness certificate.
- Sync: **Full Sync Conversations**, **Full Sync Applications**,
  **Full Sync Vacancies**, **Sync All** retain full reads.
- Delivery reconciliation reads only the sent message's chat.
- `/api/health` возвращает быстрый snapshot; тяжёлые eligibility/consistency
  проверки доступны через `/api/health/deep` и кнопку **Run deep audit**.

`HH_READ_CONCURRENCY=4` (CLI `--hh-read-concurrency`, range 1–8) bounds independent
HH GETs. CLI takes precedence over the environment. It does **not** reduce the
existing `--request-interval` (default 1.2 seconds). HTTP 429 causes a shared
read cooldown respecting `Retry-After`, with at most two GET/HEAD retries and
exponential backoff. Writes are never retried by this mechanism. Cancellation
interrupts requests and rate waits.

Browser sync requests use `Prefer: respond-async` and receive HTTP 202. Existing
API clients without this header still receive the completed SyncResult. Use
`GET /api/sync/status` for progress, counts, elapsed time and requests/sec;
`POST /api/sync/inbox` starts metadata refresh and
`POST /api/conversations/{localID}/sync` refreshes one conversation. The usual
same-origin and `X-Career-Agent: local` protections apply. Identical jobs are
coalesced in process; an operation lease prevents duplicates in another process.
A targeted job can run while a full sync is reading HH. A global priority
limiter gives safety and foreground targeted reads precedence without violating
the single HH request-start interval. Shutdown cancels browser
jobs through the server context.

Network work precedes the short store commit section. Sync reloads durable
stores under their existing locks, merges original evidence and saves at most
once per changed store per batch. Candidate analysis occurs before file locks;
a conflicting vacancy-store change cancels that analysis plan. JSON files remain
individually atomic, not a multi-file database transaction. Unchanged notifications
and unchanged conversation batches do not rewrite their stores.

`GET /api/generation` supports inexpensive UI polling; the browser does not
start HH sync on a timer. `GET /api/performance` exposes bounded aggregate stage
metrics, without URLs, candidate facts, message bodies or credentials. SyncResult
also includes `performance` with request counts and network, rate-wait, local
load/save and compute durations. Parallel network durations are sums and can
exceed wall time. Disk timing includes decoding/encoding and local persistence;
locks have separate aggregate wait/critical-section metrics.

Reproduce local measurements without contacting HH:

```bash
HH_WRITE_ENABLED=false HH_DRY_RUN=true HH_PERF_DATASET="$PWD" \
  go test -run TestStage22LocalBaseline -v
HH_WRITE_ENABLED=false HH_DRY_RUN=true HH_PERF_DATASET="$PWD" \
  go test -run '^$' -bench Stage22 -benchmem
```

The dataset benchmark copies JSON into a private temporary directory and never
prints its contents. Live contract measurements are separately opt-in:
`HH_PERF_LIVE_READ=true`; full HH measurements additionally require
`HH_PERF_LIVE_FULL=true` and `HH_PERF_DATASET`. Run only
`TestStage22LiveReadContract` for these measurements. It copies cookies, forces
the GET-only capability and requires both safety environment variables above.
See [PERFORMANCE_STAGE22.md](docs/performance/PERFORMANCE_STAGE22.md) for measured results and
remaining limits.
