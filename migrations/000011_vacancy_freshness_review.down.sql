-- Explicit rollback removes only the P1.1 objects. It is intentionally not
-- run by application startup; review history is retained during normal use.
DROP TABLE IF EXISTS vacancy_review_events;
DROP TABLE IF EXISTS vacancy_review_states;
DROP TABLE IF EXISTS vacancy_freshness;
