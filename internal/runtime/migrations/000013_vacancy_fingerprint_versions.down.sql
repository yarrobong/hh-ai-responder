ALTER TABLE vacancy_review_events
    DROP CONSTRAINT IF EXISTS vacancy_review_events_fingerprint_version_check;
ALTER TABLE vacancy_review_events
    DROP COLUMN IF EXISTS fingerprint_version;

ALTER TABLE vacancy_review_states
    DROP CONSTRAINT IF EXISTS vacancy_review_states_fingerprint_version_check;
ALTER TABLE vacancy_review_states
    DROP COLUMN IF EXISTS decision_fingerprint_version;
