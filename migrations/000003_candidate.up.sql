-- Canonical candidate aggregate.  This schema intentionally does not mirror
-- the legacy JSON files: those files are an import/compatibility backend.
CREATE TABLE IF NOT EXISTS candidates (
    id TEXT PRIMARY KEY,
    version INTEGER NOT NULL,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    created_at_ns BIGINT,
    updated_at_ns BIGINT,
    identity JSONB NOT NULL DEFAULT '{}'::jsonb,
    profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    resume_facts JSONB,
    source_fingerprint TEXT NOT NULL DEFAULT '',
    CONSTRAINT candidates_version_check CHECK (version > 0),
    CONSTRAINT candidates_identity_object_check CHECK (jsonb_typeof(identity) = 'object'),
    CONSTRAINT candidates_profile_object_check CHECK (jsonb_typeof(profile) = 'object')
);

CREATE TABLE IF NOT EXISTS candidate_contacts (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    value TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_contacts_candidate_idx ON candidate_contacts(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_external_references (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    url TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_external_references_candidate_idx ON candidate_external_references(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_education (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    level TEXT NOT NULL DEFAULT '', institution TEXT NOT NULL DEFAULT '',
    specialty TEXT NOT NULL DEFAULT '', details TEXT NOT NULL DEFAULT '',
    claim_id TEXT NOT NULL DEFAULT '', metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_education_candidate_idx ON candidate_education(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_languages (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    name TEXT NOT NULL, level TEXT NOT NULL DEFAULT '', claim_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_languages_candidate_idx ON candidate_languages(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_experiences (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    company TEXT NOT NULL DEFAULT '', position TEXT NOT NULL DEFAULT '',
    start_date TEXT NOT NULL DEFAULT '', end_date TEXT NOT NULL DEFAULT '',
    employment_type TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
    responsibilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    skills_used JSONB NOT NULL DEFAULT '[]'::jsonb,
    achievements JSONB NOT NULL DEFAULT '[]'::jsonb,
    story_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    claim_id TEXT NOT NULL DEFAULT '', metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_experiences_candidate_idx ON candidate_experiences(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_skills (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    canonical_name TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '', level TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '', last_used TEXT NOT NULL DEFAULT '',
    cannot_claim JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    claim_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    negative BOOLEAN NOT NULL DEFAULT FALSE,
    state TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_assertions JSONB NOT NULL DEFAULT '[]'::jsonb,
    CONSTRAINT candidate_skills_state_check CHECK (state IN ('active', 'disputed', 'superseded'))
);
CREATE INDEX IF NOT EXISTS candidate_skills_candidate_name_idx ON candidate_skills(candidate_id, canonical_name, id);

CREATE TABLE IF NOT EXISTS candidate_projects (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    name TEXT NOT NULL, type TEXT NOT NULL DEFAULT '', role TEXT NOT NULL DEFAULT '',
    period JSONB NOT NULL DEFAULT '{}'::jsonb, description TEXT NOT NULL DEFAULT '',
    technologies JSONB NOT NULL DEFAULT '[]'::jsonb, tasks JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb, related_skills JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_ids JSONB NOT NULL DEFAULT '[]'::jsonb, claim_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    skill_uses JSONB NOT NULL DEFAULT '[]'::jsonb, achievement_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    story_ids JSONB NOT NULL DEFAULT '[]'::jsonb, metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    profile_source JSONB, detailed_source JSONB,
    CONSTRAINT candidate_projects_type_check CHECK (type IN ('', 'commercial', 'personal', 'education'))
);
CREATE INDEX IF NOT EXISTS candidate_projects_candidate_idx ON candidate_projects(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_skill_capabilities (
    skill_id TEXT NOT NULL REFERENCES candidate_skills(id) ON DELETE RESTRICT,
    capability_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    claim_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (skill_id, capability_id)
);

CREATE TABLE IF NOT EXISTS candidate_skill_uses (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    skill_id TEXT NOT NULL REFERENCES candidate_skills(id) ON DELETE RESTRICT,
    usage_context TEXT NOT NULL,
    experience_id TEXT REFERENCES candidate_experiences(id) ON DELETE RESTRICT,
    project_id TEXT REFERENCES candidate_projects(id) ON DELETE RESTRICT,
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
    claim_id TEXT NOT NULL DEFAULT '',
    CONSTRAINT candidate_skill_uses_context_check CHECK (usage_context IN ('commercial', 'pet_project', 'educational', 'personal', 'studied_only', 'unknown', 'explicitly_not_used')),
    CONSTRAINT candidate_skill_uses_parent_check CHECK (experience_id IS NOT NULL OR project_id IS NOT NULL OR usage_context IN ('unknown', 'studied_only', 'explicitly_not_used'))
);
CREATE INDEX IF NOT EXISTS candidate_skill_uses_skill_idx ON candidate_skill_uses(skill_id, id);
CREATE INDEX IF NOT EXISTS candidate_skill_uses_project_idx ON candidate_skill_uses(project_id, id);
CREATE INDEX IF NOT EXISTS candidate_skill_uses_experience_idx ON candidate_skill_uses(experience_id, id);

CREATE TABLE IF NOT EXISTS candidate_achievements (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    title TEXT NOT NULL, problem TEXT NOT NULL DEFAULT '',
    solution JSONB NOT NULL DEFAULT '[]'::jsonb, actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    result JSONB NOT NULL DEFAULT '[]'::jsonb, technologies JSONB NOT NULL DEFAULT '[]'::jsonb,
    project_id TEXT REFERENCES candidate_projects(id) ON DELETE RESTRICT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_achievements_candidate_idx ON candidate_achievements(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_preferences (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL, value TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0,
    claim_id TEXT NOT NULL DEFAULT '', metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_preferences_candidate_kind_idx ON candidate_preferences(candidate_id, kind, id);

CREATE TABLE IF NOT EXISTS candidate_constraints (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL, operator TEXT NOT NULL DEFAULT 'equals', value TEXT NOT NULL,
    mandatory BOOLEAN NOT NULL DEFAULT TRUE, claim_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_constraints_candidate_kind_idx ON candidate_constraints(candidate_id, kind, id);

CREATE TABLE IF NOT EXISTS candidate_claims (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, field TEXT NOT NULL,
    value TEXT NOT NULL, polarity TEXT NOT NULL, state TEXT NOT NULL,
    conflict_set_id TEXT NOT NULL DEFAULT '', supersedes_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT candidate_claims_polarity_check CHECK (polarity IN ('positive', 'negative')),
    CONSTRAINT candidate_claims_state_check CHECK (state IN ('active', 'disputed', 'superseded'))
);
CREATE INDEX IF NOT EXISTS candidate_claims_subject_idx ON candidate_claims(candidate_id, subject_type, subject_id, id);
CREATE INDEX IF NOT EXISTS candidate_claims_conflict_idx ON candidate_claims(candidate_id, conflict_set_id);

CREATE TABLE IF NOT EXISTS candidate_stories (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS candidate_stories_candidate_idx ON candidate_stories(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_story_refs (
    story_id TEXT NOT NULL REFERENCES candidate_stories(id) ON DELETE RESTRICT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (story_id, entity_type, entity_id)
);

CREATE TABLE IF NOT EXISTS candidate_unknowns (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    question TEXT NOT NULL, related_entity TEXT NOT NULL DEFAULT '', hypothesis TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL, metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT candidate_unknowns_status_check CHECK (status IN ('needs_confirmation', 'confirmed', 'rejected'))
);
CREATE INDEX IF NOT EXISTS candidate_unknowns_candidate_idx ON candidate_unknowns(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_knowledge_proposals (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
    proposed_value JSONB NOT NULL, reason TEXT NOT NULL, source TEXT NOT NULL,
    confidence DOUBLE PRECISION, status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    created_at_ns BIGINT, base_value JSONB,
    CONSTRAINT candidate_proposals_status_check CHECK (status IN ('pending', 'confirmed', 'rejected')),
    CONSTRAINT candidate_proposals_source_check CHECK (source IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown'))
);
CREATE INDEX IF NOT EXISTS candidate_proposals_candidate_idx ON candidate_knowledge_proposals(candidate_id, id);

CREATE TABLE IF NOT EXISTS candidate_knowledge_events (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    timestamp TIMESTAMPTZ NOT NULL, timestamp_ns BIGINT, action TEXT NOT NULL,
    entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
    old_value JSONB, new_value JSONB,
    source TEXT NOT NULL, actor TEXT NOT NULL,
    CONSTRAINT candidate_events_source_check CHECK (source IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown'))
);
CREATE INDEX IF NOT EXISTS candidate_events_candidate_created_idx ON candidate_knowledge_events(candidate_id, timestamp, id);

-- Queryable provenance indexes. The metadata JSONB columns remain the
-- canonical per-entity value; these rows make source/evidence searchable
-- without inventing one source table per entity kind.
CREATE TABLE IF NOT EXISTS candidate_knowledge_sources (
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, source_index INTEGER NOT NULL,
    source_type TEXT NOT NULL, reference TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb, observed_at TIMESTAMPTZ,
    observed_at_ns BIGINT,
    CONSTRAINT candidate_sources_type_check CHECK (source_type IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown')),
    PRIMARY KEY (candidate_id, owner_type, owner_id, source_index)
);
CREATE INDEX IF NOT EXISTS candidate_sources_lookup_idx ON candidate_knowledge_sources(candidate_id, source_type, owner_type, owner_id);

CREATE TABLE IF NOT EXISTS candidate_evidence (
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, evidence_index INTEGER NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (candidate_id, owner_type, owner_id, evidence_index)
);
CREATE INDEX IF NOT EXISTS candidate_evidence_lookup_idx ON candidate_evidence(candidate_id, owner_type, owner_id);
