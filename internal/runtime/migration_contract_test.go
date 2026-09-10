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
