-- P1.1: provider observation state is separate from user review state.
-- Legacy vacancies intentionally receive no rows here: absence means that
-- first/last observation history is unknown until the first P1-aware sync.
CREATE TABLE IF NOT EXISTS vacancy_freshness (
    vacancy_id INTEGER PRIMARY KEY REFERENCES vacancies(id) ON DELETE RESTRICT,
    first_seen_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    first_seen_at_ns BIGINT,
    last_seen_at_ns BIGINT,
    source_fingerprint TEXT NOT NULL,
    previous_source_fingerprint TEXT NOT NULL DEFAULT '',
    material_fingerprint TEXT NOT NULL,
    fingerprint_version INTEGER NOT NULL,
    material_changed_at TIMESTAMPTZ,
    material_changed_at_ns BIGINT,
    CONSTRAINT vacancy_freshness_fingerprint_version_check CHECK (fingerprint_version > 0),
    CONSTRAINT vacancy_freshness_seen_pair_check CHECK (
        (first_seen_at IS NULL AND first_seen_at_ns IS NULL) OR
        (first_seen_at IS NOT NULL AND first_seen_at_ns IS NOT NULL)
    ),
    CONSTRAINT vacancy_freshness_last_seen_pair_check CHECK (
        (last_seen_at IS NULL AND last_seen_at_ns IS NULL) OR
        (last_seen_at IS NOT NULL AND last_seen_at_ns IS NOT NULL)
    ),
    CONSTRAINT vacancy_freshness_order_check CHECK (
        first_seen_at IS NULL OR last_seen_at IS NULL OR last_seen_at >= first_seen_at
    )
);

CREATE INDEX IF NOT EXISTS vacancy_freshness_last_seen_idx
    ON vacancy_freshness (last_seen_at DESC, vacancy_id);

CREATE TABLE IF NOT EXISTS vacancy_review_states (
    vacancy_id INTEGER PRIMARY KEY REFERENCES vacancies(id) ON DELETE RESTRICT,
    state TEXT NOT NULL CHECK (state IN ('seen', 'interesting', 'dismissed', 'prepared', 'applied')),
    state_changed_at TIMESTAMPTZ NOT NULL,
    state_changed_at_ns BIGINT,
    decision_source_fingerprint TEXT NOT NULL DEFAULT '',
    decision_material_fingerprint TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    updated_at_ns BIGINT,
    CONSTRAINT vacancy_review_states_timestamp_check CHECK (updated_at >= state_changed_at)
);

CREATE TABLE IF NOT EXISTS vacancy_review_events (
    id BIGSERIAL PRIMARY KEY,
    vacancy_id INTEGER NOT NULL REFERENCES vacancies(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL CHECK (event_type IN ('seen', 'interesting', 'dismissed', 'prepared', 'applied')),
    occurred_at TIMESTAMPTZ NOT NULL,
    occurred_at_ns BIGINT,
    source_fingerprint TEXT NOT NULL DEFAULT '',
    material_fingerprint TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL CHECK (source IN ('user', 'system_application', 'migration'))
);

CREATE INDEX IF NOT EXISTS vacancy_review_events_vacancy_occurred_idx
    ON vacancy_review_events (vacancy_id, occurred_at, id);
