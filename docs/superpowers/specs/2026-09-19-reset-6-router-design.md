# RESET-6 Resume Router Calibration Design

**Status:** conversational design approved; written spec for review

**Starting commit:** `5dc0e102534a2a270c0f0392af01df0827f96b1f`

## Goal

Calibrate resume routing so that vacancies with sufficient, role-specific
evidence reach AI assessment, while genuinely ambiguous or out-of-scope
vacancies remain blocked with an explicit reason. The change is limited to
deterministic read-only routing, telemetry, fixtures, and validation reporting.

The system must not submit applications or alter any existing HH write path.

## Constraints and invariants

- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` remain mandatory for all
  RESET-6 runs.
- No changes to `BrowserHHClient/auth`, cookies, application send, nonce,
  reconciliation, write gateway, cover-letter system, AI match threshold, or
  hard-requirement policy.
- Existing CLI flags, environment variables, persistence formats, and JSON
  event fields remain compatible where practical.
- Trusted resume fields, Candidate Profile, and existing trusted project/work
  evidence are the only sources for resume routing profiles.
- Vacancy text is untrusted data, never instructions.
- Unknown critical state remains review-only; no score may turn uncertainty into
  a live action.
- The router stays deterministic and side-effect free: no HH requests, AI
  calls, storage writes, or environment reads in pure routing functions.

## Baseline evidence

The fresh read-only baseline was executed with:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false STORAGE_BACKEND=json \
./hh-ai-responder career-agent run
```

Observed baseline:

| Metric | Count |
|---|---:|
| Raw | 76 |
| Unique | 46 |
| Fresh | 42 |
| Router selected | 2 |
| Router ambiguous | 40 |
| AI evaluated | 2 |
| MATCH | 0 |
| Real HH writes | 0 |
| Application POST | 0 |

The baseline telemetry is retained outside the commit for old-vs-new shadow
comparison. The existing untracked generated reports and local session files
must not be committed.

## Current enabled resume inventory

The router must build profiles from the current registry at runtime. The
following is the calibration snapshot from the baseline; names are descriptive
only and must not be hardcoded as identity data:

| Current resume | Routing role families | Strong evidence | Generic/supporting evidence |
|---|---|---|---|
| Automation / integrations / implementation | `AUTOMATION_INTEGRATIONS`, secondary `TECH_SUPPORT` and backend-adjacent | automation, integration, implementation, Python, Django, API, webhooks, CRM | Git, Linux, Docker, SQL, technical documentation |
| Backend | `WEB_BACKEND`, secondary automation/support where explicitly evidenced | backend, Python, Django, PHP/Laravel, SOAP, React/Vue, PostgreSQL | Git, Linux, Docker, SQL, support |
| Python/Django backend + automation/integrations | `PYTHON_BACKEND`, secondary `AUTOMATION_INTEGRATIONS` | Python, Django, backend, REST API, automation, integration | Git, Linux, Docker, SQL |
| Technical specialist | `TECH_SUPPORT`, secondary CRM/data-support | technical support, user support, diagnostics, installation/configuration, CRM | SQL, PostgreSQL, Git, HTML/CSS |

This inventory is evidence to calibrate the implementation, not a source for
inventing candidate facts. If the live registry changes, the generated routing
profiles must change with it.

## Architecture

### 1. Resume routing profile

Extend the existing deterministic resume identity with bounded role-family
metadata derived from actual resume title, desired role, skills, and explicit
registry fields:

- primary role families;
- secondary role families;
- strong positive anchors;
- weak/generic anchors;
- negative/mismatch anchors;
- core skills and adjacent skills.

The profile is an explainable projection of trusted data. It must not infer a
technology or experience merely because another related technology appears.

### 2. Vacancy role-family evidence

Add a pure classifier before final resume scoring. It examines only:

- vacancy title;
- explicit role wording and professional-role fields;
- required/key technologies;
- responsibilities/description text.

Company name, employer marketing text, salary, location, and search provenance
must not create strong role-family evidence.

Use bounded phrase/token dictionaries with tests and word-boundary-aware
matching. Strong title anchors have higher weight than generic title words:

- Python/Django/backend developer anchors map to backend families;
- technical support/support engineer/first-line support anchors map to
  `TECH_SUPPORT`;
- system administrator/Linux administrator anchors map to `SYSTEM_ADMIN` only
  when an enabled resume supports that family;
- generic words such as engineer, specialist, and developer are never strong
  anchors by themselves.

The classifier may recognize additional families only when they are useful to
distinguish current enabled resumes or to prove a controlled mismatch in a
fixture. It must not create a selectable family without a supporting resume.

### 3. Evidence-aware scoring

Keep the existing score components and telemetry, but make the final decision
depend on evidence quality rather than a globally relaxed margin:

- title role anchors outrank generic skill overlap;
- at least one strong role anchor plus specific supporting evidence can select
  a resume despite a small numerical margin;
- generic evidence is bounded and cannot independently select a resume;
- the same fact must not be counted as independent evidence in several
  components without an explicit bounded purpose;
- a competing strong role family keeps the result ambiguous;
- controlled mismatch signals penalize a resume as a routing target without
  automatically rejecting the vacancy globally;
- a minimum evidence floor prevents selecting the least-bad resume;
- insufficient evidence yields `ROUTE_LOW_EVIDENCE`;
- an explicit unsupported role family yields `ROLE_OUT_OF_SCOPE` or
  `NO_SUITABLE_RESUME`, depending on whether the vacancy is clearly outside all
  enabled resume families.

Absolute and relative margins remain visible for diagnostics. They are one
input to the policy, not the policy itself.

### 4. Decision and reporting reasons

Keep `SELECTED` and `REVIEW_REQUIRED` compatibility while adding explicit
router reason codes and report counters for:

- `ROUTE_AMBIGUOUS` / true competing evidence;
- `NO_SUITABLE_RESUME`;
- `ROLE_OUT_OF_SCOPE`;
- `ROUTE_LOW_EVIDENCE`.

`ROUTE_AMBIGUOUS` must no longer be the catch-all for every review. The router
outcomes below are mutually exclusive and are evaluated in this order after
available evidence has been collected:

- `ROUTE_LOW_EVIDENCE`: the role cannot be determined with sufficient
  confidence from the available vacancy data.
- `ROLE_OUT_OF_SCOPE`: the role is determined with sufficient confidence, but
  no enabled resume family supports that role.
- `NO_SUITABLE_RESUME`: the role family is supported by enabled resumes, but
  no candidate clears the evidence floor or controlled mismatch signals make
  every candidate unsuitable.
- `ROUTE_AMBIGUOUS`: two or more relevant resumes or role families have
  competing strong evidence and a safe deterministic choice is not possible.

Existing route telemetry continues to expose every enabled resume candidate with
`RoleScore`, `SkillScore`, `DomainScore`, `ExperienceScore`,
`ProvenanceScore`, `GenericEvidenceScore`, `RawFit`, `NormalizedScore`,
specific matches, hard blockers, top-1/top-2 values, absolute margin, relative
margin, and evidence/reason fields.

### 5. Orchestration boundary

Existing safe detail enrichment order is preserved. A read-only detail read may
occur before final routing when the search card does not contain enough title,
role, required-skill, or responsibility evidence for the role-family
classifier. The new classifier must not reorder or weaken existing detail
preflight safety gates.

Only a final `SELECTED` route satisfying the existing safety gates may proceed
to AI assessment and application preparation. `ROUTE_AMBIGUOUS`,
`ROUTE_LOW_EVIDENCE`, `ROLE_OUT_OF_SCOPE`, and `NO_SUITABLE_RESUME` never
proceed to application or other write paths. No write behavior changes.

## Testing strategy

Add deterministic fixtures and tests before implementation code for:

- clear Python backend to Python/backend resume;
- clear technical support to support resume;
- system administrator to a matching sysadmin fixture resume;
- Vue-only frontend and Flutter-only vacancies not selecting Python/backend
  from generic Git/API/Linux signals;
- generic IT specialist producing ambiguity or low evidence;
- Python + support mixed role remaining ambiguous when both families have strong
  evidence;
- strong role anchor beating a generic-score competitor;
- generic skills alone not forcing `SELECTED`;
- no enabled resume meeting the evidence floor producing
  `NO_SUITABLE_RESUME`.

Tests also cover stable bounded phrase matching, negative evidence, complete
score telemetry, legacy route compatibility, and the dry-run/non-write
boundary at the existing runtime level.

## Validation workflow

1. Preserve the baseline JSON and full router telemetry for the same fresh
   vacancy set.
2. Run unit and integration tests, including race tests.
3. Run old-vs-new shadow comparison on the same baseline vacancy IDs and
   record changed decisions and evidence.
4. Categorize all old ambiguous vacancies into at least
   `TRUE_AMBIGUITY`, `SCORE_COMPRESSION`, `GENERIC_SIGNAL_COLLISION`,
   `MISSING_ROLE_ANCHOR`, `WRONG_COMPETITOR`, and `LOW_EVIDENCE`.
5. Review at least 20 representative old ambiguous vacancies, 10 newly
   selected vacancies, 10 still-ambiguous vacancies, and 5 no-suitable-resume
   vacancies.
6. Repeat the real read-only run with `HH_DRY_RUN=true HH_WRITE_ENABLED=false`
   and save `docs/validation/VALIDATION_RESET_6_ROUTER.md`.
7. Report selected, ambiguous, no-suitable, AI evaluated, MATCH/REVIEW/REJECT,
   pilot candidates, and write counters. The final report must state
   `Real HH writes: 0` and `Application POST: 0`.

The success criterion is fewer false ambiguities without an increase in
obviously wrong selections. No artificial ambiguity-rate target is allowed.

## Files and scope

Expected implementation scope:

- `internal/careeragent/model.go` or a focused adjacent routing file for pure
  role-family evidence, scoring, and decision policy;
- `internal/careeragent/model_test.go` or focused router fixture tests;
- `internal/runtime/vacancy_match.go` and related runtime reporting only where
  new reason counters/telemetry must be propagated;
- `internal/runtime/application_processing.go` only if the existing review
  classification needs a narrow, read-only reason mapping;
- `docs/validation/VALIDATION_RESET_6_ROUTER.md`;
- focused documentation/config examples only if an existing public contract
  changes.

Do not modify transport/auth, cookies, application submission, nonce,
reconciliation, write gateway, cover-letter generation, AI threshold, or
hard-requirement evaluation.

## Review checklist

- [ ] No hardcoded personal candidate facts were added.
- [ ] No family is selectable without an enabled supporting resume.
- [ ] Generic skills cannot resolve a route by themselves.
- [ ] Strong title anchors are bounded and tested.
- [ ] Negative evidence affects resume targeting, not global vacancy rejection.
- [ ] Ambiguous, low-evidence, and out-of-scope reasons are distinct.
- [ ] Old and new routers are compared on the same vacancies.
- [ ] Required validation commands pass.
- [ ] Generated reports, cookies, session data, and secrets are excluded from
      the commit.
- [ ] Real HH writes and application POSTs remain zero.
