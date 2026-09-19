# RESET-7 SEARCH validation

Validated at `2026-09-19T10:44:12Z` with the controlled shadow command below.
The implementation baseline is the pushed docs-only plan commit
`IMPLEMENTATION_START_SHA=c0dedad`; `74cab00` was not used as the source-code
diff baseline.

## Safety and command

```bash
HH_DRY_RUN=true \
HH_WRITE_ENABLED=false \
HH_MAX_SEARCH_PAGES_PER_PROFILE=3 \
HH_MAX_SEARCH_PAGES_PER_RUN=48 \
HH_MAX_VACANCIES_PER_RUN=100 \
STORAGE_BACKEND=json \
/tmp/reset7-hh-ai-responder -u "" \
  --career-agent-result /tmp/reset7-new.json career-agent --shadow
```

Observed write audit:

- `real_hh_writes=0`
- `application_post=0`
- `shadow_write_count=0`
- application preview events: `0`
- `accounting_pass=true`

The frozen baseline `/tmp/reset7-baseline-meta.json` independently reported
`real_hh_writes=0`, `application_post=0`, `shadow_write_count=0`, and
`accounting_pass=true`. Its pre-RESET-7 discovery assertion was intentionally
not applicable because it does not contain the new `distinct_discovered`
field.

## Discovery and router populations

| Metric | Value |
|---|---:|
| Enabled resume count | 4 |
| Auto profiles | 16 |
| Raw hits | 419 |
| Distinct discovered | 187 |
| Processed by router | 50 |
| Not processed due to vacancy run cap | 132 |
| Not processed by other pre-router gates | 5 |
| Search pages fetched | 41 |
| Search pages truncated | 1 |
| Discovery truncated | `true` |
| Discovery complete | `false` |

The single truncation was explicit:
`MAX_SEARCH_PAGES_PER_PROFILE` stopped the `TECH_SUPPORT` profile after three
pages. No profile was left unstarted by the global cap. A capped run is
therefore reported incomplete and is not interpreted as negative discovery.

Router outcomes were counted only for the 50 router-entered vacancies:

```json
{
  "NO_SUITABLE_RESUME": 5,
  "ROLE_OUT_OF_SCOPE": 3,
  "ROUTE_AMBIGUOUS": 13,
  "ROUTE_LOW_EVIDENCE": 17,
  "ROUTE_SELECTED": 12
}
```

Corresponding router yields use `processed_by_router` as denominator:

```json
{
  "NO_SUITABLE_RESUME": 0.10,
  "ROLE_OUT_OF_SCOPE": 0.06,
  "ROUTE_AMBIGUOUS": 0.26,
  "ROUTE_LOW_EVIDENCE": 0.34,
  "ROUTE_SELECTED": 0.24
}
```

## Per-profile accounting

The report contained per-profile `raw_hits`, `distinct_profile_vacancies`,
`exclusive_vacancies`, `overlap_vacancies`, `union_new_contribution`,
`pages_fetched`, and truncation state for all 16 profiles. Overlap is computed
from complete source sets; it is not `raw_hits - distinct_profile_vacancies`.

The emitted role-family counts were:

```json
{
  "AUTOMATION_INTEGRATIONS": 3,
  "FRONTEND": 1,
  "PYTHON_BACKEND": 6,
  "SYSTEM_ANALYST": 1,
  "TECH_SUPPORT": 1,
  "WEB_BACKEND": 4
}
```

The first run exposed unrelated automation titles being emitted under other
families. The regression fixture `TestPlanSearchesDoesNotExpandUnrelatedResumeTitleAcrossFamilies`
was added; after narrowing title candidates to family-matching evidence, the
focused and full careeragent/runtime suites passed and the controlled rerun no
longer emitted that unsafe expansion. Mixed trusted titles containing explicit
Python/backend evidence remain eligible for their evidenced family.

## Frozen-baseline union review

The old/new ID-set review used `jq` over `/tmp/reset7-old.json` and
`/tmp/reset7-new.json` without emitting IDs or private vacancy bodies:

| Set comparison | Count |
|---|---:|
| Old unique | 161 |
| New unique | 187 |
| Retained | 89 |
| Old missing | 72 |
| New only | 98 |

Of the 72 old IDs missing from the new set, 5 had strong role evidence in the
old report: one `AUTOMATION_INTEGRATIONS`, one `PYTHON_BACKEND`, and three
`TECH_SUPPORT`. Three had previously selected resumes; the remaining two were
old rejected/review-required outcomes. The missing set is consistent with the
changed bounded query order, current HH result timing, and the explicit
vacancy/page caps; sampled supported-role titles were still present in the new
union. No automatic application was made from this review.

## Manual and fallback checks

The following read-only tests passed:

```text
TestManualProfilesGetManualMetadataWithoutChangingPrecedence
TestBuildVacancySearchProfilesPreservesAreaAndResume
TestBroadFallbackUsesOneProvenPhraseUntilORValidation
```

Manual profiles retain URL parameters and are labelled `MANUAL_PROFILE` /
`MANUAL`. The new report contained zero literal `OR` queries. Provider
alternative semantics were not proven by a controlled BrowserHHClient probe;
result: `OR_NOT_ENABLED`. The planner therefore keeps the one-proven-phrase or
omitted fallback policy.

## Protected-path audit

The audit used:

```bash
git diff "${IMPLEMENTATION_START_SHA}"..HEAD -- \
  internal/browsersession internal/adapters/hh/read/browser.go \
  internal/runtime/hh_write_gateway.go internal/runtime/application_processing.go \
  internal/runtime/vacancy_preflight.go internal/careeragent/router_role_family.go
git diff --check
```

Only telemetry-only lines appear in `internal/runtime/application_processing.go`:
router-entry counting, final-decision population accounting and summary
finalization. Browser/auth transport, cookies, write gateway, application
submission, preflight and RESET-6 router policy are unchanged.

## Final repository checks

These checks are run after the validation evidence commit as part of the final
handoff:

```bash
gofmt -w .
HH_DRY_RUN=true HH_WRITE_ENABLED=false go test ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go vet ./...
HH_DRY_RUN=true HH_WRITE_ENABLED=false go build ./...
git diff --check
```

## Final gate addendum

The checks below were run after the initial report. They are the final RESET-7
gate result and supersede the preliminary union counts above where the current
provider returned a different bounded read set.

### Unexpected family eligibility audit

The production-default run after the fix emitted only the supported planner
families:

```json
{
  "AUTOMATION_INTEGRATIONS": 3,
  "PYTHON_BACKEND": 6,
  "TECH_SUPPORT": 3,
  "WEB_BACKEND": 4,
  "FRONTEND": 0,
  "SYSTEM_ANALYST": 0
}
```

The two unexpected profiles in the pre-fix run were:

| profile/query | family | source resume | title / desired role | search hints | strong role anchor | core/adjacent evidence | pre-fix rule |
|---|---|---|---|---|---|---|---|
| `frontend developer` | `FRONTEND` | `hh-resume-6aac4e53ff110b3a8e0039ed1f4f5a68684d41` | `Специалист по автоматизации и интеграциям / инженер внедрения` / same | none | none | `JavaScript`, `React`, `TypeScript` | `specificEvidence >= 2` |
| `system analyst` | `SYSTEM_ANALYST` | `hh-resume-9d9a7b3aff10b8e0070039ed1f756941615344` | `Backend-разработчик` / same | none | none | `SOAP`, `SQL` | `specificEvidence >= 2` |

These were regressions: adjacent/core technology tokens were allowed to
activate unsupported families without a trusted family anchor. The fix keeps
specific-only eligibility for the four supported RESET-6 planner families, so
secondary `WEB_BACKEND` and `TECH_SUPPORT` evidence is preserved, while
`SYSTEM_ADMIN`, `FRONTEND`, `FLUTTER`, `ONE_C` and `SYSTEM_ANALYST` require a
title/desired-role/search-hint family anchor. Focused fixtures cover both
leakage cases and verify that a backend resume still derives `WEB_BACKEND`.

### Five strong-role missing vacancies audit

The exact five requested baseline misses were checked against the production
default run, the `4/48` diagnostic, the `5/80` diagnostic, and direct read-only
provider pages. The old source query and old router result come from the
frozen baseline; selected resume means the old route's selected resume, when
one existed.

| vacancy | old title | old family | old source query | old router / selected resume | current bounded result | provider state | query conclusion |
|---:|---|---|---|---|---|---|---|
| `137495670` | Middle backend developer | `PYTHON_BACKEND` | `Backend-разработчик` | `ROUTE_AMBIGUOUS` / none | missing in `3/48`, `4/48`, `5/80` | HTTP 200, page marker `Вакансия закрыта` | current `Backend-разработчик` vocabulary remains; not a live recall regression |
| `136813509` | Бизнес-аналитик/специалист по внедрению CRM | `AUTOMATION_INTEGRATIONS` | `Технический специалист` | `ROUTE_SELECTED` / automation-integration resume; old fact verification was `403` | missing in all current runs | HTTP 200, page marker `Вакансия закрыта` | old broad query was replaced by bounded implementation/integration phrases; current state is unavailable |
| `136970301` | Старший специалист технической поддержки (L1) | `TECH_SUPPORT` | `Технический специалист` | `ROUTE_AMBIGUOUS` / none | recovered in `3/48`, `4/48`, `5/80`; then bounded by vacancy cap | HTTP 200, page marker `Вакансия закрыта` | support-specific query discovers it; cap was not a remaining current recall defect |
| `137026634` | Инженер технической поддержки (Fortinet) | `TECH_SUPPORT` | `Технический специалист` | `ROUTE_SELECTED` / `Технический специалист` resume | recovered in `3/48`, `4/48`, `5/80`; then bounded by vacancy cap | HTTP 200, page marker `Вакансия закрыта` | support-specific query discovers it; cap was not a remaining current recall defect |
| `137540483` | Специалист технической поддержки | `TECH_SUPPORT` | `Технический специалист` | `ROUTE_SELECTED` / `Технический специалист` resume | recovered in `3/48`, `4/48`, `5/80`; then bounded by vacancy cap | HTTP 200, page marker `Вакансия закрыта` | support-specific query discovers it; cap was not a remaining current recall defect |

The two still-missing IDs were not replaced by other vacancies. Both are
provider-unavailable at the time of this audit, so neither is classified as an
active search regression.

### High-cap diagnostic

Commands were read-only with `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`.

| run | raw hits | distinct discovered | pages fetched | profile truncation | run truncation | complete |
|---|---:|---:|---:|---:|---:|---|
| production `3/profile + 48/run` | 502 | 211 | 43 | 3 (`TECH_SUPPORT`) | 0 | no |
| diagnostic `4/profile + 48/run` | 502 | 211 | 46 | 0 | 0 | yes |
| diagnostic `5/profile + 80/run` | 502 | 211 | 46 | 0 | 0 | yes |

The `4/48` and `5/80` runs were identical on this provider sample and both
recovered the same three support IDs. The final production run also retained
those three IDs; the higher cap added no distinct IDs. No unbounded retry or
cap escalation was performed.

### Support profile quality

The final production run emitted three `TECH_SUPPORT` profiles because two
eligible resumes share the family:

| query | source | raw / distinct | pages | status |
|---|---|---:|---:|---|
| `специалист технической поддержки` | automation/integration resume | 60 / 60 | 3 | truncated at profile cap |
| `инженер технической поддержки` | automation/integration resume | 57 / 57 | 3 | truncated at profile cap |
| `инженер технической поддержки` | technical-specialist resume | 52 / 52 | 3 | truncated at profile cap |

Their union contained 113 discovered vacancies. Among the six that entered the
router sample, outcomes were: `SELECTED=0`, `AMBIGUOUS=3`,
`NO_SUITABLE=0`, `OUT_OF_SCOPE=1`, `LOW_EVIDENCE=2`. The remaining support
discoveries were outside the bounded 50-vacancy router sample, not silently
treated as negative matches.

### Production cap decision

Keep the production defaults at `3/profile + 48/run`. On the current data this
is the smallest tested policy with the same 211-ID union as `4/48` and `5/80`;
raising the cap would make the run complete but did not recover any additional
currently readable vacancy. The explicit truncation remains visible in the
telemetry and report. Revisit only if a future read-only comparison shows an
active supported-role ID missing at `3/48` and present at a bounded higher cap.

### Recall conclusion

The family leakage is resolved by deterministic regression tests. Of the exact
five strong-role baseline misses, three are recovered by the current role-safe
support queries and two are proven closed by provider-side read evidence. No
active supported-role loss remains unexplained.

```text
SUPPORTED_ROLE_RECALL_ACCEPTABLE
```

Final production-default handoff:

```text
Eligible search families: AUTOMATION_INTEGRATIONS=3, PYTHON_BACKEND=6, TECH_SUPPORT=3, WEB_BACKEND=4
Raw hits: 502
Distinct discovered: 211
Processed: 50
Production page caps: 3/profile, 48/run
Discovery complete/truncated: false/true
Five strong-role baseline misses: recovered=3, provider-unavailable=2, actual regressions=0
Supported-role recall verdict: SUPPORTED_ROLE_RECALL_ACCEPTABLE
Router selected: 12
Ambiguous: 12
No suitable: 6
Out of scope: 3
Low evidence: 17
AI evaluated: 12
MATCH: 0
Fallback OR: NOT_ENABLED
Real HH writes: 0
Application POST: 0
```
