-- P1.4.1: review decisions/events must identify the fingerprint contract
-- used when their hashes were recorded. Existing rows are v1 by definition.
ALTER TABLE vacancy_review_states
    ADD COLUMN IF NOT EXISTS decision_fingerprint_version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE vacancy_review_states
    ADD CONSTRAINT vacancy_review_states_fingerprint_version_check
    CHECK (decision_fingerprint_version > 0);

ALTER TABLE vacancy_review_events
    ADD COLUMN IF NOT EXISTS fingerprint_version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE vacancy_review_events
    ADD CONSTRAINT vacancy_review_events_fingerprint_version_check
    CHECK (fingerprint_version > 0);
