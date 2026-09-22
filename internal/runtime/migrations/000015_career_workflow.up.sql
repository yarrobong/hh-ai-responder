CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    run_type TEXT NOT NULL DEFAULT 'career_agent',
    stage TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'partial', 'failed')),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    result_code TEXT,
    summary TEXT,
    error_code TEXT,
    error_summary TEXT,
    confidence DOUBLE PRECISION CHECK (confidence >= 0 AND confidence <= 1),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_run_items (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    vacancy_id BIGINT NOT NULL,
    stage TEXT NOT NULL,
    status TEXT NOT NULL,
    decision_code TEXT,
    confidence DOUBLE PRECISION CHECK (confidence >= 0 AND confidence <= 1),
    evidence_json JSONB,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(run_id, vacancy_id)
);

CREATE TABLE IF NOT EXISTS application_preparations (
    id TEXT PRIMARY KEY,
    vacancy_id BIGINT NOT NULL,
    resume_id TEXT,
    resume_provider_id TEXT,
    candidate_id TEXT NOT NULL,
    candidate_version INTEGER NOT NULL,
    candidate_snapshot_hash TEXT NOT NULL,
    route_status TEXT NOT NULL,
    route_confidence TEXT,
    evidence_json JSONB,
    story_ids JSONB,
    cover_letter TEXT,
    cover_letter_hash TEXT,
    test_answer_drafts_json JSONB,
    knowledge_requests_json JSONB,
    input_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL,
    stale_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(vacancy_id, input_fingerprint)
);

CREATE INDEX IF NOT EXISTS agent_runs_status_started_idx ON agent_runs(status, started_at DESC);
CREATE INDEX IF NOT EXISTS agent_run_items_run_created_idx ON agent_run_items(run_id, created_at DESC);
CREATE INDEX IF NOT EXISTS application_preparations_status_updated_idx ON application_preparations(status, updated_at DESC);
