CREATE TABLE IF NOT EXISTS vacancies (
    id INTEGER PRIMARY KEY CHECK (id >= 0),
    external_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    requirements JSONB,
    skills JSONB,
    salary TEXT NOT NULL DEFAULT '',
    salary_currency TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    work_format TEXT NOT NULL DEFAULT '',
    employment_type TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ,
    hh_updated_at TIMESTAMPTZ,
    hh_metadata JSONB,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    -- PostgreSQL stores timestamptz to microsecond precision. These companion
    -- values preserve the domain time.Time nanoseconds on repository writes.
    published_at_ns BIGINT,
    hh_updated_at_ns BIGINT,
    created_at_ns BIGINT,
    updated_at_ns BIGINT,
    work_schedule TEXT NOT NULL DEFAULT '',
    work_experience TEXT NOT NULL DEFAULT '',
    links JSONB,
    total_responses_count INTEGER NOT NULL DEFAULT 0,
    area_name TEXT NOT NULL DEFAULT '',
    company_id INTEGER NOT NULL DEFAULT 0,
    company_name TEXT NOT NULL DEFAULT '',
    company_site_url TEXT NOT NULL DEFAULT '',
    compensation JSONB,
    creation_time TEXT NOT NULL DEFAULT '',
    last_change_time JSONB,
    user_labels JSONB,
    response_letter_required BOOLEAN NOT NULL DEFAULT FALSE,
    user_test_present BOOLEAN NOT NULL DEFAULT FALSE,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    response_url TEXT NOT NULL DEFAULT '',
    total_responses_count_known BOOLEAN NOT NULL DEFAULT FALSE,
    match_result JSONB,
    application_recommendation JSONB,
    data_completeness TEXT NOT NULL DEFAULT '',
    reconciliation_evidence JSONB,
    CHECK ((created_at IS NULL AND updated_at IS NULL) OR
           (created_at IS NOT NULL AND updated_at IS NOT NULL AND updated_at >= created_at))
);

CREATE UNIQUE INDEX IF NOT EXISTS vacancies_external_id_unique
    ON vacancies (external_id)
    WHERE btrim(external_id) <> '';
