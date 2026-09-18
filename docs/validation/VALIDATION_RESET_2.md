# Validation RESET-2

Дата: 2026-09-19.

## Starting commit

`cfaacbf9d81b53ca8fa2616b9d42ad36c25c488c`

## Candidate

- Vacancy: `137244538`, `Python-разработчик (в офис)`
- Company: `Институт Радиоэлектронных Систем`
- Previous blocker: `archived state is unknown`

## Fresh browser auth and vacancy read

- Browser transport: Playwright, headed
- Fresh browser auth: `AUTH_OK`
- Final URL: same regional HH vacancy URL
- Page class: normal authenticated vacancy page
- Challenge/login: not detected
- Vacancy-scoped apply action: present
- Archived/closed marker: absent
- Responded marker: absent
- Raw authenticated HTML: not stored or reported

## Fresh complete preflight

- Active: `ACTIVE`
- Active evidence: `PROVIDER_CAN_APPLY`, `PROVIDER_ARCHIVED_FALSE`,
  `VACANCY_SCOPED_APPLY_ACTION`
- AlreadyResponded: `NO`
- Responded evidence: `EXPLICIT_NOT_RESPONDED`
- CanApply: `YES`
- Apply evidence: provider `responseImpossible=false` plus vacancy-scoped apply
  action
- TestRequired: `NO` (known)
- LetterRequired: `NO` (known)
- LetterAllowed: `YES` (`letterMaxLength` known and positive)
- Archived: `false` (known)

## Root cause and fix

The vacancy page had a live, vacancy-scoped apply action, but the previous
preflight treated the absence of an archive flag as unknown. The browser HTML
inspection also searched script text, so JavaScript-only login/archive/response
phrases could create false markers. The response page exposed the decisive
provider state in `applicantVacancyResponseStatuses[137244538]` and
`vacancyResponsePopup.vacancy`; the parser previously selected the generic
`redirectConfig` before those scoped containers and did not read their nested
`shortVacancy` fields.

RESET-2 adds an explicit `ACTIVE`/`INACTIVE`/`UNKNOWN` contract, vacancy-scoped
evidence and contradiction fail-closed behavior, visible-DOM marker handling,
and scoped response-state parsing. No browser transport, cookie architecture,
search planner, router weights, AI threshold/prompt, resume registry, or write
path was changed.

## Controlled validation path

- Detail: fresh read succeeded
- Route: selected `Backend-разработчик (Python/Django) / автоматизация и
  интеграции`, resume ID `279225596`, router score `42`
- AI: score `85`, recommendation `APPLY`
- Final decision: `MATCH`
- Hard missing: none
- Hard unknown: none
- Cover letter generated: `YES`
- Pilot: `READY_FOR_EXPLICIT_SEND`

The pilot nonce was issued for the preview and remained unused. No application
was sent.

Real HH writes: 0
Application POST: 0
