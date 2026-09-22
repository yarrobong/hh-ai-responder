CREATE TABLE IF NOT EXISTS automatic_application_attempts (
    attempt_id TEXT PRIMARY KEY,
    vacancy_id INTEGER NOT NULL CHECK (vacancy_id > 0),
    resume_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('SENDING', 'ACCEPTED', 'REJECTED', 'NOT_SENT', 'DELIVERY_UNCERTAIN')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    provider_status INTEGER NOT NULL DEFAULT 0,
    error_class TEXT NOT NULL DEFAULT '',
    CONSTRAINT automatic_application_attempts_timestamps_check CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS automatic_application_attempts_active_vacancy_unique
    ON automatic_application_attempts (vacancy_id)
    WHERE state IN ('SENDING', 'ACCEPTED', 'DELIVERY_UNCERTAIN');

CREATE INDEX IF NOT EXISTS automatic_application_attempts_vacancy_created_idx
    ON automatic_application_attempts (vacancy_id, created_at, attempt_id);
