package runtime

import (
	"context"
	"sync"
	"time"

	"hh-ai-responder/internal/platform"
)

// PerfSummary remains a compatibility name for the dashboard and sync
// response shapes while its implementation is owned by platform.
type PerfSummary = platform.PerfSummary

var performance = platform.NewMetrics()

// Transitional compatibility wrappers keep the current package-main callers
// stable while the remaining performance consumers move with their owning
// packages. New code should use performance.Record/Snapshot directly.
func perfRecord(operation string, start time.Time, items int) {
	performance.Record(operation, start, items)
}

func perfSnapshot() map[string]PerfSummary {
	return performance.Snapshot()
}

// Bounded memoization of a pure normalization function; contains no decisions.
var canonicalMemo = struct {
	sync.RWMutex
	values map[string]string
}{values: map[string]string{}}

func cachedContextCanonical(value string) string {
	if len(value) > 256 {
		return uncachedContextCanonical(value)
	}
	canonicalMemo.RLock()
	result, ok := canonicalMemo.values[value]
	canonicalMemo.RUnlock()
	if ok {
		return result
	}
	result = uncachedContextCanonical(value)
	canonicalMemo.Lock()
	if len(canonicalMemo.values) >= 2048 {
		canonicalMemo.values = map[string]string{}
	}
	canonicalMemo.values[value] = result
	canonicalMemo.Unlock()
	return result
}

type OperationPerformance struct {
	Duration        time.Duration `json:"duration_ns"`
	NetworkDuration time.Duration `json:"network_duration_ns"`
	RateWait        time.Duration `json:"rate_wait_ns"`
	DiskDuration    time.Duration `json:"disk_duration_ns"`
	ComputeDuration time.Duration `json:"compute_duration_ns"`
	Requests        int           `json:"requests"`
	CacheHits       int           `json:"cache_hits"`
	CacheMisses     int           `json:"cache_misses"`
}
type operationMeter struct {
	sync.Mutex
	value OperationPerformance
}
type operationMeterKey struct{}

func meterRecord(ctx context.Context, kind string, d time.Duration) {
	m, _ := ctx.Value(operationMeterKey{}).(*operationMeter)
	if m == nil {
		return
	}
	m.Lock()
	defer m.Unlock()
	switch kind {
	case "network":
		m.value.NetworkDuration += d
		m.value.Requests++
	case "wait":
		m.value.RateWait += d
	case "disk":
		m.value.DiskDuration += d
	case "compute":
		m.value.ComputeDuration += d
	case "hit":
		m.value.CacheHits++
	case "miss":
		m.value.CacheMisses++
	}
}
