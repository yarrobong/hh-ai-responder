ALTER TABLE agent_run_items
    ADD COLUMN IF NOT EXISTS target_type TEXT NOT NULL DEFAULT 'vacancy',
    ADD COLUMN IF NOT EXISTS target_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS application_id TEXT,
    ADD COLUMN IF NOT EXISTS conversation_id TEXT;

UPDATE agent_run_items SET target_id = vacancy_id::text WHERE target_type = 'vacancy' AND target_id = '';

ALTER TABLE agent_run_items DROP CONSTRAINT IF EXISTS agent_run_items_run_id_vacancy_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS agent_run_items_run_target_key ON agent_run_items(run_id, target_type, target_id);
CREATE INDEX IF NOT EXISTS agent_run_items_conversation_idx ON agent_run_items(conversation_id, created_at DESC);
