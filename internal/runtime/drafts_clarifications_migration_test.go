package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestDraftsAndClarificationsMigrationIsAdditiveAndParitySafe(t *testing.T) {
	up, err := os.ReadFile("migrations/000017_drafts_clarifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000017_drafts_clarifications.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(up)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS ai_drafts",
		"CREATE TABLE IF NOT EXISTS candidate_clarifications",
		"ai_drafts_input_fingerprint_key",
		"identity TEXT NOT NULL UNIQUE",
		"CREATE UNIQUE INDEX IF NOT EXISTS ai_drafts_input_fingerprint_key",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(text), "drop table") || strings.Contains(strings.ToLower(text), "truncate") || strings.Contains(strings.ToLower(text), "delete from") {
		t.Fatal("up migration is destructive")
	}
	for _, required := range []string{"DROP TABLE IF EXISTS candidate_clarifications", "DROP TABLE IF EXISTS ai_drafts"} {
		if !strings.Contains(string(down), required) {
			t.Fatalf("down migration missing %q", required)
		}
	}

	root, err := os.ReadFile("../../migrations/000017_drafts_clarifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(root) != text {
		t.Fatal("root and embedded migration authorities differ")
	}
}
