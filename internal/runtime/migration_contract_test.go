package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestCandidateAcquisitionMigrationHasNarrowRollback(t *testing.T) {
	up, err := os.ReadFile("migrations/000005_candidate_acquisition.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000005_candidate_acquisition.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, column := range []string{
		"gap_key", "source", "conversation_id", "application_id", "vacancy_id", "employer_message_id",
		"unknown_id", "clarification_id", "proposal_id",
	} {
		if !strings.Contains(upText, column) || !strings.Contains(downText, "DROP COLUMN IF EXISTS "+column) {
			t.Fatalf("migration column %q is not paired", column)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP EXTENSION", "DROP DATABASE"} {
		if strings.Contains(strings.ToUpper(downText), forbidden) {
			t.Fatalf("narrow acquisition rollback contains forbidden operation %q", forbidden)
		}
	}
	for _, restored := range []string{
		"status IN ('needs_confirmation', 'confirmed', 'rejected')",
		"usage_context IN ('unknown', 'studied_only', 'explicitly_not_used')",
		"source IN ('user_confirmed', 'hh_resume', 'github_verified', 'candidate_interview', 'project_analysis', 'derived', 'unknown')",
	} {
		if !strings.Contains(downText, restored) {
			t.Fatalf("rollback does not restore pre-000005 contract %q", restored)
		}
	}
}

func TestSemanticContractMigrationIsDimensionFlexibleAndRollbackSafe(t *testing.T) {
	up, err := os.ReadFile("migrations/000010_candidate_semantic_contract.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000010_candidate_semantic_contract.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, required := range []string{"ALTER COLUMN embedding TYPE vector USING embedding::vector", "embedding_provider", "embedding_space_id", "vector_dims(embedding)", "embedding_dimensions <= 16000", "legacy"} {
		if !strings.Contains(upText, required) {
			t.Fatalf("S2 migration missing %q", required)
		}
	}
	if !strings.Contains(downText, "cannot rollback candidate semantic contract while non-1536 vectors exist") || strings.Contains(strings.ToUpper(downText), "DELETE FROM") {
		t.Fatalf("down migration is not an explicit safe refusal: %s", downText)
	}
}

func TestVacancyFreshnessReviewMigrationIsAdditiveAndConstrained(t *testing.T) {
	up, err := os.ReadFile("migrations/000011_vacancy_freshness_review.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000011_vacancy_freshness_review.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, required := range []string{
		"vacancy_freshness", "vacancy_review_states", "vacancy_review_events",
		"ON DELETE RESTRICT", "source_fingerprint", "material_fingerprint",
		"fingerprint_version", "event_type IN ('seen', 'interesting', 'dismissed', 'prepared', 'applied')",
	} {
		if !strings.Contains(upText, required) {
			t.Fatalf("P1.1 migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(upText), "UPDATE VACANCIES") || strings.Contains(strings.ToUpper(upText), "DELETE FROM VACANCIES") {
		t.Fatal("P1.1 migration rewrites or deletes legacy vacancies")
	}
	for _, table := range []string{"vacancy_review_events", "vacancy_review_states", "vacancy_freshness"} {
		if !strings.Contains(downText, "DROP TABLE IF EXISTS "+table) {
			t.Fatalf("P1.1 down migration does not remove %s", table)
		}
	}
}

func TestVacancyProviderEnrichmentMigrationIsAdditiveAndPaired(t *testing.T) {
	up, err := os.ReadFile("migrations/000012_vacancy_provider_enrichment.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000012_vacancy_provider_enrichment.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "ADD COLUMN IF NOT EXISTS professional_roles JSONB") || !strings.Contains(string(down), "DROP COLUMN IF EXISTS professional_roles") {
		t.Fatalf("provider enrichment migration is not additive/paired")
	}
}

func TestVacancyFingerprintVersionMigrationIsAdditiveAndPaired(t *testing.T) {
	up, err := os.ReadFile("migrations/000013_vacancy_fingerprint_versions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000013_vacancy_fingerprint_versions.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, required := range []string{"decision_fingerprint_version INTEGER NOT NULL DEFAULT 1", "fingerprint_version INTEGER NOT NULL DEFAULT 1", "decision_fingerprint_version > 0", "fingerprint_version > 0"} {
		if !strings.Contains(upText, required) {
			t.Fatalf("fingerprint migration missing %q", required)
		}
	}
	for _, required := range []string{"DROP COLUMN IF EXISTS fingerprint_version", "DROP COLUMN IF EXISTS decision_fingerprint_version"} {
		if !strings.Contains(downText, required) {
			t.Fatalf("fingerprint migration rollback missing %q", required)
		}
	}
}
