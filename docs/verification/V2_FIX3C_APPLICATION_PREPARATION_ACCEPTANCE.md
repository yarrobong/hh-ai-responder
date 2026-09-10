# Executive summary

V2.Fix-3c: **BLOCKED**

The test-only acceptance harness is implemented and the focused structural
no-write test passes. The required single live invocation was executed once,
but stopped in the harness configuration guard before database access because
the existing configured model is `ministral-8b-latest` and the guard only
accepts model names containing the literal substring `mistral`.

No HH mutation was performed.

# Why a harness was needed

Production MATCH result: prior V2 acceptance evidence recorded `MATCH = 0`
after real Mistral analysis.

Reason preparation was previously not reached: the unchanged production
matching policy rejected every vacancy before the application-preparation
components were reached. This harness selects one real PostgreSQL vacancy and
records the production policy result separately. Its test-only policy adapter
may bypass only an initial `REJECT` decision for that selected vacancy; early
deterministic filters, structured applicability reconciliation, unknown
handling, candidate safety, and preparation validation remain active.

# Production preparation path

`HHAIResponder.ApplyVacancies` performs the automatic attempt gate and
deterministic vacancy gates, then calls `prepareApplication`.

`prepareApplication` uses the existing `applicationprocessing.Service` with
the production compatibility adapters:

1. `rootApplicationReader` / the read-only vacancy source supplies the
   description, applicability, and (when applicable) test snapshot.
2. `rootApplicationCandidate` resolves context through the canonical Candidate
   resolver.
3. `rootApplicationAnalyzer` invokes the typed vacancy-analysis service.
4. `rootApplicationPolicy` owns deterministic MATCH/REJECT/REVIEW decisions and
   applicability reconciliation.
5. `rootApplicationSemanticHints` uses the PostgreSQL semantic retriever and
   rebuilds safe context from the canonical Candidate.
6. `rootApplicationCoverLetter` invokes the typed cover-letter service when
   the real vacancy requires a letter.
7. `rootApplicationTestAnswer` invokes the typed test-answer service when a
   real test snapshot is present.

# Pre-mutation boundary

Preparation ends when `applicationprocessing.Service.Prepare` returns an
in-memory `PreparedApplication` (or a typed safe outcome such as
`NEEDS_CANDIDATE_INPUT`). It does not reserve an attempt, persist an
application, run mutation-adjacent write preflight, or call HH transport.

The production mutation boundary starts afterward in
`HHAIResponder.submitPreparedApplication`, which constructs the submission
service. Its executor reserves an automatic application attempt before the
HH application writer and local application projection are reachable.

# Harness implementation

Type: **test-only opt-in integration harness**

File: `internal/runtime/application_preparation_acceptance_test.go`

Production behavior changed: **NO**

The harness uses the existing `applicationprocessing.Service` and production
analyzer, cover-letter, test-answer, semantic-hint, candidate, and policy
adapters. It does not add a CLI flag or a runtime vacancy bypass.

Default behavior is `SKIP`. Run only the focused test with:

```text
V2_APPLICATION_PREPARATION_ACCEPTANCE=1 \
STORAGE_BACKEND=postgres \
DATABASE_URL=<configured-url> \
HH_CANDIDATE_ID=candidate-local \
HH_DRY_RUN=true \
HH_WRITE_ENABLED=false \
HH_AI_BASE_URL=<mistral-endpoint> \
HH_AI_MODEL=<mistral-model> \
HH_AI_API_KEY=<configured-secret> \
EMBEDDING_PROVIDER=openai-compatible \
EMBEDDING_BASE_URL=<embedding-endpoint> \
EMBEDDING_API_KEY=<configured-secret> \
EMBEDDING_MODEL=mistral-embed \
EMBEDDING_DIMENSIONS=1024 \
go test ./internal/runtime -run '^TestV2ApplicationPreparationAcceptance$' -count=1 -v
```

`V2_ACCEPTANCE_VACANCY_ID=<id>` may select an explicit real PostgreSQL
vacancy. Without it, the harness chooses a complete, unarchived,
unresponded PostgreSQL vacancy and prefers one that naturally exercises the
cover-letter path without a test.

# Real acceptance inputs

Database: `hh_ai_responder_s3` (the harness verifies `current_database()`)

Candidate: `candidate-local`, loaded through
`NewPostgresCandidateRepositoryForID`

VacancyID: **not selected; the single invocation stopped before database access**

Vacancy production policy result: **not evaluated in this invocation**

Harness preparation invoked despite upstream result: **not executed**

# PostgreSQL Candidate

Harness check: **implemented; live evidence unavailable**

The harness loads the canonical Candidate from PostgreSQL and compares a
canonical fingerprint before and after preparation. It does not read legacy
Candidate/Career JSON and does not add vacancy-specific facts.

# Semantic context

Provider: `openai-compatible`

Model: `mistral-embed`

Dimensions: `1024`

Documents/results: the live harness requires 3 stored documents and records
the actual retrieval result count when the applicable cover-letter path calls
the normal semantic retriever.

Space: the active configured embedding space is checked against all 3 stored
documents; no reindex is performed.

# Real Mistral

**BLOCKED — not invoked.** The existing `.env` model is
`ministral-8b-latest`, but `validateAcceptanceConfig` rejects it before the
production completion adapter is constructed because it checks only for the
literal substring `mistral`.

# Cover letter

**N/A — preparation was not reached.** No vacancy was selected.

# Application questions

**N/A in the current preparation model.** The current typed
`applicationprocessing` contract has no separate stored application-question
source. No synthetic employer questions are created.

# Test answers

**N/A — preparation was not reached.** No vacancy was selected.

# Final preparation result

**Not obtained — single live invocation blocked by the harness model-name
validation described above.**

# Candidate fact safety

The existing canonical Candidate resolver, safe projection, semantic-context
rebuild, typed vacancy analysis, and cover-letter validation remain the safety
authorities. The harness does not add a factuality engine, treat semantic
similarity as truth, or persist generated drafts. Unknown Candidate facts
remain unknown and can produce the existing typed `NEEDS_CANDIDATE_INPUT`
outcome.

# Write isolation

Real HH writer supplied: **NO**

Deny-writer calls: **N/A** — the preparation service has no writer or
reservation dependency in its composition. The focused test proves the
preparation composition completes without a write-shaped capability.

Attempt rows before/after: **`0 → 0` confirmed by sanitized post-run read; the
harness stopped before its own in-test count check**

HH mutations: **0**

If a test snapshot requires HH reads, the harness supplies a client whose
transport rejects every non-GET/HEAD request and marks the HH requester
read-only.

# PostgreSQL / semantic integrity

The live harness verifies, before and after preparation:

- Candidate count is 1 and the canonical Candidate fingerprint is unchanged;
- semantic documents remain 3, 1024-dimensional, and in the active SpaceID;
- application count is unchanged;
- automatic application attempts remain unchanged (required baseline: 0);
- legacy auto-chat attempts remain unchanged (required baseline: 0).

No PostgreSQL write is issued by the harness.

# Fix regressions

Fix-1: **PASS by existing regression coverage; live stage run pending**

Fix-2: **PASS by existing regression coverage; live stage run pending**

Fix-3a: **PASS by existing regression coverage; live stage run pending**

Fix-3b: **PASS by existing regression coverage; live stage run pending**

# Verification

Pre-change `go test -count=1 ./...`: **PASS**

Focused harness/no-write tests: **PASS**

Full post-change `go test -count=1 ./...`: **PASS**

Post-change `go test -race ./...`: **PASS**

Post-change `go vet ./...`: **PASS**

Post-change `go build ./...`: **PASS**

Post-change `go build ./cmd/hh-ai-responder`: **PASS**

Post-change `gofmt -l .`: **PASS**

Post-change `git diff --check`: **PASS**

Post-change Node checks for `web/app.js` and `internal/runtime/web/app.js`:
**PASS**

Canonical live acceptance: **BLOCKED — one invocation executed; stopped in
harness configuration validation before database access**

Docker: **NOT APPLICABLE**

LIVE HH WRITES: **0**

# Live acceptance execution

Environment source: existing repository-local `.env` loaded through the
project's `internal/config.Load` mechanism. Non-secret acceptance overrides
were supplied for the opt-in flag, PostgreSQL backend, candidate ID, dry-run
and HH-write safety flags, and the required embedding contract. `DATABASE_URL`,
`HH_AI_BASE_URL`, `HH_AI_API_KEY`, and `HH_AI_MODEL` were taken from `.env`;
embedding endpoint and credential used the accepted HH AI fallback.

Database: `hh_ai_responder_s3` (sanitized read-only pre/post checks)

Candidate: `candidate-local`

VacancyID: not selected; the harness stopped in `validateAcceptanceConfig`
before PostgreSQL access.

Production policy: not evaluated

Preparation invoked: **NO**

Real Mistral: **FAIL — not invoked; the harness rejected the existing
`ministral-8b-latest` model because its guard requires the literal substring
`mistral`**

Semantic: **FAIL — not invoked**

Cover letter: **N/A — preparation not reached**

Test answer: **N/A — preparation not reached**

Preparation result: not obtained; narrow blocker is the harness model-name
validation at `internal/runtime/application_preparation_acceptance_test.go:474`.

Candidate safety: **PASS — no candidate facts were generated, promoted, or
mutated**

Writer supplied: **NO** (the preparation composition was not entered; the
harness composition supplies no HH writer)

Attempt rows: `0 → 0`

Applications: `98 → 98`

Semantic documents: `3 → 3`

HH mutations: `0`

# Final decision

V2.Fix-3c: **BLOCKED**

Exact blocker: the single live acceptance invocation used the existing
configured Mistral-family model `ministral-8b-latest`, but the harness
configuration guard only recognizes names containing `mistral`; execution
stopped before PostgreSQL access, vacancy selection, real Mistral invocation,
semantic retrieval, or application preparation. No rerun was performed.

V2.Close: not started.
