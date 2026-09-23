package careeragent

import (
	"testing"
	"time"
)

func TestDailyRunIDIsStableForUTCDate(t *testing.T) {
	first := DailyRunID(time.Date(2026, 9, 24, 6, 2, 3, 0, time.FixedZone("yek", 5*60*60)))
	second := DailyRunID(time.Date(2026, 9, 24, 23, 59, 0, 0, time.UTC))
	if first != second || first != "daily-career-agent-2026-09-24" {
		t.Fatalf("daily ids = %q and %q", first, second)
	}
}

func TestDailyResultCodeMapsToStableHumanResult(t *testing.T) {
	for _, test := range []struct {
		status AgentRunStatus
		want   DailyResultCode
	}{
		{AgentRunStatusCompleted, DailyResultSuccess},
		{AgentRunStatusPartial, DailyResultPartialSuccess},
		{AgentRunStatusFailed, DailyResultFailed},
	} {
		if got := DailyResultForStatus(test.status); got != test.want {
			t.Fatalf("status %q -> %q, want %q", test.status, got, test.want)
		}
	}
}
