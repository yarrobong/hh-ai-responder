# Candidate writer audit

The pre-migration audit identified the following production writer surfaces.
After this stage, backend selection is centralized: JSON keeps the legacy
implementation, while PostgreSQL uses the canonical mutation service. There
is no dual write.

| Writer | Canonical entities affected | Before | After | Production usage |
| --- | --- | --- | --- |
| `CandidateProfile` import/save and HH resume merge | identity, profile facts, education, experience, projects, skills, languages, preferences, pending unknowns | JSON files | explicit PostgreSQL resume importer; startup merge is read-only | compatibility / importer |
| `CandidateKnowledgeBase.Add*` / `Save` | skills, projects, achievements, unknowns, proposals, events | JSON files | not reachable from postgres composition | compatibility |
| `CandidateKnowledgeUpdater.Update*` | facts, proposals, metadata, unknowns, events | JSON updater | `CandidateMutationService` | yes |
| user `confirm` / `reject` | proposal, fact metadata, unknown resolution, event | JSON updater | backend-selected command service | yes for supported knowledge commands |
| `SyncProfileKnowledge` | profile-derived knowledge and migration events | JSON files | explicitly unsupported in postgres mode | compatibility only |
| HH resume merge at runtime | local snapshot / profile facts | JSON profile save | no startup mutation; explicit importer API | read-only startup |
| GitHub verifier / candidate interview hooks | facts, proposals and provenance | JSON updater | backend-selected mutation service | yes with trusted verifier/actor |
| employer-chat clarification reconciliation | clarification records and unknown/proposal | clarification JSON + JSON updater | clarification JSON + candidate mutation service | yes |

## Stage completion inventory

| Writer | Before | After | Transactional | Production |
| --- | --- | --- | --- | --- |
| `CandidateKnowledgeUpdater.Update*` | JSON KB | `CandidateMutationService` selected by backend; JSON updater retained for `json`, typed PostgreSQL mutations for `postgres` | yes in PostgreSQL | yes |
| proposal confirm/reject | JSON KB + event | `ConfirmProposal` / `RejectProposal` | proposal, fact/claim and event in one candidate transaction | yes |
| dashboard unknown/proposal actions | JSON updater | mutation service in PostgreSQL; JSON updater in JSON mode | yes in PostgreSQL | yes |
| `profile knowledge` CLI | JSON updater | PostgreSQL proposal lifecycle; legacy-only `sync-profile` is explicitly refused | yes in PostgreSQL | yes for supported commands |
| HH startup resume merge | wrote `candidate_profile.json` | no candidate write in PostgreSQL mode; explicit importer API only | n/a at startup | read-only |
| explicit HH resume import | legacy merge semantics | `ImportHHResumeFacts`, verified provenance, protected user-confirmed facts | yes | available to importer |
| candidate context / vacancy / conversation readers | JSON mapper | `PostgresCandidateRepository` in PostgreSQL mode | read-only snapshot | yes |
| legacy profile bootstrap/import | JSON profile writer | explicitly unsupported in PostgreSQL mode; remains available in JSON mode | JSON file semantics | compatibility only |

The canonical PostgreSQL mutation boundary is `PostgresCandidateStore.WithTx`.
Normal production mutations read the aggregate inside that transaction, apply
typed domain rules, append `KnowledgeEvent`, and commit with an optimistic
version check. `PersistCandidate` remains an import/admin whole-aggregate API;
ordinary writers do not use it. There is no PostgreSQL+JSON dual-write path and
startup never silently falls back to JSON.
