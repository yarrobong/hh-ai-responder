package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestApplicationAttemptMigrationHasAtomicBlockingConstraint(t *testing.T) {
	up, err := os.ReadFile("migrations/000006_application_attempts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000006_application_attempts.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upText, downText := string(up), string(down)
	for _, value := range []string{"attempt_id", "vacancy_id", "resume_id", "SENDING", "ACCEPTED", "DELIVERY_UNCERTAIN", "automatic_application_attempts_active_vacancy_unique"} {
		if !strings.Contains(upText, value) {
			t.Fatalf("migration missing %q", value)
		}
	}
	if !strings.Contains(strings.ToUpper(upText), "UNIQUE INDEX") || !strings.Contains(downText, "DROP TABLE IF EXISTS automatic_application_attempts") {
		t.Fatal("application-attempt migration is not atomic/narrow")
	}
}
