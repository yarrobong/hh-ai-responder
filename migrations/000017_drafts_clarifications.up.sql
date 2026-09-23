-- Operational draft and clarification parity. Payload JSONB preserves the
-- existing JSON schema while indexed identity columns make replay safe.
CREATE TABLE IF NOT EXISTS ai_drafts (
    id TEXT PRIMARY KEY,
    input_fingerprint TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    employer_message_hash TEXT NOT NULL DEFAULT '',
    relevant_knowledge_hash TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    application_id TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL DEFAULT '',
    input_message_id TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL,
    original_text TEXT NOT NULL DEFAULT '',
    edited_text TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    decision_reason TEXT NOT NULL,
    used_facts JSONB NOT NULL DEFAULT '[]'::jsonb,
    CONSTRAINT ai_drafts_status_check CHECK (status IN ('generated', 'approved', 'rejected', 'superseded', 'sent')),
    CONSTRAINT ai_drafts_type_check CHECK (type IN ('follow_up', 'employer_reply', 'cover_letter', 'application_answer'))
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_drafts_input_fingerprint_key
    ON ai_drafts(input_fingerprint) WHERE input_fingerprint <> '';
CREATE INDEX IF NOT EXISTS ai_drafts_conversation_idx ON ai_drafts(conversation_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS ai_drafts_application_idx ON ai_drafts(application_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS candidate_clarifications (
    id TEXT PRIMARY KEY,
    identity TEXT NOT NULL UNIQUE,
    conversation_id TEXT NOT NULL DEFAULT '',
    application_id TEXT NOT NULL DEFAULT '',
    vacancy_id TEXT NOT NULL DEFAULT '',
    employer_message_id TEXT NOT NULL DEFAULT '',
    gap_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    CONSTRAINT candidate_clarifications_status_check CHECK (status IN ('pending', 'answered', 'dismissed', 'resolved_existing_knowledge'))
);
CREATE INDEX IF NOT EXISTS candidate_clarifications_relation_idx
    ON candidate_clarifications(conversation_id, application_id, vacancy_id, employer_message_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS candidate_clarifications_gap_idx ON candidate_clarifications(gap_key);
