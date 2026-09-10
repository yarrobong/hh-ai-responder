package platform

import (
	"sync"
	"testing"
	"time"
)

func TestMetricsRecordAndSnapshot(t *testing.T) {
	m := NewMetrics()
	start := time.Now().Add(-time.Millisecond)
	m.Record("read", start, 2)
	m.Record("read", start, 3)
	snapshot := m.Snapshot()
	if got := snapshot["read"]; got.Calls != 2 || got.Items != 5 || got.Duration <= 0 {
		t.Fatalf("snapshot = %+v", got)
	}
	snapshot["read"] = PerfSummary{}
	if got := m.Snapshot()["read"]; got.Calls != 2 {
		t.Fatalf("snapshot mutated metrics: %+v", got)
	}
}

func TestMetricsIsSafeForConcurrentRecorders(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Record("concurrent", time.Now(), 1)
			}
		}()
	}
	wg.Wait()
	if got := m.Snapshot()["concurrent"]; got.Calls != 3200 || got.Items != 3200 {
		t.Fatalf("concurrent snapshot = %+v", got)
	}
}
