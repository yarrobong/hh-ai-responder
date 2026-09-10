# HH Test Answering

## Purpose

Use this skill when implementing, modifying, reviewing, debugging, or operating HH.ru application tests and questionnaires.

This includes:

* reading test tasks;
* parsing questions;
* parsing answer options;
* generating candidate answers;
* validating AI output;
* preparing a submission preview;
* submitting validated answers.

Test submission is a state-changing HH action and must be treated as high risk.

---

## Core invariant

Never submit an answer unless the system can prove that the generated answer maps correctly to the current HH test structure.

If parsing, mapping, validation, or candidate truthfulness is uncertain:

```text id="4xkr0e"
REVIEW_REQUIRED
```

No submission.

---

## Pipeline

Prefer an explicit pipeline:

```text id="82zqvy"
fetch current test
↓
parse tasks
↓
normalize task structure
↓
classify question type
↓
resolve candidate facts/context
↓
generate proposed answers
↓
strict structural validation
↓
semantic/safety validation
↓
preview
↓
write gateway
↓
submit
```

Do not allow AI generation output to flow directly into the HH write request.

---

## Current-state requirement

Always operate on the current test payload.

Do not submit answers based solely on:

* cached HTML;
* previous application attempts;
* old logs;
* stale task IDs;
* remembered option IDs.

Before a live write, verify that the task/option identifiers are still valid.

---

## Question representation

Normalize questions into a stable internal form.

Useful fields may include:

```text id="9vs5ev"
task_id
question_text
question_type
required
options
min_selected
max_selected
free_text_constraints
```

Option representation should preserve:

```text id="gt3zpv"
option_id
label
```

Never generate option IDs with AI.

---

## Supported question types

Handle only explicitly supported question types.

Examples may include:

```text id="2977q5"
single_choice
multiple_choice
free_text
boolean
numeric
```

If HH returns an unknown or unsupported type:

```text id="p22uub"
REVIEW_REQUIRED
```

Do not guess how to serialize it.

---

## AI responsibilities

AI may:

* understand the question;
* propose a free-text answer;
* choose among provided semantic options;
* explain reasoning internally;
* identify missing candidate information.

AI must not invent:

* task IDs;
* option IDs;
* candidate facts;
* certifications;
* experience;
* availability;
* salary expectations;
* completed assignments.

---

## Option mapping

For choice questions, separate semantic choice from ID mapping.

Example:

HH returns:

```text id="9wxf7m"
task_id: 42

options:
- id: 101, label: "Да"
- id: 102, label: "Нет"
```

AI should conceptually select:

```text id="0lyig4"
"Да"
```

Deterministic code should map it to:

```text id="7ih0g5"
101
```

Never ask AI to fabricate:

```text id="x82v7b"
{"option_id": 101}
```

without deterministic verification against the current task.

---

## Single-choice validation

For single-choice:

* exactly one valid option must be selected;
* selected ID must exist in the current task;
* duplicate option IDs are invalid;
* empty selection for a required task is invalid.

Any violation blocks submission.

---

## Multiple-choice validation

For multiple-choice:

* every selected ID must exist;
* duplicate IDs are invalid;
* minimum selection count must be respected;
* maximum selection count must be respected;
* no unknown options may be submitted.

Do not infer arbitrary min/max constraints if HH does not provide them.

---

## Free-text answers

Free-text answers must be truthful and relevant.

Do not invent:

* years of experience;
* project results;
* technologies;
* employer names;
* salary commitments;
* relocation;
* schedule availability.

Use Candidate Knowledge and trusted resume data.

If the question requires unknown candidate information, route to review rather than fabricate an answer.

---

## Candidate-specific questions

Examples:

```text id="q5xknk"
Сколько лет коммерческого опыта у вас с Python?
```

If trusted data only proves personal projects:

do not convert that into commercial experience.

Expected result:

```text id="z2e7ud"
REVIEW_REQUIRED
```

or a truthful answer if the exact format permits explaining lack of commercial experience.

---

## Technical knowledge questions

Technical questions may be answered by AI when they test knowledge rather than candidate history.

Examples:

* SQL;
* HTTP;
* Python;
* algorithms;
* programming concepts.

However:

* do not claim the candidate personally used a technology merely because AI knows the answer;
* distinguish knowledge-answer correctness from candidate-history claims.

---

## Trick/ambiguous questions

If a question is ambiguous enough that multiple answers are plausible and a wrong answer may materially affect the application:

```text id="1jiwro"
REVIEW_REQUIRED
```

Do not optimize for automatic completion rate.

---

## Parsing failures

Parsing uncertainty must fail closed.

Examples:

* task text missing;
* option labels missing;
* malformed HH payload;
* duplicate task IDs;
* missing required identifiers;
* HTML structure changed;
* unexpected nested tasks.

Do not generate "best effort" submission payloads after parser failure.

---

## AI output format

Use structured AI output where practical.

Example conceptual shape:

```text id="eqjll7"
{
  "answers": [
    {
      "task_id": "42",
      "selected_labels": ["Да"],
      "free_text": null
    }
  ]
}
```

Then deterministic code must verify and transform the result.

Do not trust JSON merely because it parses successfully.

---

## Structural validation

Before submission verify:

* every required task has an answer;
* no unknown task IDs exist;
* no task appears twice;
* no unexpected answer type is present;
* choice IDs map to current options;
* required free-text is non-empty;
* selection counts are valid;
* no extra tasks are submitted.

Validation should return explicit errors.

---

## Semantic validation

After structural validation, perform safety checks.

Look for unsupported statements such as:

```text id="fcl3ju"
"I have 5 years of Kubernetes experience"
```

when no trusted evidence exists.

A structurally valid answer may still be unsafe.

Unsafe semantic content blocks submission.

---

## Completeness

Do not submit a partially generated test when HH expects all required tasks.

If AI answered 8 of 10 required questions:

```text id="drz7cr"
submission blocked
```

Do not silently omit the remaining tasks.

---

## Duplicate tasks

Duplicate task IDs in either:

* HH input;
* parsed representation;
* AI output;

must be handled explicitly.

Do not let "last value wins" behavior silently determine submitted answers.

---

## Preview

Before a live submission, maintain a machine-readable preview where practical.

Useful fields include:

```text id="bvjl8x"
vacancy_id
task_id
question
selected_option_labels
free_text_preview
validation_status
```

This helps debug failed or unsafe submissions without exposing secrets.

---

## Dry-run

`HH_DRY_RUN=true` must block test submission.

Dry-run may perform:

* fetch;
* parse;
* AI generation;
* validation;
* preview;
* logging.

It must never call the state-changing HH submission endpoint.

---

## Write gateway

All test submission must pass through the explicit HH write boundary.

Do not submit tests directly from:

* AI orchestrators;
* parsers;
* preview handlers;
* dashboard code;
* matching code.

The write gateway should receive only validated submission data.

---

## Idempotency

Avoid accidental duplicate submissions.

Before write, inspect current application/test state where HH supports it.

Do not assume a failed HTTP client response proves that the server did not process the submission.

If submission outcome is ambiguous, do not automatically retry without checking state.

---

## Error classification

Distinguish failures such as:

```text id="px4chj"
test_parse_failed
unsupported_question_type
candidate_fact_unknown
ai_output_invalid
unknown_task_id
unknown_option_id
duplicate_task
missing_required_answer
semantic_validation_failed
dry_run_blocked
hh_submission_failed
submission_state_unknown
```

Structured errors are preferable to generic log strings.

---

## Logging

Log enough information to understand validation decisions.

Do not log:

* HH cookies;
* auth headers;
* API keys;
* unnecessarily complete private payloads;
* internal system prompts.

For free-text answers, consider whether full content is necessary before storing it in normal logs.

---

## Retry behavior

Safe retries may be appropriate for:

* read requests;
* AI generation;
* transient parsing dependencies.

Be conservative with write retries.

Never blindly retry a test submission after:

* timeout;
* connection reset;
* ambiguous response.

First determine whether HH already accepted the submission.

---

## Testing

Important regression tests include:

### Unknown option ID

AI/transform produces an ID absent from current HH options.

Expected:

```text id="6gdgwr"
validation failure
no write
```

### Partial required answers

One required task is unanswered.

Expected:

```text id="071an6"
validation failure
no write
```

### Duplicate task

AI output contains the same task twice.

Expected:

```text id="18bvjv"
validation failure
no write
```

### Unsupported type

HH returns an unfamiliar question type.

Expected:

```text id="oq9gyf"
REVIEW_REQUIRED
no write
```

### Candidate fact unknown

Question asks for commercial experience not present in trusted data.

Expected:

```text id="ojni3u"
no invented answer
REVIEW_REQUIRED
```

### Valid technical question

Current task and options parse correctly, AI selects a valid semantic answer, deterministic mapping succeeds.

Expected:

```text id="6m1nma"
validated submission payload
```

### Dry-run

Everything validates while `HH_DRY_RUN=true`.

Expected:

```text id="3dyzif"
preview allowed
HH submission request count = 0
```

### Ambiguous write result

Submission connection fails after request transmission.

Expected:

```text id="mjs97h"
do not blindly resubmit
verify current state first
```

---

## Refactoring test code

Before modifying test-answering logic, locate:

1. HH test fetch;
2. parser;
3. internal task representation;
4. AI prompt/schema;
5. answer mapper;
6. structural validator;
7. semantic validator;
8. preview/logging;
9. write gateway;
10. tests.

Do not merge parser + AI + submission into one opaque function.

Keep boundaries auditable.

---

## Completion checklist

Before declaring a test-answering task complete:

* current HH task structure is used;
* unsupported task types fail closed;
* AI cannot invent task/option IDs;
* every selected option is validated;
* required questions are complete;
* duplicate tasks are rejected;
* candidate facts remain truthful;
* semantic validation runs before write;
* dry-run blocks submissions;
* ambiguous write outcomes are not blindly retried;
* tests never perform real HH writes;
* relevant regression tests pass;
* global AGENTS.md rules remain satisfied.
