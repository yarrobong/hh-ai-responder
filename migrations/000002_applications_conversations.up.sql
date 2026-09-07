-- Career-data relations are deliberately restrictive. The application and
-- conversation histories are not disposable projections, so deleting a
-- vacancy or parent record must be an explicit future operation.
CREATE TABLE IF NOT EXISTS applications (
    id TEXT PRIMARY KEY,
    external_id TEXT NOT NULL DEFAULT '',
    vacancy_id INTEGER NOT NULL,
    -- Compatibility projection of the domain aggregate. The canonical
    -- relational ownership is conversations.application_id below; keeping
    -- only that side as an FK avoids a circular mandatory relation.
    conversation_id TEXT NOT NULL DEFAULT '',
    company_name TEXT NOT NULL DEFAULT '',
    vacancy_title TEXT NOT NULL DEFAULT '',
    vacancy_url TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    raw_status TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    created_at_ns BIGINT,
    updated_at_ns BIGINT,
    follow_up_state TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    next_action TEXT NOT NULL DEFAULT '',
    match_result JSONB,
    hh_metadata JSONB,
    partial BOOLEAN NOT NULL DEFAULT FALSE,
    data_completeness TEXT NOT NULL DEFAULT '',
    reconciliation_evidence JSONB,
    CONSTRAINT applications_vacancy_fk FOREIGN KEY (vacancy_id)
        REFERENCES vacancies(id) ON DELETE RESTRICT,
    CONSTRAINT applications_timestamps_check CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS applications_external_id_unique
    ON applications (external_id)
    WHERE btrim(external_id) <> '';
CREATE INDEX IF NOT EXISTS applications_vacancy_id_idx ON applications (vacancy_id);
CREATE INDEX IF NOT EXISTS applications_status_idx ON applications (status);

CREATE TABLE IF NOT EXISTS application_events (
    sequence BIGSERIAL NOT NULL,
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    created_at_ns BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT application_events_application_fk FOREIGN KEY (application_id)
        REFERENCES applications(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS application_events_application_created_idx
    ON application_events (application_id, created_at, id);

CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    external_id TEXT NOT NULL DEFAULT '',
    vacancy_id INTEGER NOT NULL,
    application_id TEXT,
    company_name TEXT NOT NULL DEFAULT '',
    vacancy_title TEXT NOT NULL DEFAULT '',
    vacancy_description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    created_at_ns BIGINT,
    updated_at_ns BIGINT,
    hh_updated_at TIMESTAMPTZ,
    hh_updated_at_ns BIGINT,
    last_employer_message_at TIMESTAMPTZ,
    last_employer_message_at_ns BIGINT,
    last_candidate_message_at TIMESTAMPTZ,
    last_candidate_message_at_ns BIGINT,
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_action TEXT NOT NULL DEFAULT '',
    waiting_since TIMESTAMPTZ,
    waiting_since_ns BIGINT,
    last_activity_at TIMESTAMPTZ,
    last_activity_at_ns BIGINT,
    follow_up_state TEXT NOT NULL DEFAULT '',
    raw_status TEXT NOT NULL DEFAULT '',
    hh_metadata JSONB,
    CONSTRAINT conversations_vacancy_fk FOREIGN KEY (vacancy_id)
        REFERENCES vacancies(id) ON DELETE RESTRICT,
    CONSTRAINT conversations_application_fk FOREIGN KEY (application_id)
        REFERENCES applications(id) ON DELETE SET NULL,
    CONSTRAINT conversations_timestamps_check CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS conversations_external_id_unique
    ON conversations (external_id)
    WHERE btrim(external_id) <> '';
CREATE INDEX IF NOT EXISTS conversations_vacancy_id_idx ON conversations (vacancy_id);
CREATE INDEX IF NOT EXISTS conversations_status_idx ON conversations (status);

CREATE TABLE IF NOT EXISTS conversation_messages (
    sequence BIGSERIAL NOT NULL,
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL,
    timestamp_ns BIGINT,
    sender TEXT NOT NULL,
    direction TEXT NOT NULL,
    source TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    system_event BOOLEAN NOT NULL DEFAULT FALSE,
    content_unavailable BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB,
    CONSTRAINT conversation_messages_conversation_fk FOREIGN KEY (conversation_id)
        REFERENCES conversations(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS conversation_messages_conversation_timestamp_idx
    ON conversation_messages (conversation_id, timestamp, id);
CREATE UNIQUE INDEX IF NOT EXISTS conversation_messages_external_id_unique
    ON conversation_messages (conversation_id, source, external_id)
    WHERE btrim(external_id) <> '';
