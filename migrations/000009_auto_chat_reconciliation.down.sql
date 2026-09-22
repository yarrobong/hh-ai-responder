ALTER TABLE legacy_auto_chat_attempts
    DROP COLUMN IF EXISTS reconciliation_causality_note,
    DROP COLUMN IF EXISTS reconciliation_observed_at,
    DROP COLUMN IF EXISTS reconciliation_provider_message_id,
    DROP COLUMN IF EXISTS reconciliation_source,
    DROP COLUMN IF EXISTS reconciliation_kind;
