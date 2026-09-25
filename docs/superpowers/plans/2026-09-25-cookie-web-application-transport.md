# Cookie/Web Application Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Add a strict, persistent HH cookie/web transport that supports GET authentication, fresh application preflight, controlled live application, dry-run, and reconciliation without requiring OAuth or creating a second application state machine.

**Architecture:** Introduce `internal/hhwebsession` as the shared cookie/session primitive. Give the read adapter only a GET-capable client and give the dedicated `CookieWebVacancyResponseWriter` the controlled POST capability. Refactor the existing API-specific controlled application composition into a transport-neutral service while reusing `applicationsubmission`, `applicationattempt`, `applicationreconciliation`, and `hhwritegateway`.

**Tech Stack:** Go standard library HTTP/cookie APIs, existing `httptest` fixtures, existing HTML/state parsers, JSON/PostgreSQL workflow repositories, current application use cases, and current CLI/configuration.

**Spec:** `docs/superpowers/specs/2026-09-25-cookie-web-application-transport-design.md` at commit `03a9cc1`.

## Global Constraints

- Work only in `/Users/Yaroslav/.codex/worktrees/cookie-web-application-transport/hh-ai-responder` on branch `codex/cookie-web-application-transport`.
- Do not modify the original dirty checkout, merge, or push.
- Do not issue a production HH POST, message, resume mutation, or job-search status mutation.
- Keep `PreparationID/PreparationHash`, `ContentHash`, explicit human approval, and one-time nonce bindings exact.
- A fresh GET-only preflight is the last provider read before a possible write path.
- `HHWriteGateway` remains the only HH mutation boundary.
- Dry-run means zero POST, zero nonce consumption, and zero application attempts.
- Live order is approval validation → fresh preflight → one-time approval nonce consumption → durable application attempt reservation → `HHWriteGateway` → writer → reconciliation.
- Batch processing is sequential; pre-send blocks may continue, but transport uncertainty, reconciliation uncertainty, or unhealthy cookie persistence stops the batch.
- OAuth API transport remains available and unchanged for `HH_TRANSPORT=api`; browser transport must not access OAuth configuration or token files.
- Unknown critical provider state blocks the operation and never triggers a blind retry.
- PostgreSQL remains canonical when configured; JSON remains compatible for existing local workflows.
- Reuse `applicationsubmission`, `applicationattempt`, `applicationreconciliation`, and `hhwritegateway`; do not introduce a parallel application state machine.

## Review Focus

- A `Set-Cookie` domain or redirect must never widen cookie scope or leak cookies to an external host. Pin with `TestPersistentJarRejectsForeignDomainAndExternalRedirect` in Task 1.
- A changed browser resume hash after approval must be stale, not silently remapped. Pin with `TestPreparationApprovalRejectsChangedBrowserResumeHash` in Task 3.
- Vacancy-wide duplicate absence must not be presented as resume-specific evidence when HH does not expose resume identity. Pin with `TestWebPreflightDoesNotInferResumeSpecificDuplicateAbsence` in Task 4.
- A cookie persistence error before versus after POST has different transport semantics. Pin with `TestWriterPersistenceFailureBeforePost` and `TestWriterPersistenceFailureAfterPost` in Task 5.
- A confirmed reconciliation after an unhealthy cookie persistence event must still stop the next batch item. Pin with `TestBatchStopsAfterConfirmedReconciliationWithUnhealthySession` in Task 8.

## File map

The implementation will use these focused units and existing boundaries:

- Create `internal/hhwebsession/netscape.go`, `internal/hhwebsession/jar.go`, `internal/hhwebsession/session.go`, and `internal/hhwebsession/redirect.go` for strict cookie parsing, persistent jar state, session clients, XSRF lookup, and redirect policy.
- Create `internal/hhwebsession/netscape_test.go`, `internal/hhwebsession/jar_test.go`, and `internal/hhwebsession/session_test.go` for the session contract.
- Create `internal/adapters/hh/web/read.go`, `internal/adapters/hh/web/resumes.go`, and `internal/adapters/hh/web/application_writer.go` for cookie-backed GET composition, resume mapping, and the one controlled application writer.
- Create `internal/adapters/hh/web/read_test.go`, `internal/adapters/hh/web/resumes_test.go`, and `internal/adapters/hh/web/application_writer_test.go` with `httptest` providers only.
- Modify `internal/adapters/hh/read/client.go` so its HTTP dependency is a narrow request executor while its own methods remain GET-only; update `client_test.go` accordingly.
- Modify runtime composition in `internal/runtime/runtime.go`, `hh_transport.go`, `browser_doctor_command.go`, `browser_session_command.go`, `hh_doctor.go`, `hh_read_validation.go`, and `hh_read_sync.go` to use the new session without leaving a second persistent jar implementation.
- Modify preparation/approval identity in `internal/careeragent/workflow.go`, `internal/runtime/career_workflow_compat.go`, `internal/runtime/application_preparation_compat.go`, `internal/runtime/hh_api_application.go`, `internal/runtime/hh_api_approval_export.go`, `internal/runtime/career_agent_pilot.go`, and their tests.
- Modify JSON/PostgreSQL workflow persistence in `internal/adapters/storage/json/career_workflow.go`, `internal/adapters/storage/postgres/career_workflow.go`, and mirrored migrations `migrations/000020_application_preparation_browser_resume_hash.{up,down}.sql` and `internal/runtime/migrations/000020_application_preparation_browser_resume_hash.{up,down}.sql`.
- Modify controlled orchestration in `internal/runtime/hh_api_application.go`, `internal/runtime/hh_api_batch.go`, `internal/runtime/hh_api_command.go`, and `internal/cli/parse.go` while retaining the current command names.
- Update `README.md` and `example.env` only for the final transport/configuration behavior.

---

### Task 1: Build `internal/hhwebsession`

**Files:**

- Create: `internal/hhwebsession/netscape.go`
- Create: `internal/hhwebsession/jar.go`
- Create: `internal/hhwebsession/session.go`
- Create: `internal/hhwebsession/redirect.go`
- Test: `internal/hhwebsession/netscape_test.go`
- Test: `internal/hhwebsession/jar_test.go`
- Test: `internal/hhwebsession/session_test.go`

**Interfaces:**

- Produce `type Session` with `New(path string, options Options) (*Session, error)`, `ReadClient() RequestDoer`, `WriteClient() RequestDoer`, `XSRFToken(base *url.URL) (string, error)`, `SafeMetadata(now time.Time) Metadata`, and `PersistenceError() error`.
- Produce `type RequestDoer interface { Do(*http.Request) (*http.Response, error) }` so the read adapter receives a client that is not typed as a general `*http.Client`.
- `ReadClient` permits only GET/HEAD requests and follows only same-HH redirects. `WriteClient` permits the controlled writer's request method and returns the original 3xx response without following it.
- The session owns a `http.CookieJar` implementation that preserves host-only/include-subdomains, domain, path, secure, expiry, session, and HttpOnly metadata.

**Steps:**

- [ ] Add failing parser tests for valid Netscape rows, host-only rows, `TRUE` include-subdomains rows, `#HttpOnly_` rows, empty values, malformed field counts, malformed expiry, empty identity, expired rows, non-HH rows, and domain widening.
- [ ] Run `go test ./internal/hhwebsession -run 'TestNetscape' -count=1`; expect failures because the package does not exist.
- [ ] Implement strict parsing without logging or returning cookie values in diagnostic metadata. Treat `#HttpOnly_` as a prefix on the domain column, not as a comment.
- [ ] Add failing jar tests for bare-host versus subdomain matching, secure cookies over HTTP, path matching, expiry pruning without file deletion, session-cookie round trip, `Set-Cookie` replacement/deletion, unrelated domain rejection, and concurrent `Cookies`/`SetCookies` calls.
- [ ] Implement the persistent jar using locked state plus standard cookie matching semantics. Persist only accepted HH cookies. Preserve the original host-only/include-subdomains bit when serializing Netscape rows.
- [ ] Add failing persistence tests that inspect atomic replacement, previous-file retention after write failure, file mode `0600`, and safe metadata containing names/domains/counts/expiry only.
- [ ] Implement temp-file write in the target directory, close/sync before rename, restrictive mode, and observable `PersistenceError` state. Never overwrite the last usable file after parser or server errors.
- [ ] Add failing redirect/XSRF tests for internal GET redirects, login/captcha classification, external-host redirect rejection, POST `ErrUseLastResponse`, exact `_xsrf` lookup, and no cookie forwarding to an external host.
- [ ] Implement separate read/write request executors sharing the same session jar and persistence state. Reject foreign `Set-Cookie` domains and classify persistence errors for callers.
- [ ] Run `go test ./internal/hhwebsession -count=1` and `go test -race ./internal/hhwebsession -count=1`; expect PASS.
- [ ] Commit the self-contained session boundary.

**Acceptance criteria:** Strict Netscape state loads only for allowed HH domains; `#HttpOnly_` and host-only semantics survive a save/load cycle; server cookie updates persist atomically with mode `0600`; concurrent access is race-free; read redirects are bounded to HH; write redirects are never followed; XSRF lookup never exposes its value; persistence errors are observable and never delete the last valid file.

**Safety invariant:** This package owns authentication state but has no application-specific behavior and cannot make an uncontrolled write. A pre-POST persistence error is distinguishable from a post-transport persistence error.

**Commit boundary:** `feat: add strict persistent HH web session`

---

### Task 2: Compose the GET-only web/read path and browser doctor

**Files:**

- Create: `internal/adapters/hh/web/read.go`
- Create: `internal/adapters/hh/web/read_test.go`
- Modify: `internal/adapters/hh/read/client.go`
- Modify: `internal/adapters/hh/read/client_test.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/hh_transport.go`
- Modify: `internal/runtime/browser_doctor_command.go`
- Modify: `internal/runtime/browser_doctor_test.go`
- Modify: `internal/runtime/browser_session_command.go`
- Modify: `internal/runtime/hh_doctor.go`
- Modify: `internal/runtime/hh_doctor_test.go`
- Modify: `internal/runtime/hh_read_validation.go`
- Modify: `internal/runtime/hh_read_sync.go`
- Modify: runtime tests currently constructing `MemoryPersistentJar`: `internal/runtime/application_preparation_acceptance_test.go`, `internal/runtime/hh_read_validation_test.go`, `internal/runtime/main_test.go`, `internal/runtime/performance_stage22_test.go`, and `internal/runtime/hh_write_gateway_test.go`.

**Interfaces:**

- `hh/read.Options.HTTPClient` becomes a `RequestDoer`; `hh/read.Client` continues to expose only its existing GET read methods.
- `hh/web.NewCookieWebReadClient(session *hhwebsession.Session, baseURL *url.URL, searchParams url.Values) (*hhread.Client, error)` returns the existing read model backed by `session.ReadClient()`.
- The runtime keeps `browsersession.Adapter` read-only for Playwright/profile operations. Cookie web reads use `hhwebsession`; no read adapter receives `WriteClient`.

**Steps:**

- [ ] Add a failing adapter test proving a cookie-backed read sends cookies, updates the session from `Set-Cookie`, and rejects a non-GET request through the read capability.
- [ ] Implement `internal/adapters/hh/web/read.go` by composing the existing `hh/read.Client`, `hhread` models, vacancy parsers, and session read executor; do not duplicate HTML parsing.
- [ ] Add failing browser-doctor tests for authenticated home/resume/vacancy GETs, login redirect, session-expired cookie state, captcha/challenge, missing XSRF when write capability is requested, external redirect, and safe output with no values.
- [ ] Replace the primary `browser-doctor` path with the cookie session and real GET probes. Keep the Playwright `browser-session` command for headed user interaction and cookie preparation.
- [ ] Migrate runtime construction from `MemoryPersistentJar` to `hhwebsession.Session` and the read executor. Keep `HHAIResponder`'s XSRF access as a session lookup, not a copied token.
- [ ] Replace `browser_session_command.go` and `hh_doctor.go` diagnostic jar construction with session-safe read clients. Do not persist diagnostic-only changes unless the command is explicitly using the session lifecycle.
- [ ] Remove the legacy runtime jar implementation after all callers are migrated. Update the listed fixtures to construct a session-backed test client rather than reaching into jar internals.
- [ ] Run `go test ./internal/adapters/hh/read ./internal/adapters/hh/web ./internal/runtime -run 'Test(BrowserDoctor|HHDoctor|SelectHHTransport|.*Read.*|.*Cookie.*)' -count=1` and then the full package tests.
- [ ] Commit the read composition and doctor migration.

**Acceptance criteria:** Existing HH read models/parsers still work; cookie-web reads need no OAuth; the browser/read side cannot issue a POST through its injected capability; browser-doctor reports `AUTH_OK`, `AUTH_REQUIRED`, `SESSION_EXPIRED`, or `CHALLENGE` from real GET evidence and never recommends OAuth for browser transport; no runtime duplicate persistent jar remains.

**Safety invariant:** All operations in this task are GET-only. Read helpers cannot hide a mutation and cannot receive the controlled writer client.

**Commit boundary:** `refactor: route HH web reads through cookie session`

---

### Task 3: Bind browser resume identity into preparation and approval

**Files:**

- Create: `migrations/000020_application_preparation_browser_resume_hash.up.sql`
- Create: `migrations/000020_application_preparation_browser_resume_hash.down.sql`
- Create: `internal/runtime/migrations/000020_application_preparation_browser_resume_hash.up.sql`
- Create: `internal/runtime/migrations/000020_application_preparation_browser_resume_hash.down.sql`
- Modify: `internal/careeragent/workflow.go`
- Modify: `internal/careeragent/workflow_test.go`
- Modify: `internal/runtime/career_workflow_compat.go`
- Modify: `internal/runtime/application_preparation_compat.go`
- Modify: `internal/runtime/hh_api_application.go`
- Modify: `internal/runtime/hh_api_approval_export.go`
- Modify: `internal/runtime/career_agent_pilot.go`
- Modify: `internal/adapters/storage/json/career_workflow.go`
- Modify: `internal/adapters/storage/postgres/career_workflow.go`
- Modify: `internal/adapters/storage/json/career_workflow_test.go`
- Modify: `internal/adapters/storage/postgres/career_workflow_test.go`
- Test: `internal/runtime/application_preparation_test.go`
- Test: `internal/runtime/resume_identity_test.go`
- Test: `internal/runtime/hh_api_approval_export_test.go`
- Test: `internal/runtime/hh_api_application_test.go`

**Interfaces:**

- Add `BrowserResumeHash string json:"browser_resume_hash,omitempty"` to `careeragent.ApplicationPreparation` and `APIApplicationApproval`.
- Add the same field to the `application_preparations` PostgreSQL projection; JSON storage keeps it through the existing typed struct.
- `careeragent.PreparationInputFingerprint` must include `BrowserResumeHash`.
- `validatePreparationApprovalBinding` must compare approval `BrowserResumeHash`, durable preparation `BrowserResumeHash`, and the approved logical/provider identity.
- Browser transport validation requires a non-empty exact hash. Existing API-only approval artifacts without this field remain valid only on `HH_TRANSPORT=api`; they must not be accepted by browser transport.

**Steps:**

- [ ] Add failing model tests for an invalid preparation with missing browser hash in a browser-bound preparation, changed browser hash changing `PreparationInputFingerprint`, and unchanged hash preserving deterministic fingerprint.
- [ ] Add `BrowserResumeHash` to preparation construction from `selectedResume.Hash`; retain current internal and provider identity fields separately. Ensure the pilot artifact's existing `SelectedResumeHash` is copied into the approval field rather than re-derived after approval.
- [ ] Update JSON validation and PostgreSQL insert/select/scan/upsert statements plus both mirrored migrations. Verify old rows decode safely with an empty field and are rejected only when the browser path requires the new binding.
- [ ] Add failing approval tests for missing hash, approval/preparation hash mismatch, changed current mapping, browser hash equal to a different logical resume, and legacy API artifact compatibility.
- [ ] Extend approval export and manual review conversion to preserve the exact browser hash. Keep cover-letter and content hash validation unchanged.
- [ ] Add a fresh identity validator that returns `BLOCKED_STALE` when the current mapping differs and `APPROVED_RESUME_NOT_AVAILABLE_IN_BROWSER_SESSION` when no exact mapping exists. It must never search by title or choose another resume.
- [ ] Run focused preparation/approval/storage tests and verify migration contract tests pass.
- [ ] Commit the immutable browser resume binding.

**Acceptance criteria:** Browser `resume_hash` is fixed before human approval, covered by preparation hash, present in new approval artifacts, persisted in JSON/PostgreSQL, checked again immediately before transport, and never replaced by title/provider heuristics. API-only legacy artifacts remain compatible on API transport but cannot silently enter web transport.

**Safety invariant:** A current browser identity change invalidates the approved preparation and forces new preparation plus human approval before any nonce or attempt can be consumed.

**Commit boundary:** `feat: bind browser resume hash to application approval`

---

### Task 4: Implement cookie-web resume mapping and application preflight

**Files:**

- Create: `internal/adapters/hh/web/resumes.go`
- Create: `internal/adapters/hh/web/resumes_test.go`
- Create: `internal/runtime/cookie_web_application_preflight.go`
- Create: `internal/runtime/cookie_web_application_preflight_test.go`
- Modify: `internal/runtime/vacancy_preflight.go`
- Modify: `internal/runtime/vacancy_preflight_test.go`
- Modify: `internal/runtime/application_preparation_compat.go`
- Modify: `internal/runtime/hh_api_application.go`

**Interfaces:**

- Produce `type BrowserResume` with `BrowserHash`, `HHID`, `Title`, `InternalID`, and `ProviderID` fields; values are evidence from the authenticated resume page/local trusted data.
- Produce `type ResumeMappingReader interface { ReadBrowserResumes(context.Context) ([]BrowserResume, error) }`.
- Produce `type CookieWebApplicationPreflight struct { VacancyID int; ResponseURL string; Resume BrowserResume; VacancyPreflight VacancyPreflight; Authenticated bool; StandardResponsePathKnown bool; PersistenceHealthy bool }`.
- Produce `func (p CookieWebApplicationPreflight) ValidateForSend(letter string) error` that requires known active state, vacancy-wide duplicate absence, can-apply, exact approved hash mapping, known test absence, known letter requirement, standard response path, and healthy pre-send session persistence.

**Steps:**

- [ ] Add fixture-driven resume-page tests for exact browser hash/HH ID/title extraction, multiple resumes, missing hash, duplicate hashes, malformed embedded state, and no title-based fallback.
- [ ] Implement `/applicant/my_resumes` parsing by reusing the existing embedded `redirectConfig`/`applicantResumes` shape and mapping only provider evidence plus locally supplied canonical identity.
- [ ] Add failing preflight tests for active vacancy, archived vacancy, vacancy-wide existing response, no response, explicit resume-bound duplicate evidence, absent resume identity in negotiation state, can-apply unknown/false, selected resume missing, test present/unknown, required/optional/unknown letter, non-standard response path, login/captcha, and persistence-unhealthy states.
- [ ] Implement cookie-web preflight as GET-only reads of `/vacancy/<id>`, `/applicant/vacancy_response?...`, and vacancy-scoped negotiations as required. Reuse `parseVacancyPreflight`, `parseVacancyActiveState`, and existing evidence helpers instead of creating a second parser model.
- [ ] Treat duplicate absence as vacancy-wide unless provider evidence explicitly carries the approved resume identity. Never convert missing resume identity into resume-specific negative evidence.
- [ ] Make the preflight validator reject all unknown critical fields with a deterministic pre-send block.
- [ ] Add a test proving this preflight is the last provider read before the write path by recording request order in an `httptest.Server`.
- [ ] Run `go test ./internal/runtime -run 'TestCookieWeb.*Preflight|Test.*Resume.*Mapping|Test.*VacancyPreflight' -count=1`.
- [ ] Commit the web mapping and preflight.

**Acceptance criteria:** A browser preflight proves only facts actually exposed by HH; vacancy-wide duplicate absence is not mislabeled resume-specific; exact approved resume hash is present in current web state; unknown critical state blocks; no provider read is performed after this preflight before nonce/write orchestration begins.

**Safety invariant:** Fresh preflight is authoritative for current vacancy state and cannot authorize a write when identity, duplicate, test, letter, path, authentication, or persistence state is unknown.

**Commit boundary:** `feat: add cookie web resume mapping and preflight`

---

### Task 5: Add `CookieWebVacancyResponseWriter`

**Files:**

- Create: `internal/adapters/hh/web/application_writer.go`
- Create: `internal/adapters/hh/web/application_writer_test.go`
- Modify: `internal/ports/hhwrite/hh_write.go` only if a provider-neutral result field is required; preserve existing public semantics.
- Modify: `internal/runtime/hh_write_adapter_compat.go` only for composition/error mapping, not for a new mutation path.

**Interfaces:**

- Produce `type CookieWebVacancyResponseWriter struct { ... }` implementing `hhwrite.VacancyResponseWriter`.
- Constructor: `NewCookieWebVacancyResponseWriter(baseURL *url.URL, session *hhwebsession.Session, userAgent string) (*CookieWebVacancyResponseWriter, error)`.
- `SubmitVacancyResponse(context.Context, hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error)` accepts only vacancy ID, exact `ResumeHash`, exact letter, exact referer, and `IgnorePostponed=true`; a non-nil `Test` returns `OutcomeNotSent` with request-validation evidence.

**Steps:**

- [ ] Add a pure request-builder test that asserts exact form keys/values for vacancy ID, approved hash, approved letter, `ignore_postponed=true`, XSRF body, and exact vacancy referer. Assert no test keys are emitted.
- [ ] Implement a private request builder returning the request and a sanitized preview; use `url.Values` and never log the encoded body.
- [ ] Add failing `httptest` tests for cookies supplied by the session jar, `Accept: application/json`, `Content-Type`, `X-Requested-With`, `X-Xsrftoken`, stable user agent, exact referer, and no cookie/XSRF value in error or audit output.
- [ ] Implement the one POST to `/applicant/vacancy_response/popup` through `WriteClient`; do not call a generic `http.Client` outside the session.
- [ ] Add failing classification tests for recognized success, explicit already-applied, validation/business rejection, unauthorized, forbidden, rate limit, malformed/unknown 2xx, 3xx, 5xx, timeout after request start, and connection failure after request start.
- [ ] Implement bounded response reading and sanitized evidence using the existing provider-neutral `hhwrite.TransportError` model. Map unknown provider bodies and unexpected redirects to ambiguous delivery; never retry.
- [ ] Add persistence timing tests: an unhealthy session before request creation returns `BLOCKED_PRE_SEND`/`transport_attempted=false`; a `Set-Cookie` persistence failure after request start returns ambiguous delivery with `transport_attempted=true` and reconciliation required.
- [ ] Run `go test ./internal/adapters/hh/web -run 'TestCookieWebVacancyResponseWriter' -count=1` and race the package.
- [ ] Commit the dedicated writer.

**Acceptance criteria:** The writer sends exactly one approved form request, never sends tests, never follows redirects, never posts to another host, never exposes secrets, classifies uncertain delivery conservatively, and makes persistence timing visible to the gateway/orchestrator.

**Safety invariant:** The writer is reachable only through `HHWriteGateway`; it cannot create approval, consume nonce, reserve attempts, retry, or reconcile by itself.

**Commit boundary:** `feat: add cookie web vacancy response writer`

---

### Task 6: Refactor controlled application orchestration to transport-neutral composition

**Files:**

- Modify: `internal/runtime/hh_api_application.go`
- Modify: `internal/runtime/hh_api_application_test.go`
- Modify: `internal/runtime/hh_api_command.go`
- Modify: `internal/runtime/hh_transport.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/application_submission_compat.go`
- Create: `internal/runtime/controlled_application_transport.go`
- Create: `internal/runtime/controlled_application_transport_test.go`

**Interfaces:**

- Introduce private `controlledApplicationTransport` with `Prepare(context.Context, APIApplicationApproval) (controlledApplicationContext, error)`, `Writer() hhwrite.VacancyResponseWriter`, `EvidenceReader() applicationreconciliation.EvidenceReader`, and `PersistenceHealth() error`.
- `controlledApplicationContext` carries the exact application `ResumeID`, `VacancyPreflight`, `ResponseURL`, and transport metadata needed by existing `applicationsubmission`.
- Rename the implementation to `controlledApplicationService`; keep a source-compatible alias/wrapper for current tests only where that avoids a CLI/API break. `runHHAPIApply` and `runHHAPIApplyBatch` continue to call the same service interface.

**Steps:**

- [ ] Add failing service tests using a fake transport proving API and browser adapters share approval validation, preflight ordering, dry-run, submission, attempt, gateway, and reconciliation logic.
- [ ] Extract current API client/preflight/writer/evidence construction from `newControlledAPIApplicationService` into an API transport adapter. Keep API OAuth creation inside that adapter.
- [ ] Add browser transport construction using `hhwebsession`, cookie-web read/preflight, `CookieWebVacancyResponseWriter`, and web evidence reader. Do not instantiate `hhapi.APIHHClient`, read the token file, or load OAuth configuration on this path.
- [ ] Preserve the current service's early dry-run behavior after the explicit fresh preflight and before nonce/attempt dependencies are required. Dry-run must still build/validate the exact request context but must not consume the nonce or create an attempt.
- [ ] Preserve current `applicationattempt.Executor` as the durable reservation boundary and current `applicationsubmission.Service` as the provider-neutral submission boundary. Do not duplicate their logic in the transport adapter.
- [ ] Add an OAuth-independence test with absent token file and invalid OAuth fields; configure a valid fixture cookie session and assert browser dry-run preflight succeeds without opening the token file or API endpoint.
- [ ] Run all existing API application tests unchanged plus new browser composition tests.
- [ ] Commit the transport-neutral service.

**Acceptance criteria:** API applications still use OAuth exactly as before; browser applications use only cookies; both routes share one controlled service and existing use cases; dry-run validates fresh web state but consumes neither nonce nor attempt; the browser constructor succeeds without OAuth configuration.

**Safety invariant:** The transport adapter supplies provider I/O only. Approval, preflight gate, durable attempt reservation, gateway policy, and reconciliation remain centralized.

**Commit boundary:** `refactor: make controlled application service transport neutral`

---

### Task 7: Implement web reconciliation through existing `applicationreconciliation`

**Files:**

- Create: `internal/runtime/cookie_web_application_reconciliation.go`
- Create: `internal/runtime/cookie_web_application_reconciliation_test.go`
- Modify: `internal/runtime/application_reconciliation_compat.go`
- Modify: `internal/runtime/hh_api_application.go` to share final-outcome mapping
- Modify: `internal/usecase/applicationreconciliation/types.go` only if an additional provider-neutral evidence field is required; otherwise keep it unchanged.
- Modify: `internal/usecase/applicationreconciliation/service_test.go` only for shared regression cases.

**Interfaces:**

- Produce `type cookieWebApplicationEvidenceReader struct { ... }` implementing `applicationreconciliation.EvidenceReader`.
- Its method remains `ReadVacancyResponseEvidence(context.Context, applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error)` and performs only vacancy-scoped GETs.
- Reuse existing `applicationreconciliation.NewService`, `AttemptStore`, `EvidenceSnapshot`, `ProviderResponse`, and final outcome constants; do not add a second reconciliation state machine.

**Steps:**

- [ ] Add failing reconciliation tests for negotiations containing the target vacancy, explicit vacancy response marker, unrelated vacancy, absent provider identity, conflicting positive/negative state, unavailable GET, and unknown send result.
- [ ] Implement the reader using the same cookie session and existing HH HTML/application parsers. Use vacancy ID as the authoritative scope; include resume identity only when HH explicitly exposes it.
- [ ] Add tests proving local attempt existence alone never confirms delivery and that a positive vacancy-scoped provider identity confirms the existing attempt.
- [ ] Route success, already-applied, and unknown-send classes through the existing `applicationreconciliation.Service` and current final outcome mapping. Preserve unresolved outcomes and blocking attempt states.
- [ ] Add a session-health result to the controlled orchestration so a post-POST persistence failure remains unhealthy after a confirmed reconciliation.
- [ ] Run focused reconciliation tests and the existing `internal/usecase/applicationreconciliation` suite.
- [ ] Commit the web evidence reader and shared outcome mapping.

**Acceptance criteria:** Reconciliation is GET-only, vacancy-scoped, provider-evidence based, and uses the existing canonical attempt/reconciliation state. Unknown delivery is never retried. A persistence failure can coexist with confirmed application evidence but keeps the session unhealthy for batch control.

**Safety invariant:** Reconciliation can confirm or leave an attempt unresolved, but can never authorize a second POST or release an uncertain attempt for blind retry.

**Commit boundary:** `feat: reconcile cookie web applications with HH evidence`

---

### Task 8: Wire `apply` and `apply-batch` while preserving nonce/attempt ordering

**Files:**

- Modify: `internal/runtime/hh_api_application.go`
- Modify: `internal/runtime/hh_api_batch.go`
- Modify: `internal/runtime/hh_api_batch_test.go`
- Modify: `internal/runtime/hh_api_command.go`
- Modify: `internal/cli/parse.go`
- Modify: `internal/runtime/hh_api_application_test.go`
- Modify: `internal/runtime/application_preparation_acceptance_test.go`

**Interfaces:**

- Keep `controlledAPIApplicationExecutor` behavior but rename or alias it to `controlledApplicationExecutor` without changing `Execute(context.Context, string, time.Time) (controlledApplicationResult, error)`.
- Keep current `BatchApplicationRun`, `BatchApplicationItem`, and status values unless a new persistence-unhealthy stop status is needed; represent that state as `STOPPED_UNCERTAIN` with safe error metadata.

**Steps:**

- [ ] Add failing apply tests for browser transport acceptance, missing cookies, absent OAuth/token file, stale browser hash, missing mapping, vacancy-wide duplicate, required letter with empty content, optional letter with empty content, test required, and unknown preflight.
- [ ] Implement browser transport selection through existing `HH_TRANSPORT=browser`; keep command names and explicit approval-file grammar unchanged.
- [ ] Add failing ordering tests recording provider GET, approval-file nonce mutation, attempt `Reserve`, writer call, and reconciliation GET order. Assert no provider read occurs after the fresh preflight and before nonce reservation.
- [ ] Preserve current live sequence: validate approval/preparation/content → fresh web preflight → consume approval nonce using existing atomic file lock → invoke `applicationattempt.NewExecutor` so durable attempt reservation occurs → call `hhwritegateway.Service.SubmitVacancyResponse` → writer → existing reconciliation service.
- [ ] Preserve current dry-run sequence: validate approval/preparation/content → fresh web preflight → return `WOULD_ATTEMPT`/preview with `transport_attempted=false`; do not consume nonce, reserve an attempt, or call writer.
- [ ] Add batch tests for one, two, and three items; duplicate approval path; duplicate vacancy; pre-send block continuing to next item; transport uncertainty stopping; reconciliation error stopping; shared gateway limits; concurrent same nonce with one POST maximum; and dry-run zero mutations.
- [ ] Add the persistence-health stop test required by Review Focus: reconciliation may confirm the first item, but the second item is not started while the session is unhealthy.
- [ ] Run `go test ./internal/runtime -run 'TestHHAPIApply|TestHHAPIApplyBatch|TestCookieWebApply|Test.*Nonce|Test.*Batch' -count=1` and race the runtime package.
- [ ] Commit the CLI/batch integration.

**Acceptance criteria:** Existing CLI names and approval grammar remain valid; API mode remains unchanged; browser mode can run with cookies and no OAuth; pre-send blocks can continue; any attempted uncertainty or unhealthy persistence stops the batch; limits and nonce behavior remain shared and exact.

**Safety invariant:** The only live write sequence is approval validation → fresh preflight → nonce → durable attempt → gateway → writer → reconciliation. Dry-run has zero nonce/attempt/POST.

**Commit boundary:** `feat: enable cookie web apply and batch transport`

---

### Task 9: Update configuration, documentation, and compatibility checks

**Files:**

- Modify: `internal/config/config.go` only if a new validated field is required; prefer existing `HHTransport`.
- Modify: `internal/config/validation.go`, `internal/config/flags.go`, and `internal/config/config_test.go` only for browser transport validation/help text.
- Modify: `README.md`
- Modify: `example.env`
- Modify: `internal/runtime/hh_transport_test.go`
- Modify: `internal/runtime/browser_doctor_test.go`
- Modify: `internal/runtime/hh_api_command_test.go`

**Interfaces:**

- `HH_TRANSPORT=browser` is the cookie-web mode and requires an existing valid cookies path when a read/write operation is requested.
- `HH_TRANSPORT=api` continues to require the current API/OAuth setup and keeps current behavior.
- No new OAuth configuration is introduced for browser mode; no CLI rename is introduced.

**Steps:**

- [ ] Add failing config tests for browser mode with cookies and no OAuth, API mode with existing OAuth requirements, invalid transport values, and safe error text.
- [ ] Update validation/help only where browser mode semantics differ from the current description. Preserve CLI/env precedence and defaults.
- [ ] Document `browser-doctor`, cookies-only dry-run/live composition, remediation for stale cookies, and explicit prohibition on production POST tests.
- [ ] Update `example.env` comments without adding secrets or candidate data.
- [ ] Add a documentation/CLI regression test proving browser doctor does not suggest OAuth and API command output remains compatible.
- [ ] Run config/runtime documentation-adjacent tests and inspect `git diff --check`.
- [ ] Commit the compatibility/documentation update.

**Acceptance criteria:** Operators can select browser transport with cookies only; API transport remains available; documentation does not imply OAuth is required for browser mode; no secret or personal data is added.

**Safety invariant:** Configuration cannot silently switch transport or enable writes; existing defaults and dry-run protection remain intact.

**Commit boundary:** `docs: document cookie web transport compatibility`

---

### Task 10: Run final integration gates and produce the review evidence

**Files:**

- Modify only tests or documentation if a failing acceptance check exposes a concrete inconsistency; do not make unrelated cleanup changes.
- Add/update: `docs/validation/VALIDATION_COOKIE_WEB_APPLICATION_TRANSPORT.md`

**Interfaces:**

- No new runtime interface. This task verifies the completed composition and records evidence without production HH writes.

**Steps:**

- [ ] Add an integration-style `httptest` test with valid Netscape cookies, absent/invalid OAuth token configuration, browser transport, and a dry-run approval. Assert GET preflight succeeds, OAuth endpoint/token file is never touched, nonce remains unused, application-attempt store is unchanged, and POST count is zero.
- [ ] Add an `httptest` live-composition test with a fake HH server only. Assert the one POST has exact approved values, uses the jar, passes through the shared gateway, and performs fresh reconciliation; do not use a real HH URL or production cookies.
- [ ] Add an `httptest` reconciliation proof for success, already-applied, unknown-send confirmed, and unknown-send unresolved outcomes.
- [ ] Run `gofmt -w .` in the isolated worktree.
- [ ] Run `go test ./...` and record the result.
- [ ] Run `go test -race ./...` and record the result.
- [ ] Run `go vet ./...` and record the result.
- [ ] Run `go build ./...` and record the result.
- [ ] Run `git diff --check` and `git status --short --branch`.
- [ ] Inspect the final diff for cookies, authorization headers, token values, production HH URLs in POST tests, or accidental changes outside scope.
- [ ] Write the validation record with branch, HEAD, commits ahead of `origin/main`, working-tree status, test commands, and explicit counters: production HH POST `0`, applications sent `0`, messages sent `0`, resume mutations `0`.
- [ ] Commit the validation record only after all gates pass.

**Acceptance criteria:** All repository gates pass; cookies-only dry-run, live composition, and reconciliation are proven against `httptest`; no production HH mutation occurred; branch is unmerged and unpushed; final report can state `COOKIE_WEB_APPLICATION_TRANSPORT_READY_FOR_REVIEW` only with command evidence.

**Safety invariant:** Verification itself remains offline/GET-only except for fake `httptest` POSTs and cannot use production credentials or endpoints.

**Commit boundary:** `test: validate cookie web application transport`

## Explicit ordering decision

The current controlled API implementation already performs the critical split needed by the spec: it validates approval and preparation, performs the fresh provider preflight, returns before nonce consumption in dry-run, consumes the approval nonce under an atomic approval-file lock, and then invokes `applicationattempt.NewExecutor`, whose `store.Reserve` creates the durable `SENDING` attempt immediately before the gateway transport. The plan preserves that order for both API and browser adapters.

`applicationsubmission.Service` remains the provider-neutral request/preflight abstraction and `applicationattempt.Executor` remains the durable reservation boundary. The transport-neutral refactor must not move nonce consumption after attempt reservation, move either mutation before fresh preflight, or add a second preflight after nonce consumption. If a shared-service extraction reveals a need to change that order, implementation must stop at the affected commit boundary and request a separate safety review rather than silently changing semantics.

## Plan self-review

- Spec coverage: cookie parser/session, read-only composition, browser doctor, resume binding, web preflight, exact writer, redirect policy, persistence timing, transport selection, reconciliation, batch semantics, config/docs, OAuth independence, and full gates are covered by Tasks 1–10.
- Placeholder scan: no TODO/TBD or unspecified implementation step is used; each task names files, interfaces, tests, acceptance criteria, invariant, and commit boundary.
- Type consistency: `BrowserResumeHash`, `ResumeMappingReader`, `controlledApplicationTransport`, `controlledApplicationContext`, `CookieWebVacancyResponseWriter`, `RequestDoer`, and `EvidenceReader` are introduced before their consumers.
- Review focus: all five high-risk input classes have named tests in their owning task.

