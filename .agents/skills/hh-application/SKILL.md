# HH Application

## Purpose

Use this skill when implementing, modifying, debugging, reviewing, or testing the HH.ru application flow.

This includes:

* vacancy evaluation;
* application eligibility;
* cover-letter preparation;
* application preflight;
* test/questionnaire detection;
* application submission;
* application result handling.

The application pipeline must prioritize safety, truthfulness, and candidate control over application volume.

---

## Core invariant

A vacancy match is not authorization to submit an application.

The expected flow is:

```text
vacancy discovered
↓
trusted structured HH data
↓
deterministic checks
↓
AI extraction/scoring where useful
↓
MATCH / REJECT / REVIEW_REQUIRED
↓
read-only preflight
↓
write eligibility check
↓
application write
↓
result verification/logging
```

Any uncertainty affecting a live write must stop the flow.

---

## Decision states

Use the project-level meanings:

```text
MATCH
REJECT
REVIEW_REQUIRED
```

### MATCH

The known information indicates that the vacancy is suitable enough to continue evaluation.

MATCH does not itself authorize a write.

### REJECT

There is reliable evidence of a meaningful mismatch.

### REVIEW_REQUIRED

Important information is unknown, ambiguous, contradictory, or unsafe to automate.

REVIEW_REQUIRED must never automatically submit an application.

---

## Deterministic checks first

Run deterministic checks before asking AI where practical.

Examples include:

* salary rules;
* currency rules;
* location;
* work format;
* vacancy archive state;
* existing response;
* configured exclusions;
* known experience constraints;
* structured HH fields.

AI should not override trusted structured data.

---

## Hard requirements

When analyzing vacancy requirements, distinguish:

```text
met
missing
unknown
```

Definitions:

### met

Reliable candidate evidence satisfies the requirement.

### missing

Reliable information shows that the candidate does not satisfy the requirement.

### unknown

Available information is insufficient.

Do not convert unknown into missing.

Absence of a skill in the HH resume alone is normally insufficient to prove that the candidate lacks the skill.

---

## Requirement extraction

AI may extract:

* requirement text;
* category;
* vacancy evidence;
* whether the requirement appears mandatory or optional.

AI must not make the final safety-sensitive decision about whether the candidate actually meets a hard requirement.

Use deterministic code and trusted candidate knowledge for that decision.

---

## Optional requirements

Do not automatically reject a vacancy because of:

* nice-to-have technologies;
* preferred experience;
* optional tools;
* weakly worded preferences.

Only reliable mandatory constraints should trigger deterministic rejection.

---

## Structured HH data

Prefer HH structured fields over free-text interpretation where available.

Examples:

* vacancy state;
* application state;
* work experience;
* area;
* salary;
* employment type;
* work format;
* response/test state.

Do not ask AI to reinterpret a reliable structured value into a contradictory state.

---

## Application preflight

Immediately before a real application write, perform a read-only preflight.

Preflight should verify current state rather than relying solely on stale search results.

Where supported by HH data, check:

* vacancy still exists;
* vacancy is not archived;
* candidate has not already responded;
* application is permitted;
* test/questionnaire presence;
* cover-letter requirement;
* relevant location/work-format state;
* relevant current application state.

If a critical field cannot be determined:

```text
REVIEW_REQUIRED
```

Do not guess.

---

## Time-of-check / time-of-use

Vacancy state may change between search and application.

Do not assume that earlier data remains valid indefinitely.

Live writes should depend on fresh preflight state.

---

## Cover letters

Cover letters must:

* be relevant to the vacancy;
* use only supported candidate facts;
* avoid invented experience;
* avoid unsupported technologies;
* avoid fake enthusiasm or personal history;
* avoid promises the candidate has not authorized.

Prefer concise, vacancy-specific letters over generic large templates.

If important candidate information required for the letter is unknown, either:

* omit the unsupported claim;
* or route to review if the omission makes the application unsafe.

---

## Tests and questionnaires

If the application includes a test or questionnaire:

1. fetch the current tasks/options;
2. parse them strictly;
3. generate candidate answers;
4. validate the generated structure;
5. verify all IDs/options against the current HH response;
6. only then permit submission.

Never submit:

* malformed output;
* partial task sets;
* invented option IDs;
* duplicated answers;
* answers produced from a parsing failure;
* guessed answers where the question cannot be understood reliably.

Validation failure must stop submission.

---

## Write boundary

All application writes must pass through the project's explicit HH write boundary/gateway.

Do not add hidden writes inside:

* read clients;
* parsing helpers;
* AI orchestration;
* matching functions;
* logging helpers.

A function whose purpose appears read-only must remain read-only.

---

## Dry-run

`HH_DRY_RUN=true` must prevent the final application write.

The entire flow may still perform:

* search;
* vacancy reads;
* evaluation;
* AI analysis;
* cover-letter generation;
* test preview;
* preflight;
* logging.

But the state-changing HH request must not occur.

When changing application code, explicitly verify dry-run behavior.

---

## Idempotency

Where practical, application processing should tolerate retries without duplicate submissions.

Before submitting, use current HH state to detect an existing response.

Do not rely solely on local logs to determine whether an application already exists.

---

## Error handling

Failures should be classified usefully.

Examples:

```text
vacancy unavailable
already responded
test requires review
preflight incomplete
authentication failure
HH API failure
validation failure
write blocked by dry-run
write rejected by safety gate
```

Do not turn network/parser/API uncertainty into a write attempt.

---

## Logging

Log enough structured information to diagnose decisions.

Useful fields may include:

```text
vacancy_id
decision
reason
preflight_result
dry_run
write_attempted
write_result
test_present
cover_letter_required
```

Do not log:

* credentials;
* cookies;
* authorization headers;
* unnecessary raw private payloads;
* complete sensitive prompts.

---

## Refactoring application code

Before changing application behavior:

1. locate all application write entrypoints;
2. locate dry-run checks;
3. locate preflight;
4. locate vacancy decision state;
5. locate test handling;
6. locate cover-letter generation;
7. locate structured logging;
8. search all callers of changed functions.

Do not create a second parallel application path unless explicitly required.

Prefer one auditable write pipeline.

---

## Tests

Important regression cases include:

* MATCH does not bypass preflight;
* REVIEW_REQUIRED never writes;
* REJECT never writes;
* archived vacancy never writes;
* already-responded vacancy never writes;
* uncertain critical preflight never writes;
* dry-run never writes;
* malformed test answer never writes;
* valid safe application reaches the write gateway exactly once.

Use mocked HH endpoints or `httptest.Server`.

Tests must never submit a real HH application.

---

## Completion checklist

Before declaring an application-flow task complete:

* deterministic checks run before AI where appropriate;
* unknown is not treated as missing;
* MATCH alone cannot write;
* fresh read-only preflight exists;
* REVIEW_REQUIRED blocks writes;
* dry-run blocks writes;
* test answers are validated;
* no hidden write path was introduced;
* application writes remain auditable;
* relevant tests pass;
* global AGENTS.md rules remain satisfied.
