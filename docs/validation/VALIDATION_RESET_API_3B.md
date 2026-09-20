# RESET-API-3B validation

Date: 2026-09-21

Baseline: `b2b53bd13123a78625307d2a78832f57c41cf9cd`

## Verification suite

All commands completed successfully:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

The tests include local `httptest` fixtures, automatic MATCH compatibility,
manual AI-UNCERTAIN approval eligibility, exact letter hashing, stale and
blocked-provider cases, nonce reuse rejection, cross-process nonce
contention, and dry-run zero-POST assertions.

## Vacancy 137532422 read-only validation

Pilot command:

```bash
go run ./cmd/hh-ai-responder \
  -hh-transport api -dry-run=true -hh-write-enabled=false \
  -career-agent-result "$VALIDATION_DIR/result" \
  career-agent pilot --vacancy 137532422
```

Result: `PILOT: BLOCKED`. The read-only pilot performed HH reads and failed
closed because its router selected no enabled resume. It issued zero HH
writes and zero Application POST requests.

Approval review was attempted against that generated blocked pilot and was
rejected as an incomplete/ineligible pilot. No approval artifact was
created, and no bypass or synthetic approval was used.

Independent GET-only provider preflight:

```bash
go run ./cmd/hh-ai-responder \
  -hh-transport api -dry-run=true -hh-write-enabled=false \
  hh-api preflight 137532422 --all-resumes
```

The provider reported active/open vacancy state, duplicate `NO`, no test,
available standard application state, and suitable resumes. The command
reported `NOT_ATTEMPTED (POST not issued)` for each resume.

Because the pilot itself was blocked, the API apply dry-run was not invoked
with a fabricated approval. The supported safe flow remains:

```bash
HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder \
  -career-agent-result "$VALIDATION_DIR/result" \
  career-agent pilot --vacancy 137532422

HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder hh-api approval review \
  --pilot "$VALIDATION_DIR/result.pilot.json" \
  --out "$VALIDATION_DIR/approval.json"

HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder hh-api apply 137532422 \
  --resume-id "$(jq -r '.provider_resume_id' "$VALIDATION_DIR/approval.json")" \
  --approval-file "$VALIDATION_DIR/approval.json"
```

No real HH application was sent during implementation or validation.

Application POST count: **0**.
