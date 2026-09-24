package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestDailyRunResultMigrationIsAdditiveAndPaired(t *testing.T) {
	up, err := os.ReadFile("migrations/000018_daily_run_result.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000018_daily_run_result.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	if !strings.Contains(upText, "ALTER TABLE agent_runs") || !strings.Contains(upText, "ADD COLUMN IF NOT EXISTS daily_result_json JSONB") {
		t.Fatalf("daily run result migration is not additive: %s", upText)
	}
	if strings.Contains(strings.ToLower(upText), "drop ") || strings.Contains(strings.ToLower(upText), "delete ") || strings.Contains(strings.ToLower(upText), "truncate ") {
		t.Fatal("daily run result up migration is destructive")
	}
	if !strings.Contains(downText, "ALTER TABLE agent_runs") || !strings.Contains(downText, "DROP COLUMN IF EXISTS daily_result_json") {
		t.Fatalf("daily run result down migration is not paired: %s", downText)
	}
	root, err := os.ReadFile("../../migrations/000018_daily_run_result.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(root) != upText {
		t.Fatal("root and embedded daily run result migrations differ")
	}
}
