DROP INDEX IF EXISTS agent_run_items_conversation_idx;
DROP INDEX IF EXISTS agent_run_items_run_target_key;
DELETE FROM agent_run_items WHERE target_type <> 'vacancy';
ALTER TABLE agent_run_items
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS application_id,
    DROP COLUMN IF EXISTS target_id,
    DROP COLUMN IF EXISTS target_type;
ALTER TABLE agent_run_items ADD CONSTRAINT agent_run_items_run_id_vacancy_id_key UNIQUE (run_id, vacancy_id);
