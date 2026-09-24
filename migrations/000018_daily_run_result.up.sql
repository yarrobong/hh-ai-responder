ALTER TABLE agent_runs
    ADD COLUMN IF NOT EXISTS daily_result_json JSONB;
