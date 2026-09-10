package platform

import (
	"sync"
	"time"
)

// PerfSummary contains generic operation measurements. Operation names are
// supplied by callers and should remain fixed, non-sensitive identifiers.
type PerfSummary struct {
	Operation string        `json:"operation"`
	Calls     int64         `json:"calls"`
	Duration  time.Duration `json:"duration_ns"`
	Items     int64         `json:"items"`
}

// Metrics is a thread-safe collection of operation measurements.
type Metrics struct {
	mu     sync.Mutex
	values map[string]PerfSummary
}

func NewMetrics() *Metrics {
	return &Metrics{values: map[string]PerfSummary{}}
}

// Record adds one completed operation measurement.
func (m *Metrics) Record(operation string, start time.Time, items int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]PerfSummary{}
	}
	v := m.values[operation]
	v.Operation = operation
	v.Calls++
	v.Duration += time.Since(start)
	v.Items += int64(items)
	m.values[operation] = v
}

// Snapshot returns a copy that callers may inspect or modify independently.
func (m *Metrics) Snapshot() map[string]PerfSummary {
	if m == nil {
		return map[string]PerfSummary{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]PerfSummary, len(m.values))
	for k, v := range m.values {
		out[k] = v
	}
	return out
}
