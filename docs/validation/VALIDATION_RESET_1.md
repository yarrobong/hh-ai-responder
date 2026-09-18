# Validation reset 1

Дата: 2026-09-19.

## BASELINE

Cookie loading: PASS — Netscape rows with empty values are accepted, expired
rows are filtered, HH-domain cookies are applied before first navigation, and
diagnostics contain metadata only.

Browser doctor: `AUTH_OK` in the headed run. Home, `/applicant/my_resumes`, and
a bounded vacancy page opened in Playwright. CAPTCHA bypass: NO.

The live read-only run completed search → dedup → responded/preflight → detail
→ resume routing → AI assessment → hard requirements. The page HTML limit was
raised because the authenticated resume bootstrap is about 1 MB and was being
cut at the previous 200 KB limit.

## REAL RUN

HH session: `AUTH_OK`
Raw vacancies: 74
Unique: 43
Already responded: 3
Fresh: 40
Preflight unknown: 1
Detail reads: 40
Detail successes: 40
Route selected: 3
Route ambiguous: 37
AI evaluated: 3
AI APPLY: 1
AI UNCERTAIN: 0
AI DO_NOT_APPLY: 2
MATCH: 1
REJECT: 2
REVIEW_REQUIRED: 38
Pilot eligible: 0

Pilot: `NONE`

The closest fresh blockers were: two `SCORE_BELOW_THRESHOLD`, one
`preflight: archived state is unknown`, three `ALREADY_RESPONDED`, and four
`ROUTE_AMBIGUOUS` examples. Thresholds were not changed.

Real HH writes: 0
Application POST: 0

The generated machine-readable report is `career_agent_latest.json`; it was
not staged as a source artifact.

RESET-1 implementation commit: `cfaacbf9d81b53ca8fa2616b9d42ad36c25c488c`.
This is the commit from which RESET-2 validation started.
origin/main at RESET-2 start: `cfaacbf9d81b53ca8fa2616b9d42ad36c25c488c`.
Working tree: dirty by the implementation and validation changes; `out/` remains
untracked and untouched.
