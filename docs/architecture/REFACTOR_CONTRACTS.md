# Refactor contract matrix

These are the contracts to preserve before and during package extraction.

| Contract | Existing coverage | R1 addition | Still missing / follow-up |
| --- | --- | --- | --- |
| Env vs CLI precedence | `main_test.go`, configuration tests | none | Expand table coverage for every high-impact flag in R2 |
| `HH_DRY_RUN` default and write capability | `hh_write_gateway_test.go`, `dashboard_test.go` | none | Add explicit CLI subprocess/config characterization if routing changes |
| Major subcommand routing | command-specific tests across `*_test.go` | none | Consolidate a small routing table test before CLI extraction |
| JSON filenames and field compatibility | `*_storage_contract_test.go`, candidate storage tests | none | Preserve golden fixtures during adapter moves |
| Private file permissions where guaranteed | candidate store tests and atomic-write paths | none | Add explicit mode assertions only where existing behavior is contractual |
| Confirmed > verified/HH > derived | candidate fact/knowledge tests | none | Cover conflicting-source order with a compact table |
| Unknown is not confirmation | `candidate_knowledge_test.go`, acquisition tests | none | Add employer projection regression if projection moves |
| Employer-safe projection excludes unsafe claims | `candidate_knowledge_projection.go` tests, resolver tests | none | Add one end-to-end projection fixture before candidate extraction |
| No send without approval | `hh_write_gateway_test.go` | none | Keep gateway tests at adapter boundary |
| Fresh preflight required | `hh_write_gateway_test.go`, `vacancy_preflight_test.go` | none | Add stale targeted-read case if gateway moves |
| Terminal conversation blocked | `hh_write_gateway_test.go`, conversation eligibility tests | none | Preserve terminal-state table |
| Dry-run blocks transport | `hh_write_gateway_test.go`, dashboard tests | none | Keep fake transport assertion |
| Nonce rules | `hh_write_gateway_test.go`, validation reports | none | Add explicit replay characterization before adapter extraction |
| No automatic retry after uncertain transport | `hh_write_gateway_test.go` | none | Preserve uncertain-delivery terminal state |
| Targeted read does not invoke full sync | `stage22_1_test.go`, `hh_read_sync_test.go` | none | Keep service-level spy at R2 boundary |
| 000005 rollback owns only its columns/check replacements | migration files | `migration_contract_test.go` | Live PostgreSQL migration execution when a disposable DB is available |

The acquisition tests already provide strong lifecycle coverage, so R1 does
not duplicate them.
