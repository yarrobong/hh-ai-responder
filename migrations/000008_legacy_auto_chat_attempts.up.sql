CREATE TABLE IF NOT EXISTS legacy_auto_chat_attempts (
    attempt_id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    trigger_message_id TEXT NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN ('REPLY', 'LEAVE')),
    state TEXT NOT NULL CHECK (state IN ('SENDING', 'ACCEPTED', 'REJECTED', 'NOT_SENT', 'DELIVERY_UNCERTAIN', 'TARGET_REPLY_CONFIRMED', 'TARGET_LEAVE_CONFIRMED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    request_key TEXT NOT NULL DEFAULT '',
    provider_outgoing_message_id TEXT NOT NULL DEFAULT '',
    provider_status INTEGER NOT NULL DEFAULT 0,
    error_class TEXT NOT NULL DEFAULT '',
    CONSTRAINT legacy_auto_chat_attempts_timestamps_check CHECK (updated_at >= created_at),
    CONSTRAINT legacy_auto_chat_attempts_reply_key_check CHECK (action_type <> 'REPLY' OR btrim(request_key) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS legacy_auto_chat_attempts_active_trigger_unique
    ON legacy_auto_chat_attempts (conversation_id, trigger_message_id)
    WHERE state IN ('SENDING', 'ACCEPTED', 'DELIVERY_UNCERTAIN', 'TARGET_REPLY_CONFIRMED', 'TARGET_LEAVE_CONFIRMED');

CREATE INDEX IF NOT EXISTS legacy_auto_chat_attempts_trigger_created_idx
    ON legacy_auto_chat_attempts (conversation_id, trigger_message_id, created_at, attempt_id);
