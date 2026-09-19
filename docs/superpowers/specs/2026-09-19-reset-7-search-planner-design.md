# RESET-7 Search Planner Design

Дата: 2026-09-19
Проект: `yarrobong/hh-ai-responder`
Исходный stable baseline: `7f3b74e932bdb014b610377e1546631e706825d1` (`origin/main`)

## Current architecture

### Runtime flow

Текущий discovery проходит через один существующий механизм:

```text
HH resumes + registry overrides + Candidate Profile
    -> careeragent.NormalizeResumes / ApplyRegistryOverrides
    -> careeragent.PlanSearches
    -> runtime.rebuildCareerAgentSearchProfiles
    -> HH web/browser read transport
    -> per-profile pagination
    -> vacancy-ID deduplication and source accumulation
    -> existing detail/preflight
    -> RESET-6 RouteResume
    -> AI/local policy
```

RESET-7 продолжает улучшать `careeragent.PlanSearches`; второй search mechanism
не создаётся.

### Search profile generation today

`PlanSearches` получает `ResumeProfile`, `CandidateSignals` и
`SearchConstraints`. Для каждого enabled resume текущая очередь строится в
таком порядке:

1. `resume.SearchHints` с причиной `resume search hint`;
2. `resume.DesiredRole` и `resume.Title` с причиной `resume role/title`;
3. `CandidateSignals.Roles` с причиной `candidate role signal`;
4. `roleExpansions(resume.Title, resume.DesiredRole)` с причиной
   `deterministic role expansion`;
5. `CandidateSignals.Skills` только если предыдущие источники не дали ни
   одного термина.

Очереди enabled resumes обходятся round-robin до
`HH_MAX_SEARCH_PROFILES` (по умолчанию `16`). Дубликаты удаляются только по
паре `resume.ID + normalized query`. Профиль получает стабильный ID из
resume ID и normalized query.

Текущая deterministic expansion знает только несколько строковых признаков:

- `python` -> `Python backend`, `Python developer`;
- `django` -> `Django developer`;
- `support` -> `technical support`, `technical specialist`;
- `integration`/`implementation` -> `implementation specialist`,
  `integration specialist`;
- `automation` -> `automation engineer`, `AI automation`.

Профиль сейчас содержит `id`, `resume_id`, `resume_title`, `query`, `reason`,
`search_period_days` и `url.Values`. Runtime добавляет к provider query
`area`, `resume`, `order_by=publication_time`, `search_period` и
`items_on_page=50`. `career_agent_include` и `career_agent_exclude` также
попадают в автоматически построенный URL, но они не являются типизированными
HH web-search filters и не должны считаться доказанным способом сузить выдачу.

### Manual profiles and precedence

`HH_SEARCH_URLS` разбирается раньше auto planner. Если он не задан, используется
`HH_SEARCH_URL`; при явном CLI `-u` CLI имеет приоритет над env. Непустые
manual URLs полностью заменяют auto-generated profiles. Пустые manual values
позволяют перейти к auto planner. Если auto planner не создал ни одного
профиля, сохраняется legacy fallback `default-search` с `resume`.

Manual URL parser сохраняет `area`, `resume` и любые иные query parameters,
удаляя только `page` и принудительно устанавливая
`order_by=publication_time`, `search_period` и `items_on_page=50`. RESET-7
сохраняет это поведение и явно маркирует такие профили как
`MANUAL_PROFILE`.

### Area, filters and pagination

В auto planner area берётся из `CandidateSignals.PreferredLocation` и
передаётся как текущий provider parameter `area`. Из уже работающих web
fixtures и runtime-кода подтверждены следующие параметры auto search:

```text
text, area, resume, order_by, search_period, items_on_page
```

Текущая реализация не строит `professional_role`, `experience`, `employment`,
`schedule` или `search_field`; RESET-7 не будет придумывать эти параметры.
Они могут появиться только отдельным изменением после проверки реального HH
web transport и fixtures.

Runtime сейчас последовательно запрашивает страницы, начиная с page `0`, и
останавливается только после пустой страницы. В нормальном пути
`fetchVacanciesFromSearchProfiles` передаёт `maxUnique=0`, поэтому
`HH_MAX_VACANCIES_PER_RUN` ограничивает уже обработку после discovery, а не
число provider search reads. Это означает, что увеличение числа profiles
может создать фактически неограниченный fan-out browser GETs.

RESET-7 вводит отдельный deterministic read-only cap, не связанный с
application cap:

```text
HH_MAX_SEARCH_PAGES_PER_PROFILE=3
HH_MAX_SEARCH_PAGES_PER_RUN=48
```

Параметры также доступны как `--max-search-pages-per-profile` и
`--max-search-pages-per-run`. Значения положительные; defaults выбраны так,
чтобы не урезать текущий baseline (в нём ни один profile не потребовал более
двух страниц), но ограничить worst-case fan-out при budget `16`. Страница с
индексом `0` считается первой. Остановка по cap до получения пустой страницы
помечается как `truncated` с причиной `MAX_SEARCH_PAGES_PER_PROFILE` или
`MAX_SEARCH_PAGES_PER_RUN`; это не считается полным discovery. Profiles,
которые не были начаты из-за run cap, тоже получают явный статус.

Счётчик raw включает provider hits, возвращённые профилем, включая повторы
между страницами и профилями. Dedup выполняется по vacancy ID, сохраняет
first-seen порядок и добавляет все профили, через которые vacancy была
найдена. Cap не меняет HH write paths и не превращает обрезанный набор в
complete/negative search result.

### Baseline run before RESET-7 changes

До изменения planner выполнен read-only auto-generated run:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_MAX_VACANCIES_PER_RUN=100
STORAGE_BACKEND=json
./hh-ai-responder -u "" --career-agent-result /tmp/reset7-old.json career-agent --shadow
```

Сохранены вне source commit:

```text
/tmp/reset7-old.json
/tmp/reset7-old.json.md
/tmp/reset7-old-profiles.json
/tmp/reset7-baseline-meta.json
```

Cookies, session secrets и private raw bodies в эти артефакты не включались.
Baseline metadata содержит starting SHA, timestamp, enabled resume IDs,
generated profiles, raw count per profile, union IDs, router outcomes и
zero-write audit.

Фактический baseline:

| Metric | Value |
|---|---:|
| Enabled resumes | 4 |
| Generated profiles | 7 |
| `raw_hits` | 209 |
| `distinct_discovered` | 161 |
| `processed_by_router` | 50 (detail requested and RESET-6 routing entered) |
| `not_processed_due_to_run_cap` | 101 |
| Other pre-router gates | 10 already responded; old schema did not isolate every other gate |
| Duplicate contributions | 48 |
| Router outcomes among processed subset | `ROUTE_SELECTED=13`, `ROUTE_AMBIGUOUS=13`, `NO_SUITABLE_RESUME=5`, `ROLE_OUT_OF_SCOPE=5`, `ROUTE_LOW_EVIDENCE=14` |
| AI evaluated | 10 |
| MATCH | 0 |
| Review required | 44 |
| Real HH writes | 0 |
| Application POST | 0 |

Generated profiles were:

| Type in current implementation | Resume source | Query | Raw |
|---|---|---|---:|
| `resume role/title` | automation/integrations/implementation | `Специалист по автоматизации и интеграциям инженер внедрения` | 6 |
| `resume role/title` | backend | `Backend-разработчик` | 34 |
| `resume role/title` | Python/Django + automation/integrations | `Backend-разработчик (Python Django) автоматизация и интеграции` | 1 |
| `resume role/title` | technical specialist | `Технический специалист` | 80 |
| `deterministic role expansion` | Python/Django + automation/integrations | `Python backend` | 20 |
| `deterministic role expansion` | Python/Django + automation/integrations | `Python developer` | 48 |
| `deterministic role expansion` | Python/Django + automation/integrations | `Django developer` | 20 |

### Observed root causes

1. The planner is title-driven rather than family-driven. It emits the long
   resume title as one query and gives variants mainly to one Python resume.
2. The same enabled resume can carry several role-family signals, but the
   planner does not allocate one bounded queue per supported family. This
   makes a Python resume consume the variant budget while automation and
   support receive only broad title queries.
3. `Backend-разработчик` and `Технический специалист` are valid trusted
   titles but are broad discovery phrases. Their current profiles are not
   paired with bounded trusted role phrases such as support, implementation,
   Python/Django or integration variants.
4. Generic skills and explicit exclusion lists are carried as broad custom
   query parameters. They do not replace role phrases and can obscure which
   intent caused a result; generic skills alone must not start a profile.
5. Current provenance is sufficient for weak router input, but profile
   reason/type, role family and per-profile outcome quality are not retained
   as first-class telemetry.
6. Discovery raw volume is not relevance. The baseline reached 161 distinct
   discovered vacancies but only 50 entered the bounded detail/router path;
   the search layer therefore needs better candidate-pool composition and
   explicit discovery coverage, not a weaker RESET-6 route gate.

## Goals

- Improve discovery recall and career relevance for the role families actually
  represented by enabled resumes:
  `PYTHON_BACKEND`, `WEB_BACKEND`, `AUTOMATION_INTEGRATIONS` and
  `TECH_SUPPORT`.
- Derive every auto profile from trusted enabled resume data, Candidate Profile
  role strategy, and existing explicit search hints.
- Generate bounded role-specific queries with separate strong role phrases,
  specific technology variants and generic supporting terms.
- Allocate the existing `HH_MAX_SEARCH_PROFILES` budget fairly across eligible
  supported search families before variants and fallback.
- Preserve broad discovery through a clearly labelled, limited fallback so
  unusual vacancy titles are not silently lost.
- Preserve all existing deduplication, pagination, manual URL compatibility,
  detail/preflight, RESET-6 routing and AI/local policy boundaries.
- Make profile provenance and quality measurable without using it for automatic
  self-tuning.
- Keep RESET-7 strictly read-only: `HH_DRY_RUN=true`,
  `HH_WRITE_ENABLED=false`, real HH writes `0`, Application POST `0`.

## Non-goals and protected boundaries

RESET-7 does not change:

- `BrowserHHClient` or auth/cookies architecture;
- HH OAuth, `api.hh.ru` or developer API usage;
- RESET-6 role-family/routing policy, AI prompt, AI threshold or
  hard-requirement policy;
- detail/preflight semantics;
- application send, cover-letter generation, test submission, chat writes,
  nonce, reconciliation, write gateway or manual confirmation;
- self-learning, profile deletion or profile weighting based on one run;
- a global blacklist or a global `NOT` query.

Search remains discovery only. `MATCH` or `READY_FOR_EXPLICIT_SEND` in a
read-only report never authorizes an HH write.

## Search intent model

### Trusted input

For each enabled `ResumeProfile`, the planner may use only:

- `Title` / `DesiredRole`;
- `SearchHints`, `IncludeKeywords` and explicit trusted `ExcludeKeywords`;
- `ResumeIdentity.PrimaryRoleFamilies` and `SecondaryRoleFamilies`;
- `StrongPositiveAnchors`, `CoreSkills`, `DomainSignals` and other values
  deterministically derived from that profile;
- Candidate Profile primary/secondary roles and explicitly configured skills,
  but only as supporting signals attached to a family already supported by an
  enabled resume.

Candidate Profile is not allowed to invent a resume family. Vacancy titles,
descriptions, AI output and previous search results are never inputs to profile
generation.

### Reuse of RESET-6 role families

The planner consumes the existing runtime family vocabulary and does not
create a parallel classifier:

```text
PYTHON_BACKEND
WEB_BACKEND
AUTOMATION_INTEGRATIONS
TECH_SUPPORT
```

Unsupported RESET-6 families such as `SYSTEM_ADMIN`, `SYSTEM_ANALYST`, `ONE_C`,
`FRONTEND` and `FLUTTER` are not searched unless an enabled resume actually
derives that family. A family inferred only from a generic skill such as Linux,
Git, SQL, Docker, REST or API does not qualify.

### Eligible search family

Budget allocation must not depend only on the stored RESET-6
`PrimaryRoleFamilies` list. The planner derives an `eligible search family`
projection using the same `RoleFamily` vocabulary and canonical evidence, but
checks all trusted enabled-resume sources:

```text
title
desired role
search hints
strong role anchors
core role-specific evidence
```

A family is eligible when these sources provide explicit, family-specific
support. A family that RESET-6 stores as secondary can therefore receive a
targeted slot: for example, `WEB_BACKEND` is eligible when a Python/backend
resume also has trusted backend/web role evidence and core PHP/Laravel/Node or
equivalent role-specific anchors. A generic or weak secondary signal does not
become eligible automatically.

The derivation is a planner-facing evidence projection, not a second set of
role families and not a change to RESET-6 routing policy. It records the
evidence sources that made the family eligible so fixtures and telemetry can
explain every reserved slot.

### Vocabulary classes

Each candidate query is assembled from one bounded role phrase plus zero or one
specific supporting anchor:

| Class | Source | Search use |
|---|---|---|
| Strong role phrase | trusted title/desired role, family-specific RU/EN alias derived from it | required base of targeted profile |
| Specific technology/domain term | trusted strong anchor/core skill/domain signal | optional variant, never independent |
| Generic supporting term | Git, Linux, SQL, Docker, REST, API, CRM and equivalent generic signals | metadata/filter evidence only; never a profile by itself |

The planner never creates a huge AND query containing the full skills list.
`Python Django SQL Linux Docker REST Git CRM` is explicitly disallowed.
Language variants are bounded and generated only when the enabled resume's
trusted family and anchors support them; a translated term is not an added
candidate fact.

## Profile generation

### Profile contract

The existing `SearchProfile` remains the transport input. RESET-7 adds
explainable metadata to it (or an equivalent sidecar that survives runtime
conversion):

```text
profile_type: TARGETED | ADJACENT | BROAD_FALLBACK | MANUAL
reason: PRIMARY_ROLE | SECONDARY_ROLE | TECHNOLOGY_VARIANT |
        LANGUAGE_VARIANT | BROAD_FALLBACK | MANUAL_PROFILE
role_family: optional RESET-6 RoleFamily
source_resume_ids: one or more stable enabled resume IDs
query: normalized provider text query
provider params: area, resume, order_by, search_period, items_on_page
```

The profile ID is stable from canonical role family, normalized query,
provider-affecting filters and source resume identity. Equivalent generated
profiles collapse deterministically before budget allocation. Exact duplicate
query strings from the same resume, duplicate hints, RU/EN aliases that
normalize to the same phrase, and repeated family expansions produce one
profile. Different `resume` provider filters remain distinct unless their
canonical provider parameters are identical; if they collapse, all source
resume IDs are retained.

### Family-specific targeted profiles

For every distinct eligible search family, subject to budget, emit one
`TARGETED / PRIMARY_ROLE` profile from the shortest trusted role phrase that
retains the family anchor. Then emit bounded variants in deterministic order:

- `PYTHON_BACKEND`: Python/backend and Django/backend variants only when the
  resume has the corresponding trusted Python/Django/backend evidence;
- `WEB_BACKEND`: backend/web plus PHP/Laravel/Node or other specific technology
  variants only when that evidence exists in the enabled resume;
- `AUTOMATION_INTEGRATIONS`: automation, integration, implementation, API
  integration, CRM or webhook variants only when the title/domain/strong
  anchors support them;
- `TECH_SUPPORT`: technical support, support engineer, application/product
  support, troubleshooting or diagnostics variants only when the trusted
  profile supports the phrase.

These are bounded alias families, not a global hardcoded query list. The
family activation condition and each emitted term must be traceable to the
resume identity fixture that enabled it. A long resume title may remain as one
primary or adjacent profile for recall, but it cannot be the only query for a
family that has additional trusted role anchors.

Secondary role families emit `ADJACENT` profiles with reason
`SECONDARY_ROLE` only after every eligible search family has received its
reserved slot. They use the same trusted-data and one-specific-anchor limits.

### Broad fallback

The planner emits at most one `BROAD_FALLBACK` profile, and only after primary
and useful variant slots have been allocated. Its query is built from up to
three explicit trusted role phrases from Candidate Profile or enabled resume
titles. A literal query such as `phrase1 OR phrase2 OR phrase3` is not
provider-safe merely because URL encoding or a mock fixture accepts it. Until
a controlled read-only `BrowserHHClient` validation proves that HH applies the
expected alternative semantics, the planner uses one proven trusted role
phrase for `BROAD_FALLBACK`; if no such phrase exists, the fallback is
omitted. After provider semantics are proven, a bounded literal `OR` query may
be enabled as a separately validated behavior. The fallback never uses a
generic-only skill list or unsupported family names. It never invents `IT`,
`specialist` or another generic role.

The fallback is labelled and measured separately. It is never allowed to take
reserved eligible-family slots and is omitted when the configured budget is
too small to retain all eligible-family reservations.

### Generic and negative terms

`IncludeKeywords` and generic skills may annotate intent and may be used as
supporting evidence for a targeted query, but they do not create a profile.
The auto planner does not turn a generic term into `text` by itself.

Explicit trusted `ExcludeKeywords` remain available to existing deterministic
policy. RESET-7 does not turn them into provider-wide `NOT` clauses. A
family-specific exclusion can affect a profile only when it is an explicit
trusted configuration for that resume and a fixture/live validation proves
that the provider syntax is safe. No global `NOT Linux`, `NOT 1C`,
`NOT frontend` or `NOT admin` is permitted.

## Profile budgeting and ordering

`HH_MAX_SEARCH_PROFILES` remains the single upper bound for auto-generated
profiles and keeps its current default `16`. Budget allocation is deterministic:

1. collect distinct **eligible search families** from enabled resumes;
2. reserve one targeted slot per distinct eligible family while slots remain;
3. round-robin additional targeted variants across eligible families and source
   resumes;
4. allocate eligible adjacent/secondary profiles that did not already receive
   a targeted slot;
5. allocate at most one broad fallback in the final remaining slot;
6. stop at the configured maximum.

Within each group, order is stable by role-family order used by RESET-6, stable
resume ID, reason priority, and normalized query. This guarantees that one
Python resume cannot consume all slots before support or automation receives a
useful targeted profile. If the budget is smaller than the number of distinct
eligible search families, deterministic round-robin gives each family the earliest
available slot; fallback never displaces an eligible supported family. Thus a
strongly evidenced `WEB_BACKEND` family stored as RESET-6 secondary is covered
before generic or weak secondary families.

Manual profiles do not consume the auto-generated budget because manual URL
configuration is an explicit override. They retain their current order and
provider parameters.

## Provenance and deduplication

Every vacancy source record must retain, for each matching profile:

```text
search_profile_id
role_family
query
profile_type
profile_reason
source_resume_id(s)
```

The existing all-matching-profile accumulation remains authoritative. A
vacancy found by several profiles is processed once after vacancy-ID dedup but
keeps every bounded source. Provenance is a weak routing signal only; it never
selects a resume or overrides RESET-6 evidence.

Manual sources are represented with `profile_type=MANUAL` and
`reason=MANUAL_PROFILE`. Legacy consumers that only understand profile name or
resume ID continue to receive those fields.

## Profile-quality telemetry

Telemetry is observational and does not modify future profiles automatically.
Discovery and processing/router telemetry are separate populations. A search
can discover many more vacancy IDs than the current runtime is allowed to
process, and router outcomes must never be divided by the larger discovery
union.

Run-level discovery coverage reports at least:

```text
raw_hits
distinct_discovered
processed_by_router
not_processed_due_to_run_cap
not_processed_by_other_pre_router_gate
discovery_complete
search_pages_fetched
search_pages_truncated
```

`processed_by_router` is the number of distinct discovered vacancies for which
the RESET-6 preliminary/final router was entered. `not_processed_due_to_run_cap`
counts distinct discovered vacancies skipped by the existing
`HH_MAX_VACANCIES_PER_RUN` before router/detail processing. Other pre-router
exits (for example already responded or deterministic cheap rejection) are
reported separately in `not_processed_by_other_pre_router_gate`; they are not
silently folded into router outcomes.

The following route and final-decision counters use only the
`processed_by_router` population:

```text
ROUTE_SELECTED
ROUTE_AMBIGUOUS
NO_SUITABLE_RESUME
ROLE_OUT_OF_SCOPE
ROUTE_LOW_EVIDENCE
AI evaluated
MATCH
REJECT
REVIEW_REQUIRED
```

Discovery coverage is shown separately; the report must not present
`distinct_discovered=161` and a bounded processed subset as if they were one
router denominator.

For every profile, report:

```text
profile id, type, reason, role family, source resume IDs, query
raw_hits
distinct_profile_vacancies
exclusive_vacancies
overlap_vacancies
union_new_contribution
processed_vacancies
truncated, truncation_reason, pages_fetched
ROUTE_SELECTED
ROUTE_AMBIGUOUS
NO_SUITABLE_RESUME
ROLE_OUT_OF_SCOPE
ROUTE_LOW_EVIDENCE
AI evaluated
MATCH
REJECT
REVIEW_REQUIRED
```

Because a vacancy can match multiple profiles, per-profile outcome counts are
multi-attributed diagnostics and must be accompanied by the union totals. The
union report remains the source of truth for accounting. Define profile
contributions as follows:

- `raw_hits`: every provider vacancy record returned for this profile,
  including repeated IDs on different pages;
- `distinct_profile_vacancies`: unique vacancy IDs returned by this profile;
- `exclusive_vacancies`: IDs whose all-profile source set contains only this
  profile;
- `overlap_vacancies`: IDs in this profile's distinct set whose all-profile
  source set contains at least one other profile;
- `union_new_contribution`: IDs first introduced into the global union while
  this profile is scanned;
- `processed_vacancies`: this profile's source-attributed IDs among the
  `processed_by_router` population.

`raw_hits - distinct_profile_vacancies` is only an optional provider-repeat
diagnostic. It is not an overlap definition. A vacancy found by several
profiles contributes to each relevant profile's overlap and processed
diagnostics, while union totals count it once.

Derived router diagnostics use `processed_by_router` as denominator:

```text
selected_yield = ROUTE_SELECTED / processed_by_router
out_of_scope_yield = ROLE_OUT_OF_SCOPE / processed_by_router
low_evidence_yield = ROUTE_LOW_EVIDENCE / processed_by_router
```

Per-profile route yields use that profile's `processed_vacancies` denominator;
run-level route yields use `processed_by_router`. Zero denominators are
reported as unavailable, not zero. Truncation is a validation signal, not a
successful empty result. Telemetry contains IDs, labels, queries, counts and
safe decision categories; it does not contain cookies, API keys, complete AI
prompts, or private raw response bodies.

## Manual URL compatibility

The documented precedence remains:

```text
explicit CLI -u / explicit CLI search values
    > HH_SEARCH_URLS (non-empty)
    > HH_SEARCH_URL fallback
    > auto planner when both are empty
    > legacy resume-only fallback when auto planner has no profile
```

The implementation must preserve `||` splitting, area/resume values, URL host
handling, page removal, freshness period, publication ordering and 50-item
page size. It must not silently combine manual and auto profiles. The README
must document the actual precedence and the new profile provenance fields when
the implementation lands.

## Validation methodology

Live search is time-varying, so old and new runs are not treated as a perfect
A/B experiment.

### A. Frozen old baseline

Use the four `/tmp/reset7-*` artifacts captured at SHA
`7f3b74e932bdb014b610377e1546631e706825d1` for exact old profile/query,
raw-count, union-ID and router-outcome comparisons. Do not overwrite them.

### B. New profile-level live telemetry

Run the new planner with `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false`, then
inspect every targeted/adjacent/fallback profile's raw, contribution, overlap
router outcome and truncation counters. Confirm both read caps, the number of
search page GETs and explicit `discovery_complete`/`truncated` status. Any
provider challenge, detail error or unknown critical HH state is recorded as a
validation failure/review condition, never converted into a positive result.

Before enabling any multi-phrase fallback, run a controlled read-only
BrowserHHClient validation against a bounded fixture/live search window. URL
encoding acceptance is insufficient: the validation must show that HH treats
the literal `OR` expression as alternatives rather than as literal text or a
different query language. Until that evidence exists, validate the one-proven-
phrase fallback or validate that the fallback is omitted.

### C. Bounded union safety review

Compare `old final vacancy IDs ∪ new final vacancy IDs` and classify:

- old IDs still found by new profiles;
- old IDs missing from new discovery;
- new IDs added by targeted profiles;
- new IDs added only by broad fallback.

For every old supported-role vacancy missing from the new run, inspect the
query/family reason before accepting the result. A regression is not hidden by
different publication timing.

### D. Manual sample

Review at least 10 vacancies from targeted profiles, up to 10 from broad
fallback when available, every `ROUTE_SELECTED` vacancy, and representative
`ROLE_OUT_OF_SCOPE` and `ROUTE_LOW_EVIDENCE` cases. If a group is smaller,
review all available items. Record whether the title/detail is career-relevant,
why it was discovered, and whether RESET-6 preserved the safe outcome.

Success means a more career-relevant candidate pool without obvious loss of
important supported-role vacancies. `selected >= 10` and a fixed
`out-of-scope < 5%` target are deliberately not success criteria.

## Tests

The implementation must add deterministic fixtures for the existing
`careeragent.PlanSearches` contract and runtime provenance/reporting:

1. Python backend profiles are generated only from a matching trusted resume;
   RU/EN variants stay bounded.
2. Support profiles are generated from a support resume, including bounded
   support role variants.
3. Automation/integration profiles are generated from a matching resume,
   including implementation/integration variants supported by its trusted
   anchors.
4. Git/Linux/SQL/API alone do not create a broad profile.
5. System-admin and 1C families are not searched when no enabled resume
   supports them.
6. A strongly evidenced `WEB_BACKEND` stored as RESET-6 secondary becomes an
   eligible search family and receives a targeted slot; a generic/weak
   secondary family does not.
7. Each eligible supported search family receives a slot when the budget
   permits; Python variants cannot consume the entire budget first.
8. Duplicate and equivalent hints/aliases collapse deterministically, while
   distinct provider resume filters remain safe and all collapsed source IDs
   survive.
9. Manual `HH_SEARCH_URL` and `HH_SEARCH_URLS` precedence and parameter
   preservation remain compatible.
10. Broad fallback uses one proven trusted phrase or is omitted; literal `OR`
    semantics are not enabled by URL/mock encoding alone.
11. Per-profile `raw_hits`, `distinct_profile_vacancies`,
    `exclusive_vacancies`, `overlap_vacancies`, `union_new_contribution` and
    `processed_vacancies` account correctly for multi-profile discovery.
12. Run-level discovery counters remain separate from
    `processed_by_router`; route/final outcomes and yields use only the
    processed denominator, and truncation is explicit.
13. Each profile and the run respect the read-page caps, report truncation,
    and do not silently call a truncated search complete.
14. Profile provenance survives vacancy dedup with all matching profiles.
15. Shadow mode with the new planner cannot invoke an HH write; no test sends
    an application or requires real HH cookies.

Use deterministic fixtures, `httptest.Server` and mocked AI endpoints where a
transport test is needed. Live validation remains a separate read-only run.

## Expected files and scope

RESET-7 implementation should stay within the existing discovery boundary:

- `internal/careeragent/` — family-aware planner, profile metadata,
  canonicalization and planner tests;
- `internal/runtime/career_agent_runtime.go` — conversion of planner metadata
  into runtime profiles and source provenance;
- `internal/runtime/runtime.go` — only the existing read-only discovery
  telemetry/dedup boundary, if needed;
- `internal/runtime/career_agent_command.go` and related report tests — safe
  per-profile telemetry serialization;
- `internal/config/defaults.go`, `internal/config/flags.go`,
  `internal/config/validation.go` and config tests — positive bounded search
  page settings with CLI-over-env precedence;
- `internal/runtime/multi_search_test.go` and career-agent runtime tests —
  provider parameter, bounded pagination, truncation, discovery-vs-processed
  accounting, dedup and manual compatibility fixtures;
- `README.md` and `example.env` — document
  `HH_MAX_SEARCH_PAGES_PER_PROFILE=3`,
  `HH_MAX_SEARCH_PAGES_PER_RUN=48`, precedence, truncation semantics and
  separate discovery/router telemetry;
- `docs/validation/VALIDATION_RESET_7_SEARCH.md` — frozen-baseline comparison,
  controlled fallback semantics validation, union recall review and the
  read-only live sample.

No protected auth, write, router-policy, AI-prompt, nonce, reconciliation or
cover-letter files are in scope. This design itself is the only source file
change in RESET-7 design stage; no implementation code is authorized by this
spec.

## Safety boundaries and definition of done

Every RESET-7 validation command must set:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
```

The design is accepted only when the implementation preserves all old write
gates, never turns search provenance into a final route decision, and reports
`Real HH writes: 0` and `Application POST: 0`. The final implementation stage
must additionally run the repository's required Go formatting, tests, vet,
build and `git diff --check` checks before any completion claim.
