ALTER TABLE legacy_auto_chat_attempts
    ADD COLUMN IF NOT EXISTS reconciliation_kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_provider_message_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_observed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS reconciliation_causality_note TEXT NOT NULL DEFAULT '';
