# RESET-6A Education Matching Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Fix confirmed education false negatives at the matching boundary without mutating candidate data, changing RESET-6 router scoring/margins, or enabling HH writes.

**Architecture:** Keep PostgreSQL and canonical candidate storage unchanged. Add pure education-level normalization and conservative alternative parsing inside the canonical vacancy-analysis use case, and make the RESET-6 router explicitly leave education-like required skills unknown when it has no trusted candidate-education boundary. The runtime regression will exercise a loaded canonical candidate through the employer-safe projection into CandidateFacts.

**Tech Stack:** Go, standard library string processing, existing vacancyanalysis, careeragent, and runtime unit-test fixtures.

**Spec:** User request pasted in /Users/Yaroslav/.codex/attachments/d5e85582-0fd4-4a8c-8d47-c289837298bc/pasted-text.txt.

## Global Constraints

- Do NOT mutate Candidate data.
- Do NOT change router scoring/margins.
- Do NOT perform any HH write.
- Unknown/unrecognized trusted education levels must produce UNKNOWN, not MISSING.
- Education specialization requirements remain conservative and return UNKNOWN unless existing trusted logic proves them.
- REVIEW_REQUIRED must never automatically result in an application.
- Preserve dry-run protection and existing CLI/config behavior.
- Do not change RESET-6 margin thresholds, AI minimum score, Stage29.6 precedence, Search Planner, API writer, or application approval gates.

## Review Focus

- Human-readable secondary-professional levels must normalize to the canonical level: Task 1 matrix.
- Slash/comma/or alternatives must be OR semantics, not one literal string: Task 1 matrix.
- Unknown trusted levels must not become false negatives: Task 1 unknown-level test.
- Specialized education requirements must not become broad level matches: Task 1 specialization test.
- Router skill tokens must not prove education: Task 2 router regression.

---

### Task 0: Trace the real pre-AI block before changing production code

**Files:**
- Inspect: `internal/runtime/career_agent_pilot.go`, `internal/runtime/application_processing.go`, `internal/runtime/application_processing_compat.go`, `internal/runtime/vacancy_match.go`, `internal/runtime/career_agent_runtime.go`, `internal/careeragent/model.go`, and related tests.
- Test: Add a focused runtime regression in `internal/runtime/career_agent_pilot_test.go` or the closest existing pilot/runtime test file after the exact producer is identified.

- [x] **Step 1: Trace the exact producer of the real output**

Follow `career-agent pilot --vacancy` through vacancy preflight, router selection, resume activation, canonical candidate projection, vacancy analysis, local deterministic derivation, and pilot eligibility gates. Locate the exact function that emits or supplies `hard requirements not met`, and distinguish it from router `RequirementState` unknown values. On this baseline the trace must explicitly verify whether the observed text is pre-AI or post-AI; the current code and supplied report indicate the exact string is produced by `vacancyDecisionWithReason` after AI assessment, while `educationRequirementMatches` supplies the false `MISSING` status.

- [x] **Step 2: Write a failing real-shaped runtime regression**

Use a selected resume titled `Технический специалист`, a router score shaped like the real path, a trusted canonical education value `среднее профессиональное`, and requirement `СПО/высшее/магистратура`. Use deterministic/mocked external dependencies only. Assert the baseline path reaches the AI/analyzer boundary, then stops at the local hard-requirement decision because the education false negative is `MISSING`; the test must not perform any HH write. If a separate pre-AI fixture is used to model the reported operator text, keep the trace assertion so it cannot be mistaken for the actual producer on this baseline.

- [x] **Step 3: Fix only the actual deterministic education boundary**

After the producer is proven, change that boundary so trusted education is evaluated by the canonical vacancy-analysis policy rather than raw-level comparison or resume skill-token overlap. Preserve all unrelated router scores, margins, AI thresholds, approval gates, and write protections. If the trace shows `requirementStates` is not the blocker, do not broaden the router change beyond the separate non-terminal education guard required by Task 2.

- [x] **Step 4: Verify the regression reaches vacancy analysis**

Run the focused runtime regression and assert that after the fix the analyzer is reached, education no longer causes the local hard-requirement rejection, and execution proceeds to the next read-only applicability boundary. The regression must still prove that no HH write is made.

---

### Task 1: Normalize and match education requirements conservatively

**Files:**
- Modify: internal/usecase/vacancyanalysis/validation.go near educationRequirementMatches.
- Test: internal/runtime/candidate_facts_test.go for the existing compatibility-facing education matrix.
- Test: internal/runtime/candidate_facts_test.go for the canonical PostgreSQL-shaped projection regression.

**Interfaces:**
- Consumes: vacancyanalysis.CandidateFacts education fields and HardRequirementCandidate.
- Produces: DeriveHardRequirementStatus and ValidateHardRequirements results using canonical levels higher, incomplete_higher, secondary, and secondary_professional.

- [x] Step 1: Write the failing education matrix tests.

Task 0 must be complete before these tests are added so the matrix covers the proven failing boundary.

Add table-driven cases to TestEducationRequirementsUseTrustedLevel covering:
  - Russian secondary professional plus СПО/высшее/магистратура => MET.
  - canonical secondary_professional plus СПО/высшее/магистратура => MET.
  - Russian secondary professional plus высшее образование => MISSING.
  - higher plus СПО/высшее/магистратура => MET.
  - secondary plus СПО/высшее/магистратура => MISSING.

Add a separate case with EducationKnown true and an unrecognized trusted level such as doctoral and assert UNKNOWN for СПО/высшее/магистратура. Keep the existing specialization test and add высшее техническое образование if needed to prove it remains UNKNOWN.

- [x] Step 2: Run the focused tests and verify the expected red failure.

Run:
  go test ./internal/runtime -run 'TestEducationRequirementsUseTrustedLevel|TestEducationSpecializationWithoutStructuredProfileIsUnknown' -count=1

Expected: the new human-readable secondary-professional and alternative cases fail because the current matcher does not recognize СПО and compares the raw Russian candidate level against canonical enum spellings; existing cases remain green.

- [x] Step 3: Implement the smallest pure matcher change.

Replace the substring-only educationRequirementMatches logic with three pure helpers in internal/usecase/vacancyanalysis/validation.go:

  normalizeEducationLevel(value string) (string, bool)
  educationRequirementAlternatives(requirement string) ([]string, bool)
  educationRequirementMatches(candidateLevel, requirement string) (bool, bool)

normalizeEducationLevel recognizes higher, incomplete_higher, secondary, secondary_professional, Russian aliases including СПО, среднее профессиональное образование, and среднее специальное образование, plus existing higher/incomplete-higher forms. It returns false for blank or unrecognized values.

educationRequirementAlternatives splits explicit alternatives on slash, commas, and the word или; maps СПО/среднее профессиональное/среднее специальное to secondary_professional; maps высшее/бакалавриат/специалитет/магистратура to higher; and maps explicit неполное/незаконченное высшее to incomplete_higher. It returns unsupported when blank, when specialization markers are present, or when no level alternative is recognized.

educationRequirementMatches first normalizes the candidate level. If the candidate is unrecognized, it returns false,false so the caller derives UNKNOWN. Otherwise it returns true only when the normalized candidate equals one parsed alternative. A completed higher candidate must not be inferred from incomplete_higher, and specialization markers must continue to return unsupported.

Do not read institution, specialty, age, title, skills, or free-text experience to infer a level. Do not modify candidate structs or persistence.

- [x] Step 4: Run focused education tests and verify green.

Run:
  gofmt -w internal/usecase/vacancyanalysis/validation.go internal/runtime/candidate_facts_test.go
  go test ./internal/runtime -run 'TestEducationRequirementsUseTrustedLevel|TestEducationSpecializationWithoutStructuredProfileIsUnknown' -count=1

Expected: all education matrix cases pass, including MET for Russian persisted среднее профессиональное plus СПО/высшее/магистратура, MISSING for incompatible known levels, and UNKNOWN for unrecognized trusted levels and specialization requirements.

- [x] Step 5: Add the canonical PostgreSQL-shaped regression.

Construct an in-memory Candidate with one CanonicalCandidateEducation row containing:
  Level: среднее профессиональное
  Institution: Екатеринбургский монтажный колледж
  Specialty: Информационные системы и программирование
  Metadata: canonicalTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed, at, user confirmed education)

Pass it through CanonicalEmployerSafeProjection, build the corresponding CandidateFacts or legacy projection without changing the source candidate, and assert that DeriveHardRequirementStatus for category education and requirement СПО/высшее/магистратура returns MET with Candidate education evidence. Assert that the source candidate still has the exact persisted level, institution, specialty, and metadata.

- [x] Step 6: Run the canonical regression.

Run:
  go test ./internal/runtime -run 'TestCanonicalPostgresEducationProjectionMatchesAlternativeRequirement' -count=1

Expected: PASS without a database connection, PostgreSQL write, or candidate mutation.

### Task 2: Keep RESET-6 router education-like requirements non-terminal

**Files:**
- Modify: internal/careeragent/model.go in requirementStates.
- Test: internal/careeragent/router_reset6_test.go.

**Interfaces:**
- Consumes: VacancyInput.RequiredSkills and the selected ResumeProfile.
- Produces: RouteDecision.HardRequirements with education-like requirements left unknown unless a future trusted education boundary is explicitly available; router score and margin calculations remain unchanged.

- [x] Step 1: Write the failing router regression.

Add a test using a selected technical-support resume whose Skills deliberately include СПО and Linux, then route a vacancy with RequiredSkills containing СПО/высшее/магистратура and Linux. Assert the education requirement is unknown, never met, and the Linux requirement is met when existing skill overlap supports it.

- [x] Step 2: Run the focused router test and verify the expected red failure.

Run:
  go test ./internal/careeragent -run 'TestRouteResumeKeepsEducationRequirementUnknownInsteadOfSkillMatch' -count=1

Expected: FAIL before the production change because generic token overlap can treat a resume skill token as evidence for an education-like requirement.

- [x] Step 3: Add an explicit education-like requirement guard.

In requirementStates, before token overlap against profile.Skills, classify a requirement as education-like when it contains recognized education markers: СПО, высшее, среднее профессиональное or специальное, бакалавриат, специалитет, магистратура, неполное or незаконченное высшее. Set its state to unknown and skip resume skill-token matching. Leave non-education requirements on the existing path. Do not add education data to ResumeProfile, modify scoreResume, or change router score/margin thresholds.

- [x] Step 4: Run focused router tests and verify green.

Run:
  gofmt -w internal/careeragent/model.go internal/careeragent/router_reset6_test.go
  go test ./internal/careeragent -run 'TestRouteResumeKeepsEducationRequirementUnknownInsteadOfSkillMatch|TestRouteResumePrefersHardSkillAndReviewsCloseChoice' -count=1

Expected: PASS; education remains explicitly unknown/non-terminal, technical skill handling remains unchanged, and existing router calibration stays green.

### Task 3: Full verification and safe dry-run replay

**Files:**
- No additional production files; inspect the final diff and generated artifacts only.

- [x] Step 1: Run the required Go verification suite.

Run:
  gofmt -w .
  go test ./...
  go test -race ./...
  go vet ./...
  go build ./...
  git diff --check

Expected: all commands pass. If an unrelated pre-existing failure appears, report its package and exact command output rather than claiming completion.

- [x] Step 2: Inspect the diff for safety invariants.

Run:
  git diff -- internal/usecase/vacancyanalysis/validation.go internal/runtime/candidate_facts_test.go internal/careeragent/model.go internal/careeragent/router_reset6_test.go
  git status --short

Confirm the diff contains no PostgreSQL writes, Candidate mutation, router score/margin changes, HH writer calls, secret files, or candidate-data persistence changes.

- [x] Step 3: Run only the requested GET/dry-run pilot replay if its fixture and dependencies are available.

Run:
  HH_TRANSPORT=browser HH_DRY_RUN=true HH_WRITE_ENABLED=false go run ./cmd/hh-ai-responder --career-agent-result /tmp/pilot-137532422-reset6a.json career-agent pilot --vacancy 137532422

Observed: no HH write was attempted; the false pre-AI block caused by СПО/высшее/магистратура did not occur; the configured Mistral provider returned HTTP 429 during the AI boundary, so the live output had unknown AI score/recommendation. The live run did not require an education item in AI hard_requirements. Deterministic unit tests, including the canonical PostgreSQL-shaped regression from Task 1, prove trusted persisted среднее профессиональное plus СПО/высшее/магистратура is MET.
