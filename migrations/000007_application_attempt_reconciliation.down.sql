DROP INDEX IF EXISTS automatic_application_attempts_active_vacancy_unique;

CREATE UNIQUE INDEX automatic_application_attempts_active_vacancy_unique
    ON automatic_application_attempts (vacancy_id)
    WHERE state IN ('SENDING', 'ACCEPTED', 'DELIVERY_UNCERTAIN');

ALTER TABLE automatic_application_attempts
    DROP CONSTRAINT IF EXISTS automatic_application_attempts_state_check;

ALTER TABLE automatic_application_attempts
    ADD CONSTRAINT automatic_application_attempts_state_check
    CHECK (state IN ('SENDING', 'ACCEPTED', 'REJECTED', 'NOT_SENT', 'DELIVERY_UNCERTAIN'));

ALTER TABLE automatic_application_attempts
    DROP COLUMN IF EXISTS reconciliation_kind,
    DROP COLUMN IF EXISTS reconciliation_strength,
    DROP COLUMN IF EXISTS reconciliation_source,
    DROP COLUMN IF EXISTS provider_application_id,
    DROP COLUMN IF EXISTS provider_negotiation_id,
    DROP COLUMN IF EXISTS provider_response_at,
    DROP COLUMN IF EXISTS reconciliation_observed_at;
