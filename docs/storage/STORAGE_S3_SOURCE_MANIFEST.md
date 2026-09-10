# S3 source manifest

Generated 2026-09-10 before any PostgreSQL import. Paths are relative to the
repository root. This manifest contains metadata only; it does not contain
candidate facts, message bodies, or credentials.

## Migration sources

| Path | Exists | Bytes | SHA-256 | Parse/schema | Logical records |
|---|---:|---:|---|---|---|
| `candidate_profile.json` | yes | 16265 | `303e72196f213cb2dbccb0095006347875feb3f18936d10ade238081a08f9129` | valid / version 1 | profile 1; education 1; languages 1; experience 1; profile skills 22; profile projects 3; pending unknowns 6 |
| `candidate_stories.json` | yes | 6556 | `90429f909c259aad5ce739fd06662e68697d7d46a38abf21b39d9a3a764f47eb` | valid / version 1 | stories 5 |
| `candidate_skills.json` | yes | 6736 | `4aaeb91c5ebd05d9e725d8f35d2cbe3d780e801ff4d8c9f7d3a92d6de77f5eab` | valid / version 1 | skills 10 |
| `candidate_projects.json` | yes | 2746 | `bd176d84d8dae674418d91693cc3f7c8db8af338147cd02db1084c63fb7d9690` | valid / version 1 | projects 3 |
| `candidate_achievements.json` | yes | 41 | `59d538b355c5365962108f2cc8684c62c075d9058a2fb1f459a4ae575a2a2458` | valid / version 1 | achievements 0 |
| `candidate_unknowns.json` | yes | 729 | `bc8248236ed5ebc501f3b95347dc189c25e0ff668c7586d61b43a664e44fc82c` | valid / version 1 | unknowns 1 |
| `candidate_proposals.json` | yes | 38 | `923f6894fafca17c4fad6ec5e2537ebe4f6cc309688f5c6f8f1d35c25d87eb61` | valid / version 1 | proposals 0 |
| `candidate_events.json` | yes | 17438 | `af6b5fafded42cd05a9d3622c8ca599cd2395bb38f960d17219f3389eadc0f86` | valid / version 1 | events 15 |
| `vacancies.json` | yes | 327218 | `825258d2305e16233684073b56af271118ff887de1552c95f46519b635316b4a` | valid / version 1 | vacancies 118 |
| `job_applications.json` | yes | 177629 | `ad56506bea638a3c184541c0bd67d7f77e3e8e260c8e0d43c72e39fe145573bd` | valid / version 1 | applications 98; events 308 |
| `employer_conversations.json` | yes | 791057 | `5e8bc5193e4eaefd2287de4efd7376b4e84eecfd7369f27f25e19e17248d4bdf` | valid / version 1 | conversations 261; messages 793 |

## R14 authority stores

| Path | Exists | Bytes | SHA-256 | Result |
|---|---:|---:|---|---|
| `application_attempts.json` | no | — | — | current store treats absent file as empty |
| `autochat_attempts.json` | no | — | — | current store treats absent file as empty |

## Intentional local/compatibility state inspected

| Path | Exists | Bytes | SHA-256 |
|---|---:|---:|---|
| `candidate_clarifications.json` | yes | 7065 | `13a70fa822c4b2ffde6cee0d8ded9358e836cfebfdc2eef662f0498d220d3432` |
| `ai_drafts.json` | yes | 8165 | `c5c886af17a5e0c059e9a1916fcad1609b489f1e2520984b46b76a9f0acca2da` |
| `notification_events.json` | yes | 149365 | `f4c6c017dc542f5f663a97dcc956689756c7e71adebb40dc447b6c4026ef0ac7` |
| `hh_write_actions.json` | yes | 27378 | `466047de273a9a3b06064af4d32e64fd9c0abea913ccb54f15eb53598784f8ad` |
| `hh_write_events.json` | yes | 30690 | `632ea319376051bbcb7a1d24d4c3df01be8ee9c6b563c2bce52974cfaf0805f5` |
| `hh_pilot_observations.json` | yes | 1111 | `be15b7be0a3b6b211bdfe6677010f9abb9e690b81b0c003fe6e86d317aaed3dd` |
| `quality_log.json` | yes | 268280 | `f79219943f88bb99519720db1fc7b4d21159467242435c5b9b44b552ae9ff6d7` |
| `hh_sync_state.json` | yes | 232 | `2bf694bf7542d8def7fc139de437d14490b014ea7e06b0cf3f17cc607e751128` |
| `.hh-already-responded.json` | yes | 362 | `a4dfa5a8afbd07aa78e9535a9fbb20eba79d4b5df06276610fbd44dcdbce32ca` |
| `career_monitor_state.json` | no | — | — |

These state files were not migration inputs. The `.career-validation/`
directory was inspected as fixture/output data and was not treated as the
configured runtime source.
