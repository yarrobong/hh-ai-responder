-- Reverse only the acquisition/provenance changes introduced by 000005.
-- Recreate the exact 000003/000004 checks before removing the new columns.
-- If post-000005 data uses an acquisition-only value, PostgreSQL will reject
-- this rollback rather than silently deleting or rewriting candidate data.

ALTER TABLE candidate_unknowns DROP CONSTRAINT IF EXISTS candidate_unknowns_status_check;
ALTER TABLE candidate_unknowns ADD CONSTRAINT candidate_unknowns_status_check
    CHECK (status IN ('needs_confirmation', 'confirmed', 'rejected'));

ALTER TABLE candidate_skill_uses DROP CONSTRAINT IF EXISTS candidate_skill_uses_parent_check;
ALTER TABLE candidate_skill_uses ADD CONSTRAINT candidate_skill_uses_parent_check
    CHECK (experience_id IS NOT NULL OR project_id IS NOT NULL OR usage_context IN ('unknown', 'studied_only', 'explicitly_not_used'));

ALTER TABLE candidate_knowledge_events DROP CONSTRAINT IF EXISTS candidate_events_source_check;
ALTER TABLE candidate_knowledge_events ADD CONSTRAINT candidate_events_source_check
    CHECK (source IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown'));

ALTER TABLE candidate_knowledge_sources DROP CONSTRAINT IF EXISTS candidate_sources_type_check;
ALTER TABLE candidate_knowledge_sources ADD CONSTRAINT candidate_sources_type_check
    CHECK (source_type IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown'));

ALTER TABLE candidate_unknowns
    DROP COLUMN IF EXISTS gap_key,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS application_id,
    DROP COLUMN IF EXISTS vacancy_id,
    DROP COLUMN IF EXISTS employer_message_id;

ALTER TABLE candidate_knowledge_proposals
    DROP COLUMN IF EXISTS unknown_id,
    DROP COLUMN IF EXISTS clarification_id,
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS employer_message_id;

ALTER TABLE candidate_knowledge_events
    DROP COLUMN IF EXISTS unknown_id,
    DROP COLUMN IF EXISTS clarification_id,
    DROP COLUMN IF EXISTS proposal_id,
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS employer_message_id;
