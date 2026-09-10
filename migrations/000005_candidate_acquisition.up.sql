-- Candidate knowledge acquisition provenance. These columns are workflow
-- references only; employer text remains outside canonical candidate facts.
ALTER TABLE candidate_unknowns
    ADD COLUMN IF NOT EXISTS gap_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS conversation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS vacancy_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS employer_message_id TEXT NOT NULL DEFAULT '';

ALTER TABLE candidate_knowledge_proposals
    ADD COLUMN IF NOT EXISTS unknown_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS clarification_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS conversation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS employer_message_id TEXT NOT NULL DEFAULT '';

ALTER TABLE candidate_knowledge_events
    ADD COLUMN IF NOT EXISTS unknown_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS clarification_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS proposal_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS conversation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS employer_message_id TEXT NOT NULL DEFAULT '';

ALTER TABLE candidate_unknowns DROP CONSTRAINT IF EXISTS candidate_unknowns_status_check;
ALTER TABLE candidate_unknowns ADD CONSTRAINT candidate_unknowns_status_check
    CHECK (status IN ('needs_confirmation', 'confirmed', 'rejected', 'dismissed', 'superseded'));

ALTER TABLE candidate_skill_uses DROP CONSTRAINT IF EXISTS candidate_skill_uses_parent_check;
ALTER TABLE candidate_skill_uses ADD CONSTRAINT candidate_skill_uses_parent_check
    CHECK (experience_id IS NOT NULL OR project_id IS NOT NULL OR usage_context IN ('commercial', 'pet_project', 'educational', 'personal', 'unknown', 'studied_only', 'explicitly_not_used'));

ALTER TABLE candidate_knowledge_events DROP CONSTRAINT IF EXISTS candidate_events_source_check;
ALTER TABLE candidate_knowledge_events ADD CONSTRAINT candidate_events_source_check
    CHECK (source IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'employer_conversation', 'derived', 'unknown'));
ALTER TABLE candidate_knowledge_sources DROP CONSTRAINT IF EXISTS candidate_sources_type_check;
ALTER TABLE candidate_knowledge_sources ADD CONSTRAINT candidate_sources_type_check
    CHECK (source_type IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'employer_conversation', 'derived', 'unknown'));
