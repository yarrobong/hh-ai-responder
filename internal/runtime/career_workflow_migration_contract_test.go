package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestCareerWorkflowMigrationIsAdditiveAndPaired(t *testing.T) {
	up, err := os.ReadFile("migrations/000015_career_workflow.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000015_career_workflow.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, table := range []string{"agent_runs", "agent_run_items", "application_preparations"} {
		if !strings.Contains(upText, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("career workflow migration missing table %q", table)
		}
		if !strings.Contains(downText, "DROP TABLE IF EXISTS "+table) {
			t.Fatalf("career workflow rollback missing table %q", table)
		}
	}
	for _, required := range []string{
		"UNIQUE(run_id, vacancy_id)",
		"UNIQUE(vacancy_id, input_fingerprint)",
		"evidence_json JSONB",
		"test_answer_drafts_json JSONB",
		"knowledge_requests_json JSONB",
		"REFERENCES agent_runs(id) ON DELETE CASCADE",
		"confidence >= 0 AND confidence <= 1",
	} {
		if !strings.Contains(upText, required) {
			t.Fatalf("career workflow migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"cookie", "authorization", "api_key", "prompt", "token"} {
		if strings.Contains(strings.ToLower(upText), forbidden) {
			t.Fatalf("career workflow migration contains forbidden sensitive field marker %q", forbidden)
		}
	}
}
