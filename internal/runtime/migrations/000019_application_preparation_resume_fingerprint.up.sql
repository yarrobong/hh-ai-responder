ALTER TABLE application_preparations
    ADD COLUMN IF NOT EXISTS resume_fingerprint TEXT;
