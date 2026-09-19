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
