# Configuration contract

Stage: R3.1 — Configuration Boundary

The canonical parser is `internal/config.Load(args, lookup, workingDir)`. It
uses an explicit `flag.FlagSet` and a small `LookupEnv` function; it does not
mutate `os.Args` or `flag.CommandLine`.

Precedence is intentionally preserved as:

```text
CLI flag > .env value > process environment value > default
```

The `.env` ordering reflects the existing application behavior: the old
parser loaded `.env` into the process before reading environment values, so a
same-named `.env` entry wins over the inherited process value. Values are
parsed as data and are never shell-executed. Flags after a subcommand remain
owned by that subcommand's parser.

The `FollowUpPolicy` domain value is not imported into `internal/config`.
`FollowUpConfig` contains only primitive settings and is converted at the
package-main composition boundary.

## Public inputs

| FIELD | CURRENT OWNER | ENV | FLAG | DEFAULT | VALIDATION | USED BY | DOMAIN-TYPE DEPENDENCY? | TARGET OWNER |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Search URL | `main.parseConfig` | `HH_SEARCH_URL` | `-u` | empty | URL syntax is checked when search profiles are built | vacancy search | no | `internal/config` |
| Search URLs | `main.parseConfig` | `HH_SEARCH_URLS` | — | fallback to `HH_SEARCH_URL` | split on `||`; at least one non-empty value when set | vacancy search | no | `internal/config` |
| Resume | `main.parseConfig` | `HH_RESUME` | `-r` | empty | none | HH profile/read | no | `internal/config` |
| Storage backend | `main.parseConfig` | `STORAGE_BACKEND` | `-storage-backend` | `json` | `json` or `postgres` | all persistence composition | no | `internal/config` |
| Database URL | `main.parseConfig` | `DATABASE_URL` | `-database-url` | empty | required for explicit `postgres`; never included in errors/logs | PostgreSQL composition | no | `internal/config` |
| Candidate ID | `main.parseConfig` | `HH_CANDIDATE_ID` | `-candidate-id` | `candidate-local` | none | candidate persistence | no | `internal/config` |
| Cookies path | `main.parseConfig` | — | `-c` | `<working-dir>/cookies.txt` | file use is validated by HH client | HH read/write adapters | no | `internal/config` |
| Log level | `main.parseConfig` | — | `-l` | `info` | unknown values retain existing `info` fallback | logger setup | no | `internal/config` |
| Resume response limit | `main.parseConfig` | — | `-mr` | `0` | consumed by vacancy workflow | vacancy workflow | no | `internal/config` |
| Output path | `main.parseConfig` | — | `-o` | stdout | file open/permissions at use site | event output | no | `internal/config` |
| List resumes | `main.parseConfig` | — | `-R` | `false` | boolean flag parser | root command | no | `internal/config` |
| Force letter | `main.parseConfig` | — | `-force-letter` | `false` | boolean flag parser | application workflow | no | `internal/config` |
| AI timeout | `main.parseConfig` | — | `-ai-timeout` | `30s` | greater than zero | AI client | no | `internal/config` |
| AI connect timeout | `main.parseConfig` | — | `-ai-connect-timeout` | `5s` | greater than zero and no greater than AI timeout | AI client | no | `internal/config` |
| HH read concurrency | `main.parseConfig` | `HH_READ_CONCURRENCY` | `-hh-read-concurrency` | `4` | integer from 1 through 8 | HH read sync | no | `internal/config` |
| HH request interval | `main.parseConfig` | — | `-request-interval` | `1.2s` | greater than zero | HH read client | no | `internal/config` |
| AI attempts | `main.parseConfig` | — | `-ai-attempts` | `2` | greater than zero | AI client | no | `internal/config` |
| AI API key | `main.parseConfig` | `HH_AI_API_KEY` | `-ai-api-key` | empty | no value logging; endpoint validates use | AI client | no | `internal/config` |
| AI base URL | `main.parseConfig` | `HH_AI_BASE_URL` | `-ai-base-url` | `http://localhost:11434` | URL use is validated by HTTP client | AI client/embeddings | no | `internal/config` |
| AI model | `main.parseConfig` | `HH_AI_MODEL` | `-ai-model` | `llama3:8b` | non-empty behavior remains downstream-compatible | AI client | no | `internal/config` |
| Contacts | `main.parseConfig` | `HH_CONTACTS` | `-contacts` | empty | candidate-controlled data; no inference | letters/chat drafts | no | `internal/config` |
| Letter prompt | `main.parseConfig` | `HH_LETTER_PROMPT` | `-letter-prompt` | empty | data only | letter generation | no | `internal/config` |
| Test solution prompt | `main.parseConfig` | `HH_SOLUTION_PROMPT` | `-solution-prompt` | empty | data only | test generation | no | `internal/config` |
| Chat reply prompt | `main.parseConfig` | `HH_CHAT_REPLY_PROMPT` | `-chat-reply-prompt` | empty | data only | chat drafting | no | `internal/config` |
| GitHub URL | `main.parseConfig` | `HH_GITHUB_URL` | `-github-url` | empty | candidate explicitly controls disclosure | letters | no | `internal/config` |
| Dry run | `main.parseConfig` | `HH_DRY_RUN` | `-dry-run` | `true` | boolean; `true` blocks every HH write | HH write gateway | no | `internal/config` |
| HH write enabled | `main.parseConfig` | `HH_WRITE_ENABLED` | `-hh-write-enabled` | `false` | boolean; gateway still requires all other safety checks | HH write gateway | no | `internal/config` |
| HH Chat URL | `main.parseConfig` | `HH_CHAT_URL` | `-hh-chat-url` | `https://chatik.hh.ru` | URL use is validated by transport | write preview/transport | no | `internal/config` |
| Writes per run | `main.parseConfig` | `HH_MAX_WRITES_PER_RUN` | `-hh-max-writes-per-run` | `1` | non-negative; `0` means unlimited | HH write gateway | no | `internal/config` |
| Writes per UTC day | `main.parseConfig` | `HH_MAX_WRITES_PER_DAY` | `-hh-max-writes-per-day` | `5` | non-negative; `0` means unlimited | HH write gateway | no | `internal/config` |
| Auto apply | `main.parseConfig` | `HH_AUTO_APPLY` | `-auto-apply` | `true` | boolean; dry-run/gateway still controls writes | workflow | no | `internal/config` |
| Auto chat | `main.parseConfig` | `HH_AUTO_CHAT` | `-auto-chat` | `true` | boolean; legacy fallback for chat mode | conversation workflow | no | `internal/config` |
| Auto touch | `main.parseConfig` | `HH_AUTO_TOUCH` | `-auto-touch` | `true` | boolean; dry-run/gateway still controls writes | workflow | no | `internal/config` |
| Auto job status | `main.parseConfig` | `HH_AUTO_JOB_STATUS` | `-auto-job-status` | `true` | boolean; dry-run/gateway still controls writes | workflow | no | `internal/config` |
| Chat mode | `main.parseConfig` | `HH_CHAT_MODE` | `-chat-mode` | `review` | `off`, `review`, or `auto`; explicit mode wins over legacy auto-chat | conversation workflow | no | `internal/config` |
| Minimum salary | `main.parseConfig` | `HH_MIN_SALARY` | `-min-salary` | `0` | non-negative integer | deterministic vacancy filter | no | `internal/config` |
| Salary currency | `main.parseConfig` | `HH_MIN_SALARY_CURRENCY` | `-min-salary-currency` | `RUR` | three ASCII letters | deterministic vacancy filter | no | `internal/config` |
| Include keywords | `main.parseConfig` | `HH_INCLUDE_KEYWORDS` | `-include-keywords` | empty | comma-separated data; no match is not rejection | vacancy scoring | no | `internal/config` |
| Exclude keywords | `main.parseConfig` | `HH_EXCLUDE_KEYWORDS` | `-exclude-keywords` | empty | comma-separated data | deterministic vacancy filter | no | `internal/config` |
| Minimum match score | `main.parseConfig` | `HH_MIN_MATCH_SCORE` | `-min-match-score` | `65` | integer from 0 through 100 | vacancy decision | no | `internal/config` |
| Run once | `main.parseConfig` | `HH_RUN_ONCE` | `-run-once` | `false` | boolean | runtime loop | no | `internal/config` |
| Max vacancies/run | `main.parseConfig` | `HH_MAX_VACANCIES_PER_RUN` | `-max-vacancies-per-run` | `20` | non-negative; `0` means unlimited | vacancy workflow | no | `internal/config` |
| Max applications/run | `main.parseConfig` | `HH_MAX_APPLICATIONS_PER_RUN` | `-max-applications-per-run` | `10` | non-negative; `0` means unlimited | application workflow | no | `internal/config` |
| Already-responded state | `main.parseConfig` | `HH_ALREADY_RESPONDED_STATE` | `-already-responded-state` | `<working-dir>/.hh-already-responded.json` | local path | read-only preflight state | no | `internal/config` |
| Candidate profile path | `main.parseConfig` plus legacy profile fallback | `HH_CANDIDATE_PROFILE` | `-candidate-profile` | `<working-dir>/candidate_profile.json` | local path | candidate storage | no | `internal/config` |
| Candidate stories path | `main.parseConfig` plus legacy stories fallback | `HH_CANDIDATE_STORIES` | `-candidate-stories` | `<working-dir>/candidate_stories.json` | local path | candidate storage/letters | no | `internal/config` |
| HH sync state path | `main.parseConfig` | `HH_SYNC_STATE` | `-hh-sync-state` | `<working-dir>/hh_sync_state.json` | local path | HH read sync | no | `internal/config` |
| Sync interval | `main.parseConfig` | `HH_SYNC_INTERVAL` | `-sync-interval` | `15m` | greater than zero | career monitor | no | `internal/config` |
| Quiet hours | `main.parseConfig` | `HH_QUIET_HOURS` | `-quiet-hours` | empty | `HH:MM-HH:MM` when set | local notifications | no | `internal/config` |
| Notification cooldown | `main.parseConfig` | `HH_NOTIFICATION_COOLDOWN` | `-notification-cooldown` | `15m` | greater than zero | local notifications | no | `internal/config` |
| Conversation display TTL | `main.parseConfig` | `HH_CONVERSATION_DISPLAY_TTL` | `-conversation-display-ttl` | `60s` | greater than zero | dashboard/read refresh | no | `internal/config` |
| Background inbox refresh | `main.parseConfig` | `HH_BACKGROUND_INBOX_REFRESH` | `-background-inbox-refresh` | `false` | boolean | dashboard/read sync | no | `internal/config` |
| Follow-up after application | `main.parseConfig` / dashboard parser | `HH_FOLLOW_UP_AFTER_APPLICATION` | `-follow-up-after-application` | `120h` | positive duration | follow-up domain | yes: converted to `FollowUpPolicy` | `internal/config` primitive + root composition |
| Follow-up after message | `main.parseConfig` / dashboard parser | `HH_FOLLOW_UP_AFTER_MESSAGE` | `-follow-up-after-message` | `72h` | positive duration | follow-up domain | yes: converted to `FollowUpPolicy` | `internal/config` primitive + root composition |
| Follow-up maximum | `main.parseConfig` / dashboard parser | `HH_FOLLOW_UP_MAX` | `-follow-up-max` | `2` | non-negative integer | follow-up domain | yes: converted to `FollowUpPolicy` | `internal/config` primitive + root composition |
| Follow-up minimum interval | `main.parseConfig` / dashboard parser | `HH_FOLLOW_UP_MINIMUM_INTERVAL` | `-follow-up-minimum-interval` | `72h` | positive duration | follow-up domain | yes: converted to `FollowUpPolicy` | `internal/config` primitive + root composition |
| Embedding provider | `main.parseConfig` | `EMBEDDING_PROVIDER` | — | empty | provider behavior remains downstream-owned | semantic retrieval | no | `internal/config` |
| Embedding model | `main.parseConfig` | `EMBEDDING_MODEL` | — | empty at config boundary | downstream provider default remains unchanged | semantic retrieval | no | `internal/config` |
| Dotenv file | `main` startup | — | — | `.env` in working directory | missing file tolerated; comments/quotes/escapes preserved; no shell execution | all config inputs | no | `internal/config` |
| Dashboard host | `main.parseDashboardOptions` | `HH_WEB_HOST` | `--host` | `127.0.0.1` | loopback-only business/safety validation | dashboard | no | package main until CLI extraction |
| Dashboard port | `main.parseDashboardOptions` | `HH_WEB_PORT` | `--port` | `8080` | valid TCP port | dashboard | no | package main until CLI extraction |

`HH_READ_ONLY` is an internal composition capability switch, not a public
configuration input. `POSTGRES_TEST_DATABASE_URL`, `HH_PERF_*`, and similar
test-only variables are excluded from the application contract.

## Secret handling

`DATABASE_URL` and `HH_AI_API_KEY` are marked secret inputs. `internal/config`
does not implement `String()`, debug dumps, or logging of configuration. Error
messages use field names and validation categories only; they do not include
DSNs, API keys, cookies, or other values.

## Transitional ownership

The root `Config` type remains temporarily so existing package-main call sites
and keyed test fixtures do not need a broad compatibility rewrite. The root
`parseConfig`, `getEnv`, `getEnvBool`, `parseDurationEnv`, `loadDotEnv`, and
scalar helper functions are narrow delegates/converters only. They do not own
defaults or parsing rules. They are scheduled for removal with the CLI and
composition-root extraction in R3.2.
