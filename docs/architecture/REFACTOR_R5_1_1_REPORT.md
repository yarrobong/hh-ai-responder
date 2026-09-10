# Duplicate inventory before

Candidate: root `candidate_canonical.go` contained a structural `Candidate` and canonical child values; `internal/candidate` contained mirrored values.

Truth: `TruthStatus` was structurally duplicated in root and `internal/candidate`.

Provenance: `CandidateSource`, `KnowledgeSource`, and their constants were duplicated; profile source ranking also had a root implementation.

Projection: root `CanonicalEmployerSafeProjection` and its safe-profile helpers duplicated the pure projection in `internal/candidate`; store projections had overlapping but different semantics.

Normalization: root skill-name normalization duplicated the domain rule.

Knowledge values: detailed skills, projects, achievements, unknowns, events, and proposals were duplicated between root and `internal/candidate`.

# Consolidation

Canonical owner: `internal/candidate`.

Root aliases:

- `Candidate` and canonical child values
- truth/provenance values and constants
- profile fact/value types
- knowledge entity values and statuses
- `KnowledgeProposal`, `CandidateKnowledgeEvent`, and `ResumeFacts`
- employer-safe projection value types

Root delegating wrappers:

- `CanonicalEmployerSafeProjection`
- `safeMetadata`, `safeProfileFact`, and `canonicalProfileFact`
- source validation, profile-fact validation, skill-level validation
- source priority and skill-name normalization

Root storage aggregates retained: `CandidateProfile`, `CandidateKnowledgeBase`, JSON stores, PostgreSQL repositories, and all mutation/workflow services.

# Candidate structural definitions

Remaining structural `type Candidate struct` declarations: 1.

Location: `internal/candidate/canonical.go`.

Root compatibility is `type Candidate = domaincandidate.Candidate`.

# Truth structural definitions

Remaining structural `type TruthStatus string` declarations: 1, in `internal/candidate/knowledge.go`.

Root truth names are aliases/constants. The actual vocabulary remains exactly `confirmed`, `verified`, `hypothesis`, and `unknown`.

# Provenance structural definitions

`CandidateSource` and `KnowledgeSource` each have one structural definition in `internal/candidate`; root names are aliases. Existing values and priority remain unchanged: `user_confirmed > hh_resume > github_verified > derived`.

# Safe projection

Domain implementation: `candidate.CanonicalEmployerSafeProjection` in `internal/candidate/projection.go`.

Root adapters: `CanonicalEmployerSafeProjection` delegates directly; legacy knowledge-base methods remain store/workflow projections and use the domain exposure predicate. `GetEmployerSafeKnowledge` is a detailed knowledge-store view; `GetEmployerSafeCandidateKnowledge` combines that view with confirmed legacy profile values; `CandidateKnowledgeSnapshot` is a detached relevant-knowledge compatibility snapshot. These are different semantics.

Duplicated filtering algorithm: NONE for canonical projection. Root filtering calls the domain exposure predicates.

# Knowledge model

Aliased values: `CandidateSkillDetailed`, `CandidateProject`, `CandidateAchievement`, `CandidateUnknown`, `KnowledgeProposal`, `CandidateKnowledgeEvent`, metadata/source records, statuses, and project/unknown/proposal enums.

Root storage/workflow types retained: `CandidateKnowledgeBase`, collection loading/saving, migration, proposal lifecycle, updater, mutation, acquisition, and repositories.

Why: R5.1.1 consolidates ownership only; resolver, mutation, acquisition, storage, PostgreSQL, semantic retrieval, HH parsing, AI, CLI, and dashboard remain deferred.

# Legacy profile distinction

`CandidateProfile` remains a root-owned legacy persistence/CLI aggregate. Its pure field types now alias `internal/candidate`, while file I/O, merge/import orchestration, bootstrap, and interactive methods remain root. It is intentionally distinct from canonical `candidate.Candidate`.

# Dependencies

`go list ./...`: PASS.

`go list -deps ./internal/candidate`: standard library only; no project package, vacancy, application, conversation, config, platform, HH, AI, storage, or PostgreSQL dependency.

Candidate project dependencies: NONE.

# Behavior

Candidate: UNCHANGED.

Truth: UNCHANGED; confidence remains independent from truth.

Provenance: UNCHANGED.

Confidence: UNCHANGED.

Safe projection: UNCHANGED.

Resolver, mutation, acquisition, semantic retrieval, HH, AI, vacancy, application, conversation, dashboard, JSON, PostgreSQL, and CLI: UNCHANGED.

The characterization suite verifies 11 months remains 11 months, unknown Kubernetes is not promoted, high-confidence hypotheses remain unexposable, confirmed salary/relocation remain safe, and unrelated facts do not affect the projection.

LIVE HH WRITES: 0.

# Verification

`gofmt -w .`: PASS

`go test -count=1 ./...`: PASS on rerun; one initial run hit a timing-sensitive Stage22 interval assertion and the immediate rerun passed.

`go test -race ./...`: PASS.

`go vet ./...`: PASS.

`go build ./...`: PASS.

`git diff --check`: PASS.

`node --check web/app.js`: PASS.

Focused candidate and boundary tests: PASS.

Docker: SKIPPED; Docker CLI is installed but the daemon is unavailable.

# Size

Root production LOC before consolidation: 39,852.

Root production LOC after consolidation: 39,162.

`internal/candidate` production LOC: 1,130.

# Ready for R5.2

READY. The canonical Candidate, extracted truth/provenance values, source priority, normalization, and pure safe projection now have one domain owner. Resolver/context extraction remains the next stage.

STOP after R5.1.1.
