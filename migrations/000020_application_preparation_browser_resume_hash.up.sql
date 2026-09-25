ALTER TABLE application_preparations
    ADD COLUMN IF NOT EXISTS browser_resume_hash TEXT;
