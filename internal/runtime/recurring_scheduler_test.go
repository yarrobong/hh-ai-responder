package runtime

import (
	"testing"
	"time"
)

func TestRecurringTaskIntervalsPreserveExecutableCadence(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{name: "auto-apply", interval: autoApplyInterval, want: 12 * time.Hour},
		{name: "auto-chat", interval: autoChatInterval, want: 15 * time.Minute},
		{name: "resume-touch", interval: autoTouchInterval, want: 4 * time.Hour},
		{name: "job-status", interval: autoJobStatusInterval, want: 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.interval != tt.want {
				t.Fatalf("interval = %s, want %s", tt.interval, tt.want)
			}
		})
	}
}
