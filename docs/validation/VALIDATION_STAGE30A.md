# Validation Stage 30A — live application pilot preview

Date: 2026-09-16 (Asia/Yekaterinburg)

Stage 30A was run as an explicit read-only preview. The command forced
`HH_DRY_RUN=true`, disabled `HH_WRITE_ENABLED`, enabled the `HHReadOnly`
transport guard, and disabled every automatic writer. No application POST was
executed.

## Candidate and fresh provider state

Requested candidate:

- Vacancy: [Python Backend Developer, 137428040](https://perm.hh.ru/vacancy/137428040)
- Company: Miles&Miles
- Fresh detail: title, company, URL, location and description were read from
  the current vacancy detail endpoint; detail was not taken from the Stage
  29.6 artifact.
- Fresh timestamp: `2026-09-16T11:42:52Z`
- Active: `UNKNOWN` (the provider response did not expose a reliable archived
  bit, so the pilot fails closed)
- Already responded: `YES`
- Can apply: `NO`
- Test required: `YES`
- Cover letter: required `NO`; allowed `UNKNOWN`

The candidate is therefore blocked before any write. The next Stage 29.6
shortlist previews checked during this run were `137418714`, `137384867`,
`136364927`, and `136577315`; each had a fresh `already responded=YES` and
`can_apply=NO` state. `137402396` and `137430509` additionally failed the
deterministic resume-route selection. No safe replacement candidate was found.

## Resume routing

The current router was rebuilt from the fresh `/applicant/my_resumes` read;
the resume ID was not hardcoded.

- Selected resume: `Backend-разработчик (Python/Django) / автоматизация и интеграции`
- Resume ID/hash: `a89be050ff10a4a4fc0039ed1f786946636470`
- Alternative scores: selected `55`, `Backend-разработчик` `50`,
  `Специалист по автоматизации и интеграциям / инженер внедрения` `45`,
  `Технический специалист` `19`
- The selected resume was activated and its current facts were used for the
  AI assessment and cover-letter preview.

## AI and deterministic decision

- AI score: `65`
- AI recommendation: `UNCERTAIN`
- Hard requirements: none extracted
- Hard missing: none
- Hard unknown: none
- Final local decision: `BLOCKED` (provider preflight), not an application
  authorization

The provider blockers are `already responded`, `can_apply=NO`, required
unsupported test submission, and unknown active state. AI output did not
override those facts.

## Cover-letter preview

The letter was generated for this vacancy from the selected resume's trusted
candidate context. The exact stored content hash is:

`5e23906ec6b91dc5ab17085efe23489d6b9cd3714afc9182983743a1c4d6cbcf`

Exact content:

```text
Ярослав Паршаков

Приветствую! Откликаюсь на вакансию Python Backend Developer, так как мой опыт в разработке на Django и автоматизации бизнес-процессов соответствует части требований. В компании BizonVR разрабатывал backend-сервисы на Python с использованием Django и PostgreSQL, реализовывал REST API и интеграции с внешними системами, включая CRM. Также автоматизировал рабочие процессы, что позволило сократить ручное участие в операциях. Готов изучить недостающие технологии, такие как Kafka или asyncio, для более эффективной работы в команде.
```

The letter contains no Career Agent/AI disclosure or internal score. It makes
no claim of FastAPI experience or a duration of commercial experience. The
candidate remains blocked by provider state regardless of the preview letter.

## Nonce, hash and future 30B readiness

- Content hash: computed from the exact letter bytes and persisted with the
  private pilot artifact.
- Nonce: not issued because the preview did not reach a safe ready state.
- Future send checks: fresh vacancy detail, fresh resume availability and
  routing, fresh application preflight, same vacancy, same selected resume,
  unchanged content hash, unused nonce, one-application budget and one-total-
  HH-write budget.
- Future transport: `HHWriteGateway`/application executor only; no chat, test,
  resume-touch, job-search-status or follow-up writer is enabled.
- Future reconciliation: provider read is mandatory after a transport that
  looks successful. The only final outcomes are `CONFIRMED`, `UNKNOWN`, and
  `FAILED`; ambiguous transport is `UNKNOWN` with no retry.

## Verification

The lifecycle tests cover changed vacancy, changed resume, changed content
hash, used nonce, one-application budget, ambiguous transport with no retry,
confirmed reconciliation, preview writer isolation, and pilot cover-letter
noise rejection. The actual Stage 30A command performed only fresh HH reads
and AI generation.

**REAL HH WRITES = 0**
