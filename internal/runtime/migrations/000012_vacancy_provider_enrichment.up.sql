ALTER TABLE vacancies
    ADD COLUMN IF NOT EXISTS professional_roles JSONB;
