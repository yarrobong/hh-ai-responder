package main

import (
	"context"
	"sync"
	"time"
)

// Only fixed operation names are recorded: never URLs, IDs, prompts or bodies.
type PerfSummary struct {
	Operation string        `json:"operation"`
	Calls     int64         `json:"calls"`
	Duration  time.Duration `json:"duration_ns"`
	Items     int64         `json:"items"`
}

var performance = struct {
	sync.Mutex
	values map[string]PerfSummary
}{values: map[string]PerfSummary{}}

func perfRecord(operation string, start time.Time, items int) {
	performance.Lock()
	defer performance.Unlock()
	v := performance.values[operation]
	v.Operation = operation
	v.Calls++
	v.Duration += time.Since(start)
	v.Items += int64(items)
	performance.values[operation] = v
}
func perfSnapshot() map[string]PerfSummary {
	performance.Lock()
	defer performance.Unlock()
	out := make(map[string]PerfSummary, len(performance.values))
	for k, v := range performance.values {
		out[k] = v
	}
	return out
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
