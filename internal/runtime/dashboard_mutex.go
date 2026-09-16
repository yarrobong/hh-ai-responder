package runtime

import (
	"sync"
	"time"
)

// dashboardTimedLocker measures the sync service's externally supplied
// dashboard lock as well as ordinary HTTP readers. It is a small diagnostic
// seam, not a second concurrency policy.
type dashboardTimedLocker struct {
	locker    sync.Locker
	holdStart time.Time
}

func (m *dashboardTimedLocker) Lock() {
	waitStart := time.Now()
	m.locker.Lock()
	perfRecord("dashboard.mutex_wait", waitStart, 1)
	m.holdStart = time.Now()
}

func (m *dashboardTimedLocker) Unlock() {
	perfRecord("dashboard.mutex_hold", m.holdStart, 1)
	m.locker.Unlock()
}
