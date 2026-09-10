# Candidate Knowledge

## Purpose

Use this skill when changing, extending, reviewing, or debugging the Candidate Knowledge system.

The Candidate Knowledge system is the persistent source of candidate-specific knowledge that is broader than the HH resume.

It may contain:

* confirmed candidate facts;
* skills;
* education;
* work history;
* projects;
* preferences;
* constraints;
* candidate stories;
* examples of past work;
* information explicitly confirmed by the candidate;
* facts imported from trusted external sources.

The goal is to gradually build a reliable machine-readable representation of the candidate without inventing information.

---

## Core principle

Candidate knowledge must distinguish between:

* known;
* unknown;
* inferred;
* externally observed;
* explicitly confirmed.

Never silently convert inference into fact.

Unknown information must remain unknown until reliable evidence is available.

---

## Sources of knowledge

Candidate information may come from multiple sources.

Typical sources include:

1. explicit candidate answers;
2. HH resume/profile;
3. manually configured candidate data;
4. verified GitHub/project information;
5. previous candidate stories;
6. trusted structured imports.

Sources are not equally authoritative.

Prefer explicit candidate confirmation over inference.

Prefer trusted structured data over AI interpretation.

---

## Provenance

Every important fact should have provenance where practical.

A knowledge record should be able to answer:

* what is known;
* where it came from;
* when it was learned;
* whether the candidate confirmed it;
* how confident the system is;
* whether it can be used for employer-facing claims.

Do not store important facts as anonymous strings without source information if the data model already supports provenance.

---

## Suggested fact states

Where the existing architecture permits it, distinguish states similar to:

```text
confirmed
observed
inferred
unknown
rejected
stale
```

Meaning:

### confirmed

The candidate explicitly confirmed the fact or it comes from an authoritative trusted source.

May normally be used in employer-facing communication.

### observed

The fact is directly visible in a trusted artifact.

Example:

A repository clearly contains Django code.

This may prove:

```text
has used Django in this project
```

It does not automatically prove:

```text
commercial Django experience
expert Django knowledge
three years of Django experience
```

### inferred

A reasonable conclusion exists but has not been confirmed.

Do not present inferred information to employers as confirmed fact.

### unknown

There is insufficient information.

Do not guess.

### rejected

The candidate explicitly stated that the fact is false or outdated.

Do not reintroduce it from weaker sources.

### stale

The fact may once have been true but requires revalidation.

---

## Evidence boundaries

Do not over-generalize evidence.

Examples:

```text
repository contains a tool configuration file
```

may support:

```text
candidate has used the tool in this project
```

It does not prove:

```text
production administration experience
unrelated platform knowledge
professional operations experience
```

Likewise:

```text
candidate knows Django
```

does not prove:

```text
FastAPI
Flask
Celery
DRF
```

unless those facts have separate evidence.

---

## Skill representation

Avoid representing skills as a single boolean when richer data is useful.

Where compatible with the current model, prefer information such as:

```text
skill
evidence
source
confidence
last_confirmed_at
commercial_experience
personal_project_experience
self_assessed_level
```

Do not invent numeric proficiency levels unless the candidate explicitly supplied them or the project has a defined assessment method.

---

## Candidate stories

Candidate stories are reusable evidence for:

* cover letters;
* employer replies;
* interviews;
* vacancy matching.

A story should preferably include:

```text
situation
task
action
result
technologies
candidate_role
evidence
```

Do not fabricate missing results.

If no measurable result is known, store the result as unknown rather than generating one.

---

## Knowledge acquisition

When information important to future decisions is unknown, the system may ask the candidate.

Questions should:

* be specific;
* ask one coherent thing at a time;
* explain why the information matters when useful;
* avoid asking for information already known;
* avoid asking speculative questions merely to fill the database.

Good example:

```text
Ты работал с PostgreSQL только в учебных/личных проектах или использовал его в коммерческой работе?
```

Bad example:

```text
Расскажи вообще всё про свои базы данных.
```

---

## Contradictions

If two sources conflict:

1. do not silently choose one;
2. compare source authority;
3. preserve the contradiction if unresolved;
4. request clarification when the fact materially affects behavior;
5. do not use the disputed fact for a live HH write.

Explicit candidate correction should normally override weaker historical or inferred data.

---

## Updating knowledge

Do not create duplicate facts when an existing fact can be updated safely.

Prefer:

```text
existing fact
+
new evidence
+
updated provenance/status
```

over:

```text
fact A
fact A copy
fact A copy 2
```

Preserve useful history if the data model supports revisions.

---

## Employer-facing use

Before using Candidate Knowledge in:

* cover letters;
* employer chats;
* questionnaires;
* applications;
* interview answers;

verify that the fact is safe for employer-facing use.

Prefer confirmed facts.

Observed facts may be used only within the exact scope supported by evidence.

Inferred facts must not be presented as factual claims.

---

## Sensitive information

Do not expose unrelated private candidate information.

Only provide information relevant to the current employment context.

Never expose:

* secrets;
* credentials;
* private tokens;
* unrelated personal conversations;
* unrelated private documents.

---

## Vacancy matching integration

Candidate Knowledge may provide evidence for requirement evaluation.

Example:

```text
requirement: Django
candidate evidence: confirmed Django projects
result: met
```

If the knowledge base has no evidence:

```text
result: unknown
```

not:

```text
result: missing
```

Absence of evidence is not evidence of absence.

---

## AI behavior

AI may:

* normalize candidate statements;
* classify information;
* extract candidate facts;
* suggest possible duplicates;
* identify contradictions;
* generate clarification questions.

AI must not independently elevate uncertain data to confirmed status.

Deterministic application logic remains authoritative for safety-sensitive decisions.

---

## Changes to the data model

Before modifying Candidate Knowledge schemas:

1. inspect all readers and writers;
2. inspect persistence format;
3. inspect migrations/versioning;
4. inspect AI prompt consumers;
5. inspect vacancy matching;
6. inspect cover-letter generation;
7. inspect employer-chat generation;
8. inspect tests.

Prefer backward-compatible changes.

If persistent data exists, do not change serialization formats without migration or compatibility handling.

---

## Testing

Add tests for relevant behavior.

Important cases include:

* confirmed facts survive persistence;
* unknown remains unknown;
* conflicting facts do not silently overwrite each other;
* inferred facts do not become confirmed automatically;
* duplicate acquisition does not create uncontrolled duplication;
* candidate correction overrides weaker information;
* employer-facing generation does not invent unsupported claims.

Tests must be deterministic.

---

## Completion checklist

Before declaring a Candidate Knowledge task complete, verify:

* no candidate fact was invented;
* provenance is preserved where relevant;
* unknown state remains representable;
* existing persistent data remains readable;
* contradictions are handled safely;
* employer-facing output only uses supported claims;
* relevant tests pass;
* global AGENTS.md safety rules remain satisfied.
