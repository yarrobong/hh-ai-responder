ALTER TABLE automatic_application_attempts
    ADD COLUMN IF NOT EXISTS reconciliation_kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_strength TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reconciliation_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_application_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_negotiation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_response_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS reconciliation_observed_at TIMESTAMPTZ;

ALTER TABLE automatic_application_attempts
    DROP CONSTRAINT IF EXISTS automatic_application_attempts_state_check;

ALTER TABLE automatic_application_attempts
    ADD CONSTRAINT automatic_application_attempts_state_check
    CHECK (state IN ('SENDING', 'ACCEPTED', 'REJECTED', 'NOT_SENT', 'DELIVERY_UNCERTAIN', 'TARGET_RESPONSE_CONFIRMED'));

DROP INDEX IF EXISTS automatic_application_attempts_active_vacancy_unique;

CREATE UNIQUE INDEX automatic_application_attempts_active_vacancy_unique
    ON automatic_application_attempts (vacancy_id)
    WHERE state IN ('SENDING', 'ACCEPTED', 'DELIVERY_UNCERTAIN', 'TARGET_RESPONSE_CONFIRMED');
