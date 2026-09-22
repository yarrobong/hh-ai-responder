ALTER TABLE automatic_application_attempts
    ADD COLUMN IF NOT EXISTS provider_conversation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_confirmation_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_provider_identities JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS reconciliation_history JSONB NOT NULL DEFAULT '[]'::jsonb;
